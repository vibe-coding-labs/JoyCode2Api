package keepalive

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/store"
)

// CredentialStatus represents the health of an account's credentials.
type CredentialStatus struct {
	Valid         bool      `json:"valid"`
	LastChecked   time.Time `json:"last_checked"`
	LastRefreshed time.Time `json:"last_refreshed,omitempty"`
	ErrorMessage  string    `json:"error_message,omitempty"`
}

// Keeper runs periodic keep-alive checks for all accounts.
type Keeper struct {
	store    *store.Store
	mu       sync.RWMutex
	status   map[string]*CredentialStatus
	running  bool
	stopCh   chan struct{}
	refreshTTL time.Duration // max age before an account needs refresh
}

// NewKeeper creates a new keepalive keeper.
// refreshTTL: how old a credential_refreshed_at can be before we re-check (e.g., 1h).
func NewKeeper(s *store.Store, refreshTTL time.Duration) *Keeper {
	return &Keeper{
		store:      s,
		status:     make(map[string]*CredentialStatus),
		stopCh:     make(chan struct{}),
		refreshTTL: refreshTTL,
	}
}

// GetStatus returns the credential status for an account.
func (k *Keeper) GetStatus(apiKey string) *CredentialStatus {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if s, ok := k.status[apiKey]; ok {
		return s
	}
	return nil
}

// GetAllStatuses returns a copy of all credential statuses.
func (k *Keeper) GetAllStatuses() map[string]*CredentialStatus {
	k.mu.RLock()
	defer k.mu.RUnlock()
	result := make(map[string]*CredentialStatus, len(k.status))
	for key, val := range k.status {
		result[key] = val
	}
	return result
}

// Start begins the periodic keep-alive loop.
// checkInterval: how often to scan for stale accounts (e.g., 10min).
func (k *Keeper) Start(checkInterval time.Duration) {
	if k.running {
		return
	}
	k.running = true

	go k.checkStale()

	go func() {
		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				k.checkStale()
			case <-k.stopCh:
				return
			}
		}
	}()
	slog.Info("keepalive: started", "check_interval", checkInterval, "refresh_ttl", k.refreshTTL)
}

// Stop terminates the keep-alive loop.
func (k *Keeper) Stop() {
	if k.running {
		k.running = false
		close(k.stopCh)
		slog.Info("keepalive: stopped")
	}
}

// checkStale queries accounts whose credential_refreshed_at exceeds the TTL
// and refreshes only those. Accounts never refreshed (empty field) are always checked.
func (k *Keeper) checkStale() {
	accounts, err := k.store.ListStaleAccounts(k.refreshTTL)
	if err != nil {
		slog.Error("keepalive: failed to list stale accounts", "error", err)
		return
	}
	if len(accounts) == 0 {
		return
	}

	startTime := time.Now()
	slog.Info("keepalive: round started", "stale_count", len(accounts), "refresh_ttl", k.refreshTTL)

	var validCount, refreshedCount, failedCount int

	for i, acc := range accounts {
		slog.Info("keepalive: checking account",
			"progress", fmt.Sprintf("%d/%d", i+1, len(accounts)),
			"api_key", acc.UserID,
			"user_id", acc.UserID,
		)

		result := k.checkOne(acc.UserID, acc.PtKey, acc.UserID)

		switch result {
		case "valid":
			validCount++
		case "refreshed":
			refreshedCount++
		case "failed":
			failedCount++
		}

		if i < len(accounts)-1 {
			time.Sleep(5 * time.Second)
		}
	}

	slog.Info("keepalive: round completed",
		"duration", time.Since(startTime).Round(time.Millisecond),
		"valid", validCount,
		"refreshed", refreshedCount,
		"failed", failedCount,
		"total", len(accounts),
	)
}

// checkOne validates a single account and refreshes pt_key if possible.
// Returns "valid", "refreshed", or "failed".
func (k *Keeper) checkOne(apiKey, ptKey, userID string) string {
	if userID == "" {
		slog.Error("keepalive: checkOne called with empty userID")
		return "failed"
	}

	checkStart := time.Now()

	client := joycode.NewClient(ptKey, userID)
	client.SetTimeout(30 * time.Second)

	refreshedPtKey, err := client.UserInfoWithRefresh()
	checkDuration := time.Since(checkStart)
	now := time.Now()

	k.mu.Lock()
	defer k.mu.Unlock()

	if err != nil {
		slog.Warn("keepalive: account check failed",
			"user_id", apiKey,
			"user_id", userID,
			"error", err,
			"duration", checkDuration,
			"pt_key_prefix", maskKey(ptKey),
		)
		k.status[apiKey] = &CredentialStatus{
			Valid:        false,
			LastChecked:  now,
			ErrorMessage: err.Error(),
		}
		k.store.SetCredentialValid(apiKey, false)
		return "failed"
	}

	k.store.SetCredentialValid(apiKey, true)
	status := &CredentialStatus{
		Valid:       true,
		LastChecked: now,
	}

	if refreshedPtKey != "" && refreshedPtKey != ptKey {
		slog.Info("keepalive: pt_key refresh available",
			"user_id", apiKey,
			"user_id", userID,
			"old_prefix", maskKey(ptKey),
			"new_prefix", maskKey(refreshedPtKey),
		)

		if err := k.store.UpdatePtKey(apiKey, refreshedPtKey); err != nil {
			slog.Error("keepalive: failed to save refreshed pt_key",
				"user_id", apiKey,
				"error", err,
			)
		} else {
			status.LastRefreshed = now

			verifyClient := joycode.NewClient(refreshedPtKey, userID)
			verifyClient.SetTimeout(15 * time.Second)
			if verifyErr := verifyClient.Validate(); verifyErr != nil {
				slog.Error("keepalive: refreshed pt_key verification FAILED",
					"user_id", apiKey,
					"user_id", userID,
					"error", verifyErr,
				)
			} else {
				slog.Info("keepalive: refreshed pt_key verified OK",
					"user_id", apiKey,
					"user_id", userID,
				)
			}

			slog.Info("keepalive: pt_key refreshed and saved",
				"user_id", apiKey,
				"user_id", userID,
			)
		}
	} else {
		// No refresh needed, but update credential_refreshed_at so this account
		// doesn't get re-checked next cycle
		k.store.UpdateCredentialRefreshedAt(apiKey)

		slog.Info("keepalive: account valid, no refresh needed",
			"user_id", apiKey,
			"user_id", userID,
			"duration", checkDuration,
		)
	}

	k.status[apiKey] = status
	if refreshedPtKey != "" && refreshedPtKey != ptKey {
		return "refreshed"
	}
	return "valid"
}

// maskKey returns a masked version of a pt_key for logging (first 6...last 6 chars).
func maskKey(key string) string {
	if len(key) <= 12 {
		return "***"
	}
	return key[:6] + "..." + key[len(key)-6:]
}

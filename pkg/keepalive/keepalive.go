package keepalive

import (
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
	store   *store.Store
	mu      sync.RWMutex
	status  map[string]*CredentialStatus
	running bool
	stopCh  chan struct{}
}

// NewKeeper creates a new keepalive keeper.
func NewKeeper(s *store.Store) *Keeper {
	return &Keeper{
		store:  s,
		status: make(map[string]*CredentialStatus),
		stopCh: make(chan struct{}),
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
func (k *Keeper) Start(interval time.Duration) {
	if k.running {
		return
	}
	k.running = true

	go k.checkAll()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				k.checkAll()
			case <-k.stopCh:
				return
			}
		}
	}()
	slog.Info("keepalive: started", "interval", interval)
}

// Stop terminates the keep-alive loop.
func (k *Keeper) Stop() {
	if k.running {
		k.running = false
		close(k.stopCh)
		slog.Info("keepalive: stopped")
	}
}

// checkAll validates all accounts and refreshes credentials if possible.
func (k *Keeper) checkAll() {
	accounts, err := k.store.ListAllAccountsWithCredentials()
	if err != nil {
		slog.Error("keepalive: failed to list accounts", "error", err)
		return
	}
	if len(accounts) == 0 {
		return
	}
	slog.Info("keepalive: checking accounts", "count", len(accounts))

	for _, acc := range accounts {
		k.checkOne(acc.APIKey, acc.PtKey, acc.UserID)
		time.Sleep(5 * time.Second)
	}
}

// checkOne validates a single account and refreshes pt_key if possible.
func (k *Keeper) checkOne(apiKey, ptKey, userID string) {
	client := joycode.NewClient(ptKey, userID)
	client.SetTimeout(30 * time.Second)

	refreshedPtKey, err := client.UserInfoWithRefresh()
	now := time.Now()

	k.mu.Lock()
	defer k.mu.Unlock()

	if err != nil {
		slog.Warn("keepalive: account validation failed",
			"api_key", apiKey, "error", err)
		k.status[apiKey] = &CredentialStatus{
			Valid:        false,
			LastChecked:  now,
			ErrorMessage: err.Error(),
		}
		return
	}

	status := &CredentialStatus{
		Valid:       true,
		LastChecked: now,
	}

	if refreshedPtKey != "" && refreshedPtKey != ptKey {
		if err := k.store.UpdatePtKey(apiKey, refreshedPtKey); err != nil {
			slog.Error("keepalive: failed to save refreshed pt_key",
				"api_key", apiKey, "error", err)
		} else {
			status.LastRefreshed = now
			slog.Info("keepalive: refreshed pt_key", "api_key", apiKey)
		}
	}

	k.status[apiKey] = status
	slog.Info("keepalive: account validated",
		"api_key", apiKey, "refreshed", refreshedPtKey != "" && refreshedPtKey != ptKey)
}

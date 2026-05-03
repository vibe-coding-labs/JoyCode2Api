# Account Keep-Alive (账号凭证保活机制) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 为所有已导入的 JoyCode 账号实现自动保活机制：定期调用 UserInfo API 刷新 pt_key 凭证，防止会话过期，并在前端展示每个账号的凭证健康状态。

**Architecture:** 后台保活 goroutine 定时（默认每 10 小时）遍历所有账号 → 解密 pt_key → 调用 JoyCode UserInfo API → 验证凭证有效性 → 如果响应包含新的 ptKey 则加密后写回 SQLite → 更新内存中的凭证状态缓存 → Dashboard API 读取缓存状态 → 前端展示凭证状态 Tag（有效/过期/未知）。

**Tech Stack:** Go 1.22+, SQLite (mattn/go-sqlite3), React 19, Ant Design 6, TypeScript

**Risks:**
- Task 2 修改 `pkg/joycode/client.go` 新增方法，不影响现有 `Validate()` → 缓解：纯新增方法，不改已有代码
- Task 3 保活 goroutine 中批量验证可能触发上游限流 → 缓解：账号间增加 5 秒间隔
- UserInfo 返回的 data.ptKey 可能为空或与当前相同 → 缓解：仅在非空且不同时才更新数据库
- Task 4 修改前端 Account 接口，可能影响现有展示 → 缓解：新字段均为 optional，无破坏性变更

---

### Task 1: Store 层 — 添加 pt_key 更新和批量凭证查询方法

**Depends on:** None
**Files:**
- Modify: `pkg/store/store.go:568-585`（在 `RenameAccount` 方法后添加新方法）

- [ ] **Step 1: 添加 UpdatePtKey 方法 — 更新指定账号的 pt_key 凭证**

文件: `pkg/store/store.go`（在 `RenameAccount` 方法后、`SetDefault` 方法前，约第 586 行）

```go
// UpdatePtKey updates the encrypted pt_key for an account.
func (s *Store) UpdatePtKey(apiKey, ptKey string) error {
	encPtKey, err := s.encrypt(ptKey)
	if err != nil {
		slog.Error("store: encrypt pt_key for update failed", "api_key", apiKey, "error", err)
		return fmt.Errorf("encrypt pt_key: %w", err)
	}
	_, err = s.db.Exec(
		"UPDATE accounts SET pt_key = ?, updated_at = datetime('now', 'localtime') WHERE api_key = ?",
		encPtKey, apiKey,
	)
	if err != nil {
		slog.Error("store: update pt_key failed", "api_key", apiKey, "error", err)
	}
	return err
}
```

- [ ] **Step 2: 添加 ListAllAccountsWithCredentials 方法 — 批量获取所有账号的解密凭证**

文件: `pkg/store/store.go`（紧接 Step 1 后）

```go
// ListAllAccountsWithCredentials returns all accounts with decrypted pt_keys.
// Used by keepalive to validate and refresh sessions.
func (s *Store) ListAllAccountsWithCredentials() ([]Account, error) {
	rows, err := s.db.Query("SELECT api_key, pt_key, user_id, default_model FROM accounts ORDER BY created_at")
	if err != nil {
		slog.Error("store: list accounts with credentials query failed", "error", err)
		return nil, err
	}
	defer rows.Close()

	var accounts []Account
	for rows.Next() {
		var a Account
		var encPtKey string
		if err := rows.Scan(&a.APIKey, &encPtKey, &a.UserID, &a.DefaultModel); err != nil {
			slog.Error("store: list accounts with credentials scan failed", "error", err)
			return nil, err
		}
		ptKey, err := s.decrypt(encPtKey)
		if err != nil {
			slog.Error("store: decrypt pt_key failed for keepalive", "api_key", a.APIKey, "error", err)
			continue
		}
		a.PtKey = ptKey
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}
```

- [ ] **Step 3: 验证编译通过**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./pkg/store/`
Expected:
  - Exit code: 0
  - Output does NOT contain: "Error" or "undefined"

---

### Task 2: JoyCode Client — 添加 UserInfoWithRefresh 方法

**Depends on:** Task 1
**Files:**
- Modify: `pkg/joycode/client.go:213-231`（在 `UserInfo()` 和 `Validate()` 方法后）

- [ ] **Step 1: 添加 UserInfoWithRefresh 方法 — 调用 UserInfo 并返回刷新后的 ptKey**

文件: `pkg/joycode/client.go`（在 `Validate()` 方法后，约第 232 行）

```go
// UserInfoWithRefresh calls the UserInfo API and returns the refreshed ptKey
// from the response data, if present. Returns (refreshedPtKey, nil) on success.
func (c *Client) UserInfoWithRefresh() (string, error) {
	resp, err := c.UserInfo()
	if err != nil {
		return "", fmt.Errorf("user info request failed: %w", err)
	}
	code, ok := resp["code"].(float64)
	if !ok || code != 0 {
		msg, _ := resp["msg"].(string)
		if msg == "" {
			msg = "unknown error"
		}
		return "", fmt.Errorf("user info failed (code=%.0f): %s", code, msg)
	}
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		return "", nil
	}
	if ptKey, ok := data["ptKey"].(string); ok && ptKey != "" {
		return ptKey, nil
	}
	return "", nil
}
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./pkg/joycode/`
Expected:
  - Exit code: 0
  - Output does NOT contain: "Error" or "undefined"

---

### Task 3: Keepalive 包 — 保活 goroutine 和凭证状态缓存

**Depends on:** Task 1, Task 2
**Files:**
- Create: `pkg/keepalive/keepalive.go`

- [ ] **Step 1: 创建 keepalive 包 — 保活调度器和凭证状态缓存**

```go
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
	Valid          bool      `json:"valid"`
	LastChecked    time.Time `json:"last_checked"`
	LastRefreshed  time.Time `json:"last_refreshed,omitempty"`
	ErrorMessage   string    `json:"error_message,omitempty"`
}

// Keeper runs periodic keep-alive checks for all accounts.
type Keeper struct {
	store   *store.Store
	mu      sync.RWMutex
	status  map[string]*CredentialStatus // apiKey → status
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

	// Run first check immediately in background
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
		// Stagger requests to avoid rate limiting
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

	// If server returned a refreshed ptKey, save it
	if refreshedPtKey != "" && refreshedPtKey != ptKey {
		if err := k.store.UpdatePtKey(apiKey, refreshedPtKey); err != nil {
			slog.Error("keepalive: failed to save refreshed pt_key",
				"api_key", apiKey, "error", err)
		} else {
			status.LastRefreshed = now
			slog.Info("keepalive: refreshed pt_key",
				"api_key", apiKey)
		}
	}

	k.status[apiKey] = status
	slog.Info("keepalive: account validated",
		"api_key", apiKey, "refreshed", refreshedPtKey != "" && refreshedPtKey != ptKey)
}
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./pkg/keepalive/`
Expected:
  - Exit code: 0
  - Output does NOT contain: "Error" or "undefined"

---

### Task 4: 集成保活到 serve.go + Dashboard API + 前端状态展示

**Depends on:** Task 3
**Files:**
- Modify: `cmd/JoyCodeProxy/serve.go:80-81`（store 初始化后启动 keepalive）
- Modify: `cmd/JoyCodeProxy/serve.go:228-230`（shutdown 时停止 keepalive）
- Modify: `pkg/dashboard/handler.go`（注入凭证状态到账号列表响应）
- Modify: `web/src/api.ts`（Account 接口添加凭证状态字段）
- Modify: `web/src/pages/Accounts.tsx`（展示凭证状态 Tag）

- [ ] **Step 1: 修改 serve.go 启动 keepalive goroutine — 在 store 初始化后创建并启动 Keeper**

文件: `cmd/JoyCodeProxy/serve.go`

添加 import（在现有 import 块中）:
```go
"github.com/vibe-coding-labs/JoyCodeProxy/pkg/keepalive"
```

在 store 初始化代码块后（约第 80 行 `s.MigrateTokenLogs()` 之后），添加 keepalive 启动代码:
```go
			// Start credential keepalive
			var keeper *keepalive.Keeper
			if s != nil {
				keeper = keepalive.NewKeeper(s)
				keepAliveInterval := 10 * time.Hour
				keeper.Start(keepAliveInterval)
			}
```

- [ ] **Step 2: 修改 serve.go — shutdown 时停止 Keeper**

文件: `cmd/JoyCodeProxy/serve.go`（约第 228-230 行，`s.Close()` 之前）

```go
			if keeper != nil {
				keeper.Stop()
			}
			if s != nil {
				s.Close()
			}
```

- [ ] **Step 3: 修改 dashboard handler — 注入 keeper 并在账号列表中返回凭证状态**

文件: `pkg/dashboard/handler.go`

在 `Handler` 结构体中添加 `keeper` 字段。需要先查看 Handler 结构体定义的位置和 `NewHandler` 函数的签名。

修改 `NewHandler` 函数签名以接受 keeper:
```go
func NewHandler(s *store.Store, staticFS fs.FS, k *keepalive.Keeper) *Handler {
	return &Handler{store: s, staticFS: staticFS, keeper: k}
}
```

在 Handler 结构体中添加字段:
```go
keeper *keepalive.Keeper
```

在 `listAccounts` 方法中（获取 accounts 后、写入响应前），注入凭证状态:
```go
	if h.keeper != nil {
		statuses := h.keeper.GetAllStatuses()
		for i := range accounts {
			if s, ok := statuses[accounts[i].APIKey]; ok {
				accounts[i].CredentialValid = s.Valid
				accounts[i].CredentialCheckedAt = s.LastChecked.Format("2006-01-02 15:04:05")
				accounts[i].CredentialError = s.ErrorMessage
			}
		}
	}
```

- [ ] **Step 4: 修改 serve.go 中 NewHandler 调用 — 传入 keeper**

文件: `cmd/JoyCodeProxy/serve.go`（约第 168 行）

将:
```go
				dash := dashboard.NewHandler(s, subFS)
```
改为:
```go
				dash := dashboard.NewHandler(s, subFS, keeper)
```

- [ ] **Step 5: 修改 store AccountInfo 结构体 — 添加凭证状态字段**

文件: `pkg/store/store.go` 的 `AccountInfo` 结构体（约第 37-49 行）

在结构体中添加三个新字段:
```go
	CredentialValid     bool   `json:"credential_valid,omitempty"`
	CredentialCheckedAt string `json:"credential_checked_at,omitempty"`
	CredentialError     string `json:"credential_error,omitempty"`
```

- [ ] **Step 6: 修改前端 api.ts — Account 接口添加凭证状态字段**

文件: `web/src/api.ts` 的 `Account` interface（约第 1-13 行）

添加三个新字段:
```typescript
  credential_valid?: boolean;
  credential_checked_at?: string;
  credential_error?: string;
```

- [ ] **Step 7: 修改前端 Accounts.tsx — 展示凭证状态 Tag**

文件: `web/src/pages/Accounts.tsx`

在"状态"列（约第 264-268 行）旁边或之前，添加"凭证"列:

在 columns 数组中，在"状态"列之前添加新的"凭证状态"列:
```tsx
    {
      title: '凭证状态',
      key: 'credential_status',
      render: (_: unknown, record: Account) => {
        if (!record.credential_checked_at) {
          return <Tag>等待检测</Tag>;
        }
        if (record.credential_valid) {
          return (
            <Tooltip title={`上次检测：${record.credential_checked_at}`}>
              <Tag color="green">有效</Tag>
            </Tooltip>
          );
        }
        return (
          <Tooltip title={record.credential_error || '凭证已过期，请重新登录'}>
            <Tag color="red">已过期</Tag>
          </Tooltip>
        );
      },
    },
```

- [ ] **Step 8: 构建前端**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build`
Expected:
  - Exit code: 0
  - Output contains: "built in"

- [ ] **Step 9: 构建后端并验证编译**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build -o cmd/JoyCodeProxy/JoyCodeProxy ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0
  - Output does NOT contain: "Error" or "undefined"

---

### Task 5: 部署和验证

**Depends on:** Task 4
**Files:**
- None (verification only)

- [ ] **Step 1: 重启服务**

Run: `launchctl unload ~/Library/LaunchAgents/com.joycode.proxy.plist && launchctl load ~/Library/LaunchAgents/com.joycode.proxy.plist`
Expected:
  - Exit code: 0

- [ ] **Step 2: 验证服务启动并检查 keepalive 日志**

Run: `sleep 3 && curl -s http://localhost:34891/api/health`
Expected:
  - Exit code: 0
  - Output contains: "status" and "ok"

- [ ] **Step 3: 提交所有变更**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && git add pkg/keepalive/keepalive.go pkg/store/store.go pkg/joycode/client.go cmd/JoyCodeProxy/serve.go pkg/dashboard/handler.go web/src/api.ts web/src/pages/Accounts.tsx cmd/JoyCodeProxy/static/ && git commit -m "feat(keepalive): add automatic credential keep-alive mechanism with pt_key refresh"`

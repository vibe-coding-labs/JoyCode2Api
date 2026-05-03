# UserID Masking & Active Session Counting Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 前端用户ID展示打码保护隐私 + 实时统计每个账号的活跃 Claude Code 会话数量。

**Architecture:** 用户ID打码纯前端实现，用 `maskUserId()` 工具函数将 `jd_xxx` → `jd_***xxx`，`陈继成9527` → `陈***27`，仅影响展示不影响存储和API传输。活跃会话计数通过后端中间件 atomic counter 追踪每个 api_key 的 in-flight 请求数，通过 `/api/accounts` 响应中的 `active_sessions` 字段传递到前端展示。

**Tech Stack:** Go 1.22, React 19, Ant Design 6, TypeScript 6, sync/atomic for concurrent counters

**Risks:**
- 活跃会话计数基于内存，服务重启归零 → 缓解：这是瞬时状态指标，重启后归零符合预期
- maskUserId 函数需要覆盖多种 user_id 格式（jd_ 前缀、中文用户名、纯数字等） → 缓解：针对不同模式分别处理

---

### Task 1: 前端用户ID展示打码

**Depends on:** None
**Files:**
- Modify: `web/src/pages/Accounts.tsx:210-213`（用户 ID 列渲染）
- Modify: `web/src/pages/AccountDetail.tsx:279-281`（详情页 user_id 展示）

- [ ] **Step 1: 在 Accounts.tsx 添加 maskUserId 函数并修改用户 ID 列渲染**

在 `Accounts.tsx` 文件顶部（`getBaseURL` 函数之后）添加 maskUserId 工具函数，然后修改用户 ID 列的 render 函数使用它。

在 `const getBaseURL = () => \`http://${window.location.host}\`;` 之后添加：

```typescript
const maskUserId = (id: string): string => {
  if (!id) return '-';
  if (id.length <= 3) return id[0] + '***';
  return id.slice(0, 2) + '***' + id.slice(-2);
};
```

修改用户 ID 列（Accounts.tsx:210-213）：

```typescript
    {
      title: '用户 ID',
      dataIndex: 'user_id',
      key: 'user_id',
      render: (text: string) => (
        <Typography.Text type="secondary" style={{ fontSize: 13 }}>{maskUserId(text)}</Typography.Text>
      ),
    },
```

- [ ] **Step 2: 在 AccountDetail.tsx 的 user_id 展示位置应用打码**

修改 AccountDetail.tsx:279-281 的 user_id 展示：

```typescript
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {account.user_id ? account.user_id.slice(0, 2) + '***' + account.user_id.slice(-2) : '-'} · 创建于 {account.created_at?.slice(0, 10) || '-'}
          </Typography.Text>
```

- [ ] **Step 3: 验证前端打码效果**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build`
Expected:
  - Exit code: 0
  - Output contains: "built in"
  - Output does NOT contain: "error"

- [ ] **Step 4: 提交**
Run: `git add web/src/pages/Accounts.tsx web/src/pages/AccountDetail.tsx && git commit -m "feat(ui): mask user_id display for privacy protection"`

---

### Task 2: 活跃会话计数

**Depends on:** None
**Files:**
- Modify: `cmd/JoyCodeProxy/serve.go:250-355`（requestLogMiddleware 添加计数器）
- Modify: `pkg/store/store.go:37-44`（AccountInfo 添加 ActiveSessions 字段）
- Modify: `pkg/dashboard/handler.go`（列表接口注入 active_sessions）
- Modify: `web/src/api.ts:1-8`（Account 类型添加 active_sessions）
- Modify: `web/src/pages/Accounts.tsx`（添加活跃会话列）

- [ ] **Step 1: 在 serve.go 添加全局活跃请求追踪器**

在 `cmd/JoyCodeProxy/serve.go` 文件的 import 块中添加 `"sync"` 和 `"github.com/vibe-coding-labs/JoyCodeProxy/pkg/store"` 已存在。添加一个全局的 SessionTracker：

在 `var requestCounter uint64` 之后（serve.go:33 附近）添加：

```go
var (
	serveHost       string
	servePort       int
	requestCounter  uint64
	activeSessions  sync.Map // map[string]*atomic.Int64 — api_key → in-flight count
)
```

注意：`sync/atomic` 已在 import 中，需要额外添加 `"sync"` 到 import 块。

- [ ] **Step 2: 在 requestLogMiddleware 中追踪活跃请求**

在 `requestLogMiddleware` 函数中，在解析出 apiKey 之后、调用 `next.ServeHTTP` 之前增加活跃计数，在 defer 中减少计数。

修改 `requestLogMiddleware` 函数（serve.go:250-355），在 `start := time.Now()` 之后，`next.ServeHTTP(rw, r)` 之前添加活跃会话追踪。

在 `rw := &responseWriter{ResponseWriter: w, statusCode: 200}` 之前添加：

```go
			// Track active sessions for /v1/ requests
			var resolvedAPIKey string
			if strings.HasPrefix(r.URL.Path, "/v1/") {
				ak := r.Header.Get("x-api-key")
				if ak == "" {
					auth := r.Header.Get("Authorization")
					if strings.HasPrefix(auth, "Bearer ") {
						ak = strings.TrimPrefix(auth, "Bearer ")
					}
				}
				if ak != "" {
					if account, _ := s.GetAccountByToken(ak); account != nil {
						resolvedAPIKey = account.APIKey
					} else if account, _ := s.GetAccount(ak); account != nil {
						resolvedAPIKey = account.APIKey
					}
				}
				if resolvedAPIKey == "" {
					if a, _ := s.GetDefaultAccount(); a != nil {
						resolvedAPIKey = a.APIKey
					}
				}
				if resolvedAPIKey != "" {
					counter, _ := activeSessions.LoadOrStore(resolvedAPIKey, &atomic.Int64{})
					cnt := counter.(*atomic.Int64)
					cnt.Add(1)
					defer cnt.Add(-1)
				}
			}
```

同时添加一个公开函数用于查询活跃会话数，在 serve.go 文件末尾（`responseWriter.Flush()` 方法之后）添加：

```go

// GetActiveSessions returns the number of currently active proxy requests for the given api_key.
func GetActiveSessions(apiKey string) int64 {
	if v, ok := activeSessions.Load(apiKey); ok {
		return v.(*atomic.Int64).Load()
	}
	return 0
}
```

- [ ] **Step 3: 在 AccountInfo 结构体中添加 ActiveSessions 字段**

修改 `pkg/store/store.go:37-44` 的 AccountInfo 结构体：

```go
type AccountInfo struct {
	APIKey        string `json:"api_key"`
	APIToken      string `json:"api_token"`
	UserID        string `json:"user_id"`
	IsDefault     bool   `json:"is_default"`
	DefaultModel  string `json:"default_model"`
	CreatedAt     string `json:"created_at,omitempty"`
	ActiveSessions int64 `json:"active_sessions"`
}
```

- [ ] **Step 4: 在 dashboard handler 的账号列表接口中注入活跃会话数**

在 `pkg/dashboard/handler.go` 中找到 `handleListAccounts` 函数。该函数调用 `s.ListAccounts()` 获取列表后返回 JSON。需要在返回前遍历结果，为每个账号注入 `ActiveSessions`。

在 `accounts, err := h.store.ListAccounts()` 之后、写入 JSON 响应之前，添加遍历逻辑：

在 handler.go 的 import 块中添加 `"github.com/vibe-coding-labs/JoyCodeProxy/cmd/JoyCodeProxy"` — 但这会产生循环依赖（main 包不能被导入）。

**替代方案：** 将 `GetActiveSessions` 放在一个独立的 package 中。创建 `pkg/proxy/sessions.go`：

```go
package proxy

import (
	"sync"
	"sync/atomic"
)

var activeSessions sync.Map // map[string]*atomic.Int64

// TrackActive increments the active session counter for the given api_key.
// Returns a function to decrement it (call on request completion).
func TrackActive(apiKey string) func() {
	if apiKey == "" {
		return func() {}
	}
	counter, _ := activeSessions.LoadOrStore(apiKey, &atomic.Int64{})
	cnt := counter.(*atomic.Int64)
	cnt.Add(1)
	return func() { cnt.Add(-1) }
}

// GetActiveSessions returns the number of currently active proxy requests for the given api_key.
func GetActiveSessions(apiKey string) int64 {
	if v, ok := activeSessions.Load(apiKey); ok {
		return v.(*atomic.Int64).Load()
	}
	return 0
}
```

然后修改 `cmd/JoyCodeProxy/serve.go` 的 requestLogMiddleware 使用 `proxy.TrackActive` 替代 Step 2 中的内联代码。

在 serve.go 的 import 中添加 `"github.com/vibe-coding-labs/JoyCodeProxy/pkg/proxy"`。

将 Step 2 中的内联 activeSessions 追踪代码替换为：

```go
			// Track active sessions for /v1/ requests
			if strings.HasPrefix(r.URL.Path, "/v1/") {
				ak := r.Header.Get("x-api-key")
				if ak == "" {
					authHeader := r.Header.Get("Authorization")
					if strings.HasPrefix(authHeader, "Bearer ") {
						ak = strings.TrimPrefix(authHeader, "Bearer ")
					}
				}
				var resolvedKey string
				if ak != "" {
					if account, _ := s.GetAccountByToken(ak); account != nil {
						resolvedKey = account.APIKey
					} else if account, _ := s.GetAccount(ak); account != nil {
						resolvedKey = account.APIKey
					}
				}
				if resolvedKey == "" {
					if a, _ := s.GetDefaultAccount(); a != nil {
						resolvedKey = a.APIKey
					}
				}
				if resolvedKey != "" {
					done := proxy.TrackActive(resolvedKey)
					defer done()
				}
			}
```

同时删除 serve.go 中 Step 1 添加的 `activeSessions sync.Map` 全局变量和 `GetActiveSessions` 函数。

- [ ] **Step 5: 在 dashboard handler 注入活跃会话数到账号列表响应**

在 `pkg/dashboard/handler.go` 中找到 `handleListAccounts` 函数，在获取 accounts 列表后、写入响应前，注入 active_sessions：

在 handler.go 的 import 块中添加 `"github.com/vibe-coding-labs/JoyCodeProxy/pkg/proxy"`。

在 `handleListAccounts` 函数中，`json.NewEncoder(w).Encode(...)` 调用之前添加：

```go
			for i := range accounts {
				accounts[i].ActiveSessions = proxy.GetActiveSessions(accounts[i].APIKey)
			}
```

- [ ] **Step 6: 更新前端 Account 类型和 Accounts 页面展示活跃会话**

修改 `web/src/api.ts:1-8` 的 Account 接口：

```typescript
export interface Account {
  api_key: string;
  api_token: string;
  user_id: string;
  is_default: boolean;
  default_model: string;
  created_at?: string;
  active_sessions: number;
}
```

在 `web/src/pages/Accounts.tsx` 的 columns 数组中，在"用户 ID"列之后、"状态"列之前添加"活跃会话"列：

```typescript
    {
      title: '活跃会话',
      dataIndex: 'active_sessions',
      key: 'active_sessions',
      render: (val: number) => val > 0 ? (
        <Tag color="blue">{val} 个活跃</Tag>
      ) : (
        <Typography.Text type="secondary">无</Typography.Text>
      ),
    },
```

- [ ] **Step 7: 构建并部署**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build && cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build -o joycode_proxy_bin ./cmd/JoyCodeProxy`
Expected:
  - Exit code: 0
  - Output does NOT contain: "error" or "undefined"

- [ ] **Step 8: 部署并验证**
Run: `cp /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/joycode_proxy_bin /Users/cc11001100/.joycode-proxy/joycode_proxy_bin && launchctl unload /Users/cc11001100/Library/LaunchAgents/com.joycode.proxy.plist && launchctl load /Users/cc11001100/Library/LaunchAgents/com.joycode.proxy.plist`
Expected:
  - Exit code: 0
  - Service accessible at http://localhost:34891

- [ ] **Step 9: 提交**
Run: `git add pkg/proxy/sessions.go cmd/JoyCodeProxy/serve.go pkg/store/store.go pkg/dashboard/handler.go web/src/api.ts web/src/pages/Accounts.tsx web/src/pages/AccountDetail.tsx && git commit -m "feat: add user_id masking and active session counting per account"`

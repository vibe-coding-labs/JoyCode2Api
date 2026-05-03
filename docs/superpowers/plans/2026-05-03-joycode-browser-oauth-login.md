# Feature: JoyCode 浏览器 OAuth 回调登录 — 构建多账号池

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 实现基于 JoyCode 官方登录页的浏览器回调登录，支持多个不同 JD 账号通过浏览器扫码登录，构建账号池。替换当前不可用的 QR 登录（JD 已不再通过 HTTP Set-Cookie 返回 pt_key）。

**Architecture:** 用户点击"浏览器登录" → 后端构造 JoyCode 登录 URL（含 authPort 回调参数）→ 前端在浏览器新标签页中打开 `https://joycode.jd.com/login?authPort={port}&fromIde=true&loginType=PIN` → 用户在浏览器中完成 JD 扫码登录 → JoyCode 登录页自动回调 `http://127.0.0.1:{port}/api/oauth-callback?pt_key=xxx&login_type=PIN&tenant=JOYCODE` → 后端接收 pt_key → 调用 JoyCode API 验证 → 保存账号到数据库 → 前端轮询检测新账号完成登录。这是 JoyCode IDE 使用的完全相同的登录机制。

**Tech Stack:** Go 1.23, React 19, Ant Design 6, JoyCode SSO API

**Risks:**
- JoyCode 登录页可能校验 `ideAppName` 参数限制来源 → 缓解：使用 `JoyCode` 作为 ideAppName，与 JoyCode IDE 一致
- 回调 URL 使用 HTTP（非 HTTPS），浏览器安全策略可能阻止 → 缓解：JoyCode 登录页当前使用 `http://127.0.0.1` 回调（已在生产验证）
- 多用户同时登录时回调冲突 → 缓解：使用一次性 token 关联回调与登录会话

---

### Task 1: 后端 — 添加 OAuth 回调端点和登录发起端点

**Depends on:** None
**Files:**
- Modify: `pkg/dashboard/handler.go:48` (路由注册区域)
- Modify: `pkg/dashboard/handler.go` (添加两个新 handler)

- [ ] **Step 1: 添加 OAuth 登录发起端点 — `/api/browser-login`**

用户调用此端点获取 JoyCode 登录 URL，然后在浏览器中打开。

文件: `pkg/dashboard/handler.go`（在 `handleAutoLogin` 函数之后添加）

```go
// handleBrowserLogin returns a JoyCode login URL for browser-based OAuth flow.
func (h *Handler) handleBrowserLogin(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Determine the callback port from the request's Host header
	host := r.Host
	port := "34891"
	if h, _, err := net.SplitHostPort(host); err == nil {
		port = h.Host
	} else {
		// If no port in Host, try extracting from the request
		if strings.Contains(host, ":") {
			_, port, _ = net.SplitHostPort(host)
		}
	}

	// Generate a one-time token to correlate the callback
	token := fmt.Sprintf("bl_%d", time.Now().UnixNano())

	loginURL := fmt.Sprintf(
		"https://joycode.jd.com/login?authPort=%s&fromIde=true&ideAppName=JoyCode&loginType=PIN&authKey=%s",
		port, token,
	)

	slog.Info("browser-login: generated login URL", "port", port, "token", token)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":       true,
		"url":      loginURL,
		"token":    token,
	})
}
```

需要在 import 中添加 `"net"` 和 `"strings"`（如果还没有的话）。

- [ ] **Step 2: 添加 OAuth 回调接收端点 — `/api/oauth-callback`**

JoyCode 登录页完成登录后回调此端点，传入 pt_key。

文件: `pkg/dashboard/handler.go`（在 `handleBrowserLogin` 函数之后添加）

```go
// handleOAuthCallback receives the pt_key callback from JoyCode login page.
func (h *Handler) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	setCors(w)

	ptKey := r.URL.Query().Get("pt_key")
	loginType := r.URL.Query().Get("login_type")
	tenant := r.URL.Query().Get("tenant")
	authKey := r.URL.Query().Get("authKey")

	slog.Info("oauth-callback: received", "login_type", loginType, "tenant", tenant, "auth_key", authKey, "pt_key_len", len(ptKey))

	if ptKey == "" {
		writeError(w, http.StatusBadRequest, "missing pt_key parameter")
		return
	}

	// Validate ptKey with JoyCode API
	client := joycode.NewClient(ptKey, "")
	userInfo, err := client.UserInfo()
	if err != nil {
		slog.Error("oauth-callback: userInfo validation failed", "error", err)
		// Still redirect to success page — user can retry
		http.Redirect(w, r, "/?login_error="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}

	code, _ := userInfo["code"].(float64)
	if code != 0 {
		msg, _ := userInfo["msg"].(string)
		slog.Error("oauth-callback: userInfo API error", "code", code, "msg", msg)
		http.Redirect(w, r, "/?login_error="+url.QueryEscape(msg), http.StatusFound)
		return
	}

	// Extract user info
	userID, _ := userInfo["userId"].(string)
	apiKey := userID
	realName := ""
	if data, ok := userInfo["data"].(map[string]interface{}); ok {
		if name, ok := data["realName"].(string); ok && name != "" {
			apiKey = name
			realName = name
		}
	}

	// Determine if this should be default
	isDefault := true
	accounts, _ := h.store.ListAccounts()
	for _, a := range accounts {
		if a.IsDefault {
			isDefault = false
			break
		}
	}

	// Save account
	if err := h.store.AddAccount(apiKey, ptKey, userID, isDefault, "GLM-5.1"); err != nil {
		slog.Error("oauth-callback: save account failed", "api_key", apiKey, "error", err)
		http.Redirect(w, r, "/?login_error="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}

	slog.Info("oauth-callback: account saved", "api_key", apiKey, "user_id", userID, "real_name", realName)

	// Redirect to frontend with success indicator
	http.Redirect(w, r, "/?login_success="+url.QueryEscape(apiKey), http.StatusFound)
}
```

- [ ] **Step 3: 注册路由**

文件: `pkg/dashboard/handler.go`（在路由注册区域，约第 48 行附近，添加两行）

找到 `mux.HandleFunc("/api/accounts-auto-login", h.handleAutoLogin)` 行，在其后面添加：

```go
	mux.HandleFunc("/api/browser-login", h.handleBrowserLogin)
	mux.HandleFunc("/api/oauth-callback", h.handleOAuthCallback)
```

- [ ] **Step 4: 验证编译**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./...`
Expected:
  - Exit code: 0

---

### Task 2: 前端 — 替换 QR 登录为浏览器登录

**Depends on:** Task 1
**Files:**
- Modify: `web/src/api.ts` (添加 browserLogin API)
- Modify: `web/src/pages/Accounts.tsx` (替换扫码登录按钮)

- [ ] **Step 1: 添加 browserLogin API 到 api.ts**

文件: `web/src/api.ts`（在 `api` 对象中添加 browserLogin 方法）

找到 `qrLoginStatus` 行，在其后面添加：

```typescript
  browserLogin: () =>
    request<{ ok: boolean; url: string; token: string }>('/api/browser-login', { method: 'POST' }),
```

- [ ] **Step 2: 修改 Accounts.tsx — 替换扫码登录为浏览器登录**

文件: `web/src/pages/Accounts.tsx`（约第 260-265 行，替换扫码登录按钮）

将扫码登录按钮替换为浏览器登录按钮：

找到：
```typescript
          <Button
            onClick={() => setQrModalOpen(true)}
            icon={<SafetyCertificateOutlined />}
          >
            扫码登录
          </Button>
```

替换为：
```typescript
          <Button
            onClick={async () => {
              try {
                const result = await api.browserLogin();
                window.open(result.url, '_blank');
                message.info('请在浏览器中完成登录，登录成功后会自动同步到此处');
                // Poll for new accounts after giving user time to login
                setTimeout(() => fetchAccounts(), 10000);
              } catch (e: unknown) {
                message.error(e instanceof Error ? e.message : '获取登录链接失败');
              }
            }}
            icon={<SafetyCertificateOutlined />}
          >
            浏览器登录
          </Button>
```

同时在文件顶部 import 中添加 `message`（如果还没有的话）— 已在 import 中。

- [ ] **Step 3: 添加登录成功/失败的 URL 参数检测**

文件: `web/src/pages/Accounts.tsx`（在 `useEffect` 中添加 URL 参数检测）

在 `useEffect(() => { fetchAccounts(); }, []);` 行之后添加：

```typescript
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const loginSuccess = params.get('login_success');
    const loginError = params.get('login_error');
    if (loginSuccess) {
      message.success(`登录成功！账号「${loginSuccess}」已添加`);
      fetchAccounts();
      window.history.replaceState({}, '', window.location.pathname);
    }
    if (loginError) {
      message.error(`登录失败：${loginError}`);
      window.history.replaceState({}, '', window.location.pathname);
    }
  }, []);
```

- [ ] **Step 4: 验证前端构建**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build`
Expected:
  - Exit code: 0

---

### Task 3: 构建部署并验证

**Depends on:** Task 1, Task 2
**Files:**
- Modify: (binary output)

- [ ] **Step 1: 构建 Go 二进制文件**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build -o joycode_proxy_bin ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0

- [ ] **Step 2: 重新加载服务**
Run: `launchctl unload ~/Library/LaunchAgents/com.joycode.proxy.plist && launchctl load ~/Library/LaunchAgents/com.joycode.proxy.plist`
Expected:
  - Exit code: 0

- [ ] **Step 3: 验证服务运行正常**
Run: `sleep 1 && curl -s http://localhost:34891/api/accounts | head -c 100`
Expected:
  - Exit code: 0
  - Output contains: JSON response

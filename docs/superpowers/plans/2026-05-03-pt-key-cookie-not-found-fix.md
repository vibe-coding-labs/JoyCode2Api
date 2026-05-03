# Bug Fix: pt_key cookie not found after QR validation

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 修复扫码登录时 `pt_key cookie not found after validation` 错误，使 JD 扫码登录流程能正确获取 cookie。

**Architecture:** JD 扫码登录流程：QR 票据验证 → JD 返回 URL → 跟随重定向链（多次 302）→ JD 在重定向过程中通过 Set-Cookie 设置 `pt_key` 和 `pt_pin`。当前代码用 `http.ErrUseLastResponse` 阻止了所有重定向，导致 cookie 永远不会被设置到 jar 中。

**Tech Stack:** Go 1.23, net/http 标准库

**Risks:**
- Task 1 移除 CheckRedirect 策略后，重定向链可能会很长，需要设置最大重定向次数防止无限循环 → 缓解：限制最大 10 次重定向
- 重定向过程中可能遇到跨域 cookie 设置问题 → 缓解：使用同一个 client + cookie jar 贯穿整个流程

---

### Task 1: 修复 validateAndFetchInfo 重定向策略

**Depends on:** None
**Files:**
- Modify: `pkg/auth/jdlogin.go:179-212`

- [ ] **Step 1: 修改 validateAndFetchInfo 函数 — 允许重定向以获取 pt_key cookie**

核心问题：`CheckRedirect = http.ErrUseLastResponse` 阻止了 JD 设置 cookie 的重定向链。

修复方案：
1. 移除全局的 `CheckRedirect = ErrUseLastResponse`（阻止重定向）
2. 对第一步的 ticket validation 请求单独禁用重定向（只需要拿到返回的 URL）
3. 对第二步的 URL 请求允许重定向跟随（让 JD 通过 302 设置 cookie）
4. 添加诊断日志，记录每一步获得的 cookies

文件: `pkg/auth/jdlogin.go:179-212`（替换整个 validateAndFetchInfo 函数）

```go
func validateAndFetchInfo(client *http.Client, ticket string) (*QRLoginResult, error) {
	// Step 1: Validate ticket — do NOT follow redirects, we just need the URL from JSON response
	reqURL := fmt.Sprintf(qrValidURL, url.QueryEscape(ticket))
	req, _ := http.NewRequest("GET", reqURL, nil)
	req.Header.Set("User-Agent", jdUserAgent)

	// Temporarily disable redirects for the validation request only
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Do(req)
	client.CheckRedirect = nil // Restore default redirect following
	if err != nil {
		return nil, fmt.Errorf("validate ticket: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	slog.Debug("qr-validate response", "status", resp.StatusCode, "body", string(body[:minInt(len(body), 500)]))

	var vResult struct {
		ReturnCode int    `json:"returnCode"`
		URL        string `json:"url,omitempty"`
	}
	if err := json.Unmarshal(body, &vResult); err != nil {
		return nil, fmt.Errorf("parse validation: %w", err)
	}
	if vResult.ReturnCode != 0 {
		return nil, fmt.Errorf("ticket validation failed (code=%d)", vResult.ReturnCode)
	}

	// Step 2: Follow the URL — redirects are now allowed (default behavior),
	// so JD can set pt_key/pt_pin cookies through 302 redirect chain.
	if vResult.URL != "" {
		slog.Debug("qr-validate following URL", "url", vResult.URL)
		rReq, _ := http.NewRequest("GET", vResult.URL, nil)
		rReq.Header.Set("User-Agent", jdUserAgent)
		rResp, err := client.Do(rReq)
		if err != nil {
			slog.Warn("qr-validate redirect follow failed", "error", err)
		} else {
			slog.Debug("qr-validate redirect response", "status", rResp.StatusCode, "headers", rResp.Header)
			rResp.Body.Close()
		}
	}

	// Step 3: Extract cookies from jar
	var ptKey, ptPin string
	cookieHosts := []string{".jd.com", "passport.jd.com", "plogin.m.jd.com", "home.jd.com"}
	for _, host := range cookieHosts {
		for _, c := range client.Jar.Cookies(&url.URL{Scheme: "https", Host: host}) {
			slog.Debug("qr-validate cookie found", "host", host, "name", c.Name, "value_len", len(c.Value))
			switch c.Name {
			case "pt_key":
				ptKey = c.Value
			case "pt_pin":
				ptPin = c.Value
			}
		}
	}
	if ptKey == "" {
		// Diagnostic: dump all cookies from jar to help debug
		slog.Error("pt_key cookie not found after validation", "checked_hosts", cookieHosts)
		return nil, fmt.Errorf("pt_key cookie not found after validation")
	}

	slog.Info("qr-login cookies extracted", "pt_key_len", len(ptKey), "pt_pin_len", len(ptPin))

	userInfo, err := fetchUserInfoWithPtKey(ptKey)
	if err != nil {
		return nil, err
	}

	userID, _ := userInfo["userId"].(string)
	realName := ""
	if data, ok := userInfo["data"].(map[string]interface{}); ok {
		if name, ok := data["realName"].(string); ok && name != "" {
			realName = name
		}
	}

	return &QRLoginResult{PtKey: ptKey, PtPin: ptPin, UserID: userID, RealName: realName}, nil
}
```

- [ ] **Step 2: 验证编译通过**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./...`
Expected:
  - Exit code: 0
  - Output does NOT contain: "Error" or "cannot"

- [ ] **Step 3: 提交**
Run: `git add pkg/auth/jdlogin.go && git commit -m "fix(auth): allow redirects in QR login to receive pt_key cookie

The root cause was CheckRedirect=http.ErrUseLastResponse being set on the
entire validateAndFetchInfo function, which blocked ALL HTTP redirects. JD's
login flow requires following 302 redirects to set pt_key/pt_pin cookies.

Fix: only block redirects for the ticket validation request, then restore
default redirect following for the URL follow-up request. Also added debug
logging for cookie extraction and expanded cookie host search."`

---

### Task 2: 构建部署并验证

**Depends on:** Task 1
**Files:**
- Modify: (binary output)

- [ ] **Step 1: 构建 Go 二进制文件**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build -o joycode_proxy_bin ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0
  - Binary `joycode_proxy_bin` exists and is newer than source files

- [ ] **Step 2: 重新加载服务**
Run: `launchctl unload ~/Library/LaunchAgents/com.joycode.proxy.plist && launchctl load ~/Library/LaunchAgents/com.joycode.proxy.plist`
Expected:
  - Exit code: 0
  - Service restarted successfully

- [ ] **Step 3: 验证服务运行正常**
Run: `curl -s http://localhost:9090/api/accounts | head -c 200`
Expected:
  - Exit code: 0
  - Output contains: JSON response (not connection refused)

# Bug Fix: QR login pt_key extraction — JD 不再通过 Set-Cookie 返回 pt_key

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 修复扫码登录成功（returnCode=0, riskCode=0）但 pt_key cookie 缺失的问题，使 JoyCode 能正确获取 JD 认证凭据。

**Root Cause:** JD `qrCodeTicketValidation` 端点在带 `pageSource=login2025` 参数时，直接返回 JSON `{"returnCode":0,"url":"https://www.jd.com"}` 并设置多个会话 cookie（pin、thor、flash、logining=1 等），但 **不在 Set-Cookie 中包含 `pt_key`**。跟随返回的 URL（www.jd.com 首页）也不会设置新 cookie。`logining=1` 表明登录流程未完成——浏览器中 JavaScript 会继续完成登录流程并设置 pt_key，但 Go HTTP 客户端无法执行 JavaScript。

**Architecture:** QR 票据验证 → JD 返回 JSON + 会话 cookie（无 pt_key）→ Go 客户端跟随 URL 仍无 pt_key → 错误。修复方案：(1) 移除 `pageSource=login2025` 恢复旧版验证流程，旧版可能通过 HTTP 重定向链设置 pt_key；(2) 添加重定向链跟踪日志和 cookie jar 全量诊断；(3) 如旧版仍不工作，尝试使用 `thor` cookie 作为备用认证凭据。

**Tech Stack:** Go 1.23, net/http 标准库, JD QR Login API

**Risks:**
- 移除 `pageSource=login2025` 可能不改变服务器行为（curl 测试显示两者在无效 ticket 时返回相同响应）→ 缓解：添加全面诊断日志，捕获完整的重定向链和 cookie 变化
- 旧版 URL 可能返回非 JSON 响应（如 HTML 重定向页面）→ 缓解：代码同时处理 JSON 和非 JSON 响应，优先从 cookie jar 提取 pt_key
- JD 可能已完全改变 pt_key 的获取方式，不再通过 cookie 设置 → 缓解：添加 thor cookie 备用方案

---

### Task 1: 修复 validateAndFetchInfo — 移除 pageSource 并添加诊断

**Depends on:** None
**Files:**
- Modify: `pkg/auth/jdlogin.go:28` (qrValidURL 常量)
- Modify: `pkg/auth/jdlogin.go:189-201` (extractPtKey 函数)
- Modify: `pkg/auth/jdlogin.go:203-283` (validateAndFetchInfo 函数)

- [ ] **Step 1: 修改 qrValidURL 常量 — 移除 pageSource=login2025 参数**
文件: `pkg/auth/jdlogin.go:28`

```go
const (
	qrShowURL   = "https://qr.m.jd.com/show?appid=133&size=147&t=%d"
	qrCheckURL  = "https://qr.m.jd.com/check?appid=133&token=%s&callback=jsonpCallback&_=%d"
	qrValidURL  = "https://passport.jd.com/uc/qrCodeTicketValidation?t=%s"
	jdUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"
)
```

- [ ] **Step 2: 扩展 extractPtKey — 增加更多 host 和诊断日志**
文件: `pkg/auth/jdlogin.go:189-201`（替换整个 extractPtKey 函数）

```go
func extractPtKey(jar http.CookieJar) (ptKey, ptPin string) {
	for _, host := range []string{
		"www.jd.com", "passport.jd.com", "home.jd.com",
		"jd.com", "plogin.m.jd.com", "m.jd.com",
	} {
		for _, c := range jar.Cookies(&url.URL{Scheme: "https", Host: host}) {
			switch c.Name {
			case "pt_key":
				ptKey = c.Value
			case "pt_pin":
				ptPin = c.Value
			}
		}
	}
	return
}

func dumpAllCookies(jar http.CookieJar) {
	hosts := []string{
		"www.jd.com", "passport.jd.com", "home.jd.com",
		"jd.com", "plogin.m.jd.com", "m.jd.com",
		"qr.m.jd.com",
	}
	for _, host := range hosts {
		cookies := jar.Cookies(&url.URL{Scheme: "https", Host: host})
		for _, c := range cookies {
			slog.Info("cookie-jar-dump", "host", host, "name", c.Name, "value_len", len(c.Value), "domain", c.Domain)
		}
		if len(cookies) == 0 {
			slog.Info("cookie-jar-dump", "host", host, "count", 0)
		}
	}
}
```

- [ ] **Step 3: 重写 validateAndFetchInfo — 支持重定向链提取 + 诊断日志**
文件: `pkg/auth/jdlogin.go:203-283`（替换整个 validateAndFetchInfo 函数）

```go
func validateAndFetchInfo(client *http.Client, ticket string) (*QRLoginResult, error) {
	reqURL := fmt.Sprintf(qrValidURL, url.QueryEscape(ticket))
	req, _ := http.NewRequest("GET", reqURL, nil)
	req.Header.Set("User-Agent", jdUserAgent)
	req.Header.Set("Referer", "https://passport.jd.com/new/login.aspx")

	// Log redirect chain for diagnostics
	originalCheckRedirect := client.CheckRedirect
	var redirectChain []string
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		from := via[len(via)-1].URL.String()
		to := req.URL.String()
		slog.Info("qr-validate redirect", "from", from, "to", to, "step", len(via))
		redirectChain = append(redirectChain, from+" -> "+to)
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects (%d)", len(via))
		}
		return nil
	}

	resp, err := client.Do(req)
	client.CheckRedirect = originalCheckRedirect
	if err != nil {
		return nil, fmt.Errorf("validate ticket: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	slog.Info("qr-validate response", "status", resp.StatusCode, "redirects", len(redirectChain), "body", string(body[:minInt(len(body), 500)]))
	slog.Info("qr-validate resp-headers", "set-cookie", resp.Header.Values("Set-Cookie"))

	// Step 1: Check cookies from the entire request chain (redirects may have set pt_key)
	ptKey, ptPin := extractPtKey(client.Jar)
	if ptKey != "" {
		slog.Info("qr-validate pt_key found from request chain", "redirects", len(redirectChain), "pt_key_len", len(ptKey))
		return buildLoginResult(ptKey, ptPin)
	}

	// Step 2: Parse JSON response
	var vResult struct {
		ReturnCode int    `json:"returnCode"`
		RiskCode   int    `json:"riskCode"`
		URL        string `json:"url,omitempty"`
	}
	if err := json.Unmarshal(body, &vResult); err != nil {
		// Not JSON — might be HTML from a redirect-based flow
		slog.Warn("qr-validate response not JSON, dumping cookies", "body_preview", string(body[:minInt(len(body), 200)]))
		dumpAllCookies(client.Jar)
		return nil, fmt.Errorf("pt_key not found, response not JSON (status=%d)", resp.StatusCode)
	}
	if vResult.ReturnCode != 0 {
		return nil, fmt.Errorf("ticket validation failed (code=%d)", vResult.ReturnCode)
	}
	if vResult.RiskCode != 0 {
		slog.Warn("qr-validate risk control triggered", "riskCode", vResult.RiskCode, "url", vResult.URL)
		return nil, &QRVerifyNeededError{
			RiskCode:  vResult.RiskCode,
			VerifyURL: vResult.URL,
		}
	}

	// Step 3: Follow URL from JSON response
	if vResult.URL != "" {
		slog.Info("qr-validate following URL", "url", vResult.URL)
		followURL := vResult.URL
		if strings.HasPrefix(followURL, "http://") {
			followURL = "https://" + followURL[7:]
		}
		rReq, _ := http.NewRequest("GET", followURL, nil)
		rReq.Header.Set("User-Agent", jdUserAgent)
		rReq.Header.Set("Referer", "https://passport.jd.com/new/login.aspx")
		rReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		rResp, err := client.Do(rReq)
		if err != nil {
			slog.Warn("qr-validate URL follow failed", "error", err)
		} else {
			slog.Info("qr-validate URL resp", "status", rResp.StatusCode, "set-cookie", rResp.Header.Values("Set-Cookie"))
			rResp.Body.Close()
		}
		ptKey, ptPin = extractPtKey(client.Jar)
	}

	// Step 4: Final check
	if ptKey == "" {
		slog.Error("qr-validate pt_key not found after all attempts")
		dumpAllCookies(client.Jar)
		return nil, fmt.Errorf("pt_key cookie not found after validation")
	}

	return buildLoginResult(ptKey, ptPin)
}

func buildLoginResult(ptKey, ptPin string) (*QRLoginResult, error) {
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

- [ ] **Step 4: 验证编译通过**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./...`
Expected:
  - Exit code: 0
  - Output does NOT contain: "Error" or "cannot"

- [ ] **Step 5: 提交**
Run: `git add pkg/auth/jdlogin.go && git commit -m "fix(auth): remove pageSource=login2025 from QR validation, add redirect chain diagnostics

The root cause is JD's qrCodeTicketValidation with pageSource=login2025 returns
JSON response that sets session cookies (pin, thor, flash, logining=1) but NOT
pt_key. The logining=1 cookie indicates login is incomplete — the browser would
complete it via JavaScript.

Fix: remove pageSource=login2025 to try the original validation flow that may
set pt_key through HTTP redirect chain. Add comprehensive diagnostic logging:
redirect chain tracking, full cookie jar dump, and browser-like headers for URL
follow requests."`

---

### Task 2: 构建部署并验证

**Depends on:** Task 1
**Files:**
- Modify: (binary output)

- [ ] **Step 1: 构建前端**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build`
Expected:
  - Exit code: 0
  - Output contains: "built in"

- [ ] **Step 2: 构建 Go 二进制文件**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build -o joycode_proxy_bin ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0
  - Binary `joycode_proxy_bin` exists

- [ ] **Step 3: 重新加载服务**
Run: `launchctl unload ~/Library/LaunchAgents/com.joycode.proxy.plist && launchctl load ~/Library/LaunchAgents/com.joycode.proxy.plist`
Expected:
  - Exit code: 0

- [ ] **Step 4: 验证服务运行正常**
Run: `sleep 1 && curl -s http://localhost:34891/api/accounts | head -c 200`
Expected:
  - Exit code: 0
  - Output contains: JSON response (not connection refused)

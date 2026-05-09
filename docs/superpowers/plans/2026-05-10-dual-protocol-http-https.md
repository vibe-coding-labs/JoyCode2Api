# HTTP/HTTPS 双协议兼容实现计划

> **对于智能体工作者：** 必需子技能：`superpowers:subagent-driven-development`
> 步骤使用复选框（`- [ ]`）语法。

**目标：** HTTPS 主服务处理浏览器访问（自动重定向 HTTP→HTTPS），同时 API 请求（Claude Code / Codex）无论用 HTTP 还是 HTTPS 都能正常响应，剪贴板复制在 HTTPS 页面正常工作。

**架构：** 启动两个 HTTP server：(1) 主 HTTPS server 在 port 34891 处理所有请求；(2) 辅助 HTTP redirect server 在 port+1（34892）接收 HTTP 请求，对浏览器页面（非 /v1/ 路径）返回 301 重定向到 HTTPS，对 API 路径（/v1/、/health）直接透传到主 handler。前端 `getBaseURL()` 始终生成 `http://` URL 用于 CLI 命令（Claude Code 不需要 HTTPS），浏览器页面本身通过 HTTPS 访问确保 `navigator.clipboard` 可用。

**技术栈：** Go 1.25 标准库 `net/http`，React + Ant Design 前端

**风险：**
- HTTP redirect server 端口（port+1）可能被占用 → 缓解：绑定失败时仅 log warning，不影响主 HTTPS 服务
- Claude Code 通过 HTTP 发送 API 请求时需要走 redirect server 的透传 → 缓解：/v1/ 和 /health 路径不重定向，直接透传

---

### 任务 1: 添加 HTTP→HTTPS 重定向服务器 + API 透传

**依赖于:** 无
**文件:**
- 修改: `cmd/JoyCodeProxy/serve.go:190-258`（server 启动区块）

- [ ] **步骤 1: 添加 HTTP redirect server — 浏览器请求重定向到 HTTPS，API 请求直接透传**
文件: `cmd/JoyCodeProxy/serve.go:207-241`（替换整个 goroutine server 启动区块）

```go
			go func() {
				log.Printf("JoyCode Proxy running on %s://%s", scheme, addr)
				fmt.Println()
				fmt.Printf("  JoyCode Proxy %s\n", Version)
				fmt.Println("  ─────────────────────────────────────────────────")
				fmt.Println()
				fmt.Println("  Endpoints:")
				fmt.Println("    POST /v1/chat/completions  — Chat (OpenAI format)")
				fmt.Println("    POST /v1/messages          — Chat (Anthropic/Claude Code format)")
				fmt.Println("    POST /v1/web-search        — Web Search")
				fmt.Println("    POST /v1/rerank            — Rerank documents")
				fmt.Println("    GET  /v1/models            — Model list")
				fmt.Println("    GET  /health               — Health check")
				fmt.Println()
				fmt.Println("  Dashboard:")
				fmt.Printf("    %s://%s — Web UI\n", scheme, addr)
				fmt.Println()
				fmt.Println("  Claude Code setup:")
				fmt.Printf("    export ANTHROPIC_BASE_URL=http://%s\n", addr)
				fmt.Println("    export ANTHROPIC_API_KEY=joycode")
				if verbose {
					fmt.Println()
					fmt.Println("  Verbose logging: enabled")
				}
				fmt.Println()
				var listenErr error
				if httpSrv.TLSConfig != nil {
					listenErr = httpSrv.ListenAndServeTLS("", "")

					// Start HTTP redirect server on port+1
					httpAddr := fmt.Sprintf("%s:%d", serveHost, servePort+1)
					redirectHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						// API and health endpoints: pass through directly (no redirect)
						if strings.HasPrefix(r.URL.Path, "/v1/") || r.URL.Path == "/health" {
							handler.ServeHTTP(w, r)
							return
						}
						// Browser requests: redirect to HTTPS
						target := fmt.Sprintf("https://%s%s", r.Host, r.URL.RequestURI())
						http.Redirect(w, r, target, http.StatusMovedPermanently)
					})
					httpRedirectSrv := &http.Server{
						Addr:    httpAddr,
						Handler: redirectHandler,
					}
					go func() {
						log.Printf("HTTP redirect server on %s (API pass-through, browser → HTTPS)", httpAddr)
						if err := httpRedirectSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
							log.Printf("HTTP redirect server error: %v", err)
						}
					}()
				} else {
					listenErr = httpSrv.ListenAndServe()
				}
				if listenErr != nil && listenErr != http.ErrServerClosed {
					log.Fatalf("Server error: %v", listenErr)
				}
			}()
```

- [ ] **步骤 2: 验证编译通过**
运行: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && CGO_ENABLED=1 go build -o /dev/null ./cmd/JoyCodeProxy/`
预期:
  - 退出码: 0
  - 输出不包含: "Error" 或 "undefined"

- [ ] **步骤 3: 提交**
运行: `git add cmd/JoyCodeProxy/serve.go && git commit -m "feat(server): add HTTP redirect server for browser, pass-through for API requests"`

---

### 任务 2: 修复前端 CLI 命令 URL 始终使用 HTTP

**依赖于:** 任务 1
**文件:**
- 修改: `web/src/pages/Accounts.tsx:31`（getBaseURL 函数）
- 修改: `web/src/pages/AccountDetail.tsx:73`（getBaseURL 函数）

- [ ] **步骤 1: 修改 Accounts.tsx 的 getBaseURL — CLI 命令始终用 HTTP 连接**
文件: `web/src/pages/Accounts.tsx:31`

```typescript
const getBaseURL = () => `http://${window.location.hostname}:${window.location.port || (window.location.protocol === 'https:' ? '443' : '80')}`;
```

注意：CLI 工具（Claude Code、Codex）不需要 HTTPS，用 HTTP 直连 redirect server 的透传端口更稳定。但因为 redirect server 在 port+1，而浏览器页面在 port 上，所以需要构造正确的 HTTP 端口。实际上 redirect server 的透传在 port+1，但 CLI 命令中的 URL 应该用 HTTP 协议访问主端口（如果用户通过 HTTP 访问）或 port+1（如果用户通过 HTTPS 访问）。

修正方案 — 更简单直接：redirect server 的 API 透传端口是 port+1（如 34892），但用户在浏览器中看到的是 HTTPS 主端口（34891）。CLI 命令应该指向 HTTP 透传端口。但如果直接用 `window.location.hostname` + 主端口 +1，用户可能困惑。

最佳方案：让 redirect server 也在主端口上处理（通过同一个端口），但 Go 的 `net/http` 不支持同端口同时 HTTP/HTTPS。所以保留当前设计，CLI 命令中的 HTTP URL 指向 redirect server 端口（port+1）。

文件: `web/src/pages/Accounts.tsx:31`

```typescript
const getBaseURL = () => {
  const host = window.location.hostname;
  const port = parseInt(window.location.port || '443', 10);
  return `http://${host}:${port + 1}`;
};
```

- [ ] **步骤 2: 修改 AccountDetail.tsx 的 getBaseURL — 同上**
文件: `web/src/pages/AccountDetail.tsx:73`

```typescript
const getBaseURL = () => {
  const host = window.location.hostname;
  const port = parseInt(window.location.port || '443', 10);
  return `http://${host}:${port + 1}`;
};
```

- [ ] **步骤 3: 构建前端**
运行: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build`
预期:
  - 退出码: 0
  - 输出包含: "built in"

- [ ] **步骤 4: 构建完整二进制**
运行: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && CGO_ENABLED=1 go build -o /dev/null ./cmd/JoyCodeProxy/`
预期:
  - 退出码: 0

- [ ] **步骤 5: 提交**
运行: `git add web/src/pages/Accounts.tsx web/src/pages/AccountDetail.tsx cmd/JoyCodeProxy/static/ && git commit -m "fix(frontend): use HTTP redirect port for CLI commands to avoid certificate issues"`

---

### 任务 3: 构建部署并验证

**依赖于:** 任务 2
**文件:** 无新文件

- [ ] **步骤 1: 构建并部署到本地 Mac**
运行: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && CGO_ENABLED=1 go build -o ~/.joycode-proxy/joycode_proxy_bin ./cmd/JoyCodeProxy/ && launchctl unload ~/Library/LaunchAgents/com.joycode.proxy.plist && launchctl load ~/Library/LaunchAgents/com.joycode.proxy.plist`
预期:
  - 退出码: 0

- [ ] **步骤 2: 验证本地 HTTPS + HTTP redirect**
运行: `sleep 1 && curl -sk https://localhost:34891/health && echo "---" && curl -s http://localhost:34892/health`
预期:
  - 两个 curl 都返回 JSON: `"status":"ok"`
  - 退出码: 0

- [ ] **步骤 3: 验证 HTTP 浏览器请求重定向**
运行: `curl -s -o /dev/null -w "%{http_code} %{redirect_url}" http://localhost:34892/accounts`
预期:
  - HTTP 301
  - redirect_url 包含: "https://localhost:34891/accounts"

- [ ] **步骤 4: 部署到 server001**
运行: `rsync -avz --exclude='.git' --exclude='web/node_modules' --exclude='*.db' /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/ local-server-001:~/JoyCodeProxy/ && ssh local-server-001 'export PATH=$PATH:/usr/local/go/bin && cd ~/JoyCodeProxy && GOPROXY=https://goproxy.cn,direct CGO_ENABLED=1 go build -o ~/.joycode-proxy/joycode_proxy_bin ./cmd/JoyCodeProxy/ && systemctl --user restart com.joycode.proxy.service'`
预期:
  - rsync 成功
  - go build 退出码: 0
  - service restart 成功

- [ ] **步骤 5: 验证 server001 远程 HTTPS + HTTP**
运行: `sleep 1 && curl -sk https://192.168.1.81:34891/health && echo "---" && curl -s http://192.168.1.81:34892/health`
预期:
  - 两个 curl 都返回 `"status":"ok"`

- [ ] **步骤 6: 最终提交（如有未提交的 static assets 变更）**
运行: `git status --short`
预期:
  - 无未提交变更（所有变更已在前面提交）

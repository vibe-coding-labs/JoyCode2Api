# Structured Error Logging Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 在后端所有关键路径添加结构化错误日志（使用 Go 标准库 `log/slog`），确保任何错误发生时日志包含足够的上下文（模型、账号、端点、错误详情）用于自动诊断。

**Architecture:** 使用 `log/slog` 结构化日志替代裸 `log.Printf`。三层改进：上游客户端层（joycode/client.go）→ 代理处理层（anthropic + openai handlers）→ 管理层（dashboard + middleware）。每层独立添加日志，不引入新依赖，不改业务逻辑。

**Tech Stack:** Go 1.23, log/slog (标准库), 现有项目结构

**Risks:**
- 日志量增加可能影响性能 → 缓解：slog 默认到 stderr，由 launchd 重定向到文件；不记录请求体完整内容
- 修改多个文件但都是纯添加日志语句 → 低风险，不影响业务逻辑

---

### Task 1: Add structured logging to joycode client — 上游 HTTP 错误可见性

**Depends on:** None
**Files:**
- Modify: `pkg/joycode/client.go:93-150`

`client.go` 是所有上游 API 调用的底层，当前零日志。上游返回错误时（如凭证过期、限流、服务不可用），调用者只拿到一个 error 字符串，无法区分错误类型。添加 slog 后，所有上游 HTTP 错误自动记录到日志文件。

- [ ] **Step 1: 修改 client.go 添加 slog 导入和日志语句**

文件: `pkg/joycode/client.go:1-13`（import 区域）和 `:93-150`（doPost/Post/PostStream 方法）

在 import 中添加 `"log/slog"`，然后修改三个方法：

```go
// 替换 pkg/joycode/client.go:93-104 的 doPost 方法
func (c *Client) doPost(endpoint string, body map[string]interface{}) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		slog.Error("marshal request body", "endpoint", endpoint, "error", err)
		return nil, err
	}
	req, err := http.NewRequest("POST", BaseURL+endpoint, bytes.NewReader(data))
	if err != nil {
		slog.Error("create request", "endpoint", endpoint, "error", err)
		return nil, err
	}
	req.Header = c.headers()
	return c.httpClient.Do(req)
}
```

```go
// 替换 pkg/joycode/client.go:120-137 的 Post 方法
func (c *Client) Post(endpoint string, body map[string]interface{}) (map[string]interface{}, error) {
	resp, err := c.doPost(endpoint, c.prepareBody(body))
	if err != nil {
		slog.Error("upstream request failed", "endpoint", endpoint, "error", err)
		return nil, err
	}
	data, err := decodeBody(resp)
	if err != nil {
		slog.Error("decode upstream response", "endpoint", endpoint, "status", resp.StatusCode, "error", err)
		return nil, err
	}
	if resp.StatusCode != 200 {
		slog.Error("upstream non-200", "endpoint", endpoint, "status", resp.StatusCode, "body", truncate(string(data), 500))
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		slog.Error("unmarshal upstream response", "endpoint", endpoint, "error", err)
		return nil, fmt.Errorf("invalid JSON response: %w", err)
	}
	return result, nil
}
```

```go
// 替换 pkg/joycode/client.go:139-150 的 PostStream 方法
func (c *Client) PostStream(endpoint string, body map[string]interface{}) (*http.Response, error) {
	resp, err := c.doPost(endpoint, c.prepareBody(body))
	if err != nil {
		slog.Error("upstream stream connect", "endpoint", endpoint, "error", err)
		return nil, err
	}
	if resp.StatusCode != 200 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		slog.Error("upstream stream non-200", "endpoint", endpoint, "status", resp.StatusCode, "body", truncate(string(data), 500))
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}
	return resp, nil
}
```

- [ ] **Step 2: 添加 truncate 辅助函数**

在 `pkg/joycode/client.go` 文件末尾添加：

```go
// 文件末尾追加
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
```

- [ ] **Step 3: 验证编译**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./pkg/joycode/`
Expected:
  - Exit code: 0
  - No output

- [ ] **Step 4: 提交**
Run: `git add pkg/joycode/client.go && git commit -m "feat(logging): add structured error logging to joycode upstream client"`

---

### Task 2: Add error logging to proxy handlers — 请求处理层错误可见性

**Depends on:** Task 1
**Files:**
- Modify: `pkg/openai/chat.go:14-70`
- Modify: `pkg/openai/search.go:8-54`
- Modify: `pkg/openai/handler.go:96-104`
- Modify: `pkg/anthropic/handler.go:58-370`

当前 Anthropic handler 有部分日志但缺少请求上下文；OpenAI handlers 完全没有错误日志。

- [ ] **Step 1: 修改 openai/chat.go 添加错误日志**

```go
// 替换 pkg/openai/chat.go 完整文件
package openai

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
)

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Error("decode chat request", "error", err)
		writeError(w, 400, "invalid JSON")
		return
	}
	model := DefaultModel(req.Model)
	jcBody := TranslateRequest(&req)
	client := s.getClient(r)
	if req.Stream {
		s.handleStreamChat(w, client, jcBody, model)
	} else {
		s.handleNonStreamChat(w, client, jcBody, model)
	}
}

func (s *Server) handleNonStreamChat(w http.ResponseWriter, client *joycode.Client, jcBody map[string]interface{}, model string) {
	resp, err := client.Post("/api/saas/openai/v1/chat/completions", jcBody)
	if err != nil {
		slog.Error("chat non-stream upstream error", "model", model, "error", err)
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, TranslateResponse(resp, model))
}

func (s *Server) handleStreamChat(w http.ResponseWriter, client *joycode.Client, jcBody map[string]interface{}, model string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		slog.Error("streaming not supported by response writer")
		writeError(w, 500, "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "close")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(200)

	resp, err := client.PostStream("/api/saas/openai/v1/chat/completions", jcBody)
	if err != nil {
		slog.Error("chat stream upstream error", "model", model, "error", err)
		fmt.Fprintf(w, "data: {\"error\":{\"message\":\"%s\"}}\n\n", err.Error())
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}
	defer resp.Body.Close()

	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			flusher.Flush()
		}
		if readErr != nil {
			if readErr.Error() != "EOF" {
				slog.Error("chat stream read error", "model", model, "error", readErr)
			}
			break
		}
	}
}
```

- [ ] **Step 2: 修改 openai/search.go 添加错误日志**

```go
// 替换 pkg/openai/search.go 完整文件
package openai

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

func (s *Server) handleWebSearch(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	var body struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		slog.Error("decode search request", "error", err)
		writeError(w, 400, "invalid JSON")
		return
	}
	if body.Query == "" {
		writeError(w, 400, "query is required")
		return
	}
	results, err := s.getClient(r).WebSearch(body.Query)
	if err != nil {
		slog.Error("web search upstream error", "query", body.Query, "error", err)
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]interface{}{"search_result": results})
}

func (s *Server) handleRerank(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	var body struct {
		Query     string   `json:"query"`
		Documents []string `json:"documents"`
		TopN      int      `json:"top_n"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		slog.Error("decode rerank request", "error", err)
		writeError(w, 400, "invalid JSON")
		return
	}
	if body.Query == "" || len(body.Documents) == 0 {
		writeError(w, 400, "query and documents are required")
		return
	}
	result, err := s.getClient(r).Rerank(body.Query, body.Documents, body.TopN)
	if err != nil {
		slog.Error("rerank upstream error", "query", body.Query, "error", err)
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, result)
}
```

- [ ] **Step 3: 修改 openai/handler.go 添加错误日志**

文件: `pkg/openai/handler.go:96-104`（handleModels 方法）

```go
// 替换 pkg/openai/handler.go 的 handleModels 方法（Lines 96-104）
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	models, err := s.getClient(r).ListModels()
	if err != nil {
		slog.Error("list models upstream error", "error", err)
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, TranslateModels(models))
}
```

同时在 import 区域添加 `"log/slog"`。

- [ ] **Step 4: 修改 anthropic/handler.go — 添加请求上下文到已有日志**

文件: `pkg/anthropic/handler.go`，修改以下日志调用：

将 `log.Printf` 替换为 `slog`，并在所有错误日志中添加 model 和 endpoint 上下文。

在 import 中添加 `"log/slog"`。

替换 `handleMessages` 中的请求日志（约 Line 59）：
```go
slog.Info("anthropic request", "model", req.Model, "stream", req.Stream, "max_tokens", req.MaxTokens, "messages", len(req.Messages), "tools", len(req.Tools))
```

替换 `handleStream` 中的日志（约 Line 125）：
```go
slog.Debug("stream starting", "model", jcBody["model"], "max_tokens", jcBody["max_tokens"])
```

替换 `connectStreamWithRetry` 中的所有错误日志（约 Lines 322, 334, 346）：
```go
slog.Error("stream connect error", "attempt", attempt, "max", maxRetries, "error", err)
slog.Error("stream read first line", "attempt", attempt, "max", maxRetries, "error", lastErr)
slog.Error("stream upstream error", "attempt", attempt, "max", maxRetries, "body", truncate(dataContent, 200))
```

替换 `connectStreamWithRetry` 成功日志（约 Line 360）：
```go
slog.Debug("stream connected", "attempt", attempt)
```

替换 `handleStream` 完成日志（约 Line 245）：
```go
slog.Info("stream completed", "chunks", chunkCount, "reason", fr, "tools", len(toolCalls))
```

替换 scanner 错误日志（约 Line 284）：
```go
slog.Error("stream scanner error", "error", err)
```

- [ ] **Step 5: 验证编译**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./pkg/...`
Expected:
  - Exit code: 0
  - No output

- [ ] **Step 6: 提交**
Run: `git add pkg/openai/chat.go pkg/openai/search.go pkg/openai/handler.go pkg/anthropic/handler.go && git commit -m "feat(logging): add structured error logging to all proxy handlers"`

---

### Task 3: Add error logging to dashboard + middleware — 管理层错误可见性

**Depends on:** Task 1
**Files:**
- Modify: `pkg/dashboard/handler.go:126-327`
- Modify: `cmd/JoyCodeProxy/serve.go:152-230`

Dashboard handler 大部分数据库错误未记录。Middleware 检测到错误响应时不记录详情。

- [ ] **Step 1: 修改 dashboard/handler.go 添加错误日志**

在 import 中添加 `"log/slog"`。

替换 `listAccounts` 方法（约 Line 126）：
```go
func (h *Handler) listAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.store.ListAccounts()
	if err != nil {
		slog.Error("list accounts", "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if accounts == nil {
		accounts = []store.AccountInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"accounts": accounts})
}
```

替换 `addAccount` 方法的错误分支（约 Line 159）：
```go
		if err := h.store.AddAccount(body.APIKey, body.PtKey, body.UserID, isDefault, body.DefaultModel); err != nil {
		slog.Error("add account", "api_key", body.APIKey, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
```

替换 `removeAccount` 方法（约 Line 210）：
```go
func (h *Handler) removeAccount(w http.ResponseWriter, r *http.Request, apiKey string) {
	if err := h.store.RemoveAccount(apiKey); err != nil {
		slog.Error("remove account", "api_key", apiKey, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}
```

替换 `setDefault` 方法（约 Line 218）：
```go
func (h *Handler) setDefault(w http.ResponseWriter, r *http.Request, apiKey string) {
	if err := h.store.SetDefault(apiKey); err != nil {
		slog.Error("set default account", "api_key", apiKey, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}
```

替换 `getAccountStats` 方法（约 Line 286）：
```go
func (h *Handler) getAccountStats(w http.ResponseWriter, r *http.Request, apiKey string) {
	stats, err := h.store.GetAccountStats(apiKey)
	if err != nil {
		slog.Error("get account stats", "api_key", apiKey, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if stats.ByModel == nil {
		stats.ByModel = []store.ModelCount{}
	}
	if stats.ByEndpoint == nil {
		stats.ByEndpoint = []store.EndpointCount{}
	}
	writeJSON(w, http.StatusOK, stats)
}
```

替换 `getAccountLogs` 方法（约 Line 301）：
```go
func (h *Handler) getAccountLogs(w http.ResponseWriter, r *http.Request, apiKey string) {
	limit := 200
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := fmt.Sscanf(l, "%d", &limit); err == nil && n == 1 && limit > 0 && limit <= 1000 {
			// ok
		} else {
			limit = 200
		}
	}
	logs, err := h.store.GetAccountLogs(apiKey, limit)
	if err != nil {
		slog.Error("get account logs", "api_key", apiKey, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []store.RequestLog{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"logs": logs, "total": len(logs)})
}
```

替换 `renewToken` 方法（约 Line 321）：
```go
func (h *Handler) renewToken(w http.ResponseWriter, r *http.Request, apiKey string) {
	token, err := h.store.RenewToken(apiKey)
	if err != nil {
		slog.Error("renew token", "api_key", apiKey, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "api_token": token})
}
```

替换 `updateModel` 方法（约 Line 247）：
```go
func (h *Handler) updateModel(w http.ResponseWriter, r *http.Request, apiKey string) {
	var body struct {
		DefaultModel string `json:"default_model"`
	}
	if !readJSONBody(w, r, &body) {
		return
	}
	if err := h.store.UpdateAccountModel(apiKey, body.DefaultModel); err != nil {
		slog.Error("update account model", "api_key", apiKey, "model", body.DefaultModel, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "api_key": apiKey, "default_model": body.DefaultModel})
}
```

- [ ] **Step 2: 修改 serve.go middleware — 错误响应自动记录**

在 import 中添加 `"log/slog"`。

替换 `requestLogMiddleware` 函数（约 Line 193-229）：

```go
func requestLogMiddleware(next http.Handler, s *store.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: 200}
		next.ServeHTTP(rw, r)

		path := r.URL.Path
		if strings.HasPrefix(path, "/v1/") {
			apiKey := r.Header.Get("x-api-key")
			if apiKey == "" {
				apiKey = r.Header.Get("Authorization")
				if strings.HasPrefix(apiKey, "Bearer ") {
					apiKey = strings.TrimPrefix(apiKey, "Bearer ")
				}
			}
			if apiKey == "" {
				if a, _ := s.GetDefaultAccount(); a != nil {
					apiKey = a.APIKey
				}
			}

			model := ""
			contentType := r.Header.Get("Content-Type")
			if strings.Contains(contentType, "json") && r.Method == "POST" && r.Body != nil {
				var body map[string]interface{}
				r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
				json.NewDecoder(r.Body).Decode(&body)
				if m, ok := body["model"].(string); ok {
					model = m
				}
			}

			isStream := r.URL.Query().Get("stream") != "" || path == "/v1/messages"
			latency := time.Since(start).Milliseconds()

			// Log error responses with full context
			if rw.statusCode >= 400 {
				slog.Error("proxy error response",
					"status", rw.statusCode,
					"method", r.Method,
					"path", path,
					"model", model,
					"latency_ms", latency,
					"api_key", apiKey,
				)
			}

			go s.LogRequest(apiKey, model, path, isStream, rw.statusCode, latency)
		}
	})
}
```

- [ ] **Step 3: 验证编译**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./...`
Expected:
  - Exit code: 0
  - No output

- [ ] **Step 4: 验证完整构建**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build -o joycode_proxy_bin ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0
  - No output

- [ ] **Step 5: 提交**
Run: `git add pkg/dashboard/handler.go cmd/JoyCodeProxy/serve.go && git commit -m "feat(logging): add structured error logging to dashboard handlers and middleware"`

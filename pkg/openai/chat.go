package openai

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/store"
)

const chatEndpoint = "/api/saas/openai/v1/chat/completions"

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Error("decode chat request", "error", err)
		writeError(w, 400, fmt.Sprintf("请求体解析失败: %s。请检查请求是否完整，或尝试开启新对话减少上下文长度。", err.Error()))
		return
	}
	systemDefault := ""
	if s.store != nil {
		systemDefault = s.store.GetSetting("default_model")
	}
	model := ResolveModel(req.Model, store.GetAccountDefaultModel(r), systemDefault)
	store.SetModel(r, model)
	req.Model = model
	jcBody := TranslateRequest(&req)
	client := s.getClient(r)
	preparedBody := prepareJoyCodeBody(client, model, jcBody)
	store.SetUpstreamRequest(r, formatUpstreamPayload(chatEndpoint, preparedBody))
	if req.Stream {
		s.handleStreamChat(w, r, client, preparedBody, model)
	} else {
		s.handleNonStreamChat(w, r, client, preparedBody, model)
	}
}

func (s *Server) handleNonStreamChat(w http.ResponseWriter, r *http.Request, client *joycode.Client, jcBody map[string]interface{}, model string) {
	resp, err := client.PostPreparedJSON(chatEndpoint, jcBody)
	if err != nil {
		store.SetUpstreamResponse(r, formatUpstreamPayload(chatEndpoint, map[string]interface{}{"error": err.Error()}))
		slog.Error("chat non-stream upstream error", "model", model, "error", err)
		msg := err.Error()
		code := 500
		if isTimeoutError(msg) {
			code = 504
			msg = "上游服务响应超时，请稍后重试。"
		}
		writeError(w, code, msg)
		return
	}
	if usage, ok := resp["usage"].(map[string]interface{}); ok {
		inTk, _ := usage["prompt_tokens"].(float64)
		outTk, _ := usage["completion_tokens"].(float64)
		store.SetTokenUsage(r, int(inTk), int(outTk))
	}
	store.SetUpstreamResponse(r, formatUpstreamPayload(chatEndpoint, resp))
	writeJSON(w, 200, TranslateResponse(resp, model))
}

func (s *Server) handleStreamChat(w http.ResponseWriter, r *http.Request, client *joycode.Client, jcBody map[string]interface{}, model string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		slog.Error("streaming not supported by response writer")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "close")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(200)

	resp, err := client.PostPreparedStream(chatEndpoint, jcBody)
	if err != nil {
		store.SetUpstreamResponse(r, formatUpstreamPayload(chatEndpoint, map[string]interface{}{"error": err.Error()}))
		slog.Error("chat stream upstream error", "model", model, "error", err)
		msg := err.Error()
		if isTimeoutError(msg) {
			msg = "上游服务响应超时，请稍后重试。"
		}
		fmt.Fprintf(w, "data: {\"error\":{\"message\":\"%s\"}}\n\n", msg)
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}
	defer resp.Body.Close()

	// Pipe JoyCode SSE response directly — already OpenAI-compatible format
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

func formatUpstreamPayload(endpoint string, body map[string]interface{}) string {
	payload := map[string]interface{}{
		"endpoint": endpoint,
		"body":     body,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err.Error()
	}
	const maxLen = 200000
	text := string(data)
	if len(text) > maxLen {
		return text[:maxLen] + "\n... truncated ..."
	}
	return text
}

func prepareJoyCodeBody(client interface {
	PrepareBody(map[string]interface{}) map[string]interface{}
}, model string, body map[string]interface{}) map[string]interface{} {
	if joycode.ModelAdapter(model) == "openai-response" {
		return body
	}
	return client.PrepareBody(body)
}

func isTimeoutError(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "context deadline exceeded") ||
		strings.Contains(lower, "client.timeout exceeded") ||
		strings.Contains(lower, "deadline exceeded") ||
		strings.Contains(lower, "i/o timeout")
}

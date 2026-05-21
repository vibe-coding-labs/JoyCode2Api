package anthropic

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/store"
)

type contextKey string

const requestIDKey contextKey = "request_id"

// WithRequestID injects a request ID into the request context.
func WithRequestID(r *http.Request, id uint64) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), requestIDKey, id))
}

// reqID extracts the request ID from context.
func reqID(r *http.Request) uint64 {
	if v, ok := r.Context().Value(requestIDKey).(uint64); ok {
		return v
	}
	return 0
}

// reqLog returns a slog.Logger pre-loaded with the request ID.
func reqLog(r *http.Request) *slog.Logger {
	return slog.With("request_id", reqID(r))
}

// logRequestDetails logs a compact summary of the translated request body.
// Logs message count, tool count, system prompt length, and first few message roles.
func logRequestDetails(r *http.Request, label string, body map[string]interface{}) {
	log := reqLog(r)
	msgCount := 0
	if msgs, ok := body["messages"].([]map[string]interface{}); ok {
		msgCount = len(msgs)
	}
	toolCount := 0
	if tools, ok := body["tools"].([]interface{}); ok {
		toolCount = len(tools)
	}
	maxTokens, _ := body["max_tokens"].(int)
	model, _ := body["model"].(string)
	stream, _ := body["stream"].(bool)

	log.Info(label,
		"model", model,
		"stream", stream,
		"max_tokens", maxTokens,
		"messages", msgCount,
		"tools", toolCount,
	)
}

func recordUpstreamRequest(r *http.Request, endpoint string, body map[string]interface{}) {
	payload := map[string]interface{}{
		"endpoint": endpoint,
		"body":     body,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		store.SetUpstreamRequest(r, err.Error())
		return
	}
	const maxLen = 200000
	text := string(data)
	if len(text) > maxLen {
		text = text[:maxLen] + "\n... truncated ..."
	}
	store.SetUpstreamRequest(r, text)
}

func recordUpstreamResponse(r *http.Request, endpoint string, payload string) {
	data := map[string]interface{}{
		"endpoint": endpoint,
		"body":     payload,
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		store.SetUpstreamResponse(r, truncateLogText(payload))
		return
	}
	store.SetUpstreamResponse(r, truncateLogText(string(b)))
}

func truncateLogText(text string) string {
	const maxLen = 200000
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

// logUpstreamError logs the full upstream error response for diagnosis.
func logUpstreamError(r *http.Request, attempt, maxAttempt int, body string) {
	log := reqLog(r)
	// Try to extract structured error
	var errResp map[string]interface{}
	if json.Unmarshal([]byte(body), &errResp) == nil {
		if errObj, ok := errResp["error"].(map[string]interface{}); ok {
			log.Error("upstream error",
				"attempt", attempt,
				"max", maxAttempt,
				"error_code", errObj["code"],
				"error_message", errObj["message"],
				"error_status", errObj["status"],
			)
			return
		}
	}
	// Fallback: log truncated raw body
	truncated := body
	if len(truncated) > 500 {
		truncated = truncated[:500]
	}
	log.Error("upstream error (raw)",
		"attempt", attempt,
		"max", maxAttempt,
		"body", truncated,
	)
}

package anthropic

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
)

const chatEndpoint = "/api/saas/openai/v1/chat/completions"

// ClientResolver returns the appropriate joycode.Client for a request.
type ClientResolver func(r *http.Request) *joycode.Client

// Handler serves the Anthropic Messages API.
type Handler struct {
	Client   *joycode.Client
	Resolver ClientResolver
}

// NewHandler creates a new Anthropic API handler.
func NewHandler(c *joycode.Client) *Handler {
	return &Handler{Client: c}
}

func (h *Handler) getClient(r *http.Request) *joycode.Client {
	if h.Resolver != nil {
		return h.Resolver(r)
	}
	return h.Client
}

// RegisterRoutes registers the Anthropic Messages API endpoint.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/messages", h.handleMessages)
}

func (h *Handler) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.WriteHeader(200)
		return
	}
	if r.Method != http.MethodPost {
		writeAnthropicError(w, 405, "method not allowed")
		return
	}

	var req MessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Error("decode anthropic request", "error", err)
		writeAnthropicError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = 8192
	}
	if req.MaxTokens > 32768 {
		req.MaxTokens = 32768
	}

	slog.Info("anthropic request", "model", req.Model, "stream", req.Stream, "max_tokens", req.MaxTokens, "messages", len(req.Messages), "tools", len(req.Tools))

	client := h.getClient(r)

	if req.Stream {
		h.handleStream(w, &req, client)
	} else {
		h.handleNonStream(w, &req, client)
	}
}

func (h *Handler) handleNonStream(w http.ResponseWriter, req *MessageRequest, client *joycode.Client) {
	jcBody := TranslateRequest(req)
	const maxRetries = 3
	var jcResp map[string]interface{}
	var lastErr error
	truncated := false

	for attempt := 1; attempt <= maxRetries; attempt++ {
		jcResp, lastErr = client.Post(chatEndpoint, jcBody)
		if lastErr != nil {
			if isContextLimitError(lastErr.Error()) && !truncated {
				if truncateMessages(req) {
					truncated = true
					jcBody = TranslateRequest(req)
					slog.Warn("retrying with truncated messages (non-stream)")
					continue
				}
				slog.Warn("context limit exceeded, cannot truncate further")
				writeAnthropicRequestError(w, "上下文长度超出模型限制，且无法进一步截断。请压缩对话历史或开启新对话。")
				return
			}
			slog.Error("non-stream retry error", "attempt", attempt, "max", maxRetries, "error", lastErr)
			if attempt < maxRetries {
				time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
			}
			continue
		}
		break
	}

	if lastErr != nil {
		if isContextLimitError(lastErr.Error()) {
			writeAnthropicRequestError(w, "上下文长度超出模型限制。请压缩对话历史或开启新对话。原始错误: "+lastErr.Error())
			return
		}
		writeAnthropicError(w, 500, lastErr.Error())
		return
	}
	resp := TranslateResponse(jcResp, req.Model)
	writeAnthropicJSON(w, 200, resp)
}

// prependReader replays a buffered first line before reading from the underlying source.
type prependReader struct {
	first  []byte
	offset int
	source io.Reader
	body   io.ReadCloser
}

func (r *prependReader) Read(p []byte) (int, error) {
	if r.offset < len(r.first) {
		n := copy(p, r.first[r.offset:])
		r.offset += n
		return n, nil
	}
	return r.source.Read(p)
}

func (r *prependReader) Close() error {
	return r.body.Close()
}

func (h *Handler) handleStream(w http.ResponseWriter, req *MessageRequest, client *joycode.Client) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAnthropicError(w, 500, "streaming not supported")
		return
	}

	jcBody := TranslateRequest(req)
	jcBody["stream"] = true
	slog.Debug("stream starting", "model", jcBody["model"], "max_tokens", jcBody["max_tokens"])

	// Connect with retry, auto-truncate on context limit
	resp, err := h.connectStreamWithRetry(jcBody, client)
	if err != nil {
		errMsg := err.Error()
		if isContextLimitError(errMsg) {
			if truncateMessages(req) {
				slog.Warn("retrying stream with truncated messages")
				jcBody = TranslateRequest(req)
				jcBody["stream"] = true
				resp, err = h.connectStreamWithRetry(jcBody, client)
			}
		}
		if err != nil {
			errMsg = err.Error()
			if isContextLimitError(errMsg) {
				slog.Warn("context limit exceeded (stream), cannot proceed")
				writeAnthropicRequestError(w, "上下文长度超出模型限制。请压缩对话历史或开启新对话。原始错误: "+errMsg)
				return
			}
			slog.Error("stream failed after retries", "error", errMsg)
			writeAnthropicError(w, 500, errMsg)
			return
		}
	}
	defer resp.Body.Close()

	// Commit response headers only after upstream confirmed valid
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(200)

	msgID := NewMessageID()
	model := req.Model
	totalOutput := 0

	FormatSSE(w, "message_start", sseMessageStart{
		Type: "message_start",
		Message: MessageResponse{
			ID: msgID, Type: "message", Role: "assistant",
			Model: model, Content: []ContentBlock{}, Usage: Usage{},
		},
	})
	FormatSSE(w, "ping", ssePing{Type: "ping"})
	flusher.Flush()

	type toolCallAccum struct {
		ID        string
		Name      string
		Arguments string
	}
	toolCalls := make(map[int]*toolCallAccum)
	currentBlockIndex := 0
	textBlockStarted := false
	toolBlockStarted := map[int]bool{}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	chunkCount := 0

	for scanner.Scan() {
		line := scanner.Text()
		chunkCount++
		chunk := ParseStreamChunk(line)
		if chunk == nil || len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]

		for _, tc := range choice.Delta.ToolCalls {
			idx := tc.Index
			if _, exists := toolCalls[idx]; !exists {
				toolCalls[idx] = &toolCallAccum{
					ID:   tc.ID,
					Name: tc.Function.Name,
				}
			}
			if tc.ID != "" {
				toolCalls[idx].ID = tc.ID
			}
			if tc.Function.Name != "" {
				toolCalls[idx].Name = tc.Function.Name
			}
			toolCalls[idx].Arguments += tc.Function.Arguments

			if !toolBlockStarted[idx] {
				if textBlockStarted {
					FormatSSE(w, "content_block_stop", sseContentBlockStop{
						Type: "content_block_stop", Index: currentBlockIndex,
					})
					currentBlockIndex++
					textBlockStarted = false
				}
				toolBlockStarted[idx] = true
				tcID := toolCalls[idx].ID
				if tcID == "" {
					tcID = "toolu_" + newID()
				}
				FormatSSE(w, "content_block_start", sseContentBlockStart{
					Type:  "content_block_start",
					Index: currentBlockIndex,
					ContentBlock: ContentBlock{
						Type:  "tool_use",
						ID:    tcID,
						Name:  toolCalls[idx].Name,
						Input: json.RawMessage("{}"),
					},
				})
				flusher.Flush()
			}
		}

		text := choice.Delta.Content
		if text != "" {
			if !textBlockStarted {
				textBlockStarted = true
				FormatSSE(w, "content_block_start", sseContentBlockStart{
					Type:         "content_block_start",
					Index:        currentBlockIndex,
					ContentBlock: ContentBlock{Type: "text", Text: ""},
				})
				flusher.Flush()
			}
			totalOutput += len(text)
			FormatSSE(w, "content_block_delta", sseContentBlockDelta{
				Type:  "content_block_delta",
				Index: currentBlockIndex,
				Delta: deltaText{Type: "text_delta", Text: text},
			})
			flusher.Flush()
		}

		if choice.FinishReason != nil {
			fr := *choice.FinishReason
			slog.Info("stream completed", "chunks", chunkCount, "reason", fr, "tools", len(toolCalls))
			if textBlockStarted {
				FormatSSE(w, "content_block_stop", sseContentBlockStop{
					Type: "content_block_stop", Index: currentBlockIndex,
				})
				currentBlockIndex++
				textBlockStarted = false
			}
			for i := 0; i < len(toolCalls); i++ {
				tc := toolCalls[i]
				if toolBlockStarted[i] {
					FormatSSE(w, "content_block_delta", sseContentBlockDelta{
						Type:  "content_block_delta",
						Index: currentBlockIndex,
						Delta: deltaText{Type: "input_json_delta", PartialJSON: tc.Arguments},
					})
					FormatSSE(w, "content_block_stop", sseContentBlockStop{
						Type: "content_block_stop", Index: currentBlockIndex,
					})
					currentBlockIndex++
				}
			}

			stopReason := "end_turn"
			if fr == "tool_calls" {
				stopReason = "tool_use"
			}
			FormatSSE(w, "message_delta", sseMessageDelta{
				Type:  "message_delta",
				Delta: deltaStop{StopReason: stopReason},
				Usage: struct {
					OutputTokens int `json:"output_tokens"`
				}{OutputTokens: totalOutput / 4},
			})
			FormatSSE(w, "message_stop", sseMessageStop{Type: "message_stop"})
			flusher.Flush()
		}
	}

	if err := scanner.Err(); err != nil {
		slog.Error("stream scanner error", "error", err)
		if textBlockStarted {
			FormatSSE(w, "content_block_stop", sseContentBlockStop{
				Type: "content_block_stop", Index: currentBlockIndex,
			})
			currentBlockIndex++
		}
		for i := 0; i < len(toolCalls); i++ {
			if toolBlockStarted[i] {
				FormatSSE(w, "content_block_stop", sseContentBlockStop{
					Type: "content_block_stop", Index: currentBlockIndex,
				})
				currentBlockIndex++
			}
		}
		FormatSSE(w, "message_delta", sseMessageDelta{
			Type:  "message_delta",
			Delta: deltaStop{StopReason: "end_turn"},
			Usage: struct {
				OutputTokens int `json:"output_tokens"`
			}{OutputTokens: totalOutput / 4},
		})
		FormatSSE(w, "message_stop", sseMessageStop{Type: "message_stop"})
		flusher.Flush()
	}
}

// connectStreamWithRetry attempts to connect to upstream with retries.
// Peeks at the first SSE line to detect errors before returning the response.
func (h *Handler) connectStreamWithRetry(jcBody map[string]interface{}, client *joycode.Client) (*http.Response, error) {
	const maxRetries = 3
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		resp, err := client.PostStream(chatEndpoint, jcBody)
		if err != nil {
			lastErr = err
			slog.Error("stream connect error", "attempt", attempt, "max", maxRetries, "error", err)
			if attempt < maxRetries {
				time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
			}
			continue
		}

		br := bufio.NewReaderSize(resp.Body, 64*1024)
		firstLine, err := br.ReadString('\n')
		if err != nil {
			resp.Body.Close()
			lastErr = fmt.Errorf("read first line: %w", err)
			slog.Error("stream read first line", "attempt", attempt, "max", maxRetries, "error", lastErr)
			if attempt < maxRetries {
				time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
			}
			continue
		}

		trimmed := strings.TrimSpace(firstLine)
		dataContent := strings.TrimPrefix(trimmed, "data: ")
		if isUpstreamError(dataContent) {
			resp.Body.Close()
			lastErr = fmt.Errorf("upstream error: %s", truncate(dataContent, 500))
			slog.Error("stream upstream error", "attempt", attempt, "max", maxRetries, "body", truncate(dataContent, 500))
			if isContextLimitError(dataContent) {
				return nil, lastErr
			}
			if attempt < maxRetries {
				time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
			}
			continue
		}

		// Wrap body to replay first line for the scanner
		originalBody := resp.Body
		resp.Body = &prependReader{
			first:  []byte(firstLine),
			source: br,
			body:   originalBody,
		}
		slog.Debug("stream connected", "attempt", attempt)
		return resp, nil
	}
	return nil, fmt.Errorf("stream failed after %d attempts: %w", maxRetries, lastErr)
}

// isContextLimitError checks if the upstream error indicates context length exceeded.
func isContextLimitError(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "context length") ||
		strings.Contains(lower, "context window") ||
		strings.Contains(lower, "token limit") ||
		strings.Contains(lower, "tokens exceeded") ||
		strings.Contains(lower, "input length") ||
		strings.Contains(lower, "model_context_window_exceeded") ||
		strings.Contains(lower, "prompt length") ||
		strings.Contains(lower, "max_input_tokens")
}

func isUpstreamError(line string) bool {
	if line == "" || line == "[DONE]" {
		return false
	}
	var parsed struct {
		Choices []interface{} `json:"choices"`
		Error   interface{}   `json:"error"`
		Code    interface{}   `json:"code"`
		Status  string        `json:"status"`
		Msg     string        `json:"msg"`
	}
	if err := json.Unmarshal([]byte(line), &parsed); err != nil {
		return false
	}
	if len(parsed.Choices) > 0 {
		return false
	}
	return parsed.Error != nil || parsed.Code != nil || parsed.Status != "" || parsed.Msg != ""
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func writeAnthropicJSON(w http.ResponseWriter, code int, v interface{}) {
	b, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(code)
	w.Write(b)
}

func writeAnthropicError(w http.ResponseWriter, code int, msg string) {
	writeAnthropicJSON(w, code, map[string]interface{}{
		"type":  "error",
		"error": map[string]string{"type": "api_error", "message": msg},
	})
}

func writeAnthropicRequestError(w http.ResponseWriter, msg string) {
	writeAnthropicJSON(w, 400, map[string]interface{}{
		"type":  "error",
		"error": map[string]string{"type": "invalid_request_error", "message": msg},
	})
}

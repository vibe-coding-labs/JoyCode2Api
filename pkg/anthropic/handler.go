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
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/store"
)

const chatEndpoint = "/api/saas/openai/v1/chat/completions"

// ClientResolver returns the appropriate joycode.Client for a request.
type ClientResolver func(r *http.Request) *joycode.Client

// Handler serves the Anthropic Messages API.
type Handler struct {
	Client   *joycode.Client
	Resolver ClientResolver
	store    *store.Store
}

// NewHandler creates a new Anthropic API handler.
func NewHandler(c *joycode.Client, s *store.Store) *Handler {
	return &Handler{Client: c, store: s}
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
		reqLog(r).Error("decode anthropic request", "error", err)
		writeAnthropicError(w, 400, fmt.Sprintf("请求体解析失败: %s。请检查请求是否完整，或尝试开启新对话减少上下文长度。", err.Error()))
		return
	}
	defaultMaxTokens := 8192
	if h.store != nil {
		defaultMaxTokens = h.store.GetIntSetting("default_max_tokens", 8192)
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = defaultMaxTokens
	}
	if req.MaxTokens > 32768 {
		req.MaxTokens = 32768
	}

		accountDefault := store.GetAccountDefaultModel(r)
		systemDefault := ""
		if h.store != nil {
			systemDefault = h.store.GetSetting("default_model")
		}
		resolved := resolveModel(req.Model, accountDefault, systemDefault)
		store.SetModel(r, resolved)
		reqLog(r).Info("anthropic request", "model", req.Model, "resolved", resolved, "stream", req.Stream, "max_tokens", req.MaxTokens, "messages", len(req.Messages), "tools", len(req.Tools))

	client := h.getClient(r)

	if req.Stream {
		h.handleStream(w, r, &req, client)
	} else {
		h.handleNonStream(w, r, &req, client)
	}
}

func (h *Handler) handleNonStream(w http.ResponseWriter, r *http.Request, req *MessageRequest, client *joycode.Client) {
	systemDefault := ""
	if h.store != nil {
		systemDefault = h.store.GetSetting("default_model")
	}
	// Preemptive truncation: estimate tokens and truncate before sending
	if rounds := PreemptiveTruncate(req); rounds < 0 {
		writeAnthropicRequestError(w, "上下文过长，自动截断后仍超出限制，请使用 /compact 或开启新对话。")
		return
	} else if rounds > 0 {
		slog.Warn("preemptive truncation applied (non-stream)", "rounds", rounds)
	}

	jcBody := TranslateRequest(req, store.GetAccountDefaultModel(r), systemDefault)
	logRequestDetails(r, "translated request (non-stream)", jcBody)
	maxRetries := 3
	if h.store != nil {
		maxRetries = h.store.GetIntSetting("max_retries", 3)
	}
	var jcResp map[string]interface{}
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		jcResp, lastErr = client.Post(chatEndpoint, jcBody)
		if lastErr != nil {
			if isContextLimitError(lastErr.Error()) {
				// Progressive truncation on context limit
				if truncateMessages(req) {
					jcBody = TranslateRequest(req, store.GetAccountDefaultModel(r), systemDefault)
					reqLog(r).Warn("retrying with truncated messages (non-stream)", "attempt", attempt)
					continue
				}
				reqLog(r).Warn("context limit exceeded, cannot truncate further")
				writeAnthropicRequestError(w, "上下文长度超出模型限制，且无法进一步截断。请压缩对话历史或开启新对话。")
				return
			}
			reqLog(r).Error("non-stream retry error", "attempt", attempt, "max", maxRetries, "error", lastErr)
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
		if isTimeoutError(lastErr) {
			reqLog(r).Error("upstream timeout after retries", "error", lastErr)
			writeAnthropicError(w, 504, "上游服务响应超时，请稍后重试。如果问题持续，请尝试减少上下文长度或开启新对话。")
			return
		}
		writeAnthropicError(w, 500, lastErr.Error())
		return
	}
	resp := TranslateResponse(jcResp, req.Model)
	if usage, ok := jcResp["usage"].(map[string]interface{}); ok {
		inTk, _ := usage["prompt_tokens"].(float64)
		outTk, _ := usage["completion_tokens"].(float64)
		store.SetTokenUsage(r, int(inTk), int(outTk))
	}
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

func (h *Handler) handleStream(w http.ResponseWriter, r *http.Request, req *MessageRequest, client *joycode.Client) {
	systemDefault := ""
	if h.store != nil {
		systemDefault = h.store.GetSetting("default_model")
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAnthropicError(w, 500, "streaming not supported")
		return
	}

	jcBody := TranslateRequest(req, store.GetAccountDefaultModel(r), systemDefault)
	jcBody["stream"] = true
	logRequestDetails(r, "translated request (stream)", jcBody)

	// Preemptive truncation: estimate tokens and truncate before sending
	if rounds := PreemptiveTruncate(req); rounds < 0 {
		writeAnthropicRequestError(w, "上下文过长，自动截断后仍超出限制，请使用 /compact 或开启新对话。")
		return
	} else if rounds > 0 {
		reqLog(r).Warn("preemptive truncation applied (stream)", "rounds", rounds)
	}

	jcBody = TranslateRequest(req, store.GetAccountDefaultModel(r), systemDefault)
	jcBody["stream"] = true

	// Connect with retry, progressive auto-truncate on context limit
	resp, err := h.connectStreamWithRetry(r, jcBody, client)
	for truncRound := 0; err != nil && isContextLimitError(err.Error()) && truncRound < maxTruncationRounds; truncRound++ {
		reqLog(r).Warn("stream context limit, truncating", "round", truncRound+1)
		if !truncateMessages(req) {
			break
		}
		jcBody = TranslateRequest(req, store.GetAccountDefaultModel(r), systemDefault)
		jcBody["stream"] = true
		resp, err = h.connectStreamWithRetry(r, jcBody, client)
	}
	if err != nil {
		errMsg := err.Error()
		if isContextLimitError(errMsg) {
			reqLog(r).Warn("context limit exceeded (stream), cannot proceed even after progressive truncation")
			writeAnthropicRequestError(w, "上下文长度超出模型限制，已尝试自动截断但仍无法满足。请压缩对话历史或开启新对话。原始错误: "+errMsg)
			return
		}
		if isTimeoutError(err) {
			reqLog(r).Error("upstream timeout (stream) after retries", "error", err)
			writeAnthropicError(w, 504, "上游服务响应超时，请稍后重试。如果问题持续，请尝试减少上下文长度或开启新对话。")
			return
		}
		reqLog(r).Error("stream failed after retries", "error", errMsg)
		writeAnthropicError(w, 500, errMsg)
		return
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
	toolBlockToIdx := map[int]int{}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	chunkCount := 0
	var streamInTk, streamOutTk int

	for scanner.Scan() {
		line := scanner.Text()
		chunkCount++
		chunk := ParseStreamChunk(line)
		if chunk == nil || len(chunk.Choices) == 0 {
			if chunk != nil && chunk.Usage != nil {
				streamInTk = chunk.Usage.PromptTokens
				streamOutTk = chunk.Usage.CompletionTokens
			}
			continue
		}
		choice := chunk.Choices[0]
		if chunk.Usage != nil {
			streamInTk = chunk.Usage.PromptTokens
			streamOutTk = chunk.Usage.CompletionTokens
		}

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
				toolBlockToIdx[idx] = currentBlockIndex
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
				currentBlockIndex++
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
			reqLog(r).Info("stream completed", "chunks", chunkCount, "reason", fr, "tools", len(toolCalls))

			// Ensure at least one content block exists — Anthropic SDK requires it
			if !textBlockStarted && len(toolBlockStarted) == 0 {
				textBlockStarted = true
				FormatSSE(w, "content_block_start", sseContentBlockStart{
					Type:         "content_block_start",
					Index:        currentBlockIndex,
					ContentBlock: ContentBlock{Type: "text", Text: ""},
				})
				flusher.Flush()
			}

			if textBlockStarted {
				FormatSSE(w, "content_block_stop", sseContentBlockStop{
					Type: "content_block_stop", Index: currentBlockIndex,
				})
				currentBlockIndex++
				textBlockStarted = false
			}
			for i := 0; i < len(toolCalls); i++ {
				if toolBlockStarted[i] {
					args := toolCalls[i].Arguments
					if args == "" || !json.Valid([]byte(args)) {
						args = "{}"
					}
					FormatSSE(w, "content_block_delta", sseContentBlockDelta{
						Type:  "content_block_delta",
						Index: toolBlockToIdx[i],
						Delta: deltaText{Type: "input_json_delta", PartialJSON: args},
					})
					FormatSSE(w, "content_block_stop", sseContentBlockStop{
						Type: "content_block_stop", Index: toolBlockToIdx[i],
					})
				}
			}

			stopReason := "end_turn"
			switch fr {
			case "tool_calls":
				stopReason = "tool_use"
			case "length":
				stopReason = "max_tokens"
			case "stop":
				stopReason = "end_turn"
				case "content_filter":
					stopReason = "end_turn"
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
		reqLog(r).Error("stream scanner error", "error", err)

		// Ensure at least one content block exists
		if !textBlockStarted && len(toolBlockStarted) == 0 {
			textBlockStarted = true
			FormatSSE(w, "content_block_start", sseContentBlockStart{
				Type:         "content_block_start",
				Index:        currentBlockIndex,
				ContentBlock: ContentBlock{Type: "text", Text: ""},
			})
		}

		if textBlockStarted {
			FormatSSE(w, "content_block_stop", sseContentBlockStop{
				Type: "content_block_stop", Index: currentBlockIndex,
			})
			currentBlockIndex++
		}
		for i := 0; i < len(toolCalls); i++ {
			if toolBlockStarted[i] {
				args := toolCalls[i].Arguments
				if args == "" {
					args = "{}"
				}
				if !json.Valid([]byte(args)) {
					args = fixPartialJSON(args)
				}
				FormatSSE(w, "content_block_delta", sseContentBlockDelta{
					Type:  "content_block_delta",
					Index: toolBlockToIdx[i],
					Delta: deltaText{Type: "input_json_delta", PartialJSON: args},
				})
				FormatSSE(w, "content_block_stop", sseContentBlockStop{
					Type: "content_block_stop", Index: toolBlockToIdx[i],
				})
			}
		}
		errorStopReason := "end_turn"
		if len(toolBlockStarted) > 0 {
			errorStopReason = "tool_use"
		}
		FormatSSE(w, "message_delta", sseMessageDelta{
			Type:  "message_delta",
			Delta: deltaStop{StopReason: errorStopReason},
			Usage: struct {
				OutputTokens int `json:"output_tokens"`
			}{OutputTokens: totalOutput / 4},
		})
		FormatSSE(w, "message_stop", sseMessageStop{Type: "message_stop"})
		flusher.Flush()
	}
	if streamInTk > 0 || streamOutTk > 0 {
		store.SetTokenUsage(r, streamInTk, streamOutTk)
	}
}

// fixPartialJSON attempts to close unclosed JSON objects/arrays in truncated tool arguments.
func fixPartialJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "{}"
	}
	objDepth := 0
	arrDepth := 0
	inStr := false
	escape := false
	for _, ch := range s {
		if escape {
			escape = false
			continue
		}
		if ch == '\\' && inStr {
			escape = true
			continue
		}
		if ch == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		switch ch {
		case '{':
			objDepth++
		case '}':
			objDepth--
		case '[':
			arrDepth++
		case ']':
			arrDepth--
		}
	}
	if inStr {
		s += "\""
	}
	for arrDepth > 0 {
		s += "]"
		arrDepth--
	}
	for objDepth > 0 {
		s += "}"
		objDepth--
	}
	return s
}

// connectStreamWithRetry attempts to connect to upstream with retries.
// Peeks at the first SSE line to detect errors before returning the response.
func (h *Handler) connectStreamWithRetry(r *http.Request, jcBody map[string]interface{}, client *joycode.Client) (*http.Response, error) {
	maxRetries := 3
	if h.store != nil {
		maxRetries = h.store.GetIntSetting("max_retries", 3)
	}
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		resp, err := client.PostStream(chatEndpoint, jcBody)
		if err != nil {
			lastErr = err
			reqLog(r).Error("stream connect error", "attempt", attempt, "max", maxRetries, "error", err)
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
			reqLog(r).Error("stream read first line", "attempt", attempt, "max", maxRetries, "error", lastErr)
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
			logUpstreamError(r, attempt, maxRetries, dataContent)
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
		reqLog(r).Info("stream connected", "attempt", attempt)
		return resp, nil
	}
	return nil, fmt.Errorf("stream failed after %d attempts: %w", maxRetries, lastErr)
}

// isTimeoutError checks if the error is caused by an upstream timeout.
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "Client.Timeout exceeded") ||
		strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "i/o timeout")
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

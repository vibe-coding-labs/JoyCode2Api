package anthropic

import (
	"bufio"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
)

const chatEndpoint = "/api/saas/openai/v1/chat/completions"

// Handler serves the Anthropic Messages API.
type Handler struct {
	Client *joycode.Client
}

// NewHandler creates a new Anthropic API handler.
func NewHandler(c *joycode.Client) *Handler {
	return &Handler{Client: c}
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
		writeAnthropicError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = 8192
	}

	if req.Stream {
		h.handleStream(w, &req)
	} else {
		h.handleNonStream(w, &req)
	}
}

func (h *Handler) handleNonStream(w http.ResponseWriter, req *MessageRequest) {
	jcBody := TranslateRequest(req)
	jcResp, err := h.Client.Post(chatEndpoint, jcBody)
	if err != nil {
		writeAnthropicError(w, 500, err.Error())
		return
	}
	resp := TranslateResponse(jcResp, req.Model)
	writeAnthropicJSON(w, 200, resp)
}

func (h *Handler) handleStream(w http.ResponseWriter, req *MessageRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAnthropicError(w, 500, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(200)

	jcBody := TranslateRequest(req)
	jcBody["stream"] = true
	resp, err := h.Client.PostStream(chatEndpoint, jcBody)
	if err != nil {
		log.Printf("Stream error: %v", err)
		return
	}
	defer resp.Body.Close()

	msgID := NewMessageID()
	model := req.Model
	totalOutput := 0

	// message_start
	FormatSSE(w, "message_start", sseMessageStart{
		Type: "message_start",
		Message: MessageResponse{
			ID:      msgID,
			Type:    "message",
			Role:    "assistant",
			Model:   model,
			Content: []ContentBlock{},
			Usage:   Usage{},
		},
	})
	FormatSSE(w, "ping", ssePing{Type: "ping"})
	flusher.Flush()

	// content_block_start
	FormatSSE(w, "content_block_start", sseContentBlockStart{
		Type:         "content_block_start",
		Index:        0,
		ContentBlock: ContentBlock{Type: "text", Text: ""},
	})
	flusher.Flush()

	// Stream content deltas from JoyCode SSE
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		text := ParseStreamDelta(line)
		if text == "" {
			continue
		}
		totalOutput += len(text)
		FormatSSE(w, "content_block_delta", sseContentBlockDelta{
			Type:  "content_block_delta",
			Index: 0,
			Delta: deltaText{Type: "text_delta", Text: text},
		})
		flusher.Flush()
	}

	// content_block_stop
	FormatSSE(w, "content_block_stop", sseContentBlockStop{
		Type: "content_block_stop", Index: 0,
	})
	flusher.Flush()

	// message_delta
	FormatSSE(w, "message_delta", sseMessageDelta{
		Type:  "message_delta",
		Delta: deltaStop{StopReason: "end_turn"},
		Usage: struct {
			OutputTokens int `json:"output_tokens"`
		}{OutputTokens: totalOutput / 4},
	})

	// message_stop
	FormatSSE(w, "message_stop", sseMessageStop{Type: "message_stop"})
	flusher.Flush()
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

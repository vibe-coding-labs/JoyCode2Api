package openai

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	model := DefaultModel(req.Model)
	jcBody := TranslateRequest(&req)
	if req.Stream {
		s.handleStreamChat(w, jcBody, model)
	} else {
		s.handleNonStreamChat(w, jcBody, model)
	}
}

func (s *Server) handleNonStreamChat(w http.ResponseWriter, jcBody map[string]interface{}, model string) {
	resp, err := s.Client.Post("/api/saas/openai/v1/chat/completions", jcBody)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, TranslateResponse(resp, model))
}

func (s *Server) handleStreamChat(w http.ResponseWriter, jcBody map[string]interface{}, model string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "close")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(200)

	resp, err := s.Client.PostStream("/api/saas/openai/v1/chat/completions", jcBody)
	if err != nil {
		fmt.Fprintf(w, "data: {\"error\":{\"message\":\"%s\"}}\n\n", err.Error())
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
			break
		}
	}
}

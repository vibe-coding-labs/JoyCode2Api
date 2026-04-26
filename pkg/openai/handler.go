package openai

import (
	"encoding/json"
	"net/http"

	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
)

// Server implements the OpenAI-compatible HTTP API.
type Server struct {
	Client *joycode.Client
}

// NewServer creates a new OpenAI-compatible proxy server.
func NewServer(c *joycode.Client) *Server {
	return &Server{Client: c}
}

// RegisterRoutes registers all OpenAI-compatible endpoints on the mux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/chat/completions", s.handleChat)
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/web-search", s.handleWebSearch)
	mux.HandleFunc("/v1/rerank", s.handleRerank)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/", s.handleHealth)
}

func writeCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	b, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(code)
	w.Write(b)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]interface{}{
		"error": map[string]string{"message": msg, "type": "api_error"},
	})
}

func requirePOST(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodOptions {
		writeCORS(w)
		w.WriteHeader(200)
		return false
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return false
	}
	return true
}

func requireGET(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodOptions {
		writeCORS(w)
		w.WriteHeader(200)
		return false
	}
	return true
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		writeCORS(w)
		w.WriteHeader(200)
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"status": "ok", "service": "joycode-openai-proxy",
		"endpoints": []string{
			"/v1/chat/completions", "/v1/models",
			"/v1/web-search", "/v1/rerank",
		},
	})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	models, err := s.Client.ListModels()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, TranslateModels(models))
}

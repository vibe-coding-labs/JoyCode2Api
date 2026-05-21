package openai

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/store"
)

// ClientResolver returns the appropriate joycode.Client for a request.
type ClientResolver func(r *http.Request) *joycode.Client

// Server implements the OpenAI-compatible HTTP API.
type Server struct {
	Client   *joycode.Client
	Resolver ClientResolver
	store    *store.Store
}

// NewServer creates a new OpenAI-compatible proxy server.
func NewServer(c *joycode.Client, s *store.Store) *Server {
	return &Server{Client: c, store: s}
}

func (s *Server) getClient(r *http.Request) *joycode.Client {
	if s.Resolver != nil {
		return s.Resolver(r)
	}
	return s.Client
}

// RegisterRoutes registers all OpenAI-compatible endpoints on the mux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/chat/completions", s.handleChat)
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/web-search", s.handleWebSearch)
	mux.HandleFunc("/v1/rerank", s.handleRerank)
	mux.HandleFunc("/health", s.handleHealth)
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
	models, err := s.getClient(r).ListModels()
	if err != nil {
		slog.Error("list models upstream error", "error", err)
		writeJSON(w, 200, TranslateModels(modelInfosFromNames(joycode.Models)))
		return
	}
	writeJSON(w, 200, TranslateModels(mergeModelInfos(models, joycode.Models)))
}

func mergeModelInfos(remote []joycode.ModelInfo, builtin []string) []joycode.ModelInfo {
	seen := make(map[string]bool, len(remote)+len(builtin))
	result := make([]joycode.ModelInfo, 0, len(remote)+len(builtin))
	for _, m := range remote {
		id := m.ModelID
		if id == "" {
			id = m.Label
		}
		if id == "" {
			continue
		}
		seen[id] = true
		result = append(result, m)
	}
	for _, name := range builtin {
		if seen[name] {
			continue
		}
		result = append(result, joycode.ModelInfo{Label: name, ModelID: name})
	}
	return result
}

func modelInfosFromNames(models []string) []joycode.ModelInfo {
	result := make([]joycode.ModelInfo, 0, len(models))
	for _, name := range models {
		result = append(result, joycode.ModelInfo{Label: name, ModelID: name})
	}
	return result
}

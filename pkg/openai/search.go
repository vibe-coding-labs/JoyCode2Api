package openai

import (
	"encoding/json"
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
		writeError(w, 400, "invalid JSON")
		return
	}
	if body.Query == "" {
		writeError(w, 400, "query is required")
		return
	}
	results, err := s.Client.WebSearch(body.Query)
	if err != nil {
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
		writeError(w, 400, "invalid JSON")
		return
	}
	if body.Query == "" || len(body.Documents) == 0 {
		writeError(w, 400, "query and documents are required")
		return
	}
	result, err := s.Client.Rerank(body.Query, body.Documents, body.TopN)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, result)
}

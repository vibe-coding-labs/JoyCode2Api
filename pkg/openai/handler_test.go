package openai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/store"
)

// --- Mock infrastructure ---
//
// We create a joycode.Client whose httpClient uses a transport that redirects
// requests from the real JoyCode API base URL to a local httptest.Server.
// The mock server returns canned responses based on the request path.

// redirectTransport is an http.RoundTripper that rewrites every request URL
// to point at a local test server, preserving path and query.
type redirectTransport struct {
	target string
}

func (rt redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	newURL := rt.target + req.URL.Path
	if req.URL.RawQuery != "" {
		newURL += "?" + req.URL.RawQuery
	}
	newReq, err := http.NewRequest(req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	newReq.Header = req.Header
	return http.DefaultTransport.RoundTrip(newReq)
}

// mockHandler returns an http.Handler that serves canned JoyCode API responses.
func mockHandler(responses map[string]interface{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// JoyCode 2.7 端点升 v2；mock 仍按 v1 path 注册，这里归一化 v2→v1。
		path := strings.Replace(r.URL.Path, "/v2/", "/v1/", 1)
		resp, ok := responses[path]
		if !ok {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		b, _ := json.Marshal(resp)
		w.Write(b)
	})
}

// newMockClient creates a joycode.Client that routes all requests to the
// given httptest.Server.
func newMockClient(ts *httptest.Server) *joycode.Client {
	c := joycode.NewClient("test-key", "test-user")
	// 走 direct 模式（清空网关 colorBaseURL），让 mock 按 path 路由而非 /api?functionId=
	c.ColorBaseURL = ""
	c.SetHTTPClient(&http.Client{
		Timeout:   10 * time.Second,
		Transport: redirectTransport{target: ts.URL},
	})
	return c
}

// newTempStore creates a store backed by a temporary database.
// The caller should call the returned cleanup function (which removes the
// temp directory) when done.
func newTempStore() (*store.Store, func(), error) {
	dir, err := os.MkdirTemp("", "joycode-test-*")
	if err != nil {
		return nil, nil, err
	}
	dbPath := filepath.Join(dir, "test.db")
	s, err := store.Open(dbPath)
	if err != nil {
		os.RemoveAll(dir)
		return nil, nil, err
	}
	cleanup := func() {
		s.Close()
		os.RemoveAll(dir)
	}
	return s, cleanup, nil
}

// setupOpenAIServer creates a fully wired openai.Server backed by a mock
// JoyCode API. Returns the HTTP test server for the OpenAI API, and a
// cleanup function.
func setupOpenAIServer(responses map[string]interface{}) (*httptest.Server, func()) {
	// Backend mock for JoyCode API
	backend := httptest.NewServer(mockHandler(responses))
	client := newMockClient(backend)

	st, storeCleanup, err := newTempStore()
	if err != nil {
		backend.Close()
		panic("newTempStore: " + err.Error())
	}
	srv := NewServer(client, st)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	frontend := httptest.NewServer(mux)

	cleanup := func() {
		frontend.Close()
		backend.Close()
		storeCleanup()
	}
	return frontend, cleanup
}

// --- Health endpoint tests ---

// Test 29: GET /health returns 200 with status ok
func TestHealth_Get(t *testing.T) {
	srv, cleanup := setupOpenAIServer(nil)
	defer cleanup()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", body["status"])
	}
}

// Test 30: OPTIONS /health returns 200
func TestHealth_Options(t *testing.T) {
	srv, cleanup := setupOpenAIServer(nil)
	defer cleanup()

	req, _ := http.NewRequest("OPTIONS", srv.URL+"/health", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// --- Models endpoint tests ---

// Test 31: GET /v1/models returns model list
func TestModels_Get(t *testing.T) {
	modelListResp := map[string]interface{}{
		"data": []interface{}{
			map[string]interface{}{
				"label":    "JoyAI-Code",
				"modelId":  "JoyAI-Code",
				"features": []string{},
			},
		},
	}
	srv, cleanup := setupOpenAIServer(map[string]interface{}{
		"/api/saas/models/v1/modelList": modelListResp,
	})
	defer cleanup()

	resp, err := http.Get(srv.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result["object"] != "list" {
		t.Errorf("expected object=list, got %v", result["object"])
	}
	data, ok := result["data"].([]interface{})
	if !ok {
		t.Fatal("data is not a slice")
	}
	if len(data) != 1 {
		t.Fatalf("expected 1 model, got %d", len(data))
	}
}

// Test 32: Server error returns 500
func TestModels_ServerError(t *testing.T) {
	errorBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"internal"}`))
	}))
	defer errorBackend.Close()

	client := newMockClient(errorBackend)
	st, storeCleanup, err := newTempStore()
	if err != nil {
		t.Fatal(err)
	}
	defer storeCleanup()
	srv := NewServer(client, st)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	frontend := httptest.NewServer(mux)
	defer frontend.Close()

	resp, err := http.Get(frontend.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 500 {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

// --- Chat endpoint tests ---

// Test 33: OPTIONS returns 200
func TestChat_Options(t *testing.T) {
	srv, cleanup := setupOpenAIServer(nil)
	defer cleanup()

	req, _ := http.NewRequest("OPTIONS", srv.URL+"/v1/chat/completions", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// Test 34: GET returns 405
func TestChat_Get(t *testing.T) {
	srv, cleanup := setupOpenAIServer(nil)
	defer cleanup()

	resp, err := http.Get(srv.URL + "/v1/chat/completions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 405 {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}

// Test 35: Invalid JSON returns 400
func TestChat_InvalidJSON(t *testing.T) {
	srv, cleanup := setupOpenAIServer(nil)
	defer cleanup()

	resp, err := http.Post(
		srv.URL+"/v1/chat/completions",
		"application/json",
		strings.NewReader("this is not json"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// Test 36: Valid non-stream request returns 200
func TestChat_ValidNonStream(t *testing.T) {
	chatResp := map[string]interface{}{
		"choices": []interface{}{
			map[string]interface{}{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "Hello!",
				},
			},
		},
		"usage": map[string]interface{}{
			"total_tokens": float64(15),
		},
	}
	srv, cleanup := setupOpenAIServer(map[string]interface{}{
		"/api/saas/openai/v1/chat/completions": chatResp,
	})
	defer cleanup()

	body := `{"model":"JoyAI-Code","messages":[{"role":"user","content":"hi"}],"stream":false}`
	resp, err := http.Post(
		srv.URL+"/v1/chat/completions",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result["model"] != "JoyAI-Code" {
		t.Errorf("expected model=JoyAI-Code, got %v", result["model"])
	}
	if result["object"] != "chat.completion" {
		t.Errorf("expected object=chat.completion, got %v", result["object"])
	}
}

// --- WebSearch endpoint tests ---

// Test 37: OPTIONS returns 200
func TestWebSearch_Options(t *testing.T) {
	srv, cleanup := setupOpenAIServer(nil)
	defer cleanup()

	req, _ := http.NewRequest("OPTIONS", srv.URL+"/v1/web-search", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// Test 38: Invalid JSON returns 400
func TestWebSearch_InvalidJSON(t *testing.T) {
	srv, cleanup := setupOpenAIServer(nil)
	defer cleanup()

	resp, err := http.Post(
		srv.URL+"/v1/web-search",
		"application/json",
		strings.NewReader("not json"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// Test 39: Empty query returns 400
func TestWebSearch_EmptyQuery(t *testing.T) {
	srv, cleanup := setupOpenAIServer(nil)
	defer cleanup()

	resp, err := http.Post(
		srv.URL+"/v1/web-search",
		"application/json",
		strings.NewReader(`{"query":""}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// Test 40: Valid search returns 200
func TestWebSearch_Valid(t *testing.T) {
	searchResp := map[string]interface{}{
		"search_result": []interface{}{
			map[string]interface{}{
				"title": "Test Result",
				"url":   "https://example.com",
			},
		},
	}
	srv, cleanup := setupOpenAIServer(map[string]interface{}{
		"/api/saas/openai/v1/web-search": searchResp,
	})
	defer cleanup()

	resp, err := http.Post(
		srv.URL+"/v1/web-search",
		"application/json",
		strings.NewReader(`{"query":"test query"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	sr, ok := result["search_result"]
	if !ok {
		t.Error("expected search_result in response")
	}
	results, ok := sr.([]interface{})
	if !ok || len(results) != 1 {
		t.Errorf("expected 1 search result, got %v", sr)
	}
}

// --- Rerank endpoint tests ---

// Test 41: Invalid JSON returns 400
func TestRerank_InvalidJSON(t *testing.T) {
	srv, cleanup := setupOpenAIServer(nil)
	defer cleanup()

	resp, err := http.Post(
		srv.URL+"/v1/rerank",
		"application/json",
		strings.NewReader("not json"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// Test 42: Empty query and documents returns 400
func TestRerank_EmptyQueryAndDocs(t *testing.T) {
	srv, cleanup := setupOpenAIServer(nil)
	defer cleanup()

	resp, err := http.Post(
		srv.URL+"/v1/rerank",
		"application/json",
		strings.NewReader(`{"query":"","documents":[]}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	errObj, _ := body["error"].(map[string]interface{})
	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "query and documents are required") {
		t.Errorf("expected error about query and documents, got %s", msg)
	}
}

// Test 43: Valid rerank returns 200
func TestRerank_Valid(t *testing.T) {
	rerankResp := map[string]interface{}{
		"results": []interface{}{
			map[string]interface{}{
				"index":          float64(0),
				"relevance_score": float64(0.95),
				"document":       map[string]interface{}{"text": "doc1"},
			},
		},
	}
	srv, cleanup := setupOpenAIServer(map[string]interface{}{
		"/api/saas/openai/v1/rerank": rerankResp,
	})
	defer cleanup()

	reqBody := `{"query":"test","documents":["doc1","doc2"],"top_n":2}`
	resp, err := http.Post(
		srv.URL+"/v1/rerank",
		"application/json",
		strings.NewReader(reqBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	results, ok := result["results"].([]interface{})
	if !ok || len(results) != 1 {
		t.Errorf("expected 1 result, got %v", result)
	}
}

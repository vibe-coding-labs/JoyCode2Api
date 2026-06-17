package joycode

import (
	"bytes"
	"compress/gzip"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helper: builds a Client that routes all requests through a redirect
// transport to the given httptest.Server.
// ---------------------------------------------------------------------------

func testServerClient(handler http.Handler) (*Client, func()) {
	srv := httptest.NewServer(handler)
	c := NewClient("test-key", "test-user")
	c.httpClient = srv.Client()
	c.httpClient.Transport = redirectTransport{target: srv.URL, base: http.DefaultTransport}
	return c, srv.Close
}

// redirectTransport rewrites every request URL to point at the test server
// while preserving the original path and query string.
type redirectTransport struct {
	target string
	base   http.RoundTripper
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
	return rt.base.RoundTrip(newReq)
}

// ---------------------------------------------------------------------------
// Unit tests for constructors, headers, body preparation, ID generation
// ---------------------------------------------------------------------------

func TestNewClient_SessionIDUnique(t *testing.T) {
	a := NewClient("k", "u")
	b := NewClient("k", "u")
	if a.SessionID == b.SessionID {
		t.Errorf("two clients should have different session IDs, got same %q", a.SessionID)
	}
}

func TestNewClient_EmptyCredentials(t *testing.T) {
	c := NewClient("", "")
	if c.PtKey != "" || c.UserID != "" {
		t.Errorf("expected empty credentials, got PtKey=%q UserID=%q", c.PtKey, c.UserID)
	}
	if c.SessionID == "" {
		t.Error("SessionID should still be generated with empty credentials")
	}
	if c.httpClient == nil {
		t.Error("httpClient should be initialised")
	}
}

func TestHeaders_ContainsRequiredFields(t *testing.T) {
	c := NewClient("my-key", "u1")
	h := c.headers()

	// Canonical headers (use Get, which canonicalizes the key).
	canonical := []string{
		"Content-Type",
		"User-Agent",
		"Accept",
		"Accept-Encoding",
		"Accept-Language",
	}
	for _, key := range canonical {
		if v := h.Get(key); v == "" {
			t.Errorf("headers missing required field %q", key)
		}
	}
	// Non-canonical headers stored via map literal; access directly.
	nonCanonical := []string{"ptKey", "loginType", "source-type"}
	for _, key := range nonCanonical {
		vals := h[key]
		if len(vals) == 0 || vals[0] == "" {
			t.Errorf("headers missing required field %q", key)
		}
	}
}

func TestHeaders_PtKeySet(t *testing.T) {
	c := NewClient("abc123token", "u1")
	h := c.headers()
	vals := h["ptKey"]
	if len(vals) == 0 || vals[0] != "abc123token" {
		t.Errorf("ptKey header = %v, want %q", vals, "abc123token")
	}
}

func TestPrepareBody_DefaultFields(t *testing.T) {
	c := NewClient("k", "user42")
	body := c.prepareBody(map[string]interface{}{})

	defaults := map[string]string{
		"tenant":        "JOYCODE",
		"userId":        "user42",
		"client":        "JoyCode",
		"clientVersion": ClientVersion,
		"language":      "UNKNOWN",
	}
	for field, want := range defaults {
		got, _ := body[field].(string)
		if got != want {
			t.Errorf("prepareBody()[%q] = %q, want %q", field, got, want)
		}
	}
}

func TestPrepareBody_NoLegacyTrackingFields(t *testing.T) {
	// JoyCode 2.7 协议不再自动注入 chatId/requestId/sessionId（对齐真实客户端 customFetch）。
	c := NewClient("k", "u")
	body := c.prepareBody(map[string]interface{}{})
	for _, key := range []string{"chatId", "requestId", "sessionId"} {
		if _, ok := body[key]; ok {
			t.Errorf("prepareBody should not auto-inject %q in 2.7 protocol", key)
		}
	}
}

func TestPrepareBody_ExtraChatIdPassedThrough(t *testing.T) {
	c := NewClient("k", "u")
	body := c.prepareBody(map[string]interface{}{"chatId": "keep-me"})
	if got, _ := body["chatId"].(string); got != "keep-me" {
		t.Errorf("chatId = %q, want %q", got, "keep-me")
	}
}

func TestPrepareBody_ExtraFieldsMerged(t *testing.T) {
	c := NewClient("k", "u")
	body := c.prepareBody(map[string]interface{}{
		"model":    "GLM-5",
		"stream":   true,
		"messages": []string{"hello"},
	})
	if body["model"] != "GLM-5" {
		t.Errorf("extra field model not merged: %v", body["model"])
	}
	if body["stream"] != true {
		t.Errorf("extra field stream not merged: %v", body["stream"])
	}
	msgs, ok := body["messages"].([]string)
	if !ok || len(msgs) != 1 || msgs[0] != "hello" {
		t.Errorf("extra field messages not merged correctly: %v", body["messages"])
	}
}

func TestNewHexID_LengthAndFormat(t *testing.T) {
	id := newHexID()
	if len(id) != 32 {
		t.Errorf("newHexID() length = %d, want 32", len(id))
	}
	matched, _ := regexp.MatchString("^[0-9a-f]{32}$", id)
	if !matched {
		t.Errorf("newHexID() = %q, want 32 lowercase hex chars", id)
	}
}

func TestNewHexID_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := newHexID()
		if seen[id] {
			t.Fatalf("duplicate hex ID generated: %q", id)
		}
		seen[id] = true
	}
}

// ---------------------------------------------------------------------------
// Client.Post tests
// ---------------------------------------------------------------------------

func TestClient_Post_Success(t *testing.T) {
	want := map[string]interface{}{
		"code": float64(0),
		"data": "ok",
	}
	c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	defer cleanup()

	got, err := c.Post("/api/test", map[string]interface{}{"q": "hi"})
	if err != nil {
		t.Fatalf("Post() error: %v", err)
	}
	if got["code"] != want["code"] {
		t.Errorf("Post() code = %v, want %v", got["code"], want["code"])
	}
}

func TestClient_Post_ServerError(t *testing.T) {
	c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer cleanup()

	_, err := c.Post("/api/test", nil)
	if err == nil {
		t.Fatal("Post() should return error on 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status 500, got: %v", err)
	}
}

func TestClient_Post_InvalidJSON(t *testing.T) {
	c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("<<<not json>>>"))
	}))
	defer cleanup()

	_, err := c.Post("/api/test", nil)
	if err == nil {
		t.Fatal("Post() should return error on invalid JSON")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("error should mention invalid JSON, got: %v", err)
	}
}

func TestClient_Post_GzipResponse(t *testing.T) {
	want := map[string]interface{}{
		"code": float64(0),
		"msg":  "gzipped",
	}
	c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		json.NewEncoder(gz).Encode(want)
		gz.Close()
		w.Write(buf.Bytes())
	}))
	defer cleanup()

	got, err := c.Post("/api/test", nil)
	if err != nil {
		t.Fatalf("Post() error: %v", err)
	}
	if got["msg"] != "gzipped" {
		t.Errorf("gzip response msg = %v, want %q", got["msg"], "gzipped")
	}
}

// ---------------------------------------------------------------------------
// Client.PostStream tests
// ---------------------------------------------------------------------------

func TestClient_PostStream_Success(t *testing.T) {
	c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: hello\n\ndata: world\n\n"))
	}))
	defer cleanup()

	resp, err := c.PostStream("/api/test", nil)
	if err != nil {
		t.Fatalf("PostStream() error: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "hello") {
		t.Errorf("PostStream() body = %q, want to contain 'hello'", string(body))
	}
}

func TestClient_PostStream_ServerError(t *testing.T) {
	c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer cleanup()

	_, err := c.PostStream("/api/test", nil)
	if err == nil {
		t.Fatal("PostStream() should return error on 502")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("error should mention 502, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Client.ListModels tests
// ---------------------------------------------------------------------------

func TestClient_ListModels_Success(t *testing.T) {
	modelData := []map[string]interface{}{
		{
			"label": "GLM", "chatApiModel": "glm-5", "maxTotalTokens": 8192,
			"respMaxTokens": 4096, "temperature": 0.7, "features": []string{"code"},
			"supportStream": true, "verificationStatus": "verified",
			"modelId": "glm-5-id", "createTime": float64(1700000000),
		},
		{
			"label": "Doubao", "chatApiModel": "doubao-pro", "maxTotalTokens": 16384,
			"respMaxTokens": 8192, "temperature": 0.5, "features": []string{"chat"},
			"supportStream": false, "verificationStatus": "verified",
			"modelId": "doubao-id", "createTime": float64(1700000001),
		},
	}
	c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code": float64(0),
			"data": modelData,
		})
	}))
	defer cleanup()

	models, err := c.ListModels()
	if err != nil {
		t.Fatalf("ListModels() error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("ListModels() returned %d models, want 2", len(models))
	}
	if models[0].Label != "GLM" {
		t.Errorf("models[0].Label = %q, want %q", models[0].Label, "GLM")
	}
	if models[1].ModelID != "doubao-id" {
		t.Errorf("models[1].ModelID = %q, want %q", models[1].ModelID, "doubao-id")
	}
}

func TestClient_ListModels_MalformedModel(t *testing.T) {
	c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code": float64(0),
			"data": []interface{}{
				"this-is-not-a-model-object",
				map[string]interface{}{
					"label":              "Valid",
					"chatApiModel":       "valid-model",
					"maxTotalTokens":     4096,
					"respMaxTokens":      2048,
					"temperature":        0.8,
					"features":           []string{},
					"supportStream":      true,
					"verificationStatus": "ok",
					"modelId":            "valid-id",
					"createTime":         float64(100),
				},
			},
		})
	}))
	defer cleanup()

	models, err := c.ListModels()
	if err != nil {
		t.Fatalf("ListModels() error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("ListModels() returned %d models, want 1 (bad entry skipped)", len(models))
	}
	if models[0].Label != "Valid" {
		t.Errorf("models[0].Label = %q, want %q", models[0].Label, "Valid")
	}
}

func TestClient_ListModels_MissingDataArray(t *testing.T) {
	c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code": float64(0),
			// "data" key is missing entirely
		})
	}))
	defer cleanup()

	_, err := c.ListModels()
	if err == nil {
		t.Fatal("ListModels() should return error when data array missing")
	}
	if !strings.Contains(err.Error(), "missing data array") {
		t.Errorf("error should mention missing data array, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Client.Validate tests
// ---------------------------------------------------------------------------

func TestClient_Validate_Success(t *testing.T) {
	c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code": float64(0),
			"msg":  "success",
			"data": map[string]interface{}{"userId": "u1"},
		})
	}))
	defer cleanup()

	if err := c.Validate(); err != nil {
		t.Errorf("Validate() returned unexpected error: %v", err)
	}
}

func TestClient_Validate_InvalidToken(t *testing.T) {
	c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code": float64(401),
			"msg":  "invalid token",
		})
	}))
	defer cleanup()

	err := c.Validate()
	if err == nil {
		t.Fatal("Validate() should return error for code != 0")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention code 401, got: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid token") {
		t.Errorf("error should contain msg, got: %v", err)
	}
}

func TestClient_Validate_ApiError(t *testing.T) {
	c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gateway timeout", http.StatusGatewayTimeout)
	}))
	defer cleanup()

	err := c.Validate()
	if err == nil {
		t.Fatal("Validate() should return error on server failure")
	}
	if !strings.Contains(err.Error(), "credential validation failed") {
		t.Errorf("error should mention validation failed, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Client.WebSearch tests
// ---------------------------------------------------------------------------

func TestClient_WebSearch_Success(t *testing.T) {
	c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		if reqBody["model"] != "search_pro_jina" {
			t.Errorf("WebSearch model = %v, want search_pro_jina", reqBody["model"])
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"code": float64(0),
			"search_result": []interface{}{
				map[string]interface{}{"title": "Result 1", "url": "https://example.com"},
				map[string]interface{}{"title": "Result 2", "url": "https://example.org"},
			},
		})
	}))
	defer cleanup()

	results, err := c.WebSearch("test query")
	if err != nil {
		t.Fatalf("WebSearch() error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("WebSearch() returned %d results, want 2", len(results))
	}
}

// ---------------------------------------------------------------------------
// Client.Rerank tests
// ---------------------------------------------------------------------------

func TestClient_Rerank_Success(t *testing.T) {
	c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		if reqBody["model"] != "Qwen3-Reranker-8B" {
			t.Errorf("Rerank model = %v, want Qwen3-Reranker-8B", reqBody["model"])
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"code": float64(0),
			"data": []interface{}{
				map[string]interface{}{"index": float64(1), "relevance_score": 0.95},
				map[string]interface{}{"index": float64(0), "relevance_score": 0.72},
			},
		})
	}))
	defer cleanup()

	result, err := c.Rerank("query", []string{"doc1", "doc2"}, 5)
	if err != nil {
		t.Fatalf("Rerank() error: %v", err)
	}
	data, ok := result["data"].([]interface{})
	if !ok || len(data) != 2 {
		t.Fatalf("Rerank() data = %v, want 2 items", result["data"])
	}
}

// ---------------------------------------------------------------------------
// Table-driven: doPost sends correct method, path, headers, and body
// ---------------------------------------------------------------------------

func TestClient_DoPost_SendsCorrectRequest(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		body     map[string]interface{}
	}{
		{"simple", "/api/test", map[string]interface{}{"key": "val"}},
		{"empty body", "/api/empty", map[string]interface{}{}},
		{"nested", "/api/nested", map[string]interface{}{"outer": map[string]interface{}{"inner": float64(42)}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %q, want POST", r.Method)
				}
				if r.URL.Path != tt.endpoint {
					t.Errorf("path = %q, want %q", r.URL.Path, tt.endpoint)
				}
				ct := r.Header.Get("Content-Type")
				if ct != "application/json; charset=UTF-8" {
					t.Errorf("Content-Type = %q, want application/json; charset=UTF-8", ct)
				}
				if pk := r.Header.Get("ptKey"); pk != "test-key" {
					t.Errorf("ptKey = %q, want %q", pk, "test-key")
				}
				var got map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatalf("body is not valid JSON: %v", err)
				}
				for k, v := range tt.body {
					if !reflect.DeepEqual(got[k], v) {
						t.Errorf("body[%q] = %v, want %v", k, got[k], v)
					}
				}
				json.NewEncoder(w).Encode(map[string]interface{}{"code": float64(0)})
			}))
			defer cleanup()

			_, _ = c.Post(tt.endpoint, tt.body)
		})
	}
}

// ---------------------------------------------------------------------------
// Table-driven: prepareBody with various inputs
// ---------------------------------------------------------------------------

func TestPrepareBody_Table(t *testing.T) {
	c := NewClient("k", "user1")

	tests := []struct {
		name  string
		extra map[string]interface{}
		check func(t *testing.T, body map[string]interface{})
	}{
		{
			name:  "defaults only",
			extra: map[string]interface{}{},
			check: func(t *testing.T, body map[string]interface{}) {
				for _, key := range []string{"tenant", "userId", "client", "clientVersion", "language"} {
					if body[key] == nil {
						t.Errorf("missing default field %q", key)
					}
				}
			},
		},
		{
			name:  "custom chatId and requestId",
			extra: map[string]interface{}{"chatId": "c1", "requestId": "r1"},
			check: func(t *testing.T, body map[string]interface{}) {
				if body["chatId"] != "c1" {
					t.Errorf("chatId = %v, want c1", body["chatId"])
				}
				if body["requestId"] != "r1" {
					t.Errorf("requestId = %v, want r1", body["requestId"])
				}
			},
		},
		{
			name:  "extra fields override defaults",
			extra: map[string]interface{}{"tenant": "CUSTOM"},
			check: func(t *testing.T, body map[string]interface{}) {
				if body["tenant"] != "CUSTOM" {
					t.Errorf("tenant = %v, want CUSTOM", body["tenant"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := c.prepareBody(tt.extra)
			tt.check(t, body)
		})
	}
}

// ---------------------------------------------------------------------------
// Validate edge cases
// ---------------------------------------------------------------------------

func TestClient_Validate_MissingCodeField(t *testing.T) {
	c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"msg": "no code",
		})
	}))
	defer cleanup()

	err := c.Validate()
	if err == nil {
		t.Fatal("Validate() should return error when code field is missing")
	}
}

func TestClient_Validate_CodeNotFloat(t *testing.T) {
	c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":"not-a-number","msg":"weird"}`))
	}))
	defer cleanup()

	err := c.Validate()
	if err == nil {
		t.Fatal("Validate() should return error when code is not a number")
	}
}

func TestClient_Validate_NonZeroCodeEmptyMsg(t *testing.T) {
	c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code": float64(500),
		})
	}))
	defer cleanup()

	err := c.Validate()
	if err == nil {
		t.Fatal("Validate() should return error for non-zero code")
	}
	if !strings.Contains(err.Error(), "unknown error") {
		t.Errorf("error should mention 'unknown error', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Post with nil body (prepareBody handles nil map gracefully)
// ---------------------------------------------------------------------------

func TestClient_Post_NilBody(t *testing.T) {
	c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"code": float64(0)})
	}))
	defer cleanup()

	body := c.prepareBody(nil)
	got, err := c.Post("/test", body)
	if err != nil {
		t.Fatalf("Post() error: %v", err)
	}
	if got["code"] != float64(0) {
		t.Errorf("Post() code = %v, want 0", got["code"])
	}
}

// ---------------------------------------------------------------------------
// Concurrent newHexID should not produce duplicates
// ---------------------------------------------------------------------------

func TestNewHexID_Concurrent(t *testing.T) {
	const n = 50
	ids := make(chan string, n)
	for i := 0; i < n; i++ {
		go func() {
			ids <- newHexID()
		}()
	}
	seen := make(map[string]bool)
	for i := 0; i < n; i++ {
		id := <-ids
		if seen[id] {
			t.Errorf("duplicate ID generated concurrently: %q", id)
		}
		seen[id] = true
	}
}

// ---------------------------------------------------------------------------
// Verify correct Content-Type and Accept-Encoding headers are sent
// ---------------------------------------------------------------------------

func TestClient_Post_SendsCorrectHeaders(t *testing.T) {
	c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		if ct != "application/json; charset=UTF-8" {
			t.Errorf("Content-Type = %q, want %q", ct, "application/json; charset=UTF-8")
		}
		ae := r.Header.Get("Accept-Encoding")
		if ae != "gzip, deflate" {
			t.Errorf("Accept-Encoding = %q, want %q", ae, "gzip, deflate")
		}
		ua := r.Header.Get("User-Agent")
		if ua != UserAgent {
			t.Errorf("User-Agent = %q, want %q", ua, UserAgent)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"code": float64(0)})
	}))
	defer cleanup()

	_, _ = c.Post("/test", map[string]interface{}{})
}

// ---------------------------------------------------------------------------
// ListModels with empty data array returns empty slice (not error)
// ---------------------------------------------------------------------------

func TestClient_ListModels_EmptyDataArray(t *testing.T) {
	c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code": float64(0),
			"data": []interface{}{},
		})
	}))
	defer cleanup()

	models, err := c.ListModels()
	if err != nil {
		t.Fatalf("ListModels() error: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("ListModels() returned %d models, want 0", len(models))
	}
}

// ---------------------------------------------------------------------------
// Table-driven: Post error propagation on various HTTP status codes
// ---------------------------------------------------------------------------

func TestClient_Post_VariousStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{"200 OK", 200, false},
		{"400 Bad Request", 400, true},
		{"401 Unauthorized", 401, true},
		{"403 Forbidden", 403, true},
		{"404 Not Found", 404, true},
		{"500 Internal Server Error", 500, true},
		{"502 Bad Gateway", 502, true},
		{"503 Service Unavailable", 503, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				fmt.Fprintf(w, `{"code": %d}`, tt.statusCode)
			}))
			defer cleanup()

			_, err := c.Post("/test", nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("Post() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// PostStream returns a readable body for streaming use cases
// ---------------------------------------------------------------------------

func TestClient_PostStream_ResponseBodyReadable(t *testing.T) {
	chunks := []string{"chunk1", "chunk2", "chunk3"}
	c, cleanup := testServerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		for _, ch := range chunks {
			w.Write([]byte(ch))
		}
	}))
	defer cleanup()

	resp, err := c.PostStream("/test", nil)
	if err != nil {
		t.Fatalf("PostStream() error: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading stream body: %v", err)
	}
	got := string(data)
	want := "chunk1chunk2chunk3"
	if got != want {
		t.Errorf("stream body = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// decodeBody with plain and gzipped responses
// ---------------------------------------------------------------------------

func TestDecodeBody_PlainJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	data, err := decodeBody(resp)
	if err != nil {
		t.Fatalf("decodeBody() error: %v", err)
	}
	if string(data) != `{"ok":true}` {
		t.Errorf("decodeBody() = %q, want %q", string(data), `{"ok":true}`)
	}
}

func TestDecodeBody_Gzipped(t *testing.T) {
	want := `{"gzipped":true}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		gz.Write([]byte(want))
		gz.Close()
		w.Write(buf.Bytes())
	}))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	data, err := decodeBody(resp)
	if err != nil {
		t.Fatalf("decodeBody() error: %v", err)
	}
	if string(data) != want {
		t.Errorf("decodeBody() = %q, want %q", string(data), want)
	}
}

// ---------------------------------------------------------------------------
// color gateway signing / routing (JoyCode 2.7)
// ---------------------------------------------------------------------------

func TestRequestURL_GatewaySigned(t *testing.T) {
	c := NewClient("k", "u")
	c.ColorBaseURL = "https://api-ai.jd.com"
	raw := c.requestURL("/api/saas/openai/v1/chat/completions")

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	if u.Host != "api-ai.jd.com" || u.Path != "/api" {
		t.Errorf("gateway url host/path = %q/%q, want api-ai.jd.com//api", u.Host, u.Path)
	}
	q := u.Query()
	if q.Get("appid") != "joycode_ide" {
		t.Errorf("appid = %q, want joycode_ide", q.Get("appid"))
	}
	if q.Get("functionId") != "chat_completions" {
		t.Errorf("functionId = %q, want chat_completions", q.Get("functionId"))
	}
	if q.Get("t") == "" {
		t.Error("missing timestamp t")
	}
	// 重算签名校验：HMAC_SHA256(sorted(values).join("&"), key)
	signStr := "joycode_ide&chat_completions&" + q.Get("t")
	mac := hmac.New(sha256.New, []byte(colorHMACKey))
	mac.Write([]byte(signStr))
	want := hex.EncodeToString(mac.Sum(nil))
	if q.Get("sign") != want {
		t.Errorf("sign = %q, want %q", q.Get("sign"), want)
	}
}

func TestRequestURL_DirectV2WhenNoColorBase(t *testing.T) {
	c := NewClient("k", "u")
	c.ColorBaseURL = ""
	c.MasterBaseURL = "https://joycode-api.jd.com"
	got := c.requestURL("/api/saas/models/v1/modelList")
	want := "https://joycode-api.jd.com/api/saas/models/v2/modelList"
	if got != want {
		t.Errorf("direct url = %q, want %q", got, want)
	}
}

func TestRequestURL_UnmappedEndpointStaysDirect(t *testing.T) {
	c := NewClient("k", "u")
	got := c.requestURL("/api/saas/openai/v1/rerank") // 不在 color 端点表
	want := BaseURL + "/api/saas/openai/v1/rerank"
	if got != want {
		t.Errorf("unmapped url = %q, want %q", got, want)
	}
}

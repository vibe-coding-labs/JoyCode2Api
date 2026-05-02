package dashboard

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/store"
)

func setupTestHandler(t *testing.T) (*Handler, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	// Create minimal static FS for tests
	staticDir := filepath.Join(dir, "static")
	os.MkdirAll(staticDir, 0755)
	os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<html>test</html>"), 0644)
	os.MkdirAll(filepath.Join(staticDir, "assets"), 0755)
	os.WriteFile(filepath.Join(staticDir, "assets", "test.js"), []byte("console.log(1)"), 0644)

	subFS := os.DirFS(staticDir)
	h := NewHandler(s, subFS)
	return h, s
}

func makeRequest(t *testing.T, method, path string, body interface{}) *http.Request {
	t.Helper()
	var req *http.Request
	if body != nil {
		b, _ := json.Marshal(body)
		req = httptest.NewRequest(method, path, strings.NewReader(string(b)))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	return req
}

func decodeJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&m); err != nil {
		t.Fatalf("decode json: %v, body: %s", err, w.Body.String())
	}
	return m
}

// --- Health ---

func TestHandleHealth(t *testing.T) {
	h, _ := setupTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}

	m := decodeJSON(t, w)
	if m["status"] != "ok" {
		t.Errorf("status = %v, want ok", m["status"])
	}
	if _, ok := m["accounts"]; !ok {
		t.Error("missing accounts field")
	}
	if _, ok := m["version"]; !ok {
		t.Error("missing version field")
	}
}

func TestHandleHealthCORS(t *testing.T) {
	h, _ := setupTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("OPTIONS", "/api/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("OPTIONS status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if origin := w.Header().Get("Access-Control-Allow-Origin"); origin != "*" {
		t.Errorf("CORS origin = %q, want *", origin)
	}
}

// --- Accounts ---

func TestHandleListAccountsEmpty(t *testing.T) {
	h, _ := setupTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/accounts", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}

	m := decodeJSON(t, w)
	accounts, ok := m["accounts"].([]interface{})
	if !ok {
		t.Fatalf("accounts is not a list: %T", m["accounts"])
	}
	if len(accounts) != 0 {
		t.Errorf("accounts len = %d, want 0", len(accounts))
	}
}

func TestHandleAddAndListAccounts(t *testing.T) {
	h, _ := setupTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Add account
	req := makeRequest(t, "POST", "/api/accounts", map[string]interface{}{
		"api_key": "test-key",
		"pt_key":  "test-pt",
		"user_id": "test-user",
	})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("add status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	m := decodeJSON(t, w)
	if m["ok"] != true {
		t.Errorf("ok = %v, want true", m["ok"])
	}

	// List accounts
	req = httptest.NewRequest("GET", "/api/accounts", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	m = decodeJSON(t, w)
	accounts := m["accounts"].([]interface{})
	if len(accounts) != 1 {
		t.Fatalf("accounts len = %d, want 1", len(accounts))
	}
	acc := accounts[0].(map[string]interface{})
	if acc["api_key"] != "test-key" {
		t.Errorf("api_key = %v, want test-key", acc["api_key"])
	}
}

func TestHandleAddAccountMissingFields(t *testing.T) {
	h, _ := setupTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := makeRequest(t, "POST", "/api/accounts", map[string]interface{}{
		"api_key": "test-key",
	})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleRemoveAccount(t *testing.T) {
	h, s := setupTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	s.AddAccount("del-key", "pt", "user", false, "")

	req := httptest.NewRequest("DELETE", "/api/accounts/del-key", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	m := decodeJSON(t, w)
	if m["ok"] != true {
		t.Errorf("ok = %v, want true", m["ok"])
	}
}

func TestHandleSetDefault(t *testing.T) {
	h, s := setupTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	s.AddAccount("key1", "pt1", "user1", true, "")
	s.AddAccount("key2", "pt2", "user2", false, "")

	req := httptest.NewRequest("PUT", "/api/accounts/key2/default", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}

	a, _ := s.GetAccount("key2")
	if !a.IsDefault {
		t.Error("key2 should be default after PUT")
	}
}

func TestHandleUpdateModel(t *testing.T) {
	h, s := setupTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	s.AddAccount("key1", "pt1", "user1", true, "")

	req := makeRequest(t, "PUT", "/api/accounts/key1/model", map[string]interface{}{
		"default_model": "GLM-5.1",
	})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}

	a, _ := s.GetAccount("key1")
	if a.DefaultModel != "GLM-5.1" {
		t.Errorf("model = %q, want GLM-5.1", a.DefaultModel)
	}
}

// --- Models ---

func TestHandleModels(t *testing.T) {
	h, _ := setupTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/models", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}

	m := decodeJSON(t, w)
	models, ok := m["models"].([]interface{})
	if !ok {
		t.Fatalf("models is not a list: %T", m["models"])
	}
	if len(models) == 0 {
		t.Error("expected at least 1 model")
	}
	first := models[0].(map[string]interface{})
	if first["id"] == "" || first["name"] == "" {
		t.Error("model should have id and name")
	}
}

// --- Stats ---

func TestHandleStatsEmpty(t *testing.T) {
	h, _ := setupTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}

	m := decodeJSON(t, w)
	if m["total_requests"] != float64(0) {
		t.Errorf("total_requests = %v, want 0", m["total_requests"])
	}
}

func TestHandleStatsWithLogs(t *testing.T) {
	h, s := setupTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	s.AddAccount("key1", "pt1", "user1", true, "")
	s.LogRequest("key1", "JoyAI-Code", "/v1/chat", true, 200, 500, "")

	req := httptest.NewRequest("GET", "/api/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	m := decodeJSON(t, w)
	if m["total_requests"] != float64(1) {
		t.Errorf("total_requests = %v, want 1", m["total_requests"])
	}
}

// --- Settings ---

func TestHandleGetSettingsEmpty(t *testing.T) {
	h, _ := setupTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/settings", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}

	m := decodeJSON(t, w)
	settings, ok := m["settings"].(map[string]interface{})
	if !ok {
		t.Fatalf("settings is not a map: %T", m["settings"])
	}
	if len(settings) != 0 {
		t.Errorf("settings len = %d, want 0", len(settings))
	}
}

func TestHandleUpdateAndGetSettings(t *testing.T) {
	h, _ := setupTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Update
	req := makeRequest(t, "PUT", "/api/settings", map[string]interface{}{
		"theme": "dark",
		"lang":  "zh",
	})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("put status = %d, want 200", w.Code)
	}

	// Get
	req = httptest.NewRequest("GET", "/api/settings", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	m := decodeJSON(t, w)
	settings := m["settings"].(map[string]interface{})
	if settings["theme"] != "dark" {
		t.Errorf("theme = %v, want dark", settings["theme"])
	}
	if settings["lang"] != "zh" {
		t.Errorf("lang = %v, want zh", settings["lang"])
	}
}

// --- Account Stats ---

func TestHandleAccountStats(t *testing.T) {
	h, s := setupTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	s.AddAccount("key1", "pt1", "user1", true, "")
	s.LogRequest("key1", "JoyAI-Code", "/v1/chat", true, 200, 500, "")
	s.LogRequest("key1", "GLM-5.1", "/v1/msg", false, 200, 300, "")

	req := httptest.NewRequest("GET", "/api/accounts/key1/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}

	m := decodeJSON(t, w)
	if m["total_requests"] != float64(2) {
		t.Errorf("total_requests = %v, want 2", m["total_requests"])
	}
}

func TestHandleAccountStatsNonexistent(t *testing.T) {
	h, _ := setupTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/accounts/nonexistent/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// --- Static file serving ---

func TestServeStaticIndex(t *testing.T) {
	h, _ := setupTestHandler(t)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeStatic(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "test") {
		t.Errorf("expected html content, got: %s", body)
	}
}

func TestServeStaticSPAFallback(t *testing.T) {
	h, _ := setupTestHandler(t)

	// Unknown path should fallback to index.html
	req := httptest.NewRequest("GET", "/accounts", nil)
	w := httptest.NewRecorder()
	h.ServeStatic(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "test") {
		t.Errorf("expected fallback to index.html, got: %s", body)
	}
}

func TestServeStaticAsset(t *testing.T) {
	h, _ := setupTestHandler(t)

	req := httptest.NewRequest("GET", "/assets/test.js", nil)
	w := httptest.NewRecorder()
	h.ServeStatic(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "console.log") {
		t.Errorf("expected js content, got: %s", body)
	}
}

// --- Method not allowed ---

func TestMethodNotAllowed(t *testing.T) {
	h, _ := setupTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("PATCH", "/api/accounts", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// --- Interface check ---

var _ fs.FS = (os.DirFS(""))

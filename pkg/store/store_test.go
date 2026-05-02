package store

import (
	"os"
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// --- Database init ---

func TestOpenCreatesDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("database file was not created")
	}
}

func TestOpenCreatesDir(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sub", "dir", "test.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open with nested dir: %v", err)
	}
	defer s.Close()
}

func TestDefaultDBPath(t *testing.T) {
	path, err := DefaultDBPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty default path")
	}
}

// --- Encryption ---

func TestEncryptDecrypt(t *testing.T) {
	s := openTestStore(t)

	plaintext := "super-secret-pt-key-12345"
	encrypted, err := s.encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if encrypted == plaintext {
		t.Error("encrypted should differ from plaintext")
	}

	decrypted, err := s.decrypt(encrypted)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("decrypt = %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptDifferentEachTime(t *testing.T) {
	s := openTestStore(t)

	plaintext := "same-input"
	enc1, _ := s.encrypt(plaintext)
	enc2, _ := s.encrypt(plaintext)
	if enc1 == enc2 {
		t.Error("two encryptions of same input should produce different ciphertext (random nonce)")
	}
}

func TestDecryptInvalidCiphertext(t *testing.T) {
	s := openTestStore(t)

	_, err := s.decrypt("not-valid-hex!")
	if err == nil {
		t.Error("expected error for invalid hex")
	}
}

func TestDecryptTooShort(t *testing.T) {
	s := openTestStore(t)

	_, err := s.decrypt("ab")
	if err == nil {
		t.Error("expected error for too-short ciphertext")
	}
}

// --- Account CRUD ---

func TestAddAndListAccounts(t *testing.T) {
	s := openTestStore(t)

	err := s.AddAccount("key1", "pt1", "user1", true, "JoyAI-Code")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	accounts, err := s.ListAccounts()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("len = %d, want 1", len(accounts))
	}
	if accounts[0].APIKey != "key1" {
		t.Errorf("APIKey = %q, want %q", accounts[0].APIKey, "key1")
	}
	if accounts[0].UserID != "user1" {
		t.Errorf("UserID = %q, want %q", accounts[0].UserID, "user1")
	}
	if !accounts[0].IsDefault {
		t.Error("expected IsDefault = true")
	}
	if accounts[0].DefaultModel != "JoyAI-Code" {
		t.Errorf("DefaultModel = %q, want %q", accounts[0].DefaultModel, "JoyAI-Code")
	}
}

func TestListAccountsEmpty(t *testing.T) {
	s := openTestStore(t)

	accounts, err := s.ListAccounts()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if accounts != nil {
		t.Errorf("expected nil for empty, got %v", accounts)
	}
}

func TestAddMultipleAccounts(t *testing.T) {
	s := openTestStore(t)

	s.AddAccount("key1", "pt1", "user1", true, "")
	s.AddAccount("key2", "pt2", "user2", false, "GLM-5.1")

	accounts, _ := s.ListAccounts()
	if len(accounts) != 2 {
		t.Fatalf("len = %d, want 2", len(accounts))
	}

	// Only one default
	defaultCount := 0
	for _, a := range accounts {
		if a.IsDefault {
			defaultCount++
		}
	}
	if defaultCount != 1 {
		t.Errorf("default count = %d, want 1", defaultCount)
	}
}

func TestAddAccountOverwrites(t *testing.T) {
	s := openTestStore(t)

	s.AddAccount("key1", "pt1", "user1", true, "")
	s.AddAccount("key1", "pt1-updated", "user1-new", false, "GLM-5.1")

	accounts, _ := s.ListAccounts()
	if len(accounts) != 1 {
		t.Fatalf("len = %d, want 1 (upsert)", len(accounts))
	}

	a, _ := s.GetAccount("key1")
	if a.UserID != "user1-new" {
		t.Errorf("UserID = %q, want %q", a.UserID, "user1-new")
	}
}

func TestGetAccount(t *testing.T) {
	s := openTestStore(t)

	s.AddAccount("key1", "secret-pt-key", "user1", true, "JoyAI-Code")

	a, err := s.GetAccount("key1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if a == nil {
		t.Fatal("expected account, got nil")
	}
	if a.PtKey != "secret-pt-key" {
		t.Errorf("PtKey = %q, want %q", a.PtKey, "secret-pt-key")
	}
	if a.APIKey != "key1" {
		t.Errorf("APIKey = %q, want %q", a.APIKey, "key1")
	}
}

func TestGetAccountNotFound(t *testing.T) {
	s := openTestStore(t)

	a, err := s.GetAccount("nonexistent")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if a != nil {
		t.Error("expected nil for nonexistent key")
	}
}

func TestRemoveAccount(t *testing.T) {
	s := openTestStore(t)

	s.AddAccount("key1", "pt1", "user1", true, "")
	s.RemoveAccount("key1")

	accounts, _ := s.ListAccounts()
	if len(accounts) != 0 {
		t.Errorf("len = %d, want 0 after remove", len(accounts))
	}
}

func TestRemoveNonexistent(t *testing.T) {
	s := openTestStore(t)

	err := s.RemoveAccount("nonexistent")
	if err != nil {
		t.Errorf("remove nonexistent should not error: %v", err)
	}
}

func TestSetDefault(t *testing.T) {
	s := openTestStore(t)

	s.AddAccount("key1", "pt1", "user1", true, "")
	s.AddAccount("key2", "pt2", "user2", false, "")

	s.SetDefault("key2")

	accounts, _ := s.ListAccounts()
	for _, a := range accounts {
		if a.APIKey == "key1" && a.IsDefault {
			t.Error("key1 should no longer be default")
		}
		if a.APIKey == "key2" && !a.IsDefault {
			t.Error("key2 should be default")
		}
	}
}

func TestUpdateAccountModel(t *testing.T) {
	s := openTestStore(t)

	s.AddAccount("key1", "pt1", "user1", true, "JoyAI-Code")
	s.UpdateAccountModel("key1", "GLM-5.1")

	a, _ := s.GetAccount("key1")
	if a.DefaultModel != "GLM-5.1" {
		t.Errorf("DefaultModel = %q, want %q", a.DefaultModel, "GLM-5.1")
	}
}

func TestGetDefaultAccount(t *testing.T) {
	s := openTestStore(t)

	s.AddAccount("key1", "pt1", "user1", false, "")
	s.AddAccount("key2", "pt2", "user2", true, "JoyAI-Code")

	a, err := s.GetDefaultAccount()
	if err != nil {
		t.Fatalf("get default: %v", err)
	}
	if a == nil {
		t.Fatal("expected default account, got nil")
	}
	if a.APIKey != "key2" {
		t.Errorf("default APIKey = %q, want %q", a.APIKey, "key2")
	}
}

func TestGetDefaultAccountNone(t *testing.T) {
	s := openTestStore(t)

	a, err := s.GetDefaultAccount()
	if err != nil {
		t.Fatalf("get default: %v", err)
	}
	if a != nil {
		t.Error("expected nil when no default account")
	}
}

// --- Settings ---

func TestGetSettingsEmpty(t *testing.T) {
	s := openTestStore(t)

	settings, err := s.GetSettings()
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if len(settings) != 0 {
		t.Errorf("expected empty settings, got %v", settings)
	}
}

func TestSetAndGetSetting(t *testing.T) {
	s := openTestStore(t)

	s.SetSetting("key1", "value1")
	s.SetSetting("key2", "value2")

	settings, _ := s.GetSettings()
	if settings["key1"] != "value1" {
		t.Errorf("key1 = %q, want %q", settings["key1"], "value1")
	}
	if settings["key2"] != "value2" {
		t.Errorf("key2 = %q, want %q", settings["key2"], "value2")
	}
}

func TestSetSettingsBatch(t *testing.T) {
	s := openTestStore(t)

	err := s.SetSettings(map[string]string{
		"a": "1",
		"b": "2",
	})
	if err != nil {
		t.Fatalf("set settings batch: %v", err)
	}

	settings, _ := s.GetSettings()
	if len(settings) != 2 {
		t.Errorf("len = %d, want 2", len(settings))
	}
}

func TestSetSettingOverwrite(t *testing.T) {
	s := openTestStore(t)

	s.SetSetting("key", "old")
	s.SetSetting("key", "new")

	settings, _ := s.GetSettings()
	if settings["key"] != "new" {
		t.Errorf("key = %q, want %q", settings["key"], "new")
	}
}

// --- Request Logging & Stats ---

func TestLogRequestAndGetStats(t *testing.T) {
	s := openTestStore(t)

	s.LogRequest("key1", "JoyAI-Code", "/v1/chat/completions", true, 200, 500, "")
	s.LogRequest("key1", "GLM-5.1", "/v1/chat/completions", false, 200, 300, "")
	s.LogRequest("key2", "JoyAI-Code", "/v1/messages", true, 200, 400, "")

	stats, err := s.GetStats()
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	if stats.TotalRequests != 3 {
		t.Errorf("TotalRequests = %d, want 3", stats.TotalRequests)
	}
	if stats.AccountsCount != 0 {
		t.Errorf("AccountsCount = %d, want 0 (no accounts added)", stats.AccountsCount)
	}
	if len(stats.ByModel) != 2 {
		t.Errorf("ByModel len = %d, want 2", len(stats.ByModel))
	}
	if len(stats.ByAccount) != 1 || stats.ByAccount[0].APIKey != "其他" {
		t.Errorf("ByAccount = %v, want [{其他 3}]", stats.ByAccount)
	}
}

func TestGetStatsEmpty(t *testing.T) {
	s := openTestStore(t)

	stats, err := s.GetStats()
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	if stats.TotalRequests != 0 {
		t.Errorf("TotalRequests = %d, want 0", stats.TotalRequests)
	}
}

func TestGetAccountStats(t *testing.T) {
	s := openTestStore(t)

	s.LogRequest("key1", "JoyAI-Code", "/v1/chat/completions", true, 200, 500, "")
	s.LogRequest("key1", "GLM-5.1", "/v1/messages", false, 200, 300, "")
	s.LogRequest("key1", "JoyAI-Code", "/v1/chat/completions", true, 500, 100, "")

	stats, err := s.GetAccountStats("key1")
	if err != nil {
		t.Fatalf("get account stats: %v", err)
	}
	if stats.TotalRequests != 3 {
		t.Errorf("TotalRequests = %d, want 3", stats.TotalRequests)
	}
	if stats.StreamCount != 2 {
		t.Errorf("StreamCount = %d, want 2", stats.StreamCount)
	}
	if stats.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1", stats.ErrorCount)
	}
	if len(stats.ByModel) != 2 {
		t.Errorf("ByModel len = %d, want 2", len(stats.ByModel))
	}
	if len(stats.ByEndpoint) != 2 {
		t.Errorf("ByEndpoint len = %d, want 2", len(stats.ByEndpoint))
	}
}

func TestGetAccountStatsEmpty(t *testing.T) {
	s := openTestStore(t)

	stats, err := s.GetAccountStats("nonexistent")
	if err != nil {
		t.Fatalf("get account stats: %v", err)
	}
	if stats.TotalRequests != 0 {
		t.Errorf("TotalRequests = %d, want 0", stats.TotalRequests)
	}
}

func TestGetRecentLogs(t *testing.T) {
	s := openTestStore(t)

	s.LogRequest("key1", "model1", "/v1/test", true, 200, 100, "")
	s.LogRequest("key2", "model2", "/v1/test", false, 200, 200, "")
	s.LogRequest("key1", "model3", "/v1/test", true, 200, 300, "")

	logs, err := s.GetRecentLogs(2)
	if err != nil {
		t.Fatalf("get recent logs: %v", err)
	}
	if len(logs) != 2 {
		t.Errorf("len = %d, want 2", len(logs))
	}
	// Most recent first
	if logs[0].Model != "model3" {
		t.Errorf("first log model = %q, want %q", logs[0].Model, "model3")
	}
}

func TestGetRecentLogsDefault(t *testing.T) {
	s := openTestStore(t)

	s.LogRequest("key1", "m1", "/v1", true, 200, 100, "")
	s.LogRequest("key1", "m2", "/v1", true, 200, 100, "")

	logs, err := s.GetRecentLogs(0)
	if err != nil {
		t.Fatalf("get recent logs: %v", err)
	}
	// Default limit = 100
	if len(logs) != 2 {
		t.Errorf("len = %d, want 2", len(logs))
	}
}

// --- Encryption key persistence ---

func TestEncryptionKeyReused(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Open twice with same dir — should reuse same encryption key
	s1, _ := Open(dbPath)
	enc1, _ := s1.encrypt("hello")
	s1.Close()

	s2, _ := Open(dbPath)
	dec2, err := s2.decrypt(enc1)
	s2.Close()

	if err != nil {
		t.Fatalf("decrypt with reopened store: %v", err)
	}
	if dec2 != "hello" {
		t.Errorf("decrypted = %q, want %q", dec2, "hello")
	}
}

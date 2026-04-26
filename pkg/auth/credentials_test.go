package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromSystem_DatabaseNotFound(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	_, err := LoadFromSystem()
	if err == nil {
		t.Error("expected error for missing database")
	}
}

func TestCredentials_Fields(t *testing.T) {
	creds := &Credentials{PtKey: "test-key", UserID: "test-user"}
	if creds.PtKey != "test-key" {
		t.Errorf("PtKey = %q, want test-key", creds.PtKey)
	}
	if creds.UserID != "test-user" {
		t.Errorf("UserID = %q, want test-user", creds.UserID)
	}
}

func TestLoadFromSystem_Integration(t *testing.T) {
	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, "Library", "Application Support",
		"JoyCode", "User", "globalStorage", "state.vscdb")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skip("JoyCode database not found, skipping integration test")
	}

	creds, err := LoadFromSystem()
	if err != nil {
		t.Logf("LoadFromSystem error: %v", err)
		return
	}
	if creds.PtKey == "" {
		t.Error("auto-detected PtKey should not be empty")
	}
	if creds.UserID == "" {
		t.Error("auto-detected UserID should not be empty")
	}
	t.Logf("Auto-detected credentials: userId=%s", creds.UserID)
}

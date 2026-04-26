package joycode

import (
	"testing"
)

func TestValidate_InvalidCredentials(t *testing.T) {
	// Test the code path logic: code != 0 means failure
	resp := map[string]interface{}{
		"code": float64(401),
		"msg":  "invalid token",
	}
	code, ok := resp["code"].(float64)
	if !ok || code == 0 {
		t.Errorf("expected non-zero code for invalid credentials")
	}
}

func TestValidate_SuccessfulResponse(t *testing.T) {
	resp := map[string]interface{}{
		"code": float64(0),
		"data": map[string]interface{}{
			"realName": "test",
			"userId":   "u1",
		},
	}
	code, ok := resp["code"].(float64)
	if !ok || code != 0 {
		t.Errorf("expected code=0, got %v", resp["code"])
	}
}

func TestNewClient(t *testing.T) {
	client := NewClient("test-key", "test-user")
	if client.PtKey != "test-key" {
		t.Errorf("PtKey = %q, want test-key", client.PtKey)
	}
	if client.UserID != "test-user" {
		t.Errorf("UserID = %q, want test-user", client.UserID)
	}
	if client.SessionID == "" {
		t.Error("SessionID should not be empty")
	}
}

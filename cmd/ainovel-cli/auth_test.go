package main

import (
	"path/filepath"
	"testing"

	"github.com/voocel/ainovel-cli/internal/provider/antigravity"
)

func TestRunAuthCommandUsageAndErrors(t *testing.T) {
	if code := runAuthCommand(nil); code != 2 {
		t.Errorf("runAuthCommand(nil) = %d, want 2", code)
	}
	if code := runAuthCommand([]string{"help"}); code != 0 {
		t.Errorf("runAuthCommand(help) = %d, want 0", code)
	}
	if code := runAuthCommand([]string{"--help"}); code != 0 {
		t.Errorf("runAuthCommand(--help) = %d, want 0", code)
	}
	if code := runAuthCommand([]string{"invalid"}); code != 2 {
		t.Errorf("runAuthCommand(invalid) = %d, want 2", code)
	}
	if code := runAuthCommand([]string{"login", "unsupported"}); code != 2 {
		t.Errorf("runAuthCommand(login unsupported) = %d, want 2", code)
	}
	if code := runAuthCommand([]string{"logout", "unsupported"}); code != 2 {
		t.Errorf("runAuthCommand(logout unsupported) = %d, want 2", code)
	}
}

func TestRunAuthCommandStatusAndLogout(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	// Status when not logged in
	if code := runAuthCommand([]string{"status"}); code != 0 {
		t.Errorf("runAuthCommand(status) unauthenticated = %d, want 0", code)
	}

	// Logout when not logged in
	if code := runAuthCommand([]string{"logout"}); code != 0 {
		t.Errorf("runAuthCommand(logout) unauthenticated = %d, want 0", code)
	}

	// Save dummy credentials
	dummyCreds := &antigravity.Credentials{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		ExpiresAt:    9999999999,
		ProjectID:    "test-project-123",
		Email:        "user@example.com",
	}
	if err := antigravity.SaveCredentials(dummyCreds); err != nil {
		t.Fatalf("save credentials: %v", err)
	}

	// Status when logged in
	if code := runAuthCommand([]string{"status"}); code != 0 {
		t.Errorf("runAuthCommand(status) authenticated = %d, want 0", code)
	}

	// Logout
	if code := runAuthCommand([]string{"logout"}); code != 0 {
		t.Errorf("runAuthCommand(logout) = %d, want 0", code)
	}

	// Verify file is gone
	authFile := filepath.Join(tempHome, ".ainovel", "antigravity_auth.json")
	if _, err := antigravity.LoadCredentials(); err == nil {
		t.Errorf("expected credentials file %s to be deleted after logout", authFile)
	}
}

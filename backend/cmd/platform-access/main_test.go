package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadPrivatePasswordFileRequiresOwnerOnlyPermissions(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "platform-admin-password")
	if err := os.WriteFile(filePath, []byte("long-enough-secret\n"), 0o644); err != nil {
		t.Fatalf("write password fixture: %v", err)
	}
	if _, err := readPrivatePasswordFile(filePath); err == nil {
		t.Fatal("world-readable password file was accepted")
	}
	if err := os.Chmod(filePath, 0o600); err != nil {
		t.Fatalf("secure password fixture: %v", err)
	}
	password, err := readPrivatePasswordFile(filePath)
	if err != nil {
		t.Fatalf("read private password file: %v", err)
	}
	if password != "long-enough-secret" {
		t.Fatalf("password=%q, want trailing line ending removed", password)
	}

	symlinkPath := filepath.Join(tempDir, "platform-admin-password-link")
	if err := os.Symlink(filePath, symlinkPath); err != nil {
		t.Fatalf("create password-file symlink: %v", err)
	}
	if _, err := readPrivatePasswordFile(symlinkPath); err == nil {
		t.Fatal("password-file symlink was accepted")
	}
}

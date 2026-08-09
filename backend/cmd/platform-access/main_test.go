package main

import (
	"encoding/json"
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

func TestWritePrivateJSONFileCreatesOwnerOnlyFileAndRefusesOverwrite(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "recovery-result.json")
	value := map[string]any{"status": "ready"}
	if err := writePrivateJSONFile(filePath, value); err != nil {
		t.Fatalf("write private JSON file: %v", err)
	}
	info, err := os.Lstat(filePath)
	if err != nil {
		t.Fatalf("inspect private JSON file: %v", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("output mode=%v, want regular 0600", info.Mode())
	}
	raw, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read private JSON file: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode private JSON: %v", err)
	}
	if decoded["status"] != "ready" {
		t.Fatalf("decoded value=%#v", decoded)
	}
	if err := writePrivateJSONFile(filePath, map[string]any{"status": "overwritten"}); err == nil {
		t.Fatal("existing output file was overwritten")
	}

	symlinkPath := filepath.Join(tempDir, "recovery-result-link.json")
	if err := os.Symlink(filePath, symlinkPath); err != nil {
		t.Fatalf("create output symlink: %v", err)
	}
	if err := writePrivateJSONFile(symlinkPath, value); err == nil {
		t.Fatal("output symlink was accepted")
	}
}

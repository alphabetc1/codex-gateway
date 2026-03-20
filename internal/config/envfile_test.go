package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadEnvFileParsesCommentsExportAndQuotes(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, ".env")
	content := "# comment\nexport DEST_SUFFIX_ALLOWLIST='.claude.com,.anthropic.com'\nDEST_HOST_ALLOWLIST=\"storage.googleapis.com\"\nAUTH_USERS_FILE=/tmp/users.txt\nEMPTY=\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	values, err := ReadEnvFile(path)
	if err != nil {
		t.Fatalf("ReadEnvFile() error = %v", err)
	}

	if got := values["DEST_SUFFIX_ALLOWLIST"]; got != ".claude.com,.anthropic.com" {
		t.Fatalf("DEST_SUFFIX_ALLOWLIST = %q", got)
	}
	if got := values["DEST_HOST_ALLOWLIST"]; got != "storage.googleapis.com" {
		t.Fatalf("DEST_HOST_ALLOWLIST = %q", got)
	}
	if got := values["AUTH_USERS_FILE"]; got != "/tmp/users.txt" {
		t.Fatalf("AUTH_USERS_FILE = %q", got)
	}
	if got := values["EMPTY"]; got != "" {
		t.Fatalf("EMPTY = %q, want empty", got)
	}
}

func TestLoadFromEnvFileOverlaysProcessEnv(t *testing.T) {
	t.Setenv("AUTH_USERS", "alice:hash-from-env")

	tempDir := t.TempDir()
	path := filepath.Join(tempDir, ".env")
	content := "DEST_SUFFIX_ALLOWLIST=.claude.com,.anthropic.com\nALLOW_PRIVATE_DESTINATIONS=true\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadFromEnvFile(path)
	if err != nil {
		t.Fatalf("LoadFromEnvFile() error = %v", err)
	}

	if got := cfg.AuthUsers; got != "alice:hash-from-env" {
		t.Fatalf("AuthUsers = %q", got)
	}
	if got := len(cfg.DestSuffixes); got != 2 {
		t.Fatalf("len(DestSuffixes) = %d, want 2", got)
	}
	if !cfg.AllowPrivateDestinations {
		t.Fatal("AllowPrivateDestinations = false, want true")
	}
}

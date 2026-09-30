package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "script.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

func TestRunSuccess(t *testing.T) {
	out, err := Run(writeScript(t, "echo hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) != "hello" {
		t.Errorf("output = %q, want %q", out, "hello")
	}
}

func TestRunFailure(t *testing.T) {
	out, err := Run(writeScript(t, "echo oops >&2; exit 3"))
	if err == nil || !strings.Contains(err.Error(), "deploy failed") {
		t.Fatalf("err = %v, want deploy failed", err)
	}
	if strings.TrimSpace(out) != "oops" {
		t.Errorf("output = %q, want stderr captured", out)
	}
}

func TestRunMissingScript(t *testing.T) {
	if _, err := Run(filepath.Join(t.TempDir(), "missing.sh")); err == nil {
		t.Fatal("expected error for missing script")
	}
}

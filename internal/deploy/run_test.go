package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
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
	out, err := Run(context.Background(), writeScript(t, "echo hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) != "hello" {
		t.Errorf("output = %q, want %q", out, "hello")
	}
}

func TestRunFailure(t *testing.T) {
	out, err := Run(context.Background(), writeScript(t, "echo oops >&2; exit 3"))
	if err == nil || !strings.Contains(err.Error(), "deploy failed") {
		t.Fatalf("err = %v, want deploy failed", err)
	}
	if strings.TrimSpace(out) != "oops" {
		t.Errorf("output = %q, want stderr captured", out)
	}
}

func TestRunMissingScript(t *testing.T) {
	if _, err := Run(context.Background(), filepath.Join(t.TempDir(), "missing.sh")); err == nil {
		t.Fatal("expected error for missing script")
	}
}

func TestRunCanceledKillsProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	// The background child inherits stdout, so Run would hang if only the shell were killed.
	script := writeScript(t, "sleep 30 &\necho $! > "+pidFile+"\nwait")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := Run(ctx, script)
		done <- err
	}()

	var childPID int
	deadline := time.Now().Add(time.Second)
	for childPID == 0 && time.Now().Before(deadline) {
		if b, err := os.ReadFile(pidFile); err == nil {
			childPID, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childPID == 0 {
		t.Fatal("child process did not start")
	}

	cancel()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "deploy canceled") {
			t.Fatalf("err = %v, want deploy canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}

	deadline = time.Now().Add(time.Second)
	for syscall.Kill(childPID, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("child process %d still alive after cancel", childPID)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

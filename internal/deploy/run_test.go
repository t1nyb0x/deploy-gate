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

var testRunner = Runner{MaxOutput: 1 << 20}

func TestRunSuccess(t *testing.T) {
	out, err := testRunner.Run(context.Background(), writeScript(t, "echo hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) != "hello" {
		t.Errorf("output = %q, want %q", out, "hello")
	}
}

func TestRunFailure(t *testing.T) {
	out, err := testRunner.Run(context.Background(), writeScript(t, "echo oops >&2; exit 3"))
	if err == nil || !strings.Contains(err.Error(), "deploy failed") {
		t.Fatalf("err = %v, want deploy failed", err)
	}
	if strings.TrimSpace(out) != "oops" {
		t.Errorf("output = %q, want stderr captured", out)
	}
}

func TestRunMissingScript(t *testing.T) {
	if _, err := testRunner.Run(context.Background(), filepath.Join(t.TempDir(), "missing.sh")); err == nil {
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
		_, err := testRunner.Run(ctx, script)
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

func TestRunTruncatesOutputToTail(t *testing.T) {
	r := Runner{MaxOutput: 10}
	out, err := r.Run(context.Background(), writeScript(t, "printf 'aaaaaaaaaa'; printf 'bbbbbbbbbb'"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "[... 10 bytes truncated ...]\nbbbbbbbbbb"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRunDiscardsOutputWhenMaxOutputIsZero(t *testing.T) {
	r := Runner{MaxOutput: 0}
	out, err := r.Run(context.Background(), writeScript(t, "echo hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "" {
		t.Errorf("output = %q, want empty", out)
	}
}

func TestRunDropsConfiguredEnv(t *testing.T) {
	t.Setenv("DEPLOY_SECRET", "super-secret-value")
	t.Setenv("DEPLOY_KEEP_ME", "kept")

	r := Runner{MaxOutput: 1 << 10, DropEnv: []string{"DEPLOY_SECRET"}}
	out, err := r.Run(context.Background(), writeScript(t, `echo "${DEPLOY_SECRET:-unset} ${DEPLOY_KEEP_ME:-unset}"`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) != "unset kept" {
		t.Errorf("output = %q, want %q", out, "unset kept")
	}
}

func TestRunRedactsValues(t *testing.T) {
	r := Runner{MaxOutput: 1 << 10, Redact: []string{"super-secret-value", ""}}
	out, err := r.Run(context.Background(), writeScript(t, "echo token=super-secret-value; echo done"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "super-secret-value") {
		t.Fatalf("secret leaked in output: %q", out)
	}
	if want := "token=[REDACTED]\ndone\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestTailBuffer(t *testing.T) {
	tests := []struct {
		name        string
		max         int
		writes      []string
		wantKept    string
		wantDropped int64
	}{
		{name: "under limit", max: 10, writes: []string{"abc", "def"}, wantKept: "abcdef"},
		{name: "exactly limit", max: 6, writes: []string{"abc", "def"}, wantKept: "abcdef"},
		{name: "over limit across writes", max: 4, writes: []string{"abc", "def"}, wantKept: "cdef", wantDropped: 2},
		{name: "single large write", max: 3, writes: []string{"abcdefgh"}, wantKept: "fgh", wantDropped: 5},
		{name: "many small writes", max: 2, writes: []string{"a", "b", "c", "d", "e"}, wantKept: "de", wantDropped: 3},
		{name: "zero keeps nothing", max: 0, writes: []string{"abc"}, wantKept: "", wantDropped: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &tailBuffer{max: tt.max}
			for _, w := range tt.writes {
				n, err := b.Write([]byte(w))
				if err != nil || n != len(w) {
					t.Fatalf("Write(%q) = %d, %v", w, n, err)
				}
			}
			if got := string(b.buf); got != tt.wantKept {
				t.Errorf("kept = %q, want %q", got, tt.wantKept)
			}
			if b.dropped != tt.wantDropped {
				t.Errorf("dropped = %d, want %d", b.dropped, tt.wantDropped)
			}
			if cap(b.buf) > tt.max+len("abcdefgh") {
				t.Errorf("buffer grew unbounded: cap=%d", cap(b.buf))
			}
		})
	}
}

func TestTailBufferStringDropsPartialFirstLine(t *testing.T) {
	tests := []struct {
		name   string
		max    int
		writes []string
		want   string
	}{
		{name: "no truncation keeps everything", max: 100, writes: []string{"one\ntwo\n"}, want: "one\ntwo\n"},
		{name: "partial first line dropped", max: 8, writes: []string{"4975\n4976\n4977\n"}, want: "[... 10 bytes truncated ...]\n4977\n"},
		{name: "cut exactly at line start keeps line", max: 10, writes: []string{"4975\n4976\n4977\n"}, want: "[... 5 bytes truncated ...]\n4976\n4977\n"},
		{name: "no newline in tail keeps tail", max: 4, writes: []string{"abcdefgh"}, want: "[... 4 bytes truncated ...]\nefgh"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &tailBuffer{max: tt.max}
			for _, w := range tt.writes {
				_, _ = b.Write([]byte(w))
			}
			if got := b.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

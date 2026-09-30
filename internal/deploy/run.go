package deploy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"time"
)

const (
	timeout = 5 * time.Minute
	// waitDelay bounds how long Run waits for output pipes to close after the script is killed.
	waitDelay = 5 * time.Second
)

// Runner runs deploy scripts.
type Runner struct {
	// MaxOutput is the number of trailing output bytes kept. Zero discards all output.
	MaxOutput int
	// DropEnv lists environment variable names that are not passed to the script.
	DropEnv []string
	// Redact lists values replaced with [REDACTED] in the returned output.
	Redact []string
}

func (r Runner) Run(ctx context.Context, script string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out := &tailBuffer{max: r.MaxOutput}

	cmd := exec.CommandContext(ctx, script)
	cmd.Env = r.env()
	// The same comparable writer for both streams keeps Write calls serialized.
	cmd.Stdout = out
	cmd.Stderr = out
	// Run the script in its own process group so that cancellation kills its children too,
	// and so that terminal signals (Ctrl-C) sent to deploy-gate do not reach the script directly.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = waitDelay

	err := cmd.Run()
	output := r.redact(out.String())
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return output, fmt.Errorf("deploy timed out")
	case errors.Is(ctx.Err(), context.Canceled):
		return output, fmt.Errorf("deploy canceled")
	case err != nil:
		return output, fmt.Errorf("deploy failed: %w", err)
	}

	return output, nil
}

func (r Runner) env() []string {
	return slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return slices.Contains(r.DropEnv, name)
	})
}

func (r Runner) redact(s string) string {
	for _, v := range r.Redact {
		if v != "" {
			s = strings.ReplaceAll(s, v, "[REDACTED]")
		}
	}
	return s
}

// tailBuffer keeps only the last max bytes written to it.
type tailBuffer struct {
	max     int
	buf     []byte
	dropped int64
	// lastDropped is the most recently dropped byte, used to tell whether
	// the kept tail starts at a line boundary.
	lastDropped byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if n >= b.max {
		switch {
		case n > b.max:
			b.lastDropped = p[n-b.max-1]
		case len(b.buf) > 0:
			b.lastDropped = b.buf[len(b.buf)-1]
		}
		b.dropped += int64(len(b.buf) + n - b.max)
		b.buf = append(b.buf[:0], p[n-b.max:]...)
		return n, nil
	}

	if overflow := len(b.buf) + n - b.max; overflow > 0 {
		b.lastDropped = b.buf[overflow-1]
		b.dropped += int64(overflow)
		b.buf = b.buf[:copy(b.buf, b.buf[overflow:])]
	}
	b.buf = append(b.buf, p...)
	return n, nil
}

func (b *tailBuffer) String() string {
	if b.max == 0 {
		return ""
	}
	if b.dropped == 0 {
		return string(b.buf)
	}

	kept, dropped := b.buf, b.dropped
	// Drop the partial first line left by truncation.
	if b.lastDropped != '\n' {
		if i := bytes.IndexByte(kept, '\n'); i >= 0 {
			kept, dropped = kept[i+1:], dropped+int64(i+1)
		}
	}
	// Truncation may still split a multi-byte character when no newline was found.
	return fmt.Sprintf("[... %d bytes truncated ...]\n%s", dropped, strings.ToValidUTF8(string(kept), ""))
}

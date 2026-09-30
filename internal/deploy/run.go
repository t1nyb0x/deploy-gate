package deploy

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

const (
	timeout = 5 * time.Minute
	// waitDelay bounds how long Run waits for output pipes to close after the script is killed.
	waitDelay = 5 * time.Second
)

func Run(ctx context.Context, script string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, script)
	// Run the script in its own process group so that cancellation kills its children too,
	// and so that terminal signals (Ctrl-C) sent to deploy-gate do not reach the script directly.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = waitDelay

	output, err := cmd.CombinedOutput()
	out := string(output)
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return out, fmt.Errorf("deploy timed out")
	case errors.Is(ctx.Err(), context.Canceled):
		return out, fmt.Errorf("deploy canceled")
	case err != nil:
		return out, fmt.Errorf("deploy failed: %w", err)
	}

	return out, nil
}

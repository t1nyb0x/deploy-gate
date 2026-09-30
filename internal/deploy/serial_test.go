package deploy

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// blockingRunner blocks each run until release is signaled.
type blockingRunner struct {
	started chan struct{}
	release chan struct{}

	calls   atomic.Int32
	active  atomic.Int32
	maxSeen atomic.Int32
}

func newBlockingRunner() *blockingRunner {
	return &blockingRunner{
		started: make(chan struct{}, 10),
		release: make(chan struct{}),
	}
}

func (b *blockingRunner) run(ctx context.Context, _ string) (string, error) {
	b.calls.Add(1)
	n := b.active.Add(1)
	for {
		m := b.maxSeen.Load()
		if n <= m || b.maxSeen.CompareAndSwap(m, n) {
			break
		}
	}
	defer b.active.Add(-1)
	b.started <- struct{}{}
	select {
	case <-b.release:
		return "", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func waitStarted(t *testing.T, b *blockingRunner) {
	t.Helper()
	select {
	case <-b.started:
	case <-time.After(time.Second):
		t.Fatal("expected run to start")
	}
}

func assertNotStarted(t *testing.T, b *blockingRunner) {
	t.Helper()
	select {
	case <-b.started:
		t.Fatal("unexpected run started")
	case <-time.After(50 * time.Millisecond):
	}
}

func mustTrigger(t *testing.T, s *Serial) {
	t.Helper()
	if _, err := s.Trigger(); err != nil {
		t.Fatalf("trigger: %v", err)
	}
}

func shutdownAsync(s *Serial, ctx context.Context) <-chan error {
	done := make(chan error, 1)
	go func() { done <- s.Shutdown(ctx) }()
	return done
}

// waitClosed blocks until Shutdown has marked s closed.
// Trigger before that point only re-sets pending, which Shutdown then drops.
func waitClosed(t *testing.T, s *Serial) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := s.Trigger(); errors.Is(err, ErrClosed) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("serial was not closed")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSerialRunsOnce(t *testing.T) {
	b := newBlockingRunner()
	s := NewSerial("/scripts/a.sh", b.run)

	if queued, err := s.Trigger(); err != nil || queued {
		t.Fatalf("first trigger should start immediately: queued=%v err=%v", queued, err)
	}
	waitStarted(t, b)
	b.release <- struct{}{}
	s.Wait()

	if got := b.calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

func TestSerialCoalescesTriggersWhileRunning(t *testing.T) {
	b := newBlockingRunner()
	s := NewSerial("/scripts/a.sh", b.run)

	mustTrigger(t, s)
	waitStarted(t, b)

	for i := 0; i < 5; i++ {
		if queued, err := s.Trigger(); err != nil || !queued {
			t.Fatalf("trigger %d while running should be queued: queued=%v err=%v", i, queued, err)
		}
	}
	assertNotStarted(t, b)

	b.release <- struct{}{}
	waitStarted(t, b)
	b.release <- struct{}{}
	s.Wait()

	if got := b.calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2 (one run + one coalesced follow-up)", got)
	}
}

func TestSerialNeverRunsConcurrently(t *testing.T) {
	b := newBlockingRunner()
	s := NewSerial("/scripts/a.sh", b.run)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.Trigger()
		}()
	}
	wg.Wait()

	go func() {
		for {
			select {
			case <-b.started:
				b.release <- struct{}{}
			case <-time.After(200 * time.Millisecond):
				return
			}
		}
	}()
	s.Wait()

	if got := b.maxSeen.Load(); got != 1 {
		t.Fatalf("max concurrent runs = %d, want 1", got)
	}
	if got := b.calls.Load(); got < 1 || got > 2 {
		t.Fatalf("calls = %d, want 1 or 2", got)
	}
}

func TestSerialRunsAgainAfterIdle(t *testing.T) {
	b := newBlockingRunner()
	s := NewSerial("/scripts/a.sh", b.run)

	mustTrigger(t, s)
	waitStarted(t, b)
	b.release <- struct{}{}
	s.Wait()

	if queued, err := s.Trigger(); err != nil || queued {
		t.Fatalf("trigger after idle should start immediately: queued=%v err=%v", queued, err)
	}
	waitStarted(t, b)
	b.release <- struct{}{}
	s.Wait()

	if got := b.calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2", got)
	}
}

func TestSerialShutdownWhenIdle(t *testing.T) {
	s := NewSerial("/scripts/a.sh", newBlockingRunner().run)

	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestSerialShutdownWaitsForRunningDeploy(t *testing.T) {
	b := newBlockingRunner()
	s := NewSerial("/scripts/a.sh", b.run)

	mustTrigger(t, s)
	waitStarted(t, b)

	done := shutdownAsync(s, context.Background())
	select {
	case err := <-done:
		t.Fatalf("shutdown returned before deploy finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	b.release <- struct{}{}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not return after deploy finished")
	}
}

func TestSerialShutdownDropsPendingRun(t *testing.T) {
	b := newBlockingRunner()
	s := NewSerial("/scripts/a.sh", b.run)

	mustTrigger(t, s)
	waitStarted(t, b)
	if queued, _ := s.Trigger(); !queued {
		t.Fatal("expected trigger to be queued")
	}

	done := shutdownAsync(s, context.Background())
	waitClosed(t, s)
	b.release <- struct{}{}
	if err := <-done; err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	assertNotStarted(t, b)

	if got := b.calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1 (pending run must be dropped)", got)
	}
}

func TestSerialShutdownCancelsRunOnDeadline(t *testing.T) {
	b := newBlockingRunner()
	s := NewSerial("/scripts/a.sh", b.run)

	mustTrigger(t, s)
	waitStarted(t, b)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	select {
	case err := <-shutdownAsync(s, ctx):
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want %v", err, context.DeadlineExceeded)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not return after deadline")
	}

	if got := b.active.Load(); got != 0 {
		t.Fatalf("active runs = %d, want 0 (run must be canceled)", got)
	}
}

func TestSerialTriggerAfterShutdown(t *testing.T) {
	b := newBlockingRunner()
	s := NewSerial("/scripts/a.sh", b.run)

	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if _, err := s.Trigger(); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want %v", err, ErrClosed)
	}
	assertNotStarted(t, b)
}

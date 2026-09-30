package deploy

import (
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

func (b *blockingRunner) run(string) (string, error) {
	b.calls.Add(1)
	n := b.active.Add(1)
	for {
		m := b.maxSeen.Load()
		if n <= m || b.maxSeen.CompareAndSwap(m, n) {
			break
		}
	}
	b.started <- struct{}{}
	<-b.release
	b.active.Add(-1)
	return "", nil
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

func TestSerialRunsOnce(t *testing.T) {
	b := newBlockingRunner()
	s := NewSerial("/scripts/a.sh", b.run)

	if queued := s.Trigger(); queued {
		t.Fatal("first trigger should start immediately")
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

	s.Trigger()
	waitStarted(t, b)

	for i := 0; i < 5; i++ {
		if queued := s.Trigger(); !queued {
			t.Fatalf("trigger %d while running should be queued", i)
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
			s.Trigger()
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

	s.Trigger()
	waitStarted(t, b)
	b.release <- struct{}{}
	s.Wait()

	if queued := s.Trigger(); queued {
		t.Fatal("trigger after idle should start immediately")
	}
	waitStarted(t, b)
	b.release <- struct{}{}
	s.Wait()

	if got := b.calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2", got)
	}
}

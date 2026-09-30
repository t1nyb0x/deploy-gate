package deploy

import (
	"context"
	"errors"
	"log"
	"sync"
)

// ErrClosed is returned by Trigger after Shutdown has been called.
var ErrClosed = errors.New("deploy: serial is shut down")

// Serial runs a script at most one at a time.
// Triggers that arrive while a run is in progress are coalesced into
// a single follow-up run, so the latest push is always deployed.
type Serial struct {
	script string
	run    func(ctx context.Context, script string) (string, error)

	// ctx is canceled to kill a running deploy when Shutdown exceeds its deadline.
	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	running bool
	pending bool
	closed  bool
	wg      sync.WaitGroup
}

func NewSerial(script string, run func(ctx context.Context, script string) (string, error)) *Serial {
	ctx, cancel := context.WithCancel(context.Background())
	return &Serial{script: script, run: run, ctx: ctx, cancel: cancel}
}

// Trigger requests a run. It returns true when the request was queued
// behind a run in progress, and false when a run started immediately.
// It returns ErrClosed after Shutdown has been called.
func (s *Serial) Trigger() (queued bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return false, ErrClosed
	}

	if s.running {
		s.pending = true
		return true, nil
	}

	s.running = true
	s.wg.Add(1)
	go s.loop()
	return false, nil
}

// Wait blocks until no run is in progress or pending.
func (s *Serial) Wait() {
	s.wg.Wait()
}

// Shutdown stops accepting triggers, drops any pending run, and waits for
// the run in progress to finish. If ctx is done first, the running script
// is killed and ctx's error is returned.
func (s *Serial) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	if s.pending {
		log.Printf("warning: dropping pending deploy on shutdown: script=%s", s.script)
		s.pending = false
	}
	s.mu.Unlock()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.cancel()
		return nil
	case <-ctx.Done():
		log.Printf("warning: killing deploy after shutdown timeout: script=%s", s.script)
		s.cancel()
		<-done
		return ctx.Err()
	}
}

func (s *Serial) loop() {
	defer s.wg.Done()

	for {
		output, err := s.run(s.ctx, s.script)
		if err != nil {
			log.Printf("deploy failed: script=%s error=%v output=%s", s.script, err, output)
		} else {
			log.Printf("deploy succeeded: script=%s output=%s", s.script, output)
		}

		s.mu.Lock()
		if !s.pending {
			s.running = false
			s.mu.Unlock()
			return
		}
		s.pending = false
		s.mu.Unlock()
		log.Printf("deploy rerun for coalesced request: script=%s", s.script)
	}
}

package deploy

import (
	"log"
	"sync"
)

// Serial runs a script at most one at a time.
// Triggers that arrive while a run is in progress are coalesced into
// a single follow-up run, so the latest push is always deployed.
type Serial struct {
	script string
	run    func(script string) (string, error)

	mu      sync.Mutex
	running bool
	pending bool
	wg      sync.WaitGroup
}

func NewSerial(script string, run func(script string) (string, error)) *Serial {
	return &Serial{script: script, run: run}
}

// Trigger requests a run. It returns true when the request was queued
// behind a run in progress, and false when a run started immediately.
func (s *Serial) Trigger() (queued bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		s.pending = true
		return true
	}

	s.running = true
	s.wg.Add(1)
	go s.loop()
	return false
}

// Wait blocks until no run is in progress or pending.
func (s *Serial) Wait() {
	s.wg.Wait()
}

func (s *Serial) loop() {
	defer s.wg.Done()

	for {
		output, err := s.run(s.script)
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

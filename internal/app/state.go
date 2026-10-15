package app

import (
	"context"
	"errors"
	"sync"

	"github.com/MasFana/cftui/internal/cf"
)

type State struct {
	Tunnels  []cf.Tunnel
	Selected string
	Routes   []cf.Route
	Changed  bool
	LastSync string
}

var ErrNothingToApply = errors.New("app: no pending changes to apply")

type PendingChange struct {
	Kind  string
	Route cf.Route
	Index int
}

// Staging accumulates changes locally until the user applies them. Route edits
// must never hit Cloudflare as they are typed, so every mutation is recorded
// here first and applied in one write.
type Staging struct {
	mu      sync.Mutex
	changes []PendingChange
}

func (s *Staging) Stage(c PendingChange) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.changes = append(s.changes, c)
}

func (s *Staging) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.changes)
}

func (s *Staging) Discard() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.changes = nil
}

func (s *Staging) Pending() []PendingChange {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PendingChange, len(s.changes))
	copy(out, s.changes)
	return out
}

func (s *Staging) Apply(ctx context.Context, backend cf.Backend, tunnelID string, routes []cf.Route) error {
	if s.Len() == 0 {
		return ErrNothingToApply
	}
	if err := backend.PutRoutes(ctx, tunnelID, routes); err != nil {
		return err
	}
	s.Discard()
	return nil
}

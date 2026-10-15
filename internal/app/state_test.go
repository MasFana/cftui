package app

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/masfana/cftui/internal/cf"
)

func TestStagingLifecycle(t *testing.T) {
	var s Staging
	if s.Len() != 0 {
		t.Fatal("new staging must be empty")
	}
	s.Stage(PendingChange{Kind: "add", Route: cf.Route{Hostname: "a.example.com"}})
	s.Stage(PendingChange{Kind: "delete", Index: 0})
	if s.Len() != 2 {
		t.Fatalf("want 2 staged, got %d", s.Len())
	}
	got := s.Pending()
	if len(got) != 2 || got[0].Kind != "add" || got[1].Kind != "delete" {
		t.Fatalf("pending order must be insertion order: %+v", got)
	}
	s.Discard()
	if s.Len() != 0 {
		t.Fatal("discard must clear everything")
	}
}

func TestPendingReturnsCopy(t *testing.T) {
	var s Staging
	s.Stage(PendingChange{Kind: "add"})
	got := s.Pending()
	got[0].Kind = "mutated"
	if s.Pending()[0].Kind != "add" {
		t.Fatal("Pending must not expose internal storage to callers")
	}
}

type fakeBackend struct {
	put  int
	last []cf.Route
	err  error
}

func (f *fakeBackend) ListTunnels(context.Context) ([]cf.Tunnel, error) { return nil, nil }
func (f *fakeBackend) GetRoutes(context.Context, string) ([]cf.Route, error) {
	return nil, nil
}
func (f *fakeBackend) PutRoutes(_ context.Context, _ string, r []cf.Route) error {
	f.put++
	f.last = r
	if f.err != nil {
		return fmt.Errorf("put routes: %w", f.err)
	}
	return nil
}
func (f *fakeBackend) SyncDNS(context.Context, string, []cf.Route) ([]cf.DNSChange, error) {
	return nil, nil
}
func (f *fakeBackend) Health(context.Context, string) ([]cf.Connector, error) { return nil, nil }

func TestApplyRefusesWhenClean(t *testing.T) {
	var s Staging
	f := &fakeBackend{}
	if err := s.Apply(context.Background(), f, "t1", nil); err != ErrNothingToApply {
		t.Fatalf("want ErrNothingToApply, got %v", err)
	}
	if f.put != 0 {
		t.Fatal("a clean apply must not call the backend")
	}
}

func TestApplyWritesAndClears(t *testing.T) {
	var s Staging
	s.Stage(PendingChange{Kind: "add", Route: cf.Route{Hostname: "a.example.com"}})
	f := &fakeBackend{}
	routes := []cf.Route{{Hostname: "a.example.com", Service: "http://localhost:3000"}}
	if err := s.Apply(context.Background(), f, "t1", routes); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if f.put != 1 {
		t.Fatalf("want 1 write, got %d", f.put)
	}
	if s.Len() != 0 {
		t.Fatal("apply must clear staged changes")
	}
}

func TestApplyKeepsChangesOnFailure(t *testing.T) {
	var s Staging
	s.Stage(PendingChange{Kind: "add"})
	f := &fakeBackend{err: errors.New("boom")}
	if err := s.Apply(context.Background(), f, "t1", nil); err == nil {
		t.Fatal("want error")
	}
	if s.Len() != 1 {
		t.Fatal("a failed apply must not discard the user's staged work")
	}
}

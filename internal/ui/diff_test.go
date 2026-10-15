package ui

import (
	"testing"

	"github.com/MasFana/cftui/internal/cf"
)

func TestDiffRoutesRendersAddRemove(t *testing.T) {
	old := []cf.Route{{Hostname: "old.example.com", Service: "http://localhost:3000"}}
	new := []cf.Route{{Hostname: "new.example.com", Service: "http://localhost:4000"}}
	got := DiffRoutes(old, new)
	if len(got) != 2 || got[0].Sign != '-' || got[1].Sign != '+' {
		t.Fatalf("diff = %+v", got)
	}
}

func TestChangedDetectsEquality(t *testing.T) {
	routes := []cf.Route{{Hostname: "app.example.com", Service: "http://localhost:3000"}}
	if Changed(routes, append([]cf.Route(nil), routes...)) {
		t.Fatal("equal routes must be clean")
	}
	if !Changed(routes, []cf.Route{{Hostname: "app.example.com", Service: "http://localhost:4000"}}) {
		t.Fatal("different routes must be dirty")
	}
}

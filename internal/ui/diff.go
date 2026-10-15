package ui

import (
	"fmt"

	"github.com/MasFana/cftui/internal/cf"
)

// Line is one rendered diff row.
type Line struct {
	Sign byte
	Text string
}

// Changed reports whether the two route lists differ.
func Changed(committed, staged []cf.Route) bool {
	if len(committed) != len(staged) {
		return true
	}
	for i := range committed {
		if committed[i] != staged[i] {
			return true
		}
	}
	return false
}

// DiffRoutes renders a unified, order-aware diff between committed and staged routes.
func DiffRoutes(committed, staged []cf.Route) []Line {
	if !Changed(committed, staged) {
		return nil
	}
	out := make([]Line, 0, len(committed)+len(staged))
	for _, r := range committed {
		out = append(out, Line{Sign: '-', Text: routeText(r)})
	}
	for _, r := range staged {
		out = append(out, Line{Sign: '+', Text: routeText(r)})
	}
	return out
}

func routeText(r cf.Route) string {
	host := r.Hostname
	if cf.IsCatchAll(r) {
		host = "(catch-all)"
	}
	return fmt.Sprintf("%s -> %s", host, r.Service)
}

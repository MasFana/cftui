package cf

import "strings"

const DefaultCatchAll = "http_status:404"

func IsCatchAll(r Route) bool { return strings.TrimSpace(r.Hostname) == "" }

// EnsureCatchAllLast strips every catch-all and appends exactly one at the end,
// preserving user rule order. The Cloudflare config API replaces the whole
// ingress list, so dropping the trailing catch-all would black-hole traffic.
func EnsureCatchAllLast(routes []Route) []Route {
	out := make([]Route, 0, len(routes)+1)
	last := Route{Service: DefaultCatchAll}
	found := false
	for _, r := range routes {
		if IsCatchAll(r) {
			if !found && strings.TrimSpace(r.Service) != "" {
				last = r
			}
			found = true
			continue
		}
		out = append(out, r)
	}
	return append(out, last)
}

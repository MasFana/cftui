package cf

import (
	"sort"
	"strings"
)

// DNSAction names what cftui intends to do with one hostname's DNS record.
type DNSAction string

const (
	// DNSCreate means no record exists yet; cftui will create a proxied CNAME
	// pointing at the tunnel.
	DNSCreate DNSAction = "create"
	// DNSUpdate means a record exists but points somewhere else; cftui will
	// repoint it at this tunnel.
	DNSUpdate DNSAction = "update"
	// DNSKeep means an existing record is not managed by cftui, so nothing is
	// changed.
	DNSKeep DNSAction = "keep"
	// DNSOrphan means a record cftui created for this tunnel no longer has a
	// matching ingress rule and may be deleted after confirmation.
	DNSOrphan DNSAction = "orphan"
)

// DNSPlan is one row of the DNS sync plan: what cftui found, what it wants to
// do, and why.
type DNSPlan struct {
	Hostname string
	Action   DNSAction
	Reason   string
	Current  string
	Desired  string
}

// DNSRecord is the shape PlanDNS needs from a zone.
type DNSRecord struct {
	Name         string
	Type         string
	Content      string
	Proxied      bool
	OwnerTunnel  string
	OwnerComment string
}

const cftuiOwnerMarker = "managed-by:cftui"

// Owns reports whether this record was created by cftui for tunnelID.
func (r DNSRecord) Owns(tunnelID string) bool {
	if tunnelID == "" {
		return false
	}
	return r.OwnerTunnel == tunnelID || r.OwnerComment == cftuiOwnerMarker
}

// PlanDNS compares the tunnel's desired hostnames against a zone's existing
// records and returns the changes needed, followed by the orphaned records.
//
// It is pure: no network, no filesystem, no globals, and its output does not
// depend on map iteration order, so identical inputs always produce identical
// output.
func PlanDNS(tunnelID, tunnelCNAME string, routes []Route, existing []DNSRecord) []DNSPlan {
	desired := make([]string, 0, len(routes))
	seen := make(map[string]bool, len(routes))
	for _, r := range routes {
		h := strings.ToLower(strings.TrimSpace(r.Hostname))
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		desired = append(desired, h)
	}

	byName := make(map[string]DNSRecord, len(existing))
	for _, rec := range existing {
		name := strings.ToLower(strings.TrimSpace(rec.Name))
		if name == "" {
			continue
		}
		if _, dup := byName[name]; !dup {
			byName[name] = rec
		}
	}

	plans := make([]DNSPlan, 0, len(desired))
	for _, h := range desired {
		rec, found := byName[h]
		switch {
		case !found:
			plans = append(plans, DNSPlan{
				Hostname: h,
				Action:   DNSCreate,
				Reason:   "no DNS record exists for this hostname",
				Desired:  tunnelCNAME,
			})
		case !rec.Owns(tunnelID):
			plans = append(plans, DNSPlan{
				Hostname: h,
				Action:   DNSKeep,
				Reason:   "existing record is not managed by cftui; left unchanged",
				Current:  rec.Content,
				Desired:  tunnelCNAME,
			})
		case !strings.EqualFold(rec.Content, tunnelCNAME):
			plans = append(plans, DNSPlan{
				Hostname: h,
				Action:   DNSUpdate,
				Reason:   "record points at a different target",
				Current:  rec.Content,
				Desired:  tunnelCNAME,
			})
		}
	}

	orphans := make([]DNSPlan, 0)
	for _, rec := range existing {
		name := strings.ToLower(strings.TrimSpace(rec.Name))
		if name == "" || seen[name] || !rec.Owns(tunnelID) {
			continue
		}
		orphans = append(orphans, DNSPlan{
			Hostname: name,
			Action:   DNSOrphan,
			Reason:   "record points at this tunnel but no ingress rule serves it",
			Current:  rec.Content,
		})
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].Hostname < orphans[j].Hostname })

	return append(plans, orphans...)
}

// Orphans returns only the rows that need explicit confirmation before removal.
func Orphans(plans []DNSPlan) []DNSPlan {
	out := make([]DNSPlan, 0)
	for _, p := range plans {
		if p.Action == DNSOrphan {
			out = append(out, p)
		}
	}
	return out
}

// DNSChanges converts a plan into the DNSChange list used by the backend and UI.
func DNSChanges(plans []DNSPlan) []DNSChange {
	out := make([]DNSChange, 0, len(plans))
	for _, p := range plans {
		out = append(out, DNSChange{Hostname: p.Hostname, Action: string(p.Action)})
	}
	return out
}

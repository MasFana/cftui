package cf

import (
	"reflect"
	"testing"
)

const qaTunnelID = "aaaa1111-bbbb-2222-cccc-3333dddd4444"
const qaCNAME = qaTunnelID + ".cfargotunnel.com"

func TestPlanDNSCreatesMissingRecords(t *testing.T) {
	routes := []Route{
		{Hostname: "app.example.com", Service: "http://localhost:3000"},
		{Service: "http_status:404"},
	}
	plans := PlanDNS(qaTunnelID, qaCNAME, routes, nil)

	if len(plans) != 1 {
		t.Fatalf("got %d plans, want 1 (catch-all has no hostname): %+v", len(plans), plans)
	}
	if plans[0].Hostname != "app.example.com" || plans[0].Action != DNSCreate {
		t.Fatalf("unexpected plan: %+v", plans[0])
	}
	if plans[0].Desired != qaCNAME {
		t.Errorf("Desired = %q, want the tunnel CNAME %q", plans[0].Desired, qaCNAME)
	}
}

func TestPlanDNSUpdatesRecordPointingElsewhere(t *testing.T) {
	existing := []DNSRecord{
		{
			Name:        "app.example.com",
			Type:        "CNAME",
			Content:     "old-tunnel.cfargotunnel.com",
			OwnerTunnel: qaTunnelID,
		},
	}
	plans := PlanDNS(qaTunnelID, qaCNAME, []Route{{Hostname: "app.example.com"}}, existing)

	if len(plans) != 1 || plans[0].Action != DNSUpdate {
		t.Fatalf("want one update plan, got %+v", plans)
	}
	if plans[0].Current != "old-tunnel.cfargotunnel.com" {
		t.Errorf("Current = %q, want the existing target", plans[0].Current)
	}
}

func TestPlanDNSKeepsCorrectRecordSilent(t *testing.T) {
	existing := []DNSRecord{
		{
			Name:         "app.example.com",
			Type:         "CNAME",
			Content:      qaCNAME,
			OwnerTunnel:  qaTunnelID,
			OwnerComment: cftuiOwnerMarker,
		},
	}
	plans := PlanDNS(qaTunnelID, qaCNAME, []Route{{Hostname: "app.example.com"}}, existing)

	if len(plans) != 0 {
		t.Fatalf("a correct record must produce no work, got %+v", plans)
	}
}

func TestPlanDNSLeavesForeignRecordsAlone(t *testing.T) {
	existing := []DNSRecord{
		{Name: "app.example.com", Type: "CNAME", Content: "blog.example.net", OwnerTunnel: "someone-else"},
	}
	plans := PlanDNS(qaTunnelID, qaCNAME, []Route{{Hostname: "app.example.com"}}, existing)

	if len(plans) != 1 {
		t.Fatalf("want one keep plan, got %+v", plans)
	}
	if plans[0].Action != DNSKeep {
		t.Fatalf("foreign record must be DNSKeep, got %q", plans[0].Action)
	}
	if plans[0].Current != "blog.example.net" {
		t.Errorf("Current = %q, want the untouched foreign target", plans[0].Current)
	}
}

func TestPlanDNSReportsOrphans(t *testing.T) {
	existing := []DNSRecord{
		{Name: "live.example.com", Type: "CNAME", Content: qaCNAME, OwnerTunnel: qaTunnelID},
		{Name: "stale.example.com", Type: "CNAME", Content: qaCNAME, OwnerTunnel: qaTunnelID},
		{Name: "alive.example.com", Type: "CNAME", Content: "other.cfargotunnel.com", OwnerTunnel: "other-tunnel"},
	}
	routes := []Route{
		{Hostname: "live.example.com", Service: "http://localhost:3000"},
		{Service: "http_status:404"},
	}

	plans := PlanDNS(qaTunnelID, qaCNAME, routes, existing)

	orphans := Orphans(plans)
	if len(orphans) != 1 {
		t.Fatalf("want exactly one orphan, got %+v (all plans %+v)", orphans, plans)
	}
	if orphans[0].Hostname != "stale.example.com" {
		t.Errorf("orphan = %q, want stale.example.com", orphans[0].Hostname)
	}
	for _, p := range plans {
		if p.Hostname == "alive.example.com" {
			t.Errorf("another tunnel's record must not appear: %+v", p)
		}
	}
}

func TestPlanDNSIsDeterministicAndPure(t *testing.T) {
	existing := []DNSRecord{
		{Name: "z.example.com", Type: "CNAME", Content: qaCNAME, OwnerTunnel: qaTunnelID},
		{Name: "a.example.com", Type: "CNAME", Content: qaCNAME, OwnerTunnel: qaTunnelID},
		{Name: "m.example.com", Type: "CNAME", Content: "old.cfargotunnel.com", OwnerTunnel: qaTunnelID},
	}
	routes := []Route{
		{Hostname: "m.example.com", Service: "http://localhost:3000"},
		{Hostname: "new.example.com", Service: "http://localhost:4000"},
		{Service: "http_status:404"},
	}

	first := PlanDNS(qaTunnelID, qaCNAME, routes, existing)
	for i := 0; i < 50; i++ {
		again := PlanDNS(qaTunnelID, qaCNAME, routes, existing)
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("PlanDNS is nondeterministic:\nfirst  %+v\nagain  %+v", first, again)
		}
	}

	for _, existingRec := range existing {
		if existingRec.Content == "old.cfargotunnel.com" && existingRec.OwnerTunnel != qaTunnelID {
			t.Fatalf("PlanDNS mutated its input: %+v", existingRec)
		}
	}

	wantOrphanOrder := []string{"a.example.com", "z.example.com"}
	got := Orphans(first)
	gotOrder := make([]string, len(got))
	for i, o := range got {
		gotOrder[i] = o.Hostname
	}
	if !reflect.DeepEqual(gotOrder, wantOrphanOrder) {
		t.Errorf("orphans = %v, want sorted %v", gotOrder, wantOrphanOrder)
	}
}

func TestDNSChangesMirrorsPlan(t *testing.T) {
	plans := []DNSPlan{
		{Hostname: "app.example.com", Action: DNSCreate},
		{Hostname: "old.example.com", Action: DNSOrphan},
	}
	got := DNSChanges(plans)
	want := []DNSChange{
		{Hostname: "app.example.com", Action: "create"},
		{Hostname: "old.example.com", Action: "orphan"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DNSChanges = %+v, want %+v", got, want)
	}
}

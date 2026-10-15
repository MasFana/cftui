package local

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Management says which side owns a tunnel's ingress configuration.
type Management string

const (
	// ManagedUnknown means cftui could not tell; it behaves conservatively and
	// treats the tunnel as read-only.
	ManagedUnknown Management = "unknown"
	// ManagedLocal means the tunnel is configured by ~/.cloudflared/config.yml.
	ManagedLocal Management = "local"
	// ManagedRemote means the tunnel is configured through the Cloudflare API.
	ManagedRemote Management = "remote"
)

// DetectInput carries the two facts the detection decision needs, already
// discovered by the caller.
type DetectInput struct {
	TunnelID          string
	ConfigTunnelNames []string
	CredentialIDs     []string
}

// DetectManagement decides whether a tunnel is locally or remotely managed.
//
// It is pure: it only reads its argument, so the decision is unit-testable
// without a filesystem. The order matches the PRD exactly — an explicit entry in
// config.yml wins, then a per-tunnel credentials file, and only then is the
// tunnel assumed remote.
func DetectManagement(in DetectInput) Management {
	id := strings.TrimSpace(in.TunnelID)
	if id == "" {
		return ManagedUnknown
	}
	for _, name := range in.ConfigTunnelNames {
		if strings.EqualFold(strings.TrimSpace(name), id) {
			return ManagedLocal
		}
	}
	for _, credID := range in.CredentialIDs {
		if strings.EqualFold(strings.TrimSpace(credID), id) {
			return ManagedLocal
		}
	}
	return ManagedRemote
}

// DiscoverCredentials lists the tunnel ids for which ~/.cloudflared/<id>.json
// credentials files exist. A missing directory is not an error: it just means no
// tunnel has local credentials yet.
func DiscoverCredentials(home string) ([]string, error) {
	if home == "" {
		return nil, nil
	}
	dir := filepath.Join(home, ".cloudflared")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		out = append(out, strings.TrimSuffix(e.Name(), ".json"))
	}
	sort.Strings(out)
	return out, nil
}

// Disagreement reports a warning when the local and remote views of one tunnel
// differ, or "" when they agree.
//
// It is pure. It compares rule counts first, which catches the common case of an
// ingress list edited on one side only, then names every hostname that appears
// on just one side so the message is actionable rather than a bare count.
func Disagreement(name string, localRules []Rule, remoteHosts []string) string {
	if len(localRules) == len(remoteHosts) && sameHostSet(localRules, remoteHosts) {
		return ""
	}

	localSet := make(map[string]bool, len(localRules))
	for _, r := range localRules {
		h := strings.ToLower(strings.TrimSpace(r.Hostname))
		if h != "" {
			localSet[h] = true
		}
	}
	remoteSet := make(map[string]bool, len(remoteHosts))
	for _, h := range remoteHosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if h != "" {
			remoteSet[h] = true
		}
	}

	onlyLocal := make([]string, 0)
	onlyRemote := make([]string, 0)
	for h := range localSet {
		if !remoteSet[h] {
			onlyLocal = append(onlyLocal, h)
		}
	}
	for h := range remoteSet {
		if !localSet[h] {
			onlyRemote = append(onlyRemote, h)
		}
	}
	sort.Strings(onlyLocal)
	sort.Strings(onlyRemote)

	var b strings.Builder
	b.WriteString("warning: ")
	b.WriteString(name)
	b.WriteString(" differs between the local config and Cloudflare")
	if len(onlyLocal) > 0 {
		b.WriteString("; only local: ")
		b.WriteString(strings.Join(onlyLocal, ", "))
	}
	if len(onlyRemote) > 0 {
		b.WriteString("; only remote: ")
		b.WriteString(strings.Join(onlyRemote, ", "))
	}
	return b.String()
}

func sameHostSet(localRules []Rule, remoteHosts []string) bool {
	local := make(map[string]bool, len(localRules))
	for _, r := range localRules {
		h := strings.ToLower(strings.TrimSpace(r.Hostname))
		if h != "" {
			local[h] = true
		}
	}
	remote := make(map[string]bool, len(remoteHosts))
	for _, h := range remoteHosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if h != "" {
			remote[h] = true
		}
	}
	if len(local) != len(remote) {
		return false
	}
	for h := range local {
		if !remote[h] {
			return false
		}
	}
	return true
}

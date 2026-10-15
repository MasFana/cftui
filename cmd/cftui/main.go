// Command cftui is a terminal UI for Cloudflare Zero Trust tunnels. It
// authenticates with the OAuth token found in the local cloudflared origin
// certificate, so no API token is ever typed, stored, or read from the
// environment.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/masfana/cftui/internal/cf"
	"github.com/masfana/cftui/internal/cred"
	"github.com/masfana/cftui/internal/local"
	"github.com/masfana/cftui/internal/ui"
)

const defaultConfigFile = "config.yml"

func main() {
	fs := flag.NewFlagSet("cftui", flag.ExitOnError)
	originCert := fs.String("origincert", "", "path to the cloudflared origin certificate (default: $TUNNEL_ORIGIN_CERT, then ~/.cloudflared/cert.pem)")
	configPath := fs.String("config", "", "path to the cloudflared config.yml (default: $TUNNEL_CONFIG, then ~/.cloudflared/config.yml)")
	debug := fs.Bool("debug", false, "print the tunnel list and exit without starting the TUI")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "cftui - a terminal UI for Cloudflare Zero Trust tunnels\n\n")
		fmt.Fprintf(fs.Output(), "Usage:\n  cftui [flags]\n\nFlags:\n")
		fs.PrintDefaults()
	}
	_ = fs.Parse(os.Args[1:])

	cfg := resolveConfigPath(*configPath)
	file, err := local.Load(cfg)
	if err != nil {
		fatal(err)
	}

	localB := cf.Backend(localBackend{file: file, path: cfg, pipe: local.NewPipeline()})
	backends := ui.Backends{Local: localB}
	var remoteErr error

	detect := ui.Detect{
		Management: managementForTunnel(file),
		LocalRules: func(tunnel string) []local.Rule {
			rules, err := file.TunnelIngress(tunnel)
			if err != nil {
				return nil
			}
			return rules
		},
	}

	if remote, err := remoteBackendFor(*originCert); err != nil {
		if blocker, blocked := cred.StartupScopeBlocker(context.Background()); blocked {
			fmt.Fprintln(os.Stderr, "cftui: "+blocker)
		}
		remoteErr = err
	} else {
		backends.Remote = remote
		// FR-20a: compare the local config against what Cloudflare actually
		// holds, using the remote backend as the source of truth.
		detect.RemoteHosts = func(ctx context.Context, tunnel string) ([]string, error) {
			routes, err := remote.GetRoutes(ctx, tunnel)
			if err != nil {
				return nil, err
			}
			hosts := make([]string, 0, len(routes))
			for _, r := range routes {
				if h := r.Hostname; h != "" {
					hosts = append(hosts, h)
				}
			}
			return hosts, nil
		}
		if !*debug {
			fmt.Fprintln(os.Stderr, "cftui: origin certificate loaded; remote and local backends available")
		}
	}

	if *debug {
		if remoteErr != nil {
			fmt.Fprintln(os.Stderr, "cftui: remote backend unavailable, listing local config only: "+remoteErr.Error())
		}
		src := debugBackend(backends.Local, backends.Remote)
		if err := runDebug(src, detect.Management); err != nil {
			fatal(err)
		}
		return
	}

	if remoteErr != nil {
		fmt.Fprintln(os.Stderr, "cftui: starting in local-only mode: "+remoteErr.Error())
	}

	model := ui.NewWithDetect(backends, detect)
	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fatal(err)
	}
}

// remoteBackendFor builds a remote backend from the origin certificate, or
// returns an error explaining why cftui must stay local-only.
func remoteBackendFor(explicitCert string) (cf.Backend, error) {
	if err := cred.RejectTokenFromEnv(); err != nil {
		return nil, err
	}
	cert, certPath, err := cred.Load(explicitCert)
	if err != nil {
		return nil, err
	}
	if cert.APIToken == "" {
		return nil, fmt.Errorf("origin certificate at %s has no API token; run: cloudflared tunnel login", certPath)
	}
	return remoteBackend(cert)
}

// managementForTunnel classifies every tunnel in the config as locally or
// remotely managed, using the credentials directory as the second signal. The
// result feeds the UI's per-tunnel MANUAL/MANAGED badges (FR-20) and the
// routes view's disagreement warning (FR-20a, wired in main).
func managementForTunnel(file *local.File) map[string]local.Management {
	home, _ := os.UserHomeDir()
	credIDs, err := local.DiscoverCredentials(home)
	if err != nil {
		credIDs = nil
	}
	names := file.TunnelNames()
	out := make(map[string]local.Management, len(names))
	for _, name := range names {
		out[name] = local.DetectManagement(local.DetectInput{
			TunnelID:          name,
			ConfigTunnelNames: names,
			CredentialIDs:     credIDs,
		})
	}
	return out
}

// resolveConfigPath picks the config file: an explicit flag wins, then
// $TUNNEL_CONFIG, then ~/.cloudflared/config.yml.
func resolveConfigPath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if env := os.Getenv("TUNNEL_CONFIG"); env != "" {
		return env
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home + string(os.PathSeparator) + ".cloudflared" + string(os.PathSeparator) + defaultConfigFile
	}
	return defaultConfigFile
}

// debugBackend prefers the remote backend so -debug reports what Cloudflare
// actually holds; without a certificate it falls back to the local config.
func debugBackend(localB, remoteB cf.Backend) cf.Backend {
	if remoteB != nil {
		return remoteB
	}
	return localB
}

// runDebug prints the local tunnel list to stdout and returns. It renders only
// names, statuses, management and counts: the origin certificate is never
// loaded here, so no token can reach the output.
func runDebug(b cf.Backend, management map[string]local.Management) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tunnels, err := b.ListTunnels(ctx)
	if err != nil {
		return err
	}
	if len(tunnels) == 0 {
		fmt.Println("no tunnels found")
		return nil
	}
	fmt.Printf("%-30s %-10s %-8s %7s %7s\n", "NAME", "STATUS", "MANAGED", "CONNS", "ROUTES")
	home, _ := os.UserHomeDir()
	credIDs, _ := local.DiscoverCredentials(home)
	for _, t := range tunnels {
		managed := management[t.Name]
		if t.RemoteManaged {
			managed = local.ManagedRemote
		}
		if managed == "" && t.ID != "" {
			// An API-listed tunnel the config file does not name is still
			// classified by its own id, so a CLI-created tunnel with local
			// credentials reads as local rather than unknown.
			managed = local.DetectManagement(local.DetectInput{
				TunnelID:      t.ID,
				CredentialIDs: credIDs,
			})
		}
		if managed == "" {
			managed = local.ManagedUnknown
		}
		routes := t.RouteN
		if routes == 0 {
			if rs, err := b.GetRoutes(ctx, t.ID); err == nil {
				routes = len(rs)
			}
		}
		fmt.Printf("%-30s %-10s %-8s %7d %7d\n", t.Name, t.Status, managed, t.ConnectorN, routes)
	}
	return nil
}

// fatal prints an error on stderr and exits non-zero. Error strings from the
// cred package never contain the token.
func fatal(err error) {
	fmt.Fprintf(os.Stderr, "cftui: %v\n", err)
	os.Exit(1)
}

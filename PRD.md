# PRD: cftui — a Terminal UI for Cloudflare Zero Trust Tunnels

**Version:** 0.2 (Draft, revised) · **Language:** Go · **Status:** Proposed

> **Revision note (v0.1 → v0.2).** v0.1 authenticated with a hand-made Cloudflare API
> token read from `CLOUDFLARE_API_TOKEN` / the OS keychain. That model is **replaced**:
> `cftui` now authenticates through the **local `cloudflared` OAuth origin certificate**
> (`cert.pem`) produced by `cloudflared tunnel login`. The certificate *embeds* an API
> token, plus the account and zone it was issued for — so `cftui` inherits the user's
> existing Cloudflare credential, inherits its scope, and needs no token-pasting step.
> The **primary workflow** is now **managing a self-hosted port**: point a hostname at a
> local port on this machine and let `cftui` wire up ingress + DNS and reload `cloudflared`.

---

## 1. Summary

`cftui` is a keyboard-driven terminal UI that lets developers and homelab/ops users manage
Cloudflare Tunnel (`cloudflared`) public hostnames without opening the Zero Trust dashboard
or hand-editing `config.yml`.

Its **headline workflow** is the one a homelabber performs dozens of times: *"I have
something listening on `localhost:3000` — expose it at `grafana.example.com`."* From one
screen the user supplies a subdomain and a local port; `cftui` writes the ingress rule,
creates the proxied DNS CNAME, verifies the port is actually listening, and reloads the
tunnel.

Authentication is inherited from the `cloudflared` install the user already has. `cftui`
does not replace `cloudflared`; it configures it and drives it.

## 2. Problem

- Adding a service to a tunnel means: open dashboard → pick tunnel → Public Hostnames →
  fill a form → verify DNS. Repeated dozens of times, this is slow.
- Locally-managed tunnels require editing YAML, creating DNS CNAMEs separately
  (`cloudflared tunnel route dns`), and restarting the service. Typos in ingress rules
  silently break routing.
- There is no quick way to see "what is exposed right now, on which port, and is it
  healthy?"
- Creating a scoped API token by hand in the dashboard is itself a multi-step, error-prone
  chore that most homelab users get wrong (wrong scope, wrong account).

## 3. Goals and Non-Goals

**Goals**

1. **Manage a self-hosted port** (the flagship flow): subdomain + local port → ingress rule
   + DNS CNAME + reload, in one action, in under 15 seconds.
2. Manage ingress rules (hostname, path, service type, port/address) for existing tunnels
   from a TUI.
3. Create/update/delete the DNS CNAME that points a hostname to the tunnel automatically.
4. Validate input before it reaches Cloudflare (ports, hostnames, catch-all rule,
   duplicates) **and** probe the local port before writing a rule that cannot work.
5. Show live status: tunnel health, connectors, and whether each local port is reachable.
6. Be fast, single static binary, works over SSH.

**Non-Goals (v1)**

- Installing `cloudflared` itself (only detect, drive, and optionally reload it).
- Managing Access applications and policies, WARP routing, or private network CIDR
  routes (candidates for v2).
- Multi-user collaboration, a hosted backend, or multi-account profiles.

> **Changed from v0.1:** *locally-managed `config.yml` tunnels* moved **from Non-Goals into
> scope** — see §6.6. Requiring remote mode would have excluded exactly the homelab audience
> that runs `cloudflared` as a local systemd service, which is the same audience the OAuth
> cert targets. The `Backend` abstraction that v0.1 designed for this already exists; it is
> now load-bearing rather than deferred.

## 4. Target Users

| Persona | Need |
| --- | --- |
| Homelabber | Expose Jellyfin, Home Assistant, Gitea, etc. quickly on subdomains |
| Backend/dev | Temporarily expose `localhost:3000` as `dev-name.example.com` and remove it later |
| Small-team ops / SRE | Audit and tweak tunnel routes over SSH without browser access |

## 5. Scope: Managed Objects

1. **Credential / scope** — the `cloudflared` origin certificate, which pins the account and
   the zone. Selecting a different zone means re-running `cloudflared tunnel login`.
2. **Tunnels** — list, view status (healthy/degraded/down/inactive), connectors, version.
   Create and delete are in v1 (P0).
3. **Ingress rules (public hostnames)** — fields:
   - Hostname (subdomain + zone, e.g. `app.example.com`)
   - Path (optional regex, e.g. `/api/*`)
   - Service type: `http`, `https`, `tcp`, `ssh`, `rdp`, `smb`, `unix`, `http_status`,
     `hello_world`
   - Origin address: host + port (default `localhost`)
   - Origin request options (P1): `noTLSVerify`, `originServerName`, `httpHostHeader`,
     `connectTimeout`, `disableChunkedEncoding`
4. **DNS records** — proxied CNAME `<hostname>` → `<tunnel-id>.cfargotunnel.com`.
5. **Catch-all rule** — always preserved as the last rule (default `http_status:404`); user
   cannot delete it, only change it.
6. **Local port** — the loopback port a rule points at, its TCP reachability, and the local
   process that owns it (best-effort, for display).

## 6. Functional Requirements

### 6.1 Authentication and Config (P0)

- **FR-1 (REVISED):** Authenticate using the **local `cloudflared` origin certificate**.
  Resolve `cert.pem` in this order: (1) `--origincert <path>` flag, (2) `TUNNEL_ORIGIN_CERT`
  environment variable, (3) `~/.cloudflared/cert.pem`, (4)
  `~/.cloudflared/`-derived search directories as `cloudflared` itself resolves them.
  If no certificate exists, `cftui` does not prompt for a token — it **offers to run
  `cloudflared tunnel login`** and explains exactly what that will do.
  The certificate's secrets (the embedded API token and the private key) are **never
  written to logs, never displayed, and never included in error messages**.

- **FR-2 (REVISED):** Parse `cert.pem` as a PEM bundle. Accept the legacy `PRIVATE KEY` /
  `CERTIFICATE` blocks and the `ARGO TUNNEL TOKEN` block whose bytes are raw JSON:
  ```json
  { "zoneID": "...", "accountID": "...", "apiToken": "...", "endpoint": "..." }
  ```
  A certificate missing `zoneID` or `apiToken` is a hard error with a message telling the
  user to re-run `cloudflared tunnel login`. On success `cftui` constructs its API client
  from `apiToken`, scoped to `accountID`, and uses `zoneID` as the DNS zone. **Effective
  permissions are inherited from the certificate**; the required scope
  (*Account → Cloudflare Tunnel: Edit*, *Zone → DNS: Edit*, *Zone → Zone: Read*) is
  **verified with a single cheap probe call at startup** and a 403 surfaces as a clear
  "your `cloudflared` login lacks X" message, not a raw API error.

- **FR-3:** Config file at `~/.config/cftui/config.toml` for theme, keybindings, origin-cert
  path override, reload strategy, and auto-refresh interval. The config file holds
  **no credentials**.

- **FR-3a (NEW):** Detect the `cloudflared` binary (`exec.LookPath`) and report its version.
  `cftui` is useful without the binary (API-only mode) but loses `login`, `route dns`,
  `ingress validate`, and reload — each such feature degrades with an explicit notice
  rather than failing obscurely.

- **FR-3b (REVISED):** **No API token is ever entered, stored, or read from the
  environment.** `CLOUDFLARE_API_TOKEN` is explicitly *not* consulted; if it is set, `cftui`
  ignores it. (`cftui --legacy-token` is **out of scope for v1**; see §14.)

### 6.2 Tunnel List (P0)

- FR-4: Table of tunnels: name, ID, status, connector count, number of routes, created date.
- FR-5: Select a tunnel to open its route view. Refresh with `r`; auto-refresh interval
  configurable.
- FR-5a: **Create** a tunnel (`n`: enter a name) and **delete** one (`D`: type the tunnel
  name to confirm; also removes its CNAMEs and refuses while connectors are active unless
  forced).

### 6.3 Route Management (P0)

- FR-6: Table of routes: hostname, path, service type, origin, DNS state
  (OK / missing / mismatched), port reachability (●/○).
- FR-7: **Add** (`a`) via a form: subdomain (zone suffix auto-appended from the certificate),
  service type (dropdown), host, port, optional path. Defaults to `http://localhost:<port>`.
- **FR-7a (NEW — the self-port fast path):** Pressing `a` opens the form **with the local
  port field focused**, and the form performs a live TCP probe as the user types. A port
  that is not listening shows an inline warning ("nothing is listening on localhost:3000")
  **before** the user commits; the rule can still be saved (the service may start later).
  Entering a port prefills host `localhost` and service type `http`. This is the primary
  path for the headline workflow and must be reachable in one keystroke from the tunnel list.
- FR-8: **Edit** (`e`) any field; **Delete** (`d`) with confirmation that also offers to
  delete the DNS record.
- FR-9: **Duplicate detection**: reject duplicate hostname+path combinations.
- FR-10: **Ordering**: rules evaluate top-to-bottom, so users can reorder (`shift+↑/↓`); the
  catch-all stays last.
- FR-11: **Validation**: hostname is RFC-valid and inside the certificate's zone; port is
  1–65535; service type matches address format (e.g. `ssh://host:22`, `unix:/path`,
  `http_status:404` takes a code, not a port).
- FR-12: Changes are staged locally and applied with `w` (write/apply), showing a diff of
  before/after. `u` discards pending changes.
- **FR-12a (NEW):** Local-mode applies write `~/.cloudflared/config.yml` atomically (temp
  file + rename) after copying `.bak`, preserving comments via the yaml.v3 node API.

### 6.4 DNS Sync (P0)

- FR-13: On apply, create missing CNAMEs (proxied) and update mismatched ones. Never touch
  DNS records that were not created for this tunnel without explicit confirmation.
- FR-14: Show orphaned CNAMEs (point to this tunnel but have no ingress rule) and offer
  cleanup.

### 6.5 Health and Diagnostics (P1)

- FR-15: Local TCP dial check of each origin host:port with latency; optional HTTP HEAD
  check. This is the same probe the add-form uses (FR-7a).
- FR-16: Detail pane showing the connectors (colo, version, origin IP, uptime) for the
  selected tunnel.
- FR-17: "Test URL" action to request `https://hostname` and show the status code.

### 6.6 Local Mode (PROMOTED from v2 — now P0)

- FR-18: Support **locally-managed** tunnels by reading/writing `~/.cloudflared/config.yml`
  (or `--config` path), preserving comments where practical, with an automatic `.bak`
  backup before write.
- FR-19: After write, offer to reload. Reload strategy is configurable and detected:
  `systemctl --user restart cloudflared` / `systemctl restart cloudflared` / `cloudflared`
  under a named unit / send-signal. After any write, run `cloudflared tunnel ingress
  validate` and surface its output verbatim.
- FR-20: Auto-detect whether a tunnel is remotely or locally managed and pick the right
  backend; remote routes cannot be edited via local file. Detection order: if the tunnel id
  appears in `config.yml` → local; else if it has credentials in `~/.cloudflared/<id>.json`
  → local; else remote.
- **FR-20a (NEW):** When local and remote disagree about a tunnel, `cftui` says so
  explicitly instead of silently preferring one.

### 6.7 Quality-of-Life (P1/P2)

- Search/filter (`/`), help overlay (`?`), command palette (`:`), mouse support, themes,
  copy URL to clipboard (`y`), open in browser (`o`).
- Import/export of routes as YAML/JSON (P2) and a non-interactive CLI mode, e.g.
  `cftui add app.example.com --service http://localhost:3000` (P2).
- Undo history for the current session (P2).

## 7. UX Design

**Layout (three panes):** Tunnel list (left) → Route table (center) → Detail/health pane
(right, collapsible). A footer shows context-aware keybindings; a status bar shows account,
zone (both from the certificate), pending-change count, and last sync time.

**Key bindings (defaults)**

| Key | Action |
| --- | --- |
| `↑/↓`, `j/k` | Navigate |
| `enter` | Open tunnel / view route |
| `a` / `e` / `d` | Add (port-first) / edit / delete route |
| `w` / `u` | Apply / discard pending changes |
| `r` | Refresh |
| `/` | Filter |
| `?` | Help |
| `q` / `ctrl+c` | Quit (warn on unsaved changes) |

**Add-route form (self-port flow) — port field focused on open**

```
Tunnel    : homelab
Subdomain : grafana          .example.com
Service   : [http ▾]
Host      : localhost
▸ Port    : 3000      ● listening (1.2 ms)        ← live TCP probe
Path      : (optional)
[ ] Disable TLS verify    [ ] Custom Host header
               [ Save ]  [ Cancel ]
```

When the port probe fails:

```
▸ Port    : 3000      ○ nothing is listening on localhost:3000
                       (you can still save; the service may start later)
```

## 8. Technical Design

**Stack**

- TUI: `charmbracelet/bubbletea`, `bubbles` (table, textinput, list), `lipgloss` (styling),
  `huh` (forms).
- Cloudflare API: official `cloudflare-go` SDK.
- Config: `BurntSushi/toml`; local tunnel config: `gopkg.in/yaml.v3` (node API to preserve
  comments).
- Secrets: **none stored** — the credential is the `cloudflared` certificate.
- Testing: `teatest` for TUI flows; HTTP mock server for API tests.

**Architecture**

```
cmd/cftui/main.go
internal/
  ui/          # bubbletea models: tunnels, routes, forms, help
  app/         # state, commands/messages, pending-change engine
  cf/          # Cloudflare client wrapper (tunnels, config, DNS)
  cred/        # origin-cert discovery + decode (NEW)
  local/       # config.yml reader/writer, cloudflared exec + reload
  validate/    # hostname, port, service-type rules
  health/      # TCP/HTTP probes
  config/      # app config
```

`cred` and `local` are separated deliberately: `cred` parses the certificate and never
executes anything; `local` owns every `exec.Command` against the `cloudflared` binary.

**Backend interface** — two implementations, `RemoteBackend` (HTTP API) and `LocalBackend`
(YAML), keeping the UI backend-agnostic:

```go
type Backend interface {
    ListTunnels(ctx context.Context) ([]Tunnel, error)
    GetRoutes(ctx context.Context, tunnelID string) ([]Route, error)
    PutRoutes(ctx context.Context, tunnelID string, routes []Route) error
    SyncDNS(ctx context.Context, tunnelID string, routes []Route) ([]DNSChange, error)
    Health(ctx context.Context, tunnelID string) ([]Connector, error)
}
```

**Credential model (FR-1/FR-2) — the load-bearing change**

`cert.pem` is a PEM bundle. `PRIVATE KEY` and `CERTIFICATE` blocks are legacy; the
authoritative block is `ARGO TUNNEL TOKEN`, whose `Bytes` are raw JSON:

```go
type OriginCert struct {
    ZoneID    string `json:"zoneID"`
    AccountID string `json:"accountID"`
    APIToken  string `json:"apiToken"`
    Endpoint  string `json:"endpoint,omitempty"`
}
```

Decode loop (mirrors `cloudflared`'s own `decodeOriginCert`): reap every block with
`pem.Decode`, ignore/accept `PRIVATE KEY` and `CERTIFICATE`, unmarshal the
`ARGO TUNNEL TOKEN` block as JSON, and **fail if `ZoneID` or `APIToken` is empty**. The
client is then built with `cloudflare-go`'s `api.NewWithAPIToken(cert.APIToken)` (verified
signature: `func NewWithAPIToken(token string, opts ...Option) (*API, error)`), and
`cert.ZoneID` is the DNS zone. Because `AccountID` and `ZoneID` are carried explicitly on
the `cf` client and passed to each call site, no account/zone `api.Option` constructor is
required — the option constructors are not relied upon. Each API call that can 403 maps to
a scope-specific message.

**Key API calls**

- `GET/PUT /accounts/{id}/cfd_tunnel/{tunnel_id}/configurations` — read/replace the full
  ingress list (PUT replaces everything, so always read-modify-write and keep the catch-all).
- `GET /accounts/{id}/cfd_tunnel` — list tunnels and status; `.../connections` — connectors.
- `GET/POST/PATCH/DELETE /zones/{zone_id}/dns_records` — CNAME management.

**External `cloudflared` commands** (via `exec.LookPath("cloudflared")`; every argv built
as a slice, never a shell string):

| Purpose | argv |
| --- | --- |
| Authorize (FR-1) | `cloudflared tunnel login` |
| Create CNAME without filesystem access | `cloudflared tunnel route dns <id> <hostname>` |
| Remote tunnel run token | `cloudflared tunnel token <id>` |
| Validate a written config | `cloudflared tunnel ingress validate` |
| List (fallback / cross-check) | `cloudflared tunnel list` |
| Reload (local mode) | `systemctl --user restart cloudflared` (detected) |

**Manage-self-port end-to-end (the flagship flow)**

1. User presses `a`; the form opens with **Port focused**.
2. As the user types, a debounced TCP dial to `localhost:<port>` reports
   `● listening (1.2 ms)` / `○ nothing is listening` (FR-7a).
3. Subdomain is typed; the zone suffix from `cert.ZoneID` is shown as a fixed affix.
4. On Save, `validate` checks hostname RFC validity, zone membership, port range, and
   service-type/address agreement (FR-11).
5. A pending change is staged; `w` applies. `cf` performs read-modify-write of the ingress
   list — **keeping the catch-all last** — and `SyncDNS` creates or repairs the proxied
   CNAME to `<tunnel-id>.cfargotunnel.com`.
6. Local mode: `local` writes `config.yml` (`.bak` first, atomic rename), runs
   `cloudflared tunnel ingress validate`, then offers the reload.
7. Remote mode: the PUT *is* the apply; `cftui` reports the resulting route state.

Ingress rule produced (local YAML; the JSON PUT body is the same shape):

```yaml
ingress:
  - hostname: grafana.example.com
    service: http://localhost:3000
    originRequest:
      noTLSVerify: false
  - service: http_status:404   # catch-all, always last
```

**Concurrency and safety**

- Read-modify-write with a re-fetch before apply; if the remote config changed since load,
  warn and offer to reload (avoids clobbering dashboard edits).
- All network calls are async `tea.Cmd`s with timeouts and visible spinners; UI never blocks.
- Rate-limit handling with retry and backoff on HTTP 429.

## 9. Non-Functional Requirements

| Area | Requirement |
| --- | --- |
| Performance | Cold start < 300 ms to first render; list refresh < 2 s on typical accounts |
| Portability | Installable via go install; single static binary for Linux, macOS, Windows (amd64/arm64) |
| Security | Certificate secrets never logged or displayed; least-privilege guidance based on the login used; confirm destructive actions |
| Reliability | Atomic apply with diff preview; `.bak` + atomic rename in local mode; clear error messages with Cloudflare error codes |
| Accessibility | Works in 80×24 terminals; color-blind-safe status icons plus text labels |
| Observability | Optional debug log file (`--debug`) with a redaction pass over token-shaped strings |

## 10. Milestones

| Phase | Deliverable | Est. |
| --- | --- | --- |
| M0 | Project scaffold, **origin-cert auth**, API client, tunnel list | 1 week |
| M1 | Tunnel create/delete, route CRUD, validation, diff/apply, DNS sync, **self-port flow** — **MVP** | 2 weeks |
| M2 | Health checks, connector details, origin request options, filter/help, **local `config.yml` mode** | 1–2 weeks |
| M3 | CLI mode and import/export of routes | 1–2 weeks |
| M4 | go install release (tagged Go module), README and docs | 1 week |

> Local mode moved from v2 to M2: the OAuth-cert path and the local-`config.yml` path share
> the same audience, so shipping one without the other strands the primary user.

## 11. Success Metrics

- **Time to expose a new local port: under 15 seconds** from launch (vs. ~1–2 minutes on the
  dashboard).
- **Zero `cloudflared login` prompts in a normal session** for a user who has already run it
  once.
- Zero invalid configs pushed (validation catches 100% of malformed ingress in tests).
- 95% of apply operations succeed first try in beta.
- Beta feedback: ≥ 80% of testers prefer it over the dashboard for routine changes.

## 12. Risks and Mitigations

| Risk | Mitigation |
| --- | --- |
| **Certificate scope is narrower than needed** (the embedded token can 403) | Probe scopes at startup; map 403 to a scope-specific message naming the login, not the token |
| **Certificate expiry / re-login needed** | Detect decode failures and auth errors; offer `cloudflared tunnel login` inline; never ask for a token |
| `PUT` replaces the whole config and could drop rules edited elsewhere | Re-fetch before write, diff preview, conflict warning |
| **Local write corrupts `config.yml`** (now P0, released from v2) | `.bak` copy, atomic temp+rename, yaml.v3 node API, `ingress validate` after every write |
| Cloudflare API schema changes | Pin SDK version; wrap in own `cf` package |
| Locally-managed YAML comments lost | yaml.v3 node API; always `.bak` |
| DNS record deletion removes something unrelated | Only manage CNAMEs targeting this tunnel; confirm otherwise |

## 13. Decisions

1. **Authentication:** the local `cloudflared` OAuth origin certificate (`cert.pem`) is the
   only credential source in v1. No API token entry, no keychain, no
   `CLOUDFLARE_API_TOKEN`. *(Reverses v0.1 §6.1.)* Construction uses only the verified
   `cloudflare-go` entry point `NewWithAPIToken`; account and zone travel as explicit
   `cf` client fields, not via unverified `api.Option` constructors.
2. **Primary workflow:** managing a self-hosted local port is the flagship flow and the
   first-keystroke path (FR-7a).
3. **Local mode:** promoted from v2 to v1 P0 (§6.6) — it serves the same audience as the
   OAuth cert.
4. **Tunnel create/delete:** in v1 (P0). Delete requires typing the tunnel name to confirm.
5. **Access applications:** out of scope for v1; stays in the v2 ideas.
6. **Multi-account:** not in v1. One account and one zone, as pinned by the certificate.
7. **Distribution:** `go install` (tagged Go module); no Homebrew/AUR packaging in v1.
8. **Unverified APIs are not depended on:** the PRD names only upstream symbols whose
   signatures were read directly (`NewWithAPIToken`, the `OriginCert` JSON tags, the
   `ARGO TUNNEL TOKEN` block). Anything not read yet is described behaviourally, so no
   section promises an API that may not exist.

## 14. Future Ideas (v2+)

- Escaping the certificate's single-zone scope (multi-zone / multi-account profiles).
- A `--legacy-token` escape hatch for headless CI where no `cloudflared` login exists.
- Zero Trust Access policy binding per hostname.
- Private network (CIDR) routes and WARP virtual networks.
- Templates/profiles ("expose dev stack") and git-synced route files (GitOps).
- Live connector log tail and metrics sparkline.
- Auto-discovery of listening local ports and Docker containers to suggest routes.

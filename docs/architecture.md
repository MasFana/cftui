# Architecture

## Package map

```
cmd/cftui         entry point: flags, backend selection, wiring
internal/ui       bubbletea model: views, key handling, rendering
internal/app      shared application state types
internal/cf       Cloudflare API client, ingress rules, DNS planning
internal/local    config.yml node-tree editing, management detection, reload
internal/cred     origin-cert discovery and decoding, cloudflared detection
internal/health   local and remote health probes
internal/validate input validation (tunnel names, hostnames, ports, services)
```

Dependencies point inward: `ui` and `local` may use `cf`, `cred`, and
`validate`, but `cf` never imports `ui`. That keeps the network layer testable
without a terminal.

## Backends

Everything the UI does to your tunnels goes through one narrow interface:

```go
type Backend interface {
    ListTunnels(ctx context.Context) ([]Tunnel, error)
    GetRoutes(ctx context.Context, tunnel string) ([]Route, error)
    PutRoutes(ctx context.Context, tunnel string, routes []Route) error
    SyncDNS(ctx context.Context, tunnel string, routes []Route) ([]DNSChange, error)
    Health(ctx context.Context, tunnel string) ([]Connector, error)
}
```

Two implementations satisfy it:

- **remote** — talks to the Cloudflare API using the origin certificate's
  embedded token. Available when `cert.pem` exists and `CLOUDFLARE_API_TOKEN` is unset.
- **local** — shells out to `cloudflared` and edits `config.yml` on disk.

At startup `cmd/cftui` attaches the remote backend when it can and records why
when it cannot. `-debug` prints which one won.

## The management badge

Each tunnel is badged `local`, `remote`, or `unknown`:

- **remote** — the account API lists it.
- **local** — a credentials file for it exists under `~/.cloudflared/`.
- **unknown** — neither, which usually means the file belongs to a tunnel that
  was never registered.

The badge answers "who can change this tunnel", which is why it is computed
from both sources rather than trusting the API list alone.

## Local-vs-remote disagreement

Cloudflare and your local `config.yml` can disagree: you may have edited the
file without pushing. When an origin certificate is available, `cftui` compares
the local rules against the hosts the account actually serves and renders a
footer when they differ. The comparison is order-independent and empty-safe, so
an empty local config never produces a spurious warning.

## Two invariants worth knowing

1. **Reads never invent state.** Opening a tunnel reads exactly what the source
   holds. A tunnel with no ingress shows no routes; no catch-all row is
   fabricated for display. (This was a real bug, fixed and regression-tested.)
2. **Writes always end with a catch-all.** `EnsureCatchAllLast` runs on the
   write path only, so a pushed config is always valid and the catch-all is
   always final.

## Why the help panel is modal

The help overlay is render-only by nature, which means an unguarded key handler
would let `w` apply staged changes or `q` quit the app while you are reading it.
`cftui` gates all input at the top of `handleKey` while help is open, so only
`?`, `esc`, `q`, and `ctrl+c` do anything.

## Testing

UI behaviour is tested through the model, not by driving the terminal. Feeding
keystrokes to a real bubbletea program is timing-dependent; calling `Update` with
`tea.KeyMsg` values is deterministic. Rendering is asserted by inspecting the
view's output string.

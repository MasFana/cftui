# cftui

A terminal UI for [Cloudflare Zero Trust](https://developers.cloudflare.com/cloudflare-one/) tunnels.

`cftui` puts the whole tunnel workflow — list, create, route, DNS, verify — in one
screen, so you stop hand-editing `~/.cloudflared/config.yml` and stop copy-pasting
tunnel IDs between `cloudflared` subcommands and the Cloudflare dashboard.

---

## What this app does

`cloudflared` is excellent at *running* a tunnel and awkward at *managing* one.
Routing a hostname to a local service means editing YAML by hand, remembering
that the catch-all rule must stay last, keeping DNS in sync manually, and
separately checking whether the connector is even up. Each step is easy; doing
them together, repeatedly, is not.

`cftui` is the management layer. It:

- **Lists every tunnel in your account** (not just this machine — see
  [Scope](#scope-account-wide-not-machine-local)).
- **Creates and deletes tunnels** through the Cloudflare API.
- **Edits ingress rules for you** as a YAML node tree, so your comments and key
  order survive, and keeps the catch-all rule last.
- **Stages route changes** and shows you the diff before anything hits disk.
- **Syncs DNS** — creates missing CNAMEs, repairs wrong ones, never touches
  foreign records, and lists orphans.
- **Checks health** — TCP and HTTP probes with latency.
- **Warns about drift** when your local config and Cloudflare disagree.
- **Fetches connector tokens** — `t` on a tunnel shows the token another host
  needs to run that tunnel, masked until you explicitly reveal it.

Two backends sit behind one interface so the UI never cares which it is talking
to: a **local file-backed backend** that reads and edits `config.yml`, and a
**remote backend** built on [`cloudflare-go`](https://github.com/cloudflare/cloudflare-go).
The remote side is optional — without a certificate, `cftui` runs local-only.

### The flagship flow

The thing you actually do most often is **expose a local port**. Press `a`, and
the port field is already focused; the port is probed live over TCP and you are
warned before committing if nothing is listening.

```
$ cftui
cftui - tunnels

  MANUAL   demo-tunnel     healthy    connectors=1 routes=3
  MANAGED  legacy-api      healthy    connectors=1 routes=1
  UNKNOWN  scratch         inactive   connectors=0 routes=0
```

---

## Authentication: two different tokens, one job each

This is the most commonly confused part of Cloudflare tunnels, so it is worth
being precise. **There are two different kinds of token, with two different
purposes.** `cftui` uses one of them.

### 1. The origin certificate — OAuth login (what `cftui` uses)

This is what `cloudflared tunnel login` produces:

```sh
cloudflared tunnel login
```

It opens a browser, you authorise your Cloudflare account once, and cloudflared
writes **`~/.cloudflared/cert.pem`**. That file embeds an OAuth-derived API
token, plus the **account ID** and **zone ID** it was issued for.

- **Purpose:** account-wide *management* — list/create/delete tunnels, read and
  write tunnel configuration, manage DNS records.
- **Scope:** your whole account (or the zones you authorised).
- **Lifetime:** long-lived; it persists until you revoke it or re-login.
- **Typical consumer:** dashboards, CLIs, and tools like `cftui` that read and
  change *configuration*.

`cftui` reads this certificate, decodes the API token from it, and uses that
token for every Cloudflare API call. **It never asks you to paste a token.**

### 2. The connector token — what a *running* tunnel uses

This is what makes tunnel `221cc559-…` actually run on a machine. It comes from
one of:

- **A credentials file.** `cloudflared tunnel create <name>` writes
  `~/.cloudflared/<TUNNEL-ID>.json`, and `cloudflared tunnel run --credentials-file …`
  reads it.
- **A connector token.** A single string you pass as
  `cloudflared tunnel run --token <TOKEN>` (or `$TUNNEL_TOKEN`).

```
cloudflared tunnel run --token eyJhIjoi...   # starts the connector
```

- **Purpose:** authenticating *this one connector process* to the edge so it can
  serve traffic for *one* tunnel.
- **Scope:** a single tunnel.
- **Typical consumer:** the `cloudflared` daemon itself, or a container image
  that runs it.

### How they relate

| | Origin certificate (`cert.pem`) | Connector token / credentials |
| --- | --- | --- |
| Created by | `cloudflared tunnel login` | `cloudflared tunnel create` or the dashboard |
| Used by | `cftui`, the dashboard, the API | `cloudflared tunnel run` |
| Authenticates | *you*, for management | *a connector*, to serve traffic |
| Scope | your account / zones | one tunnel |
| `cftui` uses it? | **Yes** | No |

**A connector token is not a substitute for the origin certificate, and vice
versa.** You can run a tunnel perfectly well with only a connector token and no
`cert.pem` on the box — but then you cannot *manage* it from that box. `cftui`
is a management tool, so it needs the certificate. Conversely, having `cert.pem`
does not start a connector; it only lets you configure one.

`cftui` will not *authenticate* with a connector token — it is scoped to a
single tunnel and cannot list or create tunnels, so accepting one would give you
a tool that half-works and fails confusingly later.

It can, however, **fetch** a connector token for a tunnel you already manage,
so you can run that tunnel on another machine. Press `t` on the tunnel list and
`cftui` shows the token, masked until you reveal it. That is a read of an
existing credential, not a way to log in.

### What `cftui` looks for

The origin certificate is discovered in cloudflared's own precedence order:

1. `--origincert <path>`
2. `$TUNNEL_ORIGIN_CERT`
3. `~/.cloudflared/cert.pem`

If none is found, `cftui` starts in **local-only mode**: it can still read and
edit `config.yml` through the local backend, but anything needing the Cloudflare
API is unavailable.

### Why not an API token?

Setting `CLOUDFLARE_API_TOKEN` is **explicitly rejected**, not silently ignored:

```
cred: CLOUDFLARE_API_TOKEN is not supported; run: cloudflared tunnel login
```

A long-lived, account-wide API token pasted into an environment variable is
exactly the failure mode the certificate model avoids: it is easy to leak, easy
to over-scope, and it bypasses the browser authorisation step that lets you
review what you are granting. Using the certificate means `cftui` inherits
exactly the authorisation you already gave `cloudflared`, and nothing more.

---

## Scope: account-wide, not machine-local

The tunnel list is fetched from `GET /accounts/{account_id}/cfd_tunnel` — an
**account-wide** endpoint. So the list shows **every tunnel in your account**,
wherever its connectors happen to run, and each row's connection count is that
tunnel's **total** connectors across all hosts.

The only machine-scoped thing is the *local* backend, which reads
`~/.cloudflared/config.yml` on this box. It is used for local management
detection and as the fallback when no certificate is present. It does not limit
what the remote list shows.

---

## Features

- **Self-port fast path** — `a` opens the route form with the local port focused,
  probes the port over a live TCP connection, and warns you before committing if
  nothing is listening.
- **Management badges** — every tunnel is labelled `MANUAL` (owned by your local
  `config.yml` or a matching credentials file), `MANAGED` (owned by the
  Cloudflare API), or `UNKNOWN` (no tunnel ID to reason about).
- **Drift warning** — when your local config and Cloudflare disagree about a
  tunnel's hostnames, the routes view says so and names the hosts that exist on
  only one side.
- **Safe writes** — the previous `config.yml` is copied to `config.yml.bak`, the
  new file is written to a temp file and renamed into place, and your comments
  and key order survive because the YAML is edited as a node tree rather than
  re-marshalled.
- **Validation after write** — `cloudflared tunnel ingress validate` runs against
  the new file, so a bad ingress rule is caught immediately instead of at the
  next connector restart.
- **Staged changes** — route edits are staged; `w` applies, `u` discards.
  Nothing hits disk until you say so.
- **DNS sync** — missing CNAMEs are created and wrong ones repaired, foreign
  records are never touched, and orphans are listed.
- **Health checks** — TCP and HTTP probes with latency, plus a test-URL action
  that opens the route's public hostname in your browser.

---

## Requirements

- **Go 1.24+** — to build from source (not needed if you install a release
  binary).
- **[`cloudflared`](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/)
  on your `PATH`** — required for ingress validation and for the local
  cloudflared detection used by the OAuth flow. cloudflared **2022.1.0 or newer**
  is required for the login flow; older versions are reported with an upgrade
  hint.

---

## Install

```sh
go install github.com/masfana/cftui/cmd/cftui@latest
```

Or from a clone:

```sh
git clone https://github.com/masfana/cftui
cd cftui
go build ./cmd/cftui
```

---

## Usage

```sh
cftui                                    # start the TUI
cftui -config ./config.yml               # use a specific config file
cftui -origincert /path/to/cert.pem      # use a specific origin certificate
cftui -debug                             # print the tunnel list and exit
cftui -h                                 # show flags
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `-config <path>` | `$TUNNEL_CONFIG`, then `~/.cloudflared/config.yml` | Path to the cloudflared `config.yml`. |
| `-origincert <path>` | `$TUNNEL_ORIGIN_CERT`, then `~/.cloudflared/cert.pem` | Path to the origin certificate. |
| `-debug` | `false` | Print the tunnel list and exit without starting the TUI. Needs no terminal, so it is usable in scripts and CI. |

### Keys

**Tunnels**

| Key | Action |
| --- | --- |
| `n` | create a tunnel (name is validated) |
| `d` | delete the selected tunnel (type its name to confirm) |
| `enter` | open the tunnel's routes |
| `t` | fetch the connector token to run this tunnel elsewhere |
| `r` | refresh the list |

**Routes**

| Key | Action |
| --- | --- |
| `a` | add a route, local **port** field focused first |
| `o` | open the selected route's public URL in a browser |
| `J` / `K` | move a route down / up (the catch-all stays last) |
| `p` | pin the selected route to the top |
| `w` | apply staged changes |
| `u` | discard staged changes |
| `/` | filter routes by host, path, or service |
| `esc` | back to the tunnel list |

**Connector token panel**

| Key | Action |
| --- | --- |
| `v` | reveal / hide the token |
| `t` / `esc` / `q` | close the panel |

**Add-route form**

| Key | Action |
| --- | --- |
| `tab` / `shift+tab` | move between port, hostname, and service |
| `enter` | stage the route |
| `esc` | cancel (returns to where you came from) |

**Anywhere**

| Key | Action |
| --- | --- |
| `?` | open help, or close it when already open |
| `esc` | close help when it is open |
| `q` | close help when it is open, otherwise quit (a second `q` confirms when changes are staged) |
| `ctrl+c` | quit immediately |

Help is **modal**: while it is open every other key is ignored, so a stray
keypress while reading the reference cannot create, delete, or apply anything.
Press `?`, `esc`, or `q` to close it.

### Running a tunnel elsewhere (`t`)

The `t` action answers "how do I run this tunnel on another machine?". It fetches
that tunnel's connector token — through the Cloudflare API by tunnel ID when an
origin certificate is present, or via `cloudflared tunnel token` as a fallback —
and shows it **masked** until you press `v`.

```
connector token for demo-tunnel

  run this tunnel elsewhere with: cloudflared tunnel run --token <token>

  eyJh••••••••••••1ZmI           (masked)

  v reveal  t/esc close

  This token grants connector access to the tunnel; keep it secret.
```

The token is a live credential: it grants connector access to that one tunnel.
`cftui` never writes it to disk, never logs it, and masks it by default. Rotate
it from the Cloudflare dashboard if it is ever exposed.

---

## How it works

```
cmd/cftui          flag parsing, wiring, -debug output
internal/app       view state machine
internal/cred      origin certificate discovery, PEM decoding, cloudflared detection
internal/cf        Cloudflare API client, ingress rules, DNS planning
internal/local     config.yml load/edit/save, management detection
internal/ui        bubbletea model, views, key handling
internal/validate  tunnel and hostname validation
internal/health    TCP and HTTP probes
```

Management detection is deliberately simple and explainable. For a given tunnel:

1. If its name appears in `config.yml` → `MANUAL` (`local`).
2. Else if a matching credentials file `~/.cloudflared/<id>.json` exists →
   `MANUAL` (`local`).
3. Else, given a tunnel ID → `MANAGED` (`remote`).
4. With no tunnel ID at all → `UNKNOWN`.

If the two sides disagree about hostnames, `cftui` tells you instead of guessing
which one wins.

---

## Security

- **The API token is never logged.** It is decoded from the certificate into a
  struct whose `String`/`GoString` methods redact it, so it is safe to print with
  any `fmt` verb, including `%v` and `%#v`.
- **No token is read from the environment.** `CLOUDFLARE_API_TOKEN` is rejected
  outright (see [Authentication](#authentication-two-different-tokens-one-job-each)).
- **Errors never echo secrets.** Certificate parse failures describe the
  structure, not the contents.
- **Config edits are atomic.** Write to a temp file, rename into place, keep a
  `.bak`.

---

## Development

```sh
gofmt -l .              # should print nothing
go vet ./...
go build ./...
go test ./...
go test -race ./...
```

`internal/cf` and `internal/local` are pure where it matters: DNS planning,
ingress ordering, config editing, and management detection are functions over
plain data with no I/O, so they are tested directly rather than through mocks.
The API client is exercised against a recording double, which means the tests
run without a Cloudflare account.

---

## Status

Pre-1.0, and usable. The feature set is implemented and the full suite passes,
including `-race`.

Verified end to end against a live Cloudflare account: with an origin
certificate present, `cftui -debug` lists real tunnels read from the API, and
the badge for a dashboard-managed tunnel reads `MANAGED` rather than `UNKNOWN`.
The local `config.yml` path is likewise exercised against real files — backup,
atomic write, and `cloudflared tunnel ingress validate` after write.

A tunnel with no ingress reports **no routes** rather than a synthesized
catch-all, and the help overlay is modal, so no keypress can mutate state while
the reference is on screen.

---

## Contributing

Issues and pull requests are welcome. Before opening a PR:

- `gofmt -l .` prints nothing
- `go vet ./...` is clean
- `go test ./...` passes

Please don't weaken or skip tests to make a suite pass — if a test is wrong,
fix the test and say why in the PR description.

---

## License

[MIT](LICENSE)

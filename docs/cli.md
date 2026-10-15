# CLI reference

```
cftui [flags]
```

`cftui` is a terminal UI. It has only four flags and no subcommands; the work
happens interactively once it starts.

## Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `-origincert <path>` | string | see below | Path to the cloudflared origin certificate |
| `-config <path>` | string | see below | Path to the cloudflared `config.yml` |
| `-debug` | bool | `false` | Print the tunnel list and exit; do not start the TUI |
| `-version` | bool | `false` | Print the version and exit |

### `-origincert`

Explicit path to the origin certificate. When omitted, `cftui` checks
`$TUNNEL_ORIGIN_CERT` and then `~/.cloudflared/cert.pem`.

### `-config`

Explicit path to the cloudflared `config.yml`. When omitted, `cftui` checks
`$TUNNEL_CONFIG` and then `~/.cloudflared/config.yml`.

### `-debug`

Prints the tunnel list as plain text — name, status, whether it is managed
locally or remotely, connection count, and route count — then exits with status
0. Useful for scripting and for confirming which backend `cftui` selected without
starting a full-screen app.

```console
$ cftui -debug
NAME                           STATUS     MANAGED    CONNS  ROUTES
CFTUI                          healthy    remote         4       2
```

### `-version`

Prints one line and exits:

```console
$ cftui -version
cftui v1.2.3 abc1234 2026-10-15T00:00:00Z
```

In a plain `go build` there is no release metadata, so it prints `cftui dev`.

## Environment variables

| Variable | Read by | Effect |
|---|---|---|
| `TUNNEL_ORIGIN_CERT` | `cftui` | Default origin certificate path |
| `TUNNEL_CONFIG` | `cftui` | Default `config.yml` path |
| `CLOUDFLARED` | `cftui` | Absolute path to the `cloudflared` binary; otherwise `PATH` is searched |
| `CLOUDFLARE_API_TOKEN` | `cftui` | **Rejected.** If set, the remote backend is disabled and `cftui` explains why. See [Authentication](authentication.md) |

Precedence for file paths is always: explicit flag, then environment variable,
then the built-in default under `~/.cloudflared/`.

## Exit behaviour

| Situation | Result |
|---|---|
| Normal quit (`q` or `ctrl+c`) | exit 0 |
| `-debug` | prints list, exit 0 |
| `-version` | prints version, exit 0 |
| Unknown flag | flag error, exit 2 |
| `CLOUDFLARE_API_TOKEN` set | starts local-only, prints `remote backend unavailable, listing local config only: cred: CLOUDFLARE_API_TOKEN is not supported; run: cloudflared tunnel login` to stderr |
| `cloudflared` not found | starts in local-only mode; connector-token display is unavailable |

# Configuration

`cftui` reads and writes the same `config.yml` that `cloudflared` uses. There is
no separate config format and no `cftui`-specific settings file.

## Locating config.yml

Resolution order:

1. `-config <path>`
2. `$TUNNEL_CONFIG`
3. `~/.cloudflared/config.yml`

If none exists, `cftui` runs with an empty local view; the remote backend still
works as long as you have an origin certificate.

## What an ingress block looks like

```yaml
tunnel: 221cc559-dac2-4ee2-9606-17a7ffd038fd
credentials-file: /home/you/.cloudflared/221cc559-....json

ingress:
  - hostname: app.example.com
    service: http://localhost:8080
  - hostname: ssh.example.com
    service: ssh://localhost:22
  - service: http_status:404
```

Two things matter to `cftui`:

- **The catch-all must be last.** Cloudflare requires a rule with no `hostname`
  as the final entry, and rejects a config where it is not last. `cftui` enforces
  this on every write — drag a route below the catch-all with `J` and `cftui`
  keeps the catch-all pinned at the bottom.
- **`service` needs a host.** `http://10000` is invalid; it must be
  `http://localhost:10000`. A service URL with no host is accepted by the
  config parser but fails when the connector tries to dial it.

## What cftui changes

Changes you stage in the route editor are held in memory until you press `w`.
Only then does `cftui` write them — to the account API when it has an origin
certificate, and to your local `config.yml` otherwise. Pressing `u` discards
staged changes and re-reads the source of truth.

`cftui` preserves fields it does not understand: unknown keys in a rule survive
a round-trip through the editor.

## Reading is not writing

Opening a tunnel with no ingress shows *no routes*. `cftui` does not synthesise
a catch-all row on read, because that rule does not exist in your config and
displaying it would imply otherwise. The catch-all is added only when you write
a config that needs one.

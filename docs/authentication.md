# Authentication

Cloudflare's tunnel tooling involves **two different tokens**. They look
similar but do completely different jobs, and mixing them up is a common source
of confusion. `cftui` treats them very differently on purpose.

## The origin certificate (what cftui authenticates with)

Running

```sh
cloudflared tunnel login
```

once opens a browser, you pick a zone, and cloudflared writes
`~/.cloudflared/cert.pem`. That file contains an OAuth certificate scoped to the
zone you selected, and the certificate embeds an API token. `cftui` reads that
token and uses it for every Cloudflare API call. It does not mint a token or
exchange the certificate for one.

Properties:

- It lives on disk at a well-known path; `cftui` never writes a second copy.
- It is scoped to the account and zone you chose during login.
- It is the **only** credential `cftui` will authenticate with.

`cftui` locates the certificate in this order:

1. `-origincert <path>`
2. the `TUNNEL_ORIGIN_CERT` environment variable
3. `~/.cloudflared/cert.pem`

## The connector token (what runs a tunnel)

Every tunnel has a *connector token* — a long JWT. You paste it into a machine
that should host the tunnel's connector:

```sh
cloudflared tunnel run --token <connector-token>
```

It is the credential that lets a machine *join* a tunnel. It is not an API
credential and grants no account-level access.

`cftui` can **show** you this token (press `t` on the tunnel list) so you can
copy it to another machine. It comes from the account API when `cftui` has an
origin certificate, and falls back to the local `cloudflared tunnel token`
subcommand otherwise.

`cftui` will **never authenticate with a connector token.** It cannot list
tunnels, read configs, or change DNS on one. `cftui` also does not rotate,
revoke, or delete connector tokens — Cloudflare exposes no API for that; the
dashboard is the only place to do it.

## Why cftui refuses CLOUDFLARE_API_TOKEN

If `CLOUDFLARE_API_TOKEN` is set in the environment, `cftui` **refuses to start**
its remote backend and tells you so. This is deliberate:

- An API token is a broader, longer-lived credential than an origin cert.
- Silently preferring one would mean the UI's behaviour depended on invisible
  shell state.
- A single-purpose tool that only knows one credential is far easier to audit.

If you genuinely need a different credential scope, log in again with
`cloudflared tunnel login` and select the right zone.

## Summary

| | Origin certificate | Connector token |
|---|---|---|
| Created by | `cloudflared tunnel login` | Cloudflare, per tunnel |
| Stored at | `~/.cloudflared/cert.pem` | not stored by cftui |
| Scope | account + zone | one tunnel's connector |
| cftui authenticates with it | **yes** | never |
| cftui can display it | n/a | yes (`t`) |

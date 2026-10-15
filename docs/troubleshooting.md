# Troubleshooting

## `remote backend unavailable, listing local config only`

`cftui` could not build an API client. The message on stderr says why. The two
causes are:

- **No origin certificate.** Run `cloudflared tunnel login` and pick the zone.
  Check it landed at `~/.cloudflared/cert.pem`, or pass `-origincert`.
- **`CLOUDFLARE_API_TOKEN` is set.** This is refused by design; see
  [Authentication](authentication.md). Unset it and restart:

  ```sh
  unset CLOUDFLARE_API_TOKEN
  ```

## `cred: origin certificate at <path> is incomplete`

The PEM at that path is incomplete. Re-run `cloudflared tunnel login` and select
the zone again.

## A tunnel shows status `unknown`

`unknown` means the tunnel is neither in the account's tunnel list nor backed by
a local credentials file. Usually the credentials file is a leftover from a
tunnel that was never registered. To confirm what the account actually holds:

```sh
cloudflared tunnel list
```

If the tunnel is not listed there, `cftui` cannot manage it remotely; delete the
stale `~/.cloudflared/<uuid>.json` if you no longer need it.

## A route reaches Cloudflare but the site 502s

The ingress is valid but nothing is listening on the origin. Check the `service`
value:

```yaml
- hostname: app.example.com
  service: http://localhost:8080   # host required
```

`http://8080` or `http://10000` are invalid — they have no host, and the
connector cannot dial them. The 502 is Cloudflare reporting that the origin
refused the connection; start your service (or fix the port) and retry.

Confirm something is listening locally:

```sh
curl -sS -o /dev/null -w '%{http_code}\n' http://localhost:8080
```

## `cloudflared not found`

`cftui` looks for `cloudflared` on your `PATH`. If it lives elsewhere, point at
it explicitly:

```sh
CLOUDFLARED=/opt/cloudflared cftui
```

Without `cloudflared`, creating/deleting tunnels and the connector-token
fallback are unavailable; account operations still work if you have a cert.

## `tunnel with name already exists`

You asked to create a tunnel whose name is already registered. The existing
tunnel may not be visible if your origin certificate is scoped to a different
zone. Run `cloudflared tunnel list` to see what is actually registered.

## The help panel seems to swallow keys

That is correct behaviour. Help is modal: while it is open only `?`, `esc`, `q`,
and `ctrl+c` are handled. Press `esc` or `q` to close it and resume.

## Nothing happens when I press `t`

Select a tunnel first. `t` needs a highlighted row; with an empty list it prints
`select a tunnel first`.

## Staged changes vanished

`u` discards staged changes and re-reads the source of truth. This is
intentional. If you meant to keep them, press `w` to apply before navigating
away — leaving the view without `w` drops the staging area.

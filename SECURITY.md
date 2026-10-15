# Security policy

## Reporting a vulnerability

Please do not open a public issue for a security problem. Report it privately
through GitHub's [security advisory](https://docs.github.com/en/code-security/security-advisories)
form on the repository, or email the maintainer.

Include what you did, what happened, and what you expected. A proof of concept
helps.

## Design posture

`cftui` is built to hold as few secrets as possible.

- **One credential.** It authenticates only with the OAuth origin certificate
  produced by `cloudflared tunnel login`. An API token in
  `CLOUDFLARE_API_TOKEN` is refused rather than used, so the UI's authority is
  never broader than the certificate you already created.
- **No secret persistence.** `cftui` reads the origin certificate and uses the
  API token embedded in it, in memory. It writes no credential to disk.
- **No secret logging.** The origin certificate type redacts its token in every
  `fmt` verb, so an accidental `%v` on a config struct cannot leak it.
- **Tokens are masked.** The connector-token panel shows a masked form by
  default and requires an explicit `v` to reveal.
- **Destructive actions are typed, not keyed.** Deleting a tunnel requires
  typing its exact name; a stray keystroke cannot destroy anything.

## Scope

The origin certificate carries the authority of the zone it was issued for.
Anyone who can read `~/.cloudflared/cert.pem` can act on your account through
`cftui` or `cloudflared` alike. Protect that file with normal filesystem
permissions; the tooling does not add a second factor.

`cftui` shells out to the `cloudflared` binary found on `PATH` or named by
`CLOUDFLARED`. If an attacker can replace that binary, they run as you —
`cftui` cannot defend against a compromised toolchain, and does not claim to.

## Supported versions

While the project is pre-1.0, only the latest release receives security fixes.

# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-10-15

### Added

- Terminal UI over Cloudflare Zero Trust tunnels: list, create, delete, and edit
  ingress routes.
- OAuth origin-certificate authentication; API tokens are refused.
- Local-vs-remote management badges.
- Connector-token display (masked, reveal with `v`).
- Connector-token management: list, create, rename, and delete tokens.
- Local `config.yml` editing with catch-all enforcement and reload pipeline.
- `-version` flag, reporting linker-injected version, commit, and build date.
- Multiple-platform release builds (Linux, macOS, Windows on `amd64` and
  `arm64`) via goreleaser.
- Continuous integration across Linux, macOS, and Windows, plus a cross-compile
  matrix.
- Documentation under `docs/`: installation, authentication, CLI, keys,
  configuration, architecture, troubleshooting, and development.
- `SECURITY.md` describing the credential model and disclosure process.

### Changed

- The `?` help panel is now modal: while it is open, only `?`, `esc`, `q`, and
  `ctrl+c` are handled, so list keys can no longer act behind it.

### Fixed

- Opening a tunnel with no ingress no longer synthesises a catch-all route for
  display. The read path shows exactly what the config holds; the catch-all is
  added only on write.
- The ROUTES column no longer follows a stale count when the route list is
  empty.

[Unreleased]: https://github.com/MasFana/cftui/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/MasFana/cftui/releases/tag/v0.1.0

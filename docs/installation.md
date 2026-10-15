# Installation

## Prebuilt binaries

Every tagged release publishes static binaries for:

| OS | Architectures |
|---|---|
| Linux | `amd64`, `arm64` |
| macOS | `amd64` (Intel), `arm64` (Apple silicon) |
| Windows | `amd64`, `arm64` |

Download the archive matching your platform from the
[releases page](https://github.com/MasFana/cftui/releases), verify it against
the published `checksums.txt`, extract the `cftui` binary, and put it on your
`PATH`.

The binaries are built with `CGO_ENABLED=0`, so the Linux builds are fully
static and run on any distribution regardless of its libc.

```sh
# verify a download
sha256sum --check checksums.txt --ignore-missing
```

## Install with Go

Requires Go 1.24 or newer.

```sh
go install github.com/MasFana/cftui/cmd/cftui@latest
```

This installs to `$(go env GOPATH)/bin`, which is usually already on your
`PATH`. Binaries installed this way report `cftui dev` for `-version` because no
release metadata was injected at link time.

## Build from source

```sh
git clone https://github.com/MasFana/cftui
cd cftui
go build -o cftui ./cmd/cftui
```

To reproduce a release build, including the version stamp:

```sh
go build -trimpath -ldflags "-s -w \
  -X main.version=v1.0.0 \
  -X main.commit=$(git rev-parse --short HEAD) \
  -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o cftui ./cmd/cftui
```

## Prerequisites

`cftui` orchestrates `cloudflared`; it does not replace it.

1. Install [`cloudflared`](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/)
   and make sure `cloudflared` is on your `PATH` (or set `CLOUDFLARED` to its
   absolute path).
2. Authenticate once so an origin certificate exists:

   ```sh
   cloudflared tunnel login
   ```

   This opens a browser and writes `~/.cloudflared/cert.pem`.

Without an origin certificate `cftui` still runs, but it is limited to editing
your local `config.yml` and cannot list tunnels from, or push anything to, your
Cloudflare account.

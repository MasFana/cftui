# cftui documentation

`cftui` is a terminal UI for [Cloudflare Zero Trust](https://developers.cloudflare.com/cloudflare-one/)
tunnels. It authenticates with the OAuth origin certificate that `cloudflared`
already installed on your machine, so you never paste an API token into it.

| Document | What it covers |
|---|---|
| [Installation](installation.md) | Prebuilt binaries, `go install`, building from source |
| [Authentication](authentication.md) | Origin cert vs. connector token, and why they differ |
| [CLI reference](cli.md) | Flags, environment variables, exit behaviour |
| [Key reference](keys.md) | Every key, per view |
| [Configuration](configuration.md) | How `config.yml` is found and edited |
| [Architecture](architecture.md) | Packages, data flow, invariants |
| [Troubleshooting](troubleshooting.md) | Common errors and what they mean |
| [Development](development.md) | Layout, tests, release process |

If you only read one thing, read the [README](../README.md).

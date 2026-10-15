# Development

## Setup

```sh
git clone https://github.com/MasFana/cftui
cd cftui
go build -o cftui ./cmd/cftui
go test ./...
```

Requires Go 1.24 or newer (`go.mod` pins the minimum).

## Layout

See [architecture.md](architecture.md) for the package map and the data flow.
The short version: `cmd/cftui` wires everything together, `internal/ui` owns the
terminal, and `internal/cf` / `internal/local` own the two backends.

## The gate

Every change must pass all of these. CI runs the same checks.

```sh
gofmt -l .                 # must print nothing
go vet ./...               # must be silent
go build ./...             # must succeed
go test -count=1 ./...     # must pass
go test -race -count=1 ./internal/ui/ ./internal/cf/
```

Never weaken, skip, or delete a test to make the gate pass. If a test is wrong,
fix the test and say why; if the code is wrong, fix the code.

## Testing philosophy

- **Drive the model, not the terminal.** Call `Update` with `tea.KeyMsg` values
  and inspect the view string. Feeding keystrokes to a live bubbletea program is
  timing-dependent and flaky.
- **Mutation-test behavioural tests.** Before trusting a new test, break the
  production code it covers, confirm the test fails, then restore. A test that
  passes against broken code proves nothing.
- **Prefer real I/O over mocks for the credential layer.** Certificate handling
  is exercised against real files in temp dirs.

## Style

- `gofmt` is not negotiable.
- Comments explain *why*, not *what*. The code already says what.
- Errors carry context: `fmt.Errorf("tunnel token %s: %w", id, err)`, not
  `return err`.

## Releases

Releases are automated. To cut one:

1. Make sure `main` is green.
2. Tag and push:

   ```sh
   git tag -a v1.0.0 -m "v1.0.0"
   git push origin v1.0.0
   ```

3. The [release workflow](../.github/workflows/release.yml) runs
   [goreleaser](../.goreleaser.yaml), which builds Linux/macOS/Windows binaries
   for `amd64` and `arm64`, writes `checksums.txt`, and creates the GitHub
   release.

To preview the exact artifacts locally without publishing:

```sh
goreleaser release --snapshot --clean
```

Artifacts land in `dist/`. Everything is built with `CGO_ENABLED=0`, so the
Linux binaries are static.

The version a binary reports comes from linker flags set in
`.goreleaser.yaml`; `main.version`, `main.commit`, and `main.date` are empty in a
plain `go build`, and `cftui -version` prints `cftui dev` as a result.

## Continuous integration

[ci.yml](../.github/workflows/ci.yml) runs the gate on Linux, macOS, and Windows
for every push and pull request, plus a cross-compile matrix covering all six
target platform/architecture pairs.

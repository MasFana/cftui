# Contributing

Thanks for taking the time to contribute.

## Before you start

For anything larger than a bug fix, open an issue first so we can agree on the
shape of the change before you write it. It is much easier to redirect a plan
than a pull request.

## The gate

Your change must pass all of:

```sh
gofmt -l .                 # prints nothing
go vet ./...               # silent
go build ./...             # succeeds
go test -count=1 ./...     # passes
go test -race -count=1 ./internal/ui/ ./internal/cf/
```

CI runs the same checks on Linux, macOS, and Windows, and cross-compiles for
every released platform. A red CI is a red change.

Never make a suite pass by skipping, deleting, or weakening a test. If a test is
wrong, fix it and explain why in the commit message.

## Tests

Drive the bubbletea model directly with `tea.KeyMsg`; do not try to script a
real terminal. Before trusting a new behavioural test, break the production code
it covers, watch the test fail, then restore the code — a test that passes
against broken code is not protecting anything.

## Commits

Write commits that explain *why* the change exists, not just what it does. Keep
them focused; a commit that does three unrelated things is three commits. The
repository history is meant to be readable start to finish.

## Documentation

User-facing behaviour belongs in `docs/`. If you change a flag, a key binding, a
default path, or an error message, update the matching document in the same
commit — the docs describe what the code actually does, and drift is a bug.

## Reporting bugs

Open an issue with your platform, the `cftui -version` output, what you ran, and
what happened. For anything security-related, follow [SECURITY.md](SECURITY.md)
instead of opening a public issue.

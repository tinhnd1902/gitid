## What and why

<!-- What does this change, and why? Link the issue it closes, e.g. "Closes #123". -->

## How I tested it

<!-- Commands you ran and what you saw. Screenshots or a short recording help for TUI changes. -->

## Checklist

- [ ] `go build ./...`, `make lint` and `go test -race ./...` pass locally
- [ ] `make test-integration` and `make check-home` pass
- [ ] New or changed behavior has tests (and `go test ./internal/tui -update` was used only on purpose when a golden file changed)
- [ ] Tests use `testutil.TempEnv` (a temporary HOME) and never touch the real HOME or the network
- [ ] User-facing text goes through `internal/i18n` with both `vi` and `en` strings
- [ ] Docs and `CHANGELOG.md` (Unreleased) are updated when behavior changes
- [ ] No real personal data in code, fixtures or docs (use `alice@example.com`, `acme`, `git.example.com`)

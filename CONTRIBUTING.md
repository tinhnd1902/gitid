# Contributing to gitid

Thanks for helping! This guide is short on purpose. By taking part you agree to
follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## Workflow: fork, branch, pull request

1. **Fork** the repository on GitHub and clone your fork.
2. Create a **branch** from `main` with a descriptive name, for example
   `fix/ssh-include-order` or `feat/gitea-orgs`.
3. Make your change in small commits. Keep one topic per pull request.
4. Run the checks below, push the branch to your fork and open a **pull
   request** against `main`. Fill in the template; link the issue it closes.
5. Maintainers review; please keep the discussion on the pull request. Expect
   a few rounds of feedback.

For anything larger than a bug fix, open an issue first so we can agree on
the approach before you spend time on it.

## Setup

You need Go (the version in [`go.mod`](go.mod)), git 2.36 or newer and `make`.
Optional: [golangci-lint](https://golangci-lint.run/) for `make lint`.

```bash
git clone https://github.com/<your-user>/gitid.git
cd gitid
make build            # builds bin/gitid
```

## Make targets

| Target                  | What it does                                                                  |
| ----------------------- | ----------------------------------------------------------------------------- |
| `make build`            | Build `bin/gitid` (version via `VERSION=...`, default `dev`)                  |
| `make test`             | `go test -race ./...`                                                         |
| `make test-integration` | `go test -tags integration ./...` (real git, temporary HOME, offline)         |
| `make cover`            | Coverage per package; fails if a core package drops below its threshold       |
| `make vet`              | `go vet ./...`                                                                |
| `make fmt-check`        | Fails if `gofmt -l .` lists files                                             |
| `make lint`             | `golangci-lint run` if installed, otherwise `make vet fmt-check`              |
| `make check-home`       | Fails if code outside `cmd/` and `internal/env` reads HOME / XDG directly     |
| `make release-snapshot` | Local GoReleaser dry run (only defined when `goreleaser` is installed)        |
| `make install`          | `go install` the binary                                                       |

Before you open a pull request, run at least:

```bash
go build ./... && make lint && go test -race -count=1 ./... && make test-integration && make check-home
```

## Test rules

These exist to protect your own machine. They are not optional.

- **Never touch the real HOME.** Every test builds its environment with
  `testutil.TempEnv(t)` (HOME is `t.TempDir()`, and `GIT_CONFIG_GLOBAL` and
  `GIT_CONFIG_NOSYSTEM=1` are set). Production code receives paths through
  `env.Env`; only `cmd/` and `internal/env/fromos.go` may read `HOME` or
  `XDG_CONFIG_HOME` from the process (`make check-home` enforces it).
- **No network in tests**, and no real `ssh`, `ssh-keygen`, `pbcopy` or `open`.
  Go through `execx.Runner` and use `execx.Fake`. Integration tests
  (`//go:build integration`) may run real `git`, but only on local repositories.
- **Never read private keys.** Only `Lstat` is allowed on a private key file;
  only `.pub` files are read.
- **Use example data.** Fixtures and docs use `alice@example.com`, `acme`,
  `git.example.com`, never real people, companies or hosts.
- Add or update tests for every behavior you change. Do not weaken or skip a
  test to make it pass; if you think a test is wrong, say why in the pull
  request.
- TUI tests drive the model with Bubble Tea messages. The golden file in
  `internal/tui/testdata/` is refreshed on purpose with
  `go test ./internal/tui -run Golden -update`; review the diff before you
  commit it.

## Code style

- `gofmt` formatting (see `.editorconfig`); the lint configuration is in
  `.golangci.yml`.
- User-facing text lives in `internal/i18n` and needs both a Vietnamese and an
  English string with the same printf verbs. A test checks this.
- `internal/cli` and `internal/tui` talk to the rest of gitid only through
  `internal/app`. Do not call `git`, `ssh` or the file system from them
  directly.
- Wrap errors with context, and prefer sentinel errors that callers can test
  with `errors.Is`.

## Recording the demo

The README hero animation is produced with [vhs](https://github.com/charmbracelet/vhs)
from [`docs/demo.tape`](docs/demo.tape). The tape runs the TUI against a
temporary HOME, so your own configuration is never shown or modified:

```bash
make build
vhs docs/demo.tape      # writes docs/img/hero.gif
```

Then uncomment the hero line in `README.md` and `README_VI.md`.

## Releasing (maintainers)

1. Update `CHANGELOG.md`: move the Unreleased entries under the new version.
2. Tag `vX.Y.Z` and push the tag.
3. Run `goreleaser release --clean` (see `.goreleaser.yaml`).

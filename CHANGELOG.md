# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-11-16

First public version.

### Added

- Terminal UI (`gitid` without arguments): a tree of companies and accounts
  with fuzzy filter and a detail pane, validated add/edit/delete forms, a diff
  preview before every write, SSH key creation with a public-key screen,
  clone with live progress, repository check with remote repair, a doctor with
  per-check progress, and an import screen. Light and dark terminals are
  supported; the layout adapts from 80x24 up.
- Command line: `list`, `check`, `clone`, `apply`, `doctor`, `fix-remote`,
  `key gen`, `import` and `version`, with `--json` output where it makes sense
  and exit codes 0 (ok), 1 (error), 2 (problems found). `check --json` reports
  warnings as `{code, message}` objects.
- Identity selection with plain git features: one `gitdir:` include per
  company and `hasconfig:remote.*.url:` includes per account, generated into
  `$XDG_CONFIG_HOME/gitid/git/` and a single managed block in `~/.gitconfig`.
  Needs git 2.36 or newer.
- SSH support: an ed25519 key per account under `~/.ssh/gitid/`, Host aliases
  in `~/.ssh/config.d/gitid.conf`, and one managed `Include` line at the top
  of `~/.ssh/config`.
- Platforms: GitHub, GitLab (including self-hosted), Bitbucket, Gitea and
  Azure DevOps (including legacy `visualstudio.com` URLs).
- Safety: unified-diff dry run, timestamped backups with a manifest (newest 30
  kept), atomic writes that follow symlinks and keep file modes, stale-plan
  detection, a process lock, idempotent managed blocks, and removal of all
  gitid content when the last company is deleted.
- Vietnamese (default) and English interface (`settings.language` or the
  `GITID_LANG` environment variable).
- Open-source project files: README (English and Vietnamese), CI on macOS and
  Linux, GoReleaser configuration, contribution and security guides.
- Release automation: pushing a `vX.Y.Z` tag runs a GitHub Actions workflow
  that builds macOS and Linux (amd64 and arm64) archives with
  `checksums.txt` and publishes a GitHub release.
- Homebrew: `brew install tinhnd1902/tap/gitid` (macOS cask, updated by the
  release workflow when the tap token secret is configured).
- Install script: `curl -fsSL https://raw.githubusercontent.com/tinhnd1902/gitid/main/install.sh | sh`
  installs the latest release for macOS and Linux (amd64 and arm64), verifies
  its sha256 and puts `gitid` on your `PATH`. `VERSION`, `INSTALL_DIR` and
  `NO_MODIFY_PATH=1` are supported.

### Security

- `gitid key gen` refuses passphrase mode when stdin is not a terminal (an
  empty passphrase could be created silently); scripts must pass
  `--no-passphrase --yes`. The clipboard is used only on a terminal or with
  `--copy`.
- Private keys are never read, printed or overwritten; only `.pub` files are
  read. Deleting a key needs a separate typed confirmation.
- Passwords or tokens inside pasted URLs are dropped and masked.
- Every value written to git or ssh configuration is validated and escaped;
  clone arguments are always passed after `--`.

[Unreleased]: https://github.com/tinhnd1902/gitid/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/tinhnd1902/gitid/releases/tag/v0.1.0

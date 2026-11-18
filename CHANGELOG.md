# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- TUI help bar: every screen now has a two-row bar (one row below 14 terminal
  rows). Row 1 holds the keys of the selected row or step (`▸` marks the next
  step), row 2 the keys that are always available, then `? all keys`, then the
  exit key last. On the list, row 2 shows `↑↓ move`, `A add company` and
  `a add account`. The row labels show only while every key of the row fits
  with them. The body is one row shorter in two-row mode.
- TUI focus: the list pane of the list screen has a heavy border (and the
  hint `(↑↓ select)` when it has rows to move over); the detail pane has a thin
  one.
- TUI `?` overlay is searchable: type to filter by description (accents and
  capitals are ignored, Vietnamese input works) or key, `↑`/`↓` select,
  `enter` runs the row exactly like its key. `esc` clears the filter, then
  closes.
- New rebindable action `palette` (default `ctrl+k`, same screens as `help`)
  that opens the overlay. Note: a `keys.yaml` written earlier that binds
  `ctrl+k` to another action in a screen the palette shares now conflicts with
  the new default; that binding is reported and the action keeps its default
  key. Bind `palette` to another key to keep yours.
- Repo browser: the clone key (`c`) now opens a screen with three tabs, Local,
  URL and Online, switched with `tab` and `shift+tab`. It opens on the URL tab,
  which is the clone flow exactly as before (paste, pick the account, clone,
  retry in terminal mode when authentication fails).
- Local tab: gitid scans the base folder of every company in the background
  (up to 4 folders deep and 20 000 folders per company; `node_modules`,
  `vendor` and hidden folders are skipped; no symlink is followed) and lists
  every repository with the account its remote maps to, its status (wrong
  username/alias, unknown account, foreign host, no remote, unreadable remote,
  ambiguous, git error, unsafe owner) and flags (email differs, local email
  override, wrong company folder, cloned twice, symlink; worktree, submodule
  and bare repositories are marked). `k`/`enter` checks a repository, `f` goes
  straight to the fix-remote question, `c` copies the path, `/` filters, `!`
  shows only the problems, `left`/`right` switch company, `r` scans again,
  `esc` stops a scan. One read-only `git config` call per repository, eight at
  a time. The result is kept for the session; a configuration change is
  applied to it without scanning again, and a finished clone or fix updates
  only that repository. Leaving the screen stops the scan.
- Online tab: a placeholder that says listing online repositories is coming
  soon (no network is used).
- New rebindable action `problems` (default `!`) and the `repos` screen scope
  for `filter`, `check`, `fix`, `copy` and `reload` (`gitid keys` lists them).
  Since `filter`, `check`, `fix`, `copy`, `reload` and `problems` now share the
  repo browser screen, they cannot share a key with each other there: for
  example `filter: "f"` is reported as a conflict with `fix` (the default key
  stays).
- README.md and README_VI.md: "Clone and the repo browser".
- Test: the shortcut guard covers the repo browser states and its sentinel
  keys now run through `alt+a` to `alt+z` and then `alt+A` to `alt+Z`.

### Changed

- Internal: `gitid check` and the local scan share one classification, so they
  always agree; the TUI root closes screens that own background work when
  they leave the stack, and broadcasts the results of background work to
  every screen.

## [0.1.6] - 2026-11-18

### Added

- Custom key bindings: an optional file `keys.yaml` next to `config.yaml`
  (`$XDG_CONFIG_HOME/gitid/keys.yaml`, default `~/.config/gitid/keys.yaml`)
  lets you rebind the TUI shortcuts, for example `check: "v"` or
  `clone: ["c", "n"]`. gitid only reads the file: it never creates, rewrites,
  backs up or deletes it, and editing it never makes a save of `config.yaml`
  stale. A key that is invalid, reserved or used twice on one screen, an
  unknown action name or a broken file never stops gitid: the affected
  action keeps its default key and the problem is shown in the status bar, at
  the bottom of the `?` overlay, by `gitid keys` and by `gitid doctor`. The
  help bar, the `?` overlay, the next-step hints, the status badge, the
  welcome steps, the done screen and the discard question all show the keys
  that are bound now. `r` on the list reloads the file; the keys also apply
  when `config.yaml` itself cannot be loaded.
- `gitid keys` prints every action with its keys, default and screens, marks
  the ones changed in `keys.yaml` and lists the problems (exit code 2 when
  there are any); `--json` prints the same as JSON, and `--example` prints a
  template with every line commented out (`mkdir -p ~/.config/gitid &&
  gitid keys --example > ~/.config/gitid/keys.yaml`; it writes no file
  itself).
- `gitid doctor`: new `keybindings` check. A problem in `keys.yaml` is a
  warning, never a failure.
- README.md and README_VI.md: "Custom key bindings" / "Tùy chỉnh phím tắt".
- Test: the shortcut guard also runs with every rebindable action moved to
  its own `alt+<letter>` and fails on any key shown in a frame that is not
  handled there, so a hint can no longer name a hard-coded letter.
- Terminal UI: `q` goes back one step on every screen except the list, where it
  still quits. In a text field `q` is typed as a letter, and the "?" overlay
  closes with `q` too. It is ignored while gitid is busy (applying, cloning,
  generating a key, saving).
- Terminal UI: `r` on the list reloads `config.yaml` from disk (for example
  after editing it by hand) and says so in the status bar. A file that cannot
  be read keeps the list you already see and shows the error.
- Terminal UI: leaving a company, account or import form with `esc` (or `q` on
  a choice field) after changing a value asks "Discard what you typed? y/n"
  first. A form you did not change closes at once, as before.
- Terminal UI: a Vietnamese input method that is switched on sends composed
  letters (`â`, `đ`, `ơ`) or several letters at once; on screens without a text
  field gitid now says "switch to EN to use shortcuts" instead of ignoring them.
- Terminal UI: `enter` on the welcome screen adds the first company, and the
  clone screen starts on the account that was highlighted in the list (the
  default account of a company row).
- Test: a guard that opens every screen state and presses each key its help
  bar, its "?" overlay and its next-step text advertise, so a shortcut can
  never be shown without being handled.

### Fixed

- Terminal UI: `esc` in the fix-remote confirmation returned to the list and
  dropped the check result; it now goes back to the check result. The same
  for `esc` in the "create the key without a passphrase" confirmation, which
  now returns to the choice of mode.
- Terminal UI: the help bar offered `? all keys` on screens where `?` is typed
  into a text field; it is hidden there.

### Changed

- Terminal UI: the key hints in messages ("press p", "bấm k", the welcome
  steps, the status badge) are built from the key bindings in effect instead
  of fixed letters; the text is the same with the default keys.
- Terminal UI: the selected Yes/No button also shows a `▸` marker, so it does
  not depend on color alone. The faint text color is darker on light
  terminals and lighter on dark ones (contrast of at least 4.5:1).
- Terminal UI: the "?" overlay lists the keys of every screen (`esc`/`q`
  back, `↑`/`↓`, `pgup`/`pgdn`, `home`/`end`, `?`, `ctrl+c`) and the `r`
  reload key on the list; `enter` on the apply review says "press y to apply";
  the SSH step on the done screen says how to reach the account (enter to the
  list, select it, then `g`).
- Terminal UI: the forms (company, account, import) use a built-in form
  component instead of the `huh` library. Hints and errors never split a URL
  or an email address, validation errors appear right under the field, the
  form scrolls to the focused field and shows its position (`n/m`) when it
  does not fit, the remote preview stays visible while you type, and a
  resize re-wraps everything at once. One dependency fewer (`huh` and the
  libraries only it needed).

## [0.1.5] - 2026-11-17

### Improved

- Terminal UI, account form: a live preview shows the remote the account will
  produce (`https://alice@github.com/<owner>/<repo>.git`, or the SSH alias
  form, rendered by the same code as clone and fix-remote) and the gitid ID
  derived from the username. The username hint now says it is the login in
  your profile URL, not an email or display name; the Git name hint says it
  is not the username; the email hint says gitid does not verify it and points
  to the platform's noreply address.
- Terminal UI, account form: host, orgs and SSH host/port are behind one
  "Advanced settings?" question (off by default; on for Gitea and for accounts
  that already use them). The default-account question only appears when the
  company has another account. The host placeholder is an example (`vd:`).
- Terminal UI, after apply: a "Next steps" section says to clone into the
  company folder (`c`), check a repo (`k`), what to expect on the first HTTPS
  clone or push (Personal Access Token, credential helper) and that gitid
  verifies nothing with the platform. `c` opens clone from the done screen.
- Terminal UI, account detail: shows the remote, the folder, what HTTPS asks
  for and the next step; long values wrap instead of being cut. The stacked
  layout (80x24) gives the detail pane the rows the list does not need.
- Terminal UI, `k` outside a repository says which folder was checked and what
  to do (open a terminal in a repo, or press `c` to clone), instead of only
  offering to retry. The detail screen handles `c` too.
- Terminal UI: the selected account row drops the email, not the ID, when the
  row is narrow; the clone URL placeholder is an example; the clone
  authentication hint mentions Personal Access Tokens; an import with nothing
  found says how to add a company by hand; the `?` overlay uses columns so the
  global keys stay visible on small terminals; resizing an open form re-wraps
  its hints.

### Fixed

- Terminal UI: adding a second account with the same platform, host and
  username to one company (for example `tinhnd` and `tinhnd-github`) is
  refused with a pointer to the existing account; an email typed as a GitHub,
  GitLab or Gitea username is refused. The same login in another company only
  warns. Accounts that are already duplicated are flagged with `!` in the list
  and in the detail pane.
- Terminal UI: saving, deleting, importing and key generation no longer
  overwrite a `config.yaml` that changed since the TUI loaded it (for example
  from a second gitid window or `gitid import`). The change is refused, the
  list reloads and the form stays open to save again.
- Changing the username of an account now explains that repos cloned with the
  old username need `gitid fix-remote`.
- Account IDs derived from usernames are always valid (`john.doe` becomes
  `john-doe`, runs of other characters collapse, at most 32 characters,
  collisions get `-<platform>`, `-<platform>-2`, ...); before, such a username
  failed on save with a message about a field the form never showed. The
  importer uses the same rule.

## [0.1.4] - 2026-11-17

### Fixed

- Terminal UI: the detail screen now handles `a` (add an account to the row's
  company), `p` (apply), `k` (check) and `D` (doctor), and its help bar shows
  the next step first. The keys named in its hints used to do nothing.
- Terminal UI: navigation started by a key is applied immediately, so fast
  typing, paste or IME input can no longer stack a detail screen on top of a
  form or send letters to the wrong screen.
- Terminal UI: after saving an account, the list selects that account when you
  return to it.
- Terminal UI: the status badge only says "press X" on the list and detail
  screens; on other screens it shows the same state without a key.

## [0.1.3] - 2026-11-17

### Improved

- Terminal UI, first-run experience: a welcome guide with the three steps
  (add a company, add an account, preview and apply) when nothing is set up.
- Creating a company no longer opens an empty "nothing to apply" screen or
  shows "In sync": the list selects the new company and says to press `a` to
  add an account. A company without account is labelled `no accounts yet`, and
  the status badge, the detail pane and the help bar all point to the next key.
- The status badge now answers "what next": unapplied changes (highlighted),
  companies without an account, a failed plan, or "Applied, nothing pending"
  only when that is true.
- The help bar shows 4 to 6 keys for the current state with the next step
  first (`▸`), and never truncates with an ellipsis; `?` still lists all keys.
- The detail pane explains what will happen for the selected company or
  account, and appears from 84 columns (beside the list) or from 60x24 (below
  it); `enter` opens it on smaller terminals.
- Forms: placeholders read "e.g. ..." in italics, an empty required field says
  what to enter, a duplicate slug names the slug, the base folder hint follows
  the slug as you type, and the platform, email and auth fields explain
  themselves.
- Apply: a plain-language summary above the diff; after applying, the screen
  says which account each folder now uses, how to verify it (`k` opens the
  check) and which repos keep their own `user.email`.
- After deleting or importing, the confirm screen is built from the saved
  configuration (it could show an empty diff when the two raced).

## [0.1.2] - 2026-11-16

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

[Unreleased]: https://github.com/tinhnd1902/gitid/compare/v0.1.6...HEAD
[0.1.6]: https://github.com/tinhnd1902/gitid/compare/v0.1.5...v0.1.6
[0.1.5]: https://github.com/tinhnd1902/gitid/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/tinhnd1902/gitid/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/tinhnd1902/gitid/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/tinhnd1902/gitid/releases/tag/v0.1.2

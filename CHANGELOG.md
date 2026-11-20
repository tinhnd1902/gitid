# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.10] - 2026-11-20

### Added

- Settings screen: the start menu has an eighth row, Settings (`8`), with the
  interface language as a radio (Tiếng Việt, English). `←`, `→` and `enter`
  switch it; the whole interface changes at once and the choice is saved to
  `settings.language` in `config.yaml` (not a pending change for `apply`).
  When `GITID_LANG` is set, the screen says gitid uses it again at the next
  start. A new `settings` scope in the key map carries the help, palette and
  back keys.

### Changed

- Start menu: the logo is now a six-row block wordmark. It is drawn only when
  it fits whole; otherwise the one-line wordmark is shown.

## [0.1.9] - 2026-11-20

### Added

- Start menu: `gitid` now opens a home screen with the gitid wordmark, a
  tagline with the version, and seven numbered tasks (Accounts, Clone a repo,
  Check repo, Browse repos, Apply, Doctor, Import from git). `1` to `7` open a
  task at once; `↑`/`↓`, `home`/`end`, `pgup`/`pgdn` and `enter` work too. The
  letters `c`, `k`, `p`, `D` and `i` (as bound in your `keys.yaml`) open
  clone, check, apply, doctor and import from the menu, as they did on the
  Accounts screen. The menu adapts to the terminal: logo from 80x24, a
  one-line header on shorter terminals, the menu alone at 40x10.
- Update notice: when the TUI starts, gitid sends one unauthenticated
  `GET https://github.com/tinhnd1902/gitid/releases/latest` (at most once every
  24 hours, never for development builds, snapshots or prereleases) and, when a
  newer release exists, shows one line on the start menu with the command that
  fits the install (`brew upgrade gitid`, `install.sh`, or the releases page).
  The request carries no token, cookie, query or body; the redirect is not
  followed, the body is not read, and only a validated version number is ever
  shown. The time of the last attempt and the newest version seen are kept in
  `~/.config/gitid/update-check.json` (only when that folder already exists).
  Set `GITID_NO_UPDATE_CHECK=1` to turn it off entirely: no file is read or
  written and no request is made.
- Key scope `home` (the start menu): `gitid keys` and `keys --example` list it.
  The digits `1` to `9` are reserved there.

### Changed

- `q` (and `esc`) on the Accounts screen now go back to the start menu instead
  of quitting; with a filter applied, the first press clears the filter. `q`
  quits on the start menu only (`ctrl+c` still quits everywhere). The `quit`
  action now works on the start menu, and `back` works on every screen except
  it, the Accounts list included. A `keys.yaml` that customised `quit` keeps
  working on the start menu.
- `keys.yaml` edge cases from the new scopes: `back` now shares the Accounts
  screen with `add_company` and the other list actions, so `back: "A"` (or any
  key those use, such as `i` or `c`) is reported as a conflict and `back` goes
  back to its default; and a digit bound to `quit`, `help`, `palette`, `clone`,
  `check`, `apply`, `doctor` or `import` is reported (the digits belong to the
  start menu) and that action keeps its default. As always, a problem is shown
  in the status bar, the `?` overlay, `gitid keys` and `gitid doctor`, and
  never stops gitid.
- The start menu is called "Home" ("Trang chủ"); the list is still "Accounts"
  ("Tài khoản"). The header shows no crumb for the start menu. After an apply
  finishes, `enter` returns to the screen the flow started from (the start
  menu, or Accounts when it started there).
- README, README_VI and SECURITY.md: the network section now names the update
  check and its opt-out.

## [0.1.8] - 2026-11-19

### Changed

- TUI style pass: one accent colour, used only for focus, cursor and choice,
  plus ok/warn/error colours. Every text role meets a 4.5:1 contrast ratio in
  light and dark themes.
- One meaning per glyph: `▌` cursor, `▸` chosen/primary key, `●` pending badge,
  `◉` radio, `← →` cyclers.
- Detail panes use aligned label columns and an identity stamp
  (`ref · Name <email>`) as their title.
- The next step is named once, as the `▸` primary key in the help bar. Every
  screen now has a primary key.
- The home screen is called "Accounts" ("Tài khoản"). Wording is consistent
  across Vietnamese and English. Errors are no longer truncated.
- Key bindings are unchanged.

## [0.1.7] - 2026-11-19

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
- Online tab of the repo browser: lists the repositories each account can see
  on GitHub (github.com and GitHub Enterprise Server, `https://HOST/api/v3`)
  or GitLab (gitlab.com and self-hosted, `https://HOST/api/v4`), page by page
  as they arrive: `owner/name`, `priv`/`arch`/`fork`/`empty` badges, how long
  ago it was updated, `+N acc` when another of your accounts sees it too and,
  when you already have it, where (`✓`, or `!` when that clone has a problem).
  Accounts of other platforms say "not available yet". Nothing is looked up or
  sent until you open the tab. `enter` on a repository that is not cloned opens
  the clone flow at the account pick with this account chosen (the URL is built
  from the host in your configuration and rendered like a pasted URL; if
  `<base folder>/<repo>` is taken by something else, the target becomes
  `<base folder>/<owner>-<repo>` and the tab says so), on a cloned one it opens
  the Check screen for the existing clone. `c` copies the clone URL, `o` opens
  the repository's page, `/` filters, `left`/`right` switch account, `r`
  forgets the token and fetches again, `esc` stops a fetch and keeps the pages
  that came. Before a clone starts gitid checks that `config.yaml` has not
  changed on disk.
- Tokens for the Online tab, found per account in this order (first hit wins):
  the environment variable `GITID_TOKEN_<COMPANY>__<ACCOUNT>` (for example
  `GITID_TOKEN_ACME__ALICE`); a gitid Keychain item (macOS; service `gitid`,
  account `<host>/<username>`; written by `gitid token set`); `gh auth token` or
  `glab config get token`; and git's credential helper (`git credential fill`,
  run only when the Online tab is opened, with every prompt suppressed, and
  without `approve`/`reject`; macOS may still show its own Keychain dialog).
  The tab shows the source per account (`token: env`, `keychain`, `gh`, `glab`,
  `git`, `none`) and never the token. Before listing, the API's `/user` login
  must match the account's username, otherwise nothing is listed ("this token
  belongs to @x"). The token stays in memory and is never written to disk,
  put in a command line, a URL, a log or an error.
- Errors of the Online tab have their own message and hint, in vi and en: 401
  (with a hint per token source, and "looks like a password" for a credential
  from git), missing scope, SAML SSO (with the authorization link), a one-time
  code requirement, rate limit (with the reset time; a wait of 10 s or less is
  waited out once), offline/DNS, timeout, proxy, untrusted certificate,
  server error, an answer that is not the API's, wrong user, cancelled and
  truncated (50 pages).
- Network safety of the Online tab: `GET` only; https only; a redirect or a
  "next page" link to another host or to `http` is refused, so the token is
  sent only to the account's own API host; `HTTPS_PROXY`/`NO_PROXY` and
  `SSL_CERT_FILE` are honoured; TLS 1.2 or newer and certificates are always
  verified; 30 s per request, 3 accounts at a time, 8 MiB per page; texts
  from the API are stripped of control and escape characters before they are
  shown. `SECURITY.md` lists a leaked or misdirected token as in scope.
- New rebindable action `problems` (default `!`) and the `repos` screen scope
  for `filter`, `check`, `fix`, `copy` and `reload` (`gitid keys` lists them).
  Since `filter`, `check`, `fix`, `copy`, `reload` and `problems` now share the
  repo browser screen, they cannot share a key with each other there: for
  example `filter: "f"` is reported as a conflict with `fix` (the default key
  stays).
- Online tab for Gitea, Bitbucket Cloud and Azure DevOps Services (they said
  "not available yet"). Gitea (`https://HOST/api/v1`): `Authorization: token
  <t>`, `/user/repos` plus `/orgs/<org>/repos` for each organization of
  `/user/orgs`, 50 per page. Bitbucket Cloud (`bitbucket.org` only): an
  Atlassian API token (app passwords stopped working on 2026-06-09) as Basic
  auth with the account's email, then once as Bearer; the repositories of each
  workspace of `/2.0/user/workspaces`; the `/2.0/user` nickname must match the
  account's username. Azure DevOps (`dev.azure.com` only): a PAT in Basic auth
  with an empty user (a Microsoft Entra JWT as Bearer), one
  `/_apis/git/repositories?api-version=7.1` call per organization in the
  account's `orgs`; without `orgs` the organizations are looked up on
  `app.vssps.visualstudio.com`, which Microsoft may refuse for a PAT, and the
  tab says to set `orgs`; a sign-in page (HTTP 203) is reported as a rejected
  token. Same rules as GitHub and GitLab: https and same-host redirects and
  next links only, page, size and organization caps, rate-limit handling, the
  clone URL built from the configured host, hostile names get no URL. One
  organization or workspace that fails is a warning, the others are listed.
- `gitid repos [--company SLUG] [--depth N] [--problems] [--json]` prints the
  local scan (grouped by company, at most 80 columns; exit 0, or 2 when
  something needs attention, or 1 on an error). `gitid repos --online
  [--account C/A] [--max-pages N] [--local] [--json]` lists what each account
  can see with the same token chain as the Online tab, never printing a token,
  with `local_paths` for repositories you have cloned; an account with no
  token is shown as `token: none` (not a failure), a failed one gives exit 2.
  Ctrl-C prints what there is and exits 1. The JSON schema is documented in the
  README.
- `gitid token set|rm|status <company/account>`: `set` reads a token from stdin
  (hidden on a terminal) and stores it as the gitid Keychain item (macOS). The
  token goes to `security -i` on stdin, hex encoded, and is in no command line.
  `rm` deletes the item, `status [--json]` shows which source each account
  would use and never a token. Elsewhere than macOS it says to use the
  `GITID_TOKEN_*` variable.
- README.md and README_VI.md: "Clone and the repo browser", "Online tab and tokens".
- Test: the shortcut guard covers the repo browser states and its sentinel
  keys now run through `alt+a` to `alt+z` and then `alt+A` to `alt+Z`.

### Changed

- The `open` action (default `o`) now also works on the repo browser (open the
  repository's page on the Online tab); its description is "open page".
  `reload` reads "refetch" and `copy` "copy URL" on the Online tab.
- README.md and README_VI.md: the sentences "gitid itself never signs in to or
  verifies anything with the platform" and "the only step that uses the
  network" are reworded: network use is now on demand in three places (a clone,
  the `ssh -T` test and the Online tab). New section "Online tab and tokens".
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

[Unreleased]: https://github.com/tinhnd1902/gitid/compare/v0.1.10...HEAD
[0.1.10]: https://github.com/tinhnd1902/gitid/compare/v0.1.9...v0.1.10
[0.1.9]: https://github.com/tinhnd1902/gitid/compare/v0.1.8...v0.1.9
[0.1.8]: https://github.com/tinhnd1902/gitid/compare/v0.1.7...v0.1.8
[0.1.7]: https://github.com/tinhnd1902/gitid/compare/v0.1.6...v0.1.7
[0.1.6]: https://github.com/tinhnd1902/gitid/compare/v0.1.5...v0.1.6
[0.1.5]: https://github.com/tinhnd1902/gitid/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/tinhnd1902/gitid/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/tinhnd1902/gitid/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/tinhnd1902/gitid/releases/tag/v0.1.2

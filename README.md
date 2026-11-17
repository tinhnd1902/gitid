<div align="center">

# gitid

**Manage, switch, clone and check Git identities across GitHub, GitLab, Bitbucket, Gitea and Azure DevOps, from one terminal UI.**

English · [Tiếng Việt](README_VI.md)

[![Release](https://img.shields.io/github/v/release/tinhnd1902/gitid?style=flat-square)](https://github.com/tinhnd1902/gitid/releases)
[![Go version](https://img.shields.io/github/go-mod/go-version/tinhnd1902/gitid?style=flat-square)](go.mod)
[![License: MIT](https://img.shields.io/github/license/tinhnd1902/gitid?style=flat-square)](LICENSE)
[![CI](https://img.shields.io/github/actions/workflow/status/tinhnd1902/gitid/ci.yml?branch=main&style=flat-square&label=CI)](https://github.com/tinhnd1902/gitid/actions/workflows/ci.yml)
[![Stars](https://img.shields.io/github/stars/tinhnd1902/gitid?style=flat-square)](https://github.com/tinhnd1902/gitid/stargazers)

</div>

<!--
Hero animation: record it with vhs (see CONTRIBUTING.md, "Recording the demo"):

    make build && vhs docs/demo.tape      # writes docs/img/hero.gif

then uncomment the line below. It stays commented until the file exists so the
page never shows a broken image.

<p align="center"><img src="docs/img/hero.gif" alt="gitid terminal UI: company and account tree, diff preview and doctor" width="860"></p>
-->

> 💡 gitid does not wrap `git` or add shell hooks. It writes ordinary `includeIf` rules into your git configuration, so the right name and email are picked by plain `git`, in every tool, even when gitid is not running.

## Features

- **Right identity, automatically:** `user.name` and `user.email` are chosen by directory (`gitdir:`) and by remote URL (`hasconfig:remote.*.url:`), with no hooks and no per-repo setup.
- **Companies and accounts:** group several accounts per company (say, 10 companies with up to 10 accounts each), mark a default, and find any of them with a fuzzy filter.
- **Five platforms:** GitHub, GitLab (including self-hosted), Bitbucket, Gitea and Azure DevOps (including legacy `visualstudio.com` URLs), over HTTPS or SSH.
- **Safe by default:** every change is shown as a diff first, files are backed up before they are written, managed blocks are idempotent, and `--dry-run` writes nothing at all.
- **SSH without the pain:** one ed25519 key and one `Host` alias per account, the public key one keypress from your clipboard, and private keys are never read.
- **Clone and repair:** clone with the right account and a live progress bar, check which identity a repository really uses, and rewrite a wrong remote.
- **Doctor and import:** diagnose the whole setup with per-check progress, and bootstrap from the `includeIf` entries you already have.
- **Two languages:** Vietnamese (default) and English, switchable per run.

## Quick Start

**1. Homebrew (macOS):**

```bash
brew install tinhnd1902/tap/gitid
```

**2. Install script (macOS and Linux, amd64 and arm64):**

```bash
curl -fsSL https://raw.githubusercontent.com/tinhnd1902/gitid/main/install.sh | sh
```

The script downloads the latest release, verifies its sha256 against the release's `checksums.txt`, installs `gitid` (into `/usr/local/bin` when you can write there, otherwise `~/.local/bin`) and, if that directory is not on your `PATH` yet, adds it to your shell startup file. Open a new terminal afterwards. Options are environment variables:

```bash
curl -fsSL https://raw.githubusercontent.com/tinhnd1902/gitid/main/install.sh | VERSION=0.1.0 sh          # a specific release
curl -fsSL https://raw.githubusercontent.com/tinhnd1902/gitid/main/install.sh | INSTALL_DIR=$HOME/bin sh   # choose the directory (must be writable)
curl -fsSL https://raw.githubusercontent.com/tinhnd1902/gitid/main/install.sh | NO_MODIFY_PATH=1 sh        # never edit shell startup files
```

Prefer to read it before running it?

```bash
curl -fsSLo install.sh https://raw.githubusercontent.com/tinhnd1902/gitid/main/install.sh
less install.sh
sh install.sh
```

**3. Or via `go install`:**

```bash
go install github.com/tinhnd1902/gitid/cmd/gitid@latest
```

If your shell then says `command not found: gitid`, the Go bin directory is not on your `PATH`. Add it once (zsh shown; use `~/.bashrc` for bash):

```bash
echo 'export PATH="$(go env GOPATH)/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc
```

**4. Or download a release binary** (macOS and Linux, amd64 and arm64) from the [Releases page](https://github.com/tinhnd1902/gitid/releases):

```bash
tar -xzf gitid_<version>_<os>_<arch>.tar.gz
install -m 0755 gitid /usr/local/bin/gitid   # or any directory on your PATH
```

<details>
<summary>More install notes</summary>

- gitid needs **git 2.36 or newer** (it relies on `hasconfig:remote.*.url`). Run `git --version` to check.
- Binaries are not code-signed or notarized yet. Homebrew marks downloaded casks as quarantined, so the gitid cask removes that flag from the `gitid` binary only (a `postflight` step running `xattr -dr com.apple.quarantine`). This skips the Gatekeeper check for this one file; the download is still verified by the sha256 recorded in the cask. If you would rather keep Gatekeeper in the loop, use the install script or `go install` instead. For a binary you downloaded by hand and macOS blocks, remove the flag once: `xattr -d com.apple.quarantine /usr/local/bin/gitid`.
- The Homebrew install is a cask, so it is for macOS. On Linux use the install script.
- The install script writes one file (`gitid`) and, when needed, appends one line marked `# added by gitid installer` to one shell startup file (`~/.zshrc`, `~/.bash_profile` on macOS or `~/.bashrc` on Linux, `~/.config/fish/conf.d/gitid.fish`, or `~/.profile`). It never uses `sudo`, sends no telemetry and contacts only GitHub. The checksum protects against a corrupted or tampered download of a release; it cannot protect against a compromised release, because both files come from the same place.
- Verify a download with the `checksums.txt` file of the release: `shasum -a 256 -c checksums.txt --ignore-missing`.
- A binary installed with `go install` reports its version as `dev`.
- To build from source: `git clone https://github.com/tinhnd1902/gitid && cd gitid && make build` (the binary is `bin/gitid`).

</details>

**Run:**

```bash
gitid            # opens the TUI; press ? for the key map, q to quit
```

First run in the TUI, three steps (the screen shows them too, and the bottom line always marks the next key with `▸`):

1. `A` adds a company: a short slug and a base folder such as `~/Code/acme/`.
2. `a` adds a Git account (username, email) to that company. The form shows the remote the account will use (`https://<username>@github.com/…`) and the gitid ID it derives from the username. The username is your login on the platform (the part in `github.com/<login>`), not your email; the email only becomes the commit author and gitid does not verify it.
3. `p` previews the files gitid will write, then `y` applies them (every file is backed up first). After that, every repo inside the base folder uses the right account: `c` clones a repo into it, `k` checks a repo. The first HTTPS clone or push makes git ask you to sign in (use a Personal Access Token if it asks for a password); gitid itself never signs in to or verifies anything with the platform.

**Preview safely:**

```bash
gitid apply --dry-run     # prints a diff of every file gitid would change, writes nothing
```

## Usage

```text
gitid                                    # open the terminal UI

gitid list [--json]                      # companies and accounts
gitid check [path] [--json]              # which account and email does this repo use, and why
gitid doctor [--ssh-test] [--json]       # diagnose git, ssh, keys and the gitid configuration

gitid clone <url> --account acme/alice   # clone with the right account (rewrites the URL)
      [--dir <dir>] [--dry-run]          #   --dir: target directory, default <company dir>/<repo>
      [--interactive] [--force]          #   --interactive: allow password prompts on the terminal
gitid fix-remote [path]                  # rewrite a remote URL so it uses the right account
      [--account acme/alice]             #   default: detected from the remote
      [--remote origin] [--dry-run]

gitid key gen acme/carol                 # create an ed25519 key for an SSH account
      [--no-passphrase] [--yes]          #   passphrase is asked by ssh-keygen itself, so a terminal is required
      [--copy]                           #   copy the public key (default: only on a terminal)

gitid apply [--dry-run] [--yes]          # write ~/.gitconfig and ~/.ssh changes (with backup)
gitid import [--dry-run]                 # propose companies from your existing includeIf entries
      [--select initech --platform github --username alice --auth https --host <host>]

gitid version                            # print the version
gitid completion <bash|zsh|fish|powershell>   # shell completion
```

Exit codes: `0` ok, `1` error, `2` `check` or `doctor` found problems (handy in scripts).

```bash
# Preview everything, apply only after you read the diff
gitid apply --dry-run
gitid apply                                  # asks "y/N" on a terminal
gitid clone git@github.com:org/repo.git --account acme/alice --dry-run
gitid fix-remote --dry-run                   # shows old -> new, changes nothing
```

`gitid apply` refuses to run without a terminal unless you pass `--yes`, so a script can never write by accident. `gitid key gen` refuses to create a key with a passphrase when stdin is not a terminal (ssh-keygen would read end-of-file as an empty passphrase); in scripts you must opt in explicitly with `--no-passphrase --yes`. It touches the clipboard only on a terminal or with `--copy`.

`gitid check --json` prints one object. `warnings` is a list of `{code, message}`: match on the stable `code`, show the translated `message` (in the active language) to people.

```json
{
  "repo": "/home/me/Code/acme/demo",
  "remote": "https://github.com/acme-org/demo.git",
  "effective_email": "alice@example.com",
  "email_origin": "file:/home/me/.config/gitid/git/acme-alice.gitconfig",
  "account": "acme/alice",
  "reason": "default",
  "expected_email": "alice@example.com",
  "warnings": [
    {
      "code": "warn.remote_no_username",
      "message": "The remote has no username, so git cannot pick an account outside a company directory."
    }
  ]
}
```

## How it works

gitid keeps your accounts in its own `config.yaml`, then generates plain git configuration from it. Git does the selection:

```text
~/.gitconfig
├─ [user]  (your own, untouched: the fallback)
└─ # >>> gitid managed block >>>
   ├─ [includeIf "gitdir:~/Code/acme/"]  ──▶  ~/.config/gitid/git/acme.gitconfig
   │                                            ├─ [include]  acme-alice.gitconfig        default account of the company
   │                                            └─ [includeIf "hasconfig:remote.*.url:…"]
   │                                                 acme-bob.gitconfig, acme-carol.gitconfig   per-account override
   └─ [includeIf "hasconfig:remote.*.url:git@github-acme-carol:*/**"]   outside company folders
      (only patterns that identify exactly one account)
   # <<< gitid managed block <<<
```

Precedence, from weakest to strongest (the last value git reads wins):

1. Your global `[user]` is the fallback.
2. Inside a company folder, the company's **default account** applies.
3. Inside that folder, an account whose **remote URL pattern** matches the repository's remote overrides the default. The same username can therefore exist in two companies without a clash, because the match happens inside the folder-scoped file.
4. Outside every company folder, only patterns that point to exactly one account are used: SSH aliases are always unique, an HTTPS `user@host` pattern is used only when no other account produces it. `gitid doctor` reports the ambiguous ones, and the `orgs` field of an account narrows them.
5. A `user.name` or `user.email` inside the repository's own `.git/config` beats all of the above. `gitid check` warns when that happens.

How the two authentication modes look in practice:

| Mode      | Remote URL                          | What gitid sets up                                                                                          |
| --------- | ----------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| **HTTPS** | `https://alice@github.com/org/repo` | The username goes into the URL so your credential helper picks the right login. gitid never stores a password. |
| **SSH**   | `git@github-acme-carol:org/repo`    | A `Host github-acme-carol` alias in `~/.ssh/config.d/gitid.conf` with its own key and `IdentitiesOnly yes`. |

## Safety

gitid edits files in your home directory, so it is strict about it:

- **What it touches:** the managed block in `~/.gitconfig`; the managed block (a single `Include config.d/*` line) at the top of `~/.ssh/config`; `~/.ssh/config.d/gitid.conf`; keys you ask it to create under `~/.ssh/gitid/`; and its own folder `$XDG_CONFIG_HOME/gitid/` (default `~/.config/gitid/`) holding `config.yaml`, the generated `git/*.gitconfig` files and `backups/`. Nothing else.
- **Managed blocks:** everything gitid writes into your files sits between `# >>> gitid managed block … >>>` and `# <<< gitid managed block <<<`. Bytes outside the markers are never changed. Running `apply` twice changes nothing the second time, and writes no new backup.
- **Preview and confirm:** the TUI always shows the diff and asks before writing. `gitid apply --dry-run` prints the same diff and creates nothing, not even directories.
- **Backups:** before any write, the old bytes of every affected file are copied to `~/.config/gitid/backups/<timestamp>/` (directory `0700`, files `0600`) together with a `manifest.json` that lists each original path, mode and SHA-256. The newest 30 backups are kept.
- **Atomic and careful writes:** each file is written to a temporary file next to it, flushed and renamed. Symlinks (a dotfile manager) are followed so they stay symlinks, file modes are kept, and the plan is checked again right before writing: if a target changed since the diff was built, nothing is written. A lock prevents two gitid processes from applying at once, and a present `~/.gitconfig.lock` aborts the run.
- **Private keys:** gitid never reads, prints, modifies or overwrites a private key. It only checks that the file exists and its permissions; it reads the `.pub` file. Removing an account keeps its keys unless you type the account id to confirm deleting them, and only keys under `~/.ssh/gitid/` can be deleted.
- **Secrets in URLs:** a password or token pasted inside a URL is dropped and masked everywhere it would be shown.
- **Injection-safe:** names, emails, hosts and usernames are validated and escaped before they reach git or ssh configuration; clone commands always use `git clone -- <url> <dir>`.
- **Network:** none, except when you clone or press the opt-in `ssh -T` test. There is no telemetry.
- **Uninstall:** delete all companies in the TUI and apply; your files return to their original bytes. Or restore by hand (below). To remove the gitid program itself, see [Uninstall the binary](#uninstall-the-binary).

<details>
<summary>Restore by hand</summary>

Every `apply` that changes files creates a folder such as `~/.config/gitid/backups/20261116-140102.123/`:

```text
manifest.json              # created time, and for every file: path, op, mode, sha256, backup_file
000-gitconfig              # the previous bytes of ~/.gitconfig (if it existed)
001-config                 # the previous bytes of ~/.ssh/config
...
```

To undo, find the entry in `manifest.json`, then copy its `backup_file` back over `path`, for example:

```bash
cp ~/.config/gitid/backups/<timestamp>/000-gitconfig ~/.gitconfig
```

Entries with `"existed": false` were created by that run: delete them to go back. Files under `~/.config/gitid/git/` are generated and can simply be removed.

</details>

## Configuration

gitid reads and writes one file:

| Item                  | Location                                                                     |
| --------------------- | ---------------------------------------------------------------------------- |
| Configuration file    | `$XDG_CONFIG_HOME/gitid/config.yaml` (mode `0600`)                           |
| Default when XDG is unset, empty or not an absolute path | `~/.config/gitid/config.yaml`                  |
| Generated git files   | `<config dir>/git/`                                                          |
| Backups               | `<config dir>/backups/`                                                      |

Environment variables: `XDG_CONFIG_HOME` (config location, default `~/.config`) and `GITID_LANG` (`vi` or `en`; overrides `settings.language`).

```yaml
version: 1
settings:
  language: en              # vi (default) or en
  code_root: ~/Code         # parent folder proposed for new companies
companies:
  - slug: acme              # a-z, 0-9, dashes; unique; cannot be changed later
    name: ACME Corp
    base_dir: ~/Code/acme/  # repositories below this folder use the company's accounts
    default_account: alice
    accounts:
      - id: alice           # unique within the company; cannot be changed later
        platform: github    # github | gitlab | bitbucket | gitea | azure-devops
        username: alice
        name: Alice Example # git user.name
        email: alice@example.com
        auth: https         # https | ssh
        orgs: [acme-org]    # optional: only remotes owned by these orgs match this account
      - id: carol
        platform: github
        username: carol
        name: Carol Example
        email: carol@example.com
        auth: ssh
        ssh_key: ~/.ssh/gitid/acme-carol   # set when gitid creates the key
  - slug: globex
    name: Globex
    base_dir: ~/Code/globex/
    accounts:
      - id: dave
        platform: gitlab
        host: git.example.com   # self-hosted; empty = the platform default
        ssh_port: 2222          # optional
        username: dave
        name: Dave Example
        email: dave@globex.example
        auth: ssh
```

gitid rewrites `config.yaml` from its internal model whenever you save, so **comments you add by hand are not preserved**. The previous file is backed up first. A company slug and an account id cannot be changed after creation (they name the SSH alias, the key file and the generated files); delete and recreate instead.

## Features in Detail

### List

The home screen is a tree of companies and accounts with a platform tag (`GH`, `GL`, `BB`, `GT`, `AZ`), the auth mode, `★` for the company default and the email. Press `/` to filter by name, username, email, host or platform; matching is fuzzy inside each word and the tree order is kept. From 84 columns a detail pane sits next to the list; from 60 columns (and 24 rows) it sits below the list; on smaller terminals `enter` opens it. The pane explains what the selected company or account does ("What happens"); for an account it also shows the remote it produces, its folder and the next step. Two accounts of one company with the same platform, host and username are flagged with `!`. A company without an account is marked `no accounts yet`: gitid writes nothing for it until you add one with `a`.

```text
▌ acme  ACME Corp                   ~/Code/acme/  2 accounts
 ├─ ★ alice      GH https                  alice@example.com
 └─   carol      GH ssh                    carol@example.com
▪ globex  Globex                  ~/Code/globex/  1 account
 └─ ★ dave       GL ssh                  dave@globex.example

                          ● 6 unapplied change(s) — press p
 ▸ p preview & apply  a add account  e edit  d delete  q quit  ? all keys
```

The right side of the status bar always says what is left to do: unapplied changes (highlighted), a company that has no account yet, or `✓ Applied, nothing pending`. The help bar shows only the keys that matter now; the next step comes first, marked `▸`, and `?` lists every key.

### Add and edit accounts

`A` adds a company, `a` adds an account to the selected company, `e` edits, `d` deletes. Forms validate as you go with the same rules the CLI uses. Choosing SSH offers to create the key right after saving. After every change you see the diff and confirm it.

### Clone

`c` asks for a URL, ranks your accounts (the ones on the same host first), previews the rewritten URL and the target folder, then clones in the background with a progress bar. Afterwards it checks which email git will use in the new repository.

```text
Original URL: https://github.com/org/repo.git
Clone URL:    https://alice@github.com/org/repo.git
Directory:    ~/Code/acme/repo
✓ Clone finished
✓ user.email in the repository: alice@example.com (correct).
```

If authentication fails in the background, gitid offers to retry on the terminal, where a password or passphrase prompt works.

### Check and fix remote

`k` inspects the repository in your current directory: the matched account, the email git really uses, and where that email comes from. It warns about a repository-level override, an ambiguous or missing match, and configuration that is not applied yet. When a remote does not use the matching account, `f` shows the change and asks before running `git remote set-url`.

```text
Repo:       ~/Code/acme/demo
Remote:     https://github.com/acme-org/demo.git
Account:    acme/alice  (default account of the directory)
Expected:   alice@example.com
In use:     alice@example.com  (file:~/.config/gitid/git/acme-alice.gitconfig)
! The remote has no username, so git cannot pick an account outside a company directory.

Fix remote origin for account acme/alice
- https://github.com/acme-org/demo.git
+ https://alice@github.com/acme-org/demo.git
```

### Doctor

`D` runs every diagnostic at the same time, each with its own spinner: git version, configuration validity, credential helper, the ssh `Include`, `gitid.conf`, keys and their permissions, pending changes, base folders, alias collisions and ambiguous patterns. Press `s` to also test `ssh -T` against each alias (this is the only step that uses the network, and only when you press it).

```text
✓ git version        git 2.56.0
✓ gitid configuration
! Credential helper
  ! Only osxkeychain is configured; Git Credential Manager is not installed.
      → Install: brew install --cask git-credential-manager
✗ SSH keys
  ✗ Key of acme/carol is missing (~/.ssh/gitid/acme-carol).
      → Run `gitid key gen <company>/<account>`.
```

<details>
<summary>What `gitid key gen` does exactly</summary>

It refuses to continue if the private or public key file already exists. It then runs `ssh-keygen -t ed25519 -a 100 -C "<email> gitid:<company>/<account>" -f ~/.ssh/gitid/<company>-<account>` attached to your terminal, so **ssh-keygen asks for the passphrase and gitid never sees it**. `--no-passphrase` (or the second confirmation in the TUI) is the only way to pass an empty passphrase. Afterwards gitid checks that both files exist, records `ssh_key` in the configuration, shows the public key (`c` copies it with `pbcopy`) and the page where the platform wants it (`o` opens it).

</details>

### Import

`i` (or `gitid import`) reads the `includeIf "gitdir:…"` entries in your `~/.gitconfig` and the `[user]` of the files they point to, and proposes a company for each. It never guesses the platform or username: you provide those, then review the diff. Hosts found in `~/.ssh/config` are listed for information only; their keys are not opened.

## Troubleshooting

<details>
<summary>The wrong account or email is used</summary>

Run `gitid check` in the repository. It prints the effective `user.email`, the file it comes from and the account gitid expects. Typical causes: the repository has its own `user.email` in `.git/config` (remove it with `git config --local --unset user.email`); the remote has no username so nothing identifies the account outside a company folder (run `gitid fix-remote`); or the configuration is not applied yet (`gitid apply`).

</details>

<details>
<summary>"Too many authentication failures" over SSH</summary>

Your ssh-agent offers every loaded key before the right one. gitid's `Host` blocks set `IdentitiesOnly yes`, so use the alias form of the remote (`git@github-acme-carol:org/repo.git`; `gitid fix-remote` rewrites it). If a `Host *` block in your own `~/.ssh/config` adds identities, gitid's `Include` at the top of the file takes precedence for its aliases. Check with `ssh -vT git@<alias>` and `gitid doctor --ssh-test`.

</details>

<details>
<summary>Git Credential Manager is missing</summary>

For HTTPS accounts a credential helper must remember one login per username. gitid never installs software; `gitid doctor` shows the hint. On macOS: `brew install --cask git-credential-manager`. The built-in `osxkeychain` helper also works for a single account per host.

</details>

<details>
<summary>"git is too old" or includes are ignored</summary>

`hasconfig:remote.*.url` needs **git 2.36 or newer**. Upgrade git (on macOS `brew install git`) and make sure the new one comes first on your `PATH`; `gitid doctor` prints the version it found.

</details>

<details>
<summary>The terminal looks broken or too small</summary>

gitid needs at least 40x10 cells and looks best from 80x24; the detail pane appears from 84 columns (beside the list) or from 60x24 (below it). If the screen is garbled after a crash, run `reset`. The TUI starts only on a real terminal; in scripts use the subcommands.

</details>

## Uninstall the binary

This removes the `gitid` program itself. It is separate from [removing the configuration gitid manages](#safety) (the **Uninstall** bullet there), which you should do first if you want your git and ssh files back to their original bytes.

- **Homebrew:** `brew uninstall --cask gitid` (optionally `brew untap tinhnd1902/tap`).
- **Install script:** `rm "$(command -v gitid)"`, then delete the line marked `# added by gitid installer` from your shell startup file (`~/.zshrc`, `~/.bash_profile`, `~/.bashrc` or `~/.profile`), or delete `~/.config/fish/conf.d/gitid.fish` for fish.
- **`go install`:** `rm "$(go env GOPATH)/bin/gitid"`.

## Contributing

Contributions are welcome: fork the repository, create a branch, and open a pull request. See [CONTRIBUTING.md](CONTRIBUTING.md) for the make targets and the test rules (tests never touch your real HOME). Please read the [Code of Conduct](CODE_OF_CONDUCT.md), and report security problems privately as described in [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE) © 2026 tinhnd1902

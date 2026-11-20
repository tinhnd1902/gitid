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
- **Repo browser:** one screen with a Local tab (every repository under your company folders, the account its remote maps to, and what is wrong), a URL tab (clone) and an Online tab (the repositories each of your accounts can see on GitHub, GitLab, Gitea, Bitbucket Cloud or Azure DevOps, if you have a token for it).
- **Doctor and import:** diagnose the whole setup with per-check progress, and bootstrap from the `includeIf` entries you already have.
- **Start menu:** `gitid` opens a numbered menu of the everyday tasks (accounts, clone, check a repo, browse repos, apply, doctor, import, settings); press `1` to `8` to jump straight to one. It also tells you, in one line, when a newer release exists (see [Network](#safety)).
- **Two languages:** Vietnamese (default) and English. Switch in Settings (start menu, `8`): the change applies at once and is saved to `settings.language`. `GITID_LANG` still wins when gitid starts.

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
gitid            # opens the start menu; press 1-8 to pick a task, ? for the key map, q to quit
```

First run in the TUI: press `1` to open Accounts (the start menu says "Start here: add your first company"), then three steps (the screen shows them too, and the bottom line always marks the next key with `▸`):

1. `A` adds a company: a short slug and a base folder such as `~/Code/acme/`.
2. `a` adds a Git account (username, email) to that company. The form shows the remote the account will use (`https://<username>@github.com/…`) and the gitid ID it derives from the username. The username is your login on the platform (the part in `github.com/<login>`), not your email; the email only becomes the commit author and gitid does not verify it.
3. `p` previews the files gitid will write, then `y` applies them (every file is backed up first). After that, every repo inside the base folder uses the right account: `c` clones a repo into it, `k` checks a repo. The first HTTPS clone or push makes git ask you to sign in (use a Personal Access Token if it asks for a password); gitid itself never signs in for you. The one thing it does with the platform is the Online tab of the repo browser, which only reads a token you already have (see [Online tab and tokens](#online-tab-and-tokens)) and only when you open that tab.

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

gitid repos [--company acme] [--depth 4] # every repo under your company folders and what is wrong with it
      [--problems] [--json]              #   exit 2 when something needs attention
gitid repos --online [--account acme/alice] [--max-pages 50] [--local] [--json]
                                         # what each account can see on its platform (needs a token)
gitid token set acme/alice               # store a token (read from stdin) in the macOS Keychain
gitid token rm acme/alice                # remove it
gitid token status [acme/alice] [--json] # where each account's token comes from (never the token)

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

gitid keys [--json]                      # keyboard shortcuts, custom ones from keys.yaml, and problems found in it
gitid keys --example                     # print a commented keys.yaml template (writes no file)

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

- **What it touches:** the managed block in `~/.gitconfig`; the managed block (a single `Include config.d/*` line) at the top of `~/.ssh/config`; `~/.ssh/config.d/gitid.conf`; keys you ask it to create under `~/.ssh/gitid/`; and its own folder `$XDG_CONFIG_HOME/gitid/` (default `~/.config/gitid/`) holding `config.yaml`, the generated `git/*.gitconfig` files, `backups/` and, once the update check has run, `update-check.json` (the time of the last check and the newest version it saw; gitid never creates the folder just to write it). Nothing else, with one opt-in exception: `gitid token set` adds one item per account to the macOS Keychain (service `gitid`; see [Online tab and tokens](#online-tab-and-tokens)). It also reads an optional `keys.yaml` there (your key bindings) but never writes it.
- **Managed blocks:** everything gitid writes into your files sits between `# >>> gitid managed block … >>>` and `# <<< gitid managed block <<<`. Bytes outside the markers are never changed. Running `apply` twice changes nothing the second time, and writes no new backup.
- **Preview and confirm:** the TUI always shows the diff and asks before writing. `gitid apply --dry-run` prints the same diff and creates nothing, not even directories.
- **Backups:** before any write, the old bytes of every affected file are copied to `~/.config/gitid/backups/<timestamp>/` (directory `0700`, files `0600`) together with a `manifest.json` that lists each original path, mode and SHA-256. The newest 30 backups are kept.
- **Atomic and careful writes:** each file is written to a temporary file next to it, flushed and renamed. Symlinks (a dotfile manager) are followed so they stay symlinks, file modes are kept, and the plan is checked again right before writing: if a target changed since the diff was built, nothing is written. A lock prevents two gitid processes from applying at once, and a present `~/.gitconfig.lock` aborts the run.
- **Private keys:** gitid never reads, prints, modifies or overwrites a private key. It only checks that the file exists and its permissions; it reads the `.pub` file. Removing an account keeps its keys unless you type the account id to confirm deleting them, and only keys under `~/.ssh/gitid/` can be deleted.
- **Secrets in URLs:** a password or token pasted inside a URL is dropped and masked everywhere it would be shown.
- **Injection-safe:** names, emails, hosts and usernames are validated and escaped before they reach git or ssh configuration; clone commands always use `git clone -- <url> <dir>`.
- **Repo scan:** the Local tab of the repo browser only reads: it lists folders under your company base folders and runs one read-only `git config` in each repository it finds. It writes nothing; gitid itself opens no file inside a repository except the one-line `.git` file of a worktree or submodule (git reads the repository's configuration).
- **Network:** one automatic request, plus three places that are on demand. The automatic one is the **update check**: when the TUI starts, gitid sends a single unauthenticated `GET https://github.com/tinhnd1902/gitid/releases/latest` to see whether a newer release exists, at most once every 24 hours (the time of the last attempt and the newest version seen are kept in `~/.config/gitid/update-check.json`, so a failed attempt is not repeated for a day). The request has no token, cookie, query or body, and carries only `User-Agent: gitid/<version>`. gitid reads only the `Location` header of the reply, never follows the redirect and never reads the body, accepts it only when it points at `github.com/tinhnd1902/gitid/releases/tag/vX.Y.Z`, and shows nothing but the version number. It uses the same proxy and certificate settings as the Online tab, gives up after 5 seconds without delaying the first screen, and fails silently. It runs only when the TUI starts (never for `gitid list`, `apply` or any other command) and not for builds that are not a plain release (`dev`, snapshots, prereleases). To turn it off, set `GITID_NO_UPDATE_CHECK=1` (any value except empty, `0` and `false`): then gitid reads and writes nothing for it and makes no request. The on-demand ones are: a clone, the opt-in `ssh -T` test, and the Online tab of the repo browser or `gitid repos --online` (only when you open it or run it), which send read-only `GET` requests to the API of your own accounts' hosts. For an Azure DevOps account with no `orgs`, the same token is also sent to `app.vssps.visualstudio.com`, Microsoft's service that lists your organizations. There is no telemetry.
- **Tokens (Online tab):** read from an environment variable, the gitid Keychain item, `gh`/`glab` or git's credential helper; kept in memory only, never written to disk, never put in a command line, a URL, a log or an error, and sent only as the `Authorization` header to the account's own API host over https (no redirect or next-page link to another host is followed; certificate verification is never turned off). The only place gitid writes a token is the Keychain item you ask for with `gitid token set`: the token is read from stdin (typed without echo on a terminal) and handed to the macOS `security` tool through its standard input, never through its command line, so it does not show in `ps`. gitid never changes what a credential helper stores.
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

gitid reads and writes one configuration file, and reads one optional file of key bindings:

| Item                  | Location                                                                     |
| --------------------- | ---------------------------------------------------------------------------- |
| Configuration file    | `$XDG_CONFIG_HOME/gitid/config.yaml` (mode `0600`)                           |
| Default when XDG is unset, empty or not an absolute path | `~/.config/gitid/config.yaml`                  |
| Key bindings (optional, read only) | `<config dir>/keys.yaml`, see [Custom key bindings](#custom-key-bindings) |
| Generated git files   | `<config dir>/git/`                                                          |
| Backups               | `<config dir>/backups/`                                                      |

Environment variables: `XDG_CONFIG_HOME` (config location, default `~/.config`) and `GITID_LANG` (`vi` or `en`; overrides `settings.language` each time gitid starts). The Settings screen (start menu, `8`) changes `settings.language` for you; if `GITID_LANG` is set it says so, and the variable is used again at the next start.

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

### Custom key bindings

The terminal UI works without any setup. To change a shortcut, create the optional file `keys.yaml` in the same folder as `config.yaml`: `$XDG_CONFIG_HOME/gitid/keys.yaml`, which is `~/.config/gitid/keys.yaml` when `XDG_CONFIG_HOME` is unset, empty or not an absolute path (the same rule as `config.yaml`).

**gitid only reads this file.** It never creates, rewrites, backs up or deletes `keys.yaml`, so your comments stay, and editing it never makes a save of `config.yaml` "stale". There is no `keys:` section in `config.yaml` (gitid rejects one).

```sh
gitid keys                                          # every action with its keys, and any problem found in keys.yaml
mkdir -p ~/.config/gitid && gitid keys --example > ~/.config/gitid/keys.yaml    # a template with every line commented out
```

The folder does not exist before gitid first saves `config.yaml`, hence the `mkdir -p`. If you set `XDG_CONFIG_HOME` to an absolute path, use `$XDG_CONFIG_HOME/gitid/` instead of `~/.config/gitid/`.

`gitid keys --example` only prints to the screen; the redirection above creates the file, and it **overwrites an existing `keys.yaml`**. Remove the leading `#` of the lines you want to change.

The file is one mapping, `action: "key"` or `action: ["key", "key"]` (the help bar and hints show the first key; the `?` overlay shows all of them, for example `c/n`):

```yaml
check: "v"          # v instead of k
clone: ["c", "n"]   # two keys for one action
```

| Action | Default | Where it works |
| ------ | ------- | -------------- |
| `quit` | `q` | start menu |
| `help` | `?` | start menu, every screen except forms |
| `back` | `q` (`esc` always goes back too) | every screen except the start menu (the Accounts list included: it goes back to the start menu) |
| `filter` | `/` | list, repo browser |
| `add_account` | `a` | list, detail |
| `add_company` | `A` | list |
| `edit` | `e` | list, detail |
| `delete` | `d` | list, detail |
| `clone` | `c` | start menu, list, detail, apply (done), check |
| `check` | `k` | start menu, list, detail, apply (done), repo browser |
| `doctor` | `D` | start menu, list, detail |
| `apply` | `p` | start menu, list, detail, SSH key result |
| `ssh_key` | `g` | list, detail |
| `import` | `i` | start menu, list |
| `reload` | `r` | list, repo browser (rescan; refetch on the Online tab) |
| `yes` / `no` | `y` / `n` | apply review, confirmation dialogs (also the "discard what you typed?" question) |
| `fix` | `f` | check, repo browser |
| `retry` | `r` | apply, check, doctor |
| `copy` | `c` | SSH key result, repo browser (copy the path, or the clone URL on the Online tab) |
| `open` | `o` | SSH key result, repo browser (Online tab: open the repository's page) |
| `ssh_test` | `s` | doctor |
| `terminal` | `t` | clone |
| `problems` | `!` | repo browser (show only repos with a problem) |
| `palette` | `ctrl+k` | start menu, every screen except forms (opens the same overlay as `help`) |

A key is one printable ASCII character (`v`, `V`, `/`, `5`), `ctrl+<letter>`, `alt+<character>` or `f1` to `f12`. These are not valid: letters with accents (`â`, `đ`), words (`ab`), and named keys such as `enter`, `esc`, `tab`, the arrows or `space`. Quote the key when YAML would read it differently: `"?"`, `"#"`, `"&"`, `"*"`, `"!"`, `"|"`, `">"`, `"%"`, `"@"`, `` "`" ``.

These keys cannot be changed: `ctrl+c`, `esc` (always goes back one step), the digits `1` to `9` on the start menu, `enter`, `tab` and `shift+tab`, the arrows, `pgup`/`pgdn`, `home`/`end`, `ctrl+p`/`ctrl+n`/`ctrl+b`/`ctrl+f`, `space`, `h` and `l` in two-button dialogs, the keys of choice fields in forms (`y n h l j k`) and any text you type into a field. Among the `ctrl+<letter>` keys, `ctrl+h`, `ctrl+i` and `ctrl+m` (backspace, tab and enter on many terminals) are refused too.

Two actions that are live on the same screen cannot share a key (the list for `check` and `clone`, for example), but a key may mean different things on screens that never show together. The start menu shares its letters with the Accounts screen (`c`, `k`, `p`, `D`, `i` open the same items), and a digit cannot be bound to any action that works on the start menu (`quit`, `help`, `palette`, `clone`, `check`, `apply`, `doctor`, `import`): that binding is reported and the action keeps its default. Swapping two keys is fine: `clone: "e"` with `edit: "c"`. A problem never stops gitid: the affected action keeps its default key and gitid says so, in the status bar when the TUI starts, at the bottom of the `?` overlay, in `gitid keys` (which exits with 2) and in `gitid doctor` (a warning). A file that cannot be read or parsed leaves every default in place. An unknown action name (for example one from a newer gitid) is reported and ignored.

Press `r` on the list to reload `config.yaml` and `keys.yaml` without leaving the TUI. Shortcuts need a keyboard layout that sends plain letters: with a Vietnamese input method switched on (Telex, VNI) gitid cannot read `a`, `k` and the others, and says so in the status bar; switch to EN (ABC) first. The one exception is the filter of the `?` overlay, which is a text box: Vietnamese input works there. For the same reason keys with non-ASCII characters are not accepted in `keys.yaml`. If a `keys.yaml` written before `palette` existed binds `ctrl+k` to another action in a screen the palette shares, that binding is reported as a conflict and the action goes back to its default key; rebind `palette` to keep your key.

## Features in Detail

### Start menu

`gitid` opens a menu, not the list of accounts. It shows the wordmark, the tagline with the version, an update line when a newer release exists, and eight tasks, each with a number and a one-line description:

```text
  1  Accounts        Companies, accounts and SSH keys
  2  Clone a repo    Into the right folder, with the right account
  3  Check repo      Which account the repo in this folder uses
  4  Browse repos    Repos on this machine and on your platforms
  5  Apply           Preview, then write ~/.gitconfig and ~/.ssh
  6  Doctor          Check git, SSH and the configuration
  7  Import from git Read the includeIf rules already in ~/.gitconfig
  8  Settings        Interface language
```

Press `1` to `8` to open a task at once, or move the `▌` cursor with `↑`/`↓` (`home`/`end` and `pgup`/`pgdn` too; it stops at the ends) and press `enter`. The letters you used on the Accounts screen still work from the menu: `c` clone, `k` check, `p` apply, `D` doctor, `i` import (Settings has no letter: use `8` or `enter`). `q` quits, and `esc` does nothing here; on every other screen `esc` and `q` go back one step, so `esc` on Accounts returns to the menu (a filter you applied is cleared first). Digits are only shortcuts on the menu: in a form, a filter or the `?` overlay they are typed as text. After an apply finishes, `enter` returns to the screen the flow started from (the menu, or Accounts when you started it there). When a newer release exists, one line says so and names the command that fits how gitid was installed (`brew upgrade gitid` for Homebrew, `install.sh` for the install script, or the releases page otherwise); see [Network](#safety) for what the check sends and how to turn it off.

### Settings

`8` on the start menu opens Settings. It has one setting for now, the interface language, shown as a choice: `◉ Tiếng Việt  ○ English`. `←`, `→` and `enter` move to the other language. The change applies at once, on every screen (help bar, `?` overlay, start menu), and gitid saves it to `settings.language` in `config.yaml` in the background, with a backup of the previous file like any other save. It is not a pending change: `apply` has nothing to write for it. If `config.yaml` cannot be read or does not validate, the language still changes for this run and the status line says it could not be saved; the file is left as it was. If the environment variable `GITID_LANG` is set, the screen says so: your choice is saved, but the variable is used again the next time gitid starts. `esc` goes back to the start menu.

### List

The Accounts screen is a tree of companies and accounts with a platform tag (`GH`, `GL`, `BB`, `GT`, `AZ`), the auth mode, `★` for the company default and the email. Press `/` to filter by name, username, email, host or platform; matching is fuzzy inside each word and the tree order is kept. From 84 columns a detail pane sits next to the list; from 60 columns (and 24 rows) it sits below the list; on smaller terminals `enter` opens it. The pane explains what the selected company or account does ("What happens"); for an account it also shows the remote it produces, its folder and the next step. Two accounts of one company with the same platform, host and username are flagged with `!`. A company without an account is marked `no accounts yet`: gitid writes nothing for it until you add one with `a`.

The bottom of every screen has a two-row help bar (one row on terminals under 14 rows). Row 1 holds the keys of the selected row or of the current step, with `▸` on the next step to take; row 2 holds the keys that are always available (on the list: `↑↓` move, `A` add company, `a` add account), then `? all keys`, then the exit key (`q` quit on the start menu, `esc` back elsewhere) last. Labels in front of the rows ("This row:", "Always:") show only while every key of the row fits with them. The list pane has a heavy border, which marks where the arrow keys act; the detail pane next to it or below it has a thin one. Press `?` or `ctrl+k` for the shortcuts overlay: it is searchable. Type to filter by description (accents and capitals do not matter: `cong ty` finds "thêm công ty") or by key, move with `↑`/`↓`, and press `enter` to run the selected row, exactly as if you had pressed its key. `esc` clears the filter, then closes; `?` and `ctrl+k` close at any time; `q` closes while the filter is empty. Rows that only move (`↑/↓`, paging) and the "every screen" rows are shown dimmed and cannot be run.

```text
▌ acme  ACME Corp                   ~/Code/acme/  2 accounts
 ├─ ★ alice      GH https                  alice@example.com
 └─   carol      GH ssh                    carol@example.com
▪ globex  Globex                  ~/Code/globex/  1 account
 └─ ★ dave       GL ssh                  dave@globex.example

                          ● 6 unapplied change(s) — press p
 ▸ p preview & apply  a add account  e edit  d delete  esc back  ? all keys
```

The right side of the status bar always says what is left to do: unapplied changes (highlighted), a company that has no account yet, or `✓ Applied, nothing pending`. The help bar shows only the keys that matter now; the next step comes first, marked `▸`, and `?` lists every key.

`q` quits on the start menu and goes back one step on every other screen, like `esc` (inside a text field `q` is typed as a letter). `r` reloads `config.yaml` and `keys.yaml` from disk, for example after editing them by hand. On the welcome screen `enter` adds the first company. Every key above can be changed, see [Custom key bindings](#custom-key-bindings).

### Add and edit accounts

`A` adds a company, `a` adds an account to the selected company, `e` edits, `d` deletes. Forms validate as you go with the same rules the CLI uses. Choosing SSH offers to create the key right after saving. After every change you see the diff and confirm it.

### Clone and the repo browser

`c` opens the repo browser. It has three tabs, switched with `tab` and `shift+tab`: **Local**, **URL** and **Online**. It opens on **URL**, so pasting a URL and pressing `enter` works exactly as before.

**URL tab.** gitid asks for a URL, ranks your accounts (the ones on the same host first), previews the rewritten URL and the target folder, then clones in the background with a progress bar. Afterwards it checks which email git will use in the new repository. The tabs cannot be switched while a clone runs.

```text
Original URL: https://github.com/org/repo.git
Clone URL:    https://alice@github.com/org/repo.git
Directory:    ~/Code/acme/repo
✓ Clone finished
✓ user.email in the repository: alice@example.com (correct).
```

If authentication fails in the background, gitid offers to retry on the terminal, where a password or passphrase prompt works.

**Local tab.** When the browser opens, gitid scans the base folder of every company in the background and lists the repositories it finds, grouped by company, with the account the remote maps to and what is wrong. The tab label shows the total (`Local 42`; `Local …` while it is scanning).

```text
◂ All companies ▸  7 repos · 5 problems
acme  ~/Code/acme/  5 repos · 3 problems
▌✓ api            acme/alice
 ! org/x          acme/alice   wrong username/alias
 ! bb                          foreign host
 ! dup            acme/alice   ×2
globex  ~/Code/globex/  0 repos · 0 problems
  ! ~/Code/globex/ does not exist yet — clone a repo to create it
```

| Key | Action |
| --- | ------ |
| `k` or `enter` | check the selected repository (the Check screen below) |
| `f` | check it and go straight to the fix-remote question; only offered when there is a fix |
| `c` | copy the absolute path (if the copy fails, the path is shown in the status bar) |
| `/` | filter by path, account or company |
| `!` | show only the repositories with a problem |
| `←` `→` | switch between all companies and one company |
| `r` | scan again |
| `esc` | stop a running scan; a second `esc` leaves the screen |

Each row shows a mark (`✓` fine, `!` needs attention, `✗` git cannot read it, `·` a bare repository), the path below the company folder, the account and a short note. The status can be: wrong username/alias (the account matches but the remote is not in its form), unknown account (a known host, but no account with that username), foreign host, no remote, unreadable remote, ambiguous, git error, and unsafe owner (git refuses the folder because of `safe.directory`). Flags: the email in use differs from the account's, `user.email` is set in the repository's own `.git/config`, the repository is in another company's folder, the same remote is cloned more than once (`×2`; a worktree does not count), and a symlink to a repository. A worktree, a submodule and a bare repository are marked as such. The block under the list shows the details of the selected repository and, when there is a fix, the remote it would set.

How the scan works: it looks up to 4 folders deep below each company folder, visits at most 20 000 folders per company (then it says so and shows what it found), and skips `node_modules`, `vendor` and hidden folders. It never goes into a repository and never follows a symlink (a symlink that points straight at a repository is listed, marked as a symlink). A folder it cannot read is reported, with the first three paths. One `git config` call per repository, 8 at a time, reads the remote and the email git would use. The result is kept until you quit gitid: opening the browser again shows it at once, `r` scans again, a changed configuration is applied to the same result without scanning, and a finished clone or fix updates just that repository. Leaving the screen stops a running scan.

A company that has no folder yet says so ("does not exist yet — clone a repo to create it"); with no company the tab asks you to add one on the main list first.

**Online tab.** Lists the repositories each account of your configuration can see on its platform (GitHub and GitHub Enterprise Server, GitLab and self-hosted GitLab, Gitea, Bitbucket Cloud and Azure DevOps Services). Only accounts that have a token are fetched, and nothing is looked up or sent until you open this tab. `left` and `right` switch account; the bar shows the account, its platform and where its token comes from (`token: env`, `keychain`, `gh`, `glab`, `git` or `none`). The token itself is never shown.

```text
◂ acme/alice · GitHub · token: gh ▸  (1/5)
  Token of @alice · 5 repos
▌✓ acme/api         priv  3d  +2 acc  ~/Code/acme/api
 ! acme/x                 3d  +1 acc  ~/Code/acme/org/x
   acme-org/tools   empty 3d  +1 acc
```

Each row shows `owner/name`, badges (`priv`, `arch`, `fork`, `empty`), how long ago it was updated, `+N acc` when another account of yours sees the same repository, and, when you already have it on this machine, where (`✓` fine, `!` the local clone has a problem). The block under the list shows the clone URL as the account would use it and the target folder.

| Key | Action |
| --- | ------ |
| `enter` | not cloned: open the clone flow at the account pick, with this account chosen; cloned: open the Check screen for the existing clone |
| `c` | copy the clone URL (rendered for the account, no token) |
| `o` | open the repository's web page in the browser |
| `/` | filter by `owner/name` |
| `←` `→` | switch account |
| `r` | forget the token of this account, look for it again and fetch again |
| `esc` | stop a running fetch (the pages already fetched stay); a second `esc` leaves the screen |

The clone URL is built from the host in your configuration and the repository path, then rendered like any URL you paste, so it uses the account's username or its ssh alias. The API token is never used for the clone: git signs in with its own credentials, as always. If `<base folder>/<repo>` already exists as something else, gitid clones into `<base folder>/<owner>-<repo>` and says so; if that exists too, enter does nothing and the block under the list says to use the URL tab with another folder. A repository is matched with a local clone by `host/owner/repo` (renamed or transferred repositories are not followed). Before a clone starts gitid checks that `config.yaml` has not changed since the screen loaded it; if it has, it reloads and asks you to check the account again.

#### `gitid repos`

`gitid repos` prints the same scan as the Local tab, grouped by company (one line per repository, `!` in front of the ones that need attention, every line at most 80 columns). `--company acme` limits it to one company, `--depth N` (1 or more, default 4) to folders N levels below its base folder, `--problems` to the repositories that need attention. `--json` prints the stable schema below. Exit codes: `0` listed, nothing wrong; `2` something needs attention (a local problem, or an online account that failed); `1` an error (bad flag, invalid configuration, unknown company or account, or Ctrl-C, after printing what it had).

`gitid repos --online` lists what each account can see, using the same token chain as the Online tab (it never prints a token). Accounts with no token show `token: none` and the hint; that is not a failure (exit `0`). `--account acme/alice` lists one account (and resolves only its token), `--max-pages N` (1 to 500) caps the pages per account (`truncated` is then true), `--local` adds the local scan to the output (it is always run to fill `local_paths`).

```json
{"local": {"companies": [{"slug": "acme", "base_dir": "~/Code/acme/", "state": "ok", "truncated": false, "denied": 0,
   "repos": [{"path": "/abs/path", "kind": "repo", "remote": "...", "key": "github.com/acme/api", "account": "acme/alice",
              "reason": "username", "effective_email": "...", "email_origin": "...", "expected_email": "...",
              "status": "ok", "flags": [], "belongs_to": "", "duplicates": [], "fix_to": ""}]}]},
 "online": {"accounts": [{"account": "acme/alice", "platform": "github", "host": "github.com", "token_source": "gh",
   "login": "alice", "state": "ok", "error": "", "error_kind": "", "truncated": false, "warnings": [],
   "repos": [{"full_name": "acme/api", "owner": "acme", "name": "api", "private": true, "archived": false, "fork": false,
              "empty": false, "updated_at": "2026-11-10T10:00:00Z", "web_url": "...", "clone_url": "...", "local_paths": []}]}]}}
```

`state` of an online account is `ok`, `no_token`, `error` or `cancelled`; `error` holds the message in the active language and `error_kind` a stable code. `local` is left out with `--online` unless `--local` is given; `online` is left out without `--online`.

#### Online tab and tokens

For each account gitid looks for a token in this order and stops at the first hit:

1. **Environment variable** `GITID_TOKEN_<COMPANY>__<ACCOUNT>`: upper case, `-` written as `_`, two underscores between the two parts. For `acme/alice` it is `GITID_TOKEN_ACME__ALICE`. If two accounts map to the same name, the variable is ignored and the tab says so. gitid does not read `GITHUB_TOKEN`, `GH_TOKEN` or `GITLAB_TOKEN`: they cannot tell accounts apart. The `gh` program may read them itself when gitid asks it for a token (source 3), so unset them if you want the token of the account's own `gh` login. gitid also removes every `GITID_TOKEN_*` variable from the environment of the programs it starts.
2. **gitid Keychain item** (macOS): service `gitid`, account `<host>/<username>` (for example `github.com/alice`), read with `security find-generic-password -w`. Create it with `gitid token set <company>/<account>` (below) or with `security` yourself.
3. **`gh` or `glab` login:** `gh auth token --hostname=<host> --user=<username>` (older `gh` without `--user` is asked without it), or `glab config get token --host=<host>`. `glab` keeps one token per host.
4. **git's credential helper:** `git credential fill` for `https://<host>` and your username, the secret git already uses to push. gitid runs it only when you open the Online tab, with `GIT_TERMINAL_PROMPT=0`, no askpass program and `credential.interactive` set to `never` and `false` (the two values Git Credential Manager documents for "no prompts"), so it cannot ask you anything; it never runs `approve` or `reject`, so it never changes what the helper stores. The first read may still make macOS show its own "allow access to the Keychain" dialog, which gitid cannot suppress. What the helper stores may be a password rather than a token; GitHub then answers 401 and the tab says the stored credential looks like a password.

The token is kept in memory for the session (so a Keychain dialog is not repeated) and is never written to disk by gitid. It is never put in a command line, a URL, a log or a message. If a source has nothing or fails, the next one is tried; after an API call fails there is no fallback, and the error names the source so you know which secret to fix (`r` looks again).

**Storing a token.** `gitid token set acme/alice` reads the token from stdin: pipe it (`pbpaste | gitid token set acme/alice`) or type it when asked (nothing is echoed). Only printable ASCII without spaces, 1 to 4096 characters, is accepted. gitid runs `security -i` and sends it `add-generic-password -U -s gitid -a "<host>/<username>" -X <hex of the token>` on **stdin**, so the token is in no command line (`security add-generic-password -w <token>` would show it in `ps`, and `-w` at the end prompts on the terminal, not on a pipe). An existing item is replaced. `gitid token rm acme/alice` deletes the item (nothing to remove is not an error). `gitid token status` shows, for each account, which source would be used (`env`, `keychain`, `gh`, `glab`, `git` or `none`) and the variable name to set; it never prints a token. Because it runs the same lookup as the Online tab, git's credential helper may show its own Keychain dialog. On other systems `token set` and `token rm` say they are not supported and name the variable to set instead.

Before anything is listed gitid asks the API who the token belongs to (`GET /user`). If that login is not the account's username (case does not matter), nothing is listed and the tab says "this token belongs to @x". Scopes needed: GitHub classic token `repo` (or `public_repo` for public repositories only), fine-grained token "Metadata: read"; GitLab `read_api`; Gitea `read:repository` and `read:organization`; Bitbucket Cloud API token `read:repository:bitbucket` and `read:workspace:bitbucket` (not confirmed: Atlassian does not list the scopes of these calls); Azure DevOps PAT `Code (Read)`. gitid never asks for or creates a token.

Per platform: **Gitea** (your host, `https://<host>/api/v1`) sends `Authorization: token <t>` and lists `/user/repos` plus `/orgs/<org>/repos` for each of `/user/orgs`. **Bitbucket Cloud** (only `bitbucket.org`, `https://api.bitbucket.org/2.0`) uses an Atlassian API token (app passwords stopped working on 9 June 2026): with an email set on the account, Basic auth with the email, then a Bearer header; without one, a Bearer header, then Basic auth with the account username; it lists `/user/workspaces` and `/repositories/<workspace>?role=member` for each. The account `username` must match the Bitbucket nickname. **Azure DevOps Services** (only `dev.azure.com`) uses a PAT in Basic auth with an empty user name (or a Microsoft Entra token as Bearer) and lists `https://dev.azure.com/<org>/_apis/git/repositories?api-version=7.1` for each organization in the account's `orgs`; without `orgs` gitid tries to find them (`app.vssps.visualstudio.com`), which Microsoft may refuse for a PAT, and then says to set `orgs`. Azure does not say who a PAT belongs to or when a repository changed, so those are not shown.

Requests are `GET` only, to `https://api.github.com` (GitHub.com), `https://<host>/api/v3` (GitHub Enterprise Server) or `https://<host>/api/v4` (GitLab), with a 30 s limit each, at most 3 accounts at the same time, at most 50 pages of 100 repositories per account (50 per page on Gitea, at most 100 organizations or workspaces; then the tab says it stopped) and at most 8 MiB per page. A redirect or "next page" link to another host, or to plain `http`, is refused, so the token never goes anywhere but the account's own host. `HTTPS_PROXY` and `NO_PROXY` are honoured; certificates are checked against the system roots plus the file named by `SSL_CERT_FILE` if set (verification is never turned off). A rate limit shorter than 10 s is waited out once; a longer one is reported with its reset time and the pages already fetched stay. Not supported: GitHub Enterprise with a path prefix, GitHub Enterprise Cloud with data residency (`*.ghe.com`), GitLab under a relative URL root, Bitbucket Data Center and Azure DevOps Server (an account on another host than `bitbucket.org` or `dev.azure.com` is shown as unsupported: nothing is requested, and it is not an error).

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

`D` runs every diagnostic at the same time, each with its own spinner: git version, configuration validity, credential helper, the ssh `Include`, `gitid.conf`, keys and their permissions, pending changes, base folders, alias collisions and ambiguous patterns. Press `s` to also test `ssh -T` against each alias (one of the three things that use the network, and only when you press it).

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

# Security policy

gitid edits `~/.gitconfig`, `~/.ssh/config` and files under `~/.ssh/`, so
security problems matter. Thank you for reporting them responsibly.

## Reporting a vulnerability

Please **do not open a public issue** for a security problem.

Report it privately through GitHub Security Advisories:

<https://github.com/tinhnd1902/gitid/security/advisories/new>

Include, as far as you can:

- what you found and why it is a security problem,
- the gitid version (`gitid version`), your OS and your `git --version`,
- the smallest steps or a minimal configuration that reproduces it,
- the impact you expect (for example "writes outside the managed block").

Please redact tokens, private keys and personal data. You never need to send a
private key: gitid does not read them, and neither should a report.

You can expect an acknowledgement within a few days. After that we agree on a
fix and a disclosure date together, and the advisory credits you unless you
prefer to stay anonymous.

## Supported versions

gitid is young. Only the latest release receives security fixes.

| Version        | Supported |
| -------------- | --------- |
| latest release | yes       |
| anything older | no        |

## What counts as a vulnerability

Examples that are in scope:

- gitid writes, deletes or reads a file outside the paths documented in the
  README ("Safety"), or reads or prints a private key.
- A crafted remote URL, username, name, email, host or directory name that
  injects a git or ssh option or config line (for example a newline, a leading
  `-`, or a `ProxyCommand`).
- A password or token that is stored, written to a config file, logged or
  shown instead of being dropped and masked.
- A way to make `apply` overwrite content outside the managed blocks, or to
  skip the backup.
- A token read for the Online tab or `gitid repos --online` (from an
  environment variable, the gitid Keychain item, `gh`/`glab` or git's
  credential helper) that is shown, logged, written to disk, put in a command
  line, a URL or an error message, or sent anywhere other than the
  `Authorization` header of a request to the API host of the account it
  belongs to (for example through a redirect or a "next page" link to another
  host, or over plain `http`). The one documented second host is
  `app.vssps.visualstudio.com`, asked only for an Azure DevOps account that
  has no `orgs`. The same goes for certificate verification being skipped, or
  for gitid changing what a credential helper stores.
- `gitid token set` putting the token anywhere but the standard input of the
  `security` tool (an argument, an environment variable, a file, a log), or
  writing any Keychain item other than service `gitid`, account
  `<host>/<username>`; `gitid token status` or `gitid repos --json` printing a
  token.
- Text from a hosting platform's API (a repository name, an error message)
  that reaches the terminal with its escape or control characters intact.
- The update check doing more than its documented request: gitid sends one
  unauthenticated `GET https://github.com/tinhnd1902/gitid/releases/latest`
  when the TUI starts, at most once every 24 hours, with no token, cookie,
  query or body. It is a vulnerability if it sends anything else (a token, a
  path, a username, configuration content), contacts another host, follows the
  redirect, reads the response body, ignores `GITID_NO_UPDATE_CHECK` (any value
  except empty, `0` or `false` turns the check off), runs outside the TUI start
  or for a development build, or shows anything from the response other than a
  version number that matched `X.Y.Z`.

Out of scope: problems that need an attacker who already controls your user
account or your configuration files, and issues in git, ssh or your terminal
themselves.

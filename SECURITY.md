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
- A token read for the Online tab (from an environment variable, the gitid
  Keychain item, `gh`/`glab` or git's credential helper) that is shown,
  logged, written to disk, put in a command line, a URL or an error message,
  or sent anywhere other than the `Authorization` header of a request to the
  API host of the account it belongs to (for example through a redirect or a
  "next page" link to another host, or over plain `http`). The same goes for
  certificate verification being skipped, or for gitid changing what a
  credential helper stores.
- Text from a hosting platform's API (a repository name, an error message)
  that reaches the terminal with its escape or control characters intact.

Out of scope: problems that need an attacker who already controls your user
account or your configuration files, and issues in git, ssh or your terminal
themselves.

package i18n

// enOnline holds the English strings of the Online tab of the repo browser.
var enOnline = map[string]string{
	"tui.key.refetch":    "refetch",
	"tui.key.copy_url":   "copy URL",
	"tui.key.open_page":  "open page",
	"tui.key.account":    "switch account",
	"tui.key.stop_fetch": "stop fetch",

	// account bar and states
	"tui.repos.online.token":         "token: %s",
	"tui.repos.online.no_accounts":   "No account yet. Add an account on the main list first.",
	"tui.repos.online.unsupported":   "Listing the repositories of %s accounts is not available yet.",
	"tui.repos.online.resolving":     "Looking for a token…",
	"tui.repos.online.fetching":      "Fetching %s · page %d · %s repos",
	"tui.repos.online.done":          "Token of @%s · %s repos",
	"tui.repos.online.cancelled":     "Fetch stopped. Press %[1]s to try again.",
	"tui.repos.online.truncated":     "Stopped after %d pages; showing %s repos.",
	"tui.repos.online.empty":         "No repos visible to @%s.",
	"tui.repos.online.no_match":      "Nothing matches “%s”.",
	"tui.repos.online.clear_filter":  "(esc: clear filter)",
	"tui.repos.online.none_selected": "Select a repository to see its details.",

	// no token
	"tui.repos.online.no_token.github": "No token. Run gh auth login, push once over HTTPS, or set %s.",
	"tui.repos.online.no_token.gitlab": "No token. Run glab auth login, push once over HTTPS, or set %s.",
	"tui.repos.online.env_ambiguous":   "Another account maps to the variable %s too, so it is ignored. Rename an account or company.",
	"tui.repos.online.invalid_value":   "The host or username of this account cannot be used to look for a token.",

	// warnings of a listing that worked
	"tui.repos.online.warn.no_repo_scope": "The token has no repo scope: private repos are hidden.",
	"tui.repos.online.warn.sso_partial":   "Some organizations are hidden until the token is authorized for SSO.",

	// errors
	"tui.repos.online.err.auth":         "The token was rejected: it is invalid or expired.",
	"tui.repos.online.err.scope":        "The token may not list repositories: a scope or permission is missing.",
	"tui.repos.online.err.sso":          "The organization requires SAML single sign-on for this token.",
	"tui.repos.online.err.otp":          "The stored credential is a password that needs a one-time code, not a token.",
	"tui.repos.online.err.rate_limit":   "Rate limit reached. It resets at %s.",
	"tui.repos.online.err.rate_unknown": "Rate limit reached. Try again later.",
	"tui.repos.online.err.network":      "Cannot reach the host.",
	"tui.repos.online.err.dns":          "The host name does not resolve. Are you offline?",
	"tui.repos.online.err.timeout":      "The host did not answer in time.",
	"tui.repos.online.err.tls":          "The certificate of the host is not trusted.",
	"tui.repos.online.err.proxy":        "The proxy cannot be reached.",
	"tui.repos.online.err.server":       "The server failed (HTTP %d).",
	"tui.repos.online.err.bad_response": "The answer is not what the API sends (an HTML page, another host, or too large).",
	"tui.repos.online.err.wrong_user":   "This token belongs to @%s, not to this account.",
	"tui.repos.online.err.insecure":     "The host of this account is not a plain https host.",
	"tui.repos.online.err.cancelled":    "Cancelled.",
	"tui.repos.online.err.other":        "Could not list the repositories.",

	// hints
	"tui.repos.online.hint.auth.env":          "Check the value of %s.",
	"tui.repos.online.hint.auth.keychain":     "Replace the gitid Keychain item (service gitid, account %s).",
	"tui.repos.online.hint.auth.gh":           "Run gh auth login to sign in again.",
	"tui.repos.online.hint.auth.glab":         "Run glab auth login to sign in again.",
	"tui.repos.online.hint.auth.git":          "git's stored credential was refused. Sign in with gh, or set %s to a personal access token.",
	"tui.repos.online.hint.auth.git_password": "The stored credential looks like a password. Create a personal access token and set %s, or sign in with gh.",
	"tui.repos.online.hint.wrong_user":        "Sign in as this account (gh auth login, glab auth login) or set %s.",
	"tui.repos.online.hint.scope.github":      "Give the token the repo scope (classic) or Metadata: read (fine-grained).",
	"tui.repos.online.hint.scope.gitlab":      "Give the token the read_api scope.",
	"tui.repos.online.hint.sso":               "Authorize the token for the organization: %s",
	"tui.repos.online.hint.sso_nourl":         "Authorize the token for the organization in your token settings.",
	"tui.repos.online.hint.tls":               "Install the CA in the Keychain, or set SSL_CERT_FILE to a PEM file.",
	"tui.repos.online.hint.proxy":             "Check HTTPS_PROXY and NO_PROXY.",
	"tui.repos.online.hint.retry":             "Check the connection, then press %[1]s.",
	"tui.repos.online.hint.otp":               "Create a personal access token and set %s.",
	"tui.repos.online.hint.bad_response":      "Check the host of the account in the configuration.",

	// rows and details
	"tui.repos.online.badge.private":  "priv",
	"tui.repos.online.badge.archived": "arch",
	"tui.repos.online.badge.fork":     "fork",
	"tui.repos.online.badge.empty":    "empty",
	"tui.repos.online.also":           "+%d acc",
	"tui.repos.online.d.cloned":       "Cloned at",
	"tui.repos.online.d.clone_hint":   "Press enter to clone.",
	"tui.repos.online.d.check_hint":   "Already cloned. Press enter to check it.",
	"tui.repos.online.d.problem_hint": "The local clone has a problem. Press enter to check it.",
	"tui.repos.online.d.unclonable":   "gitid cannot clone this name.",
	"tui.repos.online.d.taken":        "The target folders exist already (last tried: %s). Use the URL tab with another folder.",
	"tui.repos.online.stale":          "config.yaml changed — reloaded, check the account again.",
	"tui.repos.online.copied":         "Copied: %s",
	"tui.repos.online.copy_failed":    "Could not copy. The URL is: %s",
	"tui.repos.online.opened":         "Opened: %s",
	"tui.repos.online.open_failed":    "Could not open the page: %s",

	// notes of a clone (internal/app)
	"online.warn.dir_fallback":  "That folder is taken; cloning into %s.",
	"online.warn.other_account": "git would use %s for this URL.",
	"online.warn.no_account":    "No account matches this URL (check the orgs of the account).",
}

func init() {
	for k, v := range enOnline {
		en[k] = v
	}
}

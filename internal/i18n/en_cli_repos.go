package i18n

// enCLIRepos holds the English strings of `gitid repos` and `gitid token`,
// and the Online strings the Gitea, Bitbucket and Azure DevOps listings add.
var enCLIRepos = map[string]string{
	// gitid repos
	"cli.repos.short":         "List the repositories of your companies (local clones, or online with --online)",
	"cli.flag.company":        "only this company (slug)",
	"cli.flag.depth":          "how deep to look for repositories under a company folder (default 4)",
	"cli.flag.problems":       "show only the repositories that need attention",
	"cli.flag.online":         "list what each account can see on its platform (needs a token)",
	"cli.flag.local":          "with --online, list the local clones too",
	"cli.flag.max_pages":      "most pages to fetch per account (default 50)",
	"cli.repos.depth_invalid": "--depth must be 1 or more.",
	"cli.repos.pages_invalid": "--max-pages must be between 1 and 500.",
	"cli.repos.company_none":  "No company %q in the configuration.",
	"cli.repos.cancelled":     "Cancelled. The output above is partial.",
	"cli.repos.token":         "token: %s",
	"cli.repos.token_none":    "token: none",
	"cli.repos.login":         "@%s",
	"cli.repos.unsupported":   "not available for %s accounts",

	// gitid token
	"cli.token.short":         "Store, remove or show the API token of an account (macOS Keychain)",
	"cli.token.set.short":     "Store a token read from stdin in the macOS Keychain",
	"cli.token.rm.short":      "Remove the gitid Keychain item of an account",
	"cli.token.status.short":  "Show where the token of each account comes from (never the token)",
	"cli.token.prompt":        "Token for %s (not shown): ",
	"cli.token.saved":         "Saved the token of %s in the macOS Keychain (service gitid).",
	"cli.token.removed":       "Removed the Keychain item of %s.",
	"cli.token.nothing":       "Nothing to remove for %s.",
	"cli.token.unsupported":   "Storing a token works on macOS only. Set the variable %s instead.",
	"cli.token.bad":           "The token must be 1 to 4096 printable ASCII characters, without spaces.",
	"cli.token.empty":         "No token was given. Pipe it on stdin, or type it when asked.",
	"cli.token.failed":        "The security command failed; nothing was stored.",
	"cli.token.rm_failed":     "The security command failed; nothing was removed.",
	"cli.token.bad_account":   "The host or username of this account cannot be stored in the Keychain.",
	"cli.token.no_secret_tty": "Cannot read a hidden token here. Pipe it on stdin instead.",
	"cli.token.status.none":   "none (set %s, or run: gitid token set %s)",
	"cli.token.status.pw":     "%s (looks like a password, not a token)",

	// Online: Gitea, Bitbucket, Azure DevOps
	"tui.repos.online.no_token.gitea":       "No token. Set %[1]s, or run: gitid token set %[2]s",
	"tui.repos.online.no_token.bitbucket":   "No token. Set %[1]s, or run: gitid token set %[2]s",
	"tui.repos.online.no_token.azure":       "No token. Set %[1]s to a PAT, or run: gitid token set %[2]s",
	"tui.repos.online.hint.scope.gitea":     "Give the token the read:repository and read:organization scopes.",
	"tui.repos.online.hint.scope.bitbucket": "Give the API token the read:repository:bitbucket and read:workspace:bitbucket scopes.",
	"tui.repos.online.hint.scope.azure":     "Give the PAT the Code (Read) scope for the organization.",
	"tui.repos.online.hint.azure_signin":    "Create a new PAT for the organization with the Code (Read) scope.",
	"tui.repos.online.hint.azure_orgs":      "Azure could not list your organizations with this token. Add them to orgs of the account.",
	"tui.repos.online.warn.no_orgs":         "No Azure DevOps organization found. Add them to orgs of the account.",
	"tui.repos.online.warn.org_failed":      "The organization %s could not be listed.",
	"tui.repos.online.warn.ws_failed":       "The workspace %s could not be listed.",
	"tui.repos.online.warn.orgs_failed":     "The organizations of this user could not be listed; their repos are missing.",
	"tui.repos.online.done_nologin":         "%s repos",
	"tui.repos.online.empty_nologin":        "No repos visible to this token.",
}

func init() {
	for k, v := range enCLIRepos {
		en[k] = v
	}
}

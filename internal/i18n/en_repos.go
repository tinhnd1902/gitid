package i18n

// enRepos holds the English strings of the repo browser (the local scan and
// the Repo screen).
var enRepos = map[string]string{
	"tui.key.problems": "problems only",

	// repo browser: the Repo screen with its Local · URL · Online tabs
	"tui.repos.title":      "Repo",
	"tui.repos.tab.local":  "Local",
	"tui.repos.tab.url":    "URL",
	"tui.repos.tab.online": "Online",
	"tui.key.tab":          "switch tab",
	"tui.key.group":        "switch company",
	"tui.key.copy_path":    "copy path",
	"tui.key.rescan":       "rescan",
	"tui.key.stop_scan":    "stop scan",

	// Online tab (placeholder in this version)
	"tui.repos.online.title": "Online repositories",
	"tui.repos.online.soon":  "Listing the repositories of your accounts is coming soon.",
	"tui.repos.online.hint":  "For now, paste a repository URL on the URL tab, or browse the repositories already on this machine on the Local tab.",

	// Local tab
	"tui.repos.local.all":               "All companies",
	"tui.repos.local.repos_one":         "1 repo",
	"tui.repos.local.repos_many":        "%d repos",
	"tui.repos.local.problems_one":      "1 problem",
	"tui.repos.local.problems_many":     "%d problems",
	"tui.repos.local.problems_only":     "problems only",
	"tui.repos.local.scanning":          "Scanning %s · %s folders · %d repos",
	"tui.repos.local.scanning_wait":     "Scanning…",
	"tui.repos.local.cancelled":         "Scan cancelled. Press %[1]s to scan again.",
	"tui.repos.local.no_company":        "No company yet. Add a company on the main list first.",
	"tui.repos.local.missing":           "%s does not exist yet — clone a repo to create it",
	"tui.repos.local.not_dir":           "%s is not a folder.",
	"tui.repos.local.empty":             "No repos under %s",
	"tui.repos.local.denied_one":        "1 folder could not be read: %s",
	"tui.repos.local.denied_many":       "%d folders could not be read: %s",
	"tui.repos.local.truncated":         "Stopped after %s folders; showing what was found.",
	"tui.repos.local.no_match":          "Nothing matches “%s”.",
	"tui.repos.local.no_problems":       "No problems found. Press %[1]s to show every repo.",
	"tui.repos.local.company_cancelled": "scan cancelled",
	"tui.repos.local.filter_hint":       "to filter by path, account, remote…",
	"tui.repos.local.clear_filter":      "(esc: clear filter)",
	"tui.repos.local.none_selected":     "Select a repository to see its details.",

	// statuses, flags and kinds of a local repository
	"tui.repos.status.wrong_remote":    "wrong username/alias",
	"tui.repos.status.unknown_account": "unknown account",
	"tui.repos.status.foreign_host":    "foreign host",
	"tui.repos.status.no_remote":       "no remote",
	"tui.repos.status.unparsable":      "unreadable remote",
	"tui.repos.status.ambiguous":       "ambiguous",
	"tui.repos.status.broken":          "git error",
	"tui.repos.status.dubious":         "unsafe owner",
	"tui.repos.flag.email_mismatch":    "email differs",
	"tui.repos.flag.local_override":    "local email",
	"tui.repos.flag.wrong_folder":      "wrong folder → %s",
	"tui.repos.flag.duplicate":         "×%d",
	"tui.repos.flag.via_symlink":       "symlink",
	"tui.repos.kind.worktree":          "worktree",
	"tui.repos.kind.submodule":         "submodule",
	"tui.repos.kind.bare":              "bare",

	// the hint line of the detail block
	"tui.repos.hint.fix":             "Press %[1]s to fix the remote: %[2]s",
	"tui.repos.hint.wrong_remote":    "The remote is not in the form of the account.",
	"tui.repos.hint.unknown_account": "No account has this username on this host.",
	"tui.repos.hint.foreign_host":    "No account is set up for this host.",
	"tui.repos.hint.no_remote":       "The repository has no remote.",
	"tui.repos.hint.unparsable":      "The remote URL cannot be read.",
	"tui.repos.hint.ambiguous":       "Several accounts match this remote.",
	"tui.repos.hint.bare":            "Bare repository: no working tree, nothing to check.",
	"tui.repos.hint.wrong_folder":    "The account of this remote belongs to company %s, not to this folder.",
	"tui.repos.hint.duplicate":       "The same remote is also cloned at: %s",
	"tui.repos.hint.email_mismatch":  "The email in use (%s) differs from the account's (%s).",
	"tui.repos.hint.local_override":  "user.email is set in the repository's own .git/config.",
	"tui.repos.hint.ok":              "This repository uses the right account.",
	"tui.repos.copied":               "Copied: %s",
	"tui.repos.copy_failed":          "Could not copy. The path is: %s",

	// scan (internal/app)
	"scan.detail.broken":  "git cannot read this repository.",
	"scan.detail.dubious": "git refuses this repository because of its owner (safe.directory).",
}

func init() {
	for k, v := range enRepos {
		en[k] = v
	}
}

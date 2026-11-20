package i18n

// enHome holds the English strings of the start menu (internal/tui/home.go).
var enHome = map[string]string{
	"tui.home.title":   "Home",
	"tui.home.tagline": "The right Git account for every repo.",

	"tui.home.item.accounts":            "Accounts",
	"tui.home.item.clone":               "Clone a repo",
	"tui.home.item.check":               "Check repo",
	"tui.home.item.repos":               "Browse repos",
	"tui.home.item.apply":               "Apply",
	"tui.home.item.doctor":              "Doctor",
	"tui.home.item.import":              "Import from git",
	"tui.home.item.accounts.desc_empty": "Start here: add your first company",
	"tui.home.item.accounts.desc":       "Companies, accounts and SSH keys",
	"tui.home.item.clone.desc":          "Into the right folder, with the right account",
	"tui.home.item.check.desc":          "Which account the repo in this folder uses",
	"tui.home.item.repos.desc":          "Repos on this machine and on your platforms",
	"tui.home.item.apply.desc":          "Preview, then write ~/.gitconfig and ~/.ssh",
	"tui.home.item.doctor.desc":         "Check git, SSH and the configuration",
	"tui.home.item.import.desc":         "Read the includeIf rules already in ~/.gitconfig",

	// the update line; the first argument is a version (digits and dots)
	"tui.home.update.brew":    "gitid %s is out · run brew upgrade gitid",
	"tui.home.update.script":  "gitid %s is out · update with install.sh, as when you installed it",
	"tui.home.update.unknown": "gitid %s is out · see github.com/tinhnd1902/gitid/releases",

	// the command inside the line above (shown in bold); not translated
	"tui.home.update.brew_cmd":   "brew upgrade gitid",
	"tui.home.update.script_cmd": "install.sh",

	"tui.key.home_open": "open",
	"tui.key.jump":      "jump",
}

func init() {
	for k, v := range enHome {
		en[k] = v
	}
}

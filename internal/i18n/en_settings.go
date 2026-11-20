package i18n

// enSettings holds the English strings of the Settings screen
// (internal/tui/settings.go) and of its entry on the start menu.
var enSettings = map[string]string{
	"tui.home.item.settings":      "Settings",
	"tui.home.item.settings.desc": "Interface language",

	"tui.settings.title": "Settings",
	"tui.settings.lang":  "Language",

	// the names of the languages are written in the language itself, so they
	// are the same in both tables
	"tui.settings.lang.vi": "Tiếng Việt",
	"tui.settings.lang.en": "English",

	"tui.settings.lang.env_note":    "GITID_LANG is set: gitid uses it again the next time it starts.",
	"tui.settings.lang.saved":       "Language saved: %s",
	"tui.settings.lang.save_failed": "Could not save the language: %s",

	"tui.key.change":      "change",
	"tui.key.change_next": "next option",
}

func init() {
	for k, v := range enSettings {
		en[k] = v
	}
}

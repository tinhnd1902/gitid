package i18n

// viSettings holds the Vietnamese strings of the Settings screen
// (internal/tui/settings.go) and of its entry on the start menu.
var viSettings = map[string]string{
	"tui.home.item.settings":      "Cài đặt",
	"tui.home.item.settings.desc": "Ngôn ngữ giao diện",

	"tui.settings.title": "Cài đặt",
	"tui.settings.lang":  "Ngôn ngữ",

	// the names of the languages are written in the language itself, so they
	// are the same in both tables
	"tui.settings.lang.vi": "Tiếng Việt",
	"tui.settings.lang.en": "English",

	"tui.settings.lang.env_note":    "GITID_LANG đang được đặt: lần mở sau gitid dùng giá trị đó.",
	"tui.settings.lang.saved":       "Đã lưu ngôn ngữ: %s",
	"tui.settings.lang.save_failed": "Không lưu được ngôn ngữ: %s",

	"tui.key.change":      "đổi",
	"tui.key.change_next": "chọn tiếp",
}

func init() {
	for k, v := range viSettings {
		vi[k] = v
	}
}

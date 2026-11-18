package tui

import (
	"github.com/charmbracelet/bubbles/key"

	"github.com/tinhnd1902/gitid/internal/i18n"
)

// KeyMap holds every key binding of the TUI. Descriptions come from i18n so
// the help bar follows the selected language.
type KeyMap struct {
	Quit, ForceQuit, Help, Back, Enter, Up, Down, PageUp, PageDown key.Binding
	Filter                                                         key.Binding
	AddAccount, AddCompany, Edit, Delete, Clone, Check, Doctor     key.Binding
	Apply, GenKey, Import, Detail                                  key.Binding
	Yes, No, Toggle, Copy, Open, Fix, Retry, SSHTest, TerminalMode key.Binding
	Save, Next, Cancel                                             key.Binding
	Reload                                                         key.Binding
}

func bind(keys []string, h, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(h, desc))
}

// NewKeyMap builds the bindings in the active language.
func NewKeyMap() *KeyMap {
	t := i18n.T
	return &KeyMap{
		Quit:      bind([]string{"q"}, "q", t("tui.key.quit")),
		ForceQuit: bind([]string{"ctrl+c"}, "ctrl+c", t("tui.key.quit")),
		Help:      bind([]string{"?"}, "?", t("tui.key.help")),
		// q goes back too; inside a text input it is typed, because the
		// root only matches Back when the screen does not capture the keys.
		Back:         bind([]string{"esc", "q"}, "esc", t("tui.key.back")),
		Enter:        bind([]string{"enter"}, "enter", t("tui.key.select")),
		Up:           bind([]string{"up", "ctrl+p"}, "↑", t("tui.key.up")),
		Down:         bind([]string{"down", "ctrl+n"}, "↓", t("tui.key.down")),
		PageUp:       bind([]string{"pgup", "ctrl+b"}, "pgup", t("tui.key.pgup")),
		PageDown:     bind([]string{"pgdown", "ctrl+f"}, "pgdn", t("tui.key.pgdn")),
		Filter:       bind([]string{"/"}, "/", t("tui.key.filter")),
		AddAccount:   bind([]string{"a"}, "a", t("tui.key.add_account")),
		AddCompany:   bind([]string{"A"}, "A", t("tui.key.add_company")),
		Edit:         bind([]string{"e"}, "e", t("tui.key.edit")),
		Delete:       bind([]string{"d"}, "d", t("tui.key.delete")),
		Clone:        bind([]string{"c"}, "c", t("tui.key.clone")),
		Check:        bind([]string{"k"}, "k", t("tui.key.check")),
		Doctor:       bind([]string{"D"}, "D", t("tui.key.doctor")),
		Apply:        bind([]string{"p"}, "p", t("tui.key.apply")),
		GenKey:       bind([]string{"g"}, "g", t("tui.key.genkey")),
		Import:       bind([]string{"i"}, "i", t("tui.key.import")),
		Detail:       bind([]string{"enter"}, "enter", t("tui.key.detail")),
		Yes:          bind([]string{"y"}, "y", t("tui.key.yes")),
		No:           bind([]string{"n"}, "n", t("tui.key.no")),
		Toggle:       bind([]string{"tab", "left", "right", "h", "l"}, "tab", t("tui.key.toggle")),
		Copy:         bind([]string{"c"}, "c", t("tui.key.copy")),
		Open:         bind([]string{"o"}, "o", t("tui.key.open")),
		Fix:          bind([]string{"f"}, "f", t("tui.key.fix")),
		Retry:        bind([]string{"r"}, "r", t("tui.key.retry")),
		SSHTest:      bind([]string{"s"}, "s", t("tui.key.ssh_test")),
		TerminalMode: bind([]string{"t"}, "t", t("tui.key.terminal")),
		Save:         bind([]string{"enter"}, "enter", t("tui.key.save")),
		Next:         bind([]string{"tab"}, "tab", t("tui.key.next")),
		Cancel:       bind([]string{"esc"}, "esc", t("tui.key.cancel")),
		Reload:       bind([]string{"r"}, "r", t("tui.key.reload")),
	}
}

// Overridden descriptions for a binding (same keys, other help text).
func withDesc(b key.Binding, h, desc string) key.Binding {
	b.SetHelp(h, desc)
	return b
}

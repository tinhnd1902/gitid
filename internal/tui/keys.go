package tui

import (
	"github.com/charmbracelet/bubbles/key"

	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/keymap"
)

// KeyMap holds every key binding of the TUI. Descriptions come from i18n so
// the help bar follows the selected language. The rebindable actions come
// from internal/keymap (the defaults with the user's keys.yaml applied); the
// rest (enter, arrows, tab, esc, ctrl+c, paging) are fixed.
//
// The action names, their defaults and scopes are defined in internal/keymap;
// the KeyMap field of each action is:
//
//	quit Quit        help Help          back Back          filter Filter
//	add_account AddAccount              add_company AddCompany
//	edit Edit        delete Delete      clone Clone        check Check
//	doctor Doctor    apply Apply        ssh_key GenKey     import Import
//	reload Reload    yes Yes            no No              fix Fix
//	retry Retry      copy Copy          open Open          ssh_test SSHTest
//	terminal TerminalMode                problems Problems
//	palette Palette
type KeyMap struct {
	Quit, ForceQuit, Help, Back, Enter, Up, Down, PageUp, PageDown key.Binding
	Filter                                                         key.Binding
	AddAccount, AddCompany, Edit, Delete, Clone, Check, Doctor     key.Binding
	Apply, GenKey, Import, Detail                                  key.Binding
	Yes, No, Toggle, Copy, Open, Fix, Retry, SSHTest, TerminalMode key.Binding
	Save, Next, Cancel                                             key.Binding
	Reload, Problems, Palette                                      key.Binding

	// keys are the effective bindings the bindings above were built from; T
	// reads the labels of the hint strings from it.
	keys keymap.Map
}

func bind(keys []string, h, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(h, desc))
}

// NewKeyMap builds the bindings in the active language from the effective
// key map (keymap.Defaults() without a keys.yaml).
func NewKeyMap(m keymap.Map) *KeyMap {
	t := i18n.T
	// act binds a rebindable action: all its keys, the first one as label.
	act := func(a keymap.Action, descKey string) key.Binding {
		return bind(m.Keys(a), m.Label(a), t(descKey))
	}
	return &KeyMap{
		keys:      m,
		Quit:      act(keymap.Quit, "tui.key.quit"),
		ForceQuit: bind([]string{"ctrl+c"}, "ctrl+c", t("tui.key.quit")),
		Help:      act(keymap.Help, "tui.key.help"),
		// esc always goes back; the user's back keys (q by default) too.
		// Inside a text input they are typed, because the root only matches
		// Back when the screen does not capture the keys.
		Back:         bind(append([]string{"esc"}, m.Keys(keymap.Back)...), "esc", t("tui.key.back")),
		Enter:        bind([]string{"enter"}, "enter", t("tui.key.select")),
		Up:           bind([]string{"up", "ctrl+p"}, "↑", t("tui.key.up")),
		Down:         bind([]string{"down", "ctrl+n"}, "↓", t("tui.key.down")),
		PageUp:       bind([]string{"pgup", "ctrl+b"}, "pgup", t("tui.key.pgup")),
		PageDown:     bind([]string{"pgdown", "ctrl+f"}, "pgdn", t("tui.key.pgdn")),
		Filter:       act(keymap.Filter, "tui.key.filter"),
		AddAccount:   act(keymap.AddAccount, "tui.key.add_account"),
		AddCompany:   act(keymap.AddCompany, "tui.key.add_company"),
		Edit:         act(keymap.Edit, "tui.key.edit"),
		Delete:       act(keymap.Delete, "tui.key.delete"),
		Clone:        act(keymap.Clone, "tui.key.clone"),
		Check:        act(keymap.Check, "tui.key.check"),
		Doctor:       act(keymap.Doctor, "tui.key.doctor"),
		Apply:        act(keymap.Apply, "tui.key.apply"),
		GenKey:       act(keymap.SSHKey, "tui.key.genkey"),
		Import:       act(keymap.Import, "tui.key.import"),
		Detail:       bind([]string{"enter"}, "enter", t("tui.key.detail")),
		Yes:          act(keymap.Yes, "tui.key.yes"),
		No:           act(keymap.No, "tui.key.no"),
		Toggle:       bind([]string{"tab", "left", "right", "h", "l"}, "tab", t("tui.key.toggle")),
		Copy:         act(keymap.Copy, "tui.key.copy"),
		Open:         act(keymap.Open, "tui.key.open"),
		Fix:          act(keymap.Fix, "tui.key.fix"),
		Retry:        act(keymap.Retry, "tui.key.retry"),
		SSHTest:      act(keymap.SSHTest, "tui.key.ssh_test"),
		TerminalMode: act(keymap.Terminal, "tui.key.terminal"),
		Save:         bind([]string{"enter"}, "enter", t("tui.key.save")),
		Next:         bind([]string{"tab"}, "tab", t("tui.key.next")),
		Cancel:       bind([]string{"esc"}, "esc", t("tui.key.cancel")),
		Reload:       act(keymap.Reload, "tui.key.reload"),
		Problems:     act(keymap.Problems, "tui.key.problems"),
		Palette:      act(keymap.Palette, "tui.key.palette"),
	}
}

// hintKeys lists, for every message that names keys in its prose, the actions
// whose labels km.T puts in front of the call's own arguments: the first
// action is %[1]s, the second %[2]s, and the original arguments follow. Every
// verb of such a message carries an explicit index. The "_plain" variants of
// the status badges name no key and are not listed, so they pass through.
var hintKeys = map[string][]keymap.Action{
	"tui.status.pending":             {keymap.Apply},
	"tui.status.pending_short":       {keymap.Apply},
	"tui.status.need_company":        {keymap.AddCompany},
	"tui.status.account_saved":       {keymap.Yes},
	"tui.status.applied":             {keymap.Check},
	"tui.status.no_company":          {keymap.AddCompany},
	"tui.status.empty_company":       {keymap.AddAccount},
	"tui.status.plan_failed":         {keymap.Doctor},
	"tui.status.company_created":     {keymap.AddAccount},
	"tui.status.apply_later":         {keymap.Apply},
	"tui.status.press_yes":           {keymap.Yes},
	"tui.confirm.question":           {keymap.Yes, keymap.No},
	"tui.confirm.retry_hint":         {keymap.Retry},
	"tui.confirm.empty_no_accounts":  {keymap.AddAccount},
	"tui.confirm.next_check":         {keymap.Check},
	"tui.confirm.next_ssh":           {keymap.SSHKey},
	"tui.keyresult.apply_hint":       {keymap.Apply},
	"tui.clone.auth_hint":            {keymap.Terminal},
	"tui.check.retry_hint":           {keymap.Retry},
	"tui.check.fix_hint":             {keymap.Fix},
	"tui.check.not_repo_next":        {keymap.Clone},
	"tui.check.not_repo_next_none":   {keymap.Clone},
	"tui.empty.import":               {keymap.Import},
	"tui.list.no_accounts_hint":      {keymap.AddAccount},
	"tui.detail.how_pending":         {keymap.Apply},
	"tui.detail.how_applied":         {keymap.Check},
	"tui.detail.acc_next_applied":    {keymap.Clone, keymap.Check},
	"tui.detail.dup":                 {keymap.Delete},
	"tui.form.err.dup_account":       {keymap.Edit},
	"tui.form.warn.username_changed": {keymap.Check, keymap.Fix},
	"tui.form.discard_q":             {keymap.Yes, keymap.No},
	"tui.import.none_next":           {keymap.AddCompany},
	"tui.help.close":                 {keymap.Help, keymap.Palette, keymap.Back},
	"tui.help.close_filter":          {keymap.Help, keymap.Palette},
	"tui.repos.local.cancelled":      {keymap.Reload},
	"tui.repos.local.no_problems":    {keymap.Problems},
	"tui.repos.hint.fix":             {keymap.Fix},
}

// T translates a message like tr, but a message that names keys (see
// hintKeys) gets the labels of the effective bindings as its first arguments.
func (km *KeyMap) T(msgKey string, args ...any) string {
	acts, ok := hintKeys[msgKey]
	if !ok {
		return tr(msgKey, args...)
	}
	all := make([]any, 0, len(acts)+len(args))
	for _, a := range acts {
		all = append(all, km.keys.Label(a))
	}
	return tr(msgKey, append(all, args...)...)
}

// Overridden descriptions for a binding (same keys, other help text).
func withDesc(b key.Binding, h, desc string) key.Binding {
	b.SetHelp(h, desc)
	return b
}

// relabel gives a binding another description and keeps its own key label,
// so a rebound key shows up in the text.
func relabel(b key.Binding, desc string) key.Binding {
	b.SetHelp(b.Help().Key, desc)
	return b
}

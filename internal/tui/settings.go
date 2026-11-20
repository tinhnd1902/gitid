package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tinhnd1902/gitid/internal/i18n"
)

// The Settings screen: one row per preference. Today there is one, the
// language of the interface, shown as a radio. A choice applies at once; the
// root model switches the language and saves it (see setLangMsg), so the
// screen holds no state about it and always shows i18n.Lang().

// envLang is the variable that overrides settings.language when gitid starts.
const envLang = "GITID_LANG"

// langCodes are the languages gitid has, in the order of the radio.
var langCodes = []string{"vi", "en"}

// settingRow is one preference of the screen.
type settingRow struct {
	label   func() string
	options func() []settingOption
	current func() string
	// choose returns the command that applies the option with this code.
	choose func(code string) tea.Cmd
	// note is a muted line under the row ("" when there is nothing to say).
	note func(sh *shared) string
}

// settingOption is one choice of a row. Its name is shown as is: a language
// is written in itself, so it needs no translation key per language.
type settingOption struct{ code, name string }

// languageRow is the row of the interface language.
func languageRow() settingRow {
	return settingRow{
		label: func() string { return tr("tui.settings.lang") },
		options: func() []settingOption {
			out := make([]settingOption, len(langCodes))
			for i, c := range langCodes {
				out[i] = settingOption{c, tr("tui.settings.lang." + c)}
			}
			return out
		},
		current: i18n.Lang,
		choose:  func(code string) tea.Cmd { return func() tea.Msg { return setLangMsg{code: code} } },
		note: func(sh *shared) string {
			// only the fact is shown: the value is never echoed (it is the
			// user's environment, not text of ours)
			if v, ok := sh.svc.Env.LookupEnv(envLang); ok && v != "" {
				return tr("tui.settings.lang.env_note")
			}
			return ""
		},
	}
}

type settingsScreen struct {
	sh   *shared
	w, h int
	sel  int // the row under the cursor
	rows []settingRow
}

func newSettingsScreen(sh *shared) *settingsScreen {
	return &settingsScreen{sh: sh, rows: []settingRow{languageRow()}}
}

func (s *settingsScreen) Title() string { return tr("tui.settings.title") }
func (s *settingsScreen) Init() tea.Cmd { return nil }

func (s *settingsScreen) Resize(w, h int) { s.w, s.h = w, h }

// changeKeys is the binding of the arrows that change the choice of a row.
func changeKeys() key.Binding {
	return key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", tr("tui.key.change")))
}

// Help is the full list of the "?" overlay.
func (s *settingsScreen) Help() []key.Binding {
	km := s.sh.km
	out := []key.Binding{changeKeys(), withDesc(km.Enter, "enter", tr("tui.key.change_next"))}
	if len(s.rows) > 1 {
		out = append(out, withDesc(km.Up, "↑↓", tr("tui.key.move")))
	}
	return append(out, withDesc(km.Back, "esc", tr("tui.key.back")))
}

// ContextHelp is row 1 of the help bar: the next step.
func (s *settingsScreen) ContextHelp() []helpItem {
	return []helpItem{{b: changeKeys(), primary: true}}
}

// AlwaysHelp is row 2 of the help bar. The arrows up and down are named only
// when there is a second row to move to.
func (s *settingsScreen) AlwaysHelp() []helpItem {
	km := s.sh.km
	var out []helpItem
	if len(s.rows) > 1 {
		out = append(out, helpItem{b: withDesc(km.Up, "↑↓", tr("tui.key.move")), prio: 2})
	}
	return append(out, helpItem{b: withDesc(km.Back, "esc", tr("tui.key.back")), role: roleExit})
}

func (s *settingsScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	km := s.sh.km
	switch {
	case key.Matches(k, km.Up):
		s.sel = clamp(s.sel-1, 0, len(s.rows)-1)
	case key.Matches(k, km.Down):
		s.sel = clamp(s.sel+1, 0, len(s.rows)-1)
	case k.String() == "left":
		return s, s.step(-1)
	case k.String() == "right", key.Matches(k, km.Enter):
		return s, s.step(1)
	}
	return s, nil
}

// step moves the choice of the selected row by d options (it wraps).
func (s *settingsScreen) step(d int) tea.Cmd {
	row := s.rows[s.sel]
	opts := row.options()
	if len(opts) < 2 {
		return nil
	}
	cur := 0
	for i, o := range opts {
		if o.code == row.current() {
			cur = i
		}
	}
	next := (cur + d + len(opts)) % len(opts)
	return row.choose(opts[next].code)
}

// radios is the options of a row as radio marks on one line.
func (s *settingsScreen) radios(row settingRow) string {
	cur := row.current()
	var parts []string
	for _, o := range row.options() {
		mark := radioOff
		if o.code == cur {
			mark = radioOn
		}
		parts = append(parts, mark+o.name)
	}
	return strings.Join(parts, "  ")
}

func (s *settingsScreen) View() string {
	if s.w == 0 || s.h == 0 {
		return ""
	}
	st := s.sh.st
	n := 0
	for _, r := range s.rows {
		n = maxInt(n, lipgloss.Width(r.label()))
	}
	col := n + 2
	lines := []string{""}
	for i, r := range s.rows {
		plain := " " + pad(r.label(), col) + s.radios(r)
		if i == s.sel {
			lines = append(lines, st.Accent.Render("▌")+st.Selected.Render(pad(plain, maxInt(s.w-1, 1))))
		} else {
			lines = append(lines, " "+st.Text.Render(plain))
		}
		if note := r.note(s.sh); note != "" {
			for _, l := range wrapUnits(note, maxInt(s.w-2*homeMargin, 8)) {
				lines = append(lines, spaces(homeMargin)+st.Muted.Render(l))
			}
		}
	}
	for i, l := range lines {
		lines[i] = trunc(l, s.w)
	}
	return fitHeight(joinLines(lines), s.h)
}

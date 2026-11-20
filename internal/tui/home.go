package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tinhnd1902/gitid/internal/app"
)

// The start menu: the screen gitid opens on. A logo, a tagline with the
// version, an update line when a newer release exists, and a numbered menu of
// the tasks. The digits open an item at once; the letters the items had as
// shortcuts on the Accounts screen (clone, check, apply, doctor, import) work
// here too, so `gitid` followed by `c` still starts a clone.

// homeItem is one row of the menu.
type homeItem struct {
	name string // i18n key suffix: tui.home.item.<name>[.desc]
	// alias is the key that also opens the item (nil: digits and enter only).
	alias func(*KeyMap) key.Binding
	open  func(sh *shared) Screen
}

// homeItems are the menu rows in order; the digit of a row is its index + 1.
var homeItems = []homeItem{
	{"accounts", nil, func(sh *shared) Screen { return newListScreen(sh) }},
	{"clone", func(km *KeyMap) key.Binding { return km.Clone }, func(sh *shared) Screen { return newReposScreen(sh, nil) }},
	{"check", func(km *KeyMap) key.Binding { return km.Check }, func(sh *shared) Screen { return newCheckScreen(sh) }},
	{"repos", nil, func(sh *shared) Screen { return newReposScreenAt(sh, nil, tabLocal) }},
	{"apply", func(km *KeyMap) key.Binding { return km.Apply }, func(sh *shared) Screen { return newConfirmScreen(sh, tr("tui.confirm.title_apply")) }},
	{"doctor", func(km *KeyMap) key.Binding { return km.Doctor }, func(sh *shared) Screen { return newDoctorScreen(sh) }},
	{"import", func(km *KeyMap) key.Binding { return km.Import }, func(sh *shared) Screen { return newImportScreen(sh) }},
	{"settings", nil, func(sh *shared) Screen { return newSettingsScreen(sh) }},
}

// homeJump is the binding of the digits. They are fixed keys (like the
// arrows), reserved in the home scope of internal/keymap.
func homeJump() key.Binding {
	n := len(homeItems)
	keys := make([]string, n)
	for i := range keys {
		keys[i] = string(rune('1' + i))
	}
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp("1-"+string(rune('0'+n)), tr("tui.key.jump")))
}

// Cells of the layout.
const (
	homeMargin     = 2  // left margin of every line
	homeMinDesc    = 12 // a description narrower than this is not shown
	homeFullFixed  = 10 // rows of the full layout around the menu (see frame)
	homeCompactFix = 2  // rows of the compact layout around the menu
)

// homeFullMinW is the narrowest body that draws the logo whole: never cut.
func homeFullMinW() int { return homeMargin + logoWidth() }

type homeScreen struct {
	sh   *shared
	w, h int
	sel  int // the row under the cursor
	top  int // first menu row shown when the menu is taller than the body
}

func newHomeScreen(sh *shared) *homeScreen { return &homeScreen{sh: sh} }

func (s *homeScreen) Title() string { return tr("tui.home.title") }
func (s *homeScreen) Init() tea.Cmd { return nil }

func (s *homeScreen) Resize(w, h int) { s.w, s.h = w, h }

// Help is the full list of the "?" overlay: the keys of the menu, then the
// letters that open the same items, named by the item.
func (s *homeScreen) Help() []key.Binding {
	km := s.sh.km
	out := []key.Binding{withDesc(km.Enter, "enter", tr("tui.key.home_open")), homeJump()}
	for _, it := range homeItems {
		if it.alias != nil {
			out = append(out, relabel(it.alias(km), tr("tui.home.item."+it.name)))
		}
	}
	return append(out, km.Quit)
}

// ContextHelp is row 1 of the help bar: the next step.
func (s *homeScreen) ContextHelp() []helpItem {
	return []helpItem{{b: withDesc(s.sh.km.Enter, "enter", tr("tui.key.home_open")), primary: true}}
}

// AlwaysHelp is row 2 of the help bar: the digits, the arrows, then the exit.
func (s *homeScreen) AlwaysHelp() []helpItem {
	km := s.sh.km
	return []helpItem{
		{b: homeJump(), prio: 3},
		{b: withDesc(km.Up, "↑↓", tr("tui.key.move")), prio: 2},
		{b: km.Quit, role: roleExit},
	}
}

func (s *homeScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	km := s.sh.km
	switch {
	case key.Matches(k, km.Quit):
		return s, tea.Quit
	case key.Matches(k, homeJump()):
		return s, s.open(int(k.String()[0] - '1'))
	case key.Matches(k, km.Enter):
		return s, s.open(s.sel)
	case key.Matches(k, km.Up):
		s.move(-1)
	case key.Matches(k, km.Down):
		s.move(1)
	case key.Matches(k, km.PageUp):
		s.move(-s.page())
	case key.Matches(k, km.PageDown):
		s.move(s.page())
	case k.Type == tea.KeyHome:
		s.move(-len(homeItems))
	case k.Type == tea.KeyEnd:
		s.move(len(homeItems))
	default:
		for i, it := range homeItems {
			if it.alias != nil && key.Matches(k, it.alias(km)) {
				return s, s.open(i)
			}
		}
	}
	return s, nil
}

// open pushes the screen of row i (the cursor goes there too).
func (s *homeScreen) open(i int) tea.Cmd {
	if i < 0 || i >= len(homeItems) {
		return nil
	}
	s.sel = i
	return s.sh.pushNow(homeItems[i].open(s.sh))
}

// move moves the cursor by d rows; it stops at the ends (it does not wrap).
func (s *homeScreen) move(d int) {
	s.sel = clamp(s.sel+d, 0, len(homeItems)-1)
}

// page is how far a page key moves.
func (s *homeScreen) page() int {
	_, rows := s.frame()
	return maxInt(rows-1, 1)
}

// update is the notice of a newer release, when there is one worth showing.
func (s *homeScreen) update() (app.UpdateNotice, bool) {
	n := s.sh.update
	if !s.sh.hasUpdate || n.Latest == "" || strings.Trim(n.Latest, "0123456789.") != "" {
		return app.UpdateNotice{}, false // only digits and dots ever reach the screen
	}
	return n, true
}

// frame lays out the lines above the menu for the body size and says how many
// menu rows fit below them. The layouts, by the room they need (n menu rows,
// u the update line):
//
//	full     logo, tagline: 10+n+u rows and homeFullMinW() columns
//	compact  one line with the name and the tagline: 2+n+u rows
//	minimal  the menu alone (and the update line when it fits)
func (s *homeScreen) frame() (lines []string, rows int) {
	st := s.sh.st
	n := len(homeItems)
	notice, hasUpdate := s.update()
	u := 0
	if hasUpdate {
		u = 1
	}
	margin := spaces(homeMargin)
	ver := ""
	if v := strings.TrimPrefix(s.sh.version, "v"); v != "" {
		ver = "v" + v
	}
	inner := maxInt(s.w-homeMargin, 1)
	updateLine := func() string {
		if !hasUpdate {
			return ""
		}
		return margin + s.updateText(notice, inner)
	}
	switch {
	case s.h >= homeFullFixed+n+u && s.w >= homeFullMinW():
		lines = append(lines, "")
		for _, l := range logoLines {
			lines = append(lines, margin+st.Logo.Render(strings.TrimRight(l, " ")))
		}
		lines = append(lines, margin+s.tagline(ver, inner), "")
		if hasUpdate {
			lines = append(lines, updateLine(), "")
		}
	case s.h >= homeCompactFix+n+u:
		const name = "gitid"
		head := st.Logo.Render(name)
		if rest := inner - lipgloss.Width(name) - 3; rest > 0 {
			text := tr("tui.home.tagline")
			if ver != "" {
				text += "  " + ver
			}
			head += st.Muted.Render(" · ") + st.Text.Render(truncWords(text, rest))
		}
		lines = append(lines, margin+trunc(head, inner))
		if hasUpdate {
			lines = append(lines, updateLine())
		}
		lines = append(lines, "")
	default:
		if hasUpdate && s.h >= n+1 {
			lines = append(lines, updateLine())
		}
	}
	return lines, maxInt(s.h-len(lines), 1)
}

// tagline is the sentence in text colour and the version in muted, cut at a
// word when the line is too narrow.
func (s *homeScreen) tagline(ver string, w int) string {
	st := s.sh.st
	text := tr("tui.home.tagline")
	if ver == "" {
		return st.Text.Render(truncWords(text, w))
	}
	if lipgloss.Width(text)+2+lipgloss.Width(ver) <= w {
		return st.Text.Render(text) + "  " + st.Muted.Render(ver)
	}
	return st.Text.Render(truncWords(text+"  "+ver, w))
}

// updateText is the update line. The command is in bold, like a key inside a
// sentence. The version is digits and dots only (see update).
func (s *homeScreen) updateText(n app.UpdateNotice, w int) string {
	st := s.sh.st
	method, cmd := "unknown", ""
	switch n.Method {
	case app.UpdateViaBrew:
		method, cmd = "brew", tr("tui.home.update.brew_cmd")
	case app.UpdateViaScript:
		method, cmd = "script", tr("tui.home.update.script_cmd")
	}
	text := truncWords(tr("tui.home.update."+method, n.Latest), w)
	if cmd != "" {
		if before, after, found := strings.Cut(text, cmd); found {
			return st.Text.Render(before) + st.Bold.Render(cmd) + st.Text.Render(after)
		}
	}
	return st.Text.Render(text)
}

// labelCol is the width of the label column: the widest label of the active
// language plus two.
func labelCol() int {
	n := 0
	for _, it := range homeItems {
		n = maxInt(n, lipgloss.Width(tr("tui.home.item."+it.name)))
	}
	return n + 2
}

// desc is the description of row i. Without a company, the first row says
// where to start.
func (s *homeScreen) desc(i int) string {
	name := homeItems[i].name
	if name == "accounts" && s.sh.loaded && len(s.sh.cfg.Companies) == 0 {
		return tr("tui.home.item.accounts.desc_empty")
	}
	return tr("tui.home.item." + name + ".desc")
}

// row renders menu row i.
func (s *homeScreen) row(i, col int) string {
	st := s.sh.st
	label := tr("tui.home.item." + homeItems[i].name)
	digit := string(rune('1' + i))
	head := pad(label, col)
	// cells left for the description after the margin, the digit and the gap
	room := s.w - homeMargin - 1 - 2 - lipgloss.Width(head)
	desc := ""
	if room >= homeMinDesc {
		desc = truncWords(s.desc(i), room)
	}
	if i == s.sel {
		plain := " " + digit + "  " + head + desc
		return st.Accent.Render("▌") + st.Selected.Render(pad(plain, maxInt(s.w-2, 1)))
	}
	return trunc(spaces(homeMargin)+st.Muted.Render(digit)+"  "+st.Text.Render(head)+st.Muted.Render(desc), s.w)
}

func (s *homeScreen) View() string {
	if s.w == 0 || s.h == 0 {
		return ""
	}
	lines, rows := s.frame()
	n := len(homeItems)
	// the cursor row is always inside the window
	top := clamp(s.top, s.sel-rows+1, s.sel)
	top = clamp(top, 0, maxInt(n-rows, 0))
	s.top = top
	col := labelCol()
	for i := top; i < n && i < top+rows; i++ {
		lines = append(lines, s.row(i, col))
	}
	for i, l := range lines {
		lines[i] = trunc(l, s.w)
	}
	return fitHeight(joinLines(lines), s.h)
}

package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/i18n"
)

// Screen is one page of the TUI. The root model keeps a stack of them; only
// the top screen receives input and results of asynchronous work.
type Screen interface {
	// Init returns the first command (async loads, spinner ticks).
	Init() tea.Cmd
	// Update handles a message. It must never block: slow work belongs in a
	// tea.Cmd.
	Update(msg tea.Msg) (Screen, tea.Cmd)
	// View renders the screen into a block of exactly Resize(w, h) cells.
	View() string
	// Title is the breadcrumb label.
	Title() string
	// Help lists the bindings shown in the help bar.
	Help() []key.Binding
	// Resize gives the screen the body area.
	Resize(w, h int)
}

// helpItem is one entry of the short help bar. The primary item is the next
// step the user is expected to take; it is drawn first and emphasized.
type helpItem struct {
	b       key.Binding
	primary bool
}

// contextHelper is implemented by screens that choose the entries of the
// short help bar for their current state. Help() stays the full list shown by
// the "?" overlay.
type contextHelper interface{ ContextHelp() []helpItem }

// capturer is implemented by screens that take over the keyboard (text
// inputs, forms, busy states): the root then forwards every key, including
// esc, to the screen.
type capturer interface{ Capturing() bool }

func isCapturing(s Screen) bool {
	c, ok := s.(capturer)
	return ok && c.Capturing()
}

// shared is the state every screen can read. It is only mutated by the root
// model inside Update; commands must copy what they need before they start.
type shared struct {
	svc     *app.Service
	cfg     config.Config
	loaded  bool
	pending int  // number of files an apply would change
	planned bool // pending is known
	planErr bool // the last plan could not be computed
	st      *Styles
	km      *KeyMap
	cwd     string
	version string
	nextID  int
	nav     []navOp // navigation requested by the current Update, see applyNav
}

type navKind int

const (
	navPush navKind = iota
	navReplace
	navPop
	navPopRoot
)

type navOp struct {
	kind navKind
	s    Screen
}

// Navigation rule: key handlers use the *Now methods, which queue the change
// and let Model.Update apply it in the same call, so a key that arrives right
// after goes to the new top screen. push/replace/pop (async commands) are only
// for use inside tea.Sequence after async work, where the order relative to
// other messages matters. A screen that is not the top one must not call *Now
// while handling a broadcast message.
func (sh *shared) pushNow(s Screen) tea.Cmd {
	sh.nav = append(sh.nav, navOp{navPush, s})
	return nil
}
func (sh *shared) replaceNow(s Screen) tea.Cmd {
	sh.nav = append(sh.nav, navOp{navReplace, s})
	return nil
}
func (sh *shared) popNow() tea.Cmd {
	sh.nav = append(sh.nav, navOp{kind: navPop})
	return nil
}
func (sh *shared) popRootNow() tea.Cmd {
	sh.nav = append(sh.nav, navOp{kind: navPopRoot})
	return nil
}

// id hands out unique ids so a screen can ignore results that belong to an
// earlier run of itself.
func (sh *shared) id() int {
	sh.nextID++
	return sh.nextID
}

// Command helpers.

func push(s Screen) tea.Cmd    { return func() tea.Msg { return pushMsg{s} } }
func replace(s Screen) tea.Cmd { return func() tea.Msg { return replaceMsg{s} } }
func pop() tea.Cmd             { return func() tea.Msg { return popMsg{} } }
func popRoot() tea.Cmd         { return func() tea.Msg { return popRootMsg{} } }

func say(l level, text string) tea.Cmd {
	return func() tea.Msg { return statusMsg{level: l, text: text} }
}

// hintTTL is how long a next-step message stays in the status bar. The same
// guidance is also shown persistently (badge, detail pane, help bar), so the
// message can safely expire.
const hintTTL = 10 * time.Second

// hintMsg builds a next-step status message.
func hintMsg(text string) statusMsg {
	return statusMsg{level: levelInfo, text: text, ttl: hintTTL}
}

// hint tells the user what to do next.
func hint(text string) tea.Cmd {
	return func() tea.Msg { return hintMsg(text) }
}

// focus selects the row with this key in the list.
func focus(key string) tea.Cmd { return func() tea.Msg { return focusMsg{key} } }

func changed(cfg config.Config) tea.Cmd { return func() tea.Msg { return cfgChangedMsg{cfg} } }

func refreshPlan() tea.Cmd { return func() tea.Msg { return refreshPlanMsg{} } }

// ctxTimeout is the context for short background work.
func ctxTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// panel renders content in a rounded box of exactly w x h cells (outer).
func (sh *shared) panel(title, content string, w, h int, focus bool) string {
	if w < 4 || h < 3 {
		return ""
	}
	st := sh.st.Panel
	if focus {
		st = sh.st.PanelFocus
	}
	inner := w - st.GetHorizontalFrameSize()
	body := content
	if title != "" {
		body = sh.st.PanelTitle.Render(trunc(title, inner)) + "\n" + content
	}
	body = clipBlock(body, inner, h-st.GetVerticalFrameSize())
	return st.Width(w - st.GetHorizontalBorderSize()).Height(h - st.GetVerticalBorderSize()).Render(body)
}

// clipBlock cuts every line to w cells and the block to h lines.
func clipBlock(s string, w, h int) string {
	lines := splitLines(s)
	if len(lines) > h {
		lines = lines[:h]
	}
	for i, l := range lines {
		lines[i] = trunc(l, w)
	}
	return strings.Join(lines, "\n")
}

// wrap word-wraps plain text to w cells.
func wrap(s string, w int) string {
	if w < 8 {
		return s
	}
	return lipgloss.NewStyle().Width(w).Render(s)
}

// levelStyle and levelIcon pick the look of a status level.
func (s *Styles) levelStyle(l level) lipgloss.Style {
	switch l {
	case levelOK:
		return s.OK
	case levelWarn:
		return s.Warn
	case levelErr:
		return s.Err
	}
	return s.Info
}

func levelIcon(l level) string {
	switch l {
	case levelOK:
		return "✓"
	case levelWarn:
		return "!"
	case levelErr:
		return "✗"
	}
	return "i"
}

// tr is shorthand for i18n.T.
func tr(key string, args ...any) string { return i18n.T(key, args...) }

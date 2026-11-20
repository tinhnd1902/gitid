// Package tui is the interactive terminal interface of gitid, built with
// Bubble Tea. It talks to the rest of gitid only through internal/app.
package tui

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/keymap"
)

// Options configure the TUI.
type Options struct {
	Cwd     string // directory shown as the repo hint and used by check/clone
	Version string
	// CheckUpdates asks, once at start, whether a newer release exists (see
	// internal/update for what is sent, and when nothing is). Only the real
	// program sets it.
	CheckUpdates bool

	// startAccounts opens with the Accounts screen over the start menu (tests
	// that exercise the screens below it).
	startAccounts bool
}

const (
	minWidth     = 40
	minHeight    = 10
	statusExpiry = 5 * time.Second
	// updateTimeout bounds the update check, which runs behind the first frame.
	updateTimeout = 5 * time.Second
)

// Model is the root Bubble Tea model: a stack of screens plus the header,
// status bar and help bar that every screen shares.
type Model struct {
	sh       *shared
	opts     Options
	stack    []Screen
	w, h     int
	status   statusMsg
	statSeq  int
	planSeq  int
	showHelp bool
	ov       helpOverlay // filter and selection of the "?" overlay
	// reloading is set by the reload key: the next cfgLoadedMsg then says so.
	reloading bool
	// keyShown are the keys.yaml problems the status bar already announced,
	// so the same set is not announced again after every reload.
	keyShown []keymap.Issue
}

// New creates the root model. The configuration is loaded by Init.
func New(svc *app.Service, opts Options) *Model {
	sh := &shared{svc: svc, st: NewStyles(), km: NewKeyMap(keymap.Defaults()), cwd: opts.Cwd, version: opts.Version}
	m := &Model{sh: sh, opts: opts, ov: helpOverlay{filter: newOverlayFilter(sh.st)}}
	m.stack = []Screen{newHomeScreen(sh)}
	if opts.startAccounts {
		m.stack = append(m.stack, newListScreen(sh))
	}
	return m
}

// Run starts the TUI on the terminal.
func Run(svc *app.Service, opts Options) error {
	// Detect the terminal background once, before Bubble Tea owns the
	// terminal: the adaptive colors need it, and the query would otherwise be
	// answered into the input stream. A terminal that never answers must not
	// stall the start for long.
	detectBackground(1500 * time.Millisecond)
	model := New(svc, opts)
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err := p.Run()
	model.shutdown()
	return err
}

func (m *Model) top() Screen { return m.stack[len(m.stack)-1] }

// onHome reports whether the start menu is the screen on top.
func (m *Model) onHome() bool { _, ok := m.top().(*homeScreen); return ok }

func (m *Model) bodySize() (int, int) { return m.w, maxInt(m.h-m.chromeRows(), 1) }

// Init loads the configuration, initializes the screens on the stack and, when
// asked, starts the update check. It does no I/O itself: the first frame never
// waits for the check.
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.loadCmd()}
	for _, s := range m.stack {
		cmds = append(cmds, s.Init())
	}
	if m.opts.CheckUpdates {
		cmds = append(cmds, m.updateCmd())
	}
	return tea.Batch(cmds...)
}

// updateCmd asks the service for a newer release. Quitting does not wait for
// it, and a panic in it is no notice.
func (m *Model) updateCmd() tea.Cmd {
	svc := m.sh.svc
	return func() (msg tea.Msg) {
		defer func() {
			if recover() != nil {
				msg = updateMsg{}
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
		defer cancel()
		n, ok := svc.CheckUpdate(ctx)
		return updateMsg{notice: n, ok: ok}
	}
}

// hubDepth is the number of screens that stay when a flow says "back to the
// hub" (apply done, then enter): the start menu and, when the flow was started
// from the Accounts screen, that screen.
func (m *Model) hubDepth() int {
	if len(m.stack) > 1 {
		if _, ok := m.stack[1].(*listScreen); ok {
			return 2
		}
	}
	return 1
}

func (m *Model) loadCmd() tea.Cmd {
	svc := m.sh.svc
	return func() tea.Msg {
		cfg, err := svc.Load()
		// keys.yaml is read apart from config.yaml (never written, never part
		// of a save) and applies even when config.yaml failed to load
		keys, issues := keymap.Load(svc.Env)
		return cfgLoadedMsg{cfg: cfg, err: err, keys: keys, keyIssues: issues}
	}
}

func (m *Model) planCmd() tea.Cmd {
	m.planSeq++
	seq, svc, cfg := m.planSeq, m.sh.svc, m.sh.cfg
	return func() tea.Msg {
		p, err := svc.BuildPlan(cfg)
		return planBuiltMsg{seq: seq, plan: p, err: err}
	}
}

func (m *Model) resizeAll() {
	bw, bh := m.bodySize()
	for _, s := range m.stack {
		s.Resize(bw, bh)
	}
}

func (m *Model) setStatus(s statusMsg) tea.Cmd {
	m.status = s
	m.statSeq++
	seq := m.statSeq
	if s.level == levelErr {
		return nil // errors stay until replaced
	}
	ttl := s.ttl
	if ttl == 0 {
		ttl = statusExpiry
	}
	return tea.Tick(ttl, func(time.Time) tea.Msg { return statusExpireMsg{seq} })
}

// broadcast sends msg to every screen of the stack.
func (m *Model) broadcast(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for i, s := range m.stack {
		ns, c := s.Update(msg)
		m.stack[i] = ns
		cmds = append(cmds, c)
	}
	return tea.Batch(cmds...)
}

// Update routes messages: global concerns here, everything else to the top
// screen.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.update(msg)
	return m, tea.Batch(cmd, m.applyNav())
}

// applyNav applies the navigation queued by the screens during this Update.
func (m *Model) applyNav() tea.Cmd {
	ops := m.sh.nav
	m.sh.nav = nil
	var cmds []tea.Cmd
	for _, op := range ops {
		switch op.kind {
		case navPush:
			cmds = append(cmds, m.doPush(op.s))
		case navReplace:
			cmds = append(cmds, m.doReplace(op.s))
		case navPop:
			cmds = append(cmds, m.doPop())
		case navPopRoot:
			cmds = append(cmds, m.truncate(m.hubDepth()))
		}
	}
	return tea.Batch(cmds...)
}

// closeScreens calls Close on every screen of ss that has background work to
// stop. The commands the screens return are batched.
func closeScreens(ss ...Screen) tea.Cmd {
	var cmds []tea.Cmd
	for _, s := range ss {
		if c, ok := s.(closer); ok {
			if cmd := c.Close(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	return tea.Batch(cmds...)
}

// shutdown closes every screen of the stack (the program is ending): running
// scans and clones are cancelled.
func (m *Model) shutdown() { _ = closeScreens(m.stack...) }

// truncate drops the screens above the first n (closing them, top first).
func (m *Model) truncate(n int) tea.Cmd {
	if n < 1 || n >= len(m.stack) {
		return nil
	}
	gone := slices.Clone(m.stack[n:])
	slices.Reverse(gone)
	m.stack = m.stack[:n]
	return closeScreens(gone...)
}

func (m *Model) doPush(s Screen) tea.Cmd {
	bw, bh := m.bodySize()
	s.Resize(bw, bh)
	m.stack = append(m.stack, s)
	return s.Init()
}

func (m *Model) doReplace(s Screen) tea.Cmd {
	bw, bh := m.bodySize()
	s.Resize(bw, bh)
	old := m.stack[len(m.stack)-1]
	m.stack[len(m.stack)-1] = s
	return tea.Batch(closeScreens(old), s.Init())
}

func (m *Model) doPop() tea.Cmd { return m.truncate(len(m.stack) - 1) }

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.resizeAll()
		return m, nil

	case tea.KeyMsg:
		if key.Matches(msg, m.sh.km.ForceQuit) {
			return m, tea.Quit
		}
		// A Vietnamese input method sends composed or multi-rune input that
		// no shortcut matches: say so instead of doing nothing. Screens that
		// capture the keyboard decide for themselves (a text input takes the
		// text). The overlay has a text filter too: composed input goes into it.
		if imeLike(msg) && !m.showHelp && !isCapturing(m.top()) {
			return m, m.setStatus(imeStatus())
		}
		if m.showHelp {
			return m.overlayUpdate(msg)
		}
		if !isCapturing(m.top()) {
			switch {
			case key.Matches(msg, m.sh.km.Help), key.Matches(msg, m.sh.km.Palette):
				return m, m.openOverlay()
			case key.Matches(msg, m.sh.km.Back) && len(m.stack) > 1:
				// one step: a screen with an inner step (a confirmation) goes
				// back inside itself first
				if sb, ok := m.top().(stepBacker); ok && sb.StepBack() {
					return m, nil
				}
				return m, m.doPop()
			}
		}

	case pushMsg:
		return m, m.doPush(msg.s)
	case replaceMsg:
		return m, m.doReplace(msg.s)
	case popMsg:
		return m, m.doPop()
	case popRootMsg:
		return m, m.truncate(m.hubDepth())
	case updateMsg:
		m.sh.update, m.sh.hasUpdate = msg.notice, msg.ok
		return m, nil

	case statusMsg:
		return m, m.setStatus(msg)
	case statusExpireMsg:
		if msg.seq == m.statSeq {
			m.status = statusMsg{}
		}
		return m, nil

	case reloadMsg:
		m.reloading = true
		return m, m.loadCmd()
	case cfgLoadedMsg:
		var cmds []tea.Cmd
		reloading := m.reloading
		m.reloading = false
		// replaced in place: screens and forms hold the pointer
		*m.sh.km = *NewKeyMap(msg.keys)
		m.sh.keyIssues = msg.keyIssues
		keysWarn := len(msg.keyIssues) > 0 && !slices.Equal(msg.keyIssues, m.keyShown)
		switch {
		case msg.err != nil:
			// the rows already shown stay: they are the last good state
			cmds = append(cmds, m.setStatus(statusMsg{level: levelErr, text: errText(msg.err)}))
		default:
			m.sh.cfg, m.sh.loaded = msg.cfg, true
			cmds = append(cmds, m.planCmd())
			if reloading && !keysWarn {
				cmds = append(cmds, m.setStatus(statusMsg{level: levelOK, text: tr("tui.status.reloaded")}))
			}
			if keysWarn {
				m.keyShown = msg.keyIssues
				cmds = append(cmds, m.setStatus(statusMsg{level: levelWarn, text: tr("tui.status.keys_invalid", len(msg.keyIssues)), ttl: hintTTL}))
			}
		}
		if len(msg.keyIssues) == 0 {
			m.keyShown = nil
		}
		cmds = append(cmds, m.broadcast(msg))
		return m, tea.Batch(cmds...)
	case cfgChangedMsg:
		m.sh.cfg, m.sh.loaded = msg.cfg, true
		return m, tea.Batch(m.broadcast(msg), m.planCmd())
	case refreshPlanMsg:
		return m, m.planCmd()
	case planBuiltMsg:
		if msg.seq != m.planSeq {
			return m, nil
		}
		if msg.err == nil {
			m.sh.pending, m.sh.planned, m.sh.planErr = len(msg.plan.Changes), true, false
		} else {
			m.sh.planErr = true
		}
		return m, m.broadcast(msg)
	case focusMsg:
		return m, m.broadcast(msg)
	}

	// the cursor of the overlay's filter blinks through the root model
	if bm, ok := msg.(cursor.BlinkMsg); ok && m.showHelp {
		var c1 tea.Cmd
		m.ov.filter, c1 = m.ov.filter.Update(bm)
		ns, c2 := m.top().Update(bm)
		m.stack[len(m.stack)-1] = ns
		return m, tea.Batch(c1, c2)
	}

	// results of background work may belong to a screen below the top one (a
	// scan goes on behind a pushed check screen): every screen sees them
	if _, ok := msg.(bgMsg); ok {
		return m, m.broadcast(msg)
	}
	ns, cmd := m.top().Update(msg)
	m.stack[len(m.stack)-1] = ns
	return m, cmd
}

// View renders header, body, status bar and help bar in exactly w x h cells.
func (m *Model) View() string {
	if m.w == 0 || m.h == 0 {
		return ""
	}
	if m.w < minWidth || m.h < minHeight {
		msg := tr("tui.too_small", minWidth, minHeight)
		return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, wrap(msg, maxInt(m.w-2, 8)))
	}
	_, bh := m.bodySize()
	var body string
	if m.showHelp {
		body = m.helpView(m.w, bh)
	} else {
		body = m.top().View()
	}
	parts := []string{m.headerView(), fitHeight(body, bh), m.statusView()}
	parts = append(parts, renderHelpBar(m.sh.st, m.helpBarItems(), m.w, m.helpRowCount())...)
	out := strings.Join(parts, "\n")
	return lipgloss.NewStyle().MaxWidth(m.w).MaxHeight(m.h).Render(out)
}

func (m *Model) headerView() string {
	st := m.sh.st
	left := st.Brand.Render("gitid")
	// the start menu is not a crumb: the brand stands for it
	var crumbs []string
	for _, s := range m.stack {
		if _, home := s.(*homeScreen); !home {
			crumbs = append(crumbs, s.Title())
		}
	}
	sep := st.CrumbSep.Render(" › ")
	trail := ""
	for i, c := range crumbs {
		if i == len(crumbs)-1 {
			trail += st.Crumb.Render(c)
		} else {
			trail += st.Muted.Render(c) + sep
		}
	}
	if trail != "" {
		left += "  " + trail
	}
	// the folder gitid was started in; the version lives in the "?" overlay
	right := ""
	if m.sh.cwd != "" {
		right = st.Muted.Render(truncLeft(m.sh.svc.Env.Contract(m.sh.cwd), maxInt(m.w/3, 8)))
	}
	avail := m.w - lipgloss.Width(right) - 2
	left = trunc(left, maxInt(avail, 8))
	gap := m.w - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if gap < 1 {
		right = ""
		gap = maxInt(m.w-lipgloss.Width(left), 0)
	}
	return " " + left + spaces(gap) + right
}

// maxBadgeSlug is the longest company slug quoted in the status badge.
const maxBadgeSlug = 16

// syncBadge is the right part of the status bar. It states a fact (what is
// not applied, what is missing) and names no key: the help bar carries the
// next step. It shows the success mark only when there is nothing left to do
// (no pending change and no company without an account). It is shown on the
// screens that can act on the fact: the list, the detail, the repo browser and
// the apply screen and the start menu; a form or a dialog has its own
// question.
func (m *Model) syncBadge() string {
	sh, st := m.sh, m.sh.st
	switch m.top().(type) {
	case *homeScreen, *listScreen, *detailScreen, *reposScreen, *confirmScreen:
	default:
		return ""
	}
	switch {
	case !sh.loaded:
		return ""
	case sh.planErr:
		return st.Warn.Render("! " + tr("tui.status.plan_failed"))
	case !sh.planned:
		return ""
	case len(sh.cfg.Companies) == 0:
		return st.Text.Render("i " + tr("tui.status.no_company"))
	case sh.pending > 0:
		txt := trCount("tui.status.pending", sh.pending)
		if m.w < 70 {
			txt = tr("tui.status.pending_short", sh.pending)
		}
		return st.Badge.Render("● " + txt)
	}
	if empty := emptyCompanies(sh.cfg); len(empty) == 1 {
		slug := trunc(empty[0], maxBadgeSlug)
		return st.Warn.Render("! " + tr("tui.status.empty_company", slug))
	} else if len(empty) > 1 {
		return st.Warn.Render("! " + tr("tui.status.empty_companies", len(empty)))
	}
	if c, ok := m.top().(*confirmScreen); ok && c.state == confDone {
		return "" // the screen itself says "applied": not twice
	}
	return st.OK.Render("✓ " + tr("tui.status.synced"))
}

// statusView is the status line: the newest message on the left and the badge
// on the right. When both cannot fit, an error is never cut to keep the badge:
// the badge gives way (it comes back when the error is replaced). A message
// that expires also wins over the badge.
func (m *Model) statusView() string {
	st := m.sh.st
	left := ""
	if m.status.text != "" {
		first := strings.SplitN(m.status.text, "\n", 2)[0]
		left = st.levelStyle(m.status.level).Render(levelIcon(m.status.level) + " " + first)
	}
	right := m.syncBadge()
	if left != "" && right != "" && lipgloss.Width(left)+lipgloss.Width(right)+3 > m.w {
		right = ""
	}
	right = trunc(right, maxInt(m.w-2, 4))
	left = trunc(left, maxInt(m.w-lipgloss.Width(right)-3, 4))
	gap := m.w - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	return " " + left + spaces(gap) + right + " "
}

// globalHelp are the keys that work on every screen, shown at the end of the
// "?" overlay. Navigation keys only act where there is something to move.
func (m *Model) globalHelp() []key.Binding {
	km := m.sh.km
	var back []key.Binding
	if len(m.stack) == 0 || !m.onHome() {
		back = []key.Binding{withDesc(km.Back, "esc/"+strings.Join(km.keys.Keys(keymap.Back), "/"), tr("tui.key.back_global"))}
	}
	return append(back,
		withDesc(km.Up, "↑/↓", tr("tui.key.move")),
		// one row for both directions: the keys of PageUp and PageDown
		key.NewBinding(
			key.WithKeys(append(append([]string{}, km.PageUp.Keys()...), km.PageDown.Keys()...)...),
			key.WithHelp("pgup/pgdn", tr("tui.key.nav_page"))),
		key.NewBinding(key.WithKeys("home", "end"), key.WithHelp("home/end", tr("tui.key.nav_ends"))),
		km.Help,
		km.Palette,
		km.ForceQuit,
	)
}

// imeLike reports whether a key message looks like input from an input
// method: a non-ASCII rune, or several runes at once that are not a paste.
// No shortcut is bound to such input.
func imeLike(k tea.KeyMsg) bool {
	if k.Type != tea.KeyRunes || k.Paste || len(k.Runes) == 0 {
		return false
	}
	return len(k.Runes) > 1 || k.Runes[0] > 0x7F
}

// imeStatus is the warning shown for such input on a screen without a text
// input.
func imeStatus() statusMsg {
	return statusMsg{level: levelWarn, text: tr("tui.status.ime"), ttl: hintTTL}
}

// imeCmd is imeStatus as a command, for screens.
func imeCmd() tea.Cmd { return func() tea.Msg { return imeStatus() } }

// helpItems are the enabled entries of the screen's context help: the
// screen's own choice when it has one, otherwise its full list. helpBarItems
// splits them into the two rows of the help bar.
func (m *Model) helpItems() []helpItem {
	var items []helpItem
	if ch, ok := m.top().(contextHelper); ok {
		items = ch.ContextHelp()
	} else {
		for _, b := range m.top().Help() {
			items = append(items, helpItem{b: b})
		}
	}
	out := items[:0:0]
	for _, it := range items {
		if it.b.Enabled() {
			out = append(out, it)
		}
	}
	return out
}

// overlayKey is the key text of a binding in the "?" overlay: all the keys of
// a rebindable action joined with "/" (clone: [c, n] shows "c/n"), otherwise
// the binding's own label.
func (m *Model) overlayKey(b key.Binding) string {
	h := b.Help()
	for _, d := range keymap.Defs() {
		ks := m.sh.km.keys.Keys(d.Action)
		if len(ks) > 1 && h.Key == ks[0] && slices.Equal(b.Keys(), ks) {
			return strings.Join(ks, "/")
		}
	}
	return h.Key
}

// detectBackground asks the terminal for its background color (a no-op when
// the output is not a terminal). A terminal that does not answer within d is
// assumed to be dark.
func detectBackground(d time.Duration) {
	done := make(chan bool, 1)
	go func() { done <- lipgloss.HasDarkBackground() }()
	select {
	case <-done:
	case <-time.After(d):
		lipgloss.SetHasDarkBackground(true)
	}
}

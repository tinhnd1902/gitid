// Package tui is the interactive terminal interface of gitid, built with
// Bubble Tea. It talks to the rest of gitid only through internal/app.
package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tinhnd1902/gitid/internal/app"
)

// Options configure the TUI.
type Options struct {
	Cwd     string // directory shown as the repo hint and used by check/clone
	Version string
}

const (
	minWidth     = 40
	minHeight    = 10
	statusExpiry = 5 * time.Second
	chromeRows   = 3 // header + status bar + help bar
)

// Model is the root Bubble Tea model: a stack of screens plus the header,
// status bar and help bar that every screen shares.
type Model struct {
	sh       *shared
	stack    []Screen
	w, h     int
	status   statusMsg
	statSeq  int
	planSeq  int
	showHelp bool
	// reloading is set by the reload key: the next cfgLoadedMsg then says so.
	reloading bool
}

// New creates the root model. The configuration is loaded by Init.
func New(svc *app.Service, opts Options) *Model {
	sh := &shared{svc: svc, st: NewStyles(), km: NewKeyMap(), cwd: opts.Cwd, version: opts.Version}
	m := &Model{sh: sh}
	m.stack = []Screen{newListScreen(sh)}
	return m
}

// Run starts the TUI on the terminal.
func Run(svc *app.Service, opts Options) error {
	// Detect the terminal background once, before Bubble Tea owns the
	// terminal: the adaptive colors need it, and the query would otherwise be
	// answered into the input stream. A terminal that never answers must not
	// stall the start for long.
	detectBackground(1500 * time.Millisecond)
	p := tea.NewProgram(New(svc, opts), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m *Model) top() Screen { return m.stack[len(m.stack)-1] }

func (m *Model) bodySize() (int, int) { return m.w, maxInt(m.h-chromeRows, 1) }

// Init loads the configuration and initializes the home screen.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.loadCmd(), m.stack[0].Init())
}

func (m *Model) loadCmd() tea.Cmd {
	svc := m.sh.svc
	return func() tea.Msg {
		cfg, err := svc.Load()
		return cfgLoadedMsg{cfg: cfg, err: err}
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
			m.doPop()
		case navPopRoot:
			m.stack = m.stack[:1]
		}
	}
	return tea.Batch(cmds...)
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
	m.stack[len(m.stack)-1] = s
	return s.Init()
}

func (m *Model) doPop() {
	if len(m.stack) > 1 {
		m.stack = m.stack[:len(m.stack)-1]
	}
}

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
		// text).
		if imeLike(msg) && (m.showHelp || !isCapturing(m.top())) {
			return m, m.setStatus(imeStatus())
		}
		if m.showHelp {
			if msg.Type == tea.KeyEsc || key.Matches(msg, m.sh.km.Help) || key.Matches(msg, m.sh.km.Back) || key.Matches(msg, m.sh.km.Quit) {
				m.showHelp = false
			}
			return m, nil
		}
		if !isCapturing(m.top()) {
			switch {
			case key.Matches(msg, m.sh.km.Help):
				m.showHelp = true
				return m, nil
			case key.Matches(msg, m.sh.km.Back) && len(m.stack) > 1:
				// one step: a screen with an inner step (a confirmation) goes
				// back inside itself first
				if sb, ok := m.top().(stepBacker); ok && sb.StepBack() {
					return m, nil
				}
				m.stack = m.stack[:len(m.stack)-1]
				return m, nil
			}
		}

	case pushMsg:
		return m, m.doPush(msg.s)
	case replaceMsg:
		return m, m.doReplace(msg.s)
	case popMsg:
		m.doPop()
		return m, nil
	case popRootMsg:
		m.stack = m.stack[:1]
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
		if msg.err != nil {
			// the rows already shown stay: they are the last good state
			cmds = append(cmds, m.setStatus(statusMsg{level: levelErr, text: errText(msg.err)}))
		} else {
			m.sh.cfg, m.sh.loaded = msg.cfg, true
			cmds = append(cmds, m.planCmd())
			if reloading {
				cmds = append(cmds, m.setStatus(statusMsg{level: levelOK, text: tr("tui.status.reloaded")}))
			}
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
	out := strings.Join([]string{m.headerView(), fitHeight(body, bh), m.statusView(), m.helpBar()}, "\n")
	return lipgloss.NewStyle().MaxWidth(m.w).MaxHeight(m.h).Render(out)
}

func (m *Model) headerView() string {
	st := m.sh.st
	left := st.Brand.Render("gitid")
	var crumbs []string
	for _, s := range m.stack {
		crumbs = append(crumbs, s.Title())
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
	left += "  " + trail
	right := ""
	if m.sh.cwd != "" {
		right = st.Faint.Render(truncLeft(m.sh.svc.Env.Contract(m.sh.cwd), maxInt(m.w/3, 8)))
	}
	if m.sh.version != "" {
		right += st.Faint.Render("  " + m.sh.version)
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

// syncBadge is the right part of the status bar. It answers "what do I do
// next?" in a fixed priority order, and shows the success mark only when
// there is nothing left to do (no pending change and no company without an
// account).
func (m *Model) syncBadge() string {
	sh, st := m.sh, m.sh.st
	// Only the list and the detail screen handle the keys the badge names.
	var actionable bool
	switch m.top().(type) {
	case *listScreen, *detailScreen:
		actionable = true
	}
	t := func(k string, args ...any) string {
		if !actionable {
			k += "_plain"
		}
		return tr(k, args...)
	}
	switch {
	case !sh.loaded:
		return ""
	case sh.planErr:
		return st.Warn.Render("! " + t("tui.status.plan_failed"))
	case !sh.planned:
		return ""
	case len(sh.cfg.Companies) == 0:
		return st.Info.Render("i " + t("tui.status.no_company"))
	case sh.pending > 0:
		txt := t("tui.status.pending", sh.pending)
		if m.w < 70 {
			txt = t("tui.status.pending_short", sh.pending)
		}
		return st.Badge.Render("● " + txt)
	}
	if empty := emptyCompanies(sh.cfg); len(empty) == 1 {
		slug := trunc(empty[0], maxBadgeSlug)
		return st.Warn.Render("! " + t("tui.status.empty_company", slug))
	} else if len(empty) > 1 {
		return st.Warn.Render("! " + tr("tui.status.empty_companies", len(empty)))
	}
	return st.OK.Render("✓ " + tr("tui.status.synced"))
}

func (m *Model) statusView() string {
	st := m.sh.st
	left := ""
	if m.status.text != "" {
		first := strings.SplitN(m.status.text, "\n", 2)[0]
		left = st.levelStyle(m.status.level).Render(levelIcon(m.status.level) + " " + first)
	}
	right := m.syncBadge()
	// A message that expires (not an error) wins over the badge when both
	// cannot fit: the badge comes back when the message goes.
	if left != "" && right != "" && m.status.level != levelErr &&
		lipgloss.Width(left)+lipgloss.Width(right)+3 > m.w {
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
	return []key.Binding{
		withDesc(km.Back, "esc/q", tr("tui.key.back_global")),
		withDesc(km.Up, "↑/↓", tr("tui.key.move")),
		withDesc(km.PageUp, "pgup/dn", tr("tui.key.nav_page")),
		key.NewBinding(key.WithKeys("home", "end"), key.WithHelp("home/end", tr("tui.key.nav_ends"))),
		km.Help,
		km.ForceQuit,
	}
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

// helpItems are the entries of the short help bar: the screen's own choice
// when it has one, otherwise its full list. "? all keys" is always last.
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

// helpBar draws the short help bar. It never truncates with an ellipsis:
// when the bar is too wide, whole low-priority items are dropped from the
// end. The primary item and "? all keys" are never dropped.
func (m *Model) helpBar() string {
	st := m.sh.st
	km := m.sh.km
	more := helpItem{b: withDesc(km.Help, "?", tr("tui.key.all_keys"))}
	// A screen that captures the keyboard never opens the overlay (the "?"
	// would be typed), so the item is not offered there.
	showMore := !isCapturing(m.top())
	items := m.helpItems()
	render := func(it helpItem) string {
		h := it.b.Help()
		if it.primary {
			return st.HelpPrimary.Render("▸ " + h.Key + " " + h.Desc)
		}
		return st.Help.ShortKey.Render(h.Key) + " " + st.Help.ShortDesc.Render(h.Desc)
	}
	sep := "  "
	width := func(items []helpItem) int {
		w := 1
		if showMore {
			w += lipgloss.Width(render(more))
		}
		for _, it := range items {
			w += lipgloss.Width(render(it)) + len(sep)
		}
		return w
	}
	for width(items) > m.w {
		drop := -1
		for i := len(items) - 1; i >= 0; i-- {
			if !items[i].primary {
				drop = i
				break
			}
		}
		if drop < 0 {
			break
		}
		items = append(items[:drop:drop], items[drop+1:]...)
	}
	var b strings.Builder
	b.WriteString(" ")
	for _, it := range items {
		b.WriteString(render(it))
		b.WriteString(st.Help.ShortSeparator.Render(sep))
	}
	if !showMore {
		return strings.TrimRight(b.String(), " ")
	}
	b.WriteString(render(more))
	return b.String()
}

// helpView lists every key of the current screen, then the global keys. When
// the bindings do not fit the height they go in two or three columns, and as
// a last resort the blank separator lines go; the global section and the
// close hint are always visible.
func (m *Model) helpView(w, h int) string {
	st := m.sh.st
	enabled := func(bs []key.Binding) []key.Binding {
		var out []key.Binding
		for _, k := range bs {
			if k.Enabled() {
				out = append(out, k)
			}
		}
		return out
	}
	screen, global := enabled(m.top().Help()), enabled(m.globalHelp())
	inner := maxInt(w-4, 10)
	avail := maxInt(h-2, 1) // panel border

	perCol := func(n, cols int) int { return (n + cols - 1) / cols }
	// fixed lines around the bindings: title, screen name, global title, the
	// close hint, the rows of the global section, plus four blank separators
	fixed := func(compact bool, cols int) int {
		n := 4 + perCol(len(global), cols) + 1
		if !compact {
			n += 4
		}
		return n
	}
	// colWidths gives each of n columns the room its longest description
	// needs (cell: 2 indent + 9 key + the text); fits says whether all of
	// them are shown in full. When they are not, the widest columns shrink.
	colWidths := func(list []key.Binding, n int) (ws []int, fits bool) {
		rows := perCol(len(list), n)
		ws = make([]int, n)
		total := 0
		for c := range ws {
			ws[c] = 12
			for r := 0; r < rows; r++ {
				if idx := c*rows + r; idx < len(list) {
					ws[c] = maxInt(ws[c], 11+lipgloss.Width(list[idx].Help().Desc))
				}
			}
			if c < n-1 {
				ws[c] += 2 // gap before the next column
			}
			total += ws[c]
		}
		fits = total <= inner
		// shrink from the last column: the first ones keep their text
		for c := n - 1; c >= 0 && total > inner; c-- {
			cut := minInt(total-inner, ws[c]-12)
			if cut > 0 {
				ws[c] -= cut
				total -= cut
			}
		}
		return ws, fits
	}
	roomy := func(n int) bool {
		_, a := colWidths(screen, n)
		_, b := colWidths(global, n)
		return a && b
	}
	cols, compact := 1, false
	pick := func(needRoom bool) bool {
		for _, c := range []bool{false, true} {
			for n := 1; n <= 3; n++ {
				if needRoom && !roomy(n) {
					continue
				}
				if fixed(c, n)+perCol(len(screen), n) <= avail {
					cols, compact = n, c
					return true
				}
			}
		}
		return false
	}
	// prefer full descriptions; else the fewest columns that fit the height
	// (their text is then cut at a word, see cell)
	if !pick(true) && !pick(false) {
		cols, compact = 3, true
	}

	cell := func(k key.Binding, cw int) string {
		hp := k.Help()
		desc := hp.Desc
		if cw > 0 {
			desc = truncWords(desc, maxInt(cw-11, 1))
		}
		out := "  " + st.Accent.Render(pad(hp.Key, 9)) + st.Muted.Render(desc)
		if cw > 0 {
			out += spaces(cw - lipgloss.Width(out))
		}
		return out
	}
	blank := func(b *strings.Builder) {
		if !compact {
			b.WriteString("\n")
		}
	}
	// section writes a list of bindings in cols columns (one: a plain list)
	section := func(b *strings.Builder, list []key.Binding) {
		if cols == 1 {
			for _, k := range list {
				b.WriteString(cell(k, 0) + "\n")
			}
			return
		}
		cws, _ := colWidths(list, cols)
		rows := perCol(len(list), cols)
		for r := 0; r < rows; r++ {
			var line strings.Builder
			for c := 0; c < cols; c++ {
				if idx := c*rows + r; idx < len(list) {
					line.WriteString(cell(list[idx], cws[c]))
				}
			}
			b.WriteString(strings.TrimRight(line.String(), " ") + "\n")
		}
	}
	var b strings.Builder
	b.WriteString(st.PanelTitle.Render(tr("tui.help.title")) + "\n")
	blank(&b)
	b.WriteString(st.Bold.Render(m.top().Title()) + "\n")
	section(&b, screen)
	blank(&b)
	b.WriteString(st.Bold.Render(tr("tui.help.global")) + "\n")
	section(&b, global)
	blank(&b)
	b.WriteString(st.Faint.Render(tr("tui.help.close")))
	return m.sh.panel("", b.String(), w, h, true)
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

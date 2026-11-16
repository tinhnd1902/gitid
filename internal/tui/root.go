// Package tui is the interactive terminal interface of gitid, built with
// Bubble Tea. It talks to the rest of gitid only through internal/app.
package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
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
	help     help.Model
	showHelp bool
}

// New creates the root model. The configuration is loaded by Init.
func New(svc *app.Service, opts Options) *Model {
	sh := &shared{svc: svc, st: NewStyles(), km: NewKeyMap(), cwd: opts.Cwd, version: opts.Version}
	hm := help.New()
	hm.Styles = sh.st.Help
	hm.ShortSeparator = "  "
	m := &Model{sh: sh, help: hm}
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
	return tea.Tick(statusExpiry, func(time.Time) tea.Msg { return statusExpireMsg{seq} })
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
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.help.Width = msg.Width - 2
		m.resizeAll()
		return m, nil

	case tea.KeyMsg:
		if key.Matches(msg, m.sh.km.ForceQuit) {
			return m, tea.Quit
		}
		if m.showHelp {
			if msg.Type == tea.KeyEsc || key.Matches(msg, m.sh.km.Help) || key.Matches(msg, m.sh.km.Quit) {
				m.showHelp = false
			}
			return m, nil
		}
		if !isCapturing(m.top()) {
			switch {
			case key.Matches(msg, m.sh.km.Help):
				m.showHelp = true
				return m, nil
			case msg.Type == tea.KeyEsc && len(m.stack) > 1:
				m.stack = m.stack[:len(m.stack)-1]
				return m, nil
			}
		}

	case pushMsg:
		bw, bh := m.bodySize()
		msg.s.Resize(bw, bh)
		m.stack = append(m.stack, msg.s)
		return m, msg.s.Init()
	case replaceMsg:
		bw, bh := m.bodySize()
		msg.s.Resize(bw, bh)
		m.stack[len(m.stack)-1] = msg.s
		return m, msg.s.Init()
	case popMsg:
		if len(m.stack) > 1 {
			m.stack = m.stack[:len(m.stack)-1]
		}
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

	case cfgLoadedMsg:
		var cmds []tea.Cmd
		if msg.err != nil {
			cmds = append(cmds, m.setStatus(statusMsg{levelErr, errText(msg.err)}))
		} else {
			m.sh.cfg, m.sh.loaded = msg.cfg, true
			cmds = append(cmds, m.planCmd())
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
			m.sh.pending, m.sh.planned = len(msg.plan.Changes), true
		}
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

func (m *Model) pendingBadge() string {
	st := m.sh.st
	switch {
	case !m.sh.planned:
		return ""
	case m.sh.pending > 0:
		txt := tr("tui.status.pending", m.sh.pending)
		if m.w < 70 {
			txt = tr("tui.status.pending_short", m.sh.pending)
		}
		return st.Warn.Render("● " + txt)
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
	right := m.pendingBadge()
	avail := m.w - lipgloss.Width(right) - 3
	left = trunc(left, maxInt(avail, 4))
	gap := m.w - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	return " " + left + spaces(gap) + right + " "
}

func (m *Model) globalHelp() []key.Binding {
	km := m.sh.km
	return []key.Binding{km.Help, km.ForceQuit}
}

func (m *Model) helpBar() string {
	bs := append(append([]key.Binding(nil), m.top().Help()...), m.globalHelp()...)
	return " " + m.help.ShortHelpView(bs)
}

func (m *Model) helpView(w, h int) string {
	st := m.sh.st
	var b strings.Builder
	b.WriteString(st.PanelTitle.Render(tr("tui.help.title")) + "\n\n")
	row := func(bs []key.Binding) {
		for _, k := range bs {
			if !k.Enabled() {
				continue
			}
			h := k.Help()
			b.WriteString("  " + st.Accent.Render(pad(h.Key, 8)) + st.Muted.Render(h.Desc) + "\n")
		}
	}
	b.WriteString(st.Bold.Render(m.top().Title()) + "\n")
	row(m.top().Help())
	b.WriteString("\n" + st.Bold.Render(tr("tui.help.global")) + "\n")
	row(m.globalHelp())
	b.WriteString("\n" + st.Faint.Render(tr("tui.help.close")))
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

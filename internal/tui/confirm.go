package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tinhnd1902/gitid/internal/plan"
)

type confirmState int

const (
	confLoading confirmState = iota
	confEmpty
	confReady
	confApplying
	confDone
	confFailed
)

// confirmScreen shows the pending changes as a colored diff, asks for
// confirmation, applies them in the background and reports the backup.
type confirmScreen struct {
	sh    *shared
	title string
	id    int
	state confirmState
	plan  plan.Plan
	res   plan.Result
	err   error
	vp    viewport.Model
	spin  spinner.Model
	w, h  int
}

func newConfirmScreen(sh *shared, title string) *confirmScreen {
	return &confirmScreen{sh: sh, title: title, id: sh.id(), spin: sh.st.Spinner, vp: viewport.New(10, 5)}
}

func (c *confirmScreen) Title() string { return c.title }

func (c *confirmScreen) Init() tea.Cmd { return tea.Batch(c.spin.Tick, c.load()) }

func (c *confirmScreen) load() tea.Cmd {
	c.state = confLoading
	svc, cfg, id := c.sh.svc, c.sh.cfg, c.id
	return func() tea.Msg {
		p, err := svc.BuildPlan(cfg)
		return confirmPlanMsg{id: id, plan: p, err: err}
	}
}

// Capturing keeps esc away from the root while the apply is running.
func (c *confirmScreen) Capturing() bool { return c.state == confApplying }

func (c *confirmScreen) Help() []key.Binding {
	km := c.sh.km
	switch c.state {
	case confReady:
		return []key.Binding{withDesc(km.Yes, "y", tr("tui.confirm.apply")), withDesc(km.No, "n/esc", tr("tui.key.cancel")), withDesc(km.Up, "↑/↓", tr("tui.key.scroll"))}
	case confDone, confEmpty:
		return []key.Binding{km.Enter, km.Back}
	case confFailed:
		return []key.Binding{km.Retry, km.Back}
	}
	return nil
}

func (c *confirmScreen) Resize(w, h int) {
	c.w, c.h = w, h
	c.layout()
}

// headerLines is the file list above the diff.
func (c *confirmScreen) headerLines() []string {
	st := c.sh.st
	lines := []string{st.Bold.Render(tr("tui.confirm.files", len(c.plan.Changes)))}
	maxFiles := clamp(c.h/4, 2, 8)
	for i, ch := range c.plan.Changes {
		if i == maxFiles {
			lines = append(lines, st.Faint.Render(fmt.Sprintf("  … +%d", len(c.plan.Changes)-maxFiles)))
			break
		}
		lines = append(lines, "  "+opBadge(st, string(ch.Op))+" "+st.Text.Render(ch.Display))
	}
	for _, w := range c.plan.Warnings {
		lines = append(lines, st.Warn.Render("  ! "+tr(w)))
	}
	return lines
}

func opBadge(st *Styles, op string) string {
	switch op {
	case "create":
		return st.OK.Render("+ " + pad(tr("tui.op.create"), 6))
	case "delete":
		return st.Err.Render("- " + pad(tr("tui.op.delete"), 6))
	}
	return st.Info.Render("~ " + pad(tr("tui.op.update"), 6))
}

func (c *confirmScreen) layout() {
	inner := maxInt(c.w-4, 10)
	head := 0
	if c.state == confReady {
		head = len(c.headerLines()) + 1
	}
	c.vp.Width = inner
	c.vp.Height = maxInt(c.h-2-1-1-head, 1) // borders, title, footer line
}

func (c *confirmScreen) colorDiff() string {
	st := c.sh.st
	var b strings.Builder
	for _, l := range splitLines(c.plan.Diff()) {
		switch {
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
			b.WriteString(st.DiffMeta.Render(l))
		case strings.HasPrefix(l, "@@"):
			b.WriteString(st.DiffHunk.Render(l))
		case strings.HasPrefix(l, "+"):
			b.WriteString(st.DiffAdd.Render(l))
		case strings.HasPrefix(l, "-"):
			b.WriteString(st.DiffDel.Render(l))
		default:
			b.WriteString(st.Muted.Render(l))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (c *confirmScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	km := c.sh.km
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if c.state == confLoading || c.state == confApplying {
			var cmd tea.Cmd
			c.spin, cmd = c.spin.Update(msg)
			return c, cmd
		}
		return c, nil
	case confirmPlanMsg:
		if msg.id != c.id {
			return c, nil
		}
		c.plan, c.err = msg.plan, msg.err
		switch {
		case msg.err != nil:
			c.state = confFailed
		case msg.plan.Empty():
			c.state = confEmpty
		default:
			c.state = confReady
			c.layout()
			c.vp.SetContent(c.colorDiff())
			c.vp.GotoTop()
		}
		return c, nil
	case applyDoneMsg:
		if msg.id != c.id {
			return c, nil
		}
		c.res, c.err = msg.res, msg.err
		if msg.err != nil {
			c.state = confFailed
			return c, say(levelErr, errText(msg.err))
		}
		c.state = confDone
		return c, tea.Batch(refreshPlan(), say(levelOK, tr("tui.status.applied")))
	case tea.KeyMsg:
		switch c.state {
		case confReady:
			switch {
			case key.Matches(msg, km.Yes):
				return c, c.apply()
			case key.Matches(msg, km.No):
				return c, pop()
			}
			var cmd tea.Cmd
			c.vp, cmd = c.vp.Update(msg)
			return c, cmd
		case confDone, confEmpty:
			if key.Matches(msg, km.Enter) {
				return c, popRoot()
			}
		case confFailed:
			if key.Matches(msg, km.Retry) {
				c.err = nil
				return c, tea.Batch(c.spin.Tick, c.load())
			}
		}
	}
	return c, nil
}

func (c *confirmScreen) apply() tea.Cmd {
	c.state = confApplying
	svc, p, id := c.sh.svc, c.plan, c.id
	return tea.Batch(c.spin.Tick, func() tea.Msg {
		ctx, cancel := ctxTimeout(applyTimeout)
		defer cancel()
		res, err := svc.Apply(ctx, p)
		return applyDoneMsg{id: id, res: res, err: err}
	})
}

func (c *confirmScreen) View() string {
	st := c.sh.st
	var body string
	switch c.state {
	case confLoading:
		body = c.spin.View() + " " + st.Muted.Render(tr("tui.confirm.building"))
	case confEmpty:
		body = st.OK.Render("✓ "+tr("tui.confirm.empty")) + "\n\n" + st.Faint.Render(tr("tui.confirm.back_hint"))
	case confReady:
		body = strings.Join(c.headerLines(), "\n") + "\n" +
			st.Faint.Render(strings.Repeat("─", maxInt(c.w-4, 4))) + "\n" + c.vp.View() + "\n" +
			st.Faint.Render(fmt.Sprintf("%3.0f%%", c.vp.ScrollPercent()*100)+"  "+tr("tui.confirm.question"))
	case confApplying:
		body = c.spin.View() + " " + st.Muted.Render(tr("tui.confirm.applying"))
	case confDone:
		body = c.doneView()
	case confFailed:
		body = st.Err.Render("✗ "+wrap(errText(c.err), maxInt(c.w-6, 10))) + "\n\n" + st.Faint.Render(tr("tui.confirm.retry_hint"))
	}
	return c.sh.panel(c.title, body, c.w, c.h, true)
}

func (c *confirmScreen) doneView() string {
	st := c.sh.st
	var b strings.Builder
	b.WriteString(st.OK.Bold(true).Render("✓ "+tr("tui.confirm.done")) + "\n\n")
	for _, p := range c.res.Written {
		b.WriteString("  " + st.OK.Render("+") + " " + st.Text.Render(c.sh.svc.Env.Contract(p)) + "\n")
	}
	for _, p := range c.res.Deleted {
		b.WriteString("  " + st.Err.Render("-") + " " + st.Text.Render(c.sh.svc.Env.Contract(p)) + "\n")
	}
	if c.res.BackupDir != "" {
		b.WriteString("\n" + st.Label.Render(tr("tui.confirm.backup")) + " " + st.Text.Render(c.sh.svc.Env.Contract(c.res.BackupDir)) + "\n")
	}
	for _, w := range c.res.Warnings {
		b.WriteString(st.Warn.Render("! "+w) + "\n")
	}
	b.WriteString("\n" + st.Faint.Render(tr("tui.confirm.back_hint")))
	return b.String()
}

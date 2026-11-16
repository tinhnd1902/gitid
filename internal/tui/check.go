package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tinhnd1902/gitid/internal/app"
)

type checkPhase int

const (
	chLoading checkPhase = iota
	chResult
	chError
	chFixPlanning
	chFixConfirm
	chFixing
)

// checkScreen inspects the repository in the current directory: which account
// its remote maps to, which email git really uses and why. When the remote
// does not use the matching account it offers fix-remote with an
// old -> new preview.
type checkScreen struct {
	sh     *shared
	id     int
	phase  checkPhase
	res    app.CheckResult
	err    error
	fix    app.FixResult
	fixErr error
	pick   choice
	note   string
	spin   spinner.Model
	w, h   int
}

func newCheckScreen(sh *shared) *checkScreen {
	return &checkScreen{
		sh: sh, id: sh.id(), spin: sh.st.Spinner,
		pick: choice{labels: [2]string{tr("tui.fix.no"), tr("tui.fix.yes")}},
	}
}

func (c *checkScreen) Title() string   { return tr("tui.check.title") }
func (c *checkScreen) Resize(w, h int) { c.w, c.h = w, h }

func (c *checkScreen) Init() tea.Cmd { return tea.Batch(c.spin.Tick, c.run()) }

func (c *checkScreen) Capturing() bool { return c.phase == chFixing }

func (c *checkScreen) run() tea.Cmd {
	c.phase = chLoading
	svc, cfg, cwd, id := c.sh.svc, c.sh.cfg, c.sh.cwd, c.id
	return func() tea.Msg {
		ctx, cancel := ctxTimeout(shortTimeout)
		defer cancel()
		res, err := svc.Check(ctx, cfg, cwd)
		return checkDoneMsg{id: id, res: res, err: err}
	}
}

func (c *checkScreen) canFix() bool { return c.res.Account != "" && c.res.Remote != "" }

func (c *checkScreen) Help() []key.Binding {
	km := c.sh.km
	switch c.phase {
	case chResult:
		out := []key.Binding{}
		if c.canFix() {
			out = append(out, km.Fix)
		}
		return append(out, km.Retry, km.Back)
	case chError:
		return []key.Binding{km.Retry, km.Back}
	case chFixConfirm:
		return []key.Binding{km.Yes, km.No, km.Toggle, km.Enter, km.Back}
	}
	return nil
}

func (c *checkScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	km := c.sh.km
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if c.phase == chLoading || c.phase == chFixPlanning || c.phase == chFixing {
			var cmd tea.Cmd
			c.spin, cmd = c.spin.Update(msg)
			return c, cmd
		}
		return c, nil
	case checkDoneMsg:
		if msg.id != c.id {
			return c, nil
		}
		c.res, c.err = msg.res, msg.err
		if msg.err != nil {
			c.phase = chError
		} else {
			c.phase = chResult
		}
		return c, nil
	case fixPlanMsg:
		if msg.id != c.id {
			return c, nil
		}
		c.fix, c.fixErr = msg.res, msg.err
		c.pick.idx = 0
		switch {
		case msg.err != nil:
			c.phase, c.note = chResult, errText(msg.err)
		case !msg.res.Changed:
			c.phase, c.note = chResult, tr("tui.fix.nothing")
		default:
			c.phase, c.note = chFixConfirm, ""
		}
		return c, nil
	case fixDoneMsg:
		if msg.id != c.id {
			return c, nil
		}
		if msg.err != nil {
			c.phase, c.note = chResult, errText(msg.err)
			return c, nil
		}
		c.note = tr("tui.fix.done")
		return c, tea.Batch(c.spin.Tick, c.run(), say(levelOK, tr("tui.status.remote_fixed")))
	case tea.KeyMsg:
		switch c.phase {
		case chResult:
			switch {
			case key.Matches(msg, km.Fix) && c.canFix():
				return c, c.planFix()
			case key.Matches(msg, km.Retry):
				c.note = ""
				return c, tea.Batch(c.spin.Tick, c.run())
			}
		case chError:
			if key.Matches(msg, km.Retry) {
				return c, tea.Batch(c.spin.Tick, c.run())
			}
		case chFixConfirm:
			switch {
			case key.Matches(msg, km.Toggle):
				c.pick.toggle()
			case key.Matches(msg, km.Yes):
				return c, c.doFix()
			case key.Matches(msg, km.No), msg.Type == tea.KeyEsc:
				c.phase = chResult
			case key.Matches(msg, km.Enter):
				if c.pick.idx == 1 {
					return c, c.doFix()
				}
				c.phase = chResult
			}
			return c, nil
		}
	}
	return c, nil
}

func (c *checkScreen) req() app.FixRequest {
	return app.FixRequest{Dir: c.sh.cwd, Account: c.res.Account}
}

func (c *checkScreen) planFix() tea.Cmd {
	c.phase, c.note = chFixPlanning, ""
	svc, cfg, req, id := c.sh.svc, c.sh.cfg, c.req(), c.id
	return tea.Batch(c.spin.Tick, func() tea.Msg {
		ctx, cancel := ctxTimeout(shortTimeout)
		defer cancel()
		res, _, _, err := svc.PlanFix(ctx, cfg, req)
		return fixPlanMsg{id: id, res: res, err: err}
	})
}

func (c *checkScreen) doFix() tea.Cmd {
	c.phase = chFixing
	svc, cfg, req, id := c.sh.svc, c.sh.cfg, c.req(), c.id
	return tea.Batch(c.spin.Tick, func() tea.Msg {
		ctx, cancel := ctxTimeout(shortTimeout)
		defer cancel()
		res, err := svc.FixRemote(ctx, cfg, req)
		return fixDoneMsg{id: id, res: res, err: err}
	})
}

func (c *checkScreen) View() string {
	st := c.sh.st
	inner := maxInt(c.w-6, 10)
	var b strings.Builder
	switch c.phase {
	case chLoading:
		b.WriteString(c.spin.View() + " " + st.Muted.Render(tr("tui.check.running")))
	case chFixPlanning:
		b.WriteString(c.spin.View() + " " + st.Muted.Render(tr("tui.fix.planning")))
	case chFixing:
		b.WriteString(c.spin.View() + " " + st.Muted.Render(tr("tui.fix.working")))
	case chError:
		b.WriteString(st.Err.Render("✗ "+wrap(errText(c.err), inner)) + "\n\n")
		b.WriteString(st.Faint.Render(tr("tui.check.retry_hint")))
	case chFixConfirm:
		b.WriteString(st.Bold.Render(tr("tui.fix.title", c.fix.Remote, c.fix.Account)) + "\n\n")
		b.WriteString(st.Err.Render("- "+trunc(c.fix.Old, inner-2)) + "\n")
		b.WriteString(st.OK.Render("+ "+trunc(c.fix.New, inner-2)) + "\n\n")
		b.WriteString(c.pick.view(st))
	case chResult:
		b.WriteString(c.resultView(inner))
	}
	return c.sh.panel(tr("tui.check.title"), b.String(), c.w, c.h, true)
}

func (c *checkScreen) resultView(w int) string {
	st := c.sh.st
	var b strings.Builder
	row := func(label, value string) {
		if value == "" {
			return
		}
		b.WriteString(st.Label.Render(pad(label, 12)) + st.Text.Render(trunc(value, maxInt(w-12, 4))) + "\n")
	}
	r := c.res
	row(tr("tui.f.repo"), c.sh.svc.Env.Contract(r.Repo))
	row(tr("tui.f.remote"), r.Remote)
	acc := r.Account
	if acc != "" && r.Reason != "" {
		acc += "  (" + tr("cli.reason."+r.Reason) + ")"
	}
	row(tr("tui.f.account"), acc)
	row(tr("tui.f.expected"), r.ExpectedEmail)
	eff := r.EffectiveEmail
	if eff != "" && r.EmailOrigin != "" {
		eff += "  (" + c.origin(r.EmailOrigin) + ")"
	}
	row(tr("tui.f.effective"), eff)
	b.WriteString("\n")
	if r.OK() {
		b.WriteString(st.OK.Render("✓ " + tr("cli.check.ok")))
	}
	for _, wk := range r.Warnings {
		b.WriteString(st.Warn.Render("! "+wrap(tr(wk), w-2)) + "\n")
	}
	if c.canFix() && !r.OK() {
		b.WriteString("\n" + st.Faint.Render(tr("tui.check.fix_hint")))
	}
	if c.note != "" {
		b.WriteString("\n\n" + st.Info.Render(wrap(c.note, w)))
	}
	return strings.TrimRight(b.String(), "\n")
}

// origin shortens the "file:/abs/path" origin printed by git.
func (c *checkScreen) origin(o string) string {
	if p, ok := strings.CutPrefix(o, "file:"); ok {
		return "file:" + c.sh.svc.Env.Contract(p)
	}
	return o
}

package tui

import (
	"errors"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/gitops"
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

// checkScreen inspects a repository (the current directory unless it was
// opened for another one): which account its remote maps to, which email git
// really uses and why. When the remote does not use the matching account it
// offers fix-remote with an old -> new preview.
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

	dir      string // the folder checked
	fromRepo bool   // opened by the repo browser for a repository it listed
	autoFix  bool   // go straight to the fix confirmation when there is a fix
	fixed    bool   // a fix was applied: the browser must read the repository again
}

// newCheckScreen checks the folder gitid was started in.
func newCheckScreen(sh *shared) *checkScreen { return newCheckScreenAt(sh, sh.cwd, false) }

// newCheckScreenAt checks the repository at dir. With autoFix the screen goes
// to the fix confirmation as soon as the check shows a fix is possible.
func newCheckScreenAt(sh *shared, dir string, autoFix bool) *checkScreen {
	return &checkScreen{
		sh: sh, id: sh.id(), spin: sh.st.Spinner, dir: dir, autoFix: autoFix,
		pick: choice{labels: [2]string{tr("tui.fix.no"), tr("tui.fix.yes")}},
	}
}

// Close tells the repo browser to read the repository again when a fix was
// applied here.
func (c *checkScreen) Close() tea.Cmd {
	if !c.fixed {
		return nil
	}
	dir := c.dir
	return func() tea.Msg { return repoTouchedMsg{dir: dir} }
}

func (c *checkScreen) Title() string   { return tr("tui.check.title") }
func (c *checkScreen) Resize(w, h int) { c.w, c.h = w, h }

func (c *checkScreen) Init() tea.Cmd { return tea.Batch(c.spin.Tick, c.run()) }

func (c *checkScreen) Capturing() bool { return c.phase == chFixing }

// StepBack: esc/q in the fix confirmation returns to the check result.
func (c *checkScreen) StepBack() bool {
	if c.phase == chFixConfirm {
		c.phase = chResult
		return true
	}
	return false
}

func (c *checkScreen) run() tea.Cmd {
	c.phase = chLoading
	svc, cfg, dir, id := c.sh.svc, c.sh.cfg, c.dir, c.id
	return func() tea.Msg {
		ctx, cancel := ctxTimeout(shortTimeout)
		defer cancel()
		res, err := svc.Check(ctx, cfg, dir)
		return checkDoneMsg{id: id, res: res, err: err}
	}
}

// notRepo reports whether the check failed because the folder gitid was
// started in is not a git repository (retrying cannot help there). A
// repository the browser listed is not offered a clone: it only says the
// check failed.
func (c *checkScreen) notRepo() bool {
	return c.phase == chError && errors.Is(c.err, gitops.ErrNotRepo) && !c.fromRepo
}

func (c *checkScreen) canFix() bool { return c.res.Account != "" && c.res.Remote != "" }

// needsFix says the check found a problem that fixing the remote can clear
// ("not applied yet" is a note about the configuration, not about the repo).
func (c *checkScreen) needsFix() bool {
	if c.phase != chResult || !c.canFix() {
		return false
	}
	for _, wk := range c.res.Warnings {
		if wk != app.WarnPendingApply {
			return true
		}
	}
	return false
}

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
		if c.notRepo() {
			return []key.Binding{km.Clone, km.Retry, km.Back}
		}
		return []key.Binding{km.Retry, km.Back}
	case chFixConfirm:
		return []key.Binding{km.Yes, km.No, km.Toggle, km.Enter, km.Back}
	}
	return nil
}

// ContextHelp makes clone the next step when the folder is not a repository,
// and the fix of the remote when the check found a problem it can clear.
func (c *checkScreen) ContextHelp() []helpItem {
	var out []helpItem
	for i, b := range c.Help() {
		out = append(out, helpItem{b: b, primary: i == 0 && (c.notRepo() || c.needsFix())})
	}
	return out
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
			return c, nil
		}
		c.phase = chResult
		if c.autoFix {
			c.autoFix = false
			if c.canFix() {
				return c, c.planFix()
			}
			c.note = tr("tui.fix.nothing")
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
		c.note, c.fixed = tr("tui.fix.done"), true
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
			switch {
			case key.Matches(msg, km.Retry):
				return c, tea.Batch(c.spin.Tick, c.run())
			case key.Matches(msg, km.Clone) && c.notRepo():
				return c, c.sh.replaceNow(newReposScreen(c.sh, nil))
			}
		case chFixConfirm:
			switch {
			case key.Matches(msg, km.Toggle):
				c.pick.toggle()
			case key.Matches(msg, km.Yes):
				return c, c.doFix()
			case key.Matches(msg, km.No):
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
	return app.FixRequest{Dir: c.dir, Account: c.res.Account}
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
		if c.notRepo() {
			cwd := "."
			if c.dir != "" {
				cwd = c.sh.svc.Env.Contract(c.dir)
			}
			b.WriteString(st.Muted.Render(wrap(tr("tui.check.where", cwd), inner)) + "\n")
			b.WriteString(st.Text.Render(wrap(c.notRepoNext(), inner)))
			break
		}
		b.WriteString(st.Text.Render(c.sh.km.T("tui.check.retry_hint")))
	case chFixConfirm:
		b.WriteString(st.Bold.Render(tr("tui.fix.title", c.fix.Remote, c.fix.Account)) + "\n\n")
		b.WriteString(st.Err.Render("- "+trunc(c.fix.Old, inner-2)) + "\n")
		b.WriteString(st.OK.Render("+ "+trunc(c.fix.New, inner-2)) + "\n\n")
		b.WriteString(c.pick.view(st))
	case chResult:
		b.WriteString(c.resultView(inner))
	}
	// the breadcrumb already says "Kiểm tra repo": no title row
	return c.sh.panel("", b.String(), c.w, c.h, true)
}

// notRepoNext says what to do instead: open a terminal in a repo (named after
// the first company folder when there is one) or clone one.
func (c *checkScreen) notRepoNext() string {
	if len(c.sh.cfg.Companies) > 0 {
		return c.sh.km.T("tui.check.not_repo_next", c.sh.cfg.Companies[0].BaseDir)
	}
	return c.sh.km.T("tui.check.not_repo_next_none")
}

func (c *checkScreen) resultView(w int) string {
	st := c.sh.st
	r := c.res
	var b strings.Builder
	// the account is named the way gitid names an identity everywhere
	acc := st.Text.Render(r.Account)
	if ref, err := c.sh.cfg.ParseRef(r.Account); err == nil && r.Account != "" {
		acc = st.identityStamp(ref)
	}
	eff := r.EffectiveEmail
	if eff != "" && r.EmailOrigin != "" {
		eff += " · " + c.origin(r.EmailOrigin)
	}
	reason := ""
	if r.Account != "" && r.Reason != "" {
		reason = tr("cli.reason." + r.Reason)
	}
	col := kvCol(w, tr("tui.f.repo"), tr("tui.f.remote"), tr("tui.f.account"), tr("tui.f.reason"), tr("tui.f.expected"), tr("tui.f.effective"))
	row := func(label, value string) {
		if value != "" {
			b.WriteString(kvRow(st, col, w, label, value) + "\n")
		}
	}
	row(tr("tui.f.repo"), c.sh.svc.Env.Contract(r.Repo))
	row(tr("tui.f.remote"), r.Remote)
	if r.Account != "" {
		b.WriteString(st.Label.Render(pad(tr("tui.f.account"), col)) + trunc(acc, maxInt(w-col, 4)) + "\n")
	}
	row(tr("tui.f.reason"), reason)
	row(tr("tui.f.expected"), r.ExpectedEmail)
	row(tr("tui.f.effective"), eff)
	b.WriteString("\n")
	// "not applied yet" is a note about the configuration, not a failure of
	// the repo: the verdict looks at the other warnings
	var real, pending []string
	for _, wk := range r.Warnings {
		if wk == app.WarnPendingApply {
			pending = append(pending, wk)
		} else {
			real = append(real, wk)
		}
	}
	if len(real) == 0 {
		b.WriteString(st.OK.Render("✓ "+tr("cli.check.ok")) + "\n")
	}
	for _, wk := range append(real, pending...) {
		b.WriteString(st.Warn.Render("! "+wrap(trTUI(wk), proseW(w)-2)) + "\n")
	}
	if c.note != "" {
		b.WriteString("\n\n" + st.Text.Render("i "+wrap(c.note, proseW(w))))
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

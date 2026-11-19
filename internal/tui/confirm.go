package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tinhnd1902/gitid/internal/config"
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
		return []key.Binding{relabel(km.Yes, tr("tui.confirm.apply")), withDesc(km.No, km.No.Help().Key+"/esc", tr("tui.key.cancel")), withDesc(km.Up, "↑/↓", tr("tui.key.scroll"))}
	case confDone:
		return []key.Binding{km.Clone, km.Check, withDesc(km.Enter, "enter", tr("tui.key.to_accounts")), km.Back}
	case confEmpty:
		return []key.Binding{withDesc(km.Enter, "enter", tr("tui.key.to_accounts")), km.Back}
	case confFailed:
		return []key.Binding{km.Retry, km.Back}
	}
	return nil
}

// ContextHelp makes the next step primary: apply when the plan is ready,
// check the repo once it was applied.
func (c *confirmScreen) ContextHelp() []helpItem {
	var out []helpItem
	for i, b := range c.Help() {
		out = append(out, helpItem{b: b, primary: i == 0 && (c.state == confReady || c.state == confDone)})
	}
	return out
}

func (c *confirmScreen) Resize(w, h int) {
	c.w, c.h = w, h
	c.layout()
}

// summaryLines is the plain-language part above the file list: what each
// kind of file is for and where the backups go. At small heights it shrinks
// to the backup line alone.
func (c *confirmScreen) summaryLines() []string {
	st := c.sh.st
	var gitconfig, sshconfig, sshconf, genDir string
	gen, del := 0, 0
	for _, ch := range c.plan.Changes {
		if string(ch.Op) == "delete" {
			del++
			continue
		}
		switch ch.Kind {
		case plan.KindGitConfig:
			gitconfig = ch.Display
		case plan.KindSSHConfig:
			sshconfig = ch.Display
		case plan.KindSSHConf:
			sshconf = ch.Display
		case plan.KindGenerated:
			gen++
			if genDir == "" {
				genDir = filepath.Dir(ch.Display)
			}
		}
	}
	var what []string
	if gitconfig != "" {
		what = append(what, tr("tui.confirm.sum_gitconfig", gitconfig))
	}
	if gen > 0 {
		what = append(what, tr("tui.confirm.sum_generated", gen, genDir))
	}
	if sshconfig != "" {
		what = append(what, tr("tui.confirm.sum_sshconfig", sshconfig))
	}
	if sshconf != "" {
		what = append(what, tr("tui.confirm.sum_sshconf", sshconf))
	}
	if del > 0 {
		what = append(what, tr("tui.confirm.sum_delete", del))
	}
	env := c.sh.svc.Env
	backup := tr("tui.confirm.sum_backup", env.Contract(env.Paths().BackupsDir))
	switch {
	case c.h >= 26: // everything
	case c.h >= 20:
		if len(what) > 3 {
			what = what[:3]
		}
	default:
		return []string{st.Muted.Render("  " + backup)}
	}
	lines := []string{st.Bold.Render(tr("tui.confirm.summary"))}
	for _, w := range what {
		lines = append(lines, st.Muted.Render("  "+w))
	}
	return append(lines, st.Muted.Render("  "+backup))
}

// headerLines is the summary and the file list above the diff.
func (c *confirmScreen) headerLines() []string {
	st := c.sh.st
	lines := c.summaryLines()
	lines = append(lines, st.Bold.Render(trCount("tui.confirm.files", len(c.plan.Changes))))
	maxFiles := clamp((c.h-len(lines))/4, 2, 8)
	for i, ch := range c.plan.Changes {
		if i == maxFiles {
			lines = append(lines, st.Muted.Render(fmt.Sprintf("  … +%d", len(c.plan.Changes)-maxFiles)))
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
	return st.Text.Render("~ " + pad(tr("tui.op.update"), 6))
}

func (c *confirmScreen) layout() {
	inner := maxInt(c.w-4, 10)
	head := 0
	if c.state == confReady {
		head = len(c.headerLines()) + 1
	}
	c.vp.Width = inner
	c.vp.Height = maxInt(c.h-2-1-head, 1) // borders, footer line, then the header and its divider
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
		// the screen itself says "Đã áp dụng": no second message
		c.state = confDone
		return c, refreshPlan()
	case tea.KeyMsg:
		switch c.state {
		case confReady:
			switch {
			case key.Matches(msg, km.Yes):
				return c, c.apply()
			case key.Matches(msg, km.No):
				return c, tea.Batch(c.sh.popNow(), hint(c.sh.km.T("tui.status.apply_later")))
			case key.Matches(msg, km.Enter):
				// enter does not apply: say which key does
				return c, say(levelInfo, km.T("tui.status.press_yes"))
			}
			var cmd tea.Cmd
			c.vp, cmd = c.vp.Update(msg)
			return c, cmd
		case confDone, confEmpty:
			if key.Matches(msg, km.Enter) {
				return c, c.sh.popRootNow()
			}
			if c.state == confDone && key.Matches(msg, km.Check) {
				return c, c.sh.replaceNow(newCheckScreen(c.sh))
			}
			if c.state == confDone && key.Matches(msg, km.Clone) {
				return c, c.sh.replaceNow(newReposScreen(c.sh, nil))
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
		if empty := emptyCompanies(c.sh.cfg); len(empty) > 0 {
			body = st.Warn.Render(wrap("! "+c.sh.km.T("tui.confirm.empty_no_accounts", empty[0]), minInt(maxInt(c.w-6, 10), measure)))
		} else {
			body = st.OK.Render("✓ " + tr("tui.confirm.empty"))
		}
	case confReady:
		foot := c.sh.km.T("tui.confirm.question")
		// the percentage matters only while there is more to scroll to
		if c.vp.TotalLineCount() > c.vp.Height {
			foot = fmt.Sprintf("%3.0f%%", c.vp.ScrollPercent()*100) + "  " + foot
		}
		body = strings.Join(c.headerLines(), "\n") + "\n" +
			st.divider(c.w-4) + "\n" + c.vp.View() + "\n" + st.Text.Render(foot)
	case confApplying:
		body = c.spin.View() + " " + st.Muted.Render(tr("tui.confirm.applying"))
	case confDone:
		body = c.doneView()
	case confFailed:
		body = st.Err.Render("✗ "+wrap(errText(c.err), minInt(maxInt(c.w-6, 10), measure))) + "\n\n" + st.Text.Render(c.sh.km.T("tui.confirm.retry_hint"))
	}
	// the breadcrumb already says what this screen is: no title row
	return c.sh.panel("", body, c.w, c.h, true)
}

// doneView tells what changed for the user ("from now on"), what the help bar
// cannot say (the first sign-in over HTTPS or SSH) and only then lists the
// files and the backup. The next step (clone, check) is the help bar's: the
// body does not repeat it. When the screen is short, parts go in a fixed
// order: the file list, the override note, the "gitid verifies nothing" line,
// the blank lines, the backup line, then the long sign-in explanations are cut
// to one line. The title and "from now on" stay as long as there is room for
// anything.
func (c *confirmScreen) doneView() string {
	st := c.sh.st
	env := c.sh.svc.Env
	inner := maxInt(c.w-4, 10)
	rows := maxInt(c.h-2, 1) // panel borders

	var effect []string
	for _, co := range c.sh.cfg.Companies {
		if def, ok := co.Default(); ok {
			ref := config.AccountRef{Company: co, Account: def}
			effect = append(effect, st.Text.Render(tr("tui.confirm.effect_line", co.BaseDir))+" "+st.identityStamp(ref))
		}
	}
	const maxEffect = 6
	extra := 0
	if len(effect) > maxEffect {
		extra = len(effect) - maxEffect
		effect = effect[:maxEffect]
	}
	var https, ssh bool
	for _, ref := range c.sh.cfg.All() {
		if ref.Account.Auth == config.AuthSSH {
			ssh = true
		} else {
			https = true
		}
	}

	// step renders "  n  text" with the text wrapped under itself (or cut to
	// one line when oneLine is set).
	step := func(n int, text string, oneLine bool) []string {
		prefix := st.Muted.Render(fmt.Sprintf("  %d  ", n))
		const indent = 5
		if oneLine {
			return []string{prefix + st.Text.Render(trunc(text, maxInt(inner-indent, 4)))}
		}
		var out []string
		for j, l := range wrapLines(text, minInt(maxInt(inner-indent, 8), measure)) {
			if j == 0 {
				out = append(out, prefix+st.Text.Render(l))
			} else {
				out = append(out, spaces(indent)+st.Text.Render(l))
			}
		}
		return out
	}

	type opts struct{ note, noVerify, blanks, backup, oneLine bool }
	build := func(o opts) (head, tail []string) {
		gap := func(l *[]string) {
			if o.blanks {
				*l = append(*l, "")
			}
		}
		head = append(head, st.OK.Render("✓ "+tr("tui.confirm.done")))
		gap(&head)
		if len(effect) > 0 {
			head = append(head, st.Bold.Render(tr("tui.confirm.effect")))
			for _, e := range effect {
				head = append(head, "  "+trunc(e, inner-2))
			}
			if extra > 0 {
				head = append(head, st.Muted.Render(fmt.Sprintf("  … +%d", extra)))
			}
			gap(&head)
		}
		if https || ssh {
			head = append(head, st.Bold.Render(tr("tui.confirm.next")))
		}
		n := 1
		add := func(text string) {
			head = append(head, step(n, text, o.oneLine)...)
			n++
		}
		// clone and check are the help bar's next steps: the body only holds
		// what the bar cannot say
		if https {
			add(tr("tui.confirm.next_https"))
		}
		if ssh {
			add(c.sh.km.T("tui.confirm.next_ssh"))
		}
		if o.noVerify {
			head = append(head, styledWrap(st.Muted, tr("tui.confirm.no_verify"), proseW(inner))...)
		}
		if o.note {
			head = append(head, styledWrap(st.Muted, tr("tui.confirm.override_note"), proseW(inner))...)
		}
		gap(&head)
		if o.backup && c.res.BackupDir != "" {
			col := kvCol(inner, tr("tui.confirm.backup"))
			tail = append(tail, kvRow(st, col, inner, tr("tui.confirm.backup"), env.Contract(c.res.BackupDir)))
		}
		for _, w := range c.res.Warnings {
			tail = append(tail, st.Warn.Render(trunc("! "+w, inner)))
		}
		return head, tail
	}
	// richest first; the first that fits wins, the last one is used otherwise
	levels := []opts{
		{note: true, noVerify: true, blanks: true, backup: true},
		{noVerify: true, blanks: true, backup: true},
		{blanks: true, backup: true},
		{backup: true},
		{},
		{oneLine: true},
	}
	var head, tail []string
	for _, o := range levels {
		head, tail = build(o)
		if len(head)+len(tail) <= rows {
			break
		}
	}

	var files []string
	for _, p := range c.res.Written {
		files = append(files, "  "+st.OK.Render("+")+" "+st.Text.Render(trunc(env.Contract(p), maxInt(inner-4, 4))))
	}
	for _, p := range c.res.Deleted {
		files = append(files, "  "+st.Err.Render("-")+" "+st.Text.Render(trunc(env.Contract(p), maxInt(inner-4, 4))))
	}
	if avail := rows - len(head) - len(tail); avail <= 0 {
		files = nil
	} else if len(files) > avail {
		more := st.Muted.Render(fmt.Sprintf("  … +%d", len(files)-avail+1))
		files = append(files[:avail-1:avail-1], more)
	}
	lines := append(append(head, files...), tail...)
	return strings.Join(lines, "\n")
}

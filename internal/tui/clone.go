package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
)

type clonePhase int

const (
	clURL clonePhase = iota
	clPick
	clRunning
	clVerifying
	clDone
	clFailed
)

// cloneTermDoneMsg reports the end of the terminal-mode clone.
type cloneTermDoneMsg struct {
	id  int
	err error
}

// cloneScreen guides a clone: paste a URL, pick the account (ranked by host),
// preview the rewritten URL and target directory, clone with a progress bar,
// and verify the email git will use in the new repository.
type cloneScreen struct {
	sh        *shared
	id        int
	phase     clonePhase
	input     textinput.Model
	url       string
	cands     []config.AccountRef
	cur       int
	preview   app.ClonePlan
	prevErr   error
	errMsg    string // input step feedback
	prog      progress.Model
	pct       int
	step      string
	cancel    context.CancelFunc
	ch        chan tea.Msg
	cancelled bool
	res       app.CloneResult
	err       error
	spin      spinner.Model
	w, h      int
}

func newCloneScreen(sh *shared) *cloneScreen {
	in := textinput.New()
	in.Prompt = "› "
	in.PromptStyle = sh.st.Accent
	in.Placeholder = tr("tui.form.eg", "https://github.com/org/repo.git")
	in.CharLimit = 1024
	p := progress.New(progress.WithGradient("#7C6BFF", "#2DD4BF"), progress.WithoutPercentage())
	return &cloneScreen{sh: sh, id: sh.id(), input: in, prog: p, spin: sh.st.Spinner}
}

func (c *cloneScreen) Title() string { return tr("tui.clone.title") }

func (c *cloneScreen) Init() tea.Cmd { return tea.Batch(c.input.Focus(), c.spin.Tick) }

func (c *cloneScreen) Resize(w, h int) {
	c.w, c.h = w, h
	c.input.Width = maxInt(w-10, 10)
	c.prog.Width = maxInt(w-14, 10)
}

func (c *cloneScreen) Capturing() bool { return c.phase <= clRunning }

func (c *cloneScreen) Help() []key.Binding {
	km := c.sh.km
	switch c.phase {
	case clURL:
		return []key.Binding{km.Enter, km.Cancel}
	case clPick:
		return []key.Binding{km.Up, km.Down, withDesc(km.Enter, "enter", tr("tui.clone.start")), withDesc(km.Back, "esc", tr("tui.key.back"))}
	case clRunning:
		return []key.Binding{withDesc(km.Back, "esc", tr("tui.key.cancel"))}
	case clFailed:
		if errors.Is(c.err, app.ErrAuth) {
			return []key.Binding{km.TerminalMode, km.Back}
		}
		return []key.Binding{km.Back}
	case clDone:
		return []key.Binding{km.Enter, km.Back}
	}
	return nil
}

func waitMsg(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		m, ok := <-ch
		if !ok {
			return nil
		}
		return m
	}
}

func (c *cloneScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	km := c.sh.km
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if c.phase == clVerifying {
			var cmd tea.Cmd
			c.spin, cmd = c.spin.Update(msg)
			return c, cmd
		}
		return c, nil
	case cloneProgressMsg:
		if msg.id != c.id || c.phase != clRunning {
			return c, nil
		}
		c.step, c.pct = msg.Phase, clamp(msg.Pct, 0, 100)
		if c.ch != nil {
			return c, waitMsg(c.ch)
		}
		return c, nil
	case cloneDoneMsg:
		if msg.id != c.id {
			return c, nil
		}
		return c.finish(msg.Res, msg.Err)
	case cloneTermDoneMsg:
		if msg.id != c.id {
			return c, nil
		}
		if msg.err != nil {
			return c.finish(app.CloneResult{Plan: c.preview}, msg.err)
		}
		c.phase = clVerifying
		svc, p, id := c.sh.svc, c.preview, c.id
		return c, tea.Batch(c.spin.Tick, func() tea.Msg {
			ctx, cancel := ctxTimeout(shortTimeout)
			defer cancel()
			return cloneDoneMsg{id: id, Res: svc.VerifyClone(ctx, p)}
		})
	case tea.KeyMsg:
		switch c.phase {
		case clURL:
			switch msg.Type {
			case tea.KeyEsc:
				return c, c.sh.popNow()
			case tea.KeyEnter:
				return c, c.submitURL()
			}
			var cmd tea.Cmd
			c.input, cmd = c.input.Update(msg)
			c.errMsg = ""
			return c, cmd
		case clPick:
			switch {
			case msg.Type == tea.KeyEsc:
				c.phase = clURL
				return c, c.input.Focus()
			case key.Matches(msg, km.Up):
				c.move(-1)
			case key.Matches(msg, km.Down):
				c.move(1)
			case key.Matches(msg, km.Enter):
				return c, c.start()
			}
		case clRunning:
			if msg.Type == tea.KeyEsc && c.cancel != nil {
				c.cancelled = true
				c.cancel()
			}
		case clFailed:
			if key.Matches(msg, km.TerminalMode) && errors.Is(c.err, app.ErrAuth) {
				return c, c.terminal()
			}
		case clDone:
			if key.Matches(msg, km.Enter) {
				return c, c.sh.popNow()
			}
		}
		return c, nil
	}
	if c.phase == clURL {
		var cmd tea.Cmd
		c.input, cmd = c.input.Update(msg)
		return c, cmd
	}
	return c, nil
}

func (c *cloneScreen) submitURL() tea.Cmd {
	url := strings.TrimSpace(c.input.Value())
	if url == "" {
		return nil
	}
	cands := c.sh.svc.Candidates(c.sh.cfg, url)
	if len(cands) == 0 {
		c.errMsg = tr("tui.clone.no_accounts")
		return nil
	}
	c.url, c.cands, c.cur, c.phase = url, cands, 0, clPick
	c.input.Blur()
	c.refreshPreview()
	return nil
}

func (c *cloneScreen) move(d int) {
	c.cur = clamp(c.cur+d, 0, len(c.cands)-1)
	c.refreshPreview()
}

// refreshPreview resolves the rewritten URL and target directory. It only
// computes strings and checks that the target does not exist.
func (c *cloneScreen) refreshPreview() {
	ref := c.cands[c.cur]
	c.preview, c.prevErr = c.sh.svc.PlanClone(c.sh.cfg, app.CloneRequest{URL: c.url, Account: ref.String(), Cwd: c.sh.cwd})
}

func (c *cloneScreen) start() tea.Cmd {
	if c.prevErr != nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel, c.cancelled = cancel, false
	c.phase, c.pct, c.step, c.err = clRunning, 0, "", nil
	ch := make(chan tea.Msg, 64)
	c.ch = ch
	svc, p, id := c.sh.svc, c.preview, c.id
	go func() {
		res, err := svc.Clone(ctx, p, func(pr app.CloneProgress) {
			select {
			case ch <- cloneProgressMsg{id: id, Phase: pr.Phase, Pct: pr.Pct}:
			default: // drop a report rather than block git
			}
		})
		ch <- cloneDoneMsg{id: id, Res: res, Err: err}
		close(ch)
	}()
	return waitMsg(ch)
}

func (c *cloneScreen) terminal() tea.Cmd {
	cmd, err := c.sh.svc.CloneExec(c.preview)
	if err != nil {
		c.err = err
		return nil
	}
	c.phase = clRunning
	c.step, c.pct = "", 0
	c.ch, c.cancel = nil, nil
	id := c.id
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return cloneTermDoneMsg{id: id, err: err} })
}

func (c *cloneScreen) finish(res app.CloneResult, err error) (Screen, tea.Cmd) {
	c.ch = nil
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	if err != nil {
		if c.cancelled {
			c.phase = clPick
			return c, say(levelInfo, tr("tui.clone.cancelled"))
		}
		c.phase, c.err = clFailed, err
		return c, nil
	}
	c.phase, c.res = clDone, res
	return c, say(levelOK, tr("tui.status.cloned", c.sh.svc.Env.Contract(res.Plan.Dir)))
}

// clonePhaseKeys maps the first word of git's progress phase ("Receiving
// objects", ...) to a translation key.
var clonePhaseKeys = map[string]string{
	"Enumerating": "tui.clone.phase.enumerating",
	"Counting":    "tui.clone.phase.counting",
	"Compressing": "tui.clone.phase.compressing",
	"Receiving":   "tui.clone.phase.receiving",
	"Resolving":   "tui.clone.phase.resolving",
}

func phaseLabel(p string) string {
	first, _, _ := strings.Cut(p, " ")
	if k, ok := clonePhaseKeys[first]; ok {
		return tr(k)
	}
	return p
}

func (c *cloneScreen) View() string {
	st := c.sh.st
	inner := maxInt(c.w-6, 10)
	var b strings.Builder
	switch c.phase {
	case clURL:
		b.WriteString(st.Bold.Render(tr("tui.clone.paste")) + "\n\n")
		b.WriteString(c.input.View() + "\n\n")
		b.WriteString(wrap(st.Faint.Render(tr("tui.clone.paste_hint")), inner))
		if c.errMsg != "" {
			b.WriteString("\n\n" + st.Err.Render("✗ "+c.errMsg))
		}
	case clPick:
		b.WriteString(st.Bold.Render(tr("tui.clone.pick")) + "\n")
		b.WriteString(c.candidateList(inner) + "\n")
		b.WriteString(c.previewView(inner))
	case clRunning:
		b.WriteString(st.Bold.Render(tr("tui.clone.cloning", c.sh.svc.Env.Contract(c.preview.Dir))) + "\n\n")
		b.WriteString(c.prog.ViewAs(float64(c.pct)/100) + fmt.Sprintf(" %3d%%", c.pct) + "\n")
		label := phaseLabel(c.step)
		if label == "" {
			label = tr("tui.clone.connecting")
		}
		b.WriteString(st.Muted.Render(label))
	case clVerifying:
		b.WriteString(c.spin.View() + " " + st.Muted.Render(tr("tui.clone.verifying")))
	case clDone:
		b.WriteString(st.OK.Bold(true).Render("✓ "+tr("tui.clone.done")) + "\n\n")
		b.WriteString(st.Label.Render(tr("tui.f.dir")+" ") + st.Text.Render(c.sh.svc.Env.Contract(c.res.Plan.Dir)) + "\n")
		b.WriteString(st.Label.Render(tr("tui.f.account")+" ") + st.Text.Render(c.res.Plan.Account.String()) + "\n\n")
		switch {
		case c.res.EmailOK:
			b.WriteString(st.OK.Render("✓ " + wrap(tr("tui.clone.email_ok", c.res.EffectiveEmail), inner)))
		case c.res.EffectiveEmail == "":
			b.WriteString(st.Warn.Render("! " + wrap(tr("tui.clone.email_unknown"), inner)))
		default:
			b.WriteString(st.Warn.Render("! " + wrap(tr("tui.clone.email_bad", c.res.EffectiveEmail, c.res.Plan.Account.Account.Email), inner)))
		}
	case clFailed:
		b.WriteString(st.Err.Render("✗ "+wrap(errText(c.err), inner)) + "\n")
		if errors.Is(c.err, app.ErrAuth) {
			b.WriteString("\n" + st.Warn.Render(wrap(tr("tui.clone.auth_hint"), inner)) + "\n")
			b.WriteString("\n" + st.ButtonOn.Render("t  "+tr("tui.clone.terminal_mode")))
		}
	}
	return c.sh.panel(tr("tui.clone.title"), b.String(), c.w, c.h, true)
}

func (c *cloneScreen) candidateList(w int) string {
	st := c.sh.st
	room := maxInt(c.h-2-1-1-9, 2) // keep room for the preview block
	start := 0
	if c.cur >= room {
		start = c.cur - room + 1
	}
	var lines []string
	for i := start; i < len(c.cands) && i < start+room; i++ {
		r := c.cands[i]
		a := r.Account
		plain := fmt.Sprintf("%s  %s %s  %s", r.String(), platformTag(a.Platform), authTag(a.Auth), a.Email)
		if i == c.cur {
			lines = append(lines, st.Selected.Render(pad("▌"+plain, w)))
			continue
		}
		lines = append(lines, trunc(" "+st.Text.Render(r.String())+"  "+st.platformBadge(a.Platform)+" "+st.Muted.Render(authTag(a.Auth))+"  "+st.Faint.Render(a.Email), w))
	}
	if more := len(c.cands) - (start + room); more > 0 {
		lines = append(lines, st.Faint.Render(fmt.Sprintf("  … +%d", more)))
	}
	return strings.Join(lines, "\n")
}

func (c *cloneScreen) previewView(w int) string {
	st := c.sh.st
	var b strings.Builder
	b.WriteString("\n" + st.Faint.Render(strings.Repeat("─", maxInt(w, 4))) + "\n")
	if c.prevErr != nil {
		b.WriteString(st.Err.Render("✗ " + wrap(errText(c.prevErr), w)))
		return b.String()
	}
	row := func(label, value string) {
		b.WriteString(st.Label.Render(pad(label, 10)) + st.Text.Render(trunc(value, maxInt(w-10, 4))) + "\n")
	}
	row(tr("tui.f.original"), c.preview.Original)
	row(tr("tui.f.clone_url"), c.preview.URL)
	row(tr("tui.f.dir"), c.sh.svc.Env.Contract(c.preview.Dir))
	for _, wk := range c.preview.Warnings {
		b.WriteString(st.Warn.Render("! "+trunc(tr(wk), maxInt(w-2, 4))) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

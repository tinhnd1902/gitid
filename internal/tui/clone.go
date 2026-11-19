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
	"github.com/charmbracelet/lipgloss"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/keymap"
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
	prefer    *config.AccountRef // account the pick step starts on, when it is a candidate
	w, h      int

	// embedded: the screen is the URL tab of the repo browser, which draws
	// the tab bar (the title) and gives the screen one row less.
	embedded bool
	// backPops: esc in the pick step leaves the screen instead of going back
	// to the URL step (the URL was given, there is no step to go back to).
	backPops bool
	// dir is the clone target folder; "" takes <base_dir>/<repo>.
	dir string
	// header, when embedded, is the tab bar the repo browser draws as the
	// first line inside the panel.
	header string
}

// newCloneScreen starts the clone flow. prefer (optional) is the account the
// user had highlighted: the pick step starts on it when the URL fits it.
// accentHex is the accent colour of the terminal's background: the progress
// bar takes a plain colour string, not an adaptive one.
func accentHex() string {
	if lipgloss.HasDarkBackground() {
		return colAccent.Dark
	}
	return colAccent.Light
}

func newCloneScreen(sh *shared, prefer *config.AccountRef) *cloneScreen {
	in := textinput.New()
	in.Prompt = "> "
	in.PromptStyle = sh.st.Accent
	in.Placeholder = tr("tui.form.eg", "https://github.com/org/repo.git")
	in.CharLimit = 1024
	p := progress.New(progress.WithSolidFill(accentHex()), progress.WithoutPercentage())
	return &cloneScreen{sh: sh, id: sh.id(), input: in, prog: p, spin: sh.st.Spinner, prefer: prefer}
}

// newCloneScreenFor opens the clone flow on a URL that is already known: it
// starts at the account pick, with prefer pre-selected, and clones into dir
// ("" for <base_dir>/<repo>). esc in the pick step leaves the screen.
func newCloneScreenFor(sh *shared, prefer *config.AccountRef, url, dir string) *cloneScreen {
	c := newCloneScreen(sh, prefer)
	c.input.SetValue(url)
	c.dir, c.backPops = dir, true
	c.submitURL()
	return c
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
		if strings.TrimSpace(c.input.Value()) == "" {
			return []key.Binding{km.Cancel} // nothing to choose until a URL is in
		}
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
		if c.phase == clPick && imeLike(msg) {
			return c, imeCmd()
		}
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
			case msg.Type == tea.KeyEsc, key.Matches(msg, km.Back):
				if c.backPops {
					return c, c.sh.popNow()
				}
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
	c.url, c.cands, c.cur, c.phase = url, cands, c.preferred(cands), clPick
	c.input.Blur()
	c.refreshPreview()
	return nil
}

// preferred is the index of the candidate the pick step starts on: the
// account that was highlighted when the clone was started, else the first.
func (c *cloneScreen) preferred(cands []config.AccountRef) int {
	if c.prefer == nil {
		return 0
	}
	for i, r := range cands {
		if r.Company.Slug == c.prefer.Company.Slug && r.Account.ID == c.prefer.Account.ID {
			return i
		}
	}
	return 0
}

func (c *cloneScreen) move(d int) {
	c.cur = clamp(c.cur+d, 0, len(c.cands)-1)
	c.refreshPreview()
}

// refreshPreview resolves the rewritten URL and target directory. It only
// computes strings and checks that the target does not exist.
func (c *cloneScreen) refreshPreview() {
	ref := c.cands[c.cur]
	c.preview, c.prevErr = c.sh.svc.PlanClone(c.sh.cfg, app.CloneRequest{URL: c.url, Account: ref.String(), Cwd: c.sh.cwd, Dir: c.dir})
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
	// the repo browser reads the new repository
	dir, company := res.Plan.Dir, res.Plan.Account.Company.Slug
	return c, tea.Batch(
		say(levelOK, tr("tui.status.cloned", c.sh.svc.Env.Contract(res.Plan.Dir))),
		func() tea.Msg { return repoClonedMsg{dir: dir, company: company} },
	)
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
		b.WriteString(wrap(st.Muted.Render(tr("tui.clone.paste_hint")), proseW(inner)))
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
		b.WriteString(st.OK.Render("✓ "+tr("tui.clone.done")) + "\n\n")
		col := kvCol(inner, tr("tui.f.dir"), tr("tui.f.account"))
		b.WriteString(kvRow(st, col, inner, tr("tui.f.dir"), c.sh.svc.Env.Contract(c.res.Plan.Dir)) + "\n")
		b.WriteString(st.Label.Render(pad(tr("tui.f.account"), col)) + trunc(st.identityStamp(c.res.Plan.Account), maxInt(inner-col, 4)) + "\n\n")
		switch {
		case c.res.EmailOK:
			b.WriteString(st.OK.Render("✓ " + wrap(tr("tui.clone.email_ok", c.res.EffectiveEmail), proseW(inner))))
		case c.res.EffectiveEmail == "":
			b.WriteString(st.Warn.Render("! " + wrap(tr("tui.clone.email_unknown"), proseW(inner))))
		default:
			b.WriteString(st.Warn.Render("! " + wrap(tr("tui.clone.email_bad", c.res.EffectiveEmail, c.res.Plan.Account.Account.Email), proseW(inner))))
		}
	case clFailed:
		b.WriteString(st.Err.Render("✗ "+wrap(errText(c.err), inner)) + "\n")
		if errors.Is(c.err, app.ErrAuth) {
			b.WriteString("\n" + st.Text.Render(wrap(tr("tui.clone.auth_hint"), proseW(inner))) + "\n")
			b.WriteString("\n" + st.ButtonOn.Render(c.sh.km.keys.Label(keymap.Terminal)+"  "+tr("tui.clone.terminal_mode")))
		}
	}
	// the breadcrumb (or the tab bar) already names the screen
	body := b.String()
	if c.embedded {
		body = c.header + "\n" + body
	}
	return c.sh.panel("", body, c.w, c.h, true)
}

func (c *cloneScreen) candidateList(w int) string {
	st := c.sh.st
	room := maxInt(c.h-2-1-1-9, 2) // keep room for the preview block
	start := 0
	if c.cur >= room {
		start = c.cur - room + 1
	}
	// aligned columns: the account, its platform and login, its email
	refW, kindW := 0, 0
	for _, r := range c.cands {
		refW = maxInt(refW, lipgloss.Width(r.String()))
		kindW = maxInt(kindW, lipgloss.Width(platformTag(r.Account.Platform)+" "+authTag(r.Account.Auth)))
	}
	var lines []string
	for i := start; i < len(c.cands) && i < start+room; i++ {
		r := c.cands[i]
		a := r.Account
		kind := pad(platformTag(a.Platform)+" "+authTag(a.Auth), kindW)
		plain := pad(r.String(), refW) + "  " + kind + "  " + a.Email
		if i == c.cur {
			lines = append(lines, st.Selected.Render(pad("▌"+plain, w)))
			continue
		}
		lines = append(lines, trunc(" "+st.Text.Render(pad(r.String(), refW))+"  "+st.Muted.Render(kind)+"  "+st.Muted.Render(a.Email), w))
	}
	if more := len(c.cands) - (start + room); more > 0 {
		lines = append(lines, st.Muted.Render(fmt.Sprintf("  … +%d", more)))
	}
	return strings.Join(lines, "\n")
}

func (c *cloneScreen) previewView(w int) string {
	st := c.sh.st
	var b strings.Builder
	b.WriteString("\n" + st.divider(w) + "\n")
	if c.prevErr != nil {
		b.WriteString(st.Err.Render("✗ " + wrap(errText(c.prevErr), w)))
		return b.String()
	}
	col := kvCol(w, tr("tui.f.original"), tr("tui.f.clone_url"), tr("tui.f.dir"))
	row := func(label, value string) {
		b.WriteString(kvRow(st, col, w, label, value) + "\n")
	}
	row(tr("tui.f.original"), c.preview.Original)
	row(tr("tui.f.clone_url"), c.preview.URL)
	row(tr("tui.f.dir"), c.sh.svc.Env.Contract(c.preview.Dir))
	for _, wk := range c.preview.Warnings {
		b.WriteString(st.Warn.Render("! "+trunc(tr(wk), maxInt(w-2, 4))) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

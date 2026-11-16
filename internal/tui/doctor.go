package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tinhnd1902/gitid/internal/doctor"
)

// doctorRow is one diagnostic: pending (spinner) until its results arrive.
type doctorRow struct {
	id      string
	label   string
	done    bool
	results []doctor.Result
}

// doctorScreen runs every diagnostic concurrently, each with its own
// spinner, and fills the rows in as results arrive (in any order). The
// `ssh -T` connectivity test only runs when the user presses s.
type doctorScreen struct {
	sh      *shared
	id      int
	rows    []*doctorRow
	ssh     []*doctorRow
	vp      viewport.Model
	spin    spinner.Model
	summary string
	w, h    int
}

func newDoctorScreen(sh *shared) *doctorScreen {
	d := &doctorScreen{sh: sh, id: sh.id(), spin: sh.st.Spinner, vp: viewport.New(10, 5)}
	d.reset()
	return d
}

func (d *doctorScreen) reset() {
	d.rows, d.ssh = nil, nil
	for _, c := range d.sh.svc.DoctorChecks(doctor.Options{}) {
		d.rows = append(d.rows, &doctorRow{id: c.ID, label: tr("tui.doctor.check." + c.ID)})
	}
}

func (d *doctorScreen) Title() string { return tr("tui.doctor.title") }

func (d *doctorScreen) Resize(w, h int) {
	d.w, d.h = w, h
	d.vp.Width, d.vp.Height = maxInt(w-4, 10), maxInt(h-2-2, 1)
	d.render()
}

func (d *doctorScreen) Help() []key.Binding {
	km := d.sh.km
	return []key.Binding{km.SSHTest, km.Retry, withDesc(km.Up, "↑/↓", tr("tui.key.scroll")), km.Back}
}

func (d *doctorScreen) Init() tea.Cmd { return d.start() }

// start launches every check as its own command.
func (d *doctorScreen) start() tea.Cmd {
	d.id = d.sh.id()
	d.reset()
	svc, cfg, id := d.sh.svc, d.sh.cfg, d.id
	cmds := []tea.Cmd{d.spin.Tick}
	for _, c := range svc.DoctorChecks(doctor.Options{}) {
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := ctxTimeout(shortTimeout)
			defer cancel()
			return doctorResultMsg{id: id, check: c.ID, results: svc.RunDoctorCheck(ctx, cfg, c)}
		})
	}
	d.render()
	return tea.Batch(cmds...)
}

func (d *doctorScreen) pending() bool {
	for _, r := range append(append([]*doctorRow(nil), d.rows...), d.ssh...) {
		if !r.done {
			return true
		}
	}
	return false
}

func (d *doctorScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	km := d.sh.km
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if !d.pending() {
			return d, nil
		}
		var cmd tea.Cmd
		d.spin, cmd = d.spin.Update(msg)
		d.render()
		return d, cmd
	case doctorResultMsg:
		if msg.id != d.id {
			return d, nil
		}
		for _, r := range d.rows {
			if r.id == msg.check {
				r.done, r.results = true, msg.results
			}
		}
		d.render()
		return d, nil
	case doctorSSHMsg:
		if msg.id != d.id {
			return d, nil
		}
		for _, r := range d.ssh {
			if r.id == msg.alias {
				r.done, r.results = true, []doctor.Result{msg.result}
			}
		}
		d.render()
		d.vp.GotoBottom()
		return d, nil
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, km.Retry):
			return d, d.start()
		case key.Matches(msg, km.SSHTest):
			return d, d.startSSH()
		}
		var cmd tea.Cmd
		d.vp, cmd = d.vp.Update(msg)
		return d, cmd
	}
	return d, nil
}

// startSSH runs `ssh -T` for every ssh alias (network access, opt-in).
func (d *doctorScreen) startSSH() tea.Cmd {
	aliases := d.sh.svc.SSHAliases(d.sh.cfg)
	if len(aliases) == 0 {
		return say(levelInfo, tr("tui.doctor.no_ssh"))
	}
	for _, r := range d.ssh {
		if !r.done {
			return nil // already running
		}
	}
	d.ssh = nil
	svc, cfg, id := d.sh.svc, d.sh.cfg, d.id
	cmds := []tea.Cmd{d.spin.Tick}
	for _, al := range aliases {
		d.ssh = append(d.ssh, &doctorRow{id: al, label: "ssh -T " + al})
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := ctxTimeout(shortTimeout)
			defer cancel()
			return doctorSSHMsg{id: id, alias: al, result: svc.SSHTest(ctx, cfg, al)}
		})
	}
	d.render()
	d.vp.GotoBottom()
	return tea.Batch(cmds...)
}

func statusGlyph(st *Styles, s doctor.Status) string {
	switch s {
	case doctor.OK:
		return st.OK.Render("✓")
	case doctor.Warn:
		return st.Warn.Render("!")
	case doctor.Fail:
		return st.Err.Render("✗")
	case doctor.Info:
		return st.Info.Render("i")
	}
	return st.Faint.Render("-")
}

func anyArgs(s []string) []any {
	out := make([]any, len(s))
	for i, a := range s {
		out[i] = a
	}
	return out
}

func worst(rs []doctor.Result) doctor.Status {
	w := doctor.OK
	for _, r := range rs {
		switch r.Status {
		case doctor.Fail:
			return doctor.Fail
		case doctor.Warn:
			w = doctor.Warn
		case doctor.Info:
			if w == doctor.OK {
				w = doctor.Info
			}
		}
	}
	return w
}

// render rebuilds the scrollable content from the current rows.
func (d *doctorScreen) render() {
	st := d.sh.st
	width := maxInt(d.vp.Width, 20)
	var b strings.Builder
	var ok, warn, fail int
	emit := func(r *doctorRow) {
		if !r.done {
			b.WriteString(d.spin.View() + " " + st.Muted.Render(r.label) + "\n")
			return
		}
		w := worst(r.results)
		switch w {
		case doctor.Fail:
			fail++
		case doctor.Warn:
			warn++
		default:
			ok++
		}
		head := statusGlyph(st, w) + " " + st.Bold.Render(r.label)
		if len(r.results) == 1 && r.results[0].Status == doctor.OK {
			head += st.Muted.Render("  " + tr(r.results[0].Msg, anyArgs(r.results[0].Args)...))
		}
		b.WriteString(trunc(head, width) + "\n")
		if len(r.results) == 1 && r.results[0].Status == doctor.OK {
			return
		}
		for _, res := range r.results {
			line := "  " + statusGlyph(st, res.Status) + " " + tr(res.Msg, anyArgs(res.Args)...)
			b.WriteString(trunc(line, width) + "\n")
			if res.Detail != "" && res.Status != doctor.OK {
				b.WriteString(trunc("      "+st.Faint.Render(res.Detail), width) + "\n")
			}
			if res.HintKey != "" && res.Status != doctor.OK {
				b.WriteString(trunc("      "+st.Accent.Render("→ "+tr(res.HintKey)), width) + "\n")
			}
		}
	}
	for _, r := range d.rows {
		emit(r)
	}
	if len(d.ssh) > 0 {
		b.WriteString("\n" + st.Faint.Render(tr("tui.doctor.ssh_header")) + "\n")
		for _, r := range d.ssh {
			emit(r)
		}
	}
	d.vp.SetContent(b.String())
	d.summary = ""
	if !d.pending() {
		d.summary = st.OK.Render(fmt.Sprintf("✓ %d", ok)) + st.Faint.Render(" · ") +
			st.Warn.Render(fmt.Sprintf("! %d", warn)) + st.Faint.Render(" · ") + st.Err.Render(fmt.Sprintf("✗ %d", fail))
	}
}

func (d *doctorScreen) View() string {
	head := d.summary
	if head == "" {
		head = d.sh.st.Faint.Render(tr("tui.doctor.running"))
	}
	return d.sh.panel(tr("tui.doctor.title"), head+"\n"+d.vp.View(), d.w, d.h, true)
}

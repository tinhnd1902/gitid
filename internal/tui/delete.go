package tui

import (
	"errors"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
)

// choice is a two-button yes/no control. The default (index 0) is always the
// safe answer, so enter never does anything destructive by accident.
type choice struct {
	labels [2]string
	idx    int
	danger bool // render the second button as destructive
}

func (c *choice) toggle() { c.idx = 1 - c.idx }

func (c choice) view(st *Styles) string {
	btn := func(i int) string {
		switch {
		case c.idx != i:
			return st.Button.Render(c.labels[i])
		case i == 1 && c.danger:
			return st.ButtonDanger.Render(c.labels[i])
		}
		return st.ButtonOn.Render(c.labels[i])
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, btn(0), " ", btn(1))
}

// deleteScreen removes an account (or a whole company) in two steps: a
// confirmation that defaults to "no", then - only when SSH keys exist - a
// prompt that keeps the keys unless the user types the exact id.
type deleteScreen struct {
	sh      *shared
	company config.Company
	account *config.Account // nil: delete the company
	id      int
	step    int // 0 confirm, 1 key prompt, 2 working
	pick    choice
	input   textinput.Model
	keys    []string // key paths found on disk
	errMsg  string
	spin    spinner.Model
	w, h    int
}

func newDeleteScreen(sh *shared, c config.Company, a *config.Account) *deleteScreen {
	d := &deleteScreen{sh: sh, company: c, account: a, id: sh.id(), spin: sh.st.Spinner}
	d.pick = choice{labels: [2]string{tr("tui.delete.no"), tr("tui.delete.yes")}, danger: true}
	d.input = textinput.New()
	d.input.Prompt = "› "
	d.input.PromptStyle = sh.st.Accent
	d.input.Placeholder = d.word()
	d.input.CharLimit = 64
	d.keys = d.findKeys()
	return d
}

// word is what the user must type to delete the keys.
func (d *deleteScreen) word() string {
	if d.account != nil {
		return d.account.ID
	}
	return d.company.Slug
}

func (d *deleteScreen) findKeys() []string {
	var out []string
	accs := d.company.Accounts
	if d.account != nil {
		accs = []config.Account{*d.account}
	}
	for _, a := range accs {
		if a.SSHKey == "" {
			continue
		}
		ref := config.AccountRef{Company: d.company, Account: a}
		if info := d.sh.svc.KeyInfo(ref); info.Exists || info.PubExists {
			out = append(out, a.SSHKey)
		}
	}
	return out
}

func (d *deleteScreen) Title() string { return tr("tui.delete.title") }
func (d *deleteScreen) Init() tea.Cmd { return d.spin.Tick }
func (d *deleteScreen) Resize(w, h int) {
	d.w, d.h = w, h
	d.input.Width = maxInt(w-12, 10)
}

// Capturing: the key prompt has a text input, and the working state must not
// be interrupted.
func (d *deleteScreen) Capturing() bool { return d.step >= 1 }

func (d *deleteScreen) Help() []key.Binding {
	km := d.sh.km
	switch d.step {
	case 0:
		return []key.Binding{km.Yes, km.No, km.Toggle, km.Enter, km.Back}
	case 1:
		return []key.Binding{withDesc(km.Enter, "enter", tr("tui.delete.keep_key")), withDesc(km.Back, "esc", tr("tui.key.cancel"))}
	}
	return nil
}

func (d *deleteScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	km := d.sh.km
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if d.step == 2 {
			var cmd tea.Cmd
			d.spin, cmd = d.spin.Update(msg)
			return d, cmd
		}
		return d, nil
	case deletedMsg:
		if msg.id != d.id {
			return d, nil
		}
		if msg.err != nil {
			d.step, d.errMsg = 0, errText(msg.err)
			d.pick.idx = 0
			if errors.Is(msg.err, app.ErrConfigChanged) {
				return d, changed(msg.res.Config) // show what is on disk now
			}
			return d, nil
		}
		status := tr("tui.status.account_deleted", d.company.Slug+"/"+d.word())
		if d.account == nil {
			status = tr("tui.status.company_deleted", d.company.Slug)
		}
		if len(msg.res.KeysRemoved) > 0 {
			status += " " + tr("tui.status.keys_removed", len(msg.res.KeysRemoved))
		}
		// in order: the confirm screen plans against the configuration that
		// was just saved, so it must be created after cfgChangedMsg is handled
		return d, tea.Sequence(changed(msg.res.Config), say(levelOK, status), replace(newConfirmScreen(d.sh, tr("tui.confirm.title_apply"))))
	case tea.KeyMsg:
		switch d.step {
		case 0:
			switch {
			case key.Matches(msg, km.Yes):
				return d, d.advance()
			case key.Matches(msg, km.No), msg.Type == tea.KeyEsc:
				return d, d.sh.popNow()
			case key.Matches(msg, km.Toggle):
				d.pick.toggle()
			case key.Matches(msg, km.Enter):
				if d.pick.idx == 1 {
					return d, d.advance()
				}
				return d, d.sh.popNow()
			}
		case 1:
			switch msg.Type {
			case tea.KeyEsc:
				return d, d.sh.popNow()
			case tea.KeyEnter:
				v := strings.TrimSpace(d.input.Value())
				switch {
				case v == "":
					return d, d.run(true)
				case v == d.word():
					return d, d.run(false)
				}
				d.errMsg = tr("tui.delete.mismatch", d.word())
				return d, nil
			}
			var cmd tea.Cmd
			d.input, cmd = d.input.Update(msg)
			return d, cmd
		}
		return d, nil
	}
	if d.step == 1 {
		var cmd tea.Cmd
		d.input, cmd = d.input.Update(msg)
		return d, cmd
	}
	return d, nil
}

func (d *deleteScreen) advance() tea.Cmd {
	d.errMsg = ""
	if len(d.keys) == 0 {
		return d.run(true)
	}
	d.step = 1
	return d.input.Focus()
}

// run deletes in the background. keep=true keeps every SSH key file.
func (d *deleteScreen) run(keep bool) tea.Cmd {
	d.step = 2
	d.errMsg = ""
	svc, cfg, id, comp, acc := d.sh.svc, d.sh.cfg, d.id, d.company, d.account
	return tea.Batch(d.spin.Tick, func() tea.Msg {
		// Never overwrite what another gitid window saved in the meantime.
		if disk, stale, err := svc.ChangedSince(cfg); err != nil {
			return deletedMsg{id: id, err: err}
		} else if stale {
			return deletedMsg{id: id, res: app.DeleteResult{Config: disk}, err: app.ErrConfigChanged}
		}
		if acc == nil {
			res, err := svc.DeleteCompany(cfg, comp.Slug, keep)
			return deletedMsg{id: id, res: res, err: err}
		}
		res, err := svc.DeleteAccount(cfg, config.AccountRef{Company: comp, Account: *acc}, keep)
		return deletedMsg{id: id, res: res, err: err}
	})
}

func (d *deleteScreen) View() string {
	st := d.sh.st
	inner := maxInt(d.w-6, 10)
	var b strings.Builder
	switch d.step {
	case 0:
		if d.account != nil {
			b.WriteString(st.Bold.Render(tr("tui.delete.q_account", d.company.Slug+"/"+d.account.ID)) + "\n\n")
			b.WriteString(wrap(st.Muted.Render(tr("tui.delete.account_note")), inner) + "\n")
		} else {
			b.WriteString(st.Bold.Render(tr("tui.delete.q_company", d.company.Slug, len(d.company.Accounts))) + "\n\n")
			b.WriteString(wrap(st.Muted.Render(tr("tui.delete.company_note")), inner) + "\n")
		}
		b.WriteString("\n" + d.pick.view(st) + "\n")
	case 1:
		b.WriteString(st.Bold.Render(tr("tui.delete.key_q")) + "\n\n")
		for _, k := range d.keys {
			b.WriteString("  " + st.Text.Render(k) + "\n")
		}
		b.WriteString("\n" + st.ButtonOn.Render(tr("tui.delete.keep_key")) + "  " + st.Faint.Render("enter") + "\n\n")
		b.WriteString(wrap(st.Muted.Render(tr("tui.delete.type_to_delete", d.word())), inner) + "\n")
		b.WriteString(d.input.View() + "\n")
	case 2:
		b.WriteString(d.spin.View() + " " + st.Muted.Render(tr("tui.delete.working")))
	}
	if d.errMsg != "" {
		b.WriteString("\n" + st.Err.Render("✗ "+wrap(d.errMsg, inner)))
	}
	return d.sh.panel(tr("tui.delete.title"), b.String(), d.w, d.h, true)
}

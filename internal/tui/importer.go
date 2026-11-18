package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/importer"
	"github.com/tinhnd1902/gitid/internal/platform"
)

type importPhase int

const (
	imLoading importPhase = iota
	imList
	imForm
	imSaving
)

// importScreen proposes companies from the user's existing includeIf
// entries. Nothing is guessed: the platform and username are asked in a
// small form, then the result goes through the normal save + diff flow.
type importScreen struct {
	sh     *shared
	id     int
	phase  importPhase
	res    importer.Result
	err    error
	cur    int
	form   *form
	guard  discardGuard
	plat   string
	auth   string
	user   string
	host   string
	errMsg string
	spin   spinner.Model
	w, h   int
}

func newImportScreen(sh *shared) *importScreen {
	return &importScreen{sh: sh, id: sh.id(), spin: sh.st.Spinner, plat: string(platform.GitHub), auth: string(config.AuthHTTPS)}
}

func (s *importScreen) Title() string { return tr("tui.import.title") }
func (s *importScreen) Resize(w, h int) {
	s.w, s.h = w, h
	if s.form != nil {
		s.sizeForm()
	}
}

// sizeForm gives the form the panel's inner area (the title takes one row).
func (s *importScreen) sizeForm() {
	rows := s.h - 3
	if s.guard.asking {
		rows-- // the discard question
	}
	s.form.SetSize(maxInt(s.w-4, 1), maxInt(rows, 1))
	s.form.compact = s.h < previewMinBody
}

func (s *importScreen) Capturing() bool { return s.phase == imForm || s.phase == imSaving }

func (s *importScreen) Init() tea.Cmd {
	svc, cfg, id := s.sh.svc, s.sh.cfg, s.id
	return tea.Batch(s.spin.Tick, func() tea.Msg {
		res, err := svc.Import(cfg)
		return importLoadedMsg{id: id, res: res, err: err}
	})
}

func (s *importScreen) Help() []key.Binding {
	km := s.sh.km
	switch s.phase {
	case imList:
		if len(s.res.Proposals) == 0 {
			return []key.Binding{km.Back} // nothing to pick: only the way out
		}
		return []key.Binding{km.Up, km.Down, withDesc(km.Enter, "enter", tr("tui.import.pick")), km.Back}
	case imForm:
		if s.guard.asking {
			return s.guard.helpBindings(km)
		}
		return []key.Binding{km.Save, km.Next, km.Cancel}
	}
	return nil
}

func (s *importScreen) buildForm() {
	sh := s.sh
	var opts []formOption
	for _, p := range platform.All() {
		opts = append(opts, formOption{label: p.DisplayName(), value: string(p)})
	}
	s.form = newForm(sh.st, sh.km,
		selectField(tr("tui.f.platform"), "", &s.plat, true, opts...),
		inputField(tr("tui.f.username"), tr("tui.form.username_hint"), tr("tui.form.eg", "alice"), &s.user,
			sh.fieldValidator("username", tr("tui.f.username"), true)),
		selectField(tr("tui.f.auth"), "", &s.auth, false,
			formOption{label: "HTTPS", value: string(config.AuthHTTPS)}, formOption{label: "SSH", value: string(config.AuthSSH)}),
		inputField(tr("tui.f.host"), tr("tui.form.host_hint"), tr("tui.form.eg", "git.acme.com"), &s.host,
			sh.fieldValidator("host", tr("tui.f.host"), false)),
	)
	s.sizeForm()
	s.guard.start(s.form)
}

func (s *importScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	km := s.sh.km
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if s.phase == imLoading || s.phase == imSaving {
			var cmd tea.Cmd
			s.spin, cmd = s.spin.Update(msg)
			return s, cmd
		}
		return s, nil
	case importLoadedMsg:
		if msg.id != s.id {
			return s, nil
		}
		s.res, s.err, s.phase = msg.res, msg.err, imList
		return s, nil
	case savedMsg:
		if msg.id != s.id {
			return s, nil
		}
		if msg.err != nil {
			s.phase, s.errMsg = imList, errText(msg.err)
			if errors.Is(msg.err, app.ErrConfigChanged) {
				return s, changed(msg.cfg) // show what is on disk now
			}
			return s, nil
		}
		return s, tea.Sequence(changed(msg.cfg), say(levelOK, tr("tui.status.imported")), replace(newConfirmScreen(s.sh, tr("tui.confirm.title_apply"))))
	case tea.KeyMsg:
		switch s.phase {
		case imList:
			switch {
			case key.Matches(msg, km.Up):
				s.cur = clamp(s.cur-1, 0, len(s.res.Proposals)-1)
			case key.Matches(msg, km.Down):
				s.cur = clamp(s.cur+1, 0, len(s.res.Proposals)-1)
			case key.Matches(msg, km.Enter):
				if len(s.res.Proposals) == 0 {
					return s, nil
				}
				if s.alreadyConfigured() {
					return s, say(levelWarn, tr(importer.WarnConfigured))
				}
				s.errMsg = ""
				s.buildForm()
				s.phase = imForm
				return s, s.form.Init()
			}
			return s, nil
		case imForm:
			back := func() tea.Cmd { s.phase = imList; return nil }
			if cmd, handled := s.guard.key(s.sh, s.form, msg, back); handled {
				return s, cmd
			}
		}
	}
	if s.phase == imForm && s.form != nil {
		cmd, done := s.form.Update(msg)
		if done {
			return s, tea.Batch(cmd, s.save())
		}
		return s, cmd
	}
	return s, nil
}

func (s *importScreen) alreadyConfigured() bool {
	for _, w := range s.res.Proposals[s.cur].Warnings {
		if w == importer.WarnConfigured {
			return true
		}
	}
	return false
}

func (s *importScreen) save() tea.Cmd {
	s.phase = imSaving
	svc, cfg, id := s.sh.svc, s.sh.cfg, s.id
	p := s.res.Proposals[s.cur]
	choice := app.ImportChoice{
		Platform: platform.Platform(s.plat),
		Username: strings.TrimSpace(s.user),
		Auth:     config.Auth(s.auth),
		Host:     strings.TrimSpace(s.host),
	}
	if choice.Host == platform.DefaultHost(choice.Platform) {
		choice.Host = ""
	}
	return tea.Batch(s.spin.Tick, func() tea.Msg {
		// Never overwrite what another gitid window saved in the meantime.
		if disk, stale, err := svc.ChangedSince(cfg); err != nil {
			return savedMsg{id: id, err: err}
		} else if stale {
			return savedMsg{id: id, cfg: disk, err: app.ErrConfigChanged}
		}
		comp := svc.ProposalToCompany(cfg, p, choice)
		saved, backup, err := svc.Commit(app.AddCompany(cfg, comp))
		return savedMsg{id: id, cfg: saved, backup: backup, err: err}
	})
}

func (s *importScreen) View() string {
	st := s.sh.st
	inner := maxInt(s.w-6, 10)
	var b strings.Builder
	switch s.phase {
	case imLoading:
		b.WriteString(s.spin.View() + " " + st.Muted.Render(tr("tui.import.loading")))
	case imSaving:
		b.WriteString(s.spin.View() + " " + st.Muted.Render(tr("tui.form.saving")))
	case imForm:
		s.sizeForm()
		title := tr("tui.import.form_title", s.res.Proposals[s.cur].Slug)
		if s.form.Overflows() {
			n, m := s.form.Position()
			title += st.Faint.Render(fmt.Sprintf("  %d/%d", n, m))
		}
		body := s.form.View()
		if s.guard.asking {
			body = st.Warn.Render(trunc("! "+s.guard.prompt(s.sh.km), inner)) + "\n" + body
		}
		return s.sh.panel(title, body, s.w, s.h, true)
	default:
		if s.err != nil {
			b.WriteString(st.Err.Render("✗ " + wrap(errText(s.err), inner)))
			break
		}
		b.WriteString(s.listView(inner))
	}
	return s.sh.panel(tr("tui.import.title"), b.String(), s.w, s.h, true)
}

func (s *importScreen) listView(w int) string {
	st := s.sh.st
	var b strings.Builder
	if s.errMsg != "" {
		b.WriteString(st.Err.Render("✗ "+wrap(s.errMsg, w)) + "\n\n")
	}
	if len(s.res.Proposals) == 0 {
		b.WriteString(st.Muted.Render(wrap(tr("cli.import.none"), w)) + "\n")
		b.WriteString("\n" + st.Text.Render(wrap(tr("tui.import.none_next"), w)) + "\n")
	} else {
		b.WriteString(st.Bold.Render(tr("tui.import.proposals")) + "\n")
		for i, p := range s.res.Proposals {
			line := p.Slug + "  " + p.BaseDir
			if i == s.cur {
				b.WriteString(st.Selected.Render(pad("▌"+line, w)) + "\n")
			} else {
				b.WriteString(trunc(" "+st.Text.Render(p.Slug)+"  "+st.Faint.Render(p.BaseDir), w) + "\n")
			}
		}
		p := s.res.Proposals[s.cur]
		b.WriteString("\n")
		row := func(label, value string) {
			if value != "" {
				b.WriteString(st.Label.Render(pad(label, 12)) + st.Text.Render(trunc(value, maxInt(w-12, 4))) + "\n")
			}
		}
		row(tr("tui.f.name"), p.UserName)
		row(tr("tui.f.email"), p.UserEmail)
		row(tr("tui.f.source"), p.Source)
		for _, wk := range p.Warnings {
			b.WriteString(st.Warn.Render("! "+trunc(tr(wk), maxInt(w-2, 4))) + "\n")
		}
	}
	if len(s.res.Hosts) > 0 {
		b.WriteString("\n" + st.Faint.Render(tr("cli.import.hosts")) + "\n")
		for _, h := range s.res.Hosts {
			line := strings.Join(h.Patterns, " ")
			if len(h.IdentityFile) > 0 {
				line += "  → " + strings.Join(h.IdentityFile, ", ")
			}
			b.WriteString(trunc("  "+st.Faint.Render(line), w) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

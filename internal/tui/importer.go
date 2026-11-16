package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

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
	form   *huh.Form
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
		s.form.WithWidth(maxInt(w-4, 20)).WithHeight(maxInt(h-3-errorRows, 4))
	}
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
		return []key.Binding{km.Up, km.Down, withDesc(km.Enter, "enter", tr("tui.import.pick")), km.Back}
	case imForm:
		return []key.Binding{km.Save, km.Next, km.Cancel}
	}
	return nil
}

func (s *importScreen) buildForm() {
	sh := s.sh
	var opts []huh.Option[string]
	for _, p := range platform.All() {
		opts = append(opts, huh.NewOption(p.DisplayName(), string(p)))
	}
	s.form = huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title(tr("tui.f.platform")).Options(opts...).Inline(true).Value(&s.plat),
		huh.NewInput().Title(tr("tui.f.username")).Description(tr("tui.form.username_hint")).Value(&s.user).
			Validate(sh.fieldValidator("username", true)),
		huh.NewSelect[string]().Title(tr("tui.f.auth")).
			Options(huh.NewOption("HTTPS", string(config.AuthHTTPS)), huh.NewOption("SSH", string(config.AuthSSH))).Value(&s.auth),
		huh.NewInput().Title(tr("tui.f.host")).Description(tr("tui.form.host_hint")).
			Placeholder(tr("tui.form.host_placeholder")).
			Value(&s.host).
			Validate(sh.fieldValidator("host", false)),
	)).WithTheme(sh.st.huhTheme()).WithShowHelp(false).WithShowErrors(false)
	s.form.WithWidth(maxInt(s.w-4, 20)).WithHeight(maxInt(s.h-3-errorRows, 4))
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
			return s, nil
		}
		return s, tea.Batch(changed(msg.cfg), say(levelOK, tr("tui.status.imported")), replace(newConfirmScreen(s.sh, tr("tui.confirm.title_apply"))))
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
			if msg.Type == tea.KeyEsc {
				s.phase = imList
				return s, nil
			}
		}
	}
	if s.phase == imForm && s.form != nil {
		m, cmd := s.form.Update(msg)
		s.form = m.(*huh.Form)
		if s.form.State == huh.StateCompleted {
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
		errBlock := ""
		if errs := s.form.Errors(); len(errs) > 0 {
			errBlock = st.Err.Render(wrap("✗ "+errs[0].Error(), inner))
		}
		body := fitHeight(errBlock, errorRows) + "\n" + s.form.View()
		return s.sh.panel(tr("tui.import.form_title", s.res.Proposals[s.cur].Slug), body, s.w, s.h, true)
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

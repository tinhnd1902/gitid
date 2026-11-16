package tui

import (
	"errors"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/platform"
)

// fieldValidator adapts config.ValidateField to a huh validator, so the form
// and the CLI share the same rules.
func (sh *shared) fieldValidator(field string, required bool) func(string) error {
	return func(v string) error {
		v = strings.TrimSpace(v)
		if v == "" && !required {
			return nil
		}
		if code := sh.svc.ValidateField(field, v); code != "" {
			return errors.New(tr(code))
		}
		return nil
	}
}

// errorRows is the height reserved under the title for validation errors, so
// the layout does not jump when one appears.
const errorRows = 2

// formHost wraps a huh form inside a panel with a title, an error area and
// a saving spinner. companyForm and accountForm embed it.
type formHost struct {
	sh     *shared
	form   *huh.Form
	title  string
	w, h   int
	errMsg string
	saving bool
	spin   spinner.Model
	id     int
	fixed  [][2]string // read-only (label, value) pairs shown above the form
}

func (f *formHost) initHost(sh *shared, title string) {
	f.sh, f.title = sh, title
	f.spin = sh.st.Spinner
	f.id = sh.id()
}

func (f *formHost) newForm(groups ...*huh.Group) *huh.Form {
	form := huh.NewForm(groups...).
		WithTheme(f.sh.st.huhTheme()).
		WithShowHelp(false).
		WithShowErrors(false) // errors are drawn by formHost.view, right under the title
	f.applySize(form)
	return form
}

// fixedLines is the height of the read-only header.
func (f *formHost) fixedLines() int {
	if len(f.fixed) == 0 {
		return 0
	}
	return len(f.fixed) + 2 // values, the hint line and a blank line
}

func (f *formHost) applySize(form *huh.Form) {
	if f.w > 0 {
		form.WithWidth(maxInt(f.w-4, 20)).WithHeight(maxInt(f.h-3-errorRows-f.fixedLines(), 4))
	}
}

func (f *formHost) fixedView() string {
	if len(f.fixed) == 0 {
		return ""
	}
	st := f.sh.st
	var b strings.Builder
	for _, kv := range f.fixed {
		b.WriteString(st.Label.Render(pad(kv[0], 14)) + st.Text.Render(kv[1]) + "\n")
	}
	b.WriteString(st.Faint.Render(trunc(tr("tui.form.immutable"), maxInt(f.w-6, 10))) + "\n\n")
	return b.String()
}

func (f *formHost) Resize(w, h int) {
	f.w, f.h = w, h
	if f.form != nil {
		f.applySize(f.form)
	}
}

func (f *formHost) Capturing() bool { return true }

func (f *formHost) Help() []key.Binding {
	km := f.sh.km
	if f.saving {
		return nil
	}
	return []key.Binding{km.Save, km.Next, km.Cancel}
}

func (f *formHost) view() string {
	st := f.sh.st
	var body string
	switch {
	case f.saving:
		body = "\n  " + f.spin.View() + " " + st.Muted.Render(tr("tui.form.saving"))
	default:
		msg := f.errMsg
		if errs := f.form.Errors(); len(errs) > 0 {
			msg = errs[0].Error()
		}
		errBlock := ""
		if msg != "" {
			errBlock = st.Err.Render(wrap("✗ "+msg, maxInt(f.w-6, 10)))
		}
		body = fitHeight(errBlock, errorRows) + "\n" + f.fixedView() + f.form.View()
	}
	return f.sh.panel(f.title, body, f.w, f.h, true)
}

// commit saves cfg in the background; the result is a savedMsg.
func (f *formHost) commit(cfg config.Config, after func(config.Config) Screen) tea.Cmd {
	f.saving = true
	svc, id := f.sh.svc, f.id
	return tea.Batch(f.spin.Tick, func() tea.Msg {
		saved, backup, err := svc.Commit(cfg)
		return savedMsg{id: id, cfg: saved, backup: backup, err: err, after: after}
	})
}

// handleSaved processes a savedMsg. It returns (handled, cmd, rebuild) where
// rebuild asks the caller to create a fresh form after an error.
func (f *formHost) handleSaved(msg savedMsg, okStatus string) (bool, tea.Cmd, bool) {
	if msg.id != f.id {
		return false, nil, false
	}
	f.saving = false
	if msg.err != nil {
		f.errMsg = errText(msg.err)
		return true, nil, true
	}
	cmds := []tea.Cmd{changed(msg.cfg), say(levelOK, okStatus)}
	if msg.after != nil {
		cmds = append(cmds, replace(msg.after(msg.cfg)))
	} else {
		cmds = append(cmds, replace(newConfirmScreen(f.sh, tr("tui.confirm.title_apply"))))
	}
	return true, tea.Batch(cmds...), false
}

// ---- company form ----

type companyForm struct {
	formHost
	edit    *config.Company
	slug    string
	name    string
	baseDir string
}

func newCompanyForm(sh *shared, edit *config.Company) *companyForm {
	f := &companyForm{edit: edit}
	title := tr("tui.form.company_new")
	if edit != nil {
		title = tr("tui.form.company_edit")
		f.slug, f.name, f.baseDir = edit.Slug, edit.Name, edit.BaseDir
	}
	f.initHost(sh, title)
	if edit != nil {
		f.fixed = [][2]string{{tr("tui.f.slug"), edit.Slug}}
	}
	f.form = f.build()
	return f
}

func (f *companyForm) Title() string { return f.title }

func (f *companyForm) build() *huh.Form {
	sh := f.sh
	var fields []huh.Field
	if f.edit == nil {
		fields = append(fields, huh.NewInput().
			Title(tr("tui.f.slug")).
			Description(tr("tui.form.slug_hint")).
			Value(&f.slug).
			Validate(func(v string) error {
				v = strings.TrimSpace(v)
				if err := sh.fieldValidator("slug", true)(v); err != nil {
					return err
				}
				if _, _, dup := sh.cfg.FindCompany(v); dup {
					return errors.New(tr(config.ErrSlugDuplicate))
				}
				return nil
			}))
	}
	fields = append(fields,
		huh.NewInput().
			Title(tr("tui.f.name")).
			Description(tr("tui.form.company_name_hint")).
			Value(&f.name).
			Validate(sh.fieldValidator("company_name", true)),
		huh.NewInput().
			Title(tr("tui.f.base_dir")).
			Description(tr("tui.form.base_dir_hint")).
			Placeholder(sh.svc.DefaultBaseDir(sh.cfg, "<slug>")).
			Value(&f.baseDir).
			Validate(sh.fieldValidator("base_dir", false)),
	)
	return f.newForm(huh.NewGroup(fields...))
}

func (f *companyForm) Init() tea.Cmd { return tea.Batch(f.form.Init(), f.spin.Tick) }

func (f *companyForm) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case savedMsg:
		name := strings.TrimSpace(f.slug)
		if ok, cmd, rebuild := f.handleSaved(msg, tr("tui.status.company_saved", name)); ok {
			if rebuild {
				f.form = f.build()
				return f, f.form.Init()
			}
			return f, cmd
		}
	case spinner.TickMsg:
		if f.saving {
			var cmd tea.Cmd
			f.spin, cmd = f.spin.Update(msg)
			return f, cmd
		}
		return f, nil
	case tea.KeyMsg:
		if f.saving {
			return f, nil
		}
		if msg.Type == tea.KeyEsc {
			return f, pop()
		}
	}
	if f.saving {
		return f, nil
	}
	m, cmd := f.form.Update(msg)
	f.form = m.(*huh.Form)
	if f.form.State == huh.StateCompleted {
		return f, tea.Batch(cmd, f.submit())
	}
	return f, cmd
}

func (f *companyForm) submit() tea.Cmd {
	sh := f.sh
	slug, name := strings.TrimSpace(f.slug), strings.TrimSpace(f.name)
	base := strings.TrimSpace(f.baseDir)
	if base == "" {
		base = sh.svc.DefaultBaseDir(sh.cfg, slug)
	}
	var cfg config.Config
	if f.edit != nil {
		c := *f.edit
		c.Name, c.BaseDir = name, base
		var err error
		cfg, err = app.UpdateCompany(sh.cfg, c)
		if err != nil {
			f.errMsg = errText(err)
			f.form = f.build()
			return f.form.Init()
		}
	} else {
		cfg = app.AddCompany(sh.cfg, config.Company{Slug: slug, Name: name, BaseDir: base})
	}
	return f.commit(cfg, nil)
}

func (f *companyForm) View() string { return f.view() }

// ---- account form ----

type accountForm struct {
	formHost
	companySlug string
	edit        *config.Account

	plat, auth                string
	username, name, email     string
	host, orgs, sshHost, port string
	makeDefault, genKey       bool
}

func newAccountForm(sh *shared, slug string, edit *config.Account) *accountForm {
	f := &accountForm{companySlug: slug, edit: edit, plat: string(platform.GitHub), auth: string(config.AuthHTTPS)}
	title := tr("tui.form.account_new", slug)
	if edit != nil {
		title = tr("tui.form.account_edit", slug+"/"+edit.ID)
		f.plat, f.auth = string(edit.Platform), string(edit.Auth)
		f.username, f.name, f.email = edit.Username, edit.Name, edit.Email
		f.host, f.sshHost = edit.Host, edit.SSHHost
		f.orgs = strings.Join(edit.Orgs, ", ")
		if edit.SSHPort > 0 {
			f.port = strconv.Itoa(edit.SSHPort)
		}
		if c, _, ok := sh.cfg.FindCompany(slug); ok {
			f.makeDefault = c.DefaultAccount == edit.ID
		}
	} else if c, _, ok := sh.cfg.FindCompany(slug); ok {
		f.makeDefault = len(c.Accounts) == 0
		// a new account of a company with accounts inherits their git name
		if len(c.Accounts) > 0 {
			f.name = c.Accounts[0].Name
		}
	}
	f.initHost(sh, title)
	if edit != nil {
		f.fixed = [][2]string{{tr("tui.f.id"), edit.ID}}
	}
	f.form = f.build()
	return f
}

func (f *accountForm) Title() string { return f.title }

func (f *accountForm) hasKey() bool { return f.edit != nil && f.edit.SSHKey != "" }

func (f *accountForm) build() *huh.Form {
	sh := f.sh
	var platOpts []huh.Option[string]
	for _, p := range platform.All() {
		platOpts = append(platOpts, huh.NewOption(p.DisplayName(), string(p)))
	}
	main := []huh.Field{}
	main = append(main,
		huh.NewSelect[string]().Title(tr("tui.f.platform")).Options(platOpts...).Inline(true).Value(&f.plat),
		huh.NewInput().Title(tr("tui.f.username")).Description(tr("tui.form.username_hint")).Value(&f.username).
			Validate(sh.fieldValidator("username", true)),
		huh.NewInput().Title(tr("tui.f.git_name")).Description(tr("tui.form.git_name_hint")).Value(&f.name).
			Validate(sh.fieldValidator("name", true)),
		huh.NewInput().Title(tr("tui.f.email")).Value(&f.email).
			Validate(sh.fieldValidator("email", true)),
		huh.NewSelect[string]().Title(tr("tui.f.auth")).
			Options(huh.NewOption("HTTPS ("+tr("tui.form.auth_https")+")", string(config.AuthHTTPS)),
				huh.NewOption("SSH ("+tr("tui.form.auth_ssh")+")", string(config.AuthSSH))).
			Value(&f.auth),
	)
	advanced := []huh.Field{
		huh.NewInput().Title(tr("tui.f.host")).Description(tr("tui.form.host_hint")).
			Placeholder(tr("tui.form.host_placeholder")).
			Value(&f.host).
			Validate(func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" && platform.DefaultHost(platform.Platform(f.plat)) == "" {
					return errors.New(tr(config.ErrHostRequired))
				}
				return sh.fieldValidator("host", false)(v)
			}),
		huh.NewInput().Title(tr("tui.f.orgs")).Description(tr("tui.form.orgs_hint")).Value(&f.orgs).
			Validate(func(v string) error {
				for _, o := range splitList(v) {
					if code := sh.svc.ValidateField("org", o); code != "" {
						return errors.New(tr(code))
					}
				}
				return nil
			}),
		huh.NewInput().Title(tr("tui.f.ssh_host")).Description(tr("tui.form.ssh_host_hint")).Value(&f.sshHost).
			Validate(sh.fieldValidator("ssh_host", false)),
		huh.NewInput().Title(tr("tui.f.ssh_port")).Value(&f.port).
			Validate(sh.fieldValidator("ssh_port", false)),
		huh.NewConfirm().Title(tr("tui.f.make_default")).Value(&f.makeDefault).
			Affirmative(tr("tui.yes")).Negative(tr("tui.no")),
	}
	groups := []*huh.Group{
		huh.NewGroup(main...).Title(tr("tui.form.group_main")),
		huh.NewGroup(advanced...).Title(tr("tui.form.group_advanced")),
	}
	if !f.hasKey() {
		groups = append(groups, huh.NewGroup(
			huh.NewConfirm().Title(tr("tui.form.gen_key")).Description(tr("tui.form.gen_key_hint")).Value(&f.genKey).
				Affirmative(tr("tui.yes")).Negative(tr("tui.no")),
		).WithHideFunc(func() bool { return f.auth != string(config.AuthSSH) }))
	}
	return f.newForm(groups...)
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (f *accountForm) Init() tea.Cmd { return tea.Batch(f.form.Init(), f.spin.Tick) }

func (f *accountForm) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case savedMsg:
		if ok, cmd, rebuild := f.handleSaved(msg, tr("tui.status.account_saved", f.companySlug+"/"+f.accountID())); ok {
			if rebuild {
				f.form = f.build()
				return f, f.form.Init()
			}
			return f, cmd
		}
	case spinner.TickMsg:
		if f.saving {
			var cmd tea.Cmd
			f.spin, cmd = f.spin.Update(msg)
			return f, cmd
		}
		return f, nil
	case tea.KeyMsg:
		if f.saving {
			return f, nil
		}
		if msg.Type == tea.KeyEsc {
			return f, pop()
		}
	}
	if f.saving {
		return f, nil
	}
	m, cmd := f.form.Update(msg)
	f.form = m.(*huh.Form)
	if f.form.State == huh.StateCompleted {
		return f, tea.Batch(cmd, f.submit())
	}
	return f, cmd
}

// accountID is the id the account has (or will get).
func (f *accountForm) accountID() string {
	if f.edit != nil {
		return f.edit.ID
	}
	c, _, ok := f.sh.cfg.FindCompany(f.companySlug)
	if !ok {
		return strings.ToLower(strings.TrimSpace(f.username))
	}
	return config.DefaultAccountID(c, platform.Platform(f.plat), strings.TrimSpace(f.username))
}

func (f *accountForm) submit() tea.Cmd {
	sh := f.sh
	comp, ci, ok := sh.cfg.FindCompany(f.companySlug)
	if !ok {
		f.errMsg = errText(config.ErrRefNotFound)
		f.form = f.build()
		return f.form.Init()
	}
	acc := config.Account{
		ID:       f.accountID(),
		Platform: platform.Platform(f.plat),
		Host:     strings.TrimSpace(f.host),
		SSHHost:  strings.TrimSpace(f.sshHost),
		Username: strings.TrimSpace(f.username),
		Name:     strings.TrimSpace(f.name),
		Email:    strings.TrimSpace(f.email),
		Auth:     config.Auth(f.auth),
		Orgs:     splitList(f.orgs),
	}
	if acc.Host == platform.DefaultHost(acc.Platform) {
		acc.Host = ""
	}
	if n, err := strconv.Atoi(strings.TrimSpace(f.port)); err == nil {
		acc.SSHPort = n
	}
	if f.edit != nil {
		acc.SSHKey = f.edit.SSHKey
	}
	cfg, err := app.UpsertAccount(sh.cfg, f.companySlug, acc)
	if err != nil {
		f.errMsg = errText(err)
		f.form = f.build()
		return f.form.Init()
	}
	if f.makeDefault {
		cfg.Companies[ci].DefaultAccount = acc.ID
	} else if comp.DefaultAccount == acc.ID && f.edit != nil {
		// the user cleared "default": fall back to the first other account
		for _, a := range cfg.Companies[ci].Accounts {
			if a.ID != acc.ID {
				cfg.Companies[ci].DefaultAccount = a.ID
				break
			}
		}
	}
	var after func(config.Config) Screen
	if f.genKey && acc.Auth == config.AuthSSH && acc.SSHKey == "" {
		slug, id := f.companySlug, acc.ID
		after = func(saved config.Config) Screen {
			if ref, err := saved.ParseRef(slug + "/" + id); err == nil {
				return newKeygenScreen(sh, ref)
			}
			return newConfirmScreen(sh, tr("tui.confirm.title_apply"))
		}
	}
	return f.commit(cfg, after)
}

func (f *accountForm) View() string { return f.view() }

package tui

import (
	"errors"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/platform"
)

// fieldValidator adapts config.ValidateField to a huh validator, so the form
// and the CLI share the same rules. An empty required field says what to do
// ("Please enter <label>") instead of quoting the format rule.
func (sh *shared) fieldValidator(field, label string, required bool) func(string) error {
	return func(v string) error {
		v = strings.TrimSpace(v)
		if v == "" && !required {
			return nil
		}
		if v == "" {
			return errors.New(tr("tui.form.err.required", label))
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
	// preview, when set, draws a live block between the error area and the
	// form. It is called from View, on the Update goroutine, with the usable
	// width and the number of rows it may fill. It must read the form state
	// only; huh's DescriptionFunc runs in another goroutine and is not used
	// for this.
	preview func(w, rows int) []string
	// previewHead, when set, is the height the first part of the preview
	// needs at width w (a remote that wraps); 1 when unset or when the body
	// is too short to spend more.
	previewHead func(w int) int
	// hints are the descriptions that must be wrapped here (see hintText);
	// they are set again whenever the width changes.
	hints []hintSlot
	// sizedRows is the preview height the form height was computed with.
	sizedRows int
}

// hintSlot is a form description kept word-wrapped for the current width.
type hintSlot struct {
	text string
	set  func(string)
}

// addHint registers a description: set receives the text wrapped for the
// width of the form (the text itself while the width is unknown).
func (f *formHost) addHint(text string, set func(string)) string {
	f.hints = append(f.hints, hintSlot{text: text, set: set})
	return text
}

// hintWidth is the width huh gives a description: the form width less the
// frame (border and padding) of a field.
func (f *formHost) hintWidth() int {
	return maxInt(f.w-4, 20) - 2
}

func (f *formHost) applyHints() {
	if f.w <= 0 {
		return
	}
	for _, h := range f.hints {
		h.set(hintText(h.text, f.hintWidth()))
	}
}

// Preview block height: the remote line, plus up to two lines of notes when
// the body is tall enough; the remote line alone below previewMinBody rows.
const (
	previewNoteRows = 2
	previewMinBody  = 18
	previewWrapMin  = 12 // below this body height the remote stays on one line
	previewHeadMax  = 3
)

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

// previewRows is the height reserved for the preview block (0 without one).
func (f *formHost) previewRows() int {
	switch {
	case f.preview == nil:
		return 0
	}
	head := 1
	if f.previewHead != nil && f.h >= previewWrapMin {
		head = clamp(f.previewHead(f.previewWidth()), 1, previewHeadMax)
	}
	if f.h < previewMinBody {
		return head
	}
	return head + previewNoteRows
}

func (f *formHost) previewWidth() int { return maxInt(f.w-6, 10) }

func (f *formHost) applySize(form *huh.Form) {
	if f.w > 0 {
		f.applyHints()
		f.sizedRows = f.previewRows()
		form.WithWidth(maxInt(f.w-4, 20)).WithHeight(maxInt(f.h-3-errorRows-f.fixedLines()-f.previewRows(), 4))
	}
}

// previewView renders the preview block, always previewRows lines high.
func (f *formHost) previewView() string {
	n := f.previewRows()
	if n == 0 {
		return ""
	}
	return fitHeight(joinLines(f.preview(f.previewWidth(), n)), n) + "\n"
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
	changed := f.w != w || f.h != h
	had := f.w > 0
	f.w, f.h = w, h
	if f.form != nil {
		f.applySize(f.form)
		if changed && had {
			// huh wraps the hints when it handles a message, not when the
			// size changes: push one through so nothing stays cut.
			f.form.Update(tea.WindowSizeMsg{Width: w, Height: h})
		}
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

// ContextHelp makes "enter" the next step of every form.
func (f *formHost) ContextHelp() []helpItem {
	var out []helpItem
	for i, b := range f.Help() {
		out = append(out, helpItem{b: b, primary: i == 0})
	}
	return out
}

// shownError is the message drawn in the error area: the first validation
// error of the form, or the error of the last save.
func (f *formHost) shownError() string {
	if errs := f.form.Errors(); len(errs) > 0 {
		return errs[0].Error()
	}
	return f.errMsg
}

func (f *formHost) view() string {
	st := f.sh.st
	var body string
	switch {
	case f.saving:
		body = "\n  " + f.spin.View() + " " + st.Muted.Render(tr("tui.form.saving"))
	default:
		msg := f.shownError()
		errBlock := ""
		if msg != "" {
			errBlock = st.Err.Render(wrap("✗ "+msg, maxInt(f.w-6, 10)))
		}
		if f.previewRows() != f.sizedRows {
			// the remote wrapped (or stopped wrapping) while typing
			f.applySize(f.form)
		}
		body = fitHeight(errBlock, errorRows) + "\n" + f.previewView() + f.fixedView() + f.form.View()
	}
	return f.sh.panel(f.title, body, f.w, f.h, true)
}

// commit saves cfg in the background; the result is a savedMsg.
func (f *formHost) commit(cfg config.Config, after func(config.Config) Screen) tea.Cmd {
	f.saving = true
	svc, id, base := f.sh.svc, f.id, f.sh.cfg
	return tea.Batch(f.spin.Tick, func() tea.Msg {
		// Never overwrite what another gitid window saved in the meantime.
		if disk, stale, err := svc.ChangedSince(base); err != nil {
			return savedMsg{id: id, err: err}
		} else if stale {
			return savedMsg{id: id, cfg: disk, err: app.ErrConfigChanged}
		}
		saved, backup, err := svc.Commit(cfg)
		return savedMsg{id: id, cfg: saved, backup: backup, err: err, after: after}
	})
}

// handleSaved processes a savedMsg. It returns (handled, cmd, rebuild) where
// rebuild asks the caller to create a fresh form after an error (cmd may then
// still carry a command to run, such as reloading a stale configuration). ok
// builds the command to run after a successful save.
func (f *formHost) handleSaved(msg savedMsg, ok func(config.Config) tea.Cmd) (bool, tea.Cmd, bool) {
	if msg.id != f.id {
		return false, nil, false
	}
	f.saving = false
	if msg.err != nil {
		f.errMsg = errText(msg.err)
		if errors.Is(msg.err, app.ErrConfigChanged) {
			// show the configuration found on disk; the form keeps its values
			return true, changed(msg.cfg), true
		}
		return true, nil, true
	}
	return true, ok(msg.cfg), false
}

// reviewNext is the usual follow-up of a save: publish the new configuration,
// say what was saved, then open the confirm screen (or msg.after, for
// example the key generation). The steps run in order, so the confirm screen
// always plans against the saved configuration.
func (f *formHost) reviewNext(msg savedMsg, status statusMsg, focusKey string) tea.Cmd {
	next := Screen(newConfirmScreen(f.sh, tr("tui.confirm.title_apply")))
	if msg.after != nil {
		next = msg.after(msg.cfg)
	}
	steps := []tea.Cmd{changed(msg.cfg)}
	if focusKey != "" {
		steps = append(steps, focus(focusKey))
	}
	steps = append(steps, func() tea.Msg { return status }, replace(next))
	return tea.Sequence(steps...)
}

// ---- company form ----

type companyForm struct {
	formHost
	edit    *config.Company
	slug    string
	name    string
	baseDir string
	// slugSnap is the trimmed slug as of the last Update. huh evaluates a
	// DescriptionFunc in another goroutine, which must not read f.slug (the
	// input writes it from the Update goroutine); it reads this snapshot.
	slugSnap atomic.Value // string
}

func newCompanyForm(sh *shared, edit *config.Company) *companyForm {
	f := &companyForm{edit: edit}
	title := tr("tui.form.company_new")
	if edit != nil {
		title = tr("tui.form.company_edit")
		f.slug, f.name, f.baseDir = edit.Slug, edit.Name, edit.BaseDir
	}
	f.slugSnap.Store(strings.TrimSpace(f.slug))
	f.initHost(sh, title)
	if edit != nil {
		f.fixed = [][2]string{{tr("tui.f.slug"), edit.Slug}}
	}
	f.form = f.build()
	return f
}

func (f *companyForm) Title() string { return f.title }

// baseDirDesc is the description of the base directory field: what it is and
// what an empty value means for the given slug.
func baseDirDesc(svc *app.Service, cfg config.Config, slug string) string {
	if slug == "" {
		slug = "<slug>"
	}
	return tr("tui.form.base_dir_hint") + "\n" + tr("tui.form.base_dir_default", svc.DefaultBaseDir(cfg, slug))
}

func (f *companyForm) build() *huh.Form {
	sh := f.sh
	var fields []huh.Field
	if f.edit == nil {
		fields = append(fields, huh.NewInput().
			Title(tr("tui.f.slug")).
			Description(tr("tui.form.slug_hint")).
			Placeholder(tr("tui.form.eg", "acme")).
			Value(&f.slug).
			Validate(func(v string) error {
				v = strings.TrimSpace(v)
				if err := sh.fieldValidator("slug", tr("tui.f.slug"), true)(v); err != nil {
					return err
				}
				if _, _, dup := sh.cfg.FindCompany(v); dup {
					return errors.New(tr("tui.form.err.slug_taken", v))
				}
				return nil
			}))
	}
	base := huh.NewInput().
		Title(tr("tui.f.base_dir")).
		Value(&f.baseDir).
		Validate(sh.fieldValidator("base_dir", tr("tui.f.base_dir"), false))
	if f.edit != nil {
		base.Description(baseDirDesc(sh.svc, sh.cfg, f.edit.Slug))
	} else {
		// "Leave empty for <default>" follows the slug as it is typed. The
		// closure runs in another goroutine: it only reads the atomic snapshot
		// and a copy of the settings (no slices shared with the shared config).
		cfg := config.Config{Settings: sh.cfg.Settings}
		svc := sh.svc
		snap := &f.slugSnap
		base.Description(baseDirDesc(svc, cfg, "")).
			DescriptionFunc(func() string {
				slug, _ := snap.Load().(string)
				return baseDirDesc(svc, cfg, slug)
			}, &f.slug)
	}
	fields = append(fields,
		huh.NewInput().
			Title(tr("tui.f.name")).
			Description(tr("tui.form.company_name_hint")).
			Placeholder(tr("tui.form.eg", "ACME Corp")).
			Value(&f.name).
			Validate(sh.fieldValidator("company_name", tr("tui.f.name"), true)),
		base,
	)
	return f.newForm(huh.NewGroup(fields...))
}

func (f *companyForm) Init() tea.Cmd { return tea.Batch(f.form.Init(), f.spin.Tick) }

func (f *companyForm) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case savedMsg:
		name := strings.TrimSpace(f.slug)
		next := func(cfg config.Config) tea.Cmd {
			if f.edit == nil {
				// A new company has no account yet, so there is nothing to
				// apply: go back to the list, select it and say what is next.
				if c, _, found := cfg.FindCompany(name); found && len(c.Accounts) == 0 {
					return tea.Sequence(changed(cfg), pop(), focus(name), hint(tr("tui.status.company_created", name)))
				}
			}
			return f.reviewNext(msg, statusMsg{level: levelOK, text: tr("tui.status.company_saved", name)}, "")
		}
		if ok, cmd, rebuild := f.handleSaved(msg, next); ok {
			if rebuild {
				f.form = f.build()
				return f, tea.Batch(f.form.Init(), cmd)
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
			return f, f.sh.popNow()
		}
	}
	if f.saving {
		return f, nil
	}
	m, cmd := f.form.Update(msg)
	f.form = m.(*huh.Form)
	f.slugSnap.Store(strings.TrimSpace(f.slug))
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
	showAdvanced              bool
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
		// settings that were customized are shown, not hidden
		f.showAdvanced = edit.Host != "" || edit.SSHHost != "" || edit.SSHPort > 0 || len(edit.Orgs) > 0
	} else if c, _, ok := sh.cfg.FindCompany(slug); ok {
		f.makeDefault = len(c.Accounts) == 0
		// a new account of a company with accounts inherits their git name
		if len(c.Accounts) > 0 {
			f.name = c.Accounts[0].Name
		}
	}
	f.initHost(sh, title)
	f.preview = f.previewLines
	f.previewHead = f.remoteRows
	if edit != nil {
		f.fixed = [][2]string{{tr("tui.f.id"), edit.ID}}
	}
	f.form = f.build()
	return f
}

func (f *accountForm) Title() string { return f.title }

func (f *accountForm) hasKey() bool { return f.edit != nil && f.edit.SSHKey != "" }

// editID is the id of the account being edited ("" for a new one).
func (f *accountForm) editID() string {
	if f.edit != nil {
		return f.edit.ID
	}
	return ""
}

// company is the company the account belongs to.
func (f *accountForm) company() config.Company {
	if c, _, ok := f.sh.cfg.FindCompany(f.companySlug); ok {
		return c
	}
	return config.Company{Slug: f.companySlug}
}

// hasOtherAccounts reports whether the company has an account besides the one
// being edited: only then "make default" is a real choice (the first account
// is always the default).
func (f *accountForm) hasOtherAccounts() bool {
	for _, a := range f.company().Accounts {
		if a.ID != f.editID() {
			return true
		}
	}
	return false
}

// candidate is the account the form describes as far as its login goes.
func (f *accountForm) candidate() config.Account {
	return config.Account{
		ID:       f.accountID(),
		Platform: platform.Platform(f.plat),
		Host:     strings.TrimSpace(f.host),
		Username: strings.TrimSpace(f.username),
		Auth:     config.Auth(f.auth),
	}
}

// loginError is the reason the login of the form cannot be saved: an email
// where a username belongs, or an account of the company that already signs
// in as the same user. It returns "" when there is none.
func (f *accountForm) loginError() string {
	c := f.candidate()
	if dup, ok := dupInCompany(f.company(), c, f.editID()); ok {
		return tr("tui.form.err.dup_account", dup.ID, c.EffectiveHost())
	}
	if looksLikeEmail(c.Platform, c.Username) {
		return tr("tui.form.err.username_email")
	}
	return ""
}

// previewLines is the live block above the form: the remote the account will
// produce, and one note (first match wins): a duplicate, an email typed as a
// username, the same login in another company, a changed username, or the id.
func (f *accountForm) previewLines(w, rows int) []string {
	st := f.sh.st
	acc := f.candidate()
	var lines []string
	for _, l := range f.remoteLines(w, rows) {
		lines = append(lines, st.Muted.Render(l))
	}
	if rows <= len(lines) {
		return lines
	}
	var note string
	style := st.Faint
	loginErr := f.loginError()
	var elsewhere []string
	if acc.Username != "" {
		elsewhere = loginElsewhere(f.sh.cfg, f.companySlug, acc)
	}
	switch {
	case loginErr != "" && loginErr == f.shownError():
		// the error area already says it; do not repeat it
	case loginErr != "":
		note, style = loginErr, st.Err
	case len(elsewhere) > 0:
		note, style = tr("tui.form.warn.dup_other", strings.Join(elsewhere, ", ")), st.Warn
	case f.edit != nil && acc.Username != "" && acc.Username != f.edit.Username:
		note, style = tr("tui.form.warn.username_changed", f.edit.Username), st.Warn
	case acc.Username != "" && f.edit != nil:
		note = tr("tui.form.preview_id_fixed", acc.ID)
	case acc.Username != "":
		note = tr("tui.form.preview_id", acc.ID)
	}
	if note == "" {
		return lines
	}
	noteLines := wrapLines(note, w)
	if left := rows - len(lines); len(noteLines) > left {
		noteLines = noteLines[:left]
	}
	for _, l := range noteLines {
		lines = append(lines, style.Render(l))
	}
	return lines
}

// remoteLines is the "Remote will be" line, wrapped so the URL is shown whole
// (on its own line when it does not fit after the label). Only a body too
// short to spare the rows cuts it to one line.
func (f *accountForm) remoteLines(w, rows int) []string {
	acc := f.candidate()
	if acc.Username == "" {
		acc.Username, acc.ID = sentUser, sentUser
	}
	example := remoteExample(config.AccountRef{Company: f.company(), Account: acc})
	example = strings.ReplaceAll(example, sentUser, "<username>")
	text := tr("tui.form.preview_remote", example)
	if f.h < previewWrapMin {
		return []string{trunc(text, w)}
	}
	lines := wrapUnits(text, w)
	if len(lines) > previewHeadMax {
		lines = lines[:previewHeadMax]
	}
	return lines
}

// remoteRows is how many rows the remote line needs at width w.
func (f *accountForm) remoteRows(w int) int { return len(f.remoteLines(w, 0)) }

// sentUser stands in for an empty username while the example is rendered.
const sentUser = "GITIDUSER"

func (f *accountForm) build() *huh.Form {
	sh := f.sh
	var platOpts []huh.Option[string]
	for _, p := range platform.All() {
		platOpts = append(platOpts, huh.NewOption(p.DisplayName(), string(p)))
	}
	userRequired := sh.fieldValidator("username", tr("tui.f.username"), true)
	f.hints = nil
	userIn, emailIn := huh.NewInput(), huh.NewInput()
	main := []huh.Field{}
	main = append(main,
		huh.NewSelect[string]().Title(tr("tui.f.platform")).Description(tr("tui.form.platform_hint")).
			Options(platOpts...).Inline(true).Value(&f.plat),
		userIn.Title(tr("tui.f.username")).Description(f.addHint(tr("tui.form.username_hint"), func(s string) { userIn.Description(s) })).
			Placeholder(tr("tui.form.eg", "alice")).Value(&f.username).
			Validate(func(v string) error {
				if err := userRequired(v); err != nil {
					return err
				}
				if msg := f.loginError(); msg != "" {
					return errors.New(msg)
				}
				return nil
			}),
		huh.NewInput().Title(tr("tui.f.git_name")).Description(tr("tui.form.git_name_hint")).
			Placeholder(tr("tui.form.eg", "Alice Tran")).Value(&f.name).
			Validate(sh.fieldValidator("name", tr("tui.f.git_name"), true)),
		emailIn.Title(tr("tui.f.email")).Description(f.addHint(tr("tui.form.email_hint"), func(s string) { emailIn.Description(s) })).
			Placeholder(tr("tui.form.eg", "alice@acme.com")).Value(&f.email).
			Validate(sh.fieldValidator("email", tr("tui.f.email"), true)),
		huh.NewSelect[string]().Title(tr("tui.f.auth")).Description(tr("tui.form.auth_hint")).
			Options(huh.NewOption("HTTPS ("+tr("tui.form.auth_https")+")", string(config.AuthHTTPS)),
				huh.NewOption("SSH ("+tr("tui.form.auth_ssh")+")", string(config.AuthSSH))).
			Value(&f.auth),
		huh.NewConfirm().Title(tr("tui.form.show_advanced")).Description(tr("tui.form.advanced_hint")).
			Value(&f.showAdvanced).Affirmative(tr("tui.yes")).Negative(tr("tui.no")),
	)
	advanced := []huh.Field{
		huh.NewInput().Title(tr("tui.f.host")).Description(tr("tui.form.host_hint")).
			Placeholder(tr("tui.form.eg", "git.acme.com")).
			Value(&f.host).
			Validate(func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" && platform.DefaultHost(platform.Platform(f.plat)) == "" {
					return errors.New(tr(config.ErrHostRequired))
				}
				return sh.fieldValidator("host", tr("tui.f.host"), false)(v)
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
			Validate(sh.fieldValidator("ssh_host", tr("tui.f.ssh_host"), false)),
		huh.NewInput().Title(tr("tui.f.ssh_port")).Value(&f.port).
			Validate(sh.fieldValidator("ssh_port", tr("tui.f.ssh_port"), false)),
	}
	// The hide funcs run in Form.Update, on the Update goroutine, like the
	// validators: they may read the form fields.
	groups := []*huh.Group{
		huh.NewGroup(main...).Title(tr("tui.form.group_main")),
		huh.NewGroup(advanced...).Title(tr("tui.form.group_advanced")).
			WithHideFunc(func() bool { return !f.showAdvanced && f.plat != string(platform.Gitea) }),
		huh.NewGroup(
			huh.NewConfirm().Title(tr("tui.f.make_default")).Value(&f.makeDefault).
				Affirmative(tr("tui.yes")).Negative(tr("tui.no")),
		).WithHideFunc(func() bool { return !f.hasOtherAccounts() }),
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
		next := func(config.Config) tea.Cmd {
			ref := f.companySlug + "/" + f.accountID()
			text := tr("tui.status.account_saved", ref)
			if f.edit != nil && strings.TrimSpace(f.username) != f.edit.Username {
				// repos cloned with the old username keep pointing at it
				text = tr("tui.status.account_saved_relogin", ref, f.edit.Username)
			}
			return f.reviewNext(msg, statusMsg{level: levelOK, text: text, ttl: hintTTL}, ref)
		}
		if ok, cmd, rebuild := f.handleSaved(msg, next); ok {
			if rebuild {
				f.form = f.build()
				return f, tea.Batch(f.form.Init(), cmd)
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
			return f, f.sh.popNow()
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

// fail shows msg in the error area and starts the form over with its values.
func (f *accountForm) fail(msg string) tea.Cmd {
	f.errMsg = msg
	f.form = f.build()
	return f.form.Init()
}

func (f *accountForm) submit() tea.Cmd {
	sh := f.sh
	comp, ci, ok := sh.cfg.FindCompany(f.companySlug)
	if !ok {
		return f.fail(errText(config.ErrRefNotFound))
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
	// The username validator saw the host as it was then; the host (advanced
	// settings) and the platform may have changed since.
	if msg := f.loginError(); msg != "" {
		return f.fail(msg)
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
		return f.fail(errText(err))
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

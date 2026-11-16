package tui

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/keys"
)

const (
	wideWidth = 100 // from this width the detail pane sits next to the list
	idCol     = 14  // width of the account id column
)

// rowItem is one row of the company / account tree.
type rowItem struct {
	company config.Company
	account *config.Account // nil for a company row
	last    bool            // last account of its company
}

func (r rowItem) isCompany() bool { return r.account == nil }

func (r rowItem) ref() (config.AccountRef, bool) {
	if r.account == nil {
		return config.AccountRef{}, false
	}
	return config.AccountRef{Company: r.company, Account: *r.account}, true
}

// key identifies the row across rebuilds.
func (r rowItem) key() string {
	if r.account == nil {
		return r.company.Slug
	}
	return r.company.Slug + "/" + r.account.ID
}

// FilterValue feeds the fuzzy filter: every field a user may search by.
func (r rowItem) FilterValue() string {
	if r.account == nil {
		return strings.Join([]string{r.company.Slug, r.company.Name, r.company.BaseDir}, " ")
	}
	a := r.account
	return strings.Join([]string{
		r.key(), r.company.Slug, r.company.Name, a.ID, a.Username, a.Email, a.Name,
		a.EffectiveHost(), string(a.Platform), string(a.Auth),
	}, " ")
}

// treeFilter is a fuzzy filter that matches inside single words (so that
// "beta" does not match letters scattered over unrelated fields) and keeps
// the tree order instead of sorting by score.
func treeFilter(term string, targets []string) []list.Rank {
	words := strings.Fields(strings.ToLower(term))
	if len(words) == 0 {
		return nil
	}
	var out []list.Rank
	for i, t := range targets {
		toks := strings.Fields(strings.ToLower(t))
		ok := true
		for _, w := range words {
			if len(fuzzy.Find(w, toks)) == 0 {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, list.Rank{Index: i})
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Index < out[b].Index })
	return out
}

// rowDelegate renders the tree rows.
type rowDelegate struct{ sh *shared }

func (d rowDelegate) Height() int                         { return 1 }
func (d rowDelegate) Spacing() int                        { return 0 }
func (d rowDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d rowDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	r, ok := item.(rowItem)
	if !ok {
		return
	}
	width := m.Width()
	selected := index == m.Index()
	fmt.Fprint(w, d.line(r, width, selected))
}

func authTag(a config.Auth) string {
	if a == config.AuthSSH {
		return "ssh"
	}
	return "https"
}

func (d rowDelegate) line(r rowItem, width int, selected bool) string {
	st := d.sh.st
	if r.isCompany() {
		name := ""
		if r.company.Name != "" && r.company.Name != r.company.Slug {
			name = "  " + r.company.Name
		}
		count := fmt.Sprintf("%d", len(r.company.Accounts))
		left := r.company.Slug + name
		right := r.company.BaseDir + "  " + count
		if selected {
			return st.Selected.Render(pad("▌ "+left, maxInt(width-lipgloss.Width(right)-1, 4)) + " " + right)
		}
		l := st.Accent.Bold(true).Render("▪ "+r.company.Slug) + st.Muted.Render(name)
		rr := st.Faint.Render(r.company.BaseDir) + "  " + st.Muted.Render(count)
		return joinEnds(l, rr, width)
	}
	a := r.account
	branch := "├─"
	if r.last {
		branch = "└─"
	}
	star := " "
	if r.company.DefaultAccount == a.ID {
		star = "★"
	}
	warn := ""
	if a.Auth == config.AuthSSH && a.SSHKey == "" {
		warn = " !"
	}
	tag := platformTag(a.Platform)
	mid := fmt.Sprintf("%s %s  %s %s", star, pad(a.ID, idCol), tag, authTag(a.Auth))
	email := a.Email
	if selected {
		left := branch + " " + mid + warn
		return st.Selected.Render(pad("▌"+left, maxInt(width-lipgloss.Width(email)-1, 4)) + " " + email)
	}
	starS := " "
	if star != " " {
		starS = st.Warn.Render("★")
	}
	l := st.Faint.Render(" "+branch+" ") + starS + " " + st.Text.Render(pad(a.ID, idCol)) + "  " + st.platformBadge(a.Platform) + " " + st.Muted.Render(authTag(a.Auth))
	if warn != "" {
		l += st.Warn.Render(warn)
	}
	return joinEnds(l, st.Faint.Render(email), width)
}

// joinEnds puts left and right at the two ends of a width-cell line. The
// right part is dropped first when there is no room.
func joinEnds(left, right string, width int) string {
	lw, rw := lipgloss.Width(left), lipgloss.Width(right)
	if lw+rw+2 > width {
		right, rw = "", 0
	}
	if lw > width {
		return trunc(left, width)
	}
	return left + spaces(width-lw-rw) + right
}

// listScreen is the home screen: the company / account tree with a detail
// pane.
//
// Filtering is done here, synchronously, instead of by the list component:
// the bubbles list filters in a command whose results can arrive out of order
// (a stale result for "b" overwriting the result for "beta"). With a few
// hundred rows the fuzzy match takes microseconds, so there is nothing to
// gain from doing it in the background.
type listScreen struct {
	sh      *shared
	l       list.Model
	all     []list.Item // every row, unfiltered
	filter  textinput.Model
	mode    filterMode
	w, h    int
	keyInfo keys.Info
	keyFor  string
}

type filterMode int

const (
	filterOff filterMode = iota
	filterTyping
	filterApplied
)

func newListScreen(sh *shared) *listScreen {
	l := list.New(nil, rowDelegate{sh}, 10, 10)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowFilter(false)
	l.SetFilteringEnabled(false)
	l.SetShowPagination(false)
	l.DisableQuitKeybindings()
	l.KeyMap.CursorUp = key.NewBinding(key.WithKeys("up", "ctrl+p"))
	l.KeyMap.CursorDown = key.NewBinding(key.WithKeys("down", "ctrl+n"))
	l.KeyMap.GoToStart = key.NewBinding(key.WithKeys("home"))
	l.KeyMap.GoToEnd = key.NewBinding(key.WithKeys("end"))
	l.KeyMap.NextPage = key.NewBinding(key.WithKeys("pgdown", "ctrl+f"))
	l.KeyMap.PrevPage = key.NewBinding(key.WithKeys("pgup", "ctrl+b"))
	l.KeyMap.ShowFullHelp.SetEnabled(false)
	l.KeyMap.CloseFullHelp.SetEnabled(false)
	l.Styles.NoItems = sh.st.Muted
	fi := textinput.New()
	fi.Prompt = "/ "
	fi.PromptStyle = sh.st.Accent
	fi.Cursor.Style = sh.st.Accent
	fi.CharLimit = 64
	return &listScreen{sh: sh, l: l, filter: fi}
}

func (s *listScreen) Title() string { return tr("tui.list.title") }
func (s *listScreen) Init() tea.Cmd { return nil }

func (s *listScreen) Capturing() bool { return s.mode == filterTyping }

func (s *listScreen) Help() []key.Binding {
	km := s.sh.km
	return []key.Binding{km.Filter, km.AddAccount, km.AddCompany, km.Edit, km.Delete, km.Apply, km.Clone, km.Check, km.Doctor, km.GenKey, km.Import, km.Detail, km.Quit}
}

func (s *listScreen) Resize(w, h int) {
	s.w, s.h = w, h
	lw, _ := s.widths()
	s.filter.Width = maxInt(lw-10, 8)
	s.l.SetSize(maxInt(lw-4, 4), maxInt(h-4, 1)) // width: border+padding; height: border(2) + title(1) + filter line(1)
}

func (s *listScreen) wide() bool { return s.w >= wideWidth }

func (s *listScreen) widths() (list, detail int) {
	if !s.wide() {
		return s.w, 0
	}
	lw := s.w * 55 / 100
	return lw, s.w - lw
}

// rows flattens the configuration into tree rows.
func rowsOf(cfg config.Config) []list.Item {
	var items []list.Item
	for _, c := range cfg.Companies {
		items = append(items, rowItem{company: c})
		for i := range c.Accounts {
			a := c.Accounts[i]
			items = append(items, rowItem{company: c, account: &a, last: i == len(c.Accounts)-1})
		}
	}
	return items
}

func (s *listScreen) selected() (rowItem, bool) {
	it := s.l.SelectedItem()
	if it == nil {
		return rowItem{}, false
	}
	r, ok := it.(rowItem)
	return r, ok
}

// rebuild reloads the rows from the shared configuration, keeping the
// selection when the row still exists.
func (s *listScreen) rebuild() tea.Cmd {
	prev := ""
	if r, ok := s.selected(); ok {
		prev = r.key()
	}
	s.all = rowsOf(s.sh.cfg)
	s.applyFilter(prev)
	s.keyFor = ""
	s.refreshKey()
	return nil
}

// applyFilter shows the rows that match the filter text, in tree order, and
// keeps the row with key prev selected when it is still visible.
func (s *listScreen) applyFilter(prev string) {
	items := s.all
	if s.mode != filterOff {
		if term := strings.TrimSpace(s.filter.Value()); term != "" {
			targets := make([]string, len(s.all))
			for i, it := range s.all {
				targets[i] = it.FilterValue()
			}
			items = nil
			for _, r := range treeFilter(term, targets) {
				items = append(items, s.all[r.Index])
			}
		}
	}
	s.l.SetItems(items)
	s.l.Select(0)
	for i, it := range items {
		if it.(rowItem).key() == prev {
			s.l.Select(i)
			break
		}
	}
}

func (s *listScreen) currentKey() string {
	if r, ok := s.selected(); ok {
		return r.key()
	}
	return ""
}

// setFilter updates the mode and the visible rows.
func (s *listScreen) setFilter(mode filterMode, clear bool) {
	prev := s.currentKey()
	s.mode = mode
	if clear {
		s.filter.SetValue("")
	}
	if mode != filterTyping {
		s.filter.Blur()
	}
	s.applyFilter(prev)
	s.keyFor = ""
	s.refreshKey()
}

// refreshKey Lstat's the key files of the selected account.
func (s *listScreen) refreshKey() {
	r, ok := s.selected()
	ref, isAcc := r.ref()
	if !ok || !isAcc || ref.Account.Auth != config.AuthSSH {
		s.keyFor = ""
		return
	}
	if s.keyFor == r.key() {
		return
	}
	s.keyFor = r.key()
	s.keyInfo = s.sh.svc.KeyInfo(ref)
}

func (s *listScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	km := s.sh.km
	switch msg := msg.(type) {
	case cfgLoadedMsg, cfgChangedMsg:
		s.keyFor = ""
		return s, s.rebuild()
	case tea.KeyMsg:
		switch s.mode {
		case filterTyping:
			return s.typing(msg)
		case filterApplied:
			if msg.Type == tea.KeyEsc {
				s.setFilter(filterOff, true)
				return s, nil
			}
		}
		if s.mode != filterTyping && key.Matches(msg, km.Filter) {
			s.mode = filterTyping
			return s, s.filter.Focus()
		}
		if cmd, handled := s.action(msg, km); handled {
			return s, cmd
		}
	default:
		if s.mode == filterTyping {
			var cmd tea.Cmd
			s.filter, cmd = s.filter.Update(msg) // cursor blink
			return s, cmd
		}
	}
	var cmd tea.Cmd
	s.l, cmd = s.l.Update(msg)
	s.keyFor = ""
	s.refreshKey()
	return s, cmd
}

// typing handles a key while the filter text is being edited.
func (s *listScreen) typing(msg tea.KeyMsg) (Screen, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		s.setFilter(filterOff, true)
		return s, nil
	case tea.KeyEnter:
		if strings.TrimSpace(s.filter.Value()) == "" {
			s.setFilter(filterOff, true)
		} else {
			s.setFilter(filterApplied, false)
		}
		return s, nil
	case tea.KeyUp, tea.KeyDown, tea.KeyCtrlP, tea.KeyCtrlN:
		var cmd tea.Cmd
		s.l, cmd = s.l.Update(msg) // move the selection while typing
		s.keyFor = ""
		s.refreshKey()
		return s, cmd
	}
	prev := s.currentKey()
	var cmd tea.Cmd
	s.filter, cmd = s.filter.Update(msg)
	s.applyFilter(prev)
	s.keyFor = ""
	s.refreshKey()
	return s, cmd
}

func (s *listScreen) action(msg tea.KeyMsg, km *KeyMap) (tea.Cmd, bool) {
	sh := s.sh
	r, has := s.selected()
	switch {
	case key.Matches(msg, km.Quit):
		return tea.Quit, true
	case key.Matches(msg, km.AddCompany):
		return push(newCompanyForm(sh, nil)), true
	case key.Matches(msg, km.AddAccount):
		if !has {
			return say(levelWarn, tr("tui.status.need_company")), true
		}
		return push(newAccountForm(sh, r.company.Slug, nil)), true
	case key.Matches(msg, km.Edit):
		if !has {
			return nil, true
		}
		if r.isCompany() {
			c := r.company
			return push(newCompanyForm(sh, &c)), true
		}
		a := *r.account
		return push(newAccountForm(sh, r.company.Slug, &a)), true
	case key.Matches(msg, km.Delete):
		if !has {
			return nil, true
		}
		return push(newDeleteScreen(sh, r.company, r.account)), true
	case key.Matches(msg, km.Clone):
		return push(newCloneScreen(sh)), true
	case key.Matches(msg, km.Check):
		return push(newCheckScreen(sh)), true
	case key.Matches(msg, km.Doctor):
		return push(newDoctorScreen(sh)), true
	case key.Matches(msg, km.Apply):
		return push(newConfirmScreen(sh, tr("tui.confirm.title_apply"))), true
	case key.Matches(msg, km.Import):
		return push(newImportScreen(sh)), true
	case key.Matches(msg, km.GenKey):
		ref, ok := r.ref()
		if !has || !ok {
			return say(levelWarn, tr("tui.status.pick_account")), true
		}
		return s.genKey(ref), true
	case key.Matches(msg, km.Detail):
		if !has {
			return nil, true
		}
		return push(newDetailScreen(sh, r)), true
	}
	return nil, false
}

// genKey opens the key result of an existing key, or starts the keygen flow.
func (s *listScreen) genKey(ref config.AccountRef) tea.Cmd {
	if ref.Account.Auth != config.AuthSSH {
		return say(levelWarn, tr("err.not_ssh"))
	}
	if s.sh.svc.KeyInfo(ref).Exists {
		return push(newKeyResultScreen(s.sh, ref, nil))
	}
	return push(newKeygenScreen(s.sh, ref))
}

func (s *listScreen) View() string {
	if s.w == 0 {
		return ""
	}
	lw, dw := s.widths()
	left := s.listPanel(lw)
	if dw == 0 {
		return left
	}
	right := s.sh.panel(tr("tui.detail.title"), s.detailBody(dw-4), dw, s.h, false)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

func (s *listScreen) filterLine() string {
	st := s.sh.st
	switch s.mode {
	case filterTyping:
		return s.filter.View()
	case filterApplied:
		return st.Accent.Render("/ ") + st.Text.Render(s.filter.Value()) + st.Faint.Render("  "+tr("tui.list.clear_filter"))
	}
	return st.Faint.Render("/ " + tr("tui.list.filter_hint"))
}

func (s *listScreen) listPanel(w int) string {
	inner := w - 4
	var body string
	switch {
	case !s.sh.loaded:
		body = s.sh.st.Muted.Render(tr("tui.list.loading"))
	case len(s.all) == 0:
		body = s.emptyState(inner)
	default:
		body = s.l.View()
		if len(s.l.Items()) == 0 {
			body = s.sh.st.Muted.Render(tr("tui.list.no_match"))
		}
	}
	content := s.filterLine() + "\n" + body
	title := tr("tui.list.panel", len(s.sh.cfg.Companies), len(s.sh.cfg.All()))
	focus := true
	return s.sh.panel(title, content, w, s.h, focus)
}

func (s *listScreen) emptyState(w int) string {
	st := s.sh.st
	var b strings.Builder
	b.WriteString("\n" + st.Bold.Render(tr("tui.empty.title")) + "\n\n")
	b.WriteString(wrap(st.Muted.Render(tr("tui.empty.body")), w) + "\n\n")
	b.WriteString(st.Accent.Render("A") + "  " + st.Muted.Render(tr("tui.key.add_company")) + "\n")
	b.WriteString(st.Accent.Render("i") + "  " + st.Muted.Render(tr("tui.key.import")) + "\n")
	b.WriteString(st.Accent.Render("?") + "  " + st.Muted.Render(tr("tui.key.help")))
	return b.String()
}

func (s *listScreen) detailBody(w int) string {
	r, ok := s.selected()
	if !ok {
		return s.sh.st.Faint.Render(tr("tui.detail.none"))
	}
	return detailText(s.sh, r, w, s.keyInfo, s.keyFor == r.key())
}

// detailText renders the details of a row. It is shared by the side pane and
// the full-screen detail screen.
func detailText(sh *shared, r rowItem, w int, ki keys.Info, haveKey bool) string {
	st := sh.st
	var b strings.Builder
	line := func(label, value string) {
		if value == "" {
			return
		}
		b.WriteString(st.Label.Render(pad(label, 12)) + st.Text.Render(trunc(value, maxInt(w-12, 4))) + "\n")
	}
	if r.isCompany() {
		c := r.company
		b.WriteString(st.Bold.Render(c.Slug) + "\n\n")
		line(tr("tui.f.name"), c.Name)
		line(tr("tui.f.base_dir"), c.BaseDir)
		line(tr("tui.f.default"), c.DefaultAccount)
		line(tr("tui.f.accounts"), fmt.Sprint(len(c.Accounts)))
		b.WriteString("\n")
		for _, a := range c.Accounts {
			star := " "
			if a.ID == c.DefaultAccount {
				star = st.Warn.Render("★")
			}
			b.WriteString(trunc(star+" "+st.Text.Render(a.ID)+"  "+st.platformBadge(a.Platform)+" "+st.Muted.Render(authTag(a.Auth)), w) + "\n")
		}
		return b.String()
	}
	a := *r.account
	ref := config.AccountRef{Company: r.company, Account: a}
	b.WriteString(st.Bold.Render(ref.String()) + "\n\n")
	line(tr("tui.f.platform"), a.Platform.DisplayName()+" ("+a.EffectiveHost()+")")
	line(tr("tui.f.auth"), authTag(a.Auth))
	line(tr("tui.f.username"), a.Username)
	line(tr("tui.f.git_name"), a.Name)
	line(tr("tui.f.email"), a.Email)
	if len(a.Orgs) > 0 {
		line(tr("tui.f.orgs"), strings.Join(a.Orgs, ", "))
	}
	if r.company.DefaultAccount == a.ID {
		line(tr("tui.f.default"), "★ "+tr("tui.f.default_yes"))
	}
	if a.Auth == config.AuthSSH {
		line(tr("tui.f.alias"), ref.Alias())
		path := a.SSHKey
		if path == "" {
			path = r.company.DefaultKeyPath(a)
		}
		line(tr("tui.f.key"), path)
		if haveKey {
			b.WriteString(st.Label.Render(pad("", 12)))
			switch {
			case !ki.Exists:
				b.WriteString(st.Warn.Render("! " + tr("tui.key.state_missing")))
			case ki.Mode&0o077 != 0:
				b.WriteString(st.Warn.Render("! " + tr("tui.key.state_mode", fmt.Sprintf("%04o", ki.Mode))))
			default:
				b.WriteString(st.OK.Render("✓ " + tr("tui.key.state_ok")))
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// detailScreen shows one row in full (used on narrow terminals).
type detailScreen struct {
	sh   *shared
	row  rowItem
	w, h int
	ki   keys.Info
	have bool
}

func newDetailScreen(sh *shared, r rowItem) *detailScreen {
	d := &detailScreen{sh: sh, row: r}
	if ref, ok := r.ref(); ok && ref.Account.Auth == config.AuthSSH {
		d.ki, d.have = sh.svc.KeyInfo(ref), true
	}
	return d
}

func (d *detailScreen) Title() string { return d.row.key() }
func (d *detailScreen) Init() tea.Cmd { return nil }
func (d *detailScreen) Resize(w, h int) {
	d.w, d.h = w, h
}

func (d *detailScreen) Help() []key.Binding {
	km := d.sh.km
	return []key.Binding{km.Edit, km.Delete, km.GenKey, km.Back}
}

func (d *detailScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	km := d.sh.km
	if k, ok := msg.(tea.KeyMsg); ok {
		r := d.row
		switch {
		case key.Matches(k, km.Edit):
			if r.isCompany() {
				c := r.company
				return d, replace(newCompanyForm(d.sh, &c))
			}
			a := *r.account
			return d, replace(newAccountForm(d.sh, r.company.Slug, &a))
		case key.Matches(k, km.Delete):
			return d, replace(newDeleteScreen(d.sh, r.company, r.account))
		case key.Matches(k, km.GenKey):
			ref, ok := r.ref()
			if !ok || ref.Account.Auth != config.AuthSSH {
				return d, say(levelWarn, tr("err.not_ssh"))
			}
			if d.sh.svc.KeyInfo(ref).Exists {
				return d, replace(newKeyResultScreen(d.sh, ref, nil))
			}
			return d, replace(newKeygenScreen(d.sh, ref))
		}
	}
	return d, nil
}

func (d *detailScreen) View() string {
	return d.sh.panel(tr("tui.detail.title"), detailText(d.sh, d.row, d.w-4, d.ki, d.have), d.w, d.h, true)
}

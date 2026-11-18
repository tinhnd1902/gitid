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

// Layout breakpoints. The terminal body is the area between the header and
// the status bar (terminal height minus chromeRows).
const (
	sideWidth     = 84 // from this width the detail pane sits next to the list
	stackMinWidth = 60 // from this width (and stackMinBodyH rows) it sits below
	stackMinBodyH = 21 // body rows needed for the stacked layout (24-row terminal)
	idCol         = 14 // width of the account id column
)

type layoutMode int

const (
	layoutList    layoutMode = iota // list only; enter opens the detail screen
	layoutStacked                   // detail panel below the list
	layoutSide                      // detail panel next to the list
)

// countLabel says how many accounts a company has, in words.
func countLabel(n int) string {
	switch n {
	case 0:
		return tr("tui.list.count_zero")
	case 1:
		return tr("tui.list.count_one")
	}
	return tr("tui.list.count_many", n)
}

// emptyCompanies lists the slugs of the companies that have no account yet:
// gitid writes nothing for them.
func emptyCompanies(cfg config.Config) []string {
	var out []string
	for _, c := range cfg.Companies {
		if len(c.Accounts) == 0 {
			out = append(out, c.Slug)
		}
	}
	return out
}

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
	fmt.Fprint(w, d.line(r, width, selected, companyLevel(m.Items(), width)))
}

func authTag(a config.Auth) string {
	if a == config.AuthSSH {
		return "ssh"
	}
	return "https"
}

func (d rowDelegate) line(r rowItem, width int, selected bool, level int) string {
	st := d.sh.st
	if r.isCompany() {
		return d.companyLine(r, width, selected, level)
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
	if _, dup := duplicateOf(r.company, *a); dup {
		warn = " !" // the same login twice in one company, see the detail
	}
	tag := platformTag(a.Platform)
	mid := fmt.Sprintf("%s %s  %s %s", star, pad(a.ID, idCol), tag, authTag(a.Auth))
	email := a.Email
	if selected {
		left := "▌" + branch + " " + mid + warn
		if lipgloss.Width(left)+lipgloss.Width(email)+2 > width {
			email = "" // same rule as the other rows: the email goes first, never the id
		}
		return st.Selected.Render(pad(left, maxInt(width-lipgloss.Width(email)-1, 4)) + " " + email)
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

// companyParts returns the pieces of a company row for a level of detail:
// 0 = name and base directory, 1 = name only, 2 = slug only. The count label
// is always shown.
func companyParts(c config.Company, level int) (name, base string) {
	if c.Name != "" && c.Name != c.Slug && level < 2 {
		name = "  " + c.Name
	}
	if level == 0 {
		base = c.BaseDir
	}
	return name, base
}

func companyRight(c config.Company, base string) string {
	count := countLabel(len(c.Accounts))
	if base != "" {
		return base + "  " + count
	}
	return count
}

// companyLevel picks one level of detail for every company row, so the
// columns line up: the richest level at which all visible company rows fit.
// The base directory goes first, then the name; when even the slug and the
// count do not fit, rows are cut one by one (level 2).
func companyLevel(items []list.Item, width int) int {
	for level := 0; level < 2; level++ {
		ok := true
		for _, it := range items {
			r, isRow := it.(rowItem)
			if !isRow || !r.isCompany() {
				continue
			}
			name, base := companyParts(r.company, level)
			if lipgloss.Width("▪ "+r.company.Slug+name)+lipgloss.Width(companyRight(r.company, base))+2 > width {
				ok = false
				break
			}
		}
		if ok {
			return level
		}
	}
	return 2
}

// companyLine renders a company row at the given level (see companyLevel).
func (d rowDelegate) companyLine(r rowItem, width int, selected bool, level int) string {
	st := d.sh.st
	c := r.company
	n := len(c.Accounts)
	count := countLabel(n)
	name, base := companyParts(c, level)
	right := companyRight(c, base)
	rw := lipgloss.Width(right)
	if selected {
		left := trunc("▌ "+c.Slug+name, maxInt(width-rw-2, 4))
		return st.Selected.Render(pad(left, maxInt(width-rw-1, 4)) + " " + right)
	}
	l := st.Accent.Bold(true).Render("▪ "+c.Slug) + st.Muted.Render(name)
	l = trunc(l, maxInt(width-rw-2, 4))
	countStyle := st.Muted
	if n == 0 {
		countStyle = st.Warn
	}
	rr := countStyle.Render(count)
	if base != "" {
		rr = st.Faint.Render(base) + "  " + rr
	}
	return joinEnds(l, rr, width)
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
	// Paging uses the shared bindings, so the "?" overlay row and the list
	// can not drift apart.
	l.KeyMap.NextPage = key.NewBinding(key.WithKeys(sh.km.PageDown.Keys()...))
	l.KeyMap.PrevPage = key.NewBinding(key.WithKeys(sh.km.PageUp.Keys()...))
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
	if _, has := s.selected(); !has {
		// nothing to edit, delete or open (the welcome guide)
		return []key.Binding{km.Filter, km.AddAccount, km.AddCompany, km.Apply, km.Clone, km.Check, km.Doctor, km.GenKey, km.Import, km.Reload, km.Quit}
	}
	return []key.Binding{km.Filter, km.AddAccount, km.AddCompany, km.Edit, km.Delete, km.Apply, km.Clone, km.Check, km.Doctor, km.GenKey, km.Import, km.Detail, km.Reload, km.Quit}
}

// ContextHelp picks the few keys that matter in the current state. The first
// match of the table wins; the primary item is the next step.
func (s *listScreen) ContextHelp() []helpItem {
	km := s.sh.km
	it := func(b key.Binding) helpItem { return helpItem{b: b} }
	prim := func(b key.Binding) helpItem { return helpItem{b: b, primary: true} }
	switch {
	case !s.sh.loaded:
		return []helpItem{it(km.Quit)}
	case s.welcome():
		return []helpItem{prim(withDesc(km.Detail, "enter", tr("tui.key.add_company"))), it(km.Import), it(km.Quit)}
	case s.mode == filterTyping:
		return []helpItem{
			prim(withDesc(km.Enter, "enter", tr("tui.key.filter_keep"))),
			it(withDesc(km.Back, "esc", tr("tui.key.filter_clear"))),
			it(withDesc(km.Up, "↑/↓", tr("tui.key.move"))),
		}
	}
	r, has := s.selected()
	pending := s.sh.planned && s.sh.pending > 0
	var out []helpItem
	switch {
	case !has:
		out = []helpItem{it(km.Quit)}
	case r.isCompany() && len(r.company.Accounts) == 0:
		out = []helpItem{prim(km.AddAccount), it(withDesc(km.Edit, "e", tr("tui.key.edit_company"))), it(km.Delete), it(km.AddCompany), it(km.Quit)}
	case pending:
		out = []helpItem{prim(km.Apply), it(km.AddAccount), it(km.Edit), it(km.Delete), it(km.Quit)}
	case !r.isCompany() && r.account.Auth == config.AuthSSH && s.keyFor == r.key() && !s.keyInfo.Exists:
		out = []helpItem{prim(km.GenKey), it(km.Edit), it(km.Delete), it(km.Check), it(km.Quit)}
	case r.isCompany():
		out = []helpItem{it(km.AddAccount), it(km.Edit), it(km.Delete), it(km.Clone), it(km.Check), it(km.Quit)}
	default:
		out = []helpItem{it(km.Edit), it(km.Delete), it(km.Check), it(km.Clone)}
		if r.account.Auth == config.AuthSSH {
			out = append(out, it(km.GenKey))
		}
		out = append(out, it(km.Quit))
	}
	// insert puts an item right after the primary one (or first when there is none)
	insert := func(h helpItem) {
		at := 0
		if len(out) > 0 && out[0].primary {
			at = 1
		}
		out = append(out[:at], append([]helpItem{h}, out[at:]...)...)
	}
	if has && s.layout() == layoutList {
		insert(it(km.Detail))
	}
	if s.mode == filterApplied {
		insert(it(withDesc(km.Back, "esc", tr("tui.key.filter_clear"))))
	}
	return out
}

func (s *listScreen) Resize(w, h int) {
	s.w, s.h = w, h
	lw, lh, _, _ := s.dims()
	s.filter.Width = maxInt(lw-10, 8)
	s.l.SetSize(maxInt(lw-4, 4), maxInt(lh-4, 1)) // width: border+padding; height: border(2) + title(1) + filter line(1)
}

// layout picks how the detail pane is shown for the current body size.
func (s *listScreen) layout() layoutMode {
	switch {
	case s.w >= sideWidth:
		return layoutSide
	case s.w >= stackMinWidth && s.h >= stackMinBodyH:
		return layoutStacked
	}
	return layoutList
}

// dims returns the outer size of the list panel and of the detail panel
// (zero when there is none) for the current layout.
func (s *listScreen) dims() (lw, lh, dw, dh int) {
	switch s.layout() {
	case layoutSide:
		lw = s.w * 55 / 100
		return lw, s.h, s.w - lw, s.h
	case layoutStacked:
		// The detail pane gets what the list does not need: the rows plus
		// the border, the title and the filter line. The row count is that of
		// the whole tree, so filtering does not move the split.
		need := len(s.all) + 4
		dh = clamp(s.h/3, 7, 10)
		if s.h-need > dh {
			dh = s.h - need
		}
		dh = minInt(dh, s.h-6)
		return s.w, s.h - dh, s.w, dh
	}
	return s.w, s.h, 0, 0
}

// welcome reports whether the home screen shows the first-run guide.
func (s *listScreen) welcome() bool { return s.sh.loaded && len(s.all) == 0 }

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
	s.Resize(s.w, s.h) // the stacked split depends on the number of rows
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

// focusRow selects the row with key k, clearing a filter that hides it.
func (s *listScreen) focusRow(k string) {
	find := func() int {
		for i, it := range s.l.Items() {
			if it.(rowItem).key() == k {
				return i
			}
		}
		return -1
	}
	i := find()
	if i < 0 && s.mode != filterOff {
		s.setFilter(filterOff, true)
		i = find()
	}
	if i < 0 {
		return
	}
	s.l.Select(i)
	s.keyFor = ""
	s.refreshKey()
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
	case focusMsg:
		s.focusRow(msg.key)
		return s, nil
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
		return sh.pushNow(newCompanyForm(sh, nil)), true
	case key.Matches(msg, km.AddAccount):
		if !has {
			return say(levelWarn, tr("tui.status.need_company")), true
		}
		return sh.pushNow(newAccountForm(sh, r.company.Slug, nil)), true
	case key.Matches(msg, km.Edit):
		if !has {
			return nil, true
		}
		if r.isCompany() {
			c := r.company
			return sh.pushNow(newCompanyForm(sh, &c)), true
		}
		a := *r.account
		return sh.pushNow(newAccountForm(sh, r.company.Slug, &a)), true
	case key.Matches(msg, km.Delete):
		if !has {
			return nil, true
		}
		return sh.pushNow(newDeleteScreen(sh, r.company, r.account)), true
	case key.Matches(msg, km.Clone):
		return sh.pushNow(newCloneScreen(sh, cloneHint(r, has))), true
	case key.Matches(msg, km.Check):
		return sh.pushNow(newCheckScreen(sh)), true
	case key.Matches(msg, km.Doctor):
		return sh.pushNow(newDoctorScreen(sh)), true
	case key.Matches(msg, km.Apply):
		return sh.pushNow(newConfirmScreen(sh, tr("tui.confirm.title_apply"))), true
	case key.Matches(msg, km.Import):
		return sh.pushNow(newImportScreen(sh)), true
	case key.Matches(msg, km.GenKey):
		ref, ok := r.ref()
		if !has || !ok {
			return say(levelWarn, tr("tui.status.pick_account")), true
		}
		return s.genKey(ref), true
	case key.Matches(msg, km.Detail):
		if s.welcome() {
			return sh.pushNow(newCompanyForm(sh, nil)), true // the first step
		}
		if !has {
			return nil, true
		}
		return sh.pushNow(newDetailScreen(sh, r)), true
	case key.Matches(msg, km.Reload):
		return reload(), true
	}
	return nil, false
}

// cloneHint is the account the clone flow should start on for a row: the
// account itself, or the default (else the first) account of a company.
func cloneHint(r rowItem, has bool) *config.AccountRef {
	if !has {
		return nil
	}
	if ref, ok := r.ref(); ok {
		return &ref
	}
	if a, ok := r.company.Default(); ok {
		return &config.AccountRef{Company: r.company, Account: a}
	}
	if len(r.company.Accounts) > 0 {
		return &config.AccountRef{Company: r.company, Account: r.company.Accounts[0]}
	}
	return nil
}

// genKey opens the key result of an existing key, or starts the keygen flow.
func (s *listScreen) genKey(ref config.AccountRef) tea.Cmd {
	if ref.Account.Auth != config.AuthSSH {
		return say(levelWarn, tr("err.not_ssh"))
	}
	if s.sh.svc.KeyInfo(ref).Exists {
		return s.sh.pushNow(newKeyResultScreen(s.sh, ref, nil))
	}
	return s.sh.pushNow(newKeygenScreen(s.sh, ref))
}

func (s *listScreen) View() string {
	if s.w == 0 {
		return ""
	}
	if s.welcome() {
		return s.sh.panel(tr("tui.list.panel", 0, 0), s.welcomeBody(s.w-4, s.h-3), s.w, s.h, true)
	}
	lw, lh, dw, dh := s.dims()
	left := s.listPanel(lw, lh)
	switch s.layout() {
	case layoutSide:
		right := s.sh.panel(tr("tui.detail.title"), s.detailBody(dw-4, dh-3), dw, dh, false)
		return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	case layoutStacked:
		right := s.sh.panel(tr("tui.detail.title"), s.detailBody(dw-4, dh-3), dw, dh, false)
		return lipgloss.JoinVertical(lipgloss.Left, left, right)
	}
	return left
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

func (s *listScreen) listPanel(w, h int) string {
	inner := w - 4
	st := s.sh.st
	var body string
	switch {
	case !s.sh.loaded:
		body = st.Muted.Render(tr("tui.list.loading"))
	case len(s.l.Items()) == 0:
		term := strings.TrimSpace(s.filter.Value())
		lines := []string{st.Muted.Render(trunc(tr("tui.list.no_match", term), inner))}
		for _, l := range wrapLines(tr("tui.list.no_match_hint"), inner) {
			lines = append(lines, st.Faint.Render(l))
		}
		body = strings.Join(lines, "\n")
	default:
		body = s.l.View()
		if hintLines := s.rowHint(inner); len(hintLines) > 0 {
			n := len(s.l.Items())
			rows := splitLines(body)
			if len(rows) > n {
				rows = rows[:n]
			}
			body = strings.Join(rows, "\n") + "\n\n" + strings.Join(hintLines, "\n")
		}
	}
	content := s.filterLine() + "\n" + body
	title := tr("tui.list.panel", len(s.sh.cfg.Companies), len(s.sh.cfg.All()))
	return s.sh.panel(title, content, w, h, true)
}

// rowHint is the centered "add the first account" hint shown under the rows
// when the list has no detail pane (a company without account is selected)
// and at least three lines are free.
func (s *listScreen) rowHint(w int) []string {
	r, ok := s.selected()
	if !ok || !r.isCompany() || len(r.company.Accounts) > 0 || s.layout() != layoutList {
		return nil
	}
	if s.l.Height()-len(s.l.Items()) < 3 {
		return nil
	}
	return s.sh.noAccountsLines(r.company, w, false)
}

// noAccountsLines explains what a company without account means and what to
// press. The reason is included when withWhy is set.
func (sh *shared) noAccountsLines(c config.Company, w int, withWhy bool) []string {
	st := sh.st
	var out []string
	if withWhy {
		for _, l := range wrapLines(tr("tui.list.no_accounts_why", c.Slug), w) {
			out = append(out, st.Muted.Render(l))
		}
		out = append(out, "")
	}
	// centered when it fits on one line; a wrapped hint reads better flush left
	lines := wrapLines(tr("tui.list.no_accounts_hint", c.Slug), w)
	for _, l := range lines {
		l = st.Accent.Bold(true).Render(l)
		if len(lines) == 1 {
			l = center(l, w)
		}
		out = append(out, l)
	}
	return out
}

// center centers one (possibly styled) line in w cells.
func center(line string, w int) string {
	return lipgloss.PlaceHorizontal(w, lipgloss.Center, line)
}

// wrapLines word-wraps plain text to w cells and returns the lines without
// trailing spaces.
func wrapLines(s string, w int) []string {
	lines := splitLines(wrap(s, w))
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return lines
}

// welcomeBody is the first-run guide: what gitid does, the three steps with
// the key of each, and the import shortcut. Below a comfortable height the
// optional parts are dropped, then the step text is cut to one line.
func (s *listScreen) welcomeBody(w, h int) string {
	st := s.sh.st
	steps := func(wrapText bool) []string {
		var out []string
		for i, k := range []string{"A", "a", "p"} {
			prefix := st.Faint.Render(fmt.Sprintf("  %d  ", i+1)) + st.Accent.Bold(true).Render(k) + "  "
			text := tr(fmt.Sprintf("tui.empty.step%d", i+1))
			if !wrapText {
				out = append(out, trunc(prefix+st.Text.Render(text), w))
				continue
			}
			for j, l := range wrapLines(text, maxInt(w-8, 8)) {
				if j == 0 {
					out = append(out, prefix+st.Text.Render(l))
				} else {
					out = append(out, spaces(8)+st.Text.Render(l))
				}
			}
		}
		return out
	}
	para := func(style lipgloss.Style, key string) []string {
		var out []string
		for _, l := range wrapLines(tr(key), w) {
			out = append(out, style.Render(l))
		}
		return out
	}
	title := st.Bold.Render(tr("tui.empty.title"))
	join := func(parts ...[]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	blank := []string{""}
	stepsHead := []string{st.Bold.Render(tr("tui.empty.steps"))}
	full := join([]string{title}, para(st.Muted, "tui.empty.body"), blank, stepsHead, steps(true),
		para(st.Muted, "tui.empty.after"), blank, para(st.Faint, "tui.empty.import"))
	compact := join([]string{title}, stepsHead, steps(true), para(st.Muted, "tui.empty.after"), para(st.Faint, "tui.empty.import"))
	short := join([]string{title}, steps(false))
	tiny := steps(false)
	variants := [][]string{full, compact, short, tiny}
	pick := tiny
	for _, v := range variants {
		if len(v) <= h {
			pick = v
			break
		}
	}
	return strings.Join(pick, "\n")
}

func (s *listScreen) detailBody(w, h int) string {
	r, ok := s.selected()
	if !ok {
		return s.sh.st.Faint.Render(tr("tui.detail.none"))
	}
	return detailText(s.sh, r, w, h, s.keyInfo, s.keyFor == r.key())
}

// detailText renders the details of a row in at most h lines (h <= 0: no
// limit). It is shared by the side and stacked panes and the full-screen
// detail screen. Variants are ordered from the richest to the most compact;
// the first one that fits is used, so the "what happens" part survives small
// panes.
func detailText(sh *shared, r rowItem, w, h int, ki keys.Info, haveKey bool) string {
	var variants [][]string
	if r.isCompany() {
		variants = companyVariants(sh, r.company, w)
	} else {
		variants = accountVariants(sh, r, w, ki, haveKey)
	}
	pick := variants[len(variants)-1]
	for _, v := range variants {
		if h <= 0 || len(v) <= h {
			pick = v
			break
		}
	}
	return strings.Join(pick, "\n")
}

// detailField renders "label  value" with the value cut to the width.
func detailField(st *Styles, label, value string, w int) string {
	lc := labelCol(w)
	return st.Label.Render(pad(label, lc)) + st.Text.Render(trunc(value, maxInt(w-lc, 4)))
}

// labelCol is the width of the label column of the detail pane: the longest
// label of the active language plus a space (so "Base directory" is not cut),
// at least 12 and at most half of the pane.
func labelCol(w int) int {
	n := 12
	for _, k := range []string{"tui.f.base_dir", "tui.f.default", "tui.f.accounts", "tui.f.platform", "tui.f.auth",
		"tui.f.username", "tui.f.git_name", "tui.f.email", "tui.f.orgs", "tui.f.alias", "tui.f.key", "tui.f.remote", "tui.f.dir"} {
		n = maxInt(n, lipgloss.Width(tr(k))+1)
	}
	return minInt(n, maxInt(w/2, 12))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// styledWrap wraps text and applies a style to every line.
func styledWrap(style lipgloss.Style, text string, w int) []string {
	var out []string
	for _, l := range wrapLines(text, w) {
		out = append(out, style.Render(l))
	}
	return out
}

func companyVariants(sh *shared, c config.Company, w int) [][]string {
	st := sh.st
	title := c.Slug
	if c.Name != "" && c.Name != c.Slug {
		title += " · " + c.Name
	}
	titleLine := st.Bold.Render(trunc(title, w))
	header := st.Bold.Render(tr("tui.detail.how"))
	def, hasDef := c.Default()
	defName := tr("tui.detail.no_default")
	if hasDef {
		defName = def.ID
	}
	accounts := tr("tui.detail.no_default")
	if n := len(c.Accounts); n > 0 {
		accounts = fmt.Sprint(n)
	}
	fields := []string{
		detailField(st, tr("tui.f.base_dir"), c.BaseDir, w),
		detailField(st, tr("tui.f.default"), defName, w),
		detailField(st, tr("tui.f.accounts"), accounts, w),
	}
	var list []string
	for _, a := range c.Accounts {
		star := " "
		if a.ID == c.DefaultAccount {
			star = st.Warn.Render("★")
		}
		list = append(list, trunc(star+" "+st.Text.Render(a.ID)+"  "+st.platformBadge(a.Platform)+" "+st.Muted.Render(authTag(a.Auth)), w))
	}
	cat := func(parts ...[]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	one := func(l string) []string { return []string{l} }
	blank := []string{""}

	if len(c.Accounts) == 0 {
		why := sh.noAccountsLines(c, w, true)
		return [][]string{
			cat(one(titleLine), blank, fields, blank, one(header), why),
			cat(one(titleLine), one(header), why),
			cat(one(titleLine), noBlank(why)),
			cat(noBlank(why)),
			cat(noBlank(sh.noAccountsLines(c, w, false))),
		}
	}
	// nextStep is the sentence that says what to press (apply, or verify). It
	// is never dropped, whatever the height.
	var nextStep []string
	switch {
	case sh.planErr:
	case sh.planned && sh.pending > 0:
		nextStep = styledWrap(st.Warn, tr("tui.detail.how_pending"), w)
	case sh.planned:
		nextStep = styledWrap(st.OK, tr("tui.detail.how_applied"), w)
	}
	howDefault := styledWrap(st.Muted, tr("tui.detail.how_default", c.BaseDir, def.Name, def.Email), w)
	how := func(others bool) []string {
		out := append([]string(nil), howDefault...)
		if others && len(c.Accounts) > 1 {
			out = append(out, styledWrap(st.Muted, tr("tui.detail.how_others"), w)...)
		}
		return append(out, nextStep...)
	}
	out := [][]string{
		cat(one(titleLine), blank, fields, blank, list, blank, one(header), how(true)),
		cat(one(titleLine), blank, fields, blank, one(header), how(true)),
		cat(one(titleLine), blank, fields, blank, one(header), how(false)),
		cat(one(titleLine), one(header), how(false)),
		cat(one(titleLine), how(false)),
		// very short panes: the explanation and the next step, without title
		cat(how(false)),
	}
	if len(nextStep) > 0 {
		out = append(out, nextStep)
	}
	return out
}

func noBlank(lines []string) []string {
	var out []string
	for _, l := range lines {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// accountTarget describes the remote that selects an account, for the
// "used when" line (see remote.Patterns: https needs the username in the
// URL, ssh uses the account's alias).
func accountTarget(ref config.AccountRef) string {
	a := ref.Account
	if a.Auth == config.AuthSSH {
		return "git@" + ref.Alias() + ":…"
	}
	path := ""
	if len(a.Orgs) > 0 {
		path = "{" + strings.Join(a.Orgs, ",") + "}/"
	}
	return "https://" + a.Username + "@" + a.EffectiveHost() + "/" + path + "…"
}

// wrapField renders "label  value" with the value wrapped under the value
// column instead of cut, for values that must stay readable (the remote).
func wrapField(st *Styles, label, value string, w int) []string {
	lc := labelCol(w)
	var out []string
	for i, l := range wrapUnits(value, maxInt(w-lc, 8)) {
		head := spaces(lc)
		if i == 0 {
			head = pad(label, lc)
		}
		out = append(out, st.Label.Render(head)+st.Text.Render(l))
	}
	return out
}

func accountVariants(sh *shared, r rowItem, w int, ki keys.Info, haveKey bool) [][]string {
	st := sh.st
	a := *r.account
	ref := config.AccountRef{Company: r.company, Account: a}
	titleLine := st.Bold.Render(trunc(ref.String(), w))
	example := remoteExample(ref)

	var keyState string
	if haveKey && a.Auth == config.AuthSSH {
		pad12 := st.Label.Render(pad("", labelCol(w)))
		switch {
		case !ki.Exists:
			keyState = pad12 + st.Warn.Render(trunc("! "+tr("tui.key.state_missing"), maxInt(w-labelCol(w), 4)))
		case ki.Mode&0o077 != 0:
			keyState = pad12 + st.Warn.Render(trunc("! "+tr("tui.key.state_mode", fmt.Sprintf("%04o", ki.Mode)), maxInt(w-labelCol(w), 4)))
		default:
			keyState = pad12 + st.OK.Render(trunc("✓ "+tr("tui.key.state_ok"), maxInt(w-labelCol(w), 4)))
		}
	}
	var how []string
	if r.company.DefaultAccount == a.ID {
		how = styledWrap(st.Muted, tr("tui.detail.acc_default", r.company.BaseDir), w)
	} else {
		how = styledWrap(st.Muted, tr("tui.detail.acc_other", accountTarget(ref), r.company.BaseDir, r.company.DefaultAccount), w)
	}
	// next is the sentence that says what to press; it is never dropped.
	var next []string
	switch {
	case sh.planErr:
	case sh.planned && sh.pending > 0:
		next = styledWrap(st.Warn, tr("tui.detail.how_pending"), w)
	case sh.planned:
		next = styledWrap(st.OK, tr("tui.detail.acc_next_applied", r.company.BaseDir), w)
	}
	var dup []string
	if other, ok := duplicateOf(r.company, a); ok {
		dup = styledWrap(st.Warn, tr("tui.detail.dup", r.company.Slug+"/"+other.ID), w)
	}
	opt := func(s string) []string {
		if s == "" {
			return nil
		}
		return []string{s}
	}
	cat := func(parts ...[]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	one := func(l string) []string { return []string{l} }
	blank := []string{""}

	auth := authTag(a.Auth)
	if a.Auth == config.AuthHTTPS {
		auth = tr("tui.detail.auth_https")
	}
	// Values the user typed wrap under their label instead of being cut.
	wf := func(label, value string) []string { return wrapField(st, label, value, w) }
	username := wf(tr("tui.f.username"), a.Username)
	remoteLine := wf(tr("tui.f.remote"), example)
	var fields []string
	for _, part := range [][]string{
		wf(tr("tui.f.platform"), a.Platform.DisplayName()+" ("+a.EffectiveHost()+")"),
		username,
		wf(tr("tui.f.email"), a.Email),
		wf(tr("tui.f.git_name"), a.Name),
		wf(tr("tui.f.auth"), auth),
		remoteLine,
		wf(tr("tui.f.dir"), r.company.BaseDir),
	} {
		fields = append(fields, part...)
	}
	if len(a.Orgs) > 0 {
		fields = append(fields, wf(tr("tui.f.orgs"), strings.Join(a.Orgs, ", "))...)
	}
	if r.company.DefaultAccount == a.ID {
		fields = append(fields, wf(tr("tui.f.default"), "★ "+tr("tui.f.default_yes"))...)
	}
	if a.Auth == config.AuthSSH {
		path := a.SSHKey
		if path == "" {
			path = r.company.DefaultKeyPath(a)
		}
		fields = append(fields, wf(tr("tui.f.alias"), ref.Alias())...)
		fields = append(fields, wf(tr("tui.f.key"), path)...)
	}
	// Compaction, richest first: everything with blank lines; everything
	// without them; the main fields; the login, the remote and the key state; the remote
	// alone; the remote without its label; the next step only. The remote
	// line and the next step survive the smallest pane that fits anything.
	last := next
	if len(last) == 0 {
		last = how // nothing to press yet (plan not computed): say what happens
	}
	core := cat(username, wf(tr("tui.f.email"), a.Email), wf(tr("tui.f.auth"), auth), remoteLine, wf(tr("tui.f.dir"), r.company.BaseDir))
	return [][]string{
		cat(one(titleLine), blank, fields, opt(keyState), blank, dup, how, next),
		cat(one(titleLine), fields, opt(keyState), dup, how, next),
		cat(one(titleLine), core, opt(keyState), dup, how, next),
		cat(one(titleLine), username, remoteLine, opt(keyState), dup, how, next),
		cat(one(titleLine), remoteLine, how, next),
		cat(remoteLine, next),
		cat(styledWrap(st.Text, example, w), next),
		last,
	}
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
	return []key.Binding{km.AddAccount, km.Edit, km.Delete, km.GenKey, km.Apply, km.Clone, km.Check, km.Doctor, km.Back}
}

// ContextHelp puts the next step first in the short help bar.
func (d *detailScreen) ContextHelp() []helpItem {
	km := d.sh.km
	isSSH := false
	if ref, ok := d.row.ref(); ok {
		isSSH = ref.Account.Auth == config.AuthSSH
	}
	primary := ""
	switch {
	case d.row.isCompany() && len(d.row.company.Accounts) == 0:
		primary = "a"
	case d.sh.pending > 0:
		primary = "p"
	case d.have && !d.ki.Exists:
		primary = "g"
	}
	mk := func(b key.Binding, k string) helpItem {
		return helpItem{b: b, primary: primary == k}
	}
	items := []helpItem{mk(km.AddAccount, "a"), mk(km.Apply, "p"), mk(km.Edit, "e"), mk(km.Delete, "d")}
	if isSSH {
		items = append(items, mk(km.GenKey, "g"))
	}
	items = append(items, mk(km.Clone, "c"), mk(km.Check, "k"), mk(km.Doctor, "D"), mk(km.Back, "esc"))
	// primary first
	for i, it := range items {
		if it.primary {
			items = append([]helpItem{it}, append(items[:i:i], items[i+1:]...)...)
			break
		}
	}
	return items
}

func (d *detailScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	km := d.sh.km
	if k, ok := msg.(tea.KeyMsg); ok {
		r := d.row
		switch {
		case key.Matches(k, km.Edit):
			if r.isCompany() {
				c := r.company
				return d, d.sh.replaceNow(newCompanyForm(d.sh, &c))
			}
			a := *r.account
			return d, d.sh.replaceNow(newAccountForm(d.sh, r.company.Slug, &a))
		case key.Matches(k, km.AddAccount):
			return d, d.sh.replaceNow(newAccountForm(d.sh, r.company.Slug, nil))
		case key.Matches(k, km.Apply):
			return d, d.sh.pushNow(newConfirmScreen(d.sh, tr("tui.confirm.title_apply")))
		case key.Matches(k, km.Check):
			return d, d.sh.pushNow(newCheckScreen(d.sh))
		case key.Matches(k, km.Clone):
			return d, d.sh.pushNow(newCloneScreen(d.sh, cloneHint(r, true)))
		case key.Matches(k, km.Doctor):
			return d, d.sh.pushNow(newDoctorScreen(d.sh))
		case key.Matches(k, km.Delete):
			return d, d.sh.replaceNow(newDeleteScreen(d.sh, r.company, r.account))
		case key.Matches(k, km.GenKey):
			ref, ok := r.ref()
			if !ok || ref.Account.Auth != config.AuthSSH {
				return d, say(levelWarn, tr("err.not_ssh"))
			}
			if d.sh.svc.KeyInfo(ref).Exists {
				return d, d.sh.replaceNow(newKeyResultScreen(d.sh, ref, nil))
			}
			return d, d.sh.replaceNow(newKeygenScreen(d.sh, ref))
		}
	}
	return d, nil
}

func (d *detailScreen) View() string {
	return d.sh.panel(tr("tui.detail.title"), detailText(d.sh, d.row, d.w-4, d.h-3, d.ki, d.have), d.w, d.h, true)
}

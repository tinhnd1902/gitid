package tui

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/keymap"
)

// reposTab is one tab of the repo browser.
type reposTab int

// The tabs, in display order.
const (
	tabLocal reposTab = iota
	tabURL
	tabOnline
	numTabs
)

// Timing of the scan feed: results reach the screen in batches, progress at
// most this often, and the screen classifies at most this often while a scan
// runs.
const (
	feedEvery  = 100 * time.Millisecond
	feedBatch  = 32
	classEvery = 300 * time.Millisecond
)

// ---- session cache ----

// repoSession is the raw result of the local scan, kept in shared for the
// life of the process. It holds only I/O data: a configuration change is
// classified again from it without touching the disk.
type repoSession struct {
	raw      map[string][]app.RawRepo // by company slug
	at       map[string]int           // path -> index in raw[company]
	done     map[string]app.CompanyDone
	baseDirs []string // the expanded base dirs the scan was made for
	complete bool     // every company finished (none cancelled)
}

func newRepoSession(baseDirs []string) *repoSession {
	return &repoSession{raw: map[string][]app.RawRepo{}, at: map[string]int{}, done: map[string]app.CompanyDone{}, baseDirs: baseDirs}
}

// put adds a repository, or replaces the one at the same path.
func (s *repoSession) put(r app.RawRepo) {
	if i, ok := s.at[r.Path]; ok && i < len(s.raw[r.Company]) && s.raw[r.Company][i].Path == r.Path {
		s.raw[r.Company][i] = r
		return
	}
	s.at[r.Path] = len(s.raw[r.Company])
	s.raw[r.Company] = append(s.raw[r.Company], r)
}

// rawList flattens the results in company order.
func (s *repoSession) rawList(cfg config.Config) []app.RawRepo {
	var out []app.RawRepo
	for _, c := range cfg.Companies {
		out = append(out, s.raw[c.Slug]...)
	}
	return out
}

// finished says whether every company of cfg ended its scan (cancelled or
// not started does not count).
func (s *repoSession) finished(cfg config.Config) bool {
	for _, c := range cfg.Companies {
		if st := s.done[c.Slug].State; st == "" || st == app.ScanCancelled {
			return false
		}
	}
	return true
}

// ---- messages of the scan ----

type scanProgress struct {
	company     string
	dirs, found int
}

type companyDoneEv struct {
	company string
	done    app.CompanyDone
}

// scanBatchMsg carries what a scan produced since the previous message.
type scanBatchMsg struct {
	id       int
	raws     []app.RawRepo
	dones    []companyDoneEv
	prog     scanProgress
	hasProg  bool
	finished bool
}

// repoInspectedMsg: one repository was read again (after a clone or a fix).
type repoInspectedMsg struct {
	id      int
	company string
	raw     app.RawRepo
}

// repoCopyMsg: the result of copying a path.
type repoCopyMsg struct {
	id   int
	path string
	err  error
}

func (scanBatchMsg) bgMsg()     {}
func (repoInspectedMsg) bgMsg() {}
func (repoCopyMsg) bgMsg()      {}

// scanFeed turns the events of a scan (many, from several goroutines) into a
// few messages: found repositories are batched, progress is throttled and
// dropped when the screen is behind. Results are never dropped.
type scanFeed struct {
	ctx context.Context
	id  int
	ch  chan tea.Msg

	mu       sync.Mutex
	raws     []app.RawRepo
	dones    []companyDoneEv
	prog     scanProgress
	progNew  bool
	lastSent time.Time
}

func (f *scanFeed) emit(ev app.LocalEvent) {
	f.mu.Lock()
	switch {
	case ev.Raw != nil:
		f.raws = append(f.raws, *ev.Raw)
	case ev.Done != nil:
		f.dones = append(f.dones, companyDoneEv{ev.Company, *ev.Done})
	default:
		f.prog, f.progNew = scanProgress{company: ev.Company, dirs: ev.Dirs, found: ev.Found}, true
	}
	due := ev.Done != nil || len(f.raws) >= feedBatch || time.Since(f.lastSent) >= feedEvery
	f.mu.Unlock()
	if due {
		f.flush(false)
	}
}

// flush sends what is buffered. A message with results waits for room (or
// for the scan to be cancelled); a message with only progress is dropped when
// the screen is not keeping up.
func (f *scanFeed) flush(final bool) {
	f.mu.Lock()
	msg := scanBatchMsg{id: f.id, raws: f.raws, dones: f.dones, prog: f.prog, hasProg: f.progNew, finished: final}
	results := len(f.raws) > 0 || len(f.dones) > 0 || final
	if !results && !f.progNew {
		f.mu.Unlock()
		return
	}
	f.raws, f.dones, f.progNew = nil, nil, false
	f.lastSent = time.Now()
	f.mu.Unlock()
	if results {
		select {
		case f.ch <- msg:
		case <-f.ctx.Done():
		}
		return
	}
	select {
	case f.ch <- msg:
	default:
	}
}

// ---- the screen ----

// reposScreen is the repo browser behind the clone key: three tabs, Local
// (the repositories under the company folders), URL (the clone flow) and
// Online (listing the repositories of the accounts; not available yet).
type reposScreen struct {
	sh   *shared
	id   int
	tab  reposTab
	url  *cloneScreen
	w, h int
	spin spinner.Model

	// Local tab
	runID        int
	cancel       context.CancelFunc
	ctx          context.Context
	ch           chan tea.Msg
	scanning     bool
	cancelled    bool
	prog         scanProgress
	scans        []app.CompanyScan
	stale        bool // raw results arrived since the last classification
	lastClass    time.Time
	group        int // 0: all companies, i: company i-1
	problemsOnly bool
	filter       textinput.Model
	fmode        filterMode
	rows         []localRow
	sel, top     int
	listH        int
	lidx         app.LocalIndex // the local clones by RepoKey, for the Online tab

	// Online tab (see repos_online.go)
	prefer   *config.AccountRef // the account the tab starts on
	oacc     []app.OnlineAccount
	osel     int // index in oacc
	ostarted bool
	octx     context.Context
	ocancel  context.CancelFunc
	och      chan tea.Msg
	oarmed   bool // a waiter listens to och
	ofilter  textinput.Model
	ofmode   filterMode
	orows    []int // indices into the shown account's repos
	oview    map[string]*onlineView
	olistH   int
	odetail  onlineDetail
	odcache  map[string]onlineDetail // by account key and repo path, see refreshDetail
}

// newReposScreen opens the browser on the URL tab, the clone flow of old.
// prefer (optional) is the account the pick step starts on.
func newReposScreen(sh *shared, prefer *config.AccountRef) *reposScreen {
	u := newCloneScreen(sh, prefer)
	u.embedded = true
	fi := textinput.New()
	fi.Prompt = sh.km.keys.Label(keymap.Filter) + " "
	fi.PromptStyle = sh.st.Accent
	fi.CharLimit = 64
	of := textinput.New()
	of.Prompt = fi.Prompt
	of.PromptStyle = sh.st.Accent
	of.CharLimit = 64
	return &reposScreen{sh: sh, id: sh.id(), tab: tabURL, url: u, spin: sh.st.Spinner, filter: fi, ofilter: of, sel: -1, prefer: prefer}
}

func (s *reposScreen) Title() string { return tr("tui.repos.title") }

func (s *reposScreen) Resize(w, h int) {
	s.w, s.h = w, h
	s.url.Resize(w, h)
	s.filter.Width = maxInt(w-12, 8)
	s.ofilter.Width = s.filter.Width
	s.ensureVisible()
	s.ensureOnlineVisible()
}

// Init starts the clone flow's input and, unless the cache is good, the scan.
func (s *reposScreen) Init() tea.Cmd {
	return tea.Batch(s.url.Init(), s.openLocal())
}

// Close stops the scan and a clone in progress: the screen is leaving.
func (s *reposScreen) Close() tea.Cmd {
	s.stopScan()
	s.closeOnline()
	if s.url.cancel != nil {
		s.url.cancelled = true
		s.url.cancel()
	}
	return nil
}

// Capturing: the URL tab takes the keyboard while its input or a clone is
// active, the Local tab while the filter is typed.
func (s *reposScreen) Capturing() bool {
	switch s.tab {
	case tabURL:
		return s.url.Capturing()
	case tabLocal:
		return s.fmode == filterTyping
	case tabOnline:
		return s.ofmode == filterTyping
	}
	return false
}

// StepBack: esc on the Local tab stops a running scan first, then clears an
// applied filter. false lets the root leave the screen.
func (s *reposScreen) StepBack() bool {
	if s.tab == tabOnline {
		switch {
		case s.onlineBusy():
			s.cancelOnlineRuns()
			return true
		case s.ofmode == filterApplied:
			s.setOnlineFilter(filterOff, true)
			return true
		}
		return false
	}
	if s.tab != tabLocal {
		return false
	}
	switch {
	case s.scanning:
		s.cancelScan()
		return true
	case s.fmode == filterApplied:
		s.setFilter(filterOff, true)
		return true
	}
	return false
}

// tabLocked: switching tabs is off while a clone runs or is verified.
func (s *reposScreen) tabLocked() bool {
	return s.url.phase == clRunning || s.url.phase == clVerifying
}

func (s *reposScreen) setTab(t reposTab) tea.Cmd {
	if s.tab == t {
		return nil
	}
	if s.tab == tabURL && s.url.phase == clURL {
		s.url.input.Blur()
	}
	s.tab = t
	switch {
	case t == tabURL && s.url.phase == clURL:
		return s.url.input.Focus()
	case t == tabOnline:
		return s.openOnline()
	}
	return nil
}

func (s *reposScreen) cycleTab(back bool) tea.Cmd {
	n := int(s.tab) + 1
	if back {
		n = int(s.tab) - 1
	}
	return s.setTab(reposTab((n + int(numTabs)) % int(numTabs)))
}

func (s *reposScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return s.key(msg)
	case spinner.TickMsg:
		var cmds []tea.Cmd
		if s.scanning || s.onlineBusy() {
			var c tea.Cmd
			s.spin, c = s.spin.Update(msg)
			cmds = append(cmds, c)
			if s.stale {
				s.rebuild()
			}
		}
		_, c := s.url.Update(msg)
		return s, tea.Batch(append(cmds, c)...)
	case scanBatchMsg:
		return s.onBatch(msg)
	case cfgLoadedMsg, cfgChangedMsg:
		_, c := s.url.Update(msg)
		return s, tea.Batch(c, s.onConfig(), s.syncOnline())
	case onlineResolvedMsg, onlinePageMsg, onlineDoneMsg:
		return s.onOnlineMsg(msg)
	case onlineStaleMsg:
		return s.onOnlineStale(msg)
	case onlineCopyMsg:
		if msg.screen != s.id {
			return s, nil
		}
		if msg.err != nil {
			return s, say(levelWarn, tr("tui.repos.online.copy_failed", msg.url))
		}
		return s, say(levelOK, tr("tui.repos.online.copied", msg.url))
	case onlineOpenMsg:
		if msg.screen != s.id {
			return s, nil
		}
		if msg.err != nil {
			return s, say(levelWarn, tr("tui.repos.online.open_failed", msg.url))
		}
		return s, say(levelOK, tr("tui.repos.online.opened", msg.url))
	case repoClonedMsg:
		return s, s.reinspect(msg.dir, msg.company)
	case repoTouchedMsg:
		return s, s.reinspect(msg.dir, "")
	case repoInspectedMsg:
		if msg.id == s.id {
			s.onInspected(msg)
		}
		return s, nil
	case repoCopyMsg:
		if msg.id != s.id {
			return s, nil
		}
		if msg.err != nil {
			return s, say(levelWarn, tr("tui.repos.copy_failed", s.sh.svc.Env.Contract(msg.path)))
		}
		return s, say(levelOK, tr("tui.repos.copied", s.sh.svc.Env.Contract(msg.path)))
	case statusMsg, statusExpireMsg, focusMsg, planBuiltMsg:
		return s, nil
	}
	// everything else (cursor blink, the results of the clone) belongs to the
	// clone flow and, while the filter is typed, to the filter input
	var fcmd tea.Cmd
	if s.tab == tabLocal && s.fmode == filterTyping {
		s.filter, fcmd = s.filter.Update(msg)
	}
	if s.tab == tabOnline && s.ofmode == filterTyping {
		s.ofilter, fcmd = s.ofilter.Update(msg)
	}
	_, cmd := s.url.Update(msg)
	return s, tea.Batch(fcmd, cmd)
}

func (s *reposScreen) key(msg tea.KeyMsg) (Screen, tea.Cmd) {
	if msg.Type == tea.KeyTab || msg.Type == tea.KeyShiftTab {
		if s.tabLocked() || s.typingFilter() {
			return s, nil
		}
		return s, s.cycleTab(msg.Type == tea.KeyShiftTab)
	}
	// a paste lands in the URL input wherever the user is
	if msg.Paste && s.tab != tabURL && !s.tabLocked() && !s.typingFilter() {
		if s.url.phase != clURL {
			return s, nil
		}
		cmd := s.setTab(tabURL)
		_, c := s.url.Update(msg)
		return s, tea.Batch(cmd, c)
	}
	switch s.tab {
	case tabURL:
		_, cmd := s.url.Update(msg)
		return s, cmd
	case tabLocal:
		return s.localKey(msg)
	case tabOnline:
		return s.onlineKey(msg)
	}
	return s, nil
}

// typingFilter says whether a filter input of Local or Online has the keys.
func (s *reposScreen) typingFilter() bool {
	return (s.tab == tabLocal && s.fmode == filterTyping) || (s.tab == tabOnline && s.ofmode == filterTyping)
}

// ---- help ----

func (s *reposScreen) tabBinding() key.Binding {
	return key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", tr("tui.key.tab")))
}

func (s *reposScreen) Help() []key.Binding {
	switch s.tab {
	case tabURL:
		out := s.url.Help()
		if !s.tabLocked() {
			out = append(slices.Clone(out), s.tabBinding())
		}
		return out
	case tabOnline:
		return s.onlineHelp()
	}
	return s.localHelp()
}

// ContextHelp: the short help bar. The Local tab puts the check key first.
func (s *reposScreen) ContextHelp() []helpItem {
	switch s.tab {
	case tabLocal:
		return s.localContextHelp()
	case tabOnline:
		return s.onlineContextHelp()
	}
	var out []helpItem
	for _, b := range s.Help() {
		out = append(out, helpItem{b: b})
	}
	return out
}

// AlwaysHelp is row 2 of the help bar: the keys that work whatever repository
// is selected. The exit (esc) comes from ContextHelp. The Local tab mirrors the
// conditions of localHelp.
func (s *reposScreen) AlwaysHelp() []helpItem {
	km := s.sh.km
	var out []helpItem
	switch s.tab {
	case tabLocal:
		if s.fmode == filterTyping {
			return nil
		}
		if len(s.sh.cfg.Companies) > 0 {
			if s.repoCount() > 1 {
				out = append(out, helpItem{b: withDesc(km.Up, "↑↓", tr("tui.key.move")), prio: 1})
			}
			if len(s.scans) > 0 {
				out = append(out, helpItem{b: key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", tr("tui.key.group")))})
			}
			out = append(out,
				helpItem{b: km.Filter, prio: 2},
				helpItem{b: km.Problems, prio: 2},
				helpItem{b: relabel(km.Reload, tr("tui.key.rescan")), prio: 1})
		}
		return append(out, helpItem{b: s.tabBinding(), prio: 3})
	case tabOnline:
		return s.onlineAlwaysHelp()
	case tabURL:
		if s.tabLocked() {
			return nil
		}
		if s.url.phase == clURL && strings.TrimSpace(s.url.input.Value()) == "" {
			// nothing to do here yet: the tab key is the one thing to offer, so
			// it stays in row 1
			return nil
		}
	}
	return []helpItem{{b: s.tabBinding(), prio: 3}}
}

// repoCount is the number of repositories the Local tab can select.
func (s *reposScreen) repoCount() int {
	n := 0
	for _, r := range s.rows {
		if r.kind == rowRepo {
			n++
		}
	}
	return n
}

// ---- tab bar and view ----

func (s *reposScreen) tabLabel(t reposTab) string {
	switch t {
	case tabLocal:
		l := tr("tui.repos.tab.local")
		if n, ok := s.localCount(); ok {
			l += " " + groupDigits(n)
		} else if s.scanning || (s.sh.loaded && len(s.sh.cfg.Companies) > 0 && s.sh.repos == nil) {
			l += " …"
		}
		return l
	case tabURL:
		return tr("tui.repos.tab.url")
	}
	return tr("tui.repos.tab.online")
}

// tabBar draws the three tabs, the first line inside the panel of the active
// one; the active tab carries the "▸" mark. Up to 50 columns the gaps shrink.
func (s *reposScreen) tabBar() string {
	st := s.sh.st
	narrow := s.w <= 50
	sep := "   "
	if narrow {
		sep = "  "
	}
	var parts []string
	for t := reposTab(0); t < numTabs; t++ {
		label := s.tabLabel(t)
		switch {
		case t != s.tab:
			parts = append(parts, st.Muted.Render(label))
		case narrow:
			parts = append(parts, st.Accent.Render("▸")+st.Bold.Render(label))
		default:
			parts = append(parts, st.Accent.Render("▸ ")+st.Bold.Render(label))
		}
	}
	return trunc(strings.Join(parts, sep), maxInt(s.w-4, 4))
}

func (s *reposScreen) View() string {
	var body string
	switch s.tab {
	case tabLocal:
		body = s.localView()
	case tabURL:
		s.url.header = s.tabBar()
		body = s.url.View()
	default:
		body = s.onlineView()
	}
	return fitHeight(body, s.h)
}

// ---- helpers ----

// groupDigits writes n with a space between thousands: 20 000.
func groupDigits(n int) string {
	neg := n < 0
	if neg {
		n = -n
	}
	out := ""
	for n >= 1000 {
		out = fmt.Sprintf(" %03d", n%1000) + out
		n /= 1000
	}
	out = strconv.Itoa(n) + out
	if neg {
		return "-" + out
	}
	return out
}

// companyOf finds the company whose base directory holds dir ("" if none).
func companyOf(e env.Env, cfg config.Config, dir string) string {
	for _, c := range cfg.Companies {
		if base := e.Expand(c.BaseDir); base != "" && env.IsUnder(dir, base) {
			return c.Slug
		}
	}
	return ""
}

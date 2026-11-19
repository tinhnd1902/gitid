package tui

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/keymap"
	"github.com/tinhnd1902/gitid/internal/online"
	"github.com/tinhnd1902/gitid/internal/scan"
)

// This file is the Online tab of the repo browser: the repositories each
// account can see on its platform. The token lookup and the HTTP requests run
// in goroutines (one per account) that report through a channel; Update only
// handles their messages. No token ever reaches this file: the app layer
// hands out the source of a token ("gh", "env", ...) and the repositories.

// onlinePhase is where the listing of one account stands.
type onlinePhase int

const (
	opIdle      onlinePhase = iota // nothing started
	opResolving                    // looking for the token
	opFetching                     // pages are arriving
	opDone                         // every page fetched
	opFailed                       // stopped by an error (the pages before it are kept)
	opNoToken                      // no source has a token
	opCancelled                    // stopped by the user or by leaving the screen
)

// onlineState is the session cache of one account: what was fetched, where
// the token came from and how it ended. It holds no secret.
type onlineState struct {
	key    string
	src    online.Source
	note   string
	pwd    bool // the token git handed over looks like a password
	phase  onlinePhase
	runID  int
	cancel context.CancelFunc

	repos     []online.Repo
	keys      []string // app.RepoKey of each repo ("" when it cannot be cloned)
	keyset    map[string]struct{}
	pages     int
	login     string
	err       error
	truncated bool
	warnings  []string
}

func (st *onlineState) reset() {
	st.repos, st.keys, st.keyset = nil, nil, map[string]struct{}{}
	st.pages, st.login, st.err, st.truncated, st.warnings = 0, "", nil, false, nil
	st.src, st.note, st.pwd = online.SourceNone, "", false
}

func (st *onlineState) running() bool { return st.phase == opResolving || st.phase == opFetching }

// ---- messages ----

// The goroutine of an account sends these. screen is the id of the browser
// that started the run, run the id of the run: a message of an older run is
// ignored.
type (
	onlineResolvedMsg struct {
		screen, run int
		key         string
		info        app.TokenInfo
		err         error
	}
	onlinePageMsg struct {
		screen, run int
		key         string
		page        online.Page
	}
	onlineDoneMsg struct {
		screen, run int
		key         string
		res         app.OnlineResult
		err         error
	}
	// onlineStaleMsg: the answer of the "did config.yaml change?" check made
	// before a clone starts.
	onlineStaleMsg struct {
		screen  int
		oc      app.OnlineClone
		ref     config.AccountRef
		changed bool
		err     error
	}
	onlineCopyMsg struct {
		screen int
		url    string
		err    error
	}
	onlineOpenMsg struct {
		screen int
		url    string
		err    error
	}
)

func (onlineResolvedMsg) bgMsg() {}
func (onlinePageMsg) bgMsg()     {}
func (onlineDoneMsg) bgMsg()     {}

// onlineView is the cursor of one account's list.
type onlineView struct {
	path string // FullPath of the selected repository
	sel  int    // index in rows
	top  int
}

// onlineDetail is what the detail block shows about the selected repository.
type onlineDetail struct {
	repo online.Repo
	ok   bool
	oc   app.OnlineClone
	url  string // the clone URL as the account renders it
	err  error
}

// ---- lifecycle ----

// waitOnline waits for the next message of any run of this screen; it gives
// up (no message) when the screen is closed.
func waitOnline(ctx context.Context, ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		select {
		case m := <-ch:
			return m
		case <-ctx.Done():
			return nil
		}
	}
}

// armOnline makes sure one waiter is listening to the channel.
func (s *reposScreen) armOnline() tea.Cmd {
	if s.octx == nil {
		s.octx, s.ocancel = context.WithCancel(context.Background())
		s.och = make(chan tea.Msg, 64)
	}
	if s.oarmed {
		return nil
	}
	s.oarmed = true
	return waitOnline(s.octx, s.och)
}

// syncOnline reads the accounts of the configuration, drops what belongs to
// accounts that are gone or changed (platform, host or username), and, when
// the tab was opened before, starts the accounts that have no listing yet.
func (s *reposScreen) syncOnline() tea.Cmd {
	cfg := s.sh.cfg
	s.oacc = s.sh.svc.OnlineAccounts(cfg)
	s.dropDetailCache()
	keep := map[string]bool{}
	for _, a := range s.oacc {
		keep[a.Key] = true
	}
	for k, st := range s.sh.online {
		if !keep[k] {
			s.cancelRun(st)
			delete(s.sh.online, k)
			delete(s.oview, k)
		}
	}
	s.sh.svc.KeepTokens(cfg)
	s.osel = clamp(s.osel, 0, len(s.oacc)-1)
	var cmd tea.Cmd
	if s.ostarted {
		cmd = s.startOnline()
	}
	s.rebuildOnline()
	return cmd
}

// openOnline runs when the Online tab is shown: nothing before that touches
// a token or the network.
func (s *reposScreen) openOnline() tea.Cmd {
	if !s.sh.loaded {
		return nil
	}
	s.dropDetailCache()
	if !s.ostarted {
		s.oacc = s.sh.svc.OnlineAccounts(s.sh.cfg)
		if s.prefer != nil {
			for i, a := range s.oacc {
				if a.Ref.String() == s.prefer.String() {
					s.osel = i
				}
			}
		}
	}
	s.ostarted = true
	cmd := s.startOnline()
	s.rebuildOnline()
	return cmd
}

// startOnline starts every account that has no finished listing. The app
// layer bounds how many look for a token and fetch at the same time.
func (s *reposScreen) startOnline() tea.Cmd {
	var cmds []tea.Cmd
	for _, a := range s.oacc {
		if !a.Supported {
			continue
		}
		st := s.stateFor(a)
		if st.phase == opDone || st.running() {
			continue
		}
		cmds = append(cmds, s.startFetch(a, false))
	}
	return tea.Batch(cmds...)
}

func (s *reposScreen) stateFor(a app.OnlineAccount) *onlineState {
	if s.sh.online == nil {
		s.sh.online = map[string]*onlineState{}
	}
	st := s.sh.online[a.Key]
	if st == nil {
		st = &onlineState{key: a.Key}
		st.reset()
		s.sh.online[a.Key] = st
	}
	return st
}

// startFetch (re)starts the listing of an account. With forget the remembered
// token is dropped first, so the sources are asked again.
func (s *reposScreen) startFetch(a app.OnlineAccount, forget bool) tea.Cmd {
	st := s.stateFor(a)
	s.cancelRun(st)
	if forget {
		s.sh.svc.ForgetToken(a.Key)
	}
	arm := s.armOnline()
	ctx, cancel := context.WithCancel(s.octx)
	st.reset()
	st.cancel = cancel
	st.runID = s.sh.id()
	st.phase = opResolving
	svc, cfg, ref, key, sid, run, ch := s.sh.svc, s.sh.cfg, a.Ref.String(), a.Key, s.id, st.runID, s.och
	send := func(m tea.Msg) {
		select {
		case ch <- m:
		case <-ctx.Done():
		}
	}
	go func() {
		info, err := svc.ResolveSource(ctx, cfg, ref)
		send(onlineResolvedMsg{screen: sid, run: run, key: key, info: info, err: err})
		if err != nil || info.Source == online.SourceNone {
			return
		}
		res, err := svc.ListOnline(ctx, cfg, ref, func(p online.Page) {
			send(onlinePageMsg{screen: sid, run: run, key: key, page: p})
		})
		send(onlineDoneMsg{screen: sid, run: run, key: key, res: res, err: err})
	}()
	s.rebuildOnline()
	return tea.Batch(arm, s.spin.Tick)
}

// cancelRun stops the run of st, if any; the pages already there stay.
func (s *reposScreen) cancelRun(st *onlineState) {
	if st.cancel != nil {
		st.cancel()
		st.cancel = nil
	}
	if st.running() {
		st.phase = opCancelled
	}
	st.runID = 0
}

// cancelOnlineRuns is esc on the Online tab while something is running.
func (s *reposScreen) cancelOnlineRuns() {
	for _, st := range s.sh.online {
		if st.running() {
			s.cancelRun(st)
		}
	}
	s.rebuildOnline()
}

// onlineBusy says whether any account is still looking for a token or
// fetching.
func (s *reposScreen) onlineBusy() bool {
	for _, st := range s.sh.online {
		if st.running() {
			return true
		}
	}
	return false
}

// closeOnline stops every run of this screen (the screen is leaving).
func (s *reposScreen) closeOnline() {
	for _, st := range s.sh.online {
		if st.running() || st.cancel != nil {
			s.cancelRun(st)
		}
	}
	if s.ocancel != nil {
		s.ocancel()
	}
}

func (s *reposScreen) stateOfKey(key string, run int) *onlineState {
	st := s.sh.online[key]
	if st == nil || st.runID == 0 || st.runID != run {
		return nil
	}
	return st
}

func (s *reposScreen) onOnlineMsg(msg tea.Msg) (Screen, tea.Cmd) {
	rearm := func() tea.Cmd {
		s.oarmed = false
		return s.armOnline()
	}
	switch m := msg.(type) {
	case onlineResolvedMsg:
		if m.screen != s.id {
			return s, nil
		}
		defer s.rebuildOnline()
		st := s.stateOfKey(m.key, m.run)
		if st == nil {
			return s, rearm()
		}
		st.src, st.note, st.pwd = m.info.Source, m.info.Note, m.info.LooksLikePassword
		switch {
		case m.err != nil:
			s.cancelRun(st)
		case m.info.Source == online.SourceNone:
			st.phase = opNoToken
			st.cancel = nil
		default:
			st.phase = opFetching
		}
		return s, rearm()
	case onlinePageMsg:
		if m.screen != s.id {
			return s, nil
		}
		st := s.stateOfKey(m.key, m.run)
		if st == nil {
			return s, rearm()
		}
		st.phase, st.pages = opFetching, m.page.N
		for _, r := range m.page.Repos {
			k := s.sh.svc.OnlineRepoKey(s.sh.cfg, r)
			st.repos = append(st.repos, r)
			st.keys = append(st.keys, k)
			if k != "" {
				st.keyset[k] = struct{}{}
			}
		}
		for _, w := range m.page.Warnings {
			if !slices.Contains(st.warnings, w) {
				st.warnings = append(st.warnings, w)
			}
		}
		s.rebuildOnline()
		return s, rearm()
	case onlineDoneMsg:
		if m.screen != s.id {
			return s, nil
		}
		st := s.stateOfKey(m.key, m.run)
		if st == nil {
			return s, rearm()
		}
		st.cancel, st.runID = nil, 0
		st.src, st.login, st.truncated, st.pages, st.pwd = m.res.Source, m.res.Login, m.res.Truncated, m.res.Pages, m.res.LooksLikePassword
		for _, w := range m.res.Warnings {
			if !slices.Contains(st.warnings, w) {
				st.warnings = append(st.warnings, w)
			}
		}
		switch {
		case m.err == nil:
			st.phase = opDone
		case errors.Is(m.err, context.Canceled):
			st.phase = opCancelled
		default:
			st.phase, st.err = opFailed, m.err
		}
		s.rebuildOnline()
		return s, rearm()
	}
	return s, nil
}

// ---- accounts, rows and the detail ----

// curAccount is the account the tab shows.
func (s *reposScreen) curAccount() (app.OnlineAccount, *onlineState, bool) {
	if s.osel < 0 || s.osel >= len(s.oacc) {
		return app.OnlineAccount{}, nil, false
	}
	a := s.oacc[s.osel]
	if !a.Supported {
		return a, nil, true
	}
	return a, s.stateFor(a), true
}

func (s *reposScreen) curView() *onlineView {
	a, _, ok := s.curAccount()
	if !ok {
		return &onlineView{}
	}
	if s.oview == nil {
		s.oview = map[string]*onlineView{}
	}
	v := s.oview[a.Key]
	if v == nil {
		v = &onlineView{sel: -1}
		s.oview[a.Key] = v
	}
	return v
}

func (s *reposScreen) onlineTerm() string {
	if s.ofmode == filterOff {
		return ""
	}
	return strings.TrimSpace(s.ofilter.Value())
}

// rebuildOnline filters the repositories of the shown account and keeps the
// selection on the same repository.
func (s *reposScreen) rebuildOnline() {
	if s.sh == nil {
		return
	}
	_, st, ok := s.curAccount()
	v := s.curView()
	if !ok || st == nil {
		s.orows = nil
		v.sel, v.top = -1, 0
		s.refreshDetail()
		return
	}
	var rows []int
	if term := s.onlineTerm(); term != "" {
		targets := make([]string, len(st.repos))
		for i, r := range st.repos {
			targets[i] = r.FullPath
		}
		for _, rk := range treeFilter(term, targets) {
			rows = append(rows, rk.Index)
		}
	} else {
		rows = make([]int, len(st.repos))
		for i := range rows {
			rows[i] = i
		}
	}
	s.orows = rows
	v.sel = -1
	for i, ri := range rows {
		if st.repos[ri].FullPath == v.path && v.path != "" {
			v.sel = i
			break
		}
	}
	if v.sel < 0 && len(rows) > 0 {
		v.sel = 0
	}
	s.syncSel(v, st)
	s.ensureOnlineVisible()
	s.refreshDetail()
}

func (s *reposScreen) syncSel(v *onlineView, st *onlineState) {
	if v.sel >= 0 && v.sel < len(s.orows) {
		v.path = st.repos[s.orows[v.sel]].FullPath
	} else {
		v.path = ""
	}
}

func (s *reposScreen) selectedOnline() (online.Repo, bool) {
	_, st, ok := s.curAccount()
	v := s.curView()
	if !ok || st == nil || v.sel < 0 || v.sel >= len(s.orows) {
		return online.Repo{}, false
	}
	return st.repos[s.orows[v.sel]], true
}

// refreshDetail prepares the clone request of the selected repository. The
// request plans the clone, which looks at the target folder on disk (an
// Lstat), so the answer is kept per repository: moving the cursor back and
// forth, or typing in the filter, does not touch the disk again. The cache is
// dropped (dropDetailCache) whenever what the answer depends on changes: the
// local scan, the configuration, a refetch, and before enter acts on it.
func (s *reposScreen) refreshDetail() {
	s.odetail = onlineDetail{}
	a, _, ok := s.curAccount()
	r, sel := s.selectedOnline()
	if !ok || !sel {
		return
	}
	k := a.Key + "\x00" + r.FullPath
	if d, hit := s.odcache[k]; hit {
		s.odetail = d
		return
	}
	d := onlineDetail{repo: r, ok: true}
	d.oc, d.err = s.sh.svc.OnlineCloneRequest(s.sh.cfg, a.Ref.String(), r, s.lidx)
	if u, err := s.sh.svc.OnlineCloneURL(s.sh.cfg, a.Ref.String(), r); err == nil {
		d.url = u
	}
	if s.odcache == nil {
		s.odcache = map[string]onlineDetail{}
	}
	s.odcache[k] = d
	s.odetail = d
}

// dropDetailCache forgets the prepared clone requests.
func (s *reposScreen) dropDetailCache() { s.odcache = nil }

// localClones are the local repositories that match an online one.
func (s *reposScreen) localClones(key string) []app.LocalRepo {
	if key == "" {
		return nil
	}
	return s.lidx[key]
}

// onlineEnter is what enter does on the selected repository, if anything.
type onlineEnterKind int

const (
	enterNone onlineEnterKind = iota
	enterClone
	enterCheck
)

func (s *reposScreen) onlineEnter() onlineEnterKind {
	d := s.odetail
	if !d.ok {
		return enterNone
	}
	if len(d.oc.AlreadyCloned) > 0 {
		if _, ok := checkTarget(d.oc.AlreadyCloned); ok {
			return enterCheck
		}
		return enterNone
	}
	if d.err != nil {
		return enterNone
	}
	return enterClone
}

// checkTarget is the local clone enter opens: the first that is not bare.
func checkTarget(clones []app.LocalRepo) (app.LocalRepo, bool) {
	for _, c := range clones {
		if c.Kind != scan.KindBare {
			return c, true
		}
	}
	return app.LocalRepo{}, false
}

// ---- keys ----

func (s *reposScreen) setOnlineFilter(mode filterMode, clear bool) {
	s.ofmode = mode
	if clear {
		s.ofilter.SetValue("")
	}
	if mode != filterTyping {
		s.ofilter.Blur()
	}
	s.rebuildOnline()
}

func (s *reposScreen) onlineKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
	km := s.sh.km
	if s.ofmode == filterTyping {
		return s.onlineFilterKey(msg)
	}
	a, st, has := s.curAccount()
	switch {
	case key.Matches(msg, km.Filter):
		if st == nil || len(st.repos) == 0 {
			return s, nil
		}
		s.ofmode = filterTyping
		s.ofilter.Prompt = km.keys.Label(keymap.Filter) + " "
		s.rebuildOnline()
		return s, s.ofilter.Focus()
	case key.Matches(msg, km.Reload):
		if has && a.Supported {
			s.dropDetailCache()
			return s, s.startFetch(a, true)
		}
	case msg.Type == tea.KeyEnter:
		return s, s.onlineEnterKey()
	case key.Matches(msg, km.Copy):
		if s.odetail.ok && s.odetail.url != "" {
			svc, url, id := s.sh.svc, s.odetail.url, s.id
			return s, func() tea.Msg {
				ctx, cancel := ctxTimeout(shortTimeout)
				defer cancel()
				return onlineCopyMsg{screen: id, url: url, err: svc.CopyText(ctx, url)}
			}
		}
	case key.Matches(msg, km.Open):
		if s.odetail.ok && s.odetail.repo.WebURL != "" {
			svc, url, id := s.sh.svc, s.odetail.repo.WebURL, s.id
			return s, func() tea.Msg {
				ctx, cancel := ctxTimeout(shortTimeout)
				defer cancel()
				return onlineOpenMsg{screen: id, url: url, err: svc.OpenURL(ctx, url)}
			}
		}
	case msg.Type == tea.KeyLeft:
		s.moveAccount(-1)
	case msg.Type == tea.KeyRight:
		s.moveAccount(1)
	case key.Matches(msg, km.Up):
		s.moveOnline(-1)
	case key.Matches(msg, km.Down):
		s.moveOnline(1)
	case key.Matches(msg, km.PageUp):
		s.moveOnline(-maxInt(s.olistH-1, 1))
	case key.Matches(msg, km.PageDown):
		s.moveOnline(maxInt(s.olistH-1, 1))
	case msg.Type == tea.KeyHome:
		s.setOnlineSel(0)
	case msg.Type == tea.KeyEnd:
		s.setOnlineSel(len(s.orows) - 1)
	}
	return s, nil
}

func (s *reposScreen) onlineFilterKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		s.setOnlineFilter(filterOff, true)
		return s, nil
	case tea.KeyEnter:
		if strings.TrimSpace(s.ofilter.Value()) == "" {
			s.setOnlineFilter(filterOff, true)
		} else {
			s.setOnlineFilter(filterApplied, false)
		}
		return s, nil
	case tea.KeyUp, tea.KeyCtrlP:
		s.moveOnline(-1)
		return s, nil
	case tea.KeyDown, tea.KeyCtrlN:
		s.moveOnline(1)
		return s, nil
	}
	var cmd tea.Cmd
	s.ofilter, cmd = s.ofilter.Update(msg)
	s.rebuildOnline()
	return s, cmd
}

// onlineEnterKey: clone the selected repository (after checking that
// config.yaml is still what this screen loaded), or check its local clone.
func (s *reposScreen) onlineEnterKey() tea.Cmd {
	a, _, ok := s.curAccount()
	if !ok {
		return nil
	}
	// enter acts on the disk as it is now, not on the cached look
	s.dropDetailCache()
	s.refreshDetail()
	switch s.onlineEnter() {
	case enterCheck:
		c, _ := checkTarget(s.odetail.oc.AlreadyCloned)
		return s.sh.pushNow(s.checkScreenFor(c, false))
	case enterClone:
		svc, cfg, oc, id, ref := s.sh.svc, s.sh.cfg, s.odetail.oc, s.id, a.Ref
		return func() tea.Msg {
			_, changed, err := svc.ChangedSince(cfg)
			return onlineStaleMsg{screen: id, oc: oc, ref: ref, changed: changed, err: err}
		}
	}
	return nil
}

func (s *reposScreen) onOnlineStale(m onlineStaleMsg) (Screen, tea.Cmd) {
	if m.screen != s.id {
		return s, nil
	}
	if m.err != nil {
		return s, say(levelErr, errText(m.err))
	}
	if m.changed {
		return s, tea.Batch(say(levelWarn, tr("tui.repos.online.stale")), reload())
	}
	ref := m.ref
	cs := newCloneScreenFor(s.sh, &ref, m.oc.Request.URL, m.oc.Request.Dir)
	return s, s.sh.pushNow(cs)
}

func (s *reposScreen) moveAccount(d int) {
	n := len(s.oacc)
	if n < 2 {
		return
	}
	s.osel = (s.osel + d + n) % n
	s.rebuildOnline()
}

func (s *reposScreen) moveOnline(d int) { s.setOnlineSel(s.curView().sel + d) }

func (s *reposScreen) setOnlineSel(i int) {
	v := s.curView()
	_, st, _ := s.curAccount()
	if len(s.orows) == 0 || st == nil {
		return
	}
	v.sel = clamp(i, 0, len(s.orows)-1)
	s.syncSel(v, st)
	s.ensureOnlineVisible()
	s.refreshDetail()
}

// ---- help ----

func (s *reposScreen) onlineHelp() []key.Binding {
	km := s.sh.km
	if s.ofmode == filterTyping {
		return []key.Binding{withDesc(km.Enter, "enter", tr("tui.key.filter_keep")), withDesc(km.Back, "esc", tr("tui.key.filter_clear"))}
	}
	var out []key.Binding
	a, st, has := s.curAccount()
	switch s.onlineEnter() {
	case enterClone:
		out = append(out, withDesc(km.Enter, "enter", tr("tui.key.clone")))
	case enterCheck:
		out = append(out, withDesc(km.Enter, "enter", tr("tui.key.check")))
	}
	if s.odetail.ok && s.odetail.url != "" {
		out = append(out, relabel(km.Copy, tr("tui.key.copy_url")))
	}
	if s.odetail.ok && s.odetail.repo.WebURL != "" {
		out = append(out, relabel(km.Open, tr("tui.key.open_page")))
	}
	if st != nil && len(st.repos) > 0 {
		out = append(out, km.Filter)
	}
	if has && a.Supported {
		out = append(out, relabel(km.Reload, tr("tui.key.refetch")))
	}
	if len(s.oacc) > 1 {
		out = append(out, key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", tr("tui.key.account"))))
	}
	out = append(out, s.tabBinding())
	if s.onlineBusy() {
		return append(out, withDesc(km.Back, "esc", tr("tui.key.stop_fetch")))
	}
	return append(out, withDesc(km.Back, "esc", tr("tui.key.back")))
}

func (s *reposScreen) onlineContextHelp() []helpItem {
	km := s.sh.km
	if s.ofmode == filterTyping {
		return []helpItem{
			{b: withDesc(km.Enter, "enter", tr("tui.key.filter_keep")), primary: true},
			{b: withDesc(km.Back, "esc", tr("tui.key.filter_clear"))},
			{b: withDesc(km.Up, "↑/↓", tr("tui.key.move"))},
		}
	}
	var out []helpItem
	_, st, _ := s.curAccount()
	for i, b := range s.onlineHelp() {
		it := helpItem{b: b, primary: i == 0 && s.onlineEnter() != enterNone}
		switch {
		case s.onlineEnter() != enterNone:
		case st != nil && st.running():
			// nothing to do but wait, or stop
			it.primary = isExit(s.sh.km, b)
		case st != nil && (st.phase == opNoToken || st.phase == opFailed || st.phase == opCancelled) && slices.Equal(b.Keys(), s.sh.km.Reload.Keys()):
			// add the token (or fix it), then ask again
			it.b = relabel(b, tr("tui.key.retry"))
			it.primary = true
		}
		out = append(out, it)
	}
	return out
}

func (s *reposScreen) onlineAlwaysHelp() []helpItem {
	km := s.sh.km
	if s.ofmode == filterTyping {
		return nil
	}
	var out []helpItem
	a, st, has := s.curAccount()
	if len(s.orows) > 1 {
		out = append(out, helpItem{b: withDesc(km.Up, "↑↓", tr("tui.key.move")), prio: 1})
	}
	if len(s.oacc) > 1 {
		out = append(out, helpItem{b: key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", tr("tui.key.account")))})
	}
	if st != nil && len(st.repos) > 0 {
		out = append(out, helpItem{b: km.Filter, prio: 2})
	}
	if has && a.Supported {
		out = append(out, helpItem{b: relabel(km.Reload, tr("tui.key.refetch")), prio: 2})
	}
	return append(out, helpItem{b: s.tabBinding(), prio: 3})
}

// ---- geometry ----

type onlineGeom struct{ iw, ih, listH, detail int }

func (s *reposScreen) onlineStateLines(iw int) []string {
	_, st, ok := s.curAccount()
	if !ok {
		return nil
	}
	return s.stateLines(st, iw)
}

func (s *reposScreen) ogeom() onlineGeom {
	fr := s.sh.st.Panel
	g := onlineGeom{iw: s.w - fr.GetHorizontalFrameSize(), ih: (s.h - 1) - fr.GetVerticalFrameSize()}
	head := 1
	if s.ofmode != filterOff {
		head++
	}
	state := len(s.onlineStateLines(g.iw))
	// the list keeps at least three rows when the panel has them
	state = minInt(state, maxInt(g.ih-head-3, 1))
	head += state
	avail := g.ih - head
	switch {
	case avail >= 11:
		g.detail = 5
	case avail >= 7:
		g.detail = 3
	}
	if len(s.orows) == 0 {
		// no divider and no hint to select with nothing to select
		g.detail = 0
	}
	g.listH = maxInt(avail-g.detail, 1)
	return g
}

func (s *reposScreen) ensureOnlineVisible() {
	if s.sh == nil || s.h == 0 {
		return
	}
	g := s.ogeom()
	s.olistH = g.listH
	v := s.curView()
	if v.sel < 0 {
		v.top = 0
		return
	}
	if v.sel < v.top {
		v.top = v.sel
	}
	if v.sel >= v.top+g.listH {
		v.top = v.sel - g.listH + 1
	}
	v.top = clamp(v.top, 0, maxInt(len(s.orows)-g.listH, 0))
}

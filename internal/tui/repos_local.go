package tui

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/keymap"
	"github.com/tinhnd1902/gitid/internal/scan"
)

// This file is the Local tab of the repo browser: the scan, the rows and the
// view. Update never does I/O: the walk and the git calls run in a goroutine
// (scanFeed) and arrive as scanBatchMsg.

// ---- scan lifecycle ----

// openLocal prepares the Local tab when the screen opens: the cached result
// when it is complete for the same folders, else a scan. It needs the
// configuration; without it the first cfgLoadedMsg does the same.
func (s *reposScreen) openLocal() tea.Cmd {
	cfg := s.sh.cfg
	if !s.sh.loaded {
		return nil
	}
	if len(cfg.Companies) == 0 {
		s.rebuild()
		return nil
	}
	if sess := s.sh.repos; sess != nil && sess.complete && slices.Equal(sess.baseDirs, s.sh.svc.BaseDirs(cfg)) {
		s.rebuild()
		return nil
	}
	return s.startScan()
}

// onConfig reacts to a new configuration: the same folders are classified
// again from the cached data (no I/O); other folders need a new scan.
func (s *reposScreen) onConfig() tea.Cmd {
	cfg := s.sh.cfg
	if !s.sh.loaded {
		return nil
	}
	if len(cfg.Companies) == 0 {
		s.stopScan()
		s.sh.repos = nil
		s.rebuild()
		return nil
	}
	sess := s.sh.repos
	if sess == nil {
		if s.scanning {
			return nil
		}
		return s.startScan()
	}
	if !slices.Equal(sess.baseDirs, s.sh.svc.BaseDirs(cfg)) {
		return s.startScan()
	}
	if !s.scanning && !sess.complete && !s.cancelled {
		return s.startScan() // the cache was partial (an earlier scan was stopped)
	}
	s.rebuild()
	return nil
}

// startScan starts a fresh scan (the cache is dropped).
func (s *reposScreen) startScan() tea.Cmd {
	s.stopScan()
	cfg := s.sh.cfg
	if !s.sh.loaded || len(cfg.Companies) == 0 {
		s.rebuild()
		return nil
	}
	svc, opts := s.sh.svc, s.sh.scanOpts
	s.sh.repos = newRepoSession(svc.BaseDirs(cfg))
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel, s.ctx = cancel, ctx
	s.runID = s.sh.id()
	s.scanning, s.cancelled, s.prog = true, false, scanProgress{}
	ch := make(chan tea.Msg, 16)
	s.ch = ch
	feed := &scanFeed{ctx: ctx, id: s.runID, ch: ch}
	// a ticker sends what the events left in the buffer; it is stopped (and
	// waited for) before the final message, so nothing arrives after it
	stop, ticked := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(ticked)
		t := time.NewTicker(feedEvery)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				feed.flush(false)
			case <-stop:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		svc.ScanLocal(ctx, cfg, opts, feed.emit)
		close(stop)
		<-ticked
		feed.flush(true)
	}()
	s.group = clamp(s.group, 0, len(cfg.Companies))
	s.rebuild()
	return tea.Batch(waitFeed(ctx, ch), s.spin.Tick)
}

// waitFeed waits for the next message of a scan; it gives up (no message)
// when the scan is cancelled, so no goroutine waits for a stopped scan.
func waitFeed(ctx context.Context, ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		select {
		case m := <-ch:
			return m
		case <-ctx.Done():
			return nil
		}
	}
}

// stopScan cancels the running scan, if any. Messages it still sends are
// ignored (the run id is cleared).
func (s *reposScreen) stopScan() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.scanning && s.sh.repos != nil {
		s.sh.repos.complete = false
	}
	s.scanning = false
	s.runID = 0
}

// cancelScan is stopScan for the user's esc: the partial result stays and the
// screen says so.
func (s *reposScreen) cancelScan() {
	s.stopScan()
	s.cancelled = true
	s.rebuild()
}

func (s *reposScreen) onBatch(msg scanBatchMsg) (Screen, tea.Cmd) {
	sess := s.sh.repos
	if s.runID == 0 || msg.id != s.runID || sess == nil {
		return s, nil
	}
	for _, r := range msg.raws {
		sess.put(r)
	}
	for _, d := range msg.dones {
		sess.done[d.company] = d.done
	}
	if msg.hasProg {
		s.prog = msg.prog
	}
	if msg.finished {
		s.scanning, s.cancel = false, nil
		sess.complete = sess.finished(s.sh.cfg)
		s.rebuild()
		return s, nil
	}
	switch {
	case len(msg.dones) > 0 || len(msg.raws) > 0 && time.Since(s.lastClass) >= classEvery:
		s.rebuild()
	case len(msg.raws) > 0:
		s.stale = true
	}
	return s, waitFeed(s.ctx, s.ch)
}

// reinspect reads one repository again (after a clone or a fix).
func (s *reposScreen) reinspect(dir, company string) tea.Cmd {
	if s.sh.repos == nil || !s.sh.loaded {
		return nil
	}
	if company == "" {
		company = companyOf(s.sh.svc.Env, s.sh.cfg, dir)
	}
	if company == "" {
		return nil
	}
	svc, id := s.sh.svc, s.id
	return func() tea.Msg {
		ctx, cancel := ctxTimeout(shortTimeout)
		defer cancel()
		return repoInspectedMsg{id: id, company: company, raw: svc.InspectRepo(ctx, company, dir)}
	}
}

func (s *reposScreen) onInspected(msg repoInspectedMsg) {
	sess := s.sh.repos
	if sess == nil {
		return
	}
	sess.put(msg.raw)
	// a folder that did not exist before now does
	if d, ok := sess.done[msg.company]; ok && (d.State == app.ScanMissing || d.State == app.ScanNotDir) {
		sess.done[msg.company] = app.CompanyDone{State: app.ScanOK, Stats: d.Stats}
	}
	s.rebuild()
}

// ---- rows ----

type rowKind int

const (
	rowHeader rowKind = iota
	rowNotice
	rowRepo
)

// localRow is one line of the list: a company header, a notice under it, or a
// repository (scan and repo index into reposScreen.scans).
type localRow struct {
	kind       rowKind
	scan, repo int
	text       string // notice text
	warn       bool
}

// localCount is the number of repositories found in total, once there is a
// classified result for at least one company.
func (s *reposScreen) localCount() (int, bool) {
	if len(s.sh.cfg.Companies) == 0 || s.sh.repos == nil || s.scanning {
		return 0, false
	}
	n := 0
	for _, c := range s.scans {
		n += len(c.Repos)
	}
	return n, true
}

// rebuild classifies the cached raw results against the current
// configuration and rebuilds the rows.
func (s *reposScreen) rebuild() {
	prev := s.selPath()
	cfg := s.sh.cfg
	s.scans = nil
	if sess := s.sh.repos; sess != nil && len(cfg.Companies) > 0 {
		s.scans = app.ClassifyLocal(cfg, s.sh.svc.Env, sess.rawList(cfg), sess.done)
	}
	s.stale, s.lastClass = false, time.Now()
	s.lidx = app.IndexLocal(s.scans)
	s.dropDetailCache()
	s.buildRows(prev)
	if s.ostarted {
		s.rebuildOnline() // the clones decide which online repos show as cloned
	}
}

func (s *reposScreen) selPath() string {
	if r, ok := s.selected(); ok {
		return r.Path
	}
	return ""
}

func (s *reposScreen) selected() (app.LocalRepo, bool) {
	if s.sel < 0 || s.sel >= len(s.rows) || s.rows[s.sel].kind != rowRepo {
		return app.LocalRepo{}, false
	}
	row := s.rows[s.sel]
	return s.scans[row.scan].Repos[row.repo], true
}

// repoTarget is the text the filter looks at: the path below the company
// folder, the account and the company (not the remote: a fuzzy match on a
// long URL finds too much).
func (s *reposScreen) repoTarget(c app.CompanyScan, r app.LocalRepo) string {
	return strings.Join([]string{s.sh.svc.RelPath(c.BaseDir, r.Path), r.Account, c.Slug}, " ")
}

func (s *reposScreen) buildRows(prevPath string) {
	s.group = clamp(s.group, 0, len(s.scans))
	term := ""
	if s.fmode != filterOff {
		term = strings.TrimSpace(s.filter.Value())
	}
	narrowed := term != "" || s.problemsOnly
	var rows []localRow
	for i, c := range s.scans {
		if s.group != 0 && s.group-1 != i {
			continue
		}
		var idx []int
		for k, r := range c.Repos {
			if s.problemsOnly && !r.Problem() {
				continue
			}
			idx = append(idx, k)
		}
		if term != "" {
			targets := make([]string, len(idx))
			for j, k := range idx {
				targets[j] = s.repoTarget(c, c.Repos[k])
			}
			var kept []int
			for _, rk := range treeFilter(term, targets) {
				kept = append(kept, idx[rk.Index])
			}
			idx = kept
		}
		if narrowed && len(idx) == 0 {
			continue
		}
		rows = append(rows, localRow{kind: rowHeader, scan: i, repo: -1})
		if !narrowed {
			rows = append(rows, s.notices(i, c)...)
		}
		for _, k := range idx {
			rows = append(rows, localRow{kind: rowRepo, scan: i, repo: k})
		}
	}
	s.rows = rows
	// keep the selection on the same repository, else the nearest one
	s.sel = -1
	if prevPath != "" {
		for i, r := range rows {
			if r.kind == rowRepo && s.scans[r.scan].Repos[r.repo].Path == prevPath {
				s.sel = i
				break
			}
		}
	}
	if s.sel < 0 {
		s.sel = s.nearestRepo(clamp(s.sel, 0, len(rows)-1))
	}
	s.ensureVisible()
}

// nearestRepo is the repository row at or after i, else the one before.
func (s *reposScreen) nearestRepo(i int) int {
	for j := maxInt(i, 0); j < len(s.rows); j++ {
		if s.rows[j].kind == rowRepo {
			return j
		}
	}
	for j := minInt(i, len(s.rows)-1); j >= 0; j-- {
		if s.rows[j].kind == rowRepo {
			return j
		}
	}
	return -1
}

// notices are the lines under a company header: why it has no repositories,
// folders that could not be read, a scan that stopped at the cap.
func (s *reposScreen) notices(i int, c app.CompanyScan) []localRow {
	svc := s.sh.svc
	_, done := s.sh.repos.done[c.Slug]
	row := func(text string, warn bool) localRow {
		return localRow{kind: rowNotice, scan: i, repo: -1, text: text, warn: warn}
	}
	var out []localRow
	switch c.State {
	case app.ScanMissing:
		out = append(out, row(tr("tui.repos.local.missing", c.BaseDir), true))
	case app.ScanNotDir:
		out = append(out, row(tr("tui.repos.local.not_dir", c.BaseDir), true))
	case app.ScanCancelled:
		out = append(out, row(tr("tui.repos.local.company_cancelled"), true))
	default:
		if done && len(c.Repos) == 0 {
			out = append(out, row(tr("tui.repos.local.empty", c.BaseDir), false))
		}
	}
	if n := c.Stats.DeniedCount; n > 0 {
		var paths []string
		for _, p := range c.Stats.Denied[:minInt(3, len(c.Stats.Denied))] {
			paths = append(paths, svc.Env.Contract(p))
		}
		list := strings.Join(paths, ", ")
		if n == 1 {
			out = append(out, row(tr("tui.repos.local.denied_one", list), true))
		} else {
			out = append(out, row(tr("tui.repos.local.denied_many", n, list), true))
		}
	}
	if c.Stats.Truncated {
		out = append(out, row(tr("tui.repos.local.truncated", groupDigits(c.Stats.Dirs)), true))
	}
	return out
}

// ---- keys ----

func (s *reposScreen) setFilter(mode filterMode, clear bool) {
	prev := s.selPath()
	s.fmode = mode
	if clear {
		s.filter.SetValue("")
	}
	if mode != filterTyping {
		s.filter.Blur()
	}
	s.buildRows(prev)
}

func (s *reposScreen) localKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
	km := s.sh.km
	if s.fmode == filterTyping {
		return s.filterKey(msg)
	}
	r, has := s.selected()
	switch {
	case key.Matches(msg, km.Filter):
		s.fmode = filterTyping
		s.filter.Prompt = km.keys.Label(keymap.Filter) + " "
		s.buildRows(s.selPath())
		return s, s.filter.Focus()
	case key.Matches(msg, km.Problems):
		s.problemsOnly = !s.problemsOnly
		s.buildRows(s.selPath())
	case key.Matches(msg, km.Reload):
		return s, s.startScan()
	case key.Matches(msg, km.Check) || key.Matches(msg, km.Enter):
		if has && r.Kind != scan.KindBare {
			return s, s.sh.pushNow(s.checkScreenFor(r, false))
		}
	case key.Matches(msg, km.Fix):
		if has && r.Fixable() {
			return s, s.sh.pushNow(s.checkScreenFor(r, true))
		}
	case key.Matches(msg, km.Copy):
		if has {
			svc, path, id := s.sh.svc, r.Path, s.id
			return s, func() tea.Msg {
				ctx, cancel := ctxTimeout(shortTimeout)
				defer cancel()
				return repoCopyMsg{id: id, path: path, err: svc.CopyText(ctx, path)}
			}
		}
	case msg.Type == tea.KeyLeft:
		s.moveGroup(-1)
	case msg.Type == tea.KeyRight:
		s.moveGroup(1)
	case key.Matches(msg, km.Up):
		s.move(-1)
	case key.Matches(msg, km.Down):
		s.move(1)
	case key.Matches(msg, km.PageUp):
		s.move(-maxInt(s.listH-1, 1))
	case key.Matches(msg, km.PageDown):
		s.move(maxInt(s.listH-1, 1))
	case msg.Type == tea.KeyHome:
		s.moveTo(s.nearestRepo(0))
	case msg.Type == tea.KeyEnd:
		s.moveTo(s.lastRepo())
	}
	return s, nil
}

func (s *reposScreen) checkScreenFor(r app.LocalRepo, autoFix bool) *checkScreen {
	c := newCheckScreenAt(s.sh, r.Path, autoFix)
	c.fromRepo = true
	return c
}

func (s *reposScreen) filterKey(msg tea.KeyMsg) (Screen, tea.Cmd) {
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
	case tea.KeyUp, tea.KeyCtrlP:
		s.move(-1)
		return s, nil
	case tea.KeyDown, tea.KeyCtrlN:
		s.move(1)
		return s, nil
	}
	prev := s.selPath()
	var cmd tea.Cmd
	s.filter, cmd = s.filter.Update(msg)
	s.buildRows(prev)
	return s, cmd
}

func (s *reposScreen) moveGroup(d int) {
	n := len(s.scans) + 1
	if n <= 1 {
		return
	}
	s.group = (s.group + d + n) % n
	s.top = 0
	s.buildRows("")
}

// move goes d repositories down (up when negative), skipping headers.
func (s *reposScreen) move(d int) {
	if s.sel < 0 {
		return
	}
	step := 1
	if d < 0 {
		step, d = -1, -d
	}
	i := s.sel
	for ; d > 0; d-- {
		j := i + step
		for j >= 0 && j < len(s.rows) && s.rows[j].kind != rowRepo {
			j += step
		}
		if j < 0 || j >= len(s.rows) {
			break
		}
		i = j
	}
	s.moveTo(i)
}

func (s *reposScreen) moveTo(i int) {
	if i >= 0 {
		s.sel = i
		s.ensureVisible()
	}
}

func (s *reposScreen) lastRepo() int {
	for j := len(s.rows) - 1; j >= 0; j-- {
		if s.rows[j].kind == rowRepo {
			return j
		}
	}
	return -1
}

// ---- help ----

func (s *reposScreen) localHelp() []key.Binding {
	km := s.sh.km
	if s.fmode == filterTyping {
		return []key.Binding{withDesc(km.Enter, "enter", tr("tui.key.filter_keep")), withDesc(km.Back, "esc", tr("tui.key.filter_clear"))}
	}
	var out []key.Binding
	if len(s.sh.cfg.Companies) > 0 {
		if r, ok := s.selected(); ok {
			if r.Kind != scan.KindBare {
				out = append(out, km.Check)
			}
			if r.Fixable() {
				out = append(out, km.Fix)
			}
			out = append(out, relabel(km.Copy, tr("tui.key.copy_path")))
		}
		out = append(out, km.Filter, km.Problems, relabel(km.Reload, tr("tui.key.rescan")))
		if len(s.scans) > 0 {
			out = append(out, key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", tr("tui.key.group"))))
		}
	}
	out = append(out, s.tabBinding())
	switch {
	case s.scanning:
		return append(out, withDesc(km.Back, "esc", tr("tui.key.stop_scan")))
	case len(s.sh.cfg.Companies) == 0:
		return append(out, withDesc(km.Back, "esc", tr("tui.key.to_accounts")))
	}
	return append(out, withDesc(km.Back, "esc", tr("tui.key.back")))
}

func (s *reposScreen) localContextHelp() []helpItem {
	km := s.sh.km
	if s.fmode == filterTyping {
		return []helpItem{
			{b: withDesc(km.Enter, "enter", tr("tui.key.filter_keep")), primary: true},
			{b: withDesc(km.Back, "esc", tr("tui.key.filter_clear"))},
			{b: withDesc(km.Up, "↑/↓", tr("tui.key.move"))},
		}
	}
	var out []helpItem
	first := true
	check := s.canCheck()
	for _, b := range s.localHelp() {
		// the first item is the next step when there is a repository to check;
		// otherwise it is the one key that moves things on: stop the scan, go
		// back to the Accounts screen to add a company, or scan again when the
		// folders hold no repository
		next := first && check
		if !check {
			switch {
			case s.scanning, len(s.sh.cfg.Companies) == 0:
				next = isExit(s.sh.km, b)
			case s.repoCount() == 0 && !s.problemsOnly && strings.TrimSpace(s.filter.Value()) == "":
				next = slices.Equal(b.Keys(), s.sh.km.Reload.Keys())
			}
		}
		out = append(out, helpItem{b: b, primary: next})
		first = false
	}
	return out
}

func (s *reposScreen) canCheck() bool {
	r, ok := s.selected()
	return ok && r.Kind != scan.KindBare
}

// ---- geometry and view ----

type localGeom struct{ iw, ih, listH, detail int }

// geom lays the Local tab out: the selector line, optional filter and status
// lines, the list, and (when there is room) the detail block.
func (s *reposScreen) geom() localGeom {
	fr := s.sh.st.Panel
	// ih is the inner height without the tab bar, the first line of the panel
	g := localGeom{iw: s.w - fr.GetHorizontalFrameSize(), ih: (s.h - 1) - fr.GetVerticalFrameSize()}
	head := 1
	if s.fmode != filterOff {
		head++
	}
	if s.scanning || s.cancelled {
		head++
	}
	avail := g.ih - head
	switch {
	case avail >= 12:
		g.detail = 6
	case avail >= 8:
		g.detail = 3
	}
	g.listH = maxInt(avail-g.detail, 1)
	return g
}

// ensureVisible scrolls the list so the selected repository (and its company
// header when it is the first one) is on screen.
func (s *reposScreen) ensureVisible() {
	if s.sh == nil || s.h == 0 {
		return
	}
	g := s.geom()
	s.listH = g.listH
	if s.sel < 0 {
		s.top = 0
		return
	}
	want := s.sel
	if s.sel > 0 && s.rows[s.sel-1].kind != rowRepo {
		// pull the header (and its notices) into view with the first repo
		for want > 0 && s.rows[want-1].kind != rowRepo {
			want--
		}
		if s.sel-want >= g.listH {
			want = s.sel
		}
	}
	if want < s.top {
		s.top = want
	}
	if s.sel >= s.top+g.listH {
		s.top = s.sel - g.listH + 1
	}
	s.top = clamp(s.top, 0, maxInt(len(s.rows)-g.listH, 0))
}

func (s *reposScreen) counts(c ...app.CompanyScan) string {
	repos, probs := 0, 0
	for _, x := range c {
		repos += len(x.Repos)
		probs += x.Problems()
	}
	r, p := tr("tui.repos.local.repos_many", repos), tr("tui.repos.local.problems_many", probs)
	if repos == 1 {
		r = tr("tui.repos.local.repos_one")
	}
	if probs == 1 {
		p = tr("tui.repos.local.problems_one")
	}
	return r + " · " + p
}

// countPending says the scan is running and has found nothing in the company
// yet and not finished it: "0 repo · 0 vấn đề" would be a guess.
func (s *reposScreen) countPending(c app.CompanyScan) bool {
	if !s.scanning || len(c.Repos) > 0 {
		return false
	}
	if s.sh.repos != nil {
		if _, done := s.sh.repos.done[c.Slug]; done {
			return false
		}
	}
	return true
}

// found is the number of repos in the scans so far.
func (s *reposScreen) found(c ...app.CompanyScan) int {
	n := 0
	for _, x := range c {
		n += len(x.Repos)
	}
	return n
}

func (s *reposScreen) selectorLine() string {
	st := s.sh.st
	name := tr("tui.repos.local.all")
	scans := s.scans
	if s.group > 0 && s.group <= len(s.scans) {
		c := s.scans[s.group-1]
		name = c.Slug + " · " + c.BaseDir
		scans = []app.CompanyScan{c}
	}
	// ← and → are the keys that switch the company
	line := st.Muted.Render("← ") + st.Bold.Render(name) + st.Muted.Render(" →")
	if !s.scanning || s.found(scans...) > 0 {
		// no "0 repo · 0 vấn đề" while the first scan has found nothing yet
		line += "  " + st.Muted.Render(s.counts(scans...))
	}
	if s.problemsOnly {
		line += "  " + st.Warn.Render("["+tr("tui.repos.local.problems_only")+"]")
	}
	return line
}

func (s *reposScreen) filterLine() string {
	st := s.sh.st
	prompt := s.sh.km.keys.Label(keymap.Filter) + " "
	s.filter.Prompt = prompt
	if s.fmode == filterTyping {
		return s.filter.View()
	}
	return st.Muted.Render(prompt) + st.Text.Render(s.filter.Value()) + st.Muted.Render("  "+tr("tui.repos.local.clear_filter"))
}

// statusLine shows the running scan or says it was stopped.
func (s *reposScreen) statusLine() string {
	st := s.sh.st
	switch {
	case s.scanning:
		if s.prog.company == "" || (s.prog.dirs == 0 && s.prog.found == 0) {
			// no counts until the scan has something to count
			return s.spin.View() + " " + st.Muted.Render(tr("tui.repos.local.scanning_wait"))
		}
		root := s.prog.company
		for _, c := range s.sh.cfg.Companies {
			if c.Slug == s.prog.company {
				root = c.BaseDir
			}
		}
		return s.spin.View() + " " + st.Muted.Render(tr("tui.repos.local.scanning", root, groupDigits(s.prog.dirs), s.prog.found))
	case s.cancelled:
		return st.Warn.Render("! " + s.sh.km.T("tui.repos.local.cancelled"))
	}
	return ""
}

func (s *reposScreen) localView() string {
	st := s.sh.st
	h := maxInt(s.h, 4)
	if len(s.sh.cfg.Companies) == 0 {
		msg := st.Muted.Render(wrap(tr("tui.repos.local.no_company"), proseW(maxInt(s.w-6, 10))))
		return s.sh.panel("", s.tabBar()+"\n\n"+msg, s.w, h, true)
	}
	g := s.geom()
	var lines []string
	lines = append(lines, s.selectorLine())
	if s.fmode != filterOff {
		lines = append(lines, s.filterLine())
	}
	if sl := s.statusLine(); sl != "" {
		lines = append(lines, sl)
	}
	lines = append(lines, s.listLines(g)...)
	if g.detail > 0 {
		lines = append(lines, s.detailBlock(g)...)
	}
	// the tab bar is the first line of the panel, above what the geometry counts
	return s.sh.panel("", s.tabBar()+"\n"+strings.Join(lines, "\n"), s.w, h, true)
}

// listLines draws exactly g.listH rows of the list (blank lines pad).
func (s *reposScreen) listLines(g localGeom) []string {
	st := s.sh.st
	var out []string
	if len(s.rows) == 0 {
		var msg string
		switch {
		case s.fmode != filterOff && strings.TrimSpace(s.filter.Value()) != "":
			msg = tr("tui.repos.local.no_match", strings.TrimSpace(s.filter.Value()))
		case s.problemsOnly && len(s.scans) > 0 && !s.scanning:
			msg = s.sh.km.T("tui.repos.local.no_problems")
		}
		if msg != "" {
			out = append(out, st.Muted.Render(trunc(msg, g.iw)))
		}
	}
	cols := s.columns(g.iw)
	for i := s.top; i < len(s.rows) && len(out) < g.listH; i++ {
		out = append(out, s.rowLine(i, cols, g.iw))
	}
	for len(out) < g.listH {
		out = append(out, "")
	}
	return out
}

type localCols struct{ path, account, note int }

// colGap is the space between two columns of a repo row.
const colGap = 2

// columns sizes the columns of the repository rows: the account column hides
// under 60 columns of screen width, the note shrinks before the path does.
// The path column hugs its widest path, and the account and the note follow it.
func (s *reposScreen) columns(iw int) localCols {
	var c localCols
	widest := 0
	for _, row := range s.rows {
		if row.kind != rowRepo {
			continue
		}
		r := s.scans[row.scan].Repos[row.repo]
		widest = maxInt(widest, lipgloss.Width(s.sh.svc.RelPath(s.scans[row.scan].BaseDir, r.Path)))
		if s.w >= 60 {
			c.account = maxInt(c.account, lipgloss.Width(r.Account))
		}
		c.note = maxInt(c.note, lipgloss.Width(s.note(r)))
	}
	c.account = minInt(c.account, 18)
	c.note = minInt(c.note, maxInt(iw/3, 14))
	size := func() int {
		n := iw - 3
		if c.account > 0 {
			n -= c.account + colGap
		}
		if c.note > 0 {
			n -= c.note + colGap
		}
		return n
	}
	if size() < 12 {
		c.account = 0
	}
	for size() < 8 && c.note > 6 {
		c.note--
	}
	c.path = minInt(maxInt(size(), 4), maxInt(widest, 4))
	return c
}

// note is the short text after the account: the status, the flags and the
// kind of the repository.
func (s *reposScreen) note(r app.LocalRepo) string {
	var parts []string
	if r.Status != app.StatusOK && r.Status != app.StatusBare {
		parts = append(parts, tr("tui.repos.status."+string(r.Status)))
	}
	for _, f := range r.Flags {
		switch f {
		case app.FlagWrongFolder:
			parts = append(parts, tr("tui.repos.flag.wrong_folder", r.BelongsTo))
		case app.FlagDuplicate:
			parts = append(parts, tr("tui.repos.flag.duplicate", len(r.Duplicates)+1))
		default:
			parts = append(parts, tr("tui.repos.flag."+f))
		}
	}
	if r.Kind != scan.KindRepo {
		parts = append(parts, tr("tui.repos.kind."+string(r.Kind)))
	}
	return strings.Join(parts, " · ")
}

// mark is the one-cell status of a row: ok, problem, error, neutral.
func (s *reposScreen) mark(r app.LocalRepo) string {
	st := s.sh.st
	switch {
	case r.Status == app.StatusBroken || r.Status == app.StatusDubious:
		return st.Err.Render("✗")
	case r.Status == app.StatusBare:
		return st.Faint.Render("·")
	case r.Problem():
		return st.Warn.Render("!")
	}
	return st.OK.Render("✓")
}

func (s *reposScreen) rowLine(i int, cols localCols, iw int) string {
	st := s.sh.st
	row := s.rows[i]
	switch row.kind {
	case rowHeader:
		c := s.scans[row.scan]
		line := st.Bold.Render(c.Slug) + "  " + st.Muted.Render(c.BaseDir)
		if c.State != app.ScanMissing && c.State != app.ScanNotDir && !s.countPending(c) {
			// a folder that is not there has no count (the notice below says
			// so), nor has a folder the scan has not reached yet
			line += "  " + st.Muted.Render(s.counts(c))
		}
		return trunc(line, iw)
	case rowNotice:
		if row.warn {
			return trunc(st.Warn.Render("  ! "+row.text), iw)
		}
		return trunc(st.Faint.Render("  "+row.text), iw)
	}
	c := s.scans[row.scan]
	r := c.Repos[row.repo]
	path := pad(s.sh.svc.RelPath(c.BaseDir, r.Path), cols.path)
	account, note := "", s.note(r)
	if cols.account > 0 {
		account = spaces(colGap) + pad(r.Account, cols.account)
	}
	noteCol := ""
	if cols.note > 0 {
		noteCol = spaces(colGap) + trunc(note, cols.note)
	}
	if i == s.sel {
		plain := "▌" + plainMark(r) + " " + path + account + noteCol
		return st.Selected.Render(pad(plain, iw))
	}
	noteStyled := noteCol
	if noteCol != "" {
		noteStyled = st.Warn.Render(noteCol)
		if !r.Problem() {
			noteStyled = st.Faint.Render(noteCol)
		}
	}
	return trunc(" "+s.mark(r)+" "+st.Text.Render(path)+st.Muted.Render(account)+noteStyled, iw)
}

// plainMark is mark without styling (the selected row has its own style).
func plainMark(r app.LocalRepo) string {
	switch {
	case r.Status == app.StatusBroken || r.Status == app.StatusDubious:
		return "✗"
	case r.Status == app.StatusBare:
		return "·"
	case r.Problem():
		return "!"
	}
	return "✓"
}

// ---- detail block ----

// detailBlock is the separator and up to five lines about the selected
// repository, padded to g.detail lines.
func (s *reposScreen) detailBlock(g localGeom) []string {
	st := s.sh.st
	out := []string{st.divider(g.iw)}
	r, ok := s.selected()
	if !ok {
		out = append(out, st.Faint.Render(trunc(tr("tui.repos.local.none_selected"), g.iw)))
	} else {
		out = append(out, s.detailLines(r, g.iw, g.detail-1)...)
	}
	for len(out) < g.detail {
		out = append(out, "")
	}
	return out[:g.detail]
}

func (s *reposScreen) detailLines(r app.LocalRepo, iw, room int) []string {
	st := s.sh.st
	svc := s.sh.svc
	none := tr("tui.value.none")
	col := kvCol(iw, tr("tui.f.dir"), tr("tui.f.remote"), tr("tui.f.account"), tr("tui.f.effective"), tr("tui.f.fix_to"))
	row := func(label, value string) string { return kvRow(st, col, iw, label, value) }
	var lines []string
	lines = append(lines, row(tr("tui.f.dir"), svc.Env.Contract(r.Path)))
	if room >= 5 {
		remote := r.Remote
		if remote == "" {
			remote = none
		}
		lines = append(lines, row(tr("tui.f.remote"), remote))
		acc := st.Text.Render(none)
		if r.Account != "" {
			acc = st.Text.Render(r.Account)
			if ref, err := s.sh.cfg.ParseRef(r.Account); err == nil {
				acc = st.identityStamp(ref)
			}
		}
		lines = append(lines, st.Label.Render(pad(tr("tui.f.account"), col))+trunc(acc, maxInt(iw-col, 4)))
		// the email in use is told when it is not the account's
		if r.EffectiveEmail != "" && (r.HasFlag(app.FlagEmailMismatch) || r.HasFlag(app.FlagLocalOverride)) {
			inUse := r.EffectiveEmail
			if r.EmailOrigin != "" {
				inUse += " · " + s.origin(r.EmailOrigin)
			}
			lines = append(lines, row(tr("tui.f.effective"), inUse))
		}
	}
	if r.Fixable() {
		// the fix is a field, not a sentence: the help bar names the key
		return append(lines, row(tr("tui.f.fix_to"), r.FixTo))
	}
	hint, warn := s.hint(r)
	style := st.Text
	if warn {
		style = st.Warn
	}
	lines = append(lines, style.Render(trunc(hint, iw)))
	return lines
}

// origin shortens the "file:/abs/path" origin printed by git.
func (s *reposScreen) origin(o string) string {
	if p, ok := strings.CutPrefix(o, "file:"); ok {
		return "file:" + s.sh.svc.Env.Contract(p)
	}
	return o
}

// hint is the most useful sentence about a repository and whether it is a
// warning. A key is named only when it does something here.
func (s *reposScreen) hint(r app.LocalRepo) (string, bool) {
	switch {
	case r.Fixable():
		return tr("tui.repos.hint.wrong_remote"), true
	case r.Status == app.StatusBroken:
		return tr(app.DetailBroken), true
	case r.Status == app.StatusDubious:
		return tr(app.DetailDubious), true
	case r.Status == app.StatusBare:
		return tr("tui.repos.hint.bare"), false
	case r.Status != app.StatusOK:
		return tr("tui.repos.hint." + string(r.Status)), true
	case r.HasFlag(app.FlagWrongFolder):
		return tr("tui.repos.hint.wrong_folder", r.BelongsTo), true
	case r.HasFlag(app.FlagDuplicate):
		var ps []string
		for _, p := range r.Duplicates {
			ps = append(ps, s.sh.svc.Env.Contract(p))
		}
		return tr("tui.repos.hint.duplicate", strings.Join(ps, ", ")), true
	case r.HasFlag(app.FlagEmailMismatch):
		return tr("tui.repos.hint.email_mismatch", r.EffectiveEmail, r.ExpectedEmail), true
	case r.HasFlag(app.FlagLocalOverride):
		return tr("tui.repos.hint.local_override"), true
	}
	return tr("tui.repos.hint.ok"), false
}

package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/keymap"
	"github.com/tinhnd1902/gitid/internal/scan"
	"github.com/tinhnd1902/gitid/internal/testutil"
)

// ---- fixtures ----

// tuiRepo is one fixture repository under ~/Code.
type tuiRepo struct {
	rel   string // "acme/api"
	kind  string // "" (repo) | worktree | bare
	url   string
	email string
	exit  int
}

func nulJoin(parts ...string) string { return strings.Join(parts, "\x00") + "\x00" }

func (r tuiRepo) stdout() string {
	var p []string
	if r.url != "" {
		p = append(p, "file:.git/config", "remote.origin.url\n"+r.url)
	}
	if r.email != "" {
		p = append(p, "file:/home/x/.gitconfig", "user.email\n"+r.email)
	}
	if len(p) == 0 {
		return ""
	}
	return nulJoin(p...)
}

// localRepos are the fixture repositories of the Local tab tests (the A-1,
// A-3, A-6, A-10, A-11 and A-12 cases of test-cases.md).
func localRepos() []tuiRepo {
	return []tuiRepo{
		{rel: "acme/api", url: "https://alice@github.com/acme/api.git", email: "alice@acme.com"},                  // ok
		{rel: "acme/org/x", url: "https://github.com/acme/x.git", email: "alice@acme.com"},                        // wrong username, fixable
		{rel: "acme/bb", url: "https://alice@bitbucket.org/w/r.git", email: "alice@acme.com"},                     // foreign host
		{rel: "beta/wf", url: "https://bob@github.com/acme/wf.git", email: "bob@acme.com"},                        // wrong folder
		{rel: "acme/dup", url: "https://alice@github.com/acme/shared.git", email: "alice@acme.com"},               // duplicate
		{rel: "beta/dup2", url: "https://alice@github.com/acme/shared.git", email: "alice@beta.io"},               // duplicate
		{rel: "acme/wt", kind: "worktree", url: "https://alice@github.com/acme/api.git", email: "alice@acme.com"}, // worktree, not a duplicate
	}
}

// addRepos creates the folders under the harness HOME and scripts the fake
// for their single git call. It returns the absolute path of each.
func addRepos(t *testing.T, h *harness, repos []tuiRepo) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, r := range repos {
		dir := filepath.Join(h.e.Home, "Code", r.rel)
		switch r.kind {
		case "bare":
			for _, d := range []string{"objects", "refs"} {
				mustMkdir(t, filepath.Join(dir, d))
			}
			mustWrite(t, filepath.Join(dir, "HEAD"), "ref: refs/heads/main\n")
		case "worktree":
			mustMkdir(t, dir)
			mustWrite(t, filepath.Join(dir, ".git"), "gitdir: /x/main/.git/worktrees/w\n")
		default:
			mustMkdir(t, filepath.Join(dir, ".git"))
		}
		h.fake.On("git -C "+dir+" config -z --show-origin --get-regexp", execx.Response{Stdout: r.stdout(), ExitCode: r.exit})
		out[r.rel] = dir
	}
	return out
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p, s string) {
	t.Helper()
	testutil.WriteFile(t, p, s, 0o644)
}

// localHarness is the basic configuration with the fixture repositories on
// disk. The screens are closed when the test ends.
func localHarness(t *testing.T, keysYAML ...string) (*harness, map[string]string) {
	t.Helper()
	h := harnessFor(t, basicCfg(), keysYAML)
	paths := addRepos(t, h, localRepos())
	t.Cleanup(h.m.shutdown)
	return h, paths
}

// scanningHarness leaves the browser on the Local tab with a scan in progress
// (every git call waits until the test ends).
func scanningHarness(t *testing.T, keysYAML ...string) *harness {
	t.Helper()
	h, _ := localHarness(t, keysYAML...)
	release := make(chan struct{})
	for _, rp := range localRepos() {
		dir := filepath.Join(h.e.Home, "Code", rp.rel)
		h.fake.On("git -C "+dir+" config -z --show-origin --get-regexp", execx.Response{Stdout: rp.stdout(), Block: release})
	}
	t.Cleanup(func() { close(release) })
	h.act(keymap.Clone)
	h.key("shift+tab")
	h.waitFor("git calls started", func() bool { return gitCalls(h) > 0 })
	if !reposOf(h).scanning {
		t.Fatal("the scan is not running")
	}
	return h
}

func reposOf(h *harness) *reposScreen {
	h.t.Helper()
	r, ok := h.top().(*reposScreen)
	if !ok {
		h.t.Fatalf("top is %T, not the repo browser", h.top())
	}
	return r
}

// openRepos presses the clone key and waits for the scan; the browser is on
// the URL tab.
func openRepos(h *harness) *reposScreen {
	h.t.Helper()
	h.act(keymap.Clone)
	r := reposOf(h)
	h.waitFor("scan", func() bool { return !r.scanning })
	return r
}

// openLocal is openRepos followed by shift+tab to the Local tab.
func openLocal(h *harness) *reposScreen {
	h.t.Helper()
	r := openRepos(h)
	h.key("shift+tab")
	if r.tab != tabLocal {
		h.t.Fatalf("tab %v", r.tab)
	}
	return r
}

// row finds the plain text of the line holding s.
func (h *harness) rowWith(s string) string {
	for _, l := range strings.Split(plain(h.view()), "\n") {
		if strings.Contains(l, s) {
			return l
		}
	}
	return ""
}

func gitCalls(h *harness) int { return h.fake.Count("git -C") }

// stub is a minimal screen that counts its Close calls.
type stubScreen struct {
	title  string
	closed int32
	seen   int32
}

func (s *stubScreen) Init() tea.Cmd { return nil }
func (s *stubScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	if _, ok := msg.(probeMsg); ok {
		atomic.AddInt32(&s.seen, 1)
	}
	return s, nil
}
func (s *stubScreen) View() string        { return "stub " + s.title }
func (s *stubScreen) Title() string       { return s.title }
func (s *stubScreen) Help() []key.Binding { return nil }
func (s *stubScreen) Resize(w, h int)     {}
func (s *stubScreen) Close() tea.Cmd      { atomic.AddInt32(&s.closed, 1); return nil }
func (s *stubScreen) closes() int         { return int(atomic.LoadInt32(&s.closed)) }

type probeMsg struct{}

func (probeMsg) bgMsg() {}

// ---- root: closer and bgMsg (T-1, T-2) ----

func TestT1RootCallsClose(t *testing.T) {
	h := harnessFor(t, basicCfg(), nil)
	a, b := &stubScreen{title: "a"}, &stubScreen{title: "b"}
	h.send(pushMsg{a})
	h.key("esc")
	if a.closes() != 1 || h.depth() != 1 {
		t.Fatalf("esc: closed %d depth %d", a.closes(), h.depth())
	}
	// replace closes the old top, not the new one
	h.send(pushMsg{a})
	h.send(replaceMsg{b})
	if a.closes() != 2 || b.closes() != 0 || h.top() != Screen(b) {
		t.Fatalf("replace: a=%d b=%d", a.closes(), b.closes())
	}
	// popRoot closes everything above the root, once each
	c := &stubScreen{title: "c"}
	h.send(pushMsg{c})
	h.send(popRootMsg{})
	if b.closes() != 1 || c.closes() != 1 || h.depth() != 1 {
		t.Fatalf("popRoot: b=%d c=%d depth %d", b.closes(), c.closes(), h.depth())
	}
	// popNow / popMsg
	d := &stubScreen{title: "d"}
	h.send(pushMsg{d})
	h.send(popMsg{})
	if d.closes() != 1 {
		t.Fatalf("pop: %d", d.closes())
	}
	// shutdown closes what is left on the stack
	e := &stubScreen{title: "e"}
	h.send(pushMsg{e})
	h.m.shutdown()
	if e.closes() != 1 {
		t.Fatalf("shutdown: %d", e.closes())
	}
}

func TestT2BgMsgReachesScreensBelowTheTop(t *testing.T) {
	h := harnessFor(t, basicCfg(), nil)
	below, top := &stubScreen{title: "below"}, &stubScreen{title: "top"}
	h.send(pushMsg{below})
	h.send(pushMsg{top})
	h.send(probeMsg{})
	if atomic.LoadInt32(&below.seen) != 1 || atomic.LoadInt32(&top.seen) != 1 {
		t.Fatalf("below saw %d, top saw %d", below.seen, top.seen)
	}
}

// ---- check screen at a directory (T-20 .. T-22) ----

func TestT20CheckAtDir(t *testing.T) {
	h, p := localHarness(t)
	dir := p["acme/api"]
	h.fake.On("git -C "+dir+" rev-parse --show-toplevel", execx.Response{Stdout: dir + "\n"})
	h.fake.On("git -C "+dir+" config --get-regexp", execx.Response{Stdout: "remote.origin.url https://alice@github.com/acme/api.git\n"})
	h.fake.On("git -C "+dir+" config --show-origin --get user.email", execx.Response{Stdout: "file:/home/x/.gitconfig\talice@acme.com\n"})
	c := newCheckScreenAt(h.m.sh, dir, false)
	h.send(pushMsg{c})
	h.waitFor("result", func() bool { return c.phase == chResult })
	for _, call := range h.fake.CallStrings() {
		if strings.HasPrefix(call, "git -C "+h.e.Home+" ") {
			t.Fatalf("a call used the cwd: %s", call)
		}
	}
	if c.res.Repo != dir {
		t.Fatalf("repo %q", c.res.Repo)
	}
}

func TestT21AutoFix(t *testing.T) {
	h, p := localHarness(t)
	dir := p["acme/org/x"]
	h.fake.On("git -C "+dir+" rev-parse --show-toplevel", execx.Response{Stdout: dir + "\n"})
	h.fake.On("git -C "+dir+" config --get-regexp", execx.Response{Stdout: "remote.origin.url https://github.com/acme/x.git\n"})
	h.fake.On("git -C "+dir+" config --show-origin --get user.email", execx.Response{Stdout: "file:/home/x/.gitconfig\talice@acme.com\n"})
	c := newCheckScreenAt(h.m.sh, dir, true)
	h.send(pushMsg{c})
	h.waitFor("fix confirm", func() bool { return c.phase == chFixConfirm })
	if c.fix.New != "https://alice@github.com/acme/x.git" {
		t.Fatalf("fix %+v", c.fix)
	}
}

func TestT22AutoFixNothingToFix(t *testing.T) {
	h, p := localHarness(t)
	dir := p["acme/api"]
	h.fake.On("git -C "+dir+" rev-parse --show-toplevel", execx.Response{Stdout: dir + "\n"})
	h.fake.On("git -C "+dir+" config --get-regexp", execx.Response{Stdout: "remote.origin.url https://alice@github.com/acme/api.git\n"})
	h.fake.On("git -C "+dir+" config --show-origin --get user.email", execx.Response{Stdout: "file:/home/x/.gitconfig\talice@acme.com\n"})
	c := newCheckScreenAt(h.m.sh, dir, true)
	h.send(pushMsg{c})
	h.waitFor("result", func() bool { return c.phase == chResult && c.note != "" || c.phase == chFixPlanning })
	h.waitFor("settled", func() bool { return c.phase == chResult })
	if c.note != tr("tui.fix.nothing") {
		t.Fatalf("note %q", c.note)
	}
}

// ---- the browser opened by the clone key (T-30 .. T-34) ----

func TestT30CloneKeyOpensTheBrowserOnTheURLTab(t *testing.T) {
	h, _ := localHarness(t)
	h.act(keymap.Clone)
	r := reposOf(h)
	if r.tab != tabURL || !r.url.input.Focused() || r.url.phase != clURL {
		t.Fatalf("tab %v focused %v", r.tab, r.url.input.Focused())
	}
	h.mustContainPlain(tr("tui.repos.title"), tr("tui.clone.paste"))
	if !strings.Contains(plain(h.m.headerView()), tr("tui.repos.title")) {
		t.Fatalf("breadcrumb %q", plain(h.m.headerView()))
	}
	// the tab bar names the three tabs and marks the active one
	bar := plain(r.tabBar())
	if !strings.Contains(bar, "▸ "+tr("tui.repos.tab.url")) || !strings.Contains(bar, tr("tui.repos.tab.online")) {
		t.Fatalf("tab bar %q", bar)
	}
}

func TestT31URLFlowThroughTheBrowser(t *testing.T) {
	h, _ := localHarness(t)
	h.fake.On("git clone", execx.Response{Stderr: "Receiving objects:  100% (1/1)\n"})
	h.selectKey("acme/bob")
	h.act(keymap.Clone)
	h.typeText("https://github.com/org/repo.git")
	h.key("enter")
	cs := cloneOf(h)
	if cs.phase != clPick || cs.cands[cs.cur].String() != "acme/bob" {
		t.Fatalf("pick: phase %v on %s", cs.phase, cs.cands[cs.cur])
	}
	h.key("enter")
	h.waitFor("done", func() bool { return cs.phase == clDone })
	want := "git clone --progress -- https://bob@github.com/org/repo.git " + filepath.Join(h.e.Home, "Code", "acme", "repo")
	if h.fake.Count(want) != 1 {
		t.Fatalf("calls: %v", h.fake.CallStrings())
	}
	h.key("enter") // done: leaves the browser
	if h.depth() != 1 {
		t.Fatalf("depth %d", h.depth())
	}
}

// T-32: an authentication failure offers the terminal mode, as before.
func TestT32AuthFailureTerminalMode(t *testing.T) {
	h, _ := localHarness(t)
	h.fake.On("git clone", execx.Response{ExitCode: 128, Stderr: "fatal: Authentication failed for 'x'\n"})
	h.act(keymap.Clone)
	h.typeText("https://github.com/org/repo.git")
	h.key("enter")
	h.key("enter")
	cs := cloneOf(h)
	h.waitFor("failed", func() bool { return cs.phase == clFailed })
	h.mustContainPlain(tr("tui.clone.terminal_mode"))
	h.fake.On("git clone", execx.Response{})
	h.act(keymap.Terminal)
	if cs.phase != clRunning {
		t.Fatalf("phase %v", cs.phase)
	}
	h.send(cloneTermDoneMsg{id: cs.id})
	h.waitFor("verified", func() bool { return cs.phase == clDone })
}

func TestT33CloneScreenFor(t *testing.T) {
	h, p := localHarness(t)
	ref, _ := h.m.sh.cfg.ParseRef("acme/bob")
	target := filepath.Join(filepath.Dir(p["acme/api"]), "elsewhere")
	c := newCloneScreenFor(h.m.sh, &ref, "https://github.com/org/repo.git", target)
	if c.phase != clPick || c.cands[c.cur].String() != "acme/bob" || c.preview.Dir != target || c.prevErr != nil {
		t.Fatalf("phase %v on %s dir %q err %v", c.phase, c.cands[c.cur], c.preview.Dir, c.prevErr)
	}
	r := openRepos(h)
	h.send(pushMsg{c})
	if h.top() != Screen(c) {
		t.Fatal("not on top")
	}
	h.key("esc") // backPops: leaves the screen, back to the browser
	if h.top() != Screen(r) {
		t.Fatalf("top is %T", h.top())
	}
}

func TestT34CloneDoneAddsTheRepoToTheLocalTab(t *testing.T) {
	h, p := localHarness(t)
	h.fake.On("git clone", execx.Response{Stderr: "Receiving objects:  100% (1/1)\n"})
	r := openRepos(h)
	before, _ := r.localCount()
	newDir := filepath.Join(h.e.Home, "Code", "acme", "repo")
	h.fake.On("git -C "+newDir+" config -z --show-origin --get-regexp", execx.Response{Stdout: tuiRepo{url: "https://alice@github.com/org/repo.git", email: "alice@acme.com"}.stdout()})
	h.typeText("https://github.com/org/repo.git")
	h.key("enter")
	h.key("enter")
	h.waitFor("done", func() bool { return r.url.phase == clDone })
	h.waitFor("inspected", func() bool { n, _ := r.localCount(); return n == before+1 })
	_ = p
	if _, ok := h.m.sh.repos.at[newDir]; !ok {
		t.Fatal("the session does not hold the new repository")
	}
}

// ---- tabs (T-40, T-41) ----

func TestT40Tabs(t *testing.T) {
	h, _ := localHarness(t)
	r := openRepos(h)
	h.typeText("https://github.com/org/repo.git")
	h.key("tab") // URL -> Online
	if r.tab != tabOnline {
		t.Fatalf("tab %v", r.tab)
	}
	// the Online tab lists the accounts
	h.mustContainPlain(tr("tui.repos.online.token", "none"))
	h.key("tab") // Online -> Local
	if r.tab != tabLocal {
		t.Fatalf("tab %v", r.tab)
	}
	h.key("tab") // Local -> URL
	if r.tab != tabURL || r.url.input.Value() != "https://github.com/org/repo.git" {
		t.Fatalf("tab %v input %q", r.tab, r.url.input.Value())
	}
	h.key("shift+tab") // URL -> Local
	if r.tab != tabLocal {
		t.Fatalf("shift+tab: tab %v", r.tab)
	}
	h.key("shift+tab") // Local -> Online
	if r.tab != tabOnline {
		t.Fatalf("shift+tab: tab %v", r.tab)
	}
}

func TestT41TabIsLockedWhileCloning(t *testing.T) {
	h, _ := localHarness(t)
	release := make(chan struct{})
	h.fake.On("git clone", execx.Response{Block: release})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	r := openRepos(h)
	h.typeText("https://github.com/org/repo.git")
	h.key("enter")
	h.key("enter")
	if r.url.phase != clRunning {
		t.Fatalf("phase %v", r.url.phase)
	}
	before := h.snapshot()
	h.key("tab")
	h.key("shift+tab")
	if r.tab != tabURL || h.snapshot() != before {
		t.Fatal("tab switched while a clone runs")
	}
	if strings.Contains(plain(h.helpLine()), tr("tui.key.tab")) {
		t.Fatalf("the help bar offers tab while locked: %q", h.helpLine())
	}
	close(release)
	h.waitFor("done", func() bool { return r.url.phase == clDone })
	h.key("tab") // unlocked again
	if r.tab != tabOnline {
		t.Fatalf("tab %v", r.tab)
	}
}

// ---- the Local tab (T-42 .. T-75) ----

func TestT42ScanOnOpenAndCount(t *testing.T) {
	h, _ := localHarness(t)
	release := make(chan struct{})
	// hold every git call until released
	for _, r := range localRepos() {
		dir := filepath.Join(h.e.Home, "Code", r.rel)
		h.fake.On("git -C "+dir+" config -z --show-origin --get-regexp", execx.Response{Stdout: r.stdout(), Block: release})
	}
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	h.act(keymap.Clone)
	r := reposOf(h)
	if !r.scanning {
		t.Fatal("the scan did not start when the screen opened")
	}
	h.mustContainPlain(tr("tui.repos.tab.local") + " …")
	close(release)
	h.waitFor("done", func() bool { return !r.scanning })
	h.mustContainPlain(tr("tui.repos.tab.local") + " 7")
}

func TestT43LocalRows(t *testing.T) {
	h, p := localHarness(t)
	openLocal(h)
	v := plain(h.view())
	for _, want := range []string{
		"✓ api", "acme/alice", "! org/x", tr("tui.repos.status.wrong_remote"), tr("tui.repos.status.foreign_host"),
		tr("tui.repos.flag.wrong_folder", "acme"), tr("tui.repos.flag.duplicate", 2), tr("tui.repos.kind.worktree"),
		tr("tui.repos.local.repos_many", 7), "← " + tr("tui.repos.local.all") + " →", "acme", "beta",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	// the worktree shares its remote with api and is not a duplicate
	scans := reposOf(h).scans
	var wt app.LocalRepo
	for _, c := range scans {
		for _, r := range c.Repos {
			if r.Path == p["acme/wt"] {
				wt = r
			}
		}
	}
	if wt.HasFlag(app.FlagDuplicate) || wt.Kind != scan.KindWorktree {
		t.Fatalf("worktree: %+v", wt)
	}
	// problems: x, bb, wf, dup, dup2 (the worktree and api are fine)
	if !strings.Contains(v, tr("tui.repos.local.problems_many", 5)) {
		t.Errorf("problem count:\n%s", v)
	}
}

func TestT44RelativePathAndDetail(t *testing.T) {
	h, p := localHarness(t)
	r := openLocal(h)
	if strings.Contains(plain(h.view()), p["acme/api"]) {
		t.Fatal("rows show the absolute path")
	}
	// select org/x: the detail shows the contracted absolute path
	h.walkTo(r, p["acme/org/x"])
	h.mustContainPlain("~/Code/acme/org/x", "https://github.com/acme/x.git", "acme/alice", "alice@acme.com")
}

// walkTo moves the selection to the repository at path.
func (h *harness) walkTo(r *reposScreen, path string) {
	h.t.Helper()
	for i := 0; i < 40; i++ {
		if got, ok := r.selected(); ok && got.Path == path {
			return
		}
		h.key("down")
	}
	h.t.Fatalf("could not select %s", path)
}

func TestT45CheckFromTheList(t *testing.T) {
	h, p := localHarness(t)
	r := openLocal(h)
	dir := p["acme/api"]
	h.fake.On("git -C "+dir+" rev-parse --show-toplevel", execx.Response{Stdout: dir + "\n"})
	h.fake.On("git -C "+dir+" config --get-regexp", execx.Response{Stdout: "remote.origin.url https://alice@github.com/acme/api.git\n"})
	h.fake.On("git -C "+dir+" config --show-origin --get user.email", execx.Response{Stdout: "file:/home/x/.gitconfig\talice@acme.com\n"})
	h.walkTo(r, dir)
	h.act(keymap.Check)
	c, ok := h.top().(*checkScreen)
	if !ok || c.dir != dir {
		t.Fatalf("top %T", h.top())
	}
	h.waitFor("result", func() bool { return c.phase == chResult })
	h.key("esc")
	if h.top() != Screen(r) {
		t.Fatal("not back on the browser")
	}
	// enter does the same
	h.key("enter")
	if _, ok := h.top().(*checkScreen); !ok {
		t.Fatalf("enter: top %T", h.top())
	}
}

func TestT46FixFromTheList(t *testing.T) {
	h, p := localHarness(t)
	r := openLocal(h)
	dir := p["acme/org/x"]
	h.fake.On("git -C "+dir+" rev-parse --show-toplevel", execx.Response{Stdout: dir + "\n"})
	h.fake.On("git -C "+dir+" config --get-regexp", execx.Response{Stdout: "remote.origin.url https://github.com/acme/x.git\n"})
	h.fake.On("git -C "+dir+" config --show-origin --get user.email", execx.Response{Stdout: "file:/home/x/.gitconfig\talice@acme.com\n"})
	h.fake.On("git -C "+dir+" remote set-url", execx.Response{})
	h.walkTo(r, dir)
	h.act(keymap.Fix)
	c, ok := h.top().(*checkScreen)
	if !ok {
		t.Fatalf("top %T", h.top())
	}
	h.waitFor("fix confirm", func() bool { return c.phase == chFixConfirm })
	// after the fix git reports the new remote
	h.fake.On("git -C "+dir+" config -z --show-origin --get-regexp", execx.Response{Stdout: tuiRepo{url: "https://alice@github.com/acme/x.git", email: "alice@acme.com"}.stdout()})
	h.act(keymap.Yes)
	h.waitFor("fixed", func() bool { return h.fake.Count("git -C "+dir+" remote set-url") == 1 && c.fixed })
	h.waitFor("checked again", func() bool { return c.phase == chResult })
	h.key("esc") // back to the browser: it reads the repository again
	h.waitFor("re-inspected", func() bool {
		got, ok := r.rowFor(dir)
		return ok && got.Status == app.StatusOK && !got.Problem()
	})
}

// rowFor finds the classified repository at path.
func (s *reposScreen) rowFor(path string) (app.LocalRepo, bool) {
	for _, c := range s.scans {
		for _, r := range c.Repos {
			if r.Path == path {
				return r, true
			}
		}
	}
	return app.LocalRepo{}, false
}

func TestT47FixOnlyWhenFixable(t *testing.T) {
	h, p := localHarness(t)
	r := openLocal(h)
	h.walkTo(r, p["acme/bb"]) // foreign host: nothing to fix
	if strings.Contains(plain(h.helpLine()), tr("tui.key.fix")) {
		t.Fatalf("fix offered: %q", h.helpLine())
	}
	before := h.snapshot()
	h.act(keymap.Fix)
	if h.snapshot() != before {
		t.Fatal("f changed something on a repository without a fix")
	}
	h.walkTo(r, p["acme/org/x"])
	if !strings.Contains(plain(h.helpLine()), tr("tui.key.fix")) {
		t.Fatalf("fix not offered: %q", h.helpLine())
	}
}

// T-48: the scan goes on behind a pushed check screen.
func TestT48ScanContinuesBehindAPushedScreen(t *testing.T) {
	h, p := localHarness(t)
	release := make(chan struct{})
	for _, rp := range localRepos() {
		dir := filepath.Join(h.e.Home, "Code", rp.rel)
		blk := (chan struct{})(nil)
		if rp.rel != "acme/api" {
			blk = release
		}
		h.fake.On("git -C "+dir+" config -z --show-origin --get-regexp", execx.Response{Stdout: rp.stdout(), Block: blk})
	}
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	dir := p["acme/api"]
	h.fake.On("git -C "+dir+" rev-parse --show-toplevel", execx.Response{Stdout: dir + "\n"})
	h.fake.On("git -C "+dir+" config --get-regexp", execx.Response{Stdout: "remote.origin.url https://alice@github.com/acme/api.git\n"})
	h.fake.On("git -C "+dir+" config --show-origin --get user.email", execx.Response{Stdout: "file:/home/x/.gitconfig\talice@acme.com\n"})
	h.act(keymap.Clone)
	r := reposOf(h)
	h.key("shift+tab")
	h.waitFor("first repo", func() bool { _, ok := r.selected(); return ok })
	if !r.scanning {
		t.Fatal("scan ended early")
	}
	h.act(keymap.Check)
	if _, ok := h.top().(*checkScreen); !ok {
		t.Fatalf("top %T", h.top())
	}
	close(release)
	h.waitFor("scan finished behind the check screen", func() bool { return !r.scanning })
	h.key("esc")
	if h.top() != Screen(r) {
		t.Fatal("not back")
	}
	if n, _ := r.localCount(); n != 7 {
		t.Fatalf("%d repos", n)
	}
}

func TestT49CopyPath(t *testing.T) {
	h, p := localHarness(t)
	r := openLocal(h)
	h.fake.On("pbcopy", execx.Response{})
	h.walkTo(r, p["acme/api"])
	h.act(keymap.Copy)
	h.waitFor("copied", func() bool { return h.fake.Count("pbcopy") == 1 })
	var stdin string
	for _, c := range h.fake.Calls() {
		if c.Name == "pbcopy" {
			stdin = c.Stdin
		}
	}
	if stdin != p["acme/api"] {
		t.Fatalf("pbcopy stdin %q", stdin)
	}
	h.waitFor("status", func() bool { return strings.Contains(h.status(), tr("tui.repos.copied", "")[:6]) })
}

func TestT50CopyFailsShowsThePath(t *testing.T) {
	h, p := localHarness(t)
	r := openLocal(h)
	h.walkTo(r, p["acme/api"])
	h.act(keymap.Copy) // pbcopy is not scripted
	h.waitFor("warning", func() bool { return strings.Contains(h.status(), "~/Code/acme/api") })
	if !strings.Contains(h.status(), strings.Split(tr("tui.repos.copy_failed", "x"), ".")[0]) {
		t.Fatalf("status %q", h.status())
	}
}

func TestT51Filter(t *testing.T) {
	h, _ := localHarness(t)
	r := openLocal(h)
	h.act(keymap.Filter)
	if !r.Capturing() {
		t.Fatal("not capturing while the filter is typed")
	}
	h.typeText("q") // an ordinary letter is typed, not the quit key
	if h.quit() {
		t.Fatal("q quit while typing")
	}
	h.key("esc")
	if r.fmode != filterOff || r.filter.Value() != "" {
		t.Fatalf("esc: mode %v %q", r.fmode, r.filter.Value())
	}
	h.act(keymap.Filter)
	h.typeText("org")
	v := plain(h.view())
	if !strings.Contains(v, "org/x") || strings.Contains(v, "✓ api") || strings.Contains(v, "bb") {
		t.Fatalf("filtered view:\n%s", v)
	}
	h.key("enter") // applied
	if r.fmode != filterApplied || r.Capturing() {
		t.Fatalf("mode %v", r.fmode)
	}
	h.mustContainPlain(tr("tui.repos.local.clear_filter"))
	h.key("esc") // StepBack clears the filter, the screen stays
	if r.fmode != filterOff || h.top() != Screen(r) {
		t.Fatalf("esc: mode %v", r.fmode)
	}
	h.act(keymap.Filter)
	h.typeText("zzzz")
	h.mustContainPlain(tr("tui.repos.local.no_match", "zzzz"))
}

func TestT52ProblemsOnly(t *testing.T) {
	h, _ := localHarness(t)
	r := openLocal(h)
	h.act(keymap.Problems)
	v := plain(h.view())
	if !r.problemsOnly || strings.Contains(v, "✓ api") || !strings.Contains(v, "org/x") || !strings.Contains(v, tr("tui.repos.local.problems_only")) {
		t.Fatalf("problems only:\n%s", v)
	}
	h.act(keymap.Problems)
	v = plain(h.view())
	if r.problemsOnly || !strings.Contains(v, "api") {
		t.Fatalf("restored:\n%s", v)
	}
}

func TestT53Groups(t *testing.T) {
	h, _ := localHarness(t)
	r := openLocal(h)
	for _, want := range []string{"acme · ~/Code/acme/", "globex · ~/Code/globex/", "beta · ~/Code/beta/", tr("tui.repos.local.all")} {
		h.key("right")
		if !strings.Contains(plain(h.view()), "← "+want) {
			t.Fatalf("group %q:\n%s", want, h.view())
		}
	}
	h.key("left")
	if r.group != 3 || !strings.Contains(plain(h.view()), "beta · ") || strings.Contains(plain(h.view()), "✓ api") {
		t.Fatalf("left: group %d", r.group)
	}
}

func TestT54Rescan(t *testing.T) {
	h, _ := localHarness(t)
	r := openLocal(h)
	calls := gitCalls(h)
	h.act(keymap.Reload)
	h.waitFor("rescanned", func() bool { return !r.scanning })
	if got := gitCalls(h); got != 2*calls {
		t.Fatalf("git calls %d, want %d", got, 2*calls)
	}
	if n, _ := r.localCount(); n != 7 {
		t.Fatalf("%d repos", n)
	}
}

func TestT55EscCancelsTheScanFirst(t *testing.T) {
	h, _ := localHarness(t)
	release := make(chan struct{})
	for _, rp := range localRepos() {
		dir := filepath.Join(h.e.Home, "Code", rp.rel)
		h.fake.On("git -C "+dir+" config -z --show-origin --get-regexp", execx.Response{Stdout: rp.stdout(), Block: release})
	}
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	h.act(keymap.Clone)
	r := reposOf(h)
	h.key("shift+tab")
	h.key("esc")
	if h.top() != Screen(r) || r.scanning || !r.cancelled {
		t.Fatalf("first esc: top %T scanning %v cancelled %v", h.top(), r.scanning, r.cancelled)
	}
	h.mustContainPlain(defKM.T("tui.repos.local.cancelled"))
	h.key("esc")
	if h.depth() != 1 {
		t.Fatalf("second esc: depth %d", h.depth())
	}
}

func TestT56LeavingCancelsTheGitCalls(t *testing.T) {
	h, _ := localHarness(t)
	cancelled := make(chan struct{}, 16)
	release := make(chan struct{}) // never closed: only the context can end the calls
	for _, rp := range localRepos() {
		dir := filepath.Join(h.e.Home, "Code", rp.rel)
		h.fake.On("git -C "+dir+" config -z --show-origin --get-regexp", execx.Response{Stdout: rp.stdout(), Block: release, Do: func(execx.Cmd) error {
			cancelled <- struct{}{}
			return nil
		}})
	}
	h.act(keymap.Clone)
	r := reposOf(h)
	deadline := time.Now().Add(5 * time.Second)
	for h.fake.Count("git -C") == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if h.fake.Count("git -C") == 0 {
		t.Fatal("no git call started")
	}
	h.key("esc") // URL tab, clURL: esc leaves the screen
	if h.depth() != 1 || r.scanning {
		t.Fatalf("depth %d scanning %v", h.depth(), r.scanning)
	}
	select {
	case <-cancelled:
		t.Fatal("a blocked call completed instead of being cancelled")
	case <-time.After(300 * time.Millisecond):
	}
	if r.cancel != nil {
		t.Fatal("the scan context was not released")
	}
}

func TestT57SessionCache(t *testing.T) {
	h, _ := localHarness(t)
	openLocal(h)
	calls := gitCalls(h)
	h.key("esc") // leaves
	if h.depth() != 1 {
		t.Fatalf("depth %d", h.depth())
	}
	h.act(keymap.Clone)
	r := reposOf(h)
	if r.scanning {
		t.Fatal("the cached result was scanned again")
	}
	if gitCalls(h) != calls {
		t.Fatalf("git calls %d -> %d", calls, gitCalls(h))
	}
	h.mustContainPlain(tr("tui.repos.tab.local") + " 7")
}

func TestT58PartialCacheRescans(t *testing.T) {
	h, _ := localHarness(t)
	release := make(chan struct{})
	for _, rp := range localRepos() {
		dir := filepath.Join(h.e.Home, "Code", rp.rel)
		h.fake.On("git -C "+dir+" config -z --show-origin --get-regexp", execx.Response{Stdout: rp.stdout(), Block: release})
	}
	h.act(keymap.Clone)
	h.key("shift+tab")
	h.waitFor("git calls started", func() bool { return gitCalls(h) > 0 })
	h.key("esc") // cancel
	h.key("esc") // leave
	if h.m.sh.repos.complete {
		t.Fatal("a cancelled scan is complete")
	}
	started := gitCalls(h)
	close(release)
	h.act(keymap.Clone)
	r := reposOf(h)
	h.waitFor("done", func() bool { return !r.scanning })
	if gitCalls(h) <= started {
		t.Fatalf("a partial cache was used: git calls %d -> %d", started, gitCalls(h))
	}
	if n, _ := r.localCount(); n != 7 || !h.m.sh.repos.complete {
		t.Fatalf("%d repos, complete %v", n, h.m.sh.repos.complete)
	}
}

func TestT59ConfigChangeClassifiesAgainWithoutIO(t *testing.T) {
	h, p := localHarness(t)
	r := openLocal(h)
	calls := len(h.fake.Calls())
	api, _ := r.rowFor(p["acme/api"])
	if api.Status != app.StatusOK {
		t.Fatalf("api %+v", api)
	}
	cfg := basicCfg()
	cfg.Companies[0].Accounts[0].Username = "alicia" // alice -> alicia (acme and beta alike)
	cfg.Companies[2].Accounts[0].Username = "alicia"
	h.send(cfgChangedMsg{*cfg})
	api, _ = r.rowFor(p["acme/api"])
	if api.Status != app.StatusUnknownAccount {
		t.Fatalf("api after the change: %+v", api)
	}
	if len(h.fake.Calls()) != calls || r.scanning {
		t.Fatalf("calls %d -> %d scanning %v", calls, len(h.fake.Calls()), r.scanning)
	}
}

func TestT60BaseDirChangeRescans(t *testing.T) {
	h, _ := localHarness(t)
	r := openLocal(h)
	cfg := basicCfg()
	cfg.Companies = append(cfg.Companies, config.Company{Slug: "newco", Name: "New", BaseDir: "~/Code/newco/", DefaultAccount: "n",
		Accounts: []config.Account{{ID: "n", Platform: "github", Username: "n", Name: "N", Email: "n@new.io", Auth: config.AuthHTTPS}}})
	h.send(cfgChangedMsg{*cfg})
	h.waitFor("done", func() bool { return !r.scanning })
	found := false
	for _, c := range r.scans {
		if c.Slug == "newco" {
			found = true
		}
	}
	if !found || len(h.m.sh.repos.baseDirs) != 4 {
		t.Fatalf("newco not scanned: %v", h.m.sh.repos.baseDirs)
	}
}

func TestT61NoCompanies(t *testing.T) {
	h := harnessFor(t, nil, nil)
	t.Cleanup(h.m.shutdown)
	h.act(keymap.Clone)
	r := reposOf(h)
	h.key("shift+tab")
	h.mustContainPlain(tr("tui.repos.local.no_company"))
	if r.scanning || h.fake.Count("git") != 0 {
		t.Fatalf("scanning %v, git calls %v", r.scanning, h.fake.CallStrings())
	}
	if len(r.localHelp()) == 0 {
		t.Fatal("no help")
	}
	if strings.Contains(plain(h.helpLine()), tr("tui.key.filter")) {
		t.Fatalf("help offers keys that do nothing: %q", h.helpLine())
	}
}

func TestT62To63EmptyStates(t *testing.T) {
	h, _ := localHarness(t)
	openLocal(h)
	// globex has no folder, beta and acme have repos
	h.mustContainPlain(tr("tui.repos.local.missing", "~/Code/globex/"))
	mustMkdir(t, filepath.Join(h.e.Home, "Code", "globex"))
	r := reposOf(h)
	h.act(keymap.Reload)
	h.waitFor("rescanned", func() bool { return !r.scanning })
	h.mustContainPlain(tr("tui.repos.local.empty", "~/Code/globex/"))
}

func TestT64Denied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	h, _ := localHarness(t)
	locked := filepath.Join(h.e.Home, "Code", "acme", "locked")
	mustMkdir(t, locked)
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	openLocal(h)
	h.mustContainPlain(tr("tui.repos.local.denied_one", "~/Code/acme/locked"))
}

func TestT65Truncated(t *testing.T) {
	h, _ := localHarness(t)
	h.m.sh.scanOpts = app.ScanOptions{MaxDirs: 5}
	openLocal(h)
	h.mustContainPlain(tr("tui.repos.local.truncated", "5"))
}

func TestT66To67SmallSizes(t *testing.T) {
	for _, size := range [][2]int{{40, 10}, {50, 24}, {60, 20}, {80, 24}, {100, 30}} {
		for _, lang := range []string{"vi", "en"} {
			i18n.SetLang(lang)
			h, _ := localHarness(t)
			h.resize(size[0], size[1])
			r := openLocal(h)
			for _, name := range []string{"local", "filter", "problems", "url", "online"} {
				switch name {
				case "filter":
					h.act(keymap.Filter)
				case "problems":
					h.key("esc")
					h.act(keymap.Problems)
				case "url":
					h.key("tab")
				case "online":
					h.key("tab")
				}
				v := h.view()
				assertFits(t, v, size[0], size[1], fmt.Sprintf("%s %s", lang, name))
				if strings.Contains(v, "%!") || strings.Contains(v, "tui.repos.") {
					t.Fatalf("%s %dx%d %s: raw key or format error:\n%s", lang, size[0], size[1], name, v)
				}
			}
			if size[0] == 40 && size[1] == 10 {
				r.tab = tabLocal
				r.fmode, r.problemsOnly = filterOff, false
				r.buildRows("")
				if rows := strings.Count(plain(h.view()), "✓"); rows < 1 {
					t.Fatalf("40x10 shows no repository row:\n%s", h.view())
				}
			}
			i18n.SetLang("vi")
		}
	}
}

func TestT66NarrowHidesTheAccountColumn(t *testing.T) {
	h, _ := localHarness(t)
	h.resize(50, 24)
	r := openLocal(h)
	v := plain(h.view())
	if strings.Contains(v, "acme/alice  ") && strings.Contains(h.rowWith("✓ api"), "acme/alice") {
		t.Fatalf("the account column is shown at 50 columns:\n%s", v)
	}
	bar := r.tabBar()
	if !strings.Contains(bar, "▸"+r.tabLabel(r.tab)) || strings.Contains(bar, "▸ ") {
		t.Fatalf("tab bar is not in its short form: %q", bar)
	}
	h.resize(100, 30)
	if !strings.Contains(h.rowWith("✓ api"), "acme/alice") {
		t.Fatalf("no account column at 100 columns:\n%s", plain(h.view()))
	}
}

func TestT68LanguageSwitch(t *testing.T) {
	defer i18n.SetLang("vi")
	for _, lang := range []string{"en", "vi"} {
		i18n.SetLang(lang)
		h, _ := localHarness(t)
		openLocal(h)
		v := plain(h.view())
		for _, want := range []string{tr("tui.repos.tab.local"), tr("tui.repos.status.foreign_host"), tr("tui.repos.local.all")} {
			if !strings.Contains(v, want) {
				t.Errorf("%s: lacks %q", lang, want)
			}
		}
		if strings.Contains(v, "tui.repos.") || strings.Contains(v, "scan.detail") {
			t.Errorf("%s: raw keys:\n%s", lang, v)
		}
	}
}

func TestT69PasteRouting(t *testing.T) {
	h, _ := localHarness(t)
	r := openLocal(h)
	h.pasteText("https://github.com/org/repo.git")
	if r.tab != tabURL || r.url.input.Value() != "https://github.com/org/repo.git" {
		t.Fatalf("tab %v input %q", r.tab, r.url.input.Value())
	}
}

func TestT70IMEOnTheLocalTab(t *testing.T) {
	h, _ := localHarness(t)
	openLocal(h)
	h.sendRunes("â")
	if h.status() != imeStatus().text {
		t.Fatalf("status %q", h.status())
	}
}

func TestT71HelpBar(t *testing.T) {
	h, p := localHarness(t)
	h.resize(180, 30)
	r := openLocal(h)
	h.walkTo(r, p["acme/org/x"])
	bar := plain(h.helpLine())
	order := []string{"▸ k " + tr("tui.key.check"), "f " + tr("tui.key.fix"), "c " + tr("tui.key.copy_path"), "/ " + tr("tui.key.filter"), "! " + tr("tui.key.problems"), "r " + tr("tui.key.rescan"), "tab " + tr("tui.key.tab"), "? " + tr("tui.key.all_keys"), "esc " + tr("tui.key.back")}
	last := -1
	for _, part := range order {
		i := strings.Index(bar, part)
		if i < 0 || i < last {
			t.Fatalf("help bar lacks %q in order: %q", part, bar)
		}
		last = i
	}
	if strings.Contains(bar, "…") {
		t.Fatalf("the help bar is truncated: %q", bar)
	}
}

func TestT72SentinelKeysAreShown(t *testing.T) {
	y, label := sentinelKeys()
	h, p := localHarness(t, y)
	h.resize(180, 30)
	r := openLocal(h)
	h.walkTo(r, p["acme/org/x"])
	bar := plain(h.helpLine())
	for _, a := range []keymap.Action{keymap.Check, keymap.Fix, keymap.Copy, keymap.Filter, keymap.Problems, keymap.Reload} {
		if !strings.Contains(bar, label[a]) {
			t.Errorf("help bar lacks %s (%s): %q", a, label[a], bar)
		}
	}
	// the sentinel key works
	h.key(label[keymap.Problems])
	if !r.problemsOnly {
		t.Fatal("the problems key did nothing")
	}
}

func TestT73CloneEntryPoints(t *testing.T) {
	// from the detail screen
	h, _ := localHarness(t)
	h.resize(50, 24)
	openDetail(h, "acme/bob")
	h.act(keymap.Clone)
	if _, ok := h.top().(*reposScreen); !ok {
		t.Fatalf("detail: top %T", h.top())
	}
	// from apply done
	h2, _ := localHarness(t)
	h2.applyToDone()
	h2.act(keymap.Clone)
	if r, ok := h2.top().(*reposScreen); !ok || r.tab != tabURL {
		t.Fatalf("done: top %T", h2.top())
	}
	// from check "not a repository": the screen is replaced
	h3, _ := localHarness(t)
	h3.fake.On("git -C "+h3.e.Home+" rev-parse --show-toplevel", execx.Response{ExitCode: 128})
	h3.act(keymap.Check)
	h3.waitFor("error", func() bool { c, ok := h3.top().(*checkScreen); return ok && c.phase == chError })
	depth := h3.depth()
	h3.act(keymap.Clone)
	if _, ok := h3.top().(*reposScreen); !ok || h3.depth() != depth {
		t.Fatalf("not a repo: top %T depth %d (was %d)", h3.top(), h3.depth(), depth)
	}
}

func TestT74SymlinkedRepoRow(t *testing.T) {
	h, _ := localHarness(t)
	target := filepath.Join(h.e.Home, "elsewhere", "linked")
	mustMkdir(t, filepath.Join(target, ".git"))
	link := filepath.Join(h.e.Home, "Code", "acme", "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks:", err)
	}
	h.fake.On("git -C "+link+" config -z --show-origin --get-regexp", execx.Response{Stdout: tuiRepo{url: "https://alice@github.com/acme/linked.git", email: "alice@acme.com"}.stdout()})
	openLocal(h)
	if row := h.rowWith("link"); !strings.Contains(row, tr("tui.repos.flag.via_symlink")) {
		t.Fatalf("row %q", row)
	}
}

func TestT75BareRow(t *testing.T) {
	h, _ := localHarness(t)
	bare := addRepos(t, h, []tuiRepo{{rel: "acme/proj.git", kind: "bare", url: "https://alice@github.com/acme/proj.git"}})
	r := openLocal(h)
	h.walkTo(r, bare["acme/proj.git"])
	help := plain(h.helpLine())
	if strings.Contains(help, tr("tui.key.check")) || strings.Contains(help, tr("tui.key.fix")) || !strings.Contains(help, tr("tui.key.copy_path")) {
		t.Fatalf("help of a bare repo: %q", help)
	}
	before := h.snapshot()
	h.act(keymap.Check)
	h.key("enter")
	if h.snapshot() != before {
		t.Fatal("k / enter did something on a bare repository")
	}
	if !strings.Contains(h.rowWith("proj.git"), tr("tui.repos.kind.bare")) {
		t.Fatalf("row %q", h.rowWith("proj.git"))
	}
}

// the Online tab lists the accounts; with no token anywhere it says so and
// makes no request (TempEnv's HTTP transport would fail the test)
func TestOnlineTabListsAccounts(t *testing.T) {
	h, _ := localHarness(t)
	r := openRepos(h)
	h.key("tab")
	if r.tab != tabOnline {
		t.Fatalf("tab %v", r.tab)
	}
	h.waitFor("settled", func() bool { return !r.onlineBusy() })
	h.mustContainPlain("acme/alice", tr("tui.repos.online.token", "none"))
	if got := plain(h.helpLine()); !strings.Contains(got, tr("tui.key.tab")) || !strings.Contains(got, tr("tui.key.back")) {
		t.Fatalf("help %q", got)
	}
	h.key("esc")
	if h.depth() != 1 {
		t.Fatal("esc does not leave the Online tab")
	}
}

// the session cache never holds more than raw I/O data
func TestSessionHoldsRawData(t *testing.T) {
	h, _ := localHarness(t)
	openRepos(h)
	sess := h.m.sh.repos
	if sess == nil || !sess.complete || len(sess.raw["acme"]) == 0 {
		t.Fatalf("session %+v", sess)
	}
	var _ []app.RawRepo = sess.raw["acme"]
	if !regexp.MustCompile(`Code/acme$`).MatchString(sess.baseDirs[0]) {
		t.Fatalf("base dirs %v", sess.baseDirs)
	}
}

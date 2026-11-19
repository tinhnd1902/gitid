package tui

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/keymap"
	"github.com/tinhnd1902/gitid/internal/online"
	"github.com/tinhnd1902/gitid/internal/testutil"
)

// rowOf is the line of the frame that holds s, without borders and with the
// spaces collapsed ("" when there is none).
func (h *harness) rowOf(s string) string {
	for _, l := range strings.Split(h.view(), "\n") {
		l = strings.Join(strings.Fields(rawFrame(l)), " ")
		if strings.Contains(l, s) {
			return l
		}
	}
	return ""
}

// bar is the account bar of the Online tab.
func (h *harness) bar() string { return h.rowOf(" · ") }

// T-80: nothing looks for a token or touches the network before the Online
// tab is shown.
func TestT80NothingBeforeTheOnlineTab(t *testing.T) {
	h, api := onlineHarness(t)
	r := openRepos(h)
	h.key("shift+tab") // Local
	h.key("tab")       // URL
	for _, p := range []string{"gh", "glab", "security", "git -c"} {
		if n := h.fake.Count(p); n != 0 {
			t.Errorf("%d %q calls before the Online tab", n, p)
		}
	}
	if n := len(api.hits()); n != 0 {
		t.Errorf("%d requests before the Online tab", n)
	}
	h.key("tab")
	h.waitFor("settled", func() bool { return !r.onlineBusy() })
	if h.fake.Count("gh") == 0 || len(api.hits()) == 0 {
		t.Error("the Online tab did not start")
	}
}

// T-81: the token source of each account.
func TestT81Sources(t *testing.T) {
	h, _ := onlineHarness(t)
	r := openOnlineTab(h, true)
	want := map[string]string{
		"acme/alice": "env", "acme/bob": "gh", "acme/carol": "none", "globex/dave": "none", "beta/alice": "git",
	}
	for ref, src := range want {
		gotoAccount(h, r, ref)
		if bar := h.bar(); !strings.Contains(bar, ref) || !strings.Contains(bar, tr("tui.repos.online.token", src)) {
			t.Errorf("%s: bar %q lacks token: %s", ref, bar, src)
		}
		h.noSecret()
	}
	// the order of the accounts is the order of the configuration
	var refs []string
	for _, a := range r.oacc {
		refs = append(refs, a.Ref.String())
	}
	if strings.Join(refs, " ") != "acme/alice acme/bob acme/carol globex/dave beta/alice" {
		t.Errorf("order %v", refs)
	}
}

// T-82: an account without a token says what to do and makes no request.
func TestT82NoToken(t *testing.T) {
	h, api := onlineHarness(t)
	r := openOnlineTab(h, true)
	gotoAccount(h, r, "globex/dave")
	h.mustContainPlain(tr("tui.repos.online.no_token_head"))
	// one way per line: no sentence lists three alternatives
	for _, opt := range strings.Split(tr("tui.repos.online.no_token_opts.gitlab", "GITID_TOKEN_GLOBEX__DAVE", "globex/dave"), "\n") {
		h.mustContainPlain(opt)
		if n := strings.Count(opt, ","); n > 0 {
			t.Errorf("option %q is more than one", opt)
		}
	}
	if hb := h.helpLine(); !strings.HasPrefix(hb, "▸ r "+tr("tui.key.retry")) {
		t.Errorf("help bar %q", hb)
	}
	if api.hitsFor("git.example.com") != 0 {
		t.Error("a request was made for an account without a token")
	}
	gotoAccount(h, r, "acme/carol")
	for _, opt := range strings.Split(tr("tui.repos.online.no_token_opts.github", "GITID_TOKEN_ACME__CAROL", "acme/carol"), "\n") {
		h.mustContainPlain(opt)
	}
	// enter, copy and open do nothing without a list
	before := h.snapshot()
	h.key("enter")
	if h.snapshot() != before {
		t.Error("enter did something without a list")
	}
}

// T-83: no account at all.
func TestT83NoAccounts(t *testing.T) {
	api := newFakeAPI(t)
	h := buildHarnessEnv(t, emptyCompanyCfg("acme"), func(e *env.Env) { e.HTTP = api.rt }, nil)
	t.Cleanup(h.m.shutdown)
	openOnlineTab(h, true)
	h.mustContainPlain(tr("tui.repos.online.no_accounts"))
	if len(api.hits()) != 0 {
		t.Error("requests without an account")
	}
}

// T-84: pages stream in; the progress line follows.
func TestT84Streaming(t *testing.T) {
	h, api := onlineHarness(t)
	var pages [][]map[string]any
	for p := 0; p < 3; p++ {
		var pg []map[string]any
		n := 100
		if p == 2 {
			n = 50
		}
		for i := 0; i < n; i++ {
			pg = append(pg, ghJSON("acme", "r"+string(rune('a'+p))+strconv.Itoa(i), nil))
		}
		pages = append(pages, pg)
	}
	api.setPages("alice", pages)
	release := make(chan struct{})
	api.setHook(func(w http.ResponseWriter, r *http.Request, login string, page int) bool {
		if page == 3 && login == "alice" && strings.HasSuffix(r.URL.Path, "/repos") {
			select {
			case <-release:
			case <-r.Context().Done():
				return true
			}
		}
		return false
	})
	r := openRepos(h)
	h.key("tab")
	a := r.oacc[0]
	h.waitFor("two pages", func() bool { st := r.sh.online[a.Key]; return st != nil && st.pages == 2 })
	h.mustContainPlain(tr("tui.repos.online.fetching", "acme/alice", 2, "200"))
	close(release)
	h.waitFor("done", func() bool { return r.sh.online[a.Key].phase == opDone })
	if n := len(r.sh.online[a.Key].repos); n != 250 {
		t.Fatalf("%d repos", n)
	}
	h.mustContainPlain(tr("tui.repos.online.done", "alice", "250"))
	h.noSecret()
}

// T-85: badges and the age of a repository.
func TestT85Badges(t *testing.T) {
	h, _ := onlineHarness(t)
	h.resize(120, 30)
	openOnlineTab(h, true)
	row := h.rowOf("acme/api")
	for _, want := range []string{tr("tui.repos.online.badge.private"), tr("tui.age.days", 3)} {
		if !strings.Contains(row, want) {
			t.Errorf("row %q lacks %q", row, want)
		}
	}
	if row := h.rowOf("acme/web"); !strings.Contains(row, tr("tui.repos.online.badge.archived")) {
		t.Errorf("row %q", row)
	}
	if row := h.rowOf("alice/dots"); !strings.Contains(row, tr("tui.repos.online.badge.fork")) {
		t.Errorf("row %q", row)
	}
	if row := h.rowOf("acme-org/tools"); !strings.Contains(row, tr("tui.repos.online.badge.empty")) {
		t.Errorf("row %q", row)
	}
}

// T-86: a repository that is cloned shows where, and whether that clone has a
// problem.
func TestT86ClonedLocally(t *testing.T) {
	h, _ := onlineHarness(t)
	h.resize(120, 30)
	openOnlineTab(h, true)
	if row := h.rowOf("acme/api"); !strings.Contains(row, "✓") || !strings.Contains(row, "~/Code/acme/api") {
		t.Errorf("cloned and fine: %q", row)
	}
	if row := h.rowOf("acme/x"); !strings.Contains(row, "!") || !strings.Contains(row, "~/Code/acme/org/x") {
		t.Errorf("cloned with a problem: %q", row)
	}
	if row := h.rowOf("acme/web"); strings.Contains(row, "✓") || strings.Contains(row, "~/Code") {
		t.Errorf("not cloned: %q", row)
	}
}

// T-87: a repository two accounts see shows "+N".
func TestT87SeenByAnotherAccount(t *testing.T) {
	h, _ := onlineHarness(t)
	h.resize(120, 30)
	r := openOnlineTab(h, true)
	gotoAccount(h, r, "acme/alice")
	if row := h.rowOf("acme/api"); !strings.Contains(row, trCount("tui.repos.online.also", 2)) {
		t.Errorf("alice: %q", row)
	}
	gotoAccount(h, r, "acme/bob")
	if row := h.rowOf("acme/api"); !strings.Contains(row, trCount("tui.repos.online.also", 2)) {
		t.Errorf("bob: %q", row)
	}
	if row := h.rowOf("bob/notes"); strings.Contains(row, "+") {
		t.Errorf("only bob sees it: %q", row)
	}
}

// selectRepo moves the cursor to the repository with this FullPath.
func selectRepo(h *harness, r *reposScreen, full string) {
	h.t.Helper()
	h.key("home")
	for i := 0; i < len(r.orows)+1; i++ {
		if got, ok := r.selectedOnline(); ok && got.FullPath == full {
			return
		}
		h.key("down")
	}
	h.t.Fatalf("could not select %s", full)
}

// T-88 / T-89: enter on a repository that is not cloned opens the clone flow
// at the account pick; the owner stays, the user is the account's.
func TestT88And89EnterClones(t *testing.T) {
	h, _ := onlineHarness(t)
	r := openOnlineTab(h, true)
	selectRepo(h, r, "acme-org/tools")
	h.key("enter")
	c, ok := h.top().(*cloneScreen)
	if !ok {
		t.Fatalf("top is %T", h.top())
	}
	if c.phase != clPick || c.cands[c.cur].String() != "acme/alice" {
		t.Fatalf("phase %v on %s", c.phase, c.cands[c.cur])
	}
	if c.preview.URL != "https://alice@github.com/acme-org/tools.git" || c.preview.Dir != filepath.Join(h.e.Home, "Code/acme/tools") {
		t.Errorf("preview %+v", c.preview)
	}
	h.noSecret()
	h.key("esc")
	if h.top() != Screen(r) {
		t.Fatalf("esc: top is %T", h.top())
	}
}

// T-90 / T-91: the target folder is taken.
func TestT90And91TargetTaken(t *testing.T) {
	h, _ := onlineHarness(t)
	r := openOnlineTab(h, true)
	mustMkdir(t, filepath.Join(h.e.Home, "Code/acme/tools"))
	selectRepo(h, r, "acme-org/tools")
	h.key("down")
	h.key("up") // the detail is read again
	alt := filepath.Join(h.e.Home, "Code/acme/acme-org-tools")
	h.mustContainPlain(tr(app.OnlineWarnDirFallback, "~/Code/acme/acme-org-tools"))
	h.key("enter")
	c, ok := h.top().(*cloneScreen)
	if !ok || c.preview.Dir != alt {
		t.Fatalf("top %T", h.top())
	}
	h.key("esc")
	// both taken: enter does nothing, the hint says what to do. The folder
	// check of a repository is kept while the cursor moves (INFO-2), so enter
	// is what reads the disk again; it opens nothing when both are taken.
	mustMkdir(t, alt)
	h.key("enter")
	if _, ok := h.top().(*cloneScreen); ok {
		t.Fatal("enter opened the clone flow although both folders exist")
	}
	before := h.snapshot()
	h.key("enter")
	if h.snapshot() != before {
		t.Error("enter did something although both folders exist")
	}
	h.mustContainPlain(strings.Split(tr("tui.repos.online.d.taken", "X"), "X")[0][:20])
	if bar := plain(h.helpLine()); strings.Contains(bar, "enter") && strings.Contains(bar, tr("tui.key.clone")) {
		t.Errorf("the help bar offers enter: %q", bar)
	}
	_ = r
}

// T-92: enter on a repository that is cloned opens the check screen of the
// clone; nothing is cloned.
func TestT92EnterChecksTheClone(t *testing.T) {
	h, _ := onlineHarness(t)
	r := openOnlineTab(h, true)
	local := filepath.Join(h.e.Home, "Code/acme/api")
	h.fake.On("git -C "+local+" rev-parse --show-toplevel", execx.Response{Stdout: local + "\n"})
	h.fake.On("git -C "+local+" config --get-regexp", execx.Response{Stdout: "remote.origin.url https://alice@github.com/acme/api.git\n"})
	h.fake.On("git -C "+local+" config --show-origin --get user.email", execx.Response{Stdout: "file:/home/x/.gitconfig\talice@acme.com\n"})
	selectRepo(h, r, "acme/api")
	h.key("enter")
	c, ok := h.top().(*checkScreen)
	if !ok || c.dir != local {
		t.Fatalf("top %T", h.top())
	}
	if h.fake.Count("git clone") != 0 {
		t.Error("a clone was started")
	}
}

// T-93: cloned with the wrong account: the row warns, enter checks it, and the
// check screen offers the fix.
func TestT93ClonedWithAProblem(t *testing.T) {
	h, _ := onlineHarness(t)
	r := openOnlineTab(h, true)
	dir := filepath.Join(h.e.Home, "Code/acme/org/x")
	h.fake.On("git -C "+dir+" rev-parse --show-toplevel", execx.Response{Stdout: dir + "\n"})
	h.fake.On("git -C "+dir+" config --get-regexp", execx.Response{Stdout: "remote.origin.url https://github.com/acme/x.git\n"})
	h.fake.On("git -C "+dir+" config --show-origin --get user.email", execx.Response{Stdout: "file:/home/x/.gitconfig\talice@acme.com\n"})
	selectRepo(h, r, "acme/x")
	if row := h.rowOf("acme/x"); !strings.Contains(row, "!") {
		t.Errorf("row %q", row)
	}
	h.mustContainPlain(tr("tui.repos.online.d.problem_hint"))
	h.key("enter")
	c, ok := h.top().(*checkScreen)
	if !ok || c.dir != dir {
		t.Fatalf("top %T", h.top())
	}
	h.waitFor("result", func() bool { return c.phase == chResult })
	if !c.canFix() {
		t.Error("no fix offered")
	}
}

// T-94: config.yaml changed on disk since this screen loaded it: no clone, the
// configuration is reloaded.
func TestT94StaleConfig(t *testing.T) {
	h, _ := onlineHarness(t)
	r := openOnlineTab(h, true)
	selectRepo(h, r, "acme/web")
	// another gitid window renames a company
	cfg := cfgWith(h, func(c *config.Config) { c.Companies[2].Name = "Beta renamed" })
	if _, _, err := h.svc.Commit(cfg); err != nil {
		t.Fatal(err)
	}
	h.key("enter")
	if _, ok := h.top().(*cloneScreen); ok {
		t.Fatal("a clone screen opened although config.yaml changed")
	}
	if h.top() != Screen(r) {
		t.Fatalf("top %T", h.top())
	}
	h.waitFor("reloaded", func() bool { return h.m.sh.cfg.Companies[2].Name == "Beta renamed" })
	// the message and the reload request of the stale answer itself
	ref, _ := h.m.sh.cfg.ParseRef("acme/alice")
	_, cmd := r.Update(onlineStaleMsg{screen: r.id, ref: ref, changed: true})
	var gotReload, gotStatus bool
	var walk func(c tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch m := c().(type) {
		case reloadMsg:
			gotReload = true
		case statusMsg:
			gotStatus = m.text == tr("tui.repos.online.stale")
		case tea.BatchMsg:
			for _, sub := range m {
				walk(sub)
			}
		}
	}
	walk(cmd)
	if !gotReload || !gotStatus {
		t.Errorf("reload %v status %v", gotReload, gotStatus)
	}
}

// T-95: an account removed by a reload leaves the tab.
func TestT95AccountRemoved(t *testing.T) {
	h, _ := onlineHarness(t)
	r := openOnlineTab(h, true)
	gotoAccount(h, r, "acme/bob")
	bobKey := r.oacc[r.osel].Key
	if r.sh.online[bobKey] == nil {
		t.Fatal("no state for bob")
	}
	cfg := cfgWith(h, func(c *config.Config) {
		c.Companies[0].Accounts = []config.Account{c.Companies[0].Accounts[0], c.Companies[0].Accounts[2]}
	})
	h.send(cfgChangedMsg{cfg})
	if r.sh.online[bobKey] != nil {
		t.Error("the state of the removed account is kept")
	}
	for _, a := range r.oacc {
		if a.Ref.String() == "acme/bob" {
			t.Error("the selector still has bob")
		}
	}
	if r.osel < 0 || r.osel >= len(r.oacc) {
		t.Errorf("selection %d of %d", r.osel, len(r.oacc))
	}
	h.noSecret()
}

// T-96: a changed username drops the old list and the old token; the token is
// looked for again.
func TestT96UsernameChanged(t *testing.T) {
	h, api := onlineHarness(t)
	r := openOnlineTab(h, true)
	gotoAccount(h, r, "acme/bob")
	old := r.oacc[r.osel].Key
	before := h.fake.Count("gh auth token")
	cfg := cfgWith(h, func(c *config.Config) { c.Companies[0].Accounts[1].Username = "bobby" })
	h.send(cfgChangedMsg{cfg})
	if r.sh.online[old] != nil {
		t.Error("the list of the old identity is kept")
	}
	// the state of the new identity is fetched at once (the tab is open) and
	// the token is looked up again for it
	h.fake.On("gh auth token --hostname=github.com --user=bobby", execx.Response{Stdout: fixtureToken + "_bob\n"})
	h.waitFor("new lookup", func() bool { return !r.onlineBusy() && h.fake.Count("gh auth token") > before })
	// the API still answers for login bob, so the identity check refuses it
	gotoAccount(h, r, "acme/bob")
	h.mustContainPlain(tr("tui.repos.online.err.wrong_user", "bob"))
	_ = api
}

// T-97: left and right switch the account; each keeps its cursor.
func TestT97SwitchAccounts(t *testing.T) {
	h, _ := onlineHarness(t)
	r := openOnlineTab(h, true)
	selectRepo(h, r, "acme/web")
	h.key("right")
	if r.oacc[r.osel].Ref.String() != "acme/bob" {
		t.Fatalf("on %s", r.oacc[r.osel].Ref)
	}
	selectRepo(h, r, "bob/notes")
	h.key("left")
	if got, _ := r.selectedOnline(); got.FullPath != "acme/web" {
		t.Errorf("alice's cursor moved to %s", got.FullPath)
	}
	h.key("right")
	if got, _ := r.selectedOnline(); got.FullPath != "bob/notes" {
		t.Errorf("bob's cursor moved to %s", got.FullPath)
	}
	// it wraps around
	for i := 0; i < len(r.oacc)-1; i++ {
		h.key("right")
	}
	if r.oacc[r.osel].Ref.String() != "acme/alice" {
		t.Errorf("on %s", r.oacc[r.osel].Ref)
	}
}

// T-98: the filter.
func TestT98Filter(t *testing.T) {
	h, _ := onlineHarness(t)
	r := openOnlineTab(h, true)
	h.act(keymap.Filter)
	if !r.Capturing() {
		t.Fatal("the filter does not capture the keys")
	}
	h.typeText("tools")
	if len(r.orows) != 1 {
		t.Fatalf("%d rows", len(r.orows))
	}
	h.key("enter")
	if r.Capturing() || r.ofmode != filterApplied {
		t.Fatal("not applied")
	}
	h.key("esc") // clears the filter, stays
	if h.top() != Screen(r) || r.ofmode != filterOff || len(r.orows) != 5 {
		t.Fatalf("esc: mode %v rows %d", r.ofmode, len(r.orows))
	}
	h.act(keymap.Filter)
	h.typeText("zzzz")
	h.mustContainPlain(tr("tui.repos.online.no_match", "zzzz"))
	h.key("esc")
	if r.ofmode != filterOff || len(r.orows) != 5 {
		t.Fatal("esc did not clear the filter")
	}
	// q is text while typing
	h.act(keymap.Filter)
	h.typeText("q")
	if h.top() != Screen(r) || r.ofilter.Value() != "q" {
		t.Fatal("q left the screen")
	}
}

// T-99: c copies the rendered URL.
func TestT99CopyURL(t *testing.T) {
	h, _ := onlineHarness(t)
	h.fake.On("pbcopy", execx.Response{})
	r := openOnlineTab(h, true)
	selectRepo(h, r, "acme-org/tools")
	h.act(keymap.Copy)
	h.waitFor("copied", func() bool { return h.fake.Count("pbcopy") == 1 })
	for _, c := range h.fake.Calls() {
		if c.Name == "pbcopy" && c.Stdin != "https://alice@github.com/acme-org/tools.git" {
			t.Errorf("copied %q", c.Stdin)
		}
	}
	h.mustContainPlain(tr("tui.repos.online.copied", "https://alice@github.com/acme-org/tools.git"))
	// a failed copy shows the URL instead
	h2, _ := onlineHarness(t)
	r2 := openOnlineTab(h2, true)
	selectRepo(h2, r2, "acme-org/tools")
	h2.act(keymap.Copy)
	h2.waitFor("warned", func() bool { return strings.Contains(h2.m.status.text, "https://alice@github.com/acme-org/tools.git") })
}

// T-100: o opens the page of the repository.
func TestT100OpenPage(t *testing.T) {
	h, _ := onlineHarness(t)
	h.fake.On("open https://", execx.Response{})
	r := openOnlineTab(h, true)
	selectRepo(h, r, "acme-org/tools")
	h.act(keymap.Open)
	h.waitFor("opened", func() bool { return h.fake.Count("open https://github.com/acme-org/tools") == 1 })
	h.mustContainPlain(tr("tui.repos.online.opened", "https://github.com/acme-org/tools"))
}

// T-101: r forgets the token, looks it up again and fetches again.
func TestT101Refetch(t *testing.T) {
	h, api := onlineHarness(t)
	r := openOnlineTab(h, true)
	gotoAccount(h, r, "acme/bob")
	ghBefore, hitsBefore := h.fake.Count("gh auth token"), len(api.hits())
	h.act(keymap.Reload)
	h.waitFor("fetched again", func() bool { return !r.onlineBusy() && len(api.hits()) > hitsBefore })
	if n := h.fake.Count("gh auth token"); n != ghBefore+1 {
		t.Errorf("%d gh calls, want %d", n, ghBefore+1)
	}
	if st := r.sh.online[r.oacc[r.osel].Key]; st.phase != opDone || len(st.repos) != 2 {
		t.Errorf("state %+v", st)
	}
}

// T-102: esc while fetching stops it and keeps the pages that came; the second
// esc leaves.
func TestT102EscMidFetch(t *testing.T) {
	h, api := onlineHarness(t)
	api.setPages("alice", [][]map[string]any{aliceRepos()[:2], aliceRepos()[2:]})
	started := make(chan struct{}, 4)
	api.setHook(func(w http.ResponseWriter, r *http.Request, login string, page int) bool {
		if page == 2 && login == "alice" && strings.HasSuffix(r.URL.Path, "/repos") {
			started <- struct{}{}
			<-r.Context().Done()
			return true
		}
		return false
	})
	r := openRepos(h)
	h.key("tab")
	a := r.oacc[0]
	<-started
	h.waitFor("first page", func() bool { st := r.sh.online[a.Key]; return st != nil && len(st.repos) == 2 })
	h.key("esc")
	if h.top() != Screen(r) {
		t.Fatal("esc left the screen")
	}
	st := r.sh.online[a.Key]
	if st.phase != opCancelled || len(st.repos) != 2 {
		t.Fatalf("phase %v repos %d", st.phase, len(st.repos))
	}
	h.mustContainPlain(defKM.T("tui.repos.online.cancelled"))
	h.waitFor("others settled", func() bool { return !r.onlineBusy() })
	h.key("esc")
	if h.top() == Screen(r) {
		t.Fatal("the second esc did not leave")
	}
}

// T-103: leaving the screen cancels the request.
func TestT103LeaveMidFetch(t *testing.T) {
	h, api := onlineHarness(t)
	cancelled := make(chan struct{})
	var once sync.Once
	started := make(chan struct{}, 4)
	api.setHook(func(w http.ResponseWriter, r *http.Request, login string, page int) bool {
		if strings.HasSuffix(r.URL.Path, "/user") && login == "alice" {
			started <- struct{}{}
			select {
			case <-r.Context().Done():
				once.Do(func() { close(cancelled) }) // two accounts are the user alice
			case <-time.After(5 * time.Second):
			}
			return true
		}
		return false
	})
	openRepos(h)
	h.key("tab")
	<-started
	h.key("esc") // stops the fetches
	h.key("esc") // leaves
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("the request was not cancelled within a second")
	}
}

// errTransport fails every request the way a network does.
type errTransport struct{ err error }

func (e errTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, e.err }

// onPanel says what the Online tab of an account shows, in the language l.
func panelOf(t *testing.T, h *harness, ref string) string {
	t.Helper()
	r := reposOf(h)
	gotoAccount(h, r, ref)
	h.waitFor("settled", func() bool { return !r.onlineBusy() })
	h.noSecret()
	v := h.view()
	if strings.Contains(v, "tui.repos.online.") || strings.Contains(v, "%!") {
		t.Errorf("a raw key or a format error in the view:\n%s", v)
	}
	return v
}

// withLangs runs f in Vietnamese and in English.
func withLangs(t *testing.T, f func(t *testing.T)) {
	t.Helper()
	for _, lang := range []string{"vi", "en"} {
		t.Run(lang, func(t *testing.T) {
			i18n.SetLang(lang)
			t.Cleanup(func() { i18n.SetLang("vi") })
			f(t)
		})
	}
}

// T-104 .. T-115: one panel per kind of failure, with its message and hint in
// both languages.
func TestT104To115ErrorPanels(t *testing.T) {
	reset := testutil.FixedTime.Add(120 * time.Second)
	type tc struct {
		name string
		// api customises the platform for acme/alice; rt (optional) replaces the transport
		api  func(w http.ResponseWriter, r *http.Request, login string, page int) bool
		rt   http.RoundTripper
		ref  string
		want func() []string // texts that must be in the view
	}
	listFails := func(status int, hdr map[string]string, body any) func(http.ResponseWriter, *http.Request, string, int) bool {
		return func(w http.ResponseWriter, r *http.Request, login string, page int) bool {
			if !strings.HasSuffix(r.URL.Path, "/repos") && !strings.HasSuffix(r.URL.Path, "/user") || login != "alice" {
				return false
			}
			for k, v := range hdr {
				w.Header().Set(k, v)
			}
			writeJSONResp(w, status, body)
			return true
		}
	}
	cases := []tc{
		{name: "401 env", api: listFails(401, nil, map[string]any{"message": "Bad credentials " + fixtureToken}), ref: "acme/alice",
			want: func() []string {
				return []string{tr("tui.repos.online.err.auth"), tr("tui.repos.online.hint.auth.env", "GITID_TOKEN_ACME__ALICE")}
			}},
		{name: "401 gh", api: func(w http.ResponseWriter, r *http.Request, login string, page int) bool {
			if login == "bob" {
				writeJSONResp(w, 401, map[string]any{"message": "Bad credentials"})
				return true
			}
			return false
		}, ref: "acme/bob", want: func() []string {
			return []string{tr("tui.repos.online.err.auth"), tr("tui.repos.online.hint.auth.gh")}
		}},
		{name: "401 git looks like a password", api: func(w http.ResponseWriter, r *http.Request, login string, page int) bool {
			if strings.HasSuffix(r.Header.Get("Authorization"), "_beta") {
				writeJSONResp(w, 401, map[string]any{"message": "Bad credentials"})
				return true
			}
			return false
		}, ref: "beta/alice", want: func() []string {
			return []string{tr("tui.repos.online.err.auth"), tr("tui.repos.online.hint.auth.git_password", "GITID_TOKEN_BETA__ALICE")}
		}},
		{name: "401 OTP", api: listFails(401, map[string]string{"X-GitHub-OTP": "required; sms"}, map[string]any{"message": "otp"}), ref: "acme/alice",
			want: func() []string {
				return []string{tr("tui.repos.online.err.otp"), tr("tui.repos.online.hint.otp", "GITID_TOKEN_ACME__ALICE")}
			}},
		{name: "403 scope", api: listFails(403, nil, map[string]any{"message": "Resource not accessible by personal access token"}), ref: "acme/alice",
			want: func() []string {
				return []string{tr("tui.repos.online.err.scope"), tr("tui.repos.online.hint.scope.github")}
			}},
		{name: "403 SSO", api: listFails(403, map[string]string{"X-GitHub-SSO": "required; url=https://github.com/orgs/acme/sso?authorization_request=abc"}, map[string]any{"message": "sso"}), ref: "acme/alice",
			want: func() []string {
				return []string{tr("tui.repos.online.err.sso"), tr("tui.repos.online.hint.sso", "https://github.com/orgs/acme/sso?authorization_request=abc")}
			}},
		{name: "429 reset", api: listFails(429, map[string]string{"Retry-After": "120"}, map[string]any{"message": "slow"}), ref: "acme/alice",
			want: func() []string {
				return []string{tr("tui.repos.online.err.rate_limit", reset.In(time.Local).Format("15:04"))}
			}},
		{name: "500", api: listFails(500, nil, map[string]any{"message": "boom"}), ref: "acme/alice",
			want: func() []string {
				return []string{tr("tui.repos.online.err.server", 500), defKM.T("tui.repos.online.hint.retry")}
			}},
		{name: "html", api: func(w http.ResponseWriter, r *http.Request, login string, page int) bool {
			if login != "alice" {
				return false
			}
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>Sign in</html>"))
			return true
		}, ref: "acme/alice", want: func() []string {
			return []string{tr("tui.repos.online.err.bad_response"), tr("tui.repos.online.hint.bad_response")}
		}},
		{name: "wrong user", api: func(w http.ResponseWriter, r *http.Request, login string, page int) bool {
			if strings.HasSuffix(r.URL.Path, "/user") && login == "alice" {
				writeJSONResp(w, 200, map[string]any{"login": "mallory"})
				return true
			}
			return false
		}, ref: "acme/alice", want: func() []string {
			return []string{tr("tui.repos.online.err.wrong_user", "mallory"), tr("tui.repos.online.hint.wrong_user", "GITID_TOKEN_ACME__ALICE")}
		}},
		{name: "dns", rt: errTransport{&net.DNSError{Err: "no such host", Name: "api.github.com", IsNotFound: true}}, ref: "acme/alice",
			want: func() []string {
				return []string{tr("tui.repos.online.err.dns"), defKM.T("tui.repos.online.hint.retry")}
			}},
		{name: "timeout", rt: errTransport{context.DeadlineExceeded}, ref: "acme/alice",
			want: func() []string { return []string{tr("tui.repos.online.err.timeout")} }},
		{name: "tls", rt: errTransport{x509.UnknownAuthorityError{}}, ref: "acme/alice",
			want: func() []string { return []string{tr("tui.repos.online.err.tls"), tr("tui.repos.online.hint.tls")} }},
		{name: "proxy", rt: errTransport{&net.OpError{Op: "proxyconnect", Net: "tcp", Err: errors.New("refused")}}, ref: "acme/alice",
			want: func() []string { return []string{tr("tui.repos.online.err.proxy"), tr("tui.repos.online.hint.proxy")} }},
		{name: "network", rt: errTransport{&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}}, ref: "acme/alice",
			want: func() []string { return []string{tr("tui.repos.online.err.network")} }},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			withLangs(t, func(t *testing.T) {
				api := newFakeAPI(t)
				rt := c.rt
				if rt == nil {
					rt = api.rt
					api.setHook(c.api)
				}
				h := onlineHarnessRT(t, api, rt)
				h.resize(120, 40)
				openRepos(h)
				h.key("tab")
				v := panelOf(t, h, c.ref)
				pv := plain(v)
				for _, w := range c.want() {
					if !strings.Contains(pv, plain(w)) {
						t.Errorf("the panel lacks %q:\n%s", w, v)
					}
				}
			})
		})
	}
}

// the SSO partial warning on a list that worked, and the page cap
func TestT110SSOPartialAndTruncated(t *testing.T) {
	withLangs(t, func(t *testing.T) {
		api := newFakeAPI(t)
		api.setHook(func(w http.ResponseWriter, r *http.Request, login string, page int) bool {
			if login == "alice" && strings.HasSuffix(r.URL.Path, "/repos") {
				w.Header().Set("X-GitHub-SSO", "partial-results; organizations=1,2")
				w.Header().Set("X-OAuth-Scopes", "read:user")
				// an endless list: the page cap stops it
				q := r.URL.Query()
				q.Set("page", fmt.Sprint(page+1))
				w.Header().Set("Link", fmt.Sprintf(`<https://%s%s?%s>; rel="next"`, r.Host, r.URL.Path, q.Encode()))
				writeJSONResp(w, 200, []map[string]any{ghJSON("acme", "r"+strconv.Itoa(page), nil)})
				return true
			}
			return false
		})
		h := onlineHarnessRT(t, api, api.rt)
		h.resize(120, 40)
		openRepos(h)
		h.key("tab")
		v := plain(panelOf(t, h, "acme/alice"))
		for _, w := range []string{
			tr("tui.repos.online.warn.sso_partial"), tr("tui.repos.online.warn.no_repo_scope"),
			tr("tui.repos.online.truncated", online.DefaultMaxPages, strconv.Itoa(online.DefaultMaxPages)),
		} {
			if !strings.Contains(v, plain(w)) {
				t.Errorf("the panel lacks %q:\n%s", w, v)
			}
		}
		// the rows that came are listed
		if !strings.Contains(v, "acme/r1") {
			t.Errorf("no rows:\n%s", v)
		}
	})
}

// an account with no repository
func TestEmptyAccount(t *testing.T) {
	withLangs(t, func(t *testing.T) {
		api := newFakeAPI(t)
		api.setPages("alice", [][]map[string]any{{}})
		h := onlineHarnessRT(t, api, api.rt)
		openRepos(h)
		h.key("tab")
		v := plain(panelOf(t, h, "acme/alice"))
		if !strings.Contains(v, plain(tr("tui.repos.online.empty", "alice"))) {
			t.Errorf("view:\n%s", v)
		}
	})
}

// the platform's text cannot put an escape sequence on the screen (SEC-11)
func TestSEC11EscapeInjection(t *testing.T) {
	api := newFakeAPI(t)
	api.setPages("alice", [][]map[string]any{{ghJSON("acme", "evil\x1b]0;pwn\x07name", nil), ghJSON("acme", "ok", nil)}})
	api.setHook(func(w http.ResponseWriter, r *http.Request, login string, page int) bool {
		if login == "bob" {
			writeJSONResp(w, 403, map[string]any{"message": "denied \x1b[31mred\x1b[0m \x9b2J " + fixtureToken + "_bob"})
			return true
		}
		return false
	})
	h := onlineHarnessRT(t, api, api.rt)
	openRepos(h)
	h.key("tab")
	v := panelOf(t, h, "acme/alice")
	if !strings.Contains(v, "acme/ok") {
		t.Errorf("the good row is missing:\n%s", v)
	}
	// the hostile row is there, cleaned: the escape sequence is gone whole
	// (its payload too), not drawn and not left half way
	if row := h.rowOf("acme/evilname"); row == "" {
		t.Errorf("the cleaned row acme/evilname is missing:\n%s", v)
	}
	assertViewClean(t, v)
	for _, bad := range []string{"pwn", "\x1b", "\x07", "]0;", "evil\x1b"} {
		if strings.Contains(v, bad) {
			t.Errorf("the view still holds %q:\n%q", bad, v)
		}
	}
	v = panelOf(t, h, "acme/bob")
	if strings.Contains(v, "\x1b") || strings.Contains(v, "\x9b") {
		t.Errorf("an escape from the API reached the view:\n%q", v)
	}
	if !strings.Contains(plain(v), "denied") {
		t.Errorf("the message is gone:\n%s", v)
	}
}

// the Online tab fits every size the TUI supports, in every state, in
// Vietnamese and in English
func TestOnlineFitsSizes(t *testing.T) {
	withLangs(t, func(t *testing.T) {
		api := newFakeAPI(t)
		h := onlineHarnessRT(t, api, api.rt)
		r := openRepos(h)
		h.key("tab")
		h.waitFor("settled", func() bool { return !r.onlineBusy() })
		for _, size := range [][2]int{{40, 10}, {60, 20}, {80, 24}, {100, 30}, {120, 40}} {
			h.resize(size[0], size[1])
			for _, ref := range []string{"acme/alice", "acme/bob", "acme/carol", "globex/dave", "beta/alice"} {
				gotoAccount(h, r, ref)
				assertFits(t, h.view(), size[0], size[1], "online "+ref)
				h.noSecret()
			}
			gotoAccount(h, r, "acme/alice")
			h.act(keymap.Filter)
			h.typeText("a")
			assertFits(t, h.view(), size[0], size[1], "online filter")
			h.key("esc")
			h.act(keymap.Help)
			assertFits(t, h.view(), size[0], size[1], "online overlay")
			h.key("esc")
		}
		// at 40x10 the list still shows at least a row
		h.resize(40, 10)
		if !strings.Contains(plain(h.view()), "acme/") {
			t.Errorf("no row at 40x10:\n%s", h.view())
		}
	})
}

// vi and en: nothing shows a raw key
func TestOnlineLanguageSwitch(t *testing.T) {
	h, _ := onlineHarness(t)
	r := openOnlineTab(h, true)
	for _, lang := range []string{"en", "vi"} {
		i18n.SetLang(lang)
		for _, ref := range []string{"acme/alice", "acme/carol", "globex/dave"} {
			gotoAccount(h, r, ref)
			if v := h.view(); strings.Contains(v, "tui.repos.") || strings.Contains(v, "tui.key.") || strings.Contains(v, "%!") {
				t.Errorf("%s %s: raw key in the view:\n%s", lang, ref, v)
			}
		}
	}
	i18n.SetLang("vi")
}

// the help bar of the Online tab: the next step first, nothing cut
func TestOnlineHelpBar(t *testing.T) {
	h, _ := onlineHarness(t)
	h.resize(180, 30)
	r := openOnlineTab(h, true)
	selectRepo(h, r, "acme/web")
	bar := plain(h.helpLine())
	order := []string{"▸ enter " + tr("tui.key.clone"), "c " + tr("tui.key.copy_url"), "o " + tr("tui.key.open_page"), "←/→ " + tr("tui.key.account"), "/ " + tr("tui.key.filter"), "r " + tr("tui.key.refetch"), "tab " + tr("tui.key.tab"), "? " + tr("tui.key.all_keys"), "esc " + tr("tui.key.back")}
	last := -1
	for _, part := range order {
		i := strings.Index(bar, part)
		if i < 0 || i < last {
			t.Fatalf("the help bar lacks %q in order: %q", part, bar)
		}
		last = i
	}
	if strings.Contains(bar, "…") {
		t.Fatalf("the help bar is cut: %q", bar)
	}
	// on a cloned repository enter checks
	selectRepo(h, r, "acme/api")
	if bar := plain(h.helpLine()); !strings.Contains(bar, "▸ enter "+tr("tui.key.check")) {
		t.Errorf("cloned: %q", bar)
	}
	// without a token there is no list: no copy, open, filter or refetch of rows
	gotoAccount(h, r, "globex/dave")
	bar = plain(h.helpLine())
	for _, bad := range []string{tr("tui.key.copy_url"), tr("tui.key.open_page"), tr("tui.key.filter")} {
		if strings.Contains(bar, bad) {
			t.Errorf("no-token help bar shows %q: %q", bad, bar)
		}
	}
}

// with the sentinel keymap every key of the Online tab is an alt+ token and works
func TestOnlineSentinelKeys(t *testing.T) {
	y, label := sentinelKeys()
	h, _ := onlineHarness(t, y)
	h.resize(180, 30)
	r := openOnlineTab(h, true)
	selectRepo(h, r, "acme/web")
	bar := plain(h.helpLine())
	for _, a := range []keymap.Action{keymap.Copy, keymap.Open, keymap.Filter, keymap.Reload} {
		if !strings.Contains(bar, label[a]) {
			t.Errorf("help bar lacks %s (%s): %q", a, label[a], bar)
		}
	}
	h.key(label[keymap.Filter])
	if !r.Capturing() {
		t.Fatal("the sentinel filter key did nothing")
	}
}

// the old placeholder is gone: a paste on the Online tab goes to the URL tab
func TestOnlinePasteRouting(t *testing.T) {
	h, _ := onlineHarness(t)
	r := openOnlineTab(h, true)
	h.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("https://github.com/org/repo.git"), Paste: true})
	if r.tab != tabURL || r.url.input.Value() != "https://github.com/org/repo.git" {
		t.Fatalf("tab %v value %q", r.tab, r.url.input.Value())
	}
}

// the tab label and the IME status
func TestOnlineIME(t *testing.T) {
	h, _ := onlineHarness(t)
	openOnlineTab(h, true)
	h.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ư")})
	if h.m.status.text != tr("tui.status.ime") {
		t.Errorf("status %q", h.m.status.text)
	}
}

// the second visit to the tab uses the cache: no new token lookup, no request
func TestOnlineSessionCache(t *testing.T) {
	h, api := onlineHarness(t)
	r := openOnlineTab(h, true)
	gh, hits := h.fake.Count("gh auth token"), len(api.hits())
	h.key("esc") // leaves the browser
	if h.top() == Screen(r) {
		t.Fatal("still on the browser")
	}
	r2 := openRepos(h)
	h.key("tab")
	h.waitFor("settled", func() bool { return !r2.onlineBusy() })
	// accounts that ended with a list are not fetched again; the ones without
	// a token are resolved from the token cache (no new lookup either)
	if n := h.fake.Count("gh auth token"); n != gh {
		t.Errorf("%d gh calls, want %d", n, gh)
	}
	if n := len(api.hits()); n != hits {
		t.Errorf("%d requests, want %d", n, hits)
	}
	h.mustContainPlain("acme/api")
}

package tui

// Adversarial tests of the Online tab (B2): a hostile platform, hostile
// repository names, and the guarantee that the fixture token (fixtureToken)
// is in no frame, no status line, no command the runner saw and no file of
// the temp HOME. onlineHarnessRT already scans the runner calls and the disk
// when the test ends; panelOf scans every frame it draws.

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/online"
	"github.com/tinhnd1902/gitid/internal/testutil"
)

// assertViewClean fails when the frame holds a control character, an escape
// or a Unicode format character (bidi marks, zero-width characters): nothing
// from the network may reach the terminal.
func assertViewClean(t *testing.T, v string) {
	t.Helper()
	for _, r := range v {
		switch {
		case r == '\n':
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			t.Fatalf("the frame holds U+%04X:\n%q", r, v)
		}
	}
}

// echoHeaders puts the token in the response headers a hostile server could
// choose, Location included.
func echoHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Echo", fixtureToken)
	h.Set("Location", "http://github.com/login?access_token="+fixtureToken)
	h.Set("WWW-Authenticate", `Bearer realm="`+fixtureToken+`"`)
	h.Set("X-Request-Token", fixtureToken)
}

// aliceOnly makes the hook act on the alice account's calls (the other
// accounts keep working) and, with onlyRepos, only on the list, not on /user.
func aliceOnly(onlyRepos bool, f func(w http.ResponseWriter, r *http.Request, page int)) func(http.ResponseWriter, *http.Request, string, int) bool {
	return func(w http.ResponseWriter, r *http.Request, login string, page int) bool {
		if login != "alice" || !strings.HasSuffix(r.Header.Get("Authorization"), fixtureToken) {
			return false
		}
		if onlyRepos && !strings.HasSuffix(r.URL.Path, "/repos") {
			return false
		}
		f(w, r, page)
		return true
	}
}

// gzipBomb is a small gzip stream that unpacks to 96 MiB.
func gzipBomb(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&b, gzip.BestCompression)
	_, _ = zw.Write([]byte(`[{"name":"`))
	chunk := bytes.Repeat([]byte("a"), 1<<20)
	for i := 0; i < 96; i++ {
		_, _ = zw.Write(chunk)
	}
	_, _ = zw.Write([]byte(`"}]`))
	_ = zw.Close()
	return b.Bytes()
}

// A hostile platform, one panel per attack. The panel says what is wrong,
// the token is nowhere, and the hostile text does not reach the frame.
func TestAdvHostilePlatform(t *testing.T) {
	reset := testutil.FixedTime.Add(time.Hour)
	bomb := gzipBomb(t)
	type tc struct {
		name string
		hook func(w http.ResponseWriter, r *http.Request, login string, page int) bool
		want func() []string
		not  []string // must not be in the frame
		// hosts the server must never be asked for
		never []string
	}
	echoBody := map[string]any{"message": "bad " + fixtureToken, "error": fixtureToken, "error_description": fixtureToken}
	fail := func(status int, extra func(http.Header)) func(http.ResponseWriter, *http.Request, string, int) bool {
		return aliceOnly(false, func(w http.ResponseWriter, r *http.Request, _ int) {
			echoHeaders(w)
			if extra != nil {
				extra(w.Header())
			}
			writeJSONResp(w, status, echoBody)
		})
	}
	cases := []tc{
		{name: "echo 401", hook: fail(401, nil), want: func() []string { return []string{tr("tui.repos.online.err.auth")} }},
		{name: "echo 403", hook: fail(403, nil), want: func() []string { return []string{tr("tui.repos.online.err.scope")} }},
		{name: "echo 500", hook: fail(500, nil), want: func() []string { return []string{tr("tui.repos.online.err.server", 500)} }},
		{name: "echo 429 3600", hook: fail(429, func(h http.Header) { h.Set("Retry-After", "3600") }),
			want: func() []string {
				return []string{tr("tui.repos.online.err.rate_limit", reset.In(time.Local).Format("15:04"))}
			}},
		{name: "echo 429 overflow", hook: fail(429, func(h http.Header) { h.Set("Retry-After", "18446744074") }),
			want: func() []string {
				// capped at 366 days, not wrapped into a short wait
				return []string{tr("tui.repos.online.err.rate_limit", testutil.FixedTime.Add(366*24*time.Hour).In(time.Local).Format("15:04"))}
			}},
		{name: "echo sso own host", hook: fail(403, func(h http.Header) {
			h.Set("X-GitHub-SSO", "required; url=https://github.com/orgs/acme/sso?authorization_request=abc")
		}), want: func() []string {
			return []string{tr("tui.repos.online.err.sso"), tr("tui.repos.online.hint.sso", "https://github.com/orgs/acme/sso?authorization_request=abc")}
		}},
		{name: "sso link on another host", hook: fail(403, func(h http.Header) {
			h.Set("X-GitHub-SSO", "required; url=https://evil.example/orgs/acme/sso?authorization_request=abc")
		}), want: func() []string {
			return []string{tr("tui.repos.online.err.sso"), tr("tui.repos.online.hint.sso_nourl")}
		}, not: []string{"evil.example"}},
		{name: "sso link with the token", hook: fail(403, func(h http.Header) {
			h.Set("X-GitHub-SSO", "required; url=https://github.com/orgs/acme/sso?t="+fixtureToken)
		}), want: func() []string {
			return []string{tr("tui.repos.online.err.sso"), tr("tui.repos.online.hint.sso_nourl")}
		}},
		{name: "redirect to http", hook: aliceOnly(false, func(w http.ResponseWriter, r *http.Request, _ int) {
			http.Redirect(w, r, "http://api.github.com/user?t="+fixtureToken, http.StatusFound)
		}), want: func() []string {
			return []string{tr("tui.repos.online.err.bad_response")}
		}},
		{name: "link next on another host", hook: aliceOnly(true, func(w http.ResponseWriter, r *http.Request, _ int) {
			w.Header().Set("Link", `<https://evil.example/user/repos?page=2&t=`+fixtureToken+`>; rel="next"`)
			writeJSONResp(w, 200, []map[string]any{ghJSON("acme", "page-one", nil)})
		}), want: func() []string {
			return []string{tr("tui.repos.online.err.bad_response"), "acme/page-one"}
		}, never: []string{"evil.example"}},
		{name: "50 MB body", hook: aliceOnly(true, func(w http.ResponseWriter, r *http.Request, _ int) {
			w.Header().Set("Content-Type", "application/json")
			chunk := bytes.Repeat([]byte("a"), 64<<10)
			_, _ = w.Write([]byte(`[{"name":"`))
			for n := 0; n < 50<<20; n += len(chunk) {
				if _, err := w.Write(chunk); err != nil {
					return
				}
			}
		}), want: func() []string { return []string{tr("tui.repos.online.err.bad_response")} }},
		{name: "gzip bomb", hook: aliceOnly(true, func(w http.ResponseWriter, r *http.Request, _ int) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write(bomb)
		}), want: func() []string { return []string{tr("tui.repos.online.err.bad_response")} }},
		{name: "html login page with 200", hook: aliceOnly(false, func(w http.ResponseWriter, r *http.Request, _ int) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html><form>Sign in " + fixtureToken + "</form></html>"))
		}), want: func() []string { return []string{tr("tui.repos.online.err.bad_response")} }, not: []string{"Sign in"}},
		{name: "runaway chain of 1000 pages", hook: aliceOnly(true, func(w http.ResponseWriter, r *http.Request, page int) {
			if page < 1000 {
				w.Header().Set("Link", fmt.Sprintf(`<https://%s%s?page=%d>; rel="next"`, r.Host, r.URL.Path, page+1))
			}
			writeJSONResp(w, 200, []map[string]any{ghJSON("acme", fmt.Sprintf("r%d", page), nil)})
		}), want: func() []string {
			return []string{tr("tui.repos.online.truncated", online.DefaultMaxPages, fmt.Sprint(online.DefaultMaxPages))}
		}},
		{name: "login is the token", hook: aliceOnly(false, func(w http.ResponseWriter, r *http.Request, _ int) {
			writeJSONResp(w, 200, map[string]any{"login": fixtureToken})
		}), want: func() []string { return []string{tr("tui.repos.online.err.wrong_user", "***")} }},
		{name: "repository named like the token", hook: aliceOnly(true, func(w http.ResponseWriter, r *http.Request, _ int) {
			writeJSONResp(w, 200, []map[string]any{ghJSON("acme", fixtureToken, nil), ghJSON("acme", "fine", nil)})
		}), want: func() []string { return []string{"acme/***", "acme/fine"} }},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			api := newFakeAPI(t)
			api.setHook(c.hook)
			h := onlineHarnessRT(t, api, api.rt)
			h.resize(120, 40)
			openRepos(h)
			h.key("tab")
			v := panelOf(t, h, "acme/alice")
			assertViewClean(t, v)
			pv := plain(v)
			for _, w := range c.want() {
				if !strings.Contains(pv, plain(w)) {
					t.Errorf("the panel lacks %q:\n%s", w, v)
				}
			}
			for _, bad := range append([]string{"evil.example/orgs"}, c.not...) {
				if strings.Contains(v, bad) {
					t.Errorf("the panel shows %q:\n%s", bad, v)
				}
			}
			for _, host := range append([]string{"evil.example"}, c.never...) {
				if n := api.hitsFor(host); n != 0 {
					t.Errorf("%d requests to %s", n, host)
				}
			}
			for _, hit := range api.hits() {
				if strings.Contains(hit.Path+hit.Query, fixtureToken) {
					t.Errorf("the token is in a URL: %+v", hit)
				}
			}
		})
	}
}

// Names with a bidi override or an OSC 8 hyperlink are cleaned on the way in:
// the row and the detail show the plain text, the link target and the
// override never reach the frame, and the repository gets no clone URL.
func TestOnlineBidiAndOSC8Names(t *testing.T) {
	names := []struct{ name, row string }{
		{"re\u202etxt.exe", "acme/retxt.exe"},                                           // right-to-left override
		{"iso\u2066late\u2069d", "acme/isolated"},                                       // bidi isolates
		{"zero\u200bwidth\ufeff", "acme/zerowidth"},                                     // zero width characters
		{"\x1b]8;;https://evil.example/pwn\x07click-me\x1b]8;;\x07", "acme/click-me"},   // OSC 8, BEL terminated
		{"\x1b]8;;https://evil.example/pwn\x1b\\st-link\x1b]8;;\x1b\\", "acme/st-link"}, // OSC 8, ST terminated
		{"\x1b]8;id=1;https://evil.example/pwn\x07twice\x1b]8;;\x07", "acme/twice"},
		{"\x1b[31mred\x1b[0m", "acme/red"},
		{"c1\u009b2Jclean", "acme/c1clean"},
	}
	var raw []map[string]any
	for _, n := range names {
		raw = append(raw, ghJSON("acme", n.name, nil))
	}
	raw = append(raw, ghJSON("\u202eacme", "owner-rtl", nil), ghJSON("acme", "plain", nil))
	api := newFakeAPI(t)
	api.setPages("alice", [][]map[string]any{raw})
	h := onlineHarnessRT(t, api, api.rt)
	h.resize(140, 40)
	r := openRepos(h)
	h.key("tab")
	h.waitFor("settled", func() bool { return !r.onlineBusy() })
	gotoAccount(h, r, "acme/alice")
	for i := 0; i < len(raw); i++ {
		sel, ok := r.selectedOnline()
		if !ok {
			t.Fatalf("row %d: nothing selected", i)
		}
		v := h.view()
		assertViewClean(t, v)
		h.noSecret()
		for _, bad := range []string{"evil.example", "pwn", "\u202e", "\u2066", "\u200b", "\ufeff"} {
			if strings.Contains(v, bad) {
				t.Errorf("row %d (%q): the frame shows %q:\n%s", i, sel.FullPath, bad, v)
			}
		}
		if i < len(names) {
			if sel.FullPath != names[i].row {
				t.Errorf("row %d: %q, want %q", i, sel.FullPath, names[i].row)
			}
			if !strings.Contains(plain(v), names[i].row) {
				t.Errorf("row %d: the frame lacks %q:\n%s", i, names[i].row, v)
			}
		}
		if sel.FullPath != "acme/plain" && sel.CloneURL != "" {
			t.Errorf("row %d (%q): a cleaned name got a clone URL %q", i, sel.FullPath, sel.CloneURL)
		}
		// enter on a repository without a clone URL does nothing
		if sel.CloneURL == "" {
			before := h.snapshot()
			h.key("enter")
			if h.top() != Screen(r) || h.snapshot() != before {
				t.Errorf("row %d (%q): enter did something", i, sel.FullPath)
			}
		}
		h.key("down")
	}
	if n := h.fake.Count("git clone"); n != 0 {
		t.Errorf("%d clones started", n)
	}
}

// Repository names with ../, absolute paths, escape sequences and bidi
// overrides, driven through the whole clone flow up to the plan: no name
// leaves the company folder, none starts a clone, none reaches the frame
// raw.
func TestAdvHostileNamesThroughTheCloneFlow(t *testing.T) {
	type nm struct{ owner, name string }
	hostile := []nm{
		{"acme", "../../etc/passwd"},
		{"acme", ".."},
		{"..", "x"},
		{"acme", "/abs/path"},
		{"/etc", "passwd"},
		{"acme", `..\win`},
		{"acme", "a/b"},
		{"acme", "evil\x1b]0;pwn\x07name"},
		{"acme", "\x1b[2Jwipe"},
		{"acme", "re\u202etxt"},
		{"acme", "tab\tname"},
		{"acme", "nul\x00name"},
		// plain, valid-looking names that are dangerous as folder names
		{"acme", ".git"},
		{"acme", "."},
		{"acme", "..."},
		{"acme", "-rf"},
		{"acme", "--upload-pack=x"},
		{"acme", "-"},
		{"acme", "a b"},
		{"acme", "x%2e%2e"},
		{"acme", "con:"},
	}
	var raw []map[string]any
	for _, n := range hostile {
		raw = append(raw, ghJSON(n.owner, n.name, nil))
	}
	raw = append(raw, ghJSON("acme", "good", nil))
	api := newFakeAPI(t)
	api.setPages("alice", [][]map[string]any{raw})
	h := onlineHarnessRT(t, api, api.rt)
	h.resize(140, 40)
	r := openRepos(h)
	h.key("tab")
	h.waitFor("settled", func() bool { return !r.onlineBusy() })
	gotoAccount(h, r, "acme/alice")
	a, st, _ := r.curAccount()
	base := filepath.Join(h.e.Home, "Code/acme")
	cfg := h.m.sh.cfg
	if len(st.repos) != len(raw) {
		t.Fatalf("%d repositories, want %d", len(st.repos), len(raw))
	}

	// 1. the plan the service makes for each repository
	cloneable := 0
	for _, repo := range st.repos {
		oc, err := h.svc.OnlineCloneRequest(cfg, a.Ref.String(), repo, r.lidx)
		if err != nil {
			continue // refused: ErrUnclonable or a bad folder name
		}
		cloneable++
		t.Logf("planned: %q -> %s", repo.FullPath, oc.Plan.Dir)
		for _, dir := range []string{oc.Plan.Dir, oc.Request.Dir} {
			if dir == "" {
				continue
			}
			if filepath.Dir(dir) != base {
				t.Errorf("%q: the folder %q is not directly in %q", repo.FullPath, dir, base)
			}
			if b := filepath.Base(dir); b == "." || b == ".." || strings.HasPrefix(b, "-") || strings.ContainsAny(b, "/\\\x00") {
				t.Errorf("%q: the folder name %q", repo.FullPath, b)
			}
		}
		if strings.ContainsAny(oc.Plan.URL, "\x1b\x00\n\t \u202e") || strings.Contains(oc.Plan.URL, "..") {
			t.Errorf("%q: the plan URL %q", repo.FullPath, oc.Plan.URL)
		}
	}
	if cloneable != 1 { // only acme/good
		t.Errorf("%d repositories can be planned, want 1 (acme/good)", cloneable)
	}

	// 2. enter on each row: the clone flow opens only on a safe plan
	h.key("home")
	for i := range st.repos {
		sel, ok := r.selectedOnline()
		if !ok {
			t.Fatalf("row %d: nothing selected", i)
		}
		v := h.view()
		assertViewClean(t, v)
		h.noSecret()
		h.key("enter")
		if c, ok := h.top().(*cloneScreen); ok {
			if filepath.Dir(c.preview.Dir) != base || strings.HasPrefix(filepath.Base(c.preview.Dir), "-") {
				t.Errorf("%q: the clone screen would clone into %q", sel.FullPath, c.preview.Dir)
			}
			if strings.ContainsAny(c.preview.URL, "\x1b\x00\n\t \u202e") {
				t.Errorf("%q: the clone URL %q", sel.FullPath, c.preview.URL)
			}
			assertViewClean(t, h.view())
			h.key("esc")
		}
		if h.top() != Screen(r) {
			t.Fatalf("row %d (%q): top is %T", i, sel.FullPath, h.top())
		}
		h.key("down")
	}
	if n := h.fake.Count("git clone"); n != 0 {
		t.Errorf("%d clones started by browsing", n)
	}
	for _, c := range h.fake.Calls() {
		blob := strings.Join(append([]string{c.Name, c.Stdin}, c.Args...), " ")
		if strings.Contains(blob, "../") || strings.Contains(blob, "\x1b") {
			t.Errorf("a command holds a hostile name: %s", c.String())
		}
	}
}

// INFO-2: the folder check of a repository is read once while it is selected;
// moving the cursor away and back shows the same answer, and enter, which
// acts on it, reads the disk again.
func TestOnlineDetailIsKeptPerRepository(t *testing.T) {
	h, _ := onlineHarness(t)
	r := openOnlineTab(h, true)
	selectRepo(h, r, "acme-org/tools")
	first := r.odetail
	dir := filepath.Join(h.e.Home, "Code/acme/tools")
	if first.err != nil || first.oc.Plan.Dir != dir {
		t.Fatalf("detail %+v", first)
	}
	if len(r.odcache) == 0 {
		t.Fatal("nothing is cached")
	}
	mustMkdir(t, dir) // the folder appears after the check
	h.key("down")
	h.key("up")
	if got := r.odetail; got.oc.Plan.Dir != first.oc.Plan.Dir || len(got.oc.Warnings) != len(first.oc.Warnings) {
		t.Errorf("the check was read again: %+v -> %+v", first.oc.Plan.Dir, got.oc.Plan.Dir)
	}
	// enter reads the disk: the folder is taken, the fallback folder is offered
	h.key("enter")
	c, ok := h.top().(*cloneScreen)
	if !ok {
		t.Fatalf("top is %T", h.top())
	}
	if want := filepath.Join(h.e.Home, "Code/acme/acme-org-tools"); c.preview.Dir != want {
		t.Errorf("clone into %q, want %q", c.preview.Dir, want)
	}
	h.key("esc")
	// a refetch drops what was kept
	h.key("r")
	h.waitFor("settled", func() bool { return !r.onlineBusy() })
	selectRepo(h, r, "acme-org/tools")
	h.mustContainPlain(tr(app.OnlineWarnDirFallback, "~/Code/acme/acme-org-tools"))
}

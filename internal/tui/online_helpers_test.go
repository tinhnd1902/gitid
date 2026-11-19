package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/testutil"
)

// fixtureToken is the distinctive token the secrecy scans look for. Every
// token of the Online tests starts with it.
const fixtureToken = "tok_SECRET_9f3c"

// fakeAPI is the platform: one TLS server that answers for api.github.com
// and git.example.com alike (the transport sends every host to it).
type fakeAPI struct {
	t   *testing.T
	srv *httptest.Server
	rt  *testutil.RedirectRT

	mu     sync.Mutex
	logins map[string]string             // token -> login
	pages  map[string][][]map[string]any // login -> pages of /user/repos
	hook   func(w http.ResponseWriter, r *http.Request, login string, page int) bool
	seen   []apiHit
}

type apiHit struct{ Host, Path, Query, Auth string }

func ghJSON(owner, name string, extra map[string]any) map[string]any {
	m := map[string]any{
		"name": name, "full_name": owner + "/" + name, "owner": map[string]any{"login": owner},
		"private": false, "archived": false, "fork": false, "size": 10,
		"pushed_at": testutil.FixedTime.Add(-72 * time.Hour).Format(time.RFC3339),
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

// defaultRepos is what alice (and beta/alice, who is the same user) sees.
func aliceRepos() []map[string]any {
	return []map[string]any{
		ghJSON("acme", "api", map[string]any{"private": true}), // cloned at ~/Code/acme/api, fine
		ghJSON("acme", "x", nil),                               // cloned at ~/Code/acme/org/x with a wrong remote
		ghJSON("acme", "web", map[string]any{"archived": true}),
		ghJSON("acme-org", "tools", map[string]any{"empty": true, "size": 0}),
		ghJSON("alice", "dots", map[string]any{"fork": true}),
	}
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	a := &fakeAPI{t: t, logins: map[string]string{
		fixtureToken:           "alice",
		fixtureToken + "_bob":  "bob",
		fixtureToken + "_beta": "alice",
		fixtureToken + "_dave": "dave",
	}, pages: map[string][][]map[string]any{
		"alice": {aliceRepos()},
		"bob":   {{ghJSON("acme", "api", nil), ghJSON("bob", "notes", nil)}},
		"dave":  {{map[string]any{"path_with_namespace": "g/sub/p", "path": "p", "visibility": "private", "last_activity_at": testutil.FixedTime.Format(time.RFC3339)}}},
	}}
	a.srv = httptest.NewTLSServer(http.HandlerFunc(a.serve))
	t.Cleanup(a.srv.Close)
	a.rt = testutil.RedirectTransport(a.srv)
	return a
}

func (a *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.t.Errorf("method %s", r.Method)
	}
	auth := r.Header.Get("Authorization")
	if strings.Contains(r.URL.String(), fixtureToken) {
		a.t.Errorf("token in the URL %s", r.URL)
	}
	a.mu.Lock()
	a.seen = append(a.seen, apiHit{Host: r.Host, Path: r.URL.Path, Query: r.URL.RawQuery, Auth: auth})
	login, ok := a.logins[strings.TrimPrefix(auth, "Bearer ")]
	hook := a.hook
	a.mu.Unlock()
	if !ok {
		writeJSONResp(w, 401, map[string]any{"message": "Bad credentials"})
		return
	}
	isUser := strings.HasSuffix(r.URL.Path, "/user")
	page := 1
	fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
	if hook != nil && hook(w, r, login, page) {
		return
	}
	gitlab := r.Host == "git.example.com"
	switch {
	case isUser && gitlab:
		writeJSONResp(w, 200, map[string]any{"username": login})
	case isUser:
		writeJSONResp(w, 200, map[string]any{"login": login})
	default:
		a.mu.Lock()
		pages := a.pages[login]
		a.mu.Unlock()
		if page < 1 || page > len(pages) {
			writeJSONResp(w, 200, []any{})
			return
		}
		if page < len(pages) {
			q := r.URL.Query()
			q.Set("page", fmt.Sprint(page+1))
			w.Header().Set("Link", fmt.Sprintf(`<https://%s%s?%s>; rel="next"`, r.Host, r.URL.Path, q.Encode()))
		}
		writeJSONResp(w, 200, pages[page-1])
	}
}

func writeJSONResp(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	b, _ := jsonMarshal(v)
	_, _ = w.Write(b)
}

func (a *fakeAPI) setPages(login string, pages [][]map[string]any) {
	a.mu.Lock()
	a.pages[login] = pages
	a.mu.Unlock()
}

func (a *fakeAPI) setHook(h func(w http.ResponseWriter, r *http.Request, login string, page int) bool) {
	a.mu.Lock()
	a.hook = h
	a.mu.Unlock()
}

func (a *fakeAPI) hits() []apiHit {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]apiHit(nil), a.seen...)
}

// hitsFor counts the requests to host.
func (a *fakeAPI) hitsFor(host string) int {
	n := 0
	for _, h := range a.hits() {
		if h.Host == host {
			n++
		}
	}
	return n
}

// onlineHarness is the basic configuration with the token sources of the
// plan's T-81: acme/alice has an environment variable, acme/bob a gh login,
// beta/alice a stored git credential, acme/carol and globex/dave nothing.
// The fixture repositories of the Local tab are on disk.
func onlineHarness(t *testing.T, keysYAML ...string) (*harness, *fakeAPI) {
	t.Helper()
	api := newFakeAPI(t)
	return onlineHarnessRT(t, api, api.rt, keysYAML...), api
}

// onlineHarnessRT is onlineHarness with another HTTP transport (one that
// fails the way a network does).
func onlineHarnessRT(t *testing.T, api *fakeAPI, rt http.RoundTripper, keysYAML ...string) *harness {
	t.Helper()
	var h *harness
	mutate := func(e *env.Env) {
		*e = testutil.MapEnv(*e, map[string]string{"GITID_TOKEN_ACME__ALICE": fixtureToken})
		e.HTTP = rt
	}
	var before func(env.Env)
	if len(keysYAML) > 0 && keysYAML[0] != "" {
		before = func(e env.Env) { testutil.WriteFile(t, e.Paths().KeyBindingsFile, keysYAML[0], 0o644) }
	}
	h = buildHarnessEnv(t, basicCfg(), mutate, before)
	addRepos(t, h, localRepos())
	t.Cleanup(h.m.shutdown)
	h.fake.On("gh auth token --hostname=github.com --user=bob", execx.Response{Stdout: fixtureToken + "_bob\n"})
	// only the credential for alice answers (the stdin names the user)
	h.fake.On("git -c credential.interactive=never -c credential.interactive=false credential fill", execx.Response{
		Stdout: "protocol=https\nhost=github.com\nusername=alice\npassword=" + fixtureToken + "_beta\n",
		Do: func(c execx.Cmd) error {
			if !strings.Contains(c.Stdin, "username=alice\n") {
				return fmt.Errorf("no credential")
			}
			return nil
		}})
	h.fake.On("glab config get token --host=git.example.com", execx.Response{ExitCode: 1})
	// the beta/alice account must reach the git credential: the earlier
	// sources (security, gh) have no answer for it
	h.fake.On("gh auth token --hostname=github.com --user=alice", execx.Response{ExitCode: 1})
	t.Cleanup(func() { noSecretLeft(t, h) })
	return h
}

// noSecretLeft is SEC-3 and SEC-7: the token is in no call the runner saw and
// in no file of the temp HOME.
func noSecretLeft(t *testing.T, h *harness) {
	t.Helper()
	for _, c := range h.fake.Calls() {
		blob := strings.Join(append(append([]string{c.Name, c.Stdin}, c.Args...), c.Env...), "\x00")
		if strings.Contains(blob, fixtureToken) {
			t.Errorf("the token is in the call %s", c.String())
		}
	}
	_ = filepath.Walk(h.e.Home, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			if b, _ := os.ReadFile(p); strings.Contains(string(b), fixtureToken) {
				t.Errorf("the token is in the file %s", p)
			}
		}
		return nil
	})
}

// noSecret is SEC-1 and SEC-2: neither the frame nor the status bar shows a
// token, and no escape sequence of API data reaches the frame.
func (h *harness) noSecret() {
	h.t.Helper()
	v := h.view()
	if strings.Contains(v, fixtureToken) {
		h.t.Fatalf("the view shows the token:\n%s", v)
	}
	if strings.Contains(h.m.status.text, fixtureToken) {
		h.t.Fatalf("the status shows the token: %q", h.m.status.text)
	}
	if strings.Contains(v, "\x1b") {
		h.t.Fatalf("the view holds an escape character:\n%q", v)
	}
}

// openOnlineTab opens the browser and shows the Online tab; wait says
// whether to wait until no account is busy.
func openOnlineTab(h *harness, wait bool) *reposScreen {
	h.t.Helper()
	r := openRepos(h)
	h.key("tab") // URL -> Online
	if r.tab != tabOnline {
		h.t.Fatalf("tab %v", r.tab)
	}
	if wait {
		h.waitFor("accounts settled", func() bool { return !r.onlineBusy() })
	}
	h.noSecret()
	return r
}

// gotoAccount moves the Online tab to the account with this ref.
func gotoAccount(h *harness, r *reposScreen, ref string) {
	h.t.Helper()
	for i := 0; i < len(r.oacc)+1; i++ {
		if r.oacc[r.osel].Ref.String() == ref {
			return
		}
		h.key("right")
	}
	h.t.Fatalf("no account %s", ref)
}

// cfgWith returns the harness configuration after mod.
func cfgWith(h *harness, mod func(*config.Config)) config.Config {
	cfg := h.m.sh.cfg
	cfg.Companies = append([]config.Company(nil), cfg.Companies...)
	for i := range cfg.Companies {
		cfg.Companies[i].Accounts = append([]config.Account(nil), cfg.Companies[i].Accounts...)
	}
	mod(&cfg)
	return cfg
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

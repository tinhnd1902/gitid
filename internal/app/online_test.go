package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/gitops"
	"github.com/tinhnd1902/gitid/internal/online"
	"github.com/tinhnd1902/gitid/internal/platform"
	"github.com/tinhnd1902/gitid/internal/testutil"
	"github.com/tinhnd1902/gitid/internal/testutil/fixtures"
)

const tokSecret = "tok_SECRET_9f3c"

// onlineSvc is a Service whose HTTP goes to h and whose environment holds
// vars. The server answers under any host.
func onlineSvc(t *testing.T, vars map[string]string, fake *execx.Fake, h http.HandlerFunc) (*app.Service, env.Env, *testutil.RedirectRT) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	e := testutil.MapEnv(testutil.TempEnv(t), vars)
	rt := testutil.RedirectTransport(srv)
	e.HTTP = rt
	if fake == nil {
		fake = execx.NewFake()
	}
	return app.New(e, fake), e, rt
}

func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ghHandler: /user answers with login(token), /user/repos with the repos.
func ghHandler(login func(token string) string, repos []map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if strings.HasSuffix(r.URL.Path, "/user") {
			jsonOut(w, 200, map[string]any{"login": login(tok)})
			return
		}
		jsonOut(w, 200, repos)
	}
}

func ghRepo(owner, name string) map[string]any {
	return map[string]any{"name": name, "full_name": owner + "/" + name, "owner": map[string]any{"login": owner},
		"size": 5, "pushed_at": "2026-11-10T10:00:00Z"}
}

func always(l string) func(string) string { return func(string) string { return l } }

// noTokenAnywhere is SEC-3 / SEC-7: the token is in no call of the runner
// and in no file of the temp HOME.
func noTokenAnywhere(t *testing.T, fake *execx.Fake, e env.Env) {
	t.Helper()
	for _, c := range fake.Calls() {
		blob := strings.Join(append(append([]string{c.Name, c.Stdin}, c.Args...), c.Env...), "\x00")
		if strings.Contains(blob, tokSecret) {
			t.Errorf("token in the call %s", c.String())
		}
	}
	_ = filepath.Walk(e.Home, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			if b, _ := os.ReadFile(p); strings.Contains(string(b), tokSecret) {
				t.Errorf("token in the file %s", p)
			}
		}
		return nil
	})
}

// AO-1: the accounts in config order, with the variable name and support.
func TestAO1OnlineAccounts(t *testing.T) {
	s, _, _ := onlineSvc(t, nil, nil, nil)
	accs := s.OnlineAccounts(fixtures.Full())
	var refs []string
	for _, a := range accs {
		refs = append(refs, a.Ref.String())
	}
	want := "acme/alice acme/bob acme/carol globex/dave beta/alice az/dev az/devssh bb/joe gt/sam"
	if strings.Join(refs, " ") != want {
		t.Fatalf("order %v", refs)
	}
	byRef := map[string]app.OnlineAccount{}
	for _, a := range accs {
		byRef[a.Ref.String()] = a
	}
	if a := byRef["acme/alice"]; !a.Supported || a.EnvName != "GITID_TOKEN_ACME__ALICE" || a.Account.Host != "github.com" {
		t.Errorf("alice %+v", a)
	}
	if a := byRef["globex/dave"]; !a.Supported || a.Account.Host != "git.example.com" || a.Account.Platform != platform.GitLab {
		t.Errorf("dave %+v", a)
	}
	for _, r := range []string{"az/dev", "bb/joe", "gt/sam"} {
		if a := byRef[r]; a.Supported || a.Reason != app.OnlineUnsupportedPlatform {
			t.Errorf("%s: %+v", r, a)
		}
	}
	if byRef["acme/alice"].Key == byRef["beta/alice"].Key {
		t.Error("two accounts share a key")
	}
}

// AO-2: the happy path streams pages and reports the login and source.
func TestAO2ListOnline(t *testing.T) {
	cfg := fixtures.Basic()
	s, e, rt := onlineSvc(t, map[string]string{"GITID_TOKEN_ACME__ALICE": tokSecret}, nil,
		ghHandler(always("alice"), []map[string]any{ghRepo("acme-org", "api"), ghRepo("alice", "dots")}))
	var pages []online.Page
	res, err := s.ListOnline(bg, cfg, "acme/alice", func(p online.Page) { pages = append(pages, p) })
	if err != nil {
		t.Fatal(err)
	}
	if res.Login != "alice" || res.Source != online.SourceEnv || res.Count != 2 || len(pages) != 1 {
		t.Fatalf("%+v", res)
	}
	if res.LooksLikePassword {
		t.Error("an env token is not a password")
	}
	for _, h := range rt.Hosts() {
		if h != "api.github.com" {
			t.Errorf("request to %s", h)
		}
	}
	noTokenAnywhere(t, execx.NewFake(), e)
	// the result and the info hold no token
	if s := fmt.Sprintf("%+v %+v", res, pages); strings.Contains(s, tokSecret) {
		t.Errorf("token in %s", s)
	}
}

// AO-12: no token anywhere: no request (TempEnv's transport would fail the
// test), the error says so, and the runner was asked for the sources only.
func TestAO12NoTokenNoRequest(t *testing.T) {
	e := testutil.TempEnv(t)
	fake := execx.NewFake()
	s := app.New(e, fake)
	_, err := s.ListOnline(bg, fixtures.Basic(), "acme/alice", nil)
	if !errors.Is(err, app.ErrNoToken) {
		t.Fatalf("%v", err)
	}
	info, err := s.ResolveSource(bg, fixtures.Basic(), "globex/dave")
	if err != nil || info.Source != online.SourceNone {
		t.Fatalf("%+v %v", info, err)
	}
	if _, err := s.ListOnline(bg, fixtures.Basic(), "nobody/x", nil); err == nil {
		t.Error("an unknown account was listed")
	}
	// an unsupported platform is refused before anything is asked
	fake2 := execx.NewFake()
	s2 := app.New(e, fake2)
	_, err = s2.ListOnline(bg, fixtures.Full(), "bb/joe", nil)
	var oe *online.Error
	if !errors.As(err, &oe) || oe.Kind != online.KindUnsupported || len(fake2.Calls()) != 0 {
		t.Errorf("%v %v", err, fake2.CallStrings())
	}
}

// ResolveSource caches; ForgetToken asks again; KeepTokens prunes.
func TestResolveSourceCache(t *testing.T) {
	fake := execx.NewFake().On("gh auth token", execx.Response{Stdout: tokSecret})
	s, e, _ := onlineSvc(t, nil, fake, nil)
	cfg := fixtures.Basic()
	for i := 0; i < 3; i++ {
		info, err := s.ResolveSource(bg, cfg, "acme/alice")
		if err != nil || info.Source != online.SourceGH {
			t.Fatalf("%+v %v", info, err)
		}
	}
	if n := fake.Count("gh auth token"); n != 1 {
		t.Fatalf("%d gh calls", n)
	}
	key := s.OnlineAccounts(cfg)[0].Key
	s.ForgetToken(key)
	_, _ = s.ResolveSource(bg, cfg, "acme/alice")
	if n := fake.Count("gh auth token"); n != 2 {
		t.Fatalf("after Forget: %d gh calls", n)
	}
	// AO-11: a changed username is another key; KeepTokens drops the old one
	cfg2 := fixtures.Basic()
	cfg2.Companies[0].Accounts[0].Username = "alice2"
	if s.OnlineAccounts(cfg2)[0].Key == key {
		t.Fatal("the key did not change with the username")
	}
	s.KeepTokens(cfg2)
	_, _ = s.ResolveSource(bg, cfg2, "acme/alice")
	if n := fake.Count("gh auth token"); n != 3 {
		t.Fatalf("after KeepTokens: %d gh calls", n)
	}
	noTokenAnywhere(t, fake, e)
}

// AO-6: gh without --user hands over another account's token: WrongUser.
func TestAO6WrongUserFromGH(t *testing.T) {
	fake := execx.NewFake()
	fake.On("gh auth token --hostname=github.com --user=alice", execx.Response{ExitCode: 1, Stderr: "unknown flag: --user"})
	fake.On("gh auth token --hostname=github.com", execx.Response{Stdout: tokSecret})
	var reposAsked atomic.Bool
	s, e, _ := onlineSvc(t, nil, fake, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/user") {
			jsonOut(w, 200, map[string]any{"login": "bob"})
			return
		}
		reposAsked.Store(true)
		jsonOut(w, 200, []any{})
	})
	_, err := s.ListOnline(bg, fixtures.Basic(), "acme/alice", nil)
	var oe *online.Error
	if !errors.As(err, &oe) || oe.Kind != online.KindWrongUser || oe.Login != "bob" || reposAsked.Load() {
		t.Fatalf("%v", err)
	}
	noTokenAnywhere(t, fake, e)
}

// a password-looking credential from git is flagged without being shown
func TestListOnlinePasswordFromGit(t *testing.T) {
	fake := execx.NewFake().On("git -c credential.interactive=never -c credential.interactive=false credential fill", execx.Response{Stdout: "password=hunter2\n"})
	s, _, _ := onlineSvc(t, nil, fake, func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 401, map[string]any{"message": "Bad credentials"})
	})
	res, err := s.ListOnline(bg, fixtures.Basic(), "acme/alice", nil)
	var oe *online.Error
	if !errors.As(err, &oe) || oe.Kind != online.KindAuth || res.Source != online.SourceGit || !res.LooksLikePassword {
		t.Fatalf("%+v %v", res, err)
	}
	if strings.Contains(fmt.Sprintf("%+v %v", res, err), "hunter2") {
		t.Error("the credential is shown")
	}
}

func repoOf(owner, name, clone string) online.Repo {
	return online.Repo{Owner: owner, Name: name, FullPath: owner + "/" + name, CloneURL: clone}
}

// AO-3: the URL is built from the config, rendered for the account, the
// account is the one of the row, the folder is <base>/<repo>.
func TestAO3OnlineCloneRequest(t *testing.T) {
	s, e, _ := onlineSvc(t, nil, nil, nil)
	cfg := fixtures.Basic()
	oc, err := s.OnlineCloneRequest(cfg, "acme/alice", repoOf("acme-org", "api", "https://github.com/acme-org/api.git"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if oc.Request.URL != "https://github.com/acme-org/api.git" || oc.Request.Account != "acme/alice" || oc.Request.Dir != "" {
		t.Errorf("request %+v", oc.Request)
	}
	if oc.Plan.URL != "https://alice@github.com/acme-org/api.git" || oc.Plan.Dir != filepath.Join(e.Home, "Code/acme/api") {
		t.Errorf("plan %+v", oc.Plan)
	}
	if len(oc.Warnings) != 0 {
		t.Errorf("warnings %+v", oc.Warnings)
	}
	if oc.Key != "github.com/acme-org/api" {
		t.Errorf("key %q", oc.Key)
	}
	if k := s.OnlineRepoKey(cfg, repoOf("Acme-Org", "API", "https://github.com/Acme-Org/API.git")); k != "github.com/acme-org/api" {
		t.Errorf("key %q", k)
	}
	if k := s.OnlineRepoKey(cfg, repoOf("a", "b", "")); k != "" {
		t.Errorf("key %q", k)
	}
	// an ssh account renders the alias form
	oc, err = s.OnlineCloneRequest(cfg, "acme/carol", repoOf("acme", "web", "https://github.com/acme/web.git"), nil)
	if err != nil || oc.Plan.URL != "git@github-acme-carol:acme/web.git" || len(oc.Warnings) != 0 {
		t.Errorf("ssh %+v %v", oc.Plan, err)
	}
}

// AO-4: the folder is taken by another repository: <owner>-<repo>; both
// taken: the error.
func TestAO4TargetExists(t *testing.T) {
	s, e, _ := onlineSvc(t, nil, nil, nil)
	cfg := fixtures.Basic()
	r := repoOf("acme-org", "api", "https://github.com/acme-org/api.git")
	if err := os.MkdirAll(filepath.Join(e.Home, "Code/acme/api"), 0o755); err != nil {
		t.Fatal(err)
	}
	oc, err := s.OnlineCloneRequest(cfg, "acme/alice", r, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(e.Home, "Code/acme/acme-org-api")
	if oc.Plan.Dir != want || oc.Request.Dir != want {
		t.Errorf("dir %q / %q, want %q", oc.Plan.Dir, oc.Request.Dir, want)
	}
	if len(oc.Warnings) != 1 || oc.Warnings[0].Key != app.OnlineWarnDirFallback || oc.Warnings[0].Arg != "~/Code/acme/acme-org-api" {
		t.Errorf("warnings %+v", oc.Warnings)
	}
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OnlineCloneRequest(cfg, "acme/alice", r, nil); !errors.Is(err, gitops.ErrTargetExists) {
		t.Errorf("both taken: %v", err)
	}
}

// AO-5: already cloned: the local repos, no request.
func TestAO5AlreadyCloned(t *testing.T) {
	s, e, _ := onlineSvc(t, nil, nil, nil)
	cfg := fixtures.Basic()
	local := app.LocalRepo{Path: filepath.Join(e.Home, "Code/beta/x"), Key: "github.com/acme-org/api", Company: "beta"}
	idx := app.LocalIndex{"github.com/acme-org/api": {local}}
	oc, err := s.OnlineCloneRequest(cfg, "acme/alice", repoOf("acme-org", "api", "https://github.com/acme-org/api.git"), idx)
	if err != nil || len(oc.AlreadyCloned) != 1 || oc.AlreadyCloned[0].Path != local.Path || oc.Request.URL != "" {
		t.Fatalf("%+v %v", oc, err)
	}
}

// AO-7: git would not use the account for this clone.
func TestAO7PredictedIdentity(t *testing.T) {
	s, _, _ := onlineSvc(t, nil, nil, nil)
	cfg := config.Default()
	co := fixtures.Acme()
	co.Accounts = co.Accounts[:1]
	co.Accounts[0].Orgs = []string{"acme"}
	cfg.Companies = []config.Company{co}
	oc, err := s.OnlineCloneRequest(cfg, "acme/alice", repoOf("other-org", "api", "https://github.com/other-org/api.git"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(oc.Warnings) != 1 || oc.Warnings[0].Key != app.OnlineWarnNoAccount {
		t.Errorf("no match: %+v", oc.Warnings)
	}
	// beta/alice (same username) is narrowed to the org special, so git
	// would use it for this URL instead of the account of the row
	cfg = fixtures.Basic()
	cfg.Companies[2].Accounts[0].Orgs = []string{"special"}
	cfg.Companies[0].Accounts[0].Orgs = nil
	oc, err = s.OnlineCloneRequest(cfg, "acme/alice", repoOf("special", "api", "https://github.com/special/api.git"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(oc.Warnings) != 1 || oc.Warnings[0].Key != app.OnlineWarnOtherAcc || oc.Warnings[0].Arg != "beta/alice" {
		t.Errorf("other account: %+v", oc.Warnings)
	}
}

// AO-8: a name that is not a folder name.
func TestAO8BadName(t *testing.T) {
	s, _, _ := onlineSvc(t, nil, nil, nil)
	cfg := fixtures.Basic()
	_, err := s.OnlineCloneRequest(cfg, "acme/alice", repoOf("acme", "-rf", "https://github.com/acme/-rf.git"), nil)
	if !errors.Is(err, gitops.ErrBadTarget) {
		t.Errorf("-rf: %v", err)
	}
	if _, err := s.OnlineCloneRequest(cfg, "acme/alice", repoOf("acme", "x", ""), nil); !errors.Is(err, app.ErrUnclonable) {
		t.Errorf("no URL: %v", err)
	}
	if _, err := s.OnlineCloneRequest(cfg, "acme/alice", repoOf("acme", "x", "weird::url"), nil); !errors.Is(err, app.ErrUnclonable) {
		t.Errorf("odd URL: %v", err)
	}
	if _, err := s.OnlineCloneRequest(cfg, "nobody/x", repoOf("acme", "x", "https://github.com/acme/x.git"), nil); err == nil {
		t.Error("unknown account")
	}
}

// AO-9: a GitLab repository in a nested group.
func TestAO9GitLabNested(t *testing.T) {
	s, e, _ := onlineSvc(t, nil, nil, nil)
	cfg := fixtures.Basic()
	r := repoOf("g/sub", "p", "https://git.example.com/g/sub/p.git")
	oc, err := s.OnlineCloneRequest(cfg, "globex/dave", r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if oc.Plan.Dir != filepath.Join(e.Home, "Code/globex/p") || oc.Plan.URL != "git@gitlab-globex-dave:g/sub/p.git" {
		t.Errorf("plan %+v", oc.Plan)
	}
	if err := os.MkdirAll(oc.Plan.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	oc, err = s.OnlineCloneRequest(cfg, "globex/dave", r, nil)
	if err != nil || oc.Plan.Dir != filepath.Join(e.Home, "Code/globex/g-sub-p") {
		t.Errorf("fallback %+v %v", oc.Plan, err)
	}
}

// AO-10: at most three accounts are fetched at the same time.
func TestAO10FetchBound(t *testing.T) {
	cfg := config.Default()
	vars := map[string]string{}
	co := config.Company{Slug: "many", Name: "Many", BaseDir: "~/Code/many/", DefaultAccount: "u0"}
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("u%d", i)
		co.Accounts = append(co.Accounts, config.Account{ID: id, Platform: platform.GitHub, Username: id, Name: id, Email: id + "@x.io", Auth: config.AuthHTTPS})
		vars["GITID_TOKEN_MANY__"+strings.ToUpper(id)] = tokSecret + "_" + id
	}
	cfg.Companies = []config.Company{co}
	var inFlight, peak int32
	h := func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/user") {
			tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "+tokSecret+"_")
			jsonOut(w, 200, map[string]any{"login": tok})
			return
		}
		n := atomic.AddInt32(&inFlight, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		time.Sleep(60 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		jsonOut(w, 200, []any{})
	}
	s, _, _ := onlineSvc(t, vars, nil, h)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := s.ListOnline(bg, cfg, fmt.Sprintf("many/u%d", i), nil); err != nil {
				t.Errorf("u%d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if p := atomic.LoadInt32(&peak); p > app.MaxFetchAtOnce || p < 2 {
		t.Errorf("peak %d concurrent listings, want 2..%d", p, app.MaxFetchAtOnce)
	}
}

// the resolves are bounded too, and a cancel frees a waiting caller
func TestResolveBound(t *testing.T) {
	block := make(chan struct{})
	fake := execx.NewFake()
	fake.On("security", execx.Response{Block: block})
	s, _, _ := onlineSvc(t, nil, fake, nil)
	cfg := config.Default()
	co := config.Company{Slug: "many", Name: "Many", BaseDir: "~/Code/many/", DefaultAccount: "u0"}
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("u%d", i)
		co.Accounts = append(co.Accounts, config.Account{ID: id, Platform: platform.GitHub, Username: id, Name: id, Email: id + "@x.io", Auth: config.AuthHTTPS})
	}
	cfg.Companies = []config.Company{co}
	ctx, cancel := testCtx()
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.ResolveSource(ctx, cfg, fmt.Sprintf("many/u%d", i))
			if err == nil {
				t.Errorf("u%d: resolved although everything is blocked", i)
			}
		}(i)
	}
	deadline := time.Now().Add(3 * time.Second)
	for fake.Count("security") < app.MaxResolveAtOnce && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if n := fake.Count("security"); n != app.MaxResolveAtOnce {
		t.Errorf("%d lookups in flight, want %d", n, app.MaxResolveAtOnce)
	}
	cancel()
	wg.Wait()
}

// AO-13: no struct of a clone request holds the token, and the clone command
// carries none.
func TestAO13NoTokenInCloneRequest(t *testing.T) {
	fake := execx.NewFake().
		On("git clone", execx.Response{}).
		On("git -C", execx.Response{ExitCode: 1})
	s, e, _ := onlineSvc(t, map[string]string{"GITID_TOKEN_ACME__ALICE": tokSecret}, fake,
		ghHandler(always("alice"), []map[string]any{ghRepo("acme-org", "api")}))
	cfg := fixtures.Basic()
	if _, err := s.ListOnline(bg, cfg, "acme/alice", nil); err != nil {
		t.Fatal(err)
	}
	oc, err := s.OnlineCloneRequest(cfg, "acme/alice", repoOf("acme-org", "api", "https://github.com/acme-org/api.git"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if dump := fmt.Sprintf("%+v %#v", oc, oc); strings.Contains(dump, tokSecret) {
		t.Errorf("token in %s", dump)
	}
	if _, err := s.Clone(bg, oc.Plan, nil); err != nil {
		t.Fatal(err)
	}
	if fake.Count("git clone") != 1 {
		t.Fatalf("calls %v", fake.CallStrings())
	}
	noTokenAnywhere(t, fake, e)
}

func testCtx() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }

// OnlineCloneURL renders for the account; OpenURL opens https only.
func TestOnlineCloneURLAndOpenURL(t *testing.T) {
	fake := execx.NewFake().On("open https://github.com/acme/api", execx.Response{})
	s, _, _ := onlineSvc(t, nil, fake, nil)
	cfg := fixtures.Basic()
	r := repoOf("acme", "api", "https://github.com/acme/api.git")
	for ref, want := range map[string]string{
		"acme/alice": "https://alice@github.com/acme/api.git",
		"acme/carol": "git@github-acme-carol:acme/api.git",
	} {
		got, err := s.OnlineCloneURL(cfg, ref, r)
		if err != nil || got != want {
			t.Errorf("%s: %q %v", ref, got, err)
		}
	}
	if _, err := s.OnlineCloneURL(cfg, "acme/alice", repoOf("a", "b", "")); !errors.Is(err, app.ErrUnclonable) {
		t.Errorf("no URL: %v", err)
	}
	if _, err := s.OnlineCloneURL(cfg, "nobody/x", r); err == nil {
		t.Error("unknown account")
	}
	if err := s.OpenURL(bg, "https://github.com/acme/api"); err != nil {
		t.Errorf("open: %v", err)
	}
	if err := s.OpenURL(bg, "http://github.com/acme/api"); err == nil {
		t.Error("open took a plain http URL")
	}
	if n := fake.Count("open"); n != 1 {
		t.Errorf("%d open calls", n)
	}
}

// SEC-12: online.go makes no file system write.
func TestSEC12AppOnlineWritesNothing(t *testing.T) {
	b, err := os.ReadFile("online.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"os.WriteFile", "os.Create", "os.OpenFile", "os.Mkdir", "fsx."} {
		if strings.Contains(string(b), bad) {
			t.Errorf("online.go uses %s", bad)
		}
	}
}

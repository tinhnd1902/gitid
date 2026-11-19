package cli_test

// Tests of `gitid repos` (CL-*, SEC-8) and `gitid token` (KC-*). The local
// scan runs against folders in the temp HOME and a scripted git; the Online
// listing against an httptest server. There is no network, no real HOME and
// no real Keychain.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/cli"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/testutil"
	"github.com/tinhnd1902/gitid/internal/testutil/fixtures"
)

const secretTok = "tok_SECRET_9f3c"

// seedCfg writes cfg as the configuration (with a throw-away runner, so the
// harness's own fake sees no call).
func (h *harness) seedCfg(cfg config.Config) {
	h.t.Helper()
	if _, _, err := app.New(h.e, execx.NewFake()).Commit(cfg); err != nil {
		h.t.Fatal(err)
	}
}

func nulJoin(parts ...string) string { return strings.Join(parts, "\x00") + "\x00" }

// mkRepo creates ~/Code/<rel>/.git and scripts the scan's git call.
func (h *harness) mkRepo(rel, url, email string) string {
	h.t.Helper()
	dir := filepath.Join(h.e.Home, "Code", rel)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		h.t.Fatal(err)
	}
	var p []string
	if url != "" {
		p = append(p, "file:.git/config", "remote.origin.url\n"+url)
	}
	if email != "" {
		p = append(p, "file:/home/x/.gitconfig", "user.email\n"+email)
	}
	out := ""
	if len(p) > 0 {
		out = nulJoin(p...)
	}
	h.f.On("git -C "+dir+" config -z --show-origin --get-regexp", execx.Response{Stdout: out})
	return dir
}

func (h *harness) setVars(vars map[string]string) {
	h.e = testutil.MapEnv(h.e, vars)
}

// runDeps is run with a change to the Deps.
func (h *harness) runDeps(mod func(*cli.Deps), args ...string) (int, string, string) {
	h.t.Helper()
	var out, errb bytes.Buffer
	d := cli.Deps{
		Env: h.e, Runner: h.f, Stdin: strings.NewReader(h.in), Stdout: &out, Stderr: &errb,
		IsTTY: func() bool { return h.tty }, Cwd: func() (string, error) { return h.e.Home, nil },
		Version: "dev", Lang: "vi",
	}
	if mod != nil {
		mod(&d)
	}
	code := cli.Execute(d, args)
	return code, out.String(), errb.String()
}

// fits fails when a line of s is wider than 80 columns.
func fits(t *testing.T, s string) {
	t.Helper()
	for _, l := range strings.Split(s, "\n") {
		if w := ansi.StringWidth(l); w > 80 {
			t.Errorf("line of %d columns: %q", w, l)
		}
	}
}

func noSecret(t *testing.T, label string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if strings.Contains(p, secretTok) || strings.Contains(p, "SECRET_9f3c") {
			t.Errorf("%s: the token is in the output:\n%s", label, p)
		}
	}
}

// ---- local ----

const okRemote = "https://alice@github.com/acme/%s.git"

func (h *harness) localFixture() {
	h.seedCfg(fixtures.Basic())
	h.mkRepo("acme/api", fmt.Sprintf(okRemote, "api"), "alice@acme.com")
	h.mkRepo("acme/web", fmt.Sprintf(okRemote, "web"), "alice@acme.com")
}

func TestCL1Text(t *testing.T) {
	h := newHarness(t, nil)
	h.localFixture()
	code, out, errOut := h.run("repos")
	if code != 0 || errOut != "" {
		t.Fatalf("%d %q\n%s", code, errOut, out)
	}
	if !strings.Contains(out, "acme  ~/Code/acme/  2 repo") {
		t.Errorf("header missing:\n%s", out)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "! ") {
			t.Errorf("a problem line in a clean tree: %q", l)
		}
	}
	for _, want := range []string{"api", "web", "acme/alice"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	// the other companies are listed, empty
	if !strings.Contains(out, "globex") || !strings.Contains(out, "beta") {
		t.Errorf("companies missing:\n%s", out)
	}
	fits(t, out)
}

func TestCL2And3Problems(t *testing.T) {
	h := newHarness(t, nil)
	h.localFixture()
	h.mkRepo("acme/bad", "https://github.com/acme/bad.git", "alice@acme.com") // no username
	code, out, _ := h.run("repos")
	if code != 2 {
		t.Fatalf("code %d\n%s", code, out)
	}
	var problem, ok int
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, "! "):
			problem++
			if !strings.Contains(l, "bad") {
				t.Errorf("wrong problem line %q", l)
			}
		case strings.HasPrefix(l, "  ") && strings.TrimSpace(l) != "":
			ok++
		}
	}
	if problem != 1 || ok < 2 {
		t.Errorf("problem=%d ok=%d\n%s", problem, ok, out)
	}
	if !strings.Contains(out, "1 vấn đề") {
		t.Errorf("the problem count is missing:\n%s", out)
	}
	fits(t, out)
	// --problems: only the problem rows
	code, out, _ = h.run("repos", "--problems")
	if code != 2 || strings.Contains(out, "api ") || strings.Contains(out, "web ") || !strings.Contains(out, "! bad") {
		t.Errorf("%d\n%s", code, out)
	}
}

func TestCL4Company(t *testing.T) {
	h := newHarness(t, nil)
	h.localFixture()
	h.mkRepo("beta/b1", "https://alice@github.com/beta/b1.git", "alice@beta.io")
	code, out, _ := h.run("repos", "--company", "beta")
	if code != 0 || !strings.Contains(out, "b1") || strings.Contains(out, "api") || strings.Contains(out, "acme") {
		t.Errorf("%d\n%s", code, out)
	}
	code, out, errOut := h.run("repos", "--company", "nope")
	if code != 1 || out != "" || !strings.Contains(errOut, i18n.In("vi", "cli.repos.company_none", "nope")) {
		t.Errorf("%d %q %q", code, out, errOut)
	}
	// JSON keeps to one company too
	_, out, _ = h.run("repos", "--company", "beta", "--json")
	var v struct {
		Local struct {
			Companies []struct{ Slug string } `json:"companies"`
		} `json:"local"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil || len(v.Local.Companies) != 1 || v.Local.Companies[0].Slug != "beta" {
		t.Errorf("%v\n%s", err, out)
	}
}

func TestCL5Depth(t *testing.T) {
	h := newHarness(t, nil)
	h.localFixture()
	h.mkRepo("acme/group/deep", "https://alice@github.com/acme/deep.git", "alice@acme.com")
	_, out, _ := h.run("repos", "--depth", "2")
	if !strings.Contains(out, "deep") {
		t.Errorf("depth 2 should list the repo at depth 2:\n%s", out)
	}
	_, out, _ = h.run("repos", "--depth", "1")
	if strings.Contains(out, "deep") || !strings.Contains(out, "api") {
		t.Errorf("depth 1:\n%s", out)
	}
	for _, d := range []string{"0", "-1"} {
		code, out, errOut := h.run("repos", "--depth", d)
		if code != 1 || out != "" || !strings.Contains(errOut, i18n.In("vi", "cli.repos.depth_invalid")) {
			t.Errorf("depth %s: %d %q %q", d, code, out, errOut)
		}
	}
	if code, _, _ := h.run("repos", "--online", "--max-pages", "0"); code != 1 {
		t.Errorf("max-pages 0: %d", code)
	}
	if code, _, _ := h.run("repos", "--online", "--max-pages", "501"); code != 1 {
		t.Errorf("max-pages 501: %d", code)
	}
}

// localJSON is the section 7 schema: the decode fails on an unknown field.
type localJSON struct {
	Companies []struct {
		Slug      string `json:"slug"`
		BaseDir   string `json:"base_dir"`
		State     string `json:"state"`
		Truncated bool   `json:"truncated"`
		Denied    int    `json:"denied"`
		Repos     []struct {
			Path           string   `json:"path"`
			Kind           string   `json:"kind"`
			Remote         string   `json:"remote"`
			Key            string   `json:"key"`
			Account        string   `json:"account"`
			Reason         string   `json:"reason"`
			EffectiveEmail string   `json:"effective_email"`
			EmailOrigin    string   `json:"email_origin"`
			ExpectedEmail  string   `json:"expected_email"`
			Status         string   `json:"status"`
			Flags          []string `json:"flags"`
			BelongsTo      string   `json:"belongs_to"`
			Duplicates     []string `json:"duplicates"`
			FixTo          string   `json:"fix_to"`
		} `json:"repos"`
	} `json:"companies"`
}

type onlineJSON struct {
	Accounts []struct {
		Account     string   `json:"account"`
		Platform    string   `json:"platform"`
		Host        string   `json:"host"`
		TokenSource string   `json:"token_source"`
		Login       string   `json:"login"`
		State       string   `json:"state"`
		Error       string   `json:"error"`
		ErrorKind   string   `json:"error_kind"`
		Truncated   bool     `json:"truncated"`
		Warnings    []string `json:"warnings"`
		Repos       []struct {
			FullName   string   `json:"full_name"`
			Owner      string   `json:"owner"`
			Name       string   `json:"name"`
			Private    bool     `json:"private"`
			Archived   bool     `json:"archived"`
			Fork       bool     `json:"fork"`
			Empty      bool     `json:"empty"`
			UpdatedAt  *string  `json:"updated_at"`
			WebURL     string   `json:"web_url"`
			CloneURL   string   `json:"clone_url"`
			LocalPaths []string `json:"local_paths"`
		} `json:"repos"`
	} `json:"accounts"`
}

func decodeStrict(t *testing.T, s string) (v struct {
	Local  *localJSON  `json:"local"`
	Online *onlineJSON `json:"online"`
}) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("%v\n%s", err, s)
	}
	return v
}

func TestCL6JSONLocal(t *testing.T) {
	h := newHarness(t, nil)
	h.localFixture()
	code, out, _ := h.run("repos", "--json")
	if code != 0 {
		t.Fatalf("%d\n%s", code, out)
	}
	v := decodeStrict(t, out)
	if v.Online != nil || v.Local == nil || len(v.Local.Companies) != 3 {
		t.Fatalf("%s", out)
	}
	acme := v.Local.Companies[0]
	if acme.Slug != "acme" || acme.BaseDir != "~/Code/acme/" || acme.State != "ok" || len(acme.Repos) != 2 {
		t.Fatalf("%+v", acme)
	}
	for _, r := range acme.Repos {
		if !filepath.IsAbs(r.Path) || r.Status != "ok" || r.Account != "acme/alice" || r.Key == "" || r.Flags == nil || r.Duplicates == nil || r.Kind != "repo" {
			t.Errorf("%+v", r)
		}
	}
	// the arrays are [] and not null
	if !strings.Contains(out, `"flags": []`) || strings.Contains(out, "null") {
		t.Errorf("null in the JSON:\n%s", out)
	}
}

func TestCL7InvalidConfig(t *testing.T) {
	h := newHarness(t, nil)
	testutil.WriteFile(t, h.e.Paths().ConfigFile, "version: 99\ncompanies: [\n", 0o644)
	for _, args := range [][]string{{"repos"}, {"repos", "--online"}, {"token", "status"}} {
		code, out, errOut := h.run(args...)
		if code != 1 || out != "" || errOut == "" {
			t.Errorf("%v: %d %q %q", args, code, out, errOut)
		}
	}
}

// ---- online ----

// platformServer answers the GitHub API for every host: the user is the one
// whose token it is; repos come from byToken. A token it does not know is a
// 401.
type platformServer struct {
	mu    sync.Mutex
	paths []string
}

func (h *harness) githubAPI(users map[string]string, repos map[string][][]map[string]any) *platformServer {
	h.t.Helper()
	ps := &platformServer{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ps.mu.Lock()
		ps.paths = append(ps.paths, r.Host+r.URL.Path)
		ps.mu.Unlock()
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		user, ok := users[tok]
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
			return
		}
		if r.URL.Path == "/user" {
			_ = json.NewEncoder(w).Encode(map[string]any{"login": user})
			return
		}
		page := 1
		fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
		pages := repos[user]
		if page > len(pages) {
			_, _ = w.Write([]byte("[]"))
			return
		}
		if page < len(pages) {
			w.Header().Set("Link", fmt.Sprintf(`<https://%s%s?page=%d>; rel="next"`, r.Host, r.URL.Path, page+1))
		}
		_ = json.NewEncoder(w).Encode(pages[page-1])
	}))
	h.t.Cleanup(srv.Close)
	h.e.HTTP = testutil.RedirectTransport(srv)
	return ps
}

func ghRepo(owner, name string) map[string]any {
	return map[string]any{"name": name, "full_name": owner + "/" + name, "owner": map[string]any{"login": owner},
		"size": 5, "pushed_at": "2026-11-10T10:00:00Z", "private": true}
}

func (ps *platformServer) requests() []string {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return append([]string(nil), ps.paths...)
}

func TestCL8OnlineNoToken(t *testing.T) {
	h := newHarness(t, nil)
	h.localFixture()
	code, out, errOut := h.run("repos", "--online")
	if code != 0 || errOut != "" {
		t.Fatalf("%d %q\n%s", code, errOut, out)
	}
	if !strings.Contains(out, i18n.In("vi", "cli.repos.token_none")) || !strings.Contains(out, "GITID_TOKEN_ACME__ALICE") {
		t.Errorf("no token text:\n%s", out)
	}
	fits(t, out)
	// the TempEnv transport fails the test on any request, so none was made
	code, out, _ = h.run("repos", "--online", "--json")
	v := decodeStrict(t, out)
	if code != 0 || v.Local != nil || v.Online == nil || len(v.Online.Accounts) != 5 {
		t.Fatalf("%d %s", code, out)
	}
	for _, a := range v.Online.Accounts {
		if a.State != "no_token" || a.TokenSource != "none" || a.Error == "" || a.Repos == nil {
			t.Errorf("%+v", a)
		}
	}
}

func (h *harness) onlineFixture() *platformServer {
	h.localFixture()
	h.setVars(map[string]string{
		"GITID_TOKEN_ACME__ALICE": secretTok,
		"GITID_TOKEN_ACME__BOB":   "tok_BAD_0000",
	})
	h.mkRepo("acme/other-local", "https://alice@github.com/acme/other.git", "alice@acme.com")
	return h.githubAPI(map[string]string{secretTok: "alice"}, map[string][][]map[string]any{
		"alice": {{ghRepo("acme", "api"), ghRepo("acme", "cloud-only")}},
	})
}

func TestCL9OnlineHappyAndSEC8(t *testing.T) {
	h := newHarness(t, nil)
	ps := h.onlineFixture()
	code, out, errOut := h.run("repos", "--online", "--json")
	// bob's token is refused: exit 2
	if code != 2 || errOut != "" {
		t.Fatalf("%d %q\n%s", code, errOut, out)
	}
	noSecret(t, "json", out, errOut) // SEC-8
	v := decodeStrict(t, out)
	var alice, bob = -1, -1
	for i, a := range v.Online.Accounts {
		switch a.Account {
		case "acme/alice":
			alice = i
		case "acme/bob":
			bob = i
		}
	}
	a := v.Online.Accounts[alice]
	if a.State != "ok" || a.Login != "alice" || a.TokenSource != "env" || a.Platform != "github" || a.Host != "github.com" || len(a.Repos) != 2 {
		t.Fatalf("%+v", a)
	}
	api := a.Repos[0]
	if api.FullName != "acme/api" || !api.Private || api.UpdatedAt == nil || *api.UpdatedAt != "2026-11-10T10:00:00Z" ||
		api.CloneURL != "https://alice@github.com/acme/api.git" || api.WebURL != "https://github.com/acme/api" {
		t.Errorf("%+v", api)
	}
	if len(api.LocalPaths) != 1 || api.LocalPaths[0] != filepath.Join(h.e.Home, "Code", "acme", "api") {
		t.Errorf("local_paths %v", api.LocalPaths)
	}
	if len(a.Repos[1].LocalPaths) != 0 || a.Repos[1].LocalPaths == nil {
		t.Errorf("cloud-only: %v", a.Repos[1].LocalPaths)
	}
	// CL-10: the 401 of bob does not hide the others
	b := v.Online.Accounts[bob]
	if b.State != "error" || b.ErrorKind != "auth" || b.Error == "" || len(b.Repos) != 0 {
		t.Errorf("bob %+v", b)
	}
	// the text output
	code, out, _ = h.run("repos", "--online")
	if code != 2 {
		t.Errorf("text code %d", code)
	}
	noSecret(t, "text", out)
	fits(t, out)
	if !strings.Contains(out, "acme/api") || !strings.Contains(out, "✓ ~/Code/acme/api") || !strings.Contains(out, "! ✗") {
		t.Errorf("text:\n%s", out)
	}
	// the token reached only the API host
	for _, p := range ps.requests() {
		if !strings.HasPrefix(p, "api.github.com/") {
			t.Errorf("request %s", p)
		}
	}
	// no call of the runner holds the token
	for _, c := range h.f.Calls() {
		if strings.Contains(strings.Join(append([]string{c.Stdin}, c.Args...), "\x00"), "SECRET_9f3c") {
			t.Errorf("token in %s", c.String())
		}
	}
}

func TestCL11Account(t *testing.T) {
	h := newHarness(t, nil)
	ps := h.onlineFixture()
	code, out, _ := h.run("repos", "--online", "--account", "acme/alice", "--json")
	if code != 0 {
		t.Fatalf("%d\n%s", code, out)
	}
	v := decodeStrict(t, out)
	if len(v.Online.Accounts) != 1 || v.Online.Accounts[0].Account != "acme/alice" {
		t.Fatalf("%s", out)
	}
	for _, c := range h.f.CallStrings() {
		if strings.Contains(c, "bob") || strings.Contains(c, "dave") || strings.Contains(c, "carol") || strings.HasPrefix(c, "gh ") || strings.HasPrefix(c, "glab ") {
			t.Errorf("another account was resolved: %s", c)
		}
	}
	if n := len(ps.requests()); n != 2 {
		t.Errorf("%d requests: %v", n, ps.requests())
	}
	// an unknown account is an error
	if code, _, errOut := h.run("repos", "--online", "--account", "acme/nobody"); code != 1 || errOut == "" {
		t.Errorf("%d %q", code, errOut)
	}
}

func TestCL12MaxPages(t *testing.T) {
	h := newHarness(t, nil)
	h.localFixture()
	h.setVars(map[string]string{"GITID_TOKEN_ACME__ALICE": secretTok})
	h.githubAPI(map[string]string{secretTok: "alice"}, map[string][][]map[string]any{
		"alice": {{ghRepo("acme", "a")}, {ghRepo("acme", "b")}, {ghRepo("acme", "c")}},
	})
	_, out, _ := h.run("repos", "--online", "--account", "acme/alice", "--max-pages", "1", "--json")
	v := decodeStrict(t, out)
	a := v.Online.Accounts[0]
	if !a.Truncated || len(a.Repos) != 1 {
		t.Fatalf("%+v", a)
	}
	_, out, _ = h.run("repos", "--online", "--account", "acme/alice", "--max-pages", "1")
	if !strings.Contains(out, i18n.In("vi", "tui.repos.online.truncated", 1, "1")) {
		t.Errorf("no truncation note:\n%s", out)
	}
	// without the flag: all three pages
	h2 := newHarness(t, nil)
	h2.localFixture()
	h2.setVars(map[string]string{"GITID_TOKEN_ACME__ALICE": secretTok})
	h2.githubAPI(map[string]string{secretTok: "alice"}, map[string][][]map[string]any{
		"alice": {{ghRepo("acme", "a")}, {ghRepo("acme", "b")}, {ghRepo("acme", "c")}},
	})
	_, out, _ = h2.run("repos", "--online", "--account", "acme/alice", "--json")
	if a := decodeStrict(t, out).Online.Accounts[0]; a.Truncated || len(a.Repos) != 3 {
		t.Fatalf("%+v", a)
	}
}

func TestCL13Cancelled(t *testing.T) {
	h := newHarness(t, nil)
	h.localFixture()
	h.setVars(map[string]string{"GITID_TOKEN_ACME__ALICE": secretTok})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, args := range [][]string{{"repos"}, {"repos", "--online"}, {"repos", "--json"}} {
		code, out, errOut := h.runDeps(func(d *cli.Deps) { d.Ctx = ctx }, args...)
		if code != 1 || !strings.Contains(errOut, i18n.In("vi", "cli.repos.cancelled")) {
			t.Errorf("%v: %d %q", args, code, errOut)
		}
		noSecret(t, "cancelled", out, errOut)
		fits(t, out)
	}
}

func TestCL14NonTTY(t *testing.T) {
	for _, tty := range []bool{false, true} {
		h := newHarness(t, nil)
		h.tty = tty
		h.onlineFixture()
		for _, args := range [][]string{{"repos"}, {"repos", "--online"}} {
			_, out, errOut := h.run(args...)
			for _, s := range []string{out, errOut} {
				if strings.ContainsAny(s, "\r\x1b") {
					t.Errorf("tty=%v %v: control characters in the output: %q", tty, args, s)
				}
			}
		}
	}
}

// SEC-11: an escape sequence from the API reaches no output.
func TestSEC11EscapeFromAPI(t *testing.T) {
	h := newHarness(t, nil)
	h.localFixture()
	h.setVars(map[string]string{"GITID_TOKEN_ACME__ALICE": secretTok})
	h.githubAPI(map[string]string{secretTok: "alice"}, map[string][][]map[string]any{
		"alice": {{ghRepo("acme", "ev\x1b[2Jil\x1b]0;pwn\x07"), ghRepo("acme", "ok")}},
	})
	for _, args := range [][]string{{"repos", "--online", "--account", "acme/alice"}, {"repos", "--online", "--account", "acme/alice", "--json"}} {
		code, out, _ := h.run(args...)
		if code != 0 || strings.Contains(out, "\x1b") {
			t.Errorf("%v: %d %q", args, code, out)
		}
	}
}

// ---- token ----

func (h *harness) security() *map[string]string {
	items := map[string]string{}
	var mu sync.Mutex
	h.f.On("security -i", execx.Response{Do: func(c execx.Cmd) error {
		fields := strings.Fields(c.Stdin)
		var acct, hx string
		for i, w := range fields {
			switch w {
			case "-a":
				acct = strings.Trim(fields[i+1], `"`)
			case "-X":
				hx = fields[i+1]
			}
		}
		b, err := hex.DecodeString(hx)
		if err != nil {
			return err
		}
		mu.Lock()
		items[acct] = string(b)
		mu.Unlock()
		return nil
	}})
	return &items
}

func TestKC1SetPipe(t *testing.T) {
	h := newHarness(t, nil)
	h.seed()
	items := h.security()
	h.in = secretTok + "\n"
	code, out, errOut := h.run("token", "set", "acme/alice")
	if code != 0 {
		t.Fatalf("%d %q", code, errOut)
	}
	if !strings.Contains(out, i18n.In("vi", "cli.token.saved", "acme/alice")) {
		t.Errorf("%q", out)
	}
	noSecret(t, "set", out, errOut)
	var calls []execx.Cmd
	for _, c := range h.f.Calls() {
		if c.Name == "security" {
			calls = append(calls, c)
		}
	}
	if len(calls) != 1 || calls[0].String() != "security -i" {
		t.Fatalf("%v", h.f.CallStrings())
	}
	for _, a := range append(calls[0].Args, calls[0].Env...) {
		if strings.Contains(a, secretTok) || strings.Contains(a, hex.EncodeToString([]byte(secretTok))) {
			t.Errorf("token in argv/env: %q", a)
		}
	}
	if !strings.Contains(calls[0].Stdin, `add-generic-password -U -s gitid -a "github.com/alice"`) {
		t.Errorf("stdin %q", calls[0].Stdin)
	}
	if (*items)["github.com/alice"] != secretTok {
		t.Errorf("items %v", *items)
	}
}

func TestKC2TTY(t *testing.T) {
	h := newHarness(t, nil)
	h.seed()
	h.security()
	h.tty = true
	asked := 0
	code, out, errOut := h.runDeps(func(d *cli.Deps) {
		d.ReadSecret = func() (string, error) { asked++; return secretTok + "\n", nil }
	}, "token", "set", "acme/alice")
	if code != 0 || asked != 1 {
		t.Fatalf("%d asked=%d %q", code, asked, errOut)
	}
	if !strings.Contains(errOut, i18n.In("vi", "cli.token.prompt", "acme/alice")) {
		t.Errorf("no prompt: %q", errOut)
	}
	noSecret(t, "tty", out, errOut)
	// stdin is never read on a TTY
	h2 := newHarness(t, nil)
	h2.seed()
	h2.tty = true
	code, _, errOut = h2.run("token", "set", "acme/alice") // no ReadSecret
	if code != 1 || !strings.Contains(errOut, i18n.In("vi", "cli.token.no_secret_tty")) {
		t.Errorf("%d %q", code, errOut)
	}
}

func TestKC3And4BadInput(t *testing.T) {
	for in, want := range map[string]string{
		"":                               "cli.token.empty",
		"\n":                             "cli.token.empty",
		"   \n":                          "cli.token.empty",
		"two words\n":                    "cli.token.bad",
		"a\x00b\n":                       "cli.token.bad",
		"tok\x1b[31m\n":                  "cli.token.bad",
		"tók\n":                          "cli.token.bad",
		strings.Repeat("a", 5000) + "\n": "cli.token.bad",
	} {
		h := newHarness(t, nil)
		h.seed()
		h.in = in
		code, out, errOut := h.run("token", "set", "acme/alice")
		if code != 1 || out != "" || !strings.Contains(errOut, i18n.In("vi", want)) {
			t.Errorf("%q: %d %q", in, code, errOut)
		}
		for _, c := range h.f.CallStrings() {
			if strings.HasPrefix(c, "security") {
				t.Errorf("%q: security was called: %s", in, c)
			}
		}
	}
}

func TestKC5NotMac(t *testing.T) {
	h := newHarness(t, nil)
	h.seed()
	h.e.GOOS = "linux"
	h.in = secretTok + "\n"
	code, out, errOut := h.run("token", "set", "acme/alice")
	if code != 1 || out != "" || !strings.Contains(errOut, i18n.In("vi", "cli.token.unsupported", "GITID_TOKEN_ACME__ALICE")) {
		t.Fatalf("%d %q", code, errOut)
	}
	for _, c := range h.f.CallStrings() {
		if strings.HasPrefix(c, "security") {
			t.Errorf("security was called: %s", c)
		}
	}
	code, _, errOut = h.run("token", "rm", "acme/alice")
	if code != 1 || !strings.Contains(errOut, "GITID_TOKEN_ACME__ALICE") {
		t.Errorf("rm: %d %q", code, errOut)
	}
	// an unknown account is refused first
	if code, _, _ := h.run("token", "set", "acme/nobody"); code != 1 {
		t.Errorf("%d", code)
	}
}

func TestKC6Remove(t *testing.T) {
	h := newHarness(t, nil)
	h.seed()
	h.f.On("security delete-generic-password -s gitid -a github.com/alice", execx.Response{})
	code, out, _ := h.run("token", "rm", "acme/alice")
	if code != 0 || !strings.Contains(out, i18n.In("vi", "cli.token.removed", "acme/alice")) {
		t.Errorf("%d %q", code, out)
	}
	h2 := newHarness(t, nil)
	h2.seed()
	h2.f.On("security delete-generic-password", execx.Response{ExitCode: 44})
	code, out, _ = h2.run("token", "rm", "acme/alice")
	if code != 0 || !strings.Contains(out, i18n.In("vi", "cli.token.nothing", "acme/alice")) {
		t.Errorf("%d %q", code, out)
	}
}

func TestKC7Status(t *testing.T) {
	h := newHarness(t, nil)
	h.seed()
	h.setVars(map[string]string{"GITID_TOKEN_ACME__ALICE": secretTok})
	h.f.On("security find-generic-password", execx.Response{ExitCode: 44})
	code, out, errOut := h.run("token", "status", "--json")
	if code != 0 {
		t.Fatalf("%d %q", code, errOut)
	}
	noSecret(t, "status json", out, errOut)
	var v struct {
		Accounts []struct {
			Account string `json:"account"`
			EnvVar  string `json:"env_var"`
			Source  string `json:"source"`
		} `json:"accounts"`
	}
	dec := json.NewDecoder(strings.NewReader(out))
	if err := dec.Decode(&v); err != nil || len(v.Accounts) != 5 {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, a := range v.Accounts {
		want := "none"
		if a.Account == "acme/alice" {
			want = "env"
		}
		if a.Source != want {
			t.Errorf("%s: source %q, want %q", a.Account, a.Source, want)
		}
	}
	code, out, _ = h.run("token", "status")
	noSecret(t, "status", out)
	fits(t, out)
	if code != 0 || !strings.Contains(out, "acme/alice") || !strings.Contains(out, "env") {
		t.Errorf("%d\n%s", code, out)
	}
	code, out, _ = h.run("token", "status", "acme/bob")
	if code != 0 || strings.Contains(out, "acme/alice") || !strings.Contains(out, "GITID_TOKEN_ACME__BOB") {
		t.Errorf("%d\n%s", code, out)
	}
}

// Every platform has a no-token hint that names the variable and the command
// to store a token, in both languages, and every line fits 80 columns.
func TestCLNoTokenHintsAllPlatforms(t *testing.T) {
	for _, lang := range []string{"vi", "en"} {
		h := newHarness(t, nil)
		h.seedCfg(fixtures.Full())
		code, out, _ := h.runDeps(func(d *cli.Deps) { d.Lang = lang }, "repos", "--online")
		if code != 0 {
			t.Fatalf("%s: %d\n%s", lang, code, out)
		}
		for _, want := range []string{"gitid token set gt/sam", "gitid token set bb/joe", "gitid token set az/dev", "GITID_TOKEN_GT__SAM", "GITID_TOKEN_AZ__DEV"} {
			if !strings.Contains(strings.Join(strings.Fields(out), " "), want) { // the hint wraps
				t.Errorf("%s: missing %q:\n%s", lang, want, out)
			}
		}
		fits(t, out)
	}
	i18n.SetLang("vi")
}

// A Bitbucket or Azure account on another host (Data Center, DevOps Server)
// is unsupported: no request, no error, exit 0.
func TestCLOffHostUnsupported(t *testing.T) {
	h := newHarness(t, nil)
	cfg := fixtures.Full()
	for ci := range cfg.Companies {
		for ai := range cfg.Companies[ci].Accounts {
			a := &cfg.Companies[ci].Accounts[ai]
			switch a.Platform {
			case "bitbucket":
				if a.ID == "joe" {
					a.Host = "bb.corp.example"
				}
			case "azure-devops":
				if a.ID == "dev" {
					a.Host = "tfs.corp.example"
				}
			}
		}
	}
	h.seedCfg(cfg)
	h.setVars(map[string]string{"GITID_TOKEN_BB__JOE": "tok_x", "GITID_TOKEN_AZ__DEV": "tok_y"})
	code, out, errOut := h.run("repos", "--online", "--json", "--account", "bb/joe")
	if code != 0 || errOut != "" {
		t.Fatalf("bb: %d %q\n%s", code, errOut, out)
	}
	v := decodeStrict(t, out)
	if len(v.Online.Accounts) != 1 || v.Online.Accounts[0].State != "unsupported" || v.Online.Accounts[0].ErrorKind != "" {
		t.Errorf("bb: %+v", v.Online.Accounts)
	}
	code, out, errOut = h.run("repos", "--online", "--json", "--account", "az/dev")
	if code != 0 || errOut != "" {
		t.Fatalf("az: %d %q\n%s", code, errOut, out)
	}
	v = decodeStrict(t, out)
	if len(v.Online.Accounts) != 1 || v.Online.Accounts[0].State != "unsupported" {
		t.Errorf("az: %+v", v.Online.Accounts)
	}
	code, out, _ = h.run("repos", "--online", "--account", "bb/joe")
	if code != 0 || !strings.Contains(out, i18n.In("vi", "cli.repos.unsupported", "Bitbucket")) {
		t.Errorf("text: %d %q", code, out)
	}
}

// token rm: a security failure other than "not found" says nothing was
// removed, not that nothing was stored.
func TestKC6RemoveFailed(t *testing.T) {
	h := newHarness(t, nil)
	h.seed()
	h.f.On("security delete-generic-password", execx.Response{ExitCode: 1})
	code, _, errOut := h.run("token", "rm", "acme/alice")
	if code != 1 || !strings.Contains(errOut, i18n.In("vi", "cli.token.rm_failed")) ||
		strings.Contains(errOut, i18n.In("vi", "cli.token.failed")) {
		t.Errorf("%d %q", code, errOut)
	}
	if i18n.In("en", "cli.token.rm_failed") == i18n.In("vi", "cli.token.rm_failed") {
		t.Error("rm_failed not translated")
	}
}

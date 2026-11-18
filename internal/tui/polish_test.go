package tui

// Tests of the UX polish pass: the account form preview and guards, the next
// steps after apply, the account detail pane and the stacked split, the stale
// configuration guard, the wrong-username path, the check on a non-repo, the
// advanced opt-in and the small fixes. Every expectation is built from the
// same i18n keys the UI uses, so the tests follow the wording and the language.
//
// Rules: HOME is a temp dir (testutil.TempEnv), git and ssh go through
// execx.Fake, nothing reads a private key, nothing touches the network.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/keymap"
	"github.com/tinhnd1902/gitid/internal/platform"
	"github.com/tinhnd1902/gitid/internal/remote"
	"github.com/tinhnd1902/gitid/internal/testutil"
)

// ---- fixtures and helpers ----

// personalAccount is a GitHub https account of the login "tinhnd".
func personalAccount(id string) config.Account {
	return config.Account{ID: id, Platform: platform.GitHub, Username: "tinhnd", Name: "Tinh Nguyen", Email: "tinhnd@example.com", Auth: config.AuthHTTPS}
}

// dupCfg is the shape of the config that started this pass: company personal
// with two accounts that sign in as the same GitHub user.
func dupCfg() *config.Config {
	return &config.Config{Version: 1, Companies: []config.Company{{
		Slug: "personal", Name: "Personal", BaseDir: "~/Code/personal/", DefaultAccount: "tinhnd",
		Accounts: []config.Account{personalAccount("tinhnd"), personalAccount("tinhnd-github")},
	}}}
}

// oneAccCfg is the same company with only tinhnd.
func oneAccCfg() *config.Config {
	return &config.Config{Version: 1, Companies: []config.Company{{
		Slug: "personal", Name: "Personal", BaseDir: "~/Code/personal/", DefaultAccount: "tinhnd",
		Accounts: []config.Account{personalAccount("tinhnd")},
	}}}
}

// sshCfg is one company with one ssh account.
func sshCfg() *config.Config {
	a := personalAccount("alice")
	a.Username, a.Auth = "alice", config.AuthSSH
	return &config.Config{Version: 1, Companies: []config.Company{{
		Slug: "personal", Name: "Personal", BaseDir: "~/Code/personal/", DefaultAccount: "alice", Accounts: []config.Account{a},
	}}}
}

// cloneConfig copies the slices of cfg so a test can change it freely.
func cloneConfig(cfg config.Config) config.Config {
	out := cfg
	out.Companies = make([]config.Company, len(cfg.Companies))
	for i, c := range cfg.Companies {
		c.Accounts = append([]config.Account(nil), c.Accounts...)
		out.Companies[i] = c
	}
	return out
}

// noSpace removes every space, so text wrapped by the UI (and hyphen breaks)
// compares equal to the unwrapped string.
func noSpace(s string) string { return strings.ReplaceAll(plain(s), " ", "") }

// listArea is the part of a view line that belongs to the list panel (the
// detail pane sits to its right in the side layout).
func listArea(line string, width int) string {
	r := []rune(line)
	if len(r) > width {
		r = r[:width]
	}
	return string(r)
}

// fillAccount types a GitHub/HTTPS account the way a user would: platform,
// username, name (skipped when the form prefilled it), email, auth, and
// "advanced settings" = no. The make-default question, when the company has
// other accounts, is left to the caller.
func fillAccount(h *harness, user, name, email string) {
	h.t.Helper()
	h.key("enter") // platform
	h.typeText(user)
	h.key("enter")
	if name != "" {
		h.typeText(name)
	}
	h.key("enter")
	h.typeText(email)
	h.key("enter")
	h.key("enter") // auth: https
	h.key("enter") // advanced settings: no
}

// mustContainSpaceless checks text that the UI may break anywhere, even inside
// a long word such as an address: it compares without any whitespace.
func (h *harness) mustContainSpaceless(parts ...string) {
	h.t.Helper()
	v := noSpace(h.view())
	for _, p := range parts {
		if !strings.Contains(v, noSpace(p)) {
			h.t.Errorf("view does not contain %q:\n%s", p, h.view())
		}
	}
}

func (h *harness) sendKey(t tea.KeyType) { h.t.Helper(); h.send(tea.KeyMsg{Type: t}) }

func (h *harness) waitConfirm() {
	h.t.Helper()
	h.waitFor("confirm screen", func() bool { _, ok := h.top().(*confirmScreen); return ok })
}

// waitReady waits until the confirm screen shows the diff.
func (h *harness) waitReady() {
	h.t.Helper()
	h.waitFor("diff ready", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confReady })
}

// applyToDone opens the confirm screen with p and applies with y.
func (h *harness) applyToDone() {
	h.t.Helper()
	h.act(keymap.Apply)
	h.waitReady()
	h.act(keymap.Yes)
	h.waitFor("done", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confDone })
}

// ---- helper unit tests (TC-B12, remoteExample) ----

func TestSameLoginAndDuplicates(t *testing.T) {
	gh := func(id, user, host string) config.Account {
		return config.Account{ID: id, Platform: platform.GitHub, Username: user, Host: host}
	}
	if !sameLogin(gh("a", "alice", ""), gh("b", "alice", "github.com")) {
		t.Error("default host and explicit github.com are the same")
	}
	if !sameLogin(gh("a", "alice", ""), gh("b", "Alice", "GitHub.com")) {
		t.Error("case must not matter")
	}
	if sameLogin(gh("a", "alice", ""), config.Account{ID: "b", Platform: platform.GitLab, Username: "alice"}) {
		t.Error("another platform is another login")
	}
	if sameLogin(gh("a", "alice", ""), gh("b", "alice", "ghe.acme.com")) {
		t.Error("another host is another login")
	}

	c := config.Company{Slug: "x", Accounts: []config.Account{gh("a", "alice", ""), gh("b", "bob", "")}}
	if d, ok := dupInCompany(c, gh("new", "ALICE", ""), ""); !ok || d.ID != "a" {
		t.Errorf("dup %+v %v", d, ok)
	}
	if _, ok := dupInCompany(c, gh("a", "alice", ""), "a"); ok {
		t.Error("an account is not a duplicate of itself")
	}
	if _, ok := dupInCompany(c, gh("new", "carol", ""), ""); ok {
		t.Error("no duplicate expected")
	}

	cfg := config.Config{Companies: []config.Company{c,
		{Slug: "y", Accounts: []config.Account{gh("a", "alice", ""), gh("z", "zed", "")}},
		{Slug: "w", Accounts: []config.Account{gh("q", "alice", "")}},
	}}
	got := loginElsewhere(cfg, "x", gh("new", "alice", ""))
	if len(got) != 2 || got[0] != "y/a" || got[1] != "w/q" {
		t.Errorf("loginElsewhere %v", got)
	}
	if got := loginElsewhere(cfg, "x", gh("new", "nobody", "")); len(got) != 0 {
		t.Errorf("loginElsewhere %v", got)
	}

	for _, tc := range []struct {
		p    platform.Platform
		user string
		want bool
	}{
		{platform.GitHub, "alice@acme.com", true},
		{platform.GitLab, "alice@acme.com", true},
		{platform.Gitea, "alice@acme.com", true},
		{platform.Bitbucket, "alice@acme.com", false},
		{platform.AzureDevOps, "alice@acme.com", false},
		{platform.GitHub, "alice", false},
	} {
		if looksLikeEmail(tc.p, tc.user) != tc.want {
			t.Errorf("looksLikeEmail(%s, %q) != %v", tc.p, tc.user, tc.want)
		}
	}
}

func TestRemoteExample(t *testing.T) {
	acme := config.Company{Slug: "acme"}
	ref := func(a config.Account) config.AccountRef { return config.AccountRef{Company: acme, Account: a} }
	for _, tc := range []struct {
		name string
		acc  config.Account
		want string
	}{
		{"github https", config.Account{ID: "alice", Platform: platform.GitHub, Username: "alice", Auth: config.AuthHTTPS},
			"https://alice@github.com/<owner>/<repo>.git"},
		{"github ssh", config.Account{ID: "alice", Platform: platform.GitHub, Username: "alice", Auth: config.AuthSSH},
			"git@github-acme-alice:<owner>/<repo>.git"},
		{"gitlab dotted", config.Account{ID: "john-doe", Platform: platform.GitLab, Username: "john.doe", Auth: config.AuthHTTPS},
			"https://john.doe@gitlab.com/<owner>/<repo>.git"},
		{"azure https", config.Account{ID: "dev", Platform: platform.AzureDevOps, Username: "dev", Auth: config.AuthHTTPS},
			"https://dev@dev.azure.com/<org>/<project>/_git/<repo>"},
		{"azure ssh", config.Account{ID: "dev", Platform: platform.AzureDevOps, Username: "dev", Auth: config.AuthSSH},
			"git@azure-devops-acme-dev:v3/<org>/<project>/<repo>"},
		{"gitea without host", config.Account{ID: "sam", Platform: platform.Gitea, Username: "sam", Auth: config.AuthHTTPS},
			"https://sam@<host>/<owner>/<repo>.git"},
		{"custom host", config.Account{ID: "dave", Platform: platform.GitLab, Host: "git.example.com:8443", Username: "dave", Auth: config.AuthHTTPS},
			"https://dave@git.example.com:8443/<owner>/<repo>.git"},
	} {
		if got := remoteExample(ref(tc.acc)); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	// an invalid host falls back to the short description, without a panic
	bad := config.Account{ID: "x", Platform: platform.GitHub, Host: "bad host!", Username: "x", Auth: config.AuthHTTPS}
	if got := remoteExample(ref(bad)); got == "" {
		t.Error("fallback expected")
	}
}

// ---- A. account form: preview, id, hints (P0-1, P0-3) ----

func TestA1PreviewShowsTheRemoteAndTheID(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.key("a")
	h.key("enter")
	h.typeText("alice")
	h.mustContainPlain(tr("tui.form.preview_remote", "https://alice@github.com/<owner>/<repo>.git"), tr("tui.form.preview_id", "alice"))
	for _, s := range [][2]int{{80, 24}, {100, 30}, {120, 40}} {
		h.resize(s[0], s[1])
		h.mustContainPlain(tr("tui.form.preview_remote", "https://alice@github.com/<owner>/<repo>.git"), tr("tui.form.preview_id", "alice"))
		assertFits(t, h.view(), s[0], s[1], "account form")
	}
}

func TestA2PreviewFollowsAuth(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.key("a")
	h.key("enter")
	h.typeText("alice")
	h.key("enter")
	h.typeText("Alice Tran")
	h.key("enter")
	h.typeText("alice@acme.com")
	h.key("enter") // auth is focused now
	h.sendKey(tea.KeyDown)
	h.mustContainPlain(tr("tui.form.preview_remote", "git@github-personal-alice:<owner>/<repo>.git"))
	h.sendKey(tea.KeyUp)
	h.mustContainPlain(tr("tui.form.preview_remote", "https://alice@github.com/<owner>/<repo>.git"))
}

func TestA3EmptyUsernameUsesAPlaceholder(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.key("a")
	h.mustContainPlain(tr("tui.form.preview_remote", "https://<username>@github.com/<owner>/<repo>.git"))
	h.mustNotContain("GITIDUSER", "GITIDOWNER", "GITIDREPO")
}

func TestA4A5A6PreviewPerPlatform(t *testing.T) {
	pick := func(h *harness, n int) {
		for i := 0; i < n; i++ {
			h.sendKey(tea.KeyRight)
		}
	}
	// GitLab: a dotted username becomes a valid id
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.key("a")
	pick(h, 1)
	h.key("enter")
	h.typeText("john.doe")
	h.mustContainPlain(tr("tui.form.preview_remote", "https://john.doe@gitlab.com/<owner>/<repo>.git"), tr("tui.form.preview_id", "john-doe"))

	// Azure DevOps
	h = newHarness(t, emptyCompanyCfg("personal"))
	h.key("a")
	pick(h, 4)
	h.key("enter")
	h.typeText("dev")
	h.mustContainPlain(tr("tui.form.preview_remote", "https://dev@dev.azure.com/<org>/<project>/_git/<repo>"))
	h.key("enter")
	h.typeText("Dev One")
	h.key("enter")
	h.typeText("dev@az.example")
	h.key("enter")
	h.sendKey(tea.KeyDown)
	h.mustContainPlain(tr("tui.form.preview_remote", "git@azure-devops-personal-dev:v3/<org>/<project>/<repo>"))

	// Gitea without a host
	h = newHarness(t, emptyCompanyCfg("personal"))
	h.key("a")
	pick(h, 3)
	h.key("enter")
	h.typeText("sam")
	h.mustContainPlain("https://sam@<host>/<owner>/<repo>.git")
}

// The preview is the remote the saved account produces.
func TestA7PreviewEqualsWhatIsSaved(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.key("a")
	h.key("enter")
	h.typeText("alice")
	h.key("enter")
	h.typeText("Alice Tran")
	h.key("enter")
	h.typeText("alice@acme.com")
	h.key("enter")
	h.key("enter") // auth: https
	preview := tr("tui.form.preview_remote", "https://alice@github.com/<owner>/<repo>.git")
	h.mustContainPlain(preview, tr("tui.form.preview_id", "alice"))
	h.key("enter") // advanced: no -> save
	h.waitConfirm()
	ref, err := h.m.sh.cfg.ParseRef("personal/alice")
	if err != nil {
		t.Fatal(err)
	}
	if got := tr("tui.form.preview_remote", remoteExample(ref)); got != preview {
		t.Fatalf("saved account gives %q, the form showed %q", got, preview)
	}
	// and a clone with that account really produces the preview, owner/repo filled in
	out, _, err := remote.Rewrite(h.m.sh.cfg, ref, "https://github.com/o/r.git", remote.Options{})
	if want := strings.NewReplacer("<owner>", "o", "<repo>", "r").Replace(remoteExample(ref)); err != nil || out != want {
		t.Fatalf("clone gives %q (%v), the preview promised %q", out, err, want)
	}
}

func TestA8EditShowsTheFixedID(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	h.selectKey("personal/tinhnd")
	h.key("e")
	h.mustContainPlain(tr("tui.form.preview_id_fixed", "tinhnd"), tr("tui.form.preview_remote", "https://tinhnd@github.com/<owner>/<repo>.git"))
}

func TestA9HintsAreVisibleAndHonest(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.key("a")
	h.resize(100, 50)
	h.mustContainSpaceless(tr("tui.form.username_hint"), tr("tui.form.git_name_hint"), tr("tui.form.email_hint"))
	if v := i18n.In("vi", "tui.form.email_hint"); !strings.Contains(v, "không xác minh") || !strings.Contains(v, "users.noreply.github.com") {
		t.Errorf("vi email hint: %s", v)
	}
	if v := i18n.In("en", "tui.form.email_hint"); !strings.Contains(v, "does not verify") || !strings.Contains(v, "users.noreply.github.com") {
		t.Errorf("en email hint: %s", v)
	}
	defer i18n.SetLang("vi")
	i18n.SetLang("en")
	h2 := newHarness(t, emptyCompanyCfg("personal"))
	h2.key("a")
	h2.resize(100, 50)
	h2.mustContainSpaceless(tr("tui.form.email_hint"))
	h2.mustContainPlain(tr("tui.form.preview_remote", "https://<username>@github.com/<owner>/<repo>.git"))
}

func TestA10EmailHintIsNotCutOnASmallTerminal(t *testing.T) {
	for _, s := range [][2]int{{80, 24}, {100, 30}} {
		h := newHarness(t, emptyCompanyCfg("personal"))
		h.resize(s[0], s[1])
		h.key("a")
		h.key("enter")
		h.typeText("alice")
		h.key("enter")
		h.typeText("Alice Tran")
		h.key("enter") // focus: email
		h.mustContainSpaceless(tr("tui.form.email_hint"))
		assertFits(t, h.view(), s[0], s[1], "email")
	}
}

func TestA11PreviewKeepsTheFormUsable(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.key("a")
	h.key("enter")
	h.typeText("alice")
	for _, s := range [][2]int{{40, 10}, {60, 20}, {80, 24}, {100, 30}, {120, 40}} {
		h.update(tea.WindowSizeMsg{Width: s[0], Height: s[1]})
		assertFits(t, h.view(), s[0], s[1], "account form")
		f := h.top().(*accountForm)
		if f.previewRows() < 1 {
			t.Errorf("%v: no preview", s)
		}
		// a short body shows the remote line alone, a tall one adds the note
		short := s[1]-chromeRows < previewMinBody
		hasID := strings.Contains(plain(h.view()), plain(tr("tui.form.preview_id", "alice")))
		if s[0] >= 60 && short && hasID {
			t.Errorf("%v: the id line must give way on a short terminal", s)
		}
		if s[0] >= 80 && !short && !hasID {
			t.Errorf("%v: the id line is missing:\n%s", s, h.view())
		}
		// the form always keeps room for the focused field
		if inner := f.formRows(); s[1] >= 20 && inner < 4 {
			t.Errorf("%v: only %d rows left for the form", s, inner)
		}
	}
}

func TestA12FastTypingIsRaceFree(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.key("a")
	h.key("enter")
	for _, r := range strings.Repeat("a", 30) {
		h.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		_ = h.view() // View runs between the keys, like the renderer
	}
	h.drain()
	h.mustContainPlain(tr("tui.form.preview_remote", "https://"+strings.Repeat("a", 30)+"@github.com/<owner>/<repo>.git"))
}

// ---- B. duplicate and wrong-login guards (P0-2) ----

func TestB1DuplicateIsRefusedInTheField(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	h.key("a")
	h.key("enter")
	h.typeText("tinhnd")
	h.mustContainPlain(defKM.T("tui.form.err.dup_account", "tinhnd", "github.com")) // live, in the note
	h.key("enter")
	h.mustContainPlain(defKM.T("tui.form.err.dup_account", "tinhnd", "github.com"))
	if _, ok := h.top().(*accountForm); !ok {
		t.Fatalf("top is %T", h.top())
	}
	if !strings.Contains(h.view(), "✗") {
		t.Fatalf("no error mark:\n%s", h.view())
	}
}

func TestB2DuplicateIgnoresCase(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	h.key("a")
	h.key("enter")
	h.typeText("TinhND")
	h.key("enter")
	h.mustContainPlain(defKM.T("tui.form.err.dup_account", "tinhnd", "github.com"))
}

// The host is only known after the username: the check runs again on save.
func TestB3DuplicateIsRefusedOnSave(t *testing.T) {
	cfg := oneAccCfg()
	cfg.Companies[0].Accounts[0].Host = "ghe.acme.com"
	h := newHarness(t, cfg)
	h.key("a")
	h.key("enter")
	h.typeText("tinhnd") // github.com: no duplicate yet
	h.key("enter")
	h.key("enter") // name (prefilled)
	h.typeText("tinhnd@example.com")
	h.key("enter")
	h.key("enter") // auth
	h.key("y")     // advanced settings: yes
	h.typeText("ghe.acme.com")
	h.key("enter") // host
	h.key("enter") // orgs
	h.key("enter") // ssh host
	h.key("enter") // ssh port
	h.key("enter") // make default (the company has another account)
	h.mustContainPlain(defKM.T("tui.form.err.dup_account", "tinhnd", "ghe.acme.com"))
	if _, ok := h.top().(*accountForm); !ok {
		t.Fatalf("top is %T", h.top())
	}
	loaded, err := h.svc.Load()
	if err != nil || len(loaded.Companies[0].Accounts) != 1 {
		t.Fatalf("config on disk: %+v %v", loaded.Companies, err)
	}
}

func TestB4OtherPlatformIsNotADuplicate(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	h.key("a")
	h.sendKey(tea.KeyRight) // GitLab
	h.key("enter")
	h.typeText("tinhnd")
	h.mustContainPlain(tr("tui.form.preview_id", "tinhnd-gitlab"))
	h.key("enter")
	h.key("enter") // name (prefilled)
	h.typeText("tinhnd@example.com")
	h.key("enter")
	h.key("enter") // auth
	h.key("enter") // advanced: no
	h.key("enter") // make default: no
	h.waitConfirm()
	if _, _, ok := h.m.sh.cfg.Companies[0].FindAccount("tinhnd-gitlab"); !ok {
		t.Fatalf("accounts: %+v", h.m.sh.cfg.Companies[0].Accounts)
	}
}

// The same login on another host is another account. (The form checks the
// username before it asks for the host, so such an account is added from the
// host side: the check is by platform, host and username.)
func TestB5OtherHostIsNotADuplicate(t *testing.T) {
	cfg := dupCfg()
	cfg.Companies[0].Accounts[1].Host = "ghe.acme.com"
	h := newHarness(t, cfg)
	for _, l := range h.lines() {
		if strings.Contains(listArea(l, 55), "tinhnd") && strings.Contains(listArea(l, 55), " !") {
			t.Errorf("different hosts are no duplicates: %q", l)
		}
	}
	h.selectKey("personal/tinhnd-github")
	if d := h.detail(); strings.Contains(d, plain(defKM.T("tui.detail.dup", "personal/tinhnd"))) {
		t.Fatalf("detail: %s", d)
	}
	h.key("e")                // editing it is not blocked by the account on github.com
	for i := 0; i < 12; i++ { // the account has a host: the advanced fields come too
		if _, ok := h.top().(*confirmScreen); ok {
			break
		}
		h.key("enter")
		h.mustNotContain("✗")
	}
	h.waitConfirm()
}

func TestB6SameLoginInAnotherCompanyIsOnlyAWarning(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.selectKey("beta")
	h.key("a")
	h.key("enter")
	h.typeText("bob") // bob exists only in acme
	h.mustContainPlain(tr("tui.form.warn.dup_other", "acme/bob"))
	h.key("enter")
	h.key("enter") // name (prefilled)
	h.typeText("bob@beta.io")
	h.key("enter")
	h.key("enter") // auth
	h.key("enter") // advanced: no
	h.key("enter") // make default: no
	h.waitConfirm()
	if _, _, ok := h.m.sh.cfg.Companies[2].FindAccount("bob"); !ok {
		t.Fatalf("accounts: %+v", h.m.sh.cfg.Companies[2].Accounts)
	}
}

func TestB7B8EmailAsUsername(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.key("a")
	h.key("enter")
	h.typeText("alice@acme.com")
	h.key("enter")
	h.mustContainPlain(tr("tui.form.err.username_email"))

	// Bitbucket and Azure DevOps are not checked
	h = newHarness(t, emptyCompanyCfg("personal"))
	h.key("a")
	h.sendKey(tea.KeyRight)
	h.sendKey(tea.KeyRight) // Bitbucket
	h.key("enter")
	h.typeText("alice@acme.com")
	h.key("enter")
	h.mustNotContain(tr("tui.form.err.username_email"))
	h.mustContainPlain(tr("tui.f.git_name"))
}

func TestB9EditingIsNotADuplicateOfItself(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	h.selectKey("personal/tinhnd")
	h.key("e")
	for i := 0; i < 6; i++ { // platform, username, name, email, auth, advanced
		h.key("enter")
		h.mustNotContain(defKM.T("tui.form.err.dup_account", "tinhnd", "github.com"))
	}
	h.waitConfirm()
}

func TestB10ExistingDuplicateIsFlagged(t *testing.T) {
	h := newHarness(t, dupCfg())
	h.mustContain("tinhnd-github")
	rows := 0
	for _, l := range h.lines() {
		if a := listArea(l, 55); strings.Contains(a, "tinhnd") && strings.Contains(a, "GH") {
			rows++
			if !strings.Contains(a, " !") {
				t.Errorf("row without the duplicate mark: %q", l)
			}
		}
	}
	if rows != 2 {
		t.Fatalf("%d account rows:\n%s", rows, h.view())
	}
	h.selectKey("personal/tinhnd-github")
	if d := h.detail(); !strings.Contains(d, plain(defKM.T("tui.detail.dup", "personal/tinhnd"))) {
		t.Fatalf("detail: %s", d)
	}
	h.selectKey("personal/tinhnd")
	if d := h.detail(); !strings.Contains(d, plain(defKM.T("tui.detail.dup", "personal/tinhnd-github"))) {
		t.Fatalf("detail: %s", d)
	}
	// stacked: the warning is in the pane too
	h.resize(80, 24)
	h.mustContainPlain(defKM.T("tui.detail.dup", "personal/tinhnd-github"))
}

func TestB11NoFalseDuplicateMark(t *testing.T) {
	h := newHarness(t, basicCfg())
	for _, l := range h.lines() {
		area := listArea(l, 55)
		if strings.Contains(area, "alice") && strings.Contains(area, " !") {
			t.Errorf("alice rows must not be flagged: %q", l)
		}
	}
	// the mark of an ssh account without a key is unchanged
	h2 := newHarness(t, sshCfg())
	found := false
	for _, l := range h2.lines() {
		if a := listArea(l, 55); strings.Contains(a, "alice") && strings.Contains(a, "ssh") {
			found = strings.Contains(a, " !")
		}
	}
	if !found {
		t.Errorf("the ssh row without a key lost its mark:\n%s", h2.view())
	}
	h.selectKey("beta/alice")
	if d := h.detail(); strings.Contains(d, "Trùng") || strings.Contains(d, "Duplicate") {
		t.Fatalf("beta/alice is no duplicate: %s", d)
	}
}

// ---- C. done screen (P0-4) ----

func TestC1DoneListsTheNextSteps(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	h.applyToDone()
	h.mustContainPlain(tr("tui.confirm.next"), tr("tui.confirm.next_clone", "~/Code/personal/"), defKM.T("tui.confirm.next_check", "~"),
		tr("tui.confirm.next_https"), tr("tui.confirm.no_verify"), tr("tui.confirm.back_hint"), tr("tui.confirm.effect"),
		tr("tui.confirm.effect_line", "~/Code/personal/", "Tinh Nguyen", "tinhnd@example.com"))
	h.mustNotContain(defKM.T("tui.confirm.next_ssh"))
}

func TestC2SSHAccountGetsTheKeyStep(t *testing.T) {
	h := newHarness(t, sshCfg())
	h.applyToDone()
	h.mustContainPlain(defKM.T("tui.confirm.next_ssh"))
	h.mustNotContain(tr("tui.confirm.next_https"))
}

func TestC3ManyCompaniesAreNotNamed(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.applyToDone()
	h.mustContainPlain(tr("tui.confirm.next_clone_many"))
}

func TestC4DoneOpensClone(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	h.applyToDone()
	h.key("c")
	if cloneOf(h) == nil {
		t.Fatalf("top is %T", h.top())
	}
	if h.depth() != 2 {
		t.Fatalf("depth %d: the done screen must be replaced, not stacked", h.depth())
	}
	h.key("esc")
	if _, ok := h.top().(*listScreen); !ok {
		t.Fatalf("top is %T", h.top())
	}
}

func TestC5DoneOpensCheck(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	h.applyToDone()
	h.key("k")
	if _, ok := h.top().(*checkScreen); !ok {
		t.Fatalf("top is %T", h.top())
	}
}

func TestC6DoneHelpBar(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	h.applyToDone()
	hb := h.helpLine()
	if !strings.HasPrefix(hb, "▸ c") || !strings.Contains(hb, "enter "+tr("tui.key.to_list")) || strings.Contains(hb, "enter "+tr("tui.key.select")) {
		t.Fatalf("help bar %q", hb)
	}
	// the empty-plan screen also leads back to the list
	h2 := newHarness(t, oneAccCfg())
	h2.apply()
	h2.key("p")
	h2.waitFor("empty", func() bool { c, ok := h2.top().(*confirmScreen); return ok && c.state == confEmpty })
	if hb := h2.helpLine(); !strings.Contains(hb, "enter "+tr("tui.key.to_list")) {
		t.Fatalf("empty help bar %q", hb)
	}
}

func TestC7DoneFitsEverySize(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	h.applyToDone()
	for _, s := range [][2]int{{60, 20}, {80, 24}, {100, 30}, {120, 40}, {40, 10}} {
		h.resize(s[0], s[1])
		assertFits(t, h.view(), s[0], s[1], "done")
		if s[0] >= 60 {
			h.mustContainPlain(tr("tui.confirm.next"), tr("tui.confirm.next_clone", "~/Code/personal/"), tr("tui.confirm.back_hint"), tr("tui.confirm.effect"))
		}
	}
}

func TestC8DoneShrinksInOrder(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.applyToDone()
	for _, s := range [][2]int{{60, 20}, {80, 24}, {100, 30}, {100, 24}, {100, 22}} {
		h.resize(s[0], s[1])
		v := plain(h.view())
		note := strings.Contains(v, plain(tr("tui.confirm.override_note")))
		noVerify := strings.Contains(v, plain(tr("tui.confirm.no_verify")))
		if note && !noVerify {
			t.Errorf("%v: the override note must go before the no-verify line", s)
		}
		if !strings.Contains(v, plain(tr("tui.confirm.back_hint"))) || !strings.Contains(v, tr("tui.confirm.next")) {
			t.Errorf("%v: the next steps and the back hint always stay:\n%s", s, h.view())
		}
	}
}

// ---- D. account detail (P0-5) and the stacked split (P0-6) ----

func TestD1AccountDetailExplainsHowToUseIt(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	h.selectKey("personal/tinhnd")
	d := h.detail()
	for _, want := range []string{tr("tui.f.username") + " tinhnd", tr("tui.f.remote"), tr("tui.f.dir") + " ~/Code/personal/",
		plain(tr("tui.detail.auth_https")), plain(tr("tui.detail.acc_default", "~/Code/personal/")), plain(defKM.T("tui.detail.how_pending"))} {
		if !strings.Contains(d, want) {
			t.Errorf("detail lacks %q:\n%s", want, d)
		}
	}
	if !strings.Contains(noSpace(d), noSpace("https://tinhnd@github.com/<owner>/<repo>.git")) {
		t.Errorf("detail lacks the remote:\n%s", d)
	}
}

func TestD2AppliedDetailSaysWhatToDoNext(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	h.selectKey("personal/tinhnd")
	h.apply()
	d := h.detail()
	if !strings.Contains(d, plain(defKM.T("tui.detail.acc_next_applied", "~/Code/personal/"))) || strings.Contains(d, plain(defKM.T("tui.detail.how_pending"))) {
		t.Fatalf("detail: %s", d)
	}
}

// The screenshot that started it: 80x24, one company, two accounts.
func TestD3StackedPaneShowsTheFullAccount(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	h.resize(80, 24)
	h.selectKey("personal/tinhnd")
	l := h.listScreen()
	if l.layout() != layoutStacked {
		t.Fatalf("layout %v", l.layout())
	}
	if _, _, _, dh := l.dims(); dh < 12 {
		t.Fatalf("the detail pane has %d rows, want at least 12", dh)
	}
	h.mustContainPlain(tr("tui.f.username")+" tinhnd", tr("tui.f.remote"), "https://tinhnd@github.com/<owner>/<repo>.git",
		tr("tui.f.dir")+" ~/Code/personal/", defKM.T("tui.detail.how_pending"))
	assertFits(t, h.view(), 80, 24, "stacked")
	// a second account, as in the report
	cfg := dupCfg()
	cfg.Companies[0].Accounts[1].Username = "other"
	h2 := newHarness(t, cfg)
	h2.resize(80, 24)
	h2.selectKey("personal/tinhnd")
	h2.mustContainPlain(tr("tui.f.remote"), tr("tui.f.dir"), defKM.T("tui.detail.how_pending"))
}

func TestD4ManyRowsKeepTheOldSplit(t *testing.T) {
	var accs []config.Account
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("user%02d", i)
		accs = append(accs, config.Account{ID: id, Platform: "github", Username: id, Name: "U " + id, Email: id + "@example.com", Auth: config.AuthHTTPS})
	}
	cfg := &config.Config{Version: 1, Companies: []config.Company{{Slug: "big", Name: "Big", BaseDir: "~/Code/big/", DefaultAccount: "user00", Accounts: accs}}}
	h := newHarness(t, cfg)
	h.resize(80, 24)
	l := h.listScreen()
	if l.layout() != layoutStacked {
		t.Fatal("not stacked")
	}
	_, lh, _, dh := l.dims()
	if dh < 7 || dh > 10 {
		t.Errorf("detail height %d, want 7..10", dh)
	}
	if rows := lh - 4; rows < 2 {
		t.Errorf("only %d list rows", rows)
	}
	h.key("down")
	h.mustContain("user00")
	assertFits(t, h.view(), 80, 24, "many rows")
}

func TestD5FilterDoesNotMoveTheSplit(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	h.resize(80, 24)
	_, lh, _, dh := h.listScreen().dims()
	h.key("/")
	h.typeText("zzz")
	if _, lh2, _, dh2 := h.listScreen().dims(); lh2 != lh || dh2 != dh {
		t.Fatalf("split moved from %d/%d to %d/%d", lh, dh, lh2, dh2)
	}
	assertFits(t, h.view(), 80, 24, "filtered")
}

func TestD6LongRemoteWrapsInsteadOfBeingCut(t *testing.T) {
	cfg := oneAccCfg()
	cfg.Companies[0].Accounts[0].Username = strings.Repeat("u", 30)
	h := newHarness(t, cfg)
	h.selectKey("personal/tinhnd")
	d := h.listScreen().detailBody(41, 26)
	want := "https://" + strings.Repeat("u", 30) + "@github.com/<owner>/<repo>.git"
	if !strings.Contains(noSpace(d), want) {
		t.Fatalf("remote cut or missing:\n%s", d)
	}
	if strings.Contains(strings.NewReplacer(":…", "", "/…", "", "@…", "").Replace(d), "…") {
		t.Fatalf("cut text in detail:\n%s", d)
	}
}

func TestD7SSHAccountDetail(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.selectKey("acme/carol")
	d := h.detail()
	if !strings.Contains(noSpace(d), noSpace("git@github-acme-carol:<owner>/<repo>.git")) ||
		!strings.Contains(noSpace(d), noSpace(tr("tui.f.alias"))) || !strings.Contains(noSpace(d), noSpace(tr("tui.key.state_missing"))) {
		t.Fatalf("ssh account: %s", d)
	}
}

func TestD8SmallPanesKeepTheRemoteAndTheNextStep(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	h.selectKey("personal/tinhnd")
	r, _ := h.listScreen().selected()
	next := plain(defKM.T("tui.detail.how_pending"))[:20]
	for n := 2; n <= 6; n++ {
		d := plain(detailText(h.m.sh, r, 56, n, h.listScreen().keyInfo, false))
		if !strings.Contains(d, next) {
			t.Errorf("height %d lost the next step: %q", n, d)
		}
		if n >= 3 && !strings.Contains(noSpace(d), "<owner>/<repo>") {
			t.Errorf("height %d lost the remote: %q", n, d)
		}
		if got := len(strings.Split(detailText(h.m.sh, r, 56, n, h.listScreen().keyInfo, false), "\n")); got > n {
			t.Errorf("height %d: %d lines", n, got)
		}
	}
}

func TestD9DetailScreenOpensClone(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	h.resize(50, 20)
	h.selectKey("personal/tinhnd")
	h.key("enter")
	if _, ok := h.top().(*detailScreen); !ok {
		t.Fatalf("top is %T", h.top())
	}
	h.key("c")
	if cloneOf(h) == nil {
		t.Fatalf("top is %T", h.top())
	}
	h.key("esc")
	if _, ok := h.top().(*detailScreen); !ok {
		t.Fatalf("top is %T", h.top())
	}
	hasC := false
	for _, b := range h.top().Help() {
		hasC = hasC || b.Help().Key == "c"
	}
	if !hasC {
		t.Fatal("c is not listed in the help of the detail screen")
	}
}

// ---- E. stale configuration guard (P1-7) ----

// writeBehindTheBack saves bob next to the existing accounts, like another
// gitid window would, and returns the configuration it saved.
func writeBehindTheBack(t *testing.T, h *harness) config.Config {
	t.Helper()
	other := cloneConfig(h.m.sh.cfg)
	bob := personalAccount("bob")
	bob.Username, bob.Email = "bob", "bob@example.com"
	other.Companies[0].Accounts = append(other.Companies[0].Accounts, bob)
	saved, _, err := h.svc.Commit(other)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func accountIDs(c config.Config) string {
	var ids []string
	for _, a := range c.Companies[0].Accounts {
		ids = append(ids, a.ID)
	}
	return strings.Join(ids, ",")
}

func TestE1StaleWindowCannotOverwrite(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	writeBehindTheBack(t, h)
	h.key("a")
	h.key("enter")
	h.typeText("carol")
	h.key("enter")
	h.key("enter") // name (prefilled)
	h.typeText("carol@example.com")
	h.key("enter")
	h.key("enter") // auth
	h.key("enter") // advanced: no
	h.key("enter") // make default: no -> save
	h.waitFor("refused", func() bool { return strings.Contains(plain(h.view()), plain(tr("tui.err.config_changed"))) })
	disk, err := h.svc.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := accountIDs(disk); got != "tinhnd,bob" {
		t.Fatalf("disk has %s: the other window's account must survive and carol must not be written", got)
	}
	h.waitFor("list reloaded", func() bool { return accountIDs(h.m.sh.cfg) == "tinhnd,bob" })
	f, ok := h.top().(*accountForm)
	if !ok || f.username != "carol" {
		t.Fatalf("the form must stay open with its values: %T", h.top())
	}
	// saving again, on top of the fresh configuration, works
	for i := 0; i < 7; i++ { // platform, username, name, email, auth, advanced, default
		h.key("enter")
	}
	h.waitConfirm()
	disk, _ = h.svc.Load()
	if got := accountIDs(disk); got != "tinhnd,bob,carol" {
		t.Fatalf("disk has %s", got)
	}
}

func TestE2NoFalsePositiveAcrossManySaves(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	notStale := func(step string) {
		t.Helper()
		if strings.Contains(plain(h.view()), plain(tr("tui.err.config_changed"))) {
			t.Fatalf("%s: false stale report:\n%s", step, h.view())
		}
	}
	back := func() { // leave the confirm screen without applying
		h.waitReady()
		h.key("n")
		if _, ok := h.top().(*listScreen); !ok {
			t.Fatalf("top is %T", h.top())
		}
	}
	// 1. new company
	h.key("A")
	h.typeText("x")
	h.key("enter")
	h.typeText("X Corp")
	h.key("enter")
	h.key("enter") // base dir: default
	h.waitFor("company created", func() bool {
		_, ok := h.top().(*listScreen)
		return ok && h.depth() == 1 && h.listScreen().currentKey() == "x"
	})
	notStale("add company")
	// 2. new account
	h.key("a")
	fillAccount(h, "alice", "Alice", "alice@example.com")
	h.waitConfirm()
	notStale("add account")
	back()
	if _, _, ok := h.m.sh.cfg.Companies[1].FindAccount("alice"); !ok {
		t.Fatalf("accounts: %+v", h.m.sh.cfg.Companies[1])
	}
	// 3. edit the account
	h.selectKey("x/alice")
	h.key("e")
	for i := 0; i < 6; i++ {
		h.key("enter")
	}
	h.waitConfirm()
	notStale("edit account")
	back()
	// 4. edit the company
	h.selectKey("x")
	h.key("e")
	h.key("enter")
	h.key("enter")
	h.waitConfirm()
	notStale("edit company")
	back()
	// 5. delete the account
	h.selectKey("x/alice")
	h.key("d")
	h.key("y")
	h.waitConfirm()
	notStale("delete account")
	if _, changed, err := h.svc.ChangedSince(h.m.sh.cfg); err != nil || changed {
		t.Fatalf("the TUI's view of the config differs from the disk: changed=%v err=%v", changed, err)
	}
}

func TestE3DeleteIsRefusedWhenStale(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	writeBehindTheBack(t, h)
	h.selectKey("personal/tinhnd")
	h.key("d")
	h.key("y")
	h.waitFor("refused", func() bool { return strings.Contains(plain(h.view()), plain(tr("tui.err.config_changed"))) })
	disk, _ := h.svc.Load()
	if got := accountIDs(disk); got != "tinhnd,bob" {
		t.Fatalf("disk has %s", got)
	}
	h.waitFor("reloaded", func() bool { return accountIDs(h.m.sh.cfg) == "tinhnd,bob" })
}

func TestE4ImportIsRefusedWhenStale(t *testing.T) {
	h := newHarness(t, nil)
	testutil.WriteFile(t, h.e.Paths().GitConfig, "[user]\n\tname = x\n[includeIf \"gitdir:~/Code/work/initech-solutions/\"]\n\tpath = ~/.gitconfig-initech\n", 0o644)
	testutil.WriteFile(t, filepath.Join(h.e.Home, ".gitconfig-initech"), "[user]\n\tname = Dana Initech\n\temail = dana@initech.example\n", 0o644)
	h.key("i")
	h.waitFor("proposals", func() bool { return h.top().(*importScreen).phase == imList })
	h.key("enter")
	// another window saves a company while the form is open
	other := config.Default()
	other.Companies = []config.Company{{Slug: "other", Name: "Other", BaseDir: "~/Code/other/"}}
	if _, _, err := h.svc.Commit(other); err != nil {
		t.Fatal(err)
	}
	h.key("enter") // platform
	h.typeText("dana")
	h.key("enter")
	h.key("enter") // auth
	h.key("enter") // host -> save
	h.waitFor("refused", func() bool { return strings.Contains(plain(h.view()), plain(tr("tui.err.config_changed"))) })
	disk, _ := h.svc.Load()
	if len(disk.Companies) != 1 || disk.Companies[0].Slug != "other" {
		t.Fatalf("disk: %+v", disk.Companies)
	}
	h.waitFor("reloaded", func() bool { _, _, ok := h.m.sh.cfg.FindCompany("other"); return ok })
}

func TestE6KeygenDoesNotOverwriteAStaleConfig(t *testing.T) {
	h := newHarness(t, basicCfg())
	ref, _ := h.m.sh.cfg.ParseRef("acme/carol")
	keyPath := h.e.Expand(ref.Company.DefaultKeyPath(ref.Account))
	h.fake.On("ssh-keygen", execx.Response{Do: func(execx.Cmd) error {
		testutil.WriteFile(t, keyPath, "PRIVATE", 0o000) // a fake key file, never a real one
		testutil.WriteFile(t, keyPath+".pub", "ssh-ed25519 AAAAC3 acme/carol\n", 0o644)
		return nil
	}})
	h.selectKey("acme/carol")
	h.key("g")
	k := h.top().(*keygenScreen)
	h.key("enter") // with a passphrase: ssh-keygen "runs"
	other := cloneConfig(h.m.sh.cfg)
	other.Companies = append(other.Companies, config.Company{Slug: "zed", Name: "Zed", BaseDir: "~/Code/zed/"})
	if _, _, err := h.svc.Commit(other); err != nil {
		t.Fatal(err)
	}
	h.send(keyExecDoneMsg{id: k.id})
	h.waitFor("refused", func() bool { return k.phase == kgError })
	h.mustContainPlain(tr("tui.err.config_changed"))
	h.waitFor("reloaded", func() bool { _, _, ok := h.m.sh.cfg.FindCompany("zed"); return ok })
	disk, _ := h.svc.Load()
	if _, _, ok := disk.FindCompany("zed"); !ok || len(disk.Companies) != 4 {
		t.Fatalf("the other window's company must survive: %+v", disk.Companies)
	}
}

func TestE5FirstRunSavesWithoutAConfigFile(t *testing.T) {
	h := newHarness(t, nil)
	h.key("A")
	h.typeText("x")
	h.key("enter")
	h.typeText("X Corp")
	h.key("enter")
	h.key("enter")
	h.waitFor("saved", func() bool { c, err := h.svc.Load(); return err == nil && len(c.Companies) == 1 })
	h.mustNotContain(tr("tui.err.config_changed"))
}

func TestE7ErrTextMapsTheStaleError(t *testing.T) {
	if got := errText(app.ErrConfigChanged); got != tr("tui.err.config_changed") {
		t.Fatalf("got %q", got)
	}
}

// ---- F. wrong username (P1-8) ----

func TestF1ChangingTheUsernameIsGuided(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	h.selectKey("personal/tinhnd")
	h.key("e")
	h.key("enter") // platform
	for i := 0; i < len("tinhnd"); i++ {
		h.update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	h.typeText("tinhnd1902")
	h.mustContainPlain(defKM.T("tui.form.warn.username_changed", "tinhnd"), tr("tui.form.preview_remote", "https://tinhnd1902@github.com/<owner>/<repo>.git"))
	for i := 0; i < 5; i++ { // username, name, email, auth, advanced
		h.key("enter")
	}
	h.waitConfirm()
	if !strings.Contains(h.m.status.text, tr("tui.status.account_saved_relogin", "personal/tinhnd", "tinhnd")) {
		t.Fatalf("status %q", h.m.status.text)
	}
	if a, _, ok := h.m.sh.cfg.Companies[0].FindAccount("tinhnd"); !ok || a.Username != "tinhnd1902" {
		t.Fatalf("the id stays, the username changes: %+v", h.m.sh.cfg.Companies[0].Accounts)
	}
}

func TestF2KeepingTheUsernameIsQuiet(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	h.selectKey("personal/tinhnd")
	h.key("e")
	h.mustNotContain(defKM.T("tui.form.warn.username_changed", "tinhnd"))
	for i := 0; i < 6; i++ {
		h.key("enter")
	}
	h.waitConfirm()
	if !strings.Contains(h.m.status.text, defKM.T("tui.status.account_saved", "personal/tinhnd")) {
		t.Fatalf("status %q", h.m.status.text)
	}
}

// ---- G. check on a folder that is not a repository (P1-9) ----

func TestG1CheckOnANonRepoExplainsItself(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	h.fake.On("git -C "+h.e.Home+" rev-parse --show-toplevel", execx.Response{ExitCode: 128})
	h.key("k")
	h.waitFor("error", func() bool { c, ok := h.top().(*checkScreen); return ok && c.phase == chError })
	h.mustContainPlain(tr("err.not_repo"), tr("tui.check.where", "~"), defKM.T("tui.check.not_repo_next", "~/Code/personal/"))
	if hb := h.helpLine(); !strings.HasPrefix(hb, "▸ c") || !strings.Contains(hb, "r "+tr("tui.key.retry")) {
		t.Fatalf("help bar %q", hb)
	}
	h.key("c")
	if cloneOf(h) == nil {
		t.Fatalf("top is %T", h.top())
	}
	if h.depth() != 2 {
		t.Fatalf("depth %d", h.depth())
	}
}

func TestG3CheckOnANonRepoWithoutCompanies(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.On("git -C "+h.e.Home+" rev-parse --show-toplevel", execx.Response{ExitCode: 128})
	h.key("k")
	h.waitFor("error", func() bool { c, ok := h.top().(*checkScreen); return ok && c.phase == chError })
	h.mustContainPlain(defKM.T("tui.check.not_repo_next_none"))
}

func TestG4OtherCheckErrorsAreUnchanged(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	h.fake.On("git -C "+h.e.Home+" rev-parse --show-toplevel", execx.Response{ExitCode: 128})
	h.key("k")
	h.waitFor("error", func() bool { c, ok := h.top().(*checkScreen); return ok && c.phase == chError })
	c := h.top().(*checkScreen)
	c.err = app.ErrNoRemote // any other error keeps the retry hint and no clone shortcut
	h.mustContainPlain(tr("err.no_remote"), defKM.T("tui.check.retry_hint"))
	h.mustNotContain(tr("tui.check.where", "~"))
	if hb := h.helpLine(); strings.HasPrefix(hb, "▸ c") {
		t.Fatalf("help bar %q", hb)
	}
	h.key("c")
	if _, ok := h.top().(*checkScreen); !ok {
		t.Fatalf("c must do nothing here, top is %T", h.top())
	}
}

// ---- I. advanced settings are opt-in (P2-11) ----

func TestI1FirstAccountNeedsNoAdvancedAnswers(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.key("a")
	fillAccount(h, "dora", "Dora D", "dora@example.com")
	h.waitConfirm() // no host, no orgs, no ssh fields, no make-default question
	if a, _, ok := h.m.sh.cfg.Companies[0].FindAccount("dora"); !ok || h.m.sh.cfg.Companies[0].DefaultAccount != a.ID {
		t.Fatalf("company: %+v", h.m.sh.cfg.Companies[0])
	}
}

func TestI2OtherAccountsMakeDefaultAQuestion(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("a")
	fillAccount(h, "dora", "", "dora@acme.com")
	h.mustContainPlain(tr("tui.f.make_default"))
	h.key("enter")
	h.waitConfirm()
	if c := h.m.sh.cfg.Companies[0]; c.DefaultAccount != "alice" {
		t.Fatalf("default stays alice, got %q", c.DefaultAccount)
	}
}

func TestI3OptingInShowsTheAdvancedFields(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.key("a")
	h.key("enter")
	h.typeText("dora")
	h.key("enter")
	h.typeText("Dora D")
	h.key("enter")
	h.typeText("dora@example.com")
	h.key("enter")
	h.key("enter") // auth
	h.mustContainPlain(tr("tui.form.show_advanced"), tr("tui.form.advanced_hint"))
	h.mustNotContain(tr("tui.f.host"))
	h.key("y")
	h.mustContainPlain(tr("tui.f.host"), tr("tui.form.eg", "git.acme.com"))
	h.mustNotContain("github.com, gitlab.com")
}

func TestI4GiteaAlwaysAsksForTheHost(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.key("a")
	for i := 0; i < 3; i++ {
		h.sendKey(tea.KeyRight)
	}
	h.key("enter")
	h.typeText("sam")
	h.key("enter")
	h.typeText("Sam S")
	h.key("enter")
	h.typeText("sam@example.com")
	h.key("enter")
	h.key("enter") // auth
	h.key("enter") // advanced: left on its default, Gitea shows the fields anyway
	h.mustContainPlain(tr("tui.f.host"))
	h.key("enter")
	h.mustContainPlain(tr(config.ErrHostRequired))
}

func TestI5EditingCustomizedAccountShowsTheAdvancedFields(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.selectKey("globex/dave")
	h.key("e")
	if f := h.top().(*accountForm); !f.showAdvanced {
		t.Fatal("advanced must default to yes for a customized account")
	}
	for i := 0; i < 6; i++ { // platform, username, name, email, auth, advanced
		h.key("enter")
	}
	h.mustContainPlain(tr("tui.f.host"), "git.example.com", tr("tui.f.ssh_port"), "2222")
	// a plain account opts out by default
	h.key("esc")
	h.selectKey("acme/bob")
	h.key("e")
	if f := h.top().(*accountForm); f.showAdvanced {
		t.Fatal("advanced must default to no for a plain account")
	}
}

// ---- J. small fixes (P2-12) ----

func TestJ1SelectedRowKeepsTheIDWhenItIsTight(t *testing.T) {
	h := newHarness(t, nil)
	acc := config.Account{ID: "alice", Platform: platform.GitHub, Username: "alice", Name: "A", Email: "1234+alice@users.noreply.github.com", Auth: config.AuthHTTPS}
	r := rowItem{company: config.Company{Slug: "x", DefaultAccount: "alice"}, account: &acc, last: true}
	d := rowDelegate{h.m.sh}
	for _, w := range []int{30, 40, 50, 51, 60, 80} {
		for _, sel := range []bool{true, false} {
			line := plain(d.line(r, w, sel, 0))
			if !strings.Contains(line, "alice ") && !strings.Contains(line, "alice\t") {
				t.Errorf("w=%d selected=%v: id cut: %q", w, sel, line)
			}
			if got := len([]rune(d.line(r, w, sel, 0))); got > w {
				t.Errorf("w=%d selected=%v: %d cells", w, sel, got)
			}
		}
	}
	// when the email fits, the selected row shows it too
	if line := plain(d.line(r, 90, true, 0)); !strings.Contains(line, acc.Email) {
		t.Errorf("email missing on a wide row: %q", line)
	}
	// the row keeps its shape: the email goes for both states at the same width
	for w := 30; w < 90; w++ {
		sel := strings.Contains(plain(d.line(r, w, true, 0)), acc.Email)
		unsel := strings.Contains(plain(d.line(r, w, false, 0)), acc.Email)
		if sel != unsel {
			t.Errorf("w=%d: email shown selected=%v unselected=%v", w, sel, unsel)
		}
	}
}

func TestJ3ClonePlaceholderIsAnExample(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("c")
	h.mustContain(tr("tui.form.eg", "https://github.com/org/repo.git"))
}

func TestJ5ImportWithNothingFoundSaysWhatToDo(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("i")
	h.waitFor("loaded", func() bool { return h.top().(*importScreen).phase == imList })
	h.mustContainPlain(defKM.T("tui.import.none_next"))
	hb := h.helpLine()
	if !strings.Contains(hb, "esc") || strings.Contains(hb, "enter") || strings.Contains(hb, "↑") {
		t.Fatalf("help bar %q", hb)
	}
}

func TestJ6HelpOverlayAlwaysShowsTheGlobalKeys(t *testing.T) {
	for _, s := range [][2]int{{80, 24}, {60, 20}, {100, 30}, {120, 40}} {
		h := newHarness(t, basicCfg())
		h.resize(s[0], s[1])
		h.key("?")
		h.mustContainPlain(tr("tui.help.global"), "ctrl+c", defKM.T("tui.help.close"))
		for _, b := range h.top().Help() {
			if !strings.Contains(h.view(), b.Help().Key) {
				t.Errorf("%v: key %q missing from the help overlay:\n%s", s, b.Help().Key, h.view())
			}
		}
		assertFits(t, h.view(), s[0], s[1], "help")
	}
}

func TestJ7ResizingAnOpenFormRewrapsTheHints(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.key("a")
	h.update(tea.WindowSizeMsg{Width: 80, Height: 24}) // no key follows
	h.mustContainSpaceless(tr("tui.form.username_hint"))
	assertFits(t, h.view(), 80, 24, "narrowed form")
	h.update(tea.WindowSizeMsg{Width: 120, Height: 40})
	h.mustContainSpaceless(tr("tui.form.username_hint"))
	assertFits(t, h.view(), 120, 40, "widened form")
}

// ---- K. English ----

func TestKEnglishPolish(t *testing.T) {
	defer i18n.SetLang("vi")
	i18n.SetLang("en")
	h := newHarness(t, oneAccCfg())
	h.key("a")
	h.key("enter")
	h.typeText("tinhnd")
	h.mustContainPlain(defKM.T("tui.form.err.dup_account", "tinhnd", "github.com"))
	h.key("esc")
	h.key("y") // discard what was typed
	h.applyToDone()
	h.mustContainPlain("Next steps:", "Personal Access Token", "gitid never signs in")
	for _, key := range []string{"tui.confirm.next", "tui.confirm.next_https", "tui.form.preview_remote", "tui.detail.acc_next_applied"} {
		if i18n.In("en", key) == i18n.In("vi", key) {
			t.Errorf("%s is not translated", key)
		}
	}
	v := h.view()
	for _, vi := range []string{"Bước tiếp theo", "Từ giờ", "Sao lưu", "Clone repo vào"} {
		if strings.Contains(v, vi) {
			t.Errorf("Vietnamese text %q in the English view", vi)
		}
	}
}

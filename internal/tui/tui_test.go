package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/doctor"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/testutil"
)

// T1: the list renders company and account rows.
func TestT1ListRendersRows(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.mustContain("acme", "alice", "bob", "carol", "globex", "beta", "GH", "GL", "https", "ssh", "alice@beta.io", "★")
	h.mustContain(tr("tui.list.title"), tr("tui.list.filter_hint"))
	if h.depth() != 1 {
		t.Fatalf("depth %d", h.depth())
	}
}

func TestListEmptyState(t *testing.T) {
	h := newHarness(t, nil)
	h.mustContain(tr("tui.empty.title"))
	h.key("a") // no company yet
	h.mustContain(defKM.T("tui.status.need_company"))
	if h.depth() != 1 {
		t.Fatal("must not open a form without companies")
	}
}

// T2: the fuzzy filter keeps only matching rows; esc clears it.
func TestT2Filter(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("/")
	h.typeText("beta")
	// filtering runs as a command and its result arrives as a message: wait
	// for it instead of assuming it already did
	h.waitFor("filter applied", func() bool { return !strings.Contains(h.view(), "globex") })
	v := h.view()
	if !strings.Contains(v, "beta") || strings.Contains(v, "acme") || strings.Contains(v, "globex") {
		t.Fatalf("filter result:\n%s", v)
	}
	h.key("esc")
	h.waitFor("filter cleared", func() bool { return strings.Contains(h.view(), "globex") })
	h.mustContain("acme", "globex", "beta")
	// applied filter: enter accepts it, then esc clears it
	h.key("/")
	h.typeText("carol")
	h.waitFor("carol filter", func() bool { return !strings.Contains(h.view(), "bob@acme.com") })
	h.key("enter")
	h.mustContain("carol")
	h.mustNotContain("bob@acme.com")
	h.key("esc")
	h.waitFor("filter cleared", func() bool { return strings.Contains(h.view(), "bob@acme.com") })
	h.mustContain("bob@acme.com")
}

func TestTreeFilterMatchesWithinWords(t *testing.T) {
	targets := []string{"acme/bob acme bob@acme.com github", "beta alice alice@beta.io"}
	if got := treeFilter("beta", targets); len(got) != 1 || got[0].Index != 1 {
		t.Fatalf("beta: %+v", got)
	}
	if got := treeFilter("bob github", targets); len(got) != 1 || got[0].Index != 0 {
		t.Fatalf("two words: %+v", got)
	}
	if got := treeFilter("   ", targets); got != nil {
		t.Fatal("blank term")
	}
	if got := treeFilter("al", targets); len(got) != 1 {
		t.Fatalf("fuzzy prefix: %+v", got)
	}
	if got := treeFilter("a", targets); len(got) != 2 || got[0].Index != 0 || got[1].Index != 1 {
		t.Fatalf("order must follow the tree: %+v", got)
	}
}

// T3: the company form validates and saves. A new company has no account, so
// there is nothing to apply: the form returns to the list with the new
// company selected and a hint (it used to open an empty confirm screen that
// looked like "nothing to do"). Editing a company still opens the confirm
// screen (see T5).
func TestT3CompanyForm(t *testing.T) {
	h := newHarness(t, basicCfg())
	pendingBefore := h.m.sh.pending
	h.key("A")
	h.mustContain(tr("tui.form.company_new"), tr("tui.f.slug"))
	h.typeText("Bad Slug")
	h.key("enter")
	h.mustContain(tr(config.ErrSlugInvalid))
	if _, _, ok := h.m.sh.cfg.FindCompany("zeta"); ok {
		t.Fatal("must not save")
	}
	// fix the slug: clear the field and type a valid one
	for range "Bad Slug" {
		h.update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	h.drain()
	h.typeText("zeta")
	h.key("enter")
	h.typeText("Zeta Inc")
	h.key("enter")
	h.key("enter") // empty base dir -> default
	h.waitFor("back on the list", func() bool {
		l, ok := h.top().(*listScreen)
		return ok && h.depth() == 1 && l.currentKey() == "zeta"
	})
	c, _, ok := h.m.sh.cfg.FindCompany("zeta")
	if !ok || c.Name != "Zeta Inc" || c.BaseDir != "~/Code/zeta/" {
		t.Fatalf("saved company %+v ok=%v", c, ok)
	}
	if !strings.Contains(h.m.status.text, defKM.T("tui.status.company_created", "zeta")) {
		t.Fatalf("status %q", h.m.status.text)
	}
	// the new company changes nothing on disk, so the plan stays the same
	h.waitFor("replanned", func() bool { return h.m.sh.planned })
	if h.m.sh.pending != pendingBefore {
		t.Fatalf("pending %d, was %d", h.m.sh.pending, pendingBefore)
	}
}

func TestT3DuplicateSlug(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("A")
	h.typeText("acme")
	h.key("enter")
	h.mustContain(tr("tui.form.err.slug_taken", "acme"))
}

// T4: n cancels (nothing written), y applies and shows the backup.
func TestT4ConfirmApplyAndCancel(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("p")
	h.waitFor("diff ready", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confReady })
	before := testutil.Snapshot(t, h.e.Home)
	h.key("n")
	if h.depth() != 1 {
		t.Fatal("n must pop")
	}
	testutil.AssertSame(t, before, testutil.Snapshot(t, h.e.Home))

	h.key("p")
	h.waitFor("diff ready", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confReady })
	h.key("y")
	h.waitFor("apply done", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confDone })
	h.mustContain(tr("tui.confirm.done"), tr("tui.confirm.backup"))
	if _, err := os.Stat(h.e.Paths().GitConfig); err != nil {
		t.Fatal("gitconfig must exist after apply:", err)
	}
	h.waitFor("badge synced", func() bool { return h.m.sh.planned && h.m.sh.pending == 0 })
	h.mustContain(tr("tui.status.synced"))
	h.key("enter")
	if h.depth() != 1 {
		t.Fatal("enter returns to the list")
	}
}

func TestConfirmEmptyAndRetry(t *testing.T) {
	h := newHarness(t, nil)
	h.key("p")
	h.waitFor("empty", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confEmpty })
	h.mustContain(tr("tui.confirm.empty"))
	h.key("enter")
	if h.depth() != 1 {
		t.Fatal("enter pops")
	}
	// a failed plan can be retried
	c := newConfirmScreen(h.m.sh, "x")
	c.Resize(80, 20)
	c.Update(confirmPlanMsg{id: c.id, err: os.ErrPermission})
	if c.state != confFailed || !strings.Contains(c.View(), defKM.T("tui.confirm.retry_hint")) {
		t.Fatal("failed state")
	}
	// results of another run are ignored
	c.Update(confirmPlanMsg{id: c.id + 99})
	if c.state != confFailed {
		t.Fatal("foreign id must be ignored")
	}
}

func TestConfirmStaleShowsError(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("p")
	h.waitFor("diff ready", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confReady })
	// the user edits ~/.gitconfig behind our back
	testutil.WriteFile(t, h.e.Paths().GitConfig, "[user]\n\tname = x\n", 0o644)
	h.key("y")
	h.waitFor("failed", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confFailed })
	h.mustContain("✗")
	if _, err := os.Stat(h.e.Paths().BackupsDir); err == nil {
		t.Fatal("a stale apply must not create a backup")
	}
}

// T5: slug and id are read-only in the edit forms.
func TestT5EditCompanyKeepsSlug(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("e") // acme (company row)
	h.mustContain(tr("tui.form.company_edit"), "acme", tr("tui.form.immutable"))
	h.typeText("zzz") // the slug is a note without an input: the first editable field is the name
	h.mustContain("ACME Corpzzz")
	for i := 0; i < 2; i++ {
		h.key("enter")
	}
	h.waitFor("confirm", func() bool { _, ok := h.top().(*confirmScreen); return ok })
	c, _, ok := h.m.sh.cfg.FindCompany("acme")
	if !ok || c.Name != "ACME Corpzzz" || len(c.Accounts) != 3 {
		t.Fatalf("company after edit: %+v", c)
	}
	if _, _, dup := h.m.sh.cfg.FindCompany("acmezzz"); dup {
		t.Fatal("slug must not change")
	}
}

func TestEditAccountKeepsID(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("down") // acme/alice
	h.key("e")
	h.mustContain(tr("tui.form.account_edit", "acme/alice"), tr("tui.f.id"), tr("tui.form.immutable"))
	f := h.top().(*accountForm)
	if f.edit == nil || f.username != "alice" {
		t.Fatalf("prefill %+v", f)
	}
	// enter through: platform, username, name, email, auth, advanced (no) | default (only with other accounts)
	for i := 0; i < 12; i++ {
		h.key("enter")
		if _, ok := h.top().(*confirmScreen); ok {
			break
		}
	}
	h.waitFor("confirm", func() bool { _, ok := h.top().(*confirmScreen); return ok })
	ref, err := h.m.sh.cfg.ParseRef("acme/alice")
	if err != nil || ref.Account.Email != "alice@acme.com" {
		t.Fatalf("account after edit: %+v %v", ref, err)
	}
}

func TestAddAccountFlow(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("a") // acme is selected
	h.mustContain(tr("tui.form.account_new", "acme"))
	h.key("enter") // platform: github
	h.typeText("dora")
	h.key("enter")
	h.key("enter") // name prefilled from the company's first account
	h.typeText("dora@acme.com")
	h.key("enter")
	h.key("enter") // auth: https
	for i := 0; i < 8; i++ {
		h.key("enter")
		if _, ok := h.top().(*confirmScreen); ok {
			break
		}
	}
	h.waitFor("confirm", func() bool { _, ok := h.top().(*confirmScreen); return ok })
	ref, err := h.m.sh.cfg.ParseRef("acme/dora")
	if err != nil || ref.Account.Email != "dora@acme.com" || ref.Account.Auth != config.AuthHTTPS || ref.Account.Name != "Alice Nguyen" && ref.Account.Name == "" {
		t.Fatalf("new account: %+v %v", ref, err)
	}
}

func TestAddAccountValidation(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("a")
	h.key("enter") // platform
	h.typeText("bad name")
	h.key("enter")
	h.mustContain(tr(config.ErrUsernameInvalid))
}

// T6: deleting an account asks twice; the key survives by default.
func TestT6DeleteAccountKeepsKey(t *testing.T) {
	cfg := basicCfg()
	h := newHarness(t, cfg)
	ref, _ := h.m.sh.cfg.ParseRef("acme/carol")
	keyPath := h.e.Expand(ref.Company.DefaultKeyPath(ref.Account))
	testutil.WriteFile(t, keyPath, "PRIVATE", 0o600)
	testutil.WriteFile(t, keyPath+".pub", "ssh-ed25519 AAAA carol\n", 0o644)
	// record the key path in the configuration, as key generation does
	next := h.m.sh.cfg
	for ci := range next.Companies {
		for ai := range next.Companies[ci].Accounts {
			if next.Companies[ci].Accounts[ai].ID == "carol" {
				next.Companies[ci].Accounts[ai].SSHKey = ref.Company.DefaultKeyPath(ref.Account)
			}
		}
	}
	saved, _, err := h.svc.Commit(next)
	if err != nil {
		t.Fatal(err)
	}
	h.send(cfgChangedMsg{cfg: saved})

	for range []int{1, 2, 3} {
		h.key("down") // acme, alice, bob, carol
	}
	h.key("d")
	h.mustContain(tr("tui.delete.q_account", "acme/carol"), tr("tui.delete.no"))
	// enter on the default (No) must not delete anything
	h.key("enter")
	if h.depth() != 1 {
		t.Fatal("enter on the default must cancel")
	}
	if _, err := h.m.sh.cfg.ParseRef("acme/carol"); err != nil {
		t.Fatal("account must still exist")
	}

	h.key("d")
	h.key("y")
	h.mustContain(tr("tui.delete.key_q"), tr("tui.delete.keep_key"))
	// a wrong word is refused
	h.typeText("nope")
	h.key("enter")
	h.mustContain(tr("tui.delete.mismatch", "carol"))
	if _, err := h.m.sh.cfg.ParseRef("acme/carol"); err != nil {
		t.Fatal("account must still exist after a mismatch")
	}
	for range "nope" {
		h.update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	h.key("enter") // empty = keep the key
	h.waitFor("confirm", func() bool { _, ok := h.top().(*confirmScreen); return ok })
	if _, err := h.m.sh.cfg.ParseRef("acme/carol"); err == nil {
		t.Fatal("account must be gone")
	}
	for _, p := range []string{keyPath, keyPath + ".pub"} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("key file %s must survive: %v", p, err)
		}
	}
}

func TestDeleteAccountTypedKeyRemoval(t *testing.T) {
	h := newHarness(t, basicCfg())
	ref, _ := h.m.sh.cfg.ParseRef("acme/carol")
	keyPath := h.e.Expand(ref.Company.DefaultKeyPath(ref.Account))
	testutil.WriteFile(t, keyPath, "PRIVATE", 0o600)
	testutil.WriteFile(t, keyPath+".pub", "ssh-ed25519 AAAA carol\n", 0o644)
	ref.Account.SSHKey = ref.Company.DefaultKeyPath(ref.Account)
	d := newDeleteScreen(h.m.sh, ref.Company, &ref.Account)
	h.send(pushMsg{d})
	h.key("y")
	h.typeText("carol")
	h.key("enter")
	h.waitFor("confirm", func() bool { _, ok := h.top().(*confirmScreen); return ok })
	for _, p := range []string{keyPath, keyPath + ".pub"} {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("key %s must be deleted after typing the id", p)
		}
	}
}

func TestDeleteCompanyAndEsc(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("down") // acme
	h.key("up")
	h.key("d")
	h.mustContain(tr("tui.delete.q_company", "acme", 3))
	h.key("n")
	if h.depth() != 1 {
		t.Fatal("n cancels")
	}
	h.key("d")
	h.key("tab") // move to "yes"
	h.key("enter")
	h.waitFor("confirm", func() bool { _, ok := h.top().(*confirmScreen); return ok })
	if _, _, ok := h.m.sh.cfg.FindCompany("acme"); ok {
		t.Fatal("company must be deleted")
	}
}

// T7: the clone flow.
func TestT7CloneFlow(t *testing.T) {
	h := newHarness(t, basicCfg())
	release := make(chan struct{})
	h.fake.On("git clone", execx.Response{Block: release, ExitCode: 128, Stderr: "fatal: Authentication failed for 'x'\n"})
	h.key("c")
	h.mustContain(tr("tui.clone.paste"))
	h.typeText("https://github.com/org/repo.git")
	h.key("enter")
	cs := cloneOf(h)
	if cs.phase != clPick || len(cs.cands) != 5 {
		t.Fatalf("phase %v cands %d", cs.phase, len(cs.cands))
	}
	for i := 0; i < 4; i++ { // github accounts come first
		if cs.cands[i].Account.Platform != "github" && i < 4 {
			// 4 github accounts: alice, bob, carol, beta/alice
			t.Fatalf("candidate %d is %s", i, cs.cands[i])
		}
	}
	if cs.cands[4].Account.Platform != "gitlab" {
		t.Fatalf("last candidate %s", cs.cands[4])
	}
	h.mustContain("https://alice@github.com/org/repo.git", "~/Code/acme/repo", tr("tui.f.clone_url"))
	// the selection changes the preview
	h.key("down")
	h.mustContain("https://bob@github.com/org/repo.git")
	h.key("up")

	h.key("enter")
	if cs.phase != clRunning {
		t.Fatalf("phase %v", cs.phase)
	}
	h.send(cloneProgressMsg{id: cs.id, Phase: "Receiving objects", Pct: 45})
	h.mustContain("45%", tr("tui.clone.phase.receiving"))
	close(release)
	h.waitFor("failed", func() bool { return cs.phase == clFailed })
	h.mustContain(tr("tui.clone.terminal_mode"), tr("err.auth"))
	h.mustContainPlain(defKM.T("tui.clone.auth_hint")) // mentions Personal Access Tokens for HTTPS
	if !strings.Contains(i18n.In("vi", "tui.clone.auth_hint"), "Personal Access Token") || !strings.Contains(i18n.In("en", "tui.clone.auth_hint"), "Personal Access Token") {
		t.Fatal("the clone hint must mention Personal Access Tokens")
	}
	// terminal mode is offered with t
	h.fake.On("git clone", execx.Response{})
	h.key("t")
	if cs.phase != clRunning {
		t.Fatalf("terminal mode phase %v", cs.phase)
	}
	if h.fake.Count("git clone") < 2 {
		t.Fatal("terminal mode must prepare git clone")
	}
}

func TestCloneDoneVerifiesEmail(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.fake.On("git clone", execx.Response{Stderr: "Receiving objects:  100% (1/1)\n"})
	h.fake.On("git -C", execx.Response{Stdout: "file:/x/.git/config\talice@acme.com\n"})
	h.key("c")
	h.typeText("https://github.com/org/repo.git")
	h.key("enter")
	h.key("enter")
	cs := cloneOf(h)
	h.waitFor("done", func() bool { return cs.phase == clDone })
	h.mustContain(tr("tui.clone.done"), tr("tui.clone.email_ok", "alice@acme.com"))
	h.key("enter")
	if h.depth() != 1 {
		t.Fatal("enter pops")
	}
}

func TestCloneEscCancelsAndInputErrors(t *testing.T) {
	h := newHarness(t, basicCfg())
	release := make(chan struct{})
	h.fake.On("git clone", execx.Response{Block: release})
	h.key("c")
	h.key("enter") // empty URL: ignored
	if cloneOf(h).phase != clURL {
		t.Fatal("empty URL must be ignored")
	}
	h.typeText("-oProxyCommand=x")
	h.key("enter")
	cs := cloneOf(h)
	if cs.phase != clPick || cs.prevErr == nil {
		t.Fatalf("a bad URL must show an error in the preview: %v %v", cs.phase, cs.prevErr)
	}
	h.mustContain("✗")
	h.key("enter") // refused: nothing to clone
	if cs.phase != clPick {
		t.Fatal("must not start with a bad preview")
	}
	h.key("esc")
	if cs.phase != clURL {
		t.Fatal("esc goes back to the URL step")
	}
	cs.input.SetValue("https://github.com/org/repo.git")
	h.key("enter")
	h.key("enter")
	if cs.phase != clRunning {
		t.Fatal("running")
	}
	h.key("esc")
	h.waitFor("cancelled", func() bool { return cs.phase == clPick })
	close(release)
	h.key("esc")
	h.key("esc")
	if h.depth() != 1 {
		t.Fatalf("depth %d", h.depth())
	}
}

func TestCloneNoAccounts(t *testing.T) {
	h := newHarness(t, nil)
	h.key("c")
	h.typeText("https://github.com/org/repo.git")
	h.key("enter")
	h.mustContain(tr("tui.clone.no_accounts"))
}

// T8: doctor rows start as spinners and fill in out of order.
func TestT8DoctorSpinnersAndOutOfOrder(t *testing.T) {
	h := newHarness(t, basicCfg())
	d := newDoctorScreen(h.m.sh)
	d.Resize(100, 27)
	n := len(d.rows)
	if got := strings.Count(d.View(), "⠋"); got != n {
		t.Fatalf("want %d spinners, got %d:\n%s", n, got, d.View())
	}
	var s Screen = d
	s, _ = s.Update(doctorResultMsg{id: d.id, check: "plan", results: []doctor.Result{{ID: "plan", Status: doctor.Warn, Msg: "doctor.plan.pending", Args: []string{"3"}, HintKey: "doctor.hint.apply"}}})
	s, _ = s.Update(doctorResultMsg{id: d.id, check: "git_version", results: []doctor.Result{{ID: "git_version", Status: doctor.OK, Msg: "doctor.git.ok", Args: []string{"2.56.0"}}}})
	v := s.View()
	if got := strings.Count(v, "⠋"); got != n-2 {
		t.Fatalf("want %d spinners, got %d", n-2, got)
	}
	if !strings.Contains(v, tr("tui.doctor.check.git_version")) || !strings.Contains(v, tr("doctor.git.ok", "2.56.0")) ||
		!strings.Contains(v, tr("doctor.plan.pending", "3")) || !strings.Contains(v, tr("doctor.hint.apply")) {
		t.Fatalf("results not rendered:\n%s", v)
	}
	// a result of an older run is ignored
	s, _ = s.Update(doctorResultMsg{id: d.id + 1, check: "config", results: []doctor.Result{{Status: doctor.OK, Msg: "doctor.config.ok"}}})
	if strings.Contains(s.View(), tr("doctor.config.ok")) {
		t.Fatal("foreign run id must be ignored")
	}
}

func TestT8DoctorSSHOnlyOnKeypress(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.fake.On("git version", execx.Response{Stdout: "git version 2.56.0\n"})
	h.fake.On("ssh -T", execx.Response{ExitCode: 1, Stderr: "Hi! You've successfully authenticated\n"})
	h.key("D")
	h.waitFor("checks done", func() bool { return !h.top().(*doctorScreen).pending() })
	if h.fake.Count("ssh -T") != 0 {
		t.Fatal("ssh -T must not run before the key press")
	}
	h.key("s")
	h.waitFor("ssh done", func() bool { return !h.top().(*doctorScreen).pending() })
	if got := h.fake.Count("ssh -T"); got != 2 { // carol and dave use ssh
		t.Fatalf("ssh -T runs: %d", got)
	}
	h.mustContain(tr("tui.doctor.ssh_header"), "ssh -T github-acme-carol")
	// r runs everything again
	h.key("r")
	h.waitFor("rerun", func() bool { return !h.top().(*doctorScreen).pending() })
}

func TestDoctorNoSSHAccounts(t *testing.T) {
	c := basicCfg()
	c.Companies = c.Companies[:1]
	c.Companies[0].Accounts = c.Companies[0].Accounts[:2] // https only
	h := newHarness(t, c)
	h.key("D")
	h.waitFor("checks done", func() bool { return !h.top().(*doctorScreen).pending() })
	h.key("s")
	h.mustContain(tr("tui.doctor.no_ssh"))
}

// T9: check shows the mismatch and f opens the fix-remote confirmation.
func TestT9CheckAndFix(t *testing.T) {
	h := newHarness(t, basicCfg())
	top := h.e.Home + "/Code/acme/proj"
	h.m.sh.cwd = h.e.Home + "/Code/acme/proj"
	h.fake.On("git -C "+top+" rev-parse --show-toplevel", execx.Response{Stdout: top + "\n"})
	h.fake.On("git -C "+top+" config --get-regexp", execx.Response{Stdout: "remote.origin.url https://bob@github.com/org/repo.git\n"})
	h.fake.On("git -C "+top+" config --show-origin --get user.email", execx.Response{Stdout: "file:/home/x/.gitconfig\twrong@example.com\n"})
	h.fake.On("git -C "+top+" remote set-url", execx.Response{})
	h.key("k")
	h.waitFor("result", func() bool { c, ok := h.top().(*checkScreen); return ok && c.phase == chResult })
	h.mustContain(tr("warn.check.email_mismatch"), "acme/bob", "wrong@example.com", "bob@acme.com")
	h.mustContain(defKM.T("tui.check.fix_hint"))
	// fix targets the account matching the remote: nothing to change here
	h.key("f")
	h.waitFor("fix planned", func() bool { c := h.top().(*checkScreen); return c.phase != chFixPlanning })
	h.mustContain(tr("tui.fix.nothing"))

	// a remote on the wrong user is fixed after confirmation
	h.fake.On("git -C "+top+" config --get-regexp", execx.Response{Stdout: "remote.origin.url https://olduser@github.com/org/repo.git\n"})
	h.m.top().(*checkScreen).res.Account = "acme/alice"
	h.key("f")
	h.waitFor("fix confirm", func() bool { c := h.top().(*checkScreen); return c.phase == chFixConfirm })
	h.mustContain("https://olduser@github.com/org/repo.git", "https://alice@github.com/org/repo.git", tr("tui.fix.yes"))
	// the default answer is "no"
	h.key("enter")
	if h.top().(*checkScreen).phase != chResult || h.fake.Count("git -C "+top+" remote set-url") != 0 {
		t.Fatal("enter on the default must not change the remote")
	}
	h.key("f")
	h.waitFor("fix confirm", func() bool { c := h.top().(*checkScreen); return c.phase == chFixConfirm })
	h.key("y")
	h.waitFor("fixed", func() bool {
		return h.fake.Count("git -C "+top+" remote set-url origin https://alice@github.com/org/repo.git") == 1
	})
	h.waitFor("rechecked", func() bool { return h.top().(*checkScreen).phase == chResult })
}

func TestCheckNotARepo(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.fake.On("git -C "+h.e.Home+" rev-parse --show-toplevel", execx.Response{ExitCode: 128})
	h.key("k")
	h.waitFor("error", func() bool { c, ok := h.top().(*checkScreen); return ok && c.phase == chError })
	h.mustContain(tr("err.not_repo"))
	h.key("r")
	h.waitFor("error again", func() bool { return h.top().(*checkScreen).phase == chError })
}

// T10: keygen flow and the key result screen.
func TestT10KeygenAndResult(t *testing.T) {
	h := newHarness(t, basicCfg())
	ref, _ := h.m.sh.cfg.ParseRef("acme/carol")
	keyPath := h.e.Expand(ref.Company.DefaultKeyPath(ref.Account))
	h.fake.On("ssh-keygen", execx.Response{Do: func(execx.Cmd) error {
		testutil.WriteFile(t, keyPath, "PRIVATE", 0o000)
		testutil.WriteFile(t, keyPath+".pub", "ssh-ed25519 AAAAC3 acme/carol\n", 0o644)
		return nil
	}})
	for range []int{1, 2, 3} {
		h.key("down")
	}
	// no key yet: g starts the keygen flow
	h.key("g")
	k := h.top().(*keygenScreen)
	h.mustContain(tr("tui.keygen.with_pass"), tr("tui.keygen.no_pass"))
	// no passphrase needs a second confirmation whose default is "no"
	h.key("tab")
	h.key("enter")
	if k.phase != kgNoPassConfirm {
		t.Fatalf("phase %v", k.phase)
	}
	h.mustContain(strings.SplitN(tr("tui.keygen.nopass_warn"), ":", 2)[0])
	h.key("enter") // default: go back
	if k.phase != kgMode || h.fake.Count("ssh-keygen") != 0 {
		t.Fatalf("phase %v, keygen calls %d", k.phase, h.fake.Count("ssh-keygen"))
	}
	// with a passphrase: ssh-keygen is prepared for tea.ExecProcess without -N
	k.mode.idx = 0
	h.key("enter")
	if k.phase != kgRunning || h.fake.Count("ssh-keygen -t ed25519") != 1 || strings.Contains(strings.Join(h.fake.CallStrings(), " "), " -N") {
		t.Fatalf("phase %v calls %v", k.phase, h.fake.CallStrings())
	}
	// ssh-keygen "exits": the key is verified, recorded and displayed
	h.send(keyExecDoneMsg{id: k.id})
	h.waitFor("key result", func() bool { _, ok := h.top().(*keyResultScreen); return ok })
	h.mustContain("ssh-ed25519 AAAAC3", "github.com/settings/ssh/new", tr("tui.keyresult.pub"))
	ref2, err := h.m.sh.cfg.ParseRef("acme/carol")
	if err != nil || ref2.Account.SSHKey != "~/.ssh/gitid/acme-carol" {
		t.Fatalf("ssh_key not recorded: %+v %v", ref2.Account, err)
	}
	// c copies exactly the public key through pbcopy
	h.fake.On("pbcopy", execx.Response{})
	h.fake.On("open", execx.Response{})
	h.key("c")
	h.waitFor("copied", func() bool { return h.fake.Count("pbcopy") == 1 })
	var stdin string
	for _, c := range h.fake.Calls() {
		if c.Name == "pbcopy" {
			stdin = c.Stdin
		}
	}
	if strings.TrimSpace(stdin) != "ssh-ed25519 AAAAC3 acme/carol" {
		t.Fatalf("pbcopy stdin %q", stdin)
	}
	h.waitFor("copied status", func() bool { return strings.Contains(h.view(), tr("tui.keyresult.copied")) })
	h.key("o")
	h.waitFor("opened", func() bool { return h.fake.Count("open ") == 1 })
	h.mustContain(defKM.T("tui.keyresult.apply_hint"))
	// with a key on disk, g shows the existing key instead of generating
	h.key("esc")
	h.key("g")
	h.waitFor("existing key", func() bool { _, ok := h.top().(*keyResultScreen); return ok })
	if os.Getuid() != 0 {
		h.waitFor("pub", func() bool { return strings.Contains(h.view(), "ssh-ed25519 AAAAC3") })
	}
	if h.fake.Count("ssh-keygen") != 1 {
		t.Fatal("must not run ssh-keygen again")
	}
}

func TestKeygenNoPassConfirmed(t *testing.T) {
	h := newHarness(t, basicCfg())
	ref, _ := h.m.sh.cfg.ParseRef("acme/carol")
	h.fake.On("ssh-keygen", execx.Response{})
	k := newKeygenScreen(h.m.sh, ref)
	h.send(pushMsg{k})
	h.key("tab")
	h.key("enter")
	h.key("y")
	if k.phase != kgRunning || h.fake.Count("ssh-keygen -t ed25519 -a 100") != 1 || !strings.Contains(strings.Join(h.fake.CallStrings(), " "), "-N") {
		t.Fatalf("phase %v calls %v", k.phase, h.fake.CallStrings())
	}
	// a failing ssh-keygen shows the error
	h.send(keyExecDoneMsg{id: k.id, err: os.ErrPermission})
	if k.phase != kgError {
		t.Fatalf("phase %v", k.phase)
	}
	h.mustContain("✗")
}

func TestKeygenOnHTTPSAccountIsRefused(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("down") // acme/alice (https)
	h.key("g")
	h.mustContain(tr("err.not_ssh"))
	if h.depth() != 1 {
		t.Fatal("no screen for an https account")
	}
}

// T11: resizing never panics and never produces lines wider than the window.
func TestT11Resize(t *testing.T) {
	h := newHarness(t, basicCfg())
	check := func(label string) {
		t.Helper()
		for _, size := range [][2]int{{60, 20}, {80, 24}, {40, 10}, {120, 40}, {41, 11}, {200, 50}} {
			h.update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			v := h.view()
			lines := strings.Split(v, "\n")
			if len(lines) > size[1] {
				t.Errorf("%s %v: %d lines", label, size, len(lines))
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w > size[0] {
					t.Errorf("%s %v line %d is %d cells wide: %q", label, size, i, w, l)
				}
			}
		}
	}
	check("list")
	for _, k := range []string{"A", "a", "e", "d", "c", "k", "D", "p", "i", "enter"} {
		h.update(tea.WindowSizeMsg{Width: 100, Height: 30})
		h.key(k)
		check("screen after " + k)
		for h.depth() > 1 {
			h.update(popMsg{})
		}
	}
	h.update(tea.WindowSizeMsg{Width: 30, Height: 8})
	h.mustContain(strings.SplitN(tr("tui.too_small", minWidth, minHeight), ".", 2)[0])
}

func TestResizeKeepsFormUsable(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("a")
	h.send(tea.WindowSizeMsg{Width: 60, Height: 20})
	h.mustContain(tr("tui.f.platform"))
	h.send(tea.WindowSizeMsg{Width: 130, Height: 40})
	h.mustContain(tr("tui.f.platform"))
}

// T12: ctrl+c quits from every screen.
func TestT12CtrlCQuitsEverywhere(t *testing.T) {
	h := newHarness(t, basicCfg())
	isQuit := func(cmd tea.Cmd) bool {
		if cmd == nil {
			return false
		}
		_, ok := cmd().(tea.QuitMsg)
		return ok
	}
	for _, k := range []string{"", "A", "a", "d", "c", "k", "D", "p", "i", "?", "/"} {
		if k != "" {
			h.key(k)
		}
		_, cmd := h.m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		if !isQuit(cmd) {
			t.Errorf("ctrl+c must quit after %q (screen %T)", k, h.top())
		}
		for h.depth() > 1 {
			h.update(popMsg{})
		}
		h.m.showHelp = false
	}
}

func TestQuitKeyAndHelpOverlay(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("?")
	h.mustContain(tr("tui.help.title"), tr("tui.help.global"), defKM.T("tui.help.close"))
	h.key("a") // swallowed while help is open
	if h.depth() != 1 {
		t.Fatal("keys must not reach the screen while help is shown")
	}
	h.key("?")
	h.mustNotContain(tr("tui.help.title"))
	_, cmd := h.m.Update(keyMsg("q"))
	if cmd == nil {
		t.Fatal("q must quit on the list")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q must return tea.Quit")
	}
}

func TestStatusMessageExpires(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.send(statusMsg{level: levelOK, text: "hello"})
	h.mustContain("hello")
	h.send(statusExpireMsg{seq: h.m.statSeq})
	h.mustNotContain("hello")
	h.send(statusMsg{level: levelErr, text: "boom\nsecond line"})
	h.mustContain("boom")
	h.mustNotContain("second line")
	h.send(statusExpireMsg{seq: h.m.statSeq - 1}) // stale expiry is ignored
	h.mustContain("boom")
}

func TestDetailScreenOnEnter(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.key("down")
	h.key("enter")
	if _, ok := h.top().(*detailScreen); !ok {
		t.Fatalf("enter must open the detail screen, got %T", h.top())
	}
	h.mustContain("acme/alice", tr("tui.f.email"), "alice@acme.com")
	h.key("e")
	if _, ok := h.top().(*accountForm); !ok || h.depth() != 2 {
		t.Fatalf("e on the detail edits the account (%T, depth %d)", h.top(), h.depth())
	}
}

func TestListDetailPaneShowsKeyState(t *testing.T) {
	h := newHarness(t, basicCfg())
	for range []int{1, 2, 3} {
		h.key("down") // acme/carol (ssh)
	}
	h.mustContain("github-acme-carol", tr("tui.key.state_missing"))
}

// T-import: the import screen proposes companies from includeIf entries.
func TestImportFlow(t *testing.T) {
	e := testutil.TempEnv(t)
	testutil.WriteFile(t, e.Paths().GitConfig, "[user]\n\tname = x\n[includeIf \"gitdir:~/Code/work/initech-solutions/\"]\n\tpath = ~/.gitconfig-initech\n", 0o644)
	testutil.WriteFile(t, filepath.Join(e.Home, ".gitconfig-initech"), "[user]\n\tname = Dana Initech\n\temail = dana@initech.example\n", 0o644)
	testutil.WriteFile(t, e.Paths().SSHConfig, "Host github.com\n\tIdentityFile ~/.ssh/id_work\n", 0o600)
	h := newHarness(t, nil)
	h.m.sh.svc.Env = e // same temp home
	h.key("i")
	h.waitFor("proposals", func() bool { s := h.top().(*importScreen); return s.phase == imList })
	h.mustContain("initech-solutions", "dana@initech.example", "~/.gitconfig-initech", "github.com", "~/.ssh/id_work")
	h.key("enter")
	s := h.top().(*importScreen)
	if s.phase != imForm {
		t.Fatalf("phase %v", s.phase)
	}
	h.key("enter") // platform github
	h.typeText("dana")
	h.key("enter")
	h.key("enter") // auth https
	h.key("enter") // host default -> submit
	h.waitFor("confirm", func() bool { _, ok := h.top().(*confirmScreen); return ok })
	c, _, ok := h.m.sh.cfg.FindCompany("initech-solutions")
	if !ok || len(c.Accounts) != 1 || c.Accounts[0].Username != "dana" || c.Accounts[0].Email != "dana@initech.example" {
		t.Fatalf("imported company %+v", c)
	}
}

func TestImportNothingFound(t *testing.T) {
	h := newHarness(t, nil)
	h.key("i")
	h.waitFor("loaded", func() bool { return h.top().(*importScreen).phase == imList })
	h.mustContain(tr("cli.import.none"))
	h.key("enter") // nothing to pick
	if h.top().(*importScreen).phase != imList {
		t.Fatal("no form without proposals")
	}
}

func TestLanguageEnglish(t *testing.T) {
	defer i18n.SetLang("vi")
	i18n.SetLang("en")
	h := newHarness(t, basicCfg())
	h.mustContain("Overview", "to filter by name")
	h.mustContain("3 accounts", "unapplied change(s) — press p", "preview & apply", "all keys")
	h.mustNotContain("tài khoản", "chưa áp dụng")
	h.key("A")
	h.mustContain("Add company", "e.g. acme")
}

func TestErrText(t *testing.T) {
	if errText(nil) != "" {
		t.Fatal("nil")
	}
	if got := errText(os.ErrPermission); !strings.Contains(got, "permission") {
		t.Fatal(got)
	}
}

func TestHeaderKeepsBreadcrumbWithLongPath(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.m.sh.cwd = h.e.Home + "/Code/acme/a/very/deep/nested/directory/structure/that/keeps/going/and/going"
	h.update(tea.WindowSizeMsg{Width: 80, Height: 24})
	v := strings.Split(h.view(), "\n")[0]
	if !strings.Contains(v, tr("tui.list.title")) || !strings.Contains(v, "…") || lipgloss.Width(v) > 80 {
		t.Fatalf("header %q", v)
	}
	if got := truncLeft("short", 10); got != "short" {
		t.Fatal(got)
	}
	if got := truncLeft("/a/b/c/d/e", 6); lipgloss.Width(got) != 6 || !strings.HasSuffix(got, "d/e") {
		t.Fatalf("truncLeft %q", got)
	}
}

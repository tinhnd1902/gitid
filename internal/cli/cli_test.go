package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/cli"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/testutil"
	"github.com/tinhnd1902/gitid/internal/testutil/fixtures"
)

type harness struct {
	t   *testing.T
	e   env.Env
	f   *execx.Fake
	tty bool
	in  string
}

func newHarness(t *testing.T, f *execx.Fake) *harness {
	t.Helper()
	e := testutil.TempEnv(t)
	if f == nil {
		f = execx.NewFake()
	}
	t.Cleanup(func() { i18n.SetLang("vi") })
	return &harness{t: t, e: e, f: f}
}

func (h *harness) seed() {
	h.t.Helper()
	if _, _, err := app.New(h.e, h.f).Commit(fixtures.Basic()); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) run(args ...string) (int, string, string) {
	h.t.Helper()
	var out, errb bytes.Buffer
	code := cli.Execute(cli.Deps{
		Env: h.e, Runner: h.f, Stdin: strings.NewReader(h.in), Stdout: &out, Stderr: &errb,
		IsTTY: func() bool { return h.tty }, Cwd: func() (string, error) { return h.e.Home + "/Code/acme/r1", nil },
		Version: "dev", Lang: "vi",
	}, args)
	return code, out.String(), errb.String()
}

func TestX1ListJSON(t *testing.T) {
	h := newHarness(t, nil)
	h.seed()
	code, out, _ := h.run("list", "--json")
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	var v struct {
		Companies []struct {
			Slug     string `json:"slug"`
			Accounts []struct {
				ID       string `json:"id"`
				SSHAlias string `json:"ssh_alias"`
				Default  bool   `json:"default"`
			} `json:"accounts"`
		} `json:"companies"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil || len(v.Companies) != 3 || len(v.Companies[0].Accounts) != 3 {
		t.Fatalf("%v\n%s", err, out)
	}
	if v.Companies[0].Accounts[2].SSHAlias != "github-acme-carol" || !v.Companies[0].Accounts[0].Default {
		t.Fatalf("%+v", v.Companies[0])
	}
	for _, secret := range []string{"password", "token", "PRIVATE"} {
		if strings.Contains(strings.ToLower(out), strings.ToLower(secret)) {
			t.Fatalf("output contains %q", secret)
		}
	}
	// human output
	code, out, _ = h.run("list")
	if code != 0 || !strings.Contains(out, "acme  ACME Corp  ~/Code/acme/") || !strings.Contains(out, "* alice") {
		t.Fatalf("%d\n%s", code, out)
	}
	// empty
	h2 := newHarness(t, nil)
	if code, out, _ := h2.run("list"); code != 0 || !strings.Contains(out, "Chưa có công ty nào") {
		t.Fatalf("%d %s", code, out)
	}
	if code, out, _ := h2.run("list", "--json"); code != 0 || !strings.Contains(out, `"companies": []`) {
		t.Fatalf("%d %s", code, out)
	}
}

func TestX2ApplyDryRunNoWrites(t *testing.T) {
	h := newHarness(t, nil)
	h.seed()
	testutil.WriteFile(t, h.e.Paths().GitConfig, "[user]\n\tname = me\n", 0o644)
	before := testutil.Snapshot(t, h.e.Home)
	code, out, errOut := h.run("apply", "--dry-run")
	if code != 0 || errOut != "" {
		t.Fatalf("%d %s", code, errOut)
	}
	for _, want := range []string{"--- /dev/null", "+++ b/~/.gitconfig", "+[includeIf \"gitdir:~/Code/acme/\"]", "(dry-run)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	testutil.AssertSame(t, before, testutil.Snapshot(t, h.e.Home))
	if len(h.f.Calls()) != 0 {
		t.Fatalf("exec during dry-run: %v", h.f.CallStrings())
	}
}

func TestX3ApplyNonTTYRefuses(t *testing.T) {
	h := newHarness(t, nil)
	h.seed()
	before := testutil.Snapshot(t, h.e.Home)
	code, _, errOut := h.run("apply")
	if code != 1 || !strings.Contains(errOut, "--yes") {
		t.Fatalf("%d %q", code, errOut)
	}
	testutil.AssertSame(t, before, testutil.Snapshot(t, h.e.Home))
	// --yes works
	code, out, _ := h.run("apply", "--yes")
	if code != 0 || !strings.Contains(out, "Hoàn tất. Bản sao lưu: ~/.config/gitid/backups/") {
		t.Fatalf("%d\n%s", code, out)
	}
	// second apply: nothing to do, no new backup
	n := len(mustDir(t, h.e.Paths().BackupsDir))
	code, out, _ = h.run("apply", "--yes")
	if code != 0 || !strings.Contains(out, "Không có thay đổi") || len(mustDir(t, h.e.Paths().BackupsDir)) != n {
		t.Fatalf("%d\n%s", code, out)
	}
}

func mustDir(t *testing.T, d string) []os.DirEntry {
	t.Helper()
	ents, err := os.ReadDir(d)
	if err != nil {
		t.Fatal(err)
	}
	return ents
}

func TestApplyTTYConfirm(t *testing.T) {
	h := newHarness(t, nil)
	h.seed()
	h.tty = true
	h.in = "n\n"
	code, out, _ := h.run("apply")
	if code != 0 || !strings.Contains(out, "Đã hủy") {
		t.Fatalf("%d\n%s", code, out)
	}
	if _, err := os.Stat(h.e.Paths().GitConfig); err == nil {
		t.Fatal("written after 'n'")
	}
	h.in = "y\n"
	code, out, _ = h.run("apply")
	if code != 0 || !strings.Contains(out, "Đã ghi: ~/.gitconfig") {
		t.Fatalf("%d\n%s", code, out)
	}
	// removing everything deletes generated files
	if err := os.Remove(h.e.Paths().ConfigFile); err != nil {
		t.Fatal(err)
	}
	code, out, _ = h.run("apply", "--yes")
	if code != 0 || !strings.Contains(out, "Đã xóa:") {
		t.Fatalf("%d\n%s", code, out)
	}
}

func TestApplyInvalidConfigAndLocked(t *testing.T) {
	h := newHarness(t, nil)
	testutil.WriteFile(t, h.e.Paths().ConfigFile, "version: 1\ncompanies:\n  - slug: Bad Slug\n    name: x\n    base_dir: ~/Code/x/\n", 0o600)
	code, _, errOut := h.run("apply", "--yes")
	if code != 1 || !strings.Contains(errOut, "Cấu hình không hợp lệ") || !strings.Contains(errOut, "Slug") {
		t.Fatalf("%d %q", code, errOut)
	}
	h2 := newHarness(t, nil)
	h2.seed()
	testutil.WriteFile(t, h2.e.Paths().GitConfig+".lock", "", 0o644)
	code, _, errOut = h2.run("apply", "--yes")
	if code != 1 || !strings.Contains(errOut, ".gitconfig.lock") {
		t.Fatalf("%d %q", code, errOut)
	}
	// unsupported version
	h3 := newHarness(t, nil)
	testutil.WriteFile(t, h3.e.Paths().ConfigFile, "version: 9\n", 0o600)
	code, _, errOut = h3.run("list")
	if code != 1 || !strings.Contains(errOut, "không được hỗ trợ") {
		t.Fatalf("%d %q", code, errOut)
	}
}

func TestX4CloneDryRun(t *testing.T) {
	h := newHarness(t, nil)
	h.seed()
	code, out, _ := h.run("clone", "https://github.com/org/repo.git", "--account", "acme/alice", "--dry-run")
	if code != 0 || !strings.Contains(out, "https://alice@github.com/org/repo.git") || !strings.Contains(out, "~/Code/acme/repo") || !strings.Contains(out, "(dry-run)") {
		t.Fatalf("%d\n%s", code, out)
	}
	if len(h.f.Calls()) != 0 {
		t.Fatalf("exec: %v", h.f.CallStrings())
	}
	code, out, _ = h.run("clone", "https://alice:ghp_SECRET@github.com/org/repo.git", "--account", "acme/alice", "--dry-run")
	if code != 0 || strings.Contains(out, "SECRET") || !strings.Contains(out, "mật khẩu/token") {
		t.Fatalf("%d\n%s", code, out)
	}
}

func TestX5CloneRejectsOptionLikeURL(t *testing.T) {
	h := newHarness(t, nil)
	h.seed()
	code, _, errOut := h.run("clone", "-oProxyCommand=x", "--account", "acme/alice")
	if code != 1 || errOut == "" {
		t.Fatalf("%d %q", code, errOut)
	}
	code, _, errOut = h.run("clone", "--account", "acme/alice", "--", "-oProxyCommand=x")
	if code != 1 || !strings.Contains(errOut, "URL remote không hợp lệ") {
		t.Fatalf("%d %q", code, errOut)
	}
	if len(h.f.Calls()) != 0 {
		t.Fatalf("git executed: %v", h.f.CallStrings())
	}
}

func TestX8UnknownAccount(t *testing.T) {
	h := newHarness(t, nil)
	h.seed()
	code, _, errOut := h.run("clone", "https://github.com/o/r.git", "--account", "nobody/x", "--dry-run")
	if code != 1 || !strings.Contains(errOut, "Không tìm thấy tài khoản") {
		t.Fatalf("%d %q", code, errOut)
	}
	code, _, errOut = h.run("clone", "https://github.com/o/r.git", "--account", "alice", "--dry-run")
	if code != 1 || !strings.Contains(errOut, "bị trùng giữa nhiều công ty") {
		t.Fatalf("%d %q", code, errOut)
	}
	code, _, errOut = h.run("clone", "https://gitlab.com/o/r.git", "--account", "acme/alice", "--dry-run")
	if code != 1 || !strings.Contains(errOut, "--force") {
		t.Fatalf("%d %q", code, errOut)
	}
	// --account is required
	if code, _, _ := h.run("clone", "https://github.com/o/r.git"); code != 1 {
		t.Fatalf("%d", code)
	}
}

func TestCloneRunsAndVerifies(t *testing.T) {
	h := newHarness(t, nil)
	h.seed()
	dir := h.e.Home + "/Code/acme/repo"
	h.f.On("git clone", execx.Response{Stderr: "Receiving objects:  45% (1/2)\n"}).
		On("git -C "+dir+" config --show-origin --get user.email", execx.Response{Stdout: "file:/x\talice@acme.com\n"})
	code, out, _ := h.run("clone", "https://github.com/org/repo.git", "--account", "acme/alice")
	if code != 0 || !strings.Contains(out, "Đã clone vào ~/Code/acme/repo") || !strings.Contains(out, "(đúng)") {
		t.Fatalf("%d\n%s", code, out)
	}
	// wrong email: exit 2
	_ = os.RemoveAll(dir)
	h.f.On("git -C "+dir+" config --show-origin --get user.email", execx.Response{Stdout: "file:/x\tother@x.io\n"})
	code, out, _ = h.run("clone", "https://github.com/org/repo.git", "--account", "acme/alice")
	if code != 2 || !strings.Contains(out, "other@x.io") {
		t.Fatalf("%d\n%s", code, out)
	}
	// auth failure: hint + exit 1
	h2 := newHarness(t, execx.NewFake().On("git clone", execx.Response{ExitCode: 128, Stderr: "fatal: Authentication failed for 'https://u:tok123@github.com/o/r.git/'"}))
	h2.seed()
	code, out, errOut := h2.run("clone", "https://github.com/org/repo.git", "--account", "acme/alice")
	if code != 1 || !strings.Contains(out, "--interactive") || strings.Contains(errOut, "tok123") {
		t.Fatalf("%d\n%s\n%s", code, out, errOut)
	}
	// interactive mode runs git on the tty
	h3 := newHarness(t, execx.NewFake().On("git clone", execx.Response{}).On("git -C", execx.Response{Stdout: "file:/x\talice@acme.com\n"}))
	h3.seed()
	if code, _, _ := h3.run("clone", "https://github.com/org/repo.git", "--account", "acme/alice", "--interactive"); code != 0 {
		t.Fatalf("%d", code)
	}
	if env := strings.Join(h3.f.Calls()[0].Env, ","); strings.Contains(env, "GIT_TERMINAL_PROMPT") {
		t.Fatal("interactive clone must allow prompts")
	}
}

func TestCheckCommand(t *testing.T) {
	h := newHarness(t, nil)
	h.seed()
	top := h.e.Home + "/Code/acme/r1"
	if err := os.MkdirAll(top, 0o755); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := h.run("apply", "--yes"); code != 0 {
		t.Fatal("apply")
	}
	h.f.On("git -C "+top+" rev-parse --show-toplevel", execx.Response{Stdout: top + "\n"}).
		On("git -C "+top+" config --get-regexp", execx.Response{Stdout: "remote.origin.url https://bob@github.com/o/r.git\n"}).
		On("git -C "+top+" config --show-origin --get user.email", execx.Response{Stdout: "file:/x\tbob@acme.com\n"})
	code, out, _ := h.run("check")
	if code != 0 || !strings.Contains(out, "Mọi thứ khớp") || !strings.Contains(out, "acme/bob") {
		t.Fatalf("%d\n%s", code, out)
	}
	code, out, _ = h.run("check", "--json")
	var r struct {
		Repo, Remote, Account, Reason string
		EffectiveEmail                string `json:"effective_email"`
		EmailOrigin                   string `json:"email_origin"`
		Warnings                      []struct{ Code, Message string }
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil || code != 0 || r.Account != "acme/bob" || r.EffectiveEmail != "bob@acme.com" || r.Warnings == nil || len(r.Warnings) != 0 {
		t.Fatalf("%d %v\n%s", code, err, out)
	}
	// mismatch: exit 2
	h.f.On("git -C "+top+" config --show-origin --get user.email", execx.Response{Stdout: "file:/x\tother@x.io\n"})
	code, out, _ = h.run("check", top)
	if code != 2 || !strings.Contains(out, "! user.email thực tế khác") {
		t.Fatalf("%d\n%s", code, out)
	}
	// --json warnings are {code, message}: stable code plus translated text, never a raw key
	code, out, _ = h.run("check", top, "--json")
	var mj struct {
		Warnings []struct{ Code, Message string }
	}
	if err := json.Unmarshal([]byte(out), &mj); err != nil || code != 2 || len(mj.Warnings) == 0 {
		t.Fatalf("%d %v\n%s", code, err, out)
	}
	for _, w := range mj.Warnings {
		if !strings.HasPrefix(w.Code, "warn.") || w.Message == "" || w.Message == w.Code || strings.HasPrefix(w.Message, "warn.") {
			t.Fatalf("raw key in message: %+v", w)
		}
	}
	if mj.Warnings[0].Code != app.WarnEmailMismatch || !strings.Contains(mj.Warnings[0].Message, "user.email thực tế khác") {
		t.Fatalf("%+v", mj.Warnings)
	}
	// not a repo: exit 1
	h.f.On("git -C /nope rev-parse --show-toplevel", execx.Response{ExitCode: 128})
	code, _, errOut := h.run("check", "/nope")
	if code != 1 || !strings.Contains(errOut, "không phải thư mục git") {
		t.Fatalf("%d %q", code, errOut)
	}
}

func TestFixRemoteCommand(t *testing.T) {
	h := newHarness(t, nil)
	h.seed()
	top := h.e.Home + "/Code/acme/r1"
	h.f.On("git -C "+top+" rev-parse --show-toplevel", execx.Response{Stdout: top + "\n"}).
		On("git -C "+top+" config --get-regexp", execx.Response{Stdout: "remote.origin.url https://github.com/o/r.git\n"}).
		On("git -C "+top+" remote set-url", execx.Response{})
	code, out, _ := h.run("fix-remote", "--account", "acme/bob", "--dry-run")
	if code != 0 || !strings.Contains(out, "https://bob@github.com/o/r.git") || !strings.Contains(out, "(dry-run)") || h.f.Count("git -C "+top+" remote set-url") != 0 {
		t.Fatalf("%d\n%s", code, out)
	}
	code, out, _ = h.run("fix-remote", "--account", "acme/bob")
	if code != 0 || !strings.Contains(out, "Đã cập nhật remote") || h.f.Count("git -C "+top+" remote set-url origin https://bob@github.com/o/r.git") != 1 {
		t.Fatalf("%d\n%s", code, out)
	}
	h.f.On("git -C "+top+" config --get-regexp", execx.Response{Stdout: "remote.origin.url https://bob@github.com/o/r.git\n"})
	code, out, _ = h.run("fix-remote", "--account", "acme/bob")
	if code != 0 || !strings.Contains(out, "không cần thay đổi") {
		t.Fatalf("%d\n%s", code, out)
	}
}

func TestX6DoctorJSON(t *testing.T) {
	f := execx.NewFake().
		On("git version", execx.Response{Stdout: "git version 2.35.1\n"}).
		On("git credential-manager", execx.Response{}).
		On("git config --get-all", execx.Response{ExitCode: 1})
	h := newHarness(t, f)
	h.seed()
	code, out, _ := h.run("doctor", "--json")
	if code != 2 {
		t.Fatalf("code %d (git too old must fail)\n%s", code, out)
	}
	var v struct {
		Results []struct {
			ID      string `json:"id"`
			Status  string `json:"status"`
			Key     string `json:"message_key"`
			Message string `json:"message"`
		} `json:"results"`
		Worst string `json:"worst"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil || v.Worst != "fail" || len(v.Results) == 0 {
		t.Fatalf("%v\n%s", err, out)
	}
	found := false
	for _, r := range v.Results {
		if r.ID == "git_version" {
			found = r.Status == "fail" && r.Key == "doctor.git.old" && strings.Contains(r.Message, "quá cũ")
		}
	}
	if !found {
		t.Fatalf("%s", out)
	}
	// human output + ssh-test flag executes ssh
	f2 := execx.NewFake().
		On("git version", execx.Response{Stdout: "git version 2.56.0\n"}).
		On("git credential-manager", execx.Response{}).
		On("ssh -T", execx.Response{ExitCode: 1, Stderr: "successfully authenticated"})
	h2 := newHarness(t, f2)
	h2.seed()
	code, out, _ = h2.run("doctor", "--ssh-test")
	if f2.Count("ssh -T -o BatchMode=yes -o ConnectTimeout=8 git@github-acme-carol") != 1 {
		t.Fatalf("%v", f2.CallStrings())
	}
	if !strings.Contains(out, "[OK]") || !strings.Contains(out, "[LỖI]") || !strings.Contains(out, "-> Chạy `gitid apply`") {
		t.Fatalf("%d\n%s", code, out)
	}
}

func TestX7Version(t *testing.T) {
	h := newHarness(t, nil)
	code, out, _ := h.run("version")
	if code != 0 || strings.TrimSpace(out) != "gitid dev" {
		t.Fatalf("%d %q", code, out)
	}
}

func TestKeyGen(t *testing.T) {
	keygen := func() *execx.Fake {
		return execx.NewFake().On("ssh-keygen", execx.Response{Do: func(c execx.Cmd) error {
			p := c.Args[len(c.Args)-1]
			for i, a := range c.Args {
				if a == "-f" {
					p = c.Args[i+1]
				}
			}
			if err := os.WriteFile(p, []byte("PRIVATE"), 0o600); err != nil {
				return err
			}
			return os.WriteFile(p+".pub", []byte("ssh-ed25519 AAAA carol\n"), 0o644)
		}}).On("pbcopy", execx.Response{}).On("open", execx.Response{})
	}
	h := newHarness(t, keygen())
	h.seed()
	// D-1: passphrase mode on a non-terminal must refuse (ssh-keygen would read EOF as an empty passphrase)
	if code, _, errOut := h.run("key", "gen", "acme/carol"); code != 1 || !strings.Contains(errOut, "terminal") || h.f.Count("ssh-keygen") != 0 || h.f.Count("pbcopy") != 0 {
		t.Fatalf("non-tty passphrase mode: %d %q calls=%v", code, errOut, h.f.CallStrings())
	}
	h.tty = true // a terminal: passphrase mode works and the key is copied
	// seed has ssh_key set; clear it so GenerateKey records it
	code, out, _ := h.run("key", "gen", "acme/carol")
	if code != 0 || !strings.Contains(out, "ssh-ed25519 AAAA carol") || !strings.Contains(out, "https://github.com/settings/ssh/new") || !strings.Contains(out, "Đã sao chép") {
		t.Fatalf("%d\n%s", code, out)
	}
	if strings.Contains(out, "PRIVATE") {
		t.Fatal("private key printed")
	}
	// existing key: error, runner not asked to generate again
	n := h.f.Count("ssh-keygen")
	code, _, errOut := h.run("key", "gen", "acme/carol")
	if code != 1 || !strings.Contains(errOut, "không bao giờ ghi đè") || h.f.Count("ssh-keygen") != n {
		t.Fatalf("%d %q", code, errOut)
	}
	// https account
	if code, _, errOut := h.run("key", "gen", "acme/alice"); code != 1 || !strings.Contains(errOut, "không dùng SSH") {
		t.Fatalf("%d %q", code, errOut)
	}
	// --no-passphrase needs confirmation
	h2 := newHarness(t, keygen())
	h2.seed()
	if code, _, errOut := h2.run("key", "gen", "globex/dave", "--no-passphrase"); code != 1 || !strings.Contains(errOut, "--yes") || h2.f.Count("ssh-keygen") != 0 {
		t.Fatalf("%d %q", code, errOut)
	}
	h2.tty, h2.in = true, "n\n"
	if code, out, _ := h2.run("key", "gen", "globex/dave", "--no-passphrase"); code != 0 || !strings.Contains(out, "Đã hủy") || h2.f.Count("ssh-keygen") != 0 {
		t.Fatalf("%d %s", code, out)
	}
	h2.in = "y\n"
	if code, _, _ := h2.run("key", "gen", "globex/dave", "--no-passphrase"); code != 0 {
		t.Fatal(code)
	}
	if c := h2.f.Calls()[0]; c.Args[len(c.Args)-2] != "-N" || c.Args[len(c.Args)-1] != "" {
		t.Fatalf("%v", c.Args)
	}
	h3 := newHarness(t, keygen())
	h3.seed()
	// explicit --no-passphrase --yes works without a terminal and does not touch the clipboard
	if code, out, _ := h3.run("key", "gen", "globex/dave", "--no-passphrase", "--yes"); code != 0 || h3.f.Count("pbcopy") != 0 || strings.Contains(out, "Đã sao chép") {
		t.Fatalf("%d %s calls=%v", code, out, h3.f.CallStrings())
	}
	// --copy is the explicit opt-in on a non-terminal (the Fake stands in for pbcopy)
	h4 := newHarness(t, keygen())
	h4.seed()
	if code, out, _ := h4.run("key", "gen", "globex/dave", "--no-passphrase", "--yes", "--copy"); code != 0 || h4.f.Count("pbcopy") != 1 || !strings.Contains(out, "Đã sao chép") {
		t.Fatalf("%d %s calls=%v", code, out, h4.f.CallStrings())
	}
	if code, _, errOut := h3.run("key", "gen", "zzz/zzz"); code != 1 || !strings.Contains(errOut, "Không tìm thấy tài khoản") {
		t.Fatalf("%d %q", code, errOut)
	}
}

func TestImportCommand(t *testing.T) {
	h := newHarness(t, nil)
	testutil.WriteFile(t, h.e.Paths().GitConfig, "[includeIf \"gitdir:~/Code/work/main-job/initech-solutions/\"]\n\tpath = ~/.gitconfig-initech\n", 0o644)
	testutil.WriteFile(t, filepath.Join(h.e.Home, ".gitconfig-initech"), "[user]\n\tname = Dave\n\temail = x@initech.example\n", 0o644)
	testutil.WriteFile(t, h.e.Paths().SSHConfig, "Host github.com\n    IdentityFile ~/.ssh/id_personal\n", 0o644)
	before := testutil.Snapshot(t, h.e.Home)
	code, out, _ := h.run("import", "--dry-run")
	if code != 0 || !strings.Contains(out, "initech-solutions") || !strings.Contains(out, "x@initech.example") || !strings.Contains(out, "IdentityFile ~/.ssh/id_personal") {
		t.Fatalf("%d\n%s", code, out)
	}
	testutil.AssertSame(t, before, testutil.Snapshot(t, h.e.Home))
	// without --select: only lists and hints
	code, out, _ = h.run("import")
	if code != 0 || !strings.Contains(out, "gitid import --select") {
		t.Fatalf("%d\n%s", code, out)
	}
	if _, err := os.Stat(h.e.Paths().ConfigFile); err == nil {
		t.Fatal("config created without --select")
	}
	// needs flags
	if code, _, errOut := h.run("import", "--select", "initech-solutions"); code != 1 || !strings.Contains(errOut, "--platform") {
		t.Fatalf("%d %q", code, errOut)
	}
	if code, _, errOut := h.run("import", "--select", "nope", "--platform", "github", "--username", "dave"); code != 1 || !strings.Contains(errOut, "nope") {
		t.Fatalf("%d %q", code, errOut)
	}
	code, out, _ = h.run("import", "--select", "initech-solutions", "--platform", "gitlab", "--host", "git.initech.example", "--username", "dave")
	if code != 0 || !strings.Contains(out, "Đã thêm 1 công ty") {
		t.Fatalf("%d\n%s", code, out)
	}
	if _, out, _ := h.run("list"); !strings.Contains(out, "initech-solutions") {
		t.Fatal(out)
	}
	// user files untouched by import
	if testutil.ReadFile(t, h.e.Paths().GitConfig) != "[includeIf \"gitdir:~/Code/work/main-job/initech-solutions/\"]\n\tpath = ~/.gitconfig-initech\n" {
		t.Fatal("~/.gitconfig modified by import")
	}
	// invalid import (bad username) is rejected and does not corrupt the config
	if code, _, errOut := h.run("import", "--select", "initech-solutions-2", "--platform", "gitlab", "--username", "a b"); code != 1 || !strings.Contains(errOut, "Cấu hình không hợp lệ") {
		t.Fatalf("%d %q", code, errOut)
	}
}

func TestRootAndHelpAndLanguage(t *testing.T) {
	h := newHarness(t, nil)
	code, out, _ := h.run()
	if code != 0 || !strings.Contains(out, "TUI") {
		t.Fatalf("%d %s", code, out)
	}
	// StartTUI hook
	called := false
	var outb bytes.Buffer
	c := cli.Execute(cli.Deps{Env: h.e, Runner: h.f, Stdout: &outb, Lang: "en", StartTUI: func(*app.Service) error { called = true; return nil }}, nil)
	if c != 0 || !called {
		t.Fatalf("%d %v", c, called)
	}
	// hook error -> exit 1
	c = cli.Execute(cli.Deps{Env: h.e, Runner: h.f, StartTUI: func(*app.Service) error { return errors.New("boom") }, Lang: "en"}, nil)
	if c != 1 {
		t.Fatal(c)
	}
	// English output
	var eout bytes.Buffer
	c = cli.Execute(cli.Deps{Env: h.e, Runner: h.f, Stdout: &eout, Lang: "en"}, []string{"list"})
	if c != 0 || !strings.Contains(eout.String(), "No companies yet") {
		t.Fatalf("%d %s", c, eout.String())
	}
	// language from config
	testutil.WriteFile(t, h.e.Paths().ConfigFile, "version: 1\nsettings:\n  language: en\n", 0o600)
	var out2 bytes.Buffer
	c = cli.Execute(cli.Deps{Env: h.e, Runner: h.f, Stdout: &out2}, []string{"list"})
	if c != 0 || !strings.Contains(out2.String(), "No companies yet") {
		t.Fatalf("%d %s", c, out2.String())
	}
	i18n.SetLang("vi")
	// unknown command
	if code, _, errOut := h.run("frobnicate"); code != 1 || errOut == "" {
		t.Fatalf("%d %q", code, errOut)
	}
	if code, out, _ := h.run("--help"); code != 0 || !strings.Contains(out, "gitid giúp dùng nhiều tài khoản Git") {
		t.Fatalf("%d %s", code, out)
	}
	// nil deps are tolerated
	if c := cli.Execute(cli.Deps{Env: h.e}, []string{"version"}); c != 0 {
		t.Fatal(c)
	}
}

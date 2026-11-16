package doctor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/doctor"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/plan"
	"github.com/tinhnd1902/gitid/internal/testutil"
	"github.com/tinhnd1902/gitid/internal/testutil/fixtures"
)

var bg = context.Background()

func find(rs []doctor.Result, id string) *doctor.Result {
	for i := range rs {
		if rs[i].ID == id {
			return &rs[i]
		}
	}
	return nil
}

func deps(t *testing.T, cfg config.Config, f *execx.Fake) (doctor.Deps, env.Env) {
	t.Helper()
	e := testutil.TempEnv(t)
	return doctor.Deps{E: e, R: f, Cfg: config.Normalize(cfg, e)}, e
}

func goodFake() *execx.Fake {
	return execx.NewFake().
		On("git version", execx.Response{Stdout: "git version 2.56.0\n"}).
		On("git credential-manager --version", execx.Response{Stdout: "2.5.1\n"}).
		On("git config --get-all credential.helper", execx.Response{ExitCode: 1})
}

func applyAll(t *testing.T, d doctor.Deps, e env.Env) {
	t.Helper()
	p, err := plan.Build(e, d.Cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Apply(bg, e, p); err != nil {
		t.Fatal(err)
	}
}

func TestD1GitVersion(t *testing.T) {
	d, _ := deps(t, fixtures.Basic(), execx.NewFake().On("git version", execx.Response{Stdout: "git version 2.35.1\n"}))
	r := find(doctor.Run(bg, d, doctor.Options{}), "git_version")
	if r == nil || r.Status != doctor.Fail || r.Msg != doctor.MsgGitOld || r.Args[0] != "2.35.1" || r.HintKey != doctor.HintUpgradeGit {
		t.Fatalf("%+v", r)
	}
	d, _ = deps(t, fixtures.Basic(), goodFake())
	if r := find(doctor.Run(bg, d, doctor.Options{}), "git_version"); r.Status != doctor.OK || r.Args[0] != "2.56.0" {
		t.Fatalf("%+v", r)
	}
	d, _ = deps(t, fixtures.Basic(), execx.NewFake())
	if r := find(doctor.Run(bg, d, doctor.Options{}), "git_version"); r.Status != doctor.Fail || r.Msg != doctor.MsgGitUnknown {
		t.Fatalf("%+v", r)
	}
}

func TestD2CredentialHelper(t *testing.T) {
	base := func() *execx.Fake {
		return execx.NewFake().On("git version", execx.Response{Stdout: "git version 2.56.0\n"})
	}
	// GCM present
	d, _ := deps(t, fixtures.Basic(), base().On("git credential-manager --version", execx.Response{Stdout: "2.5\n"}))
	if r := find(doctor.Run(bg, d, doctor.Options{}), "credential_helper"); r.Status != doctor.OK || r.Msg != doctor.MsgCredGCM {
		t.Fatalf("%+v", r)
	}
	// absent + osxkeychain: warn with hint
	f := base().On("git credential-manager --version", execx.Response{ExitCode: 1}).On("git config --get-all credential.helper", execx.Response{Stdout: "osxkeychain\n"})
	d, _ = deps(t, fixtures.Basic(), f)
	r := find(doctor.Run(bg, d, doctor.Options{}), "credential_helper")
	if r.Status != doctor.Warn || r.Msg != doctor.MsgCredKeychain || r.HintKey != doctor.HintInstallGCM {
		t.Fatalf("%+v", r)
	}
	// neither
	f = base().On("git credential-manager --version", execx.Response{ExitCode: 1}).On("git config --get-all credential.helper", execx.Response{ExitCode: 1})
	d, _ = deps(t, fixtures.Basic(), f)
	if r := find(doctor.Run(bg, d, doctor.Options{}), "credential_helper"); r.Status != doctor.Warn || r.Msg != doctor.MsgCredNone {
		t.Fatalf("%+v", r)
	}
	for _, c := range f.CallStrings() {
		if strings.HasPrefix(c, "brew") {
			t.Fatal("brew must never be executed")
		}
	}
	// only ssh accounts: skip
	cfg := fixtures.Basic()
	cfg.Companies = []config.Company{fixtures.Globex()}
	d, _ = deps(t, cfg, base())
	if r := find(doctor.Run(bg, d, doctor.Options{}), "credential_helper"); r.Status != doctor.Skip {
		t.Fatalf("%+v", r)
	}
}

func TestD3D5Include(t *testing.T) {
	d, e := deps(t, fixtures.Basic(), goodFake())
	rs := doctor.Run(bg, d, doctor.Options{})
	if r := find(rs, "ssh_include"); r.Status != doctor.Fail || r.HintKey != doctor.HintApply || r.Msg != doctor.MsgSSHIncludeBad {
		t.Fatalf("D3 %+v", r)
	}
	if r := find(rs, "ssh_conf"); r.Status != doctor.Fail || r.Msg != doctor.MsgSSHConfStale {
		t.Fatalf("%+v", r)
	}
	if r := find(rs, "plan"); r.Status != doctor.Warn || r.Msg != doctor.MsgPlanPending || r.HintKey != doctor.HintApply { // D5
		t.Fatalf("D5 %+v", r)
	}
	// include not at top
	applyAll(t, d, e)
	testutil.WriteFile(t, e.Paths().SSHConfig, "Host x\n    Include config.d/*\n", 0o600)
	if r := find(doctor.Run(bg, d, doctor.Options{}), "ssh_include"); r.Status != doctor.Fail || r.Msg != doctor.MsgSSHIncludeScoped {
		t.Fatalf("%+v", r)
	}
	// corrupt block
	testutil.WriteFile(t, e.Paths().SSHConfig, "# >>> gitid managed block (do not edit; run `gitid apply`) >>>\n", 0o600)
	if r := find(doctor.Run(bg, d, doctor.Options{}), "ssh_include"); r.Status != doctor.Fail {
		t.Fatalf("%+v", r)
	}
}

func TestAllGreenAfterApply(t *testing.T) {
	d, e := deps(t, fixtures.Basic(), goodFake())
	applyAll(t, d, e)
	for _, c := range d.Cfg.Companies {
		if err := os.MkdirAll(e.Expand(c.BaseDir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"acme-carol", "globex-dave"} {
		testutil.WriteFile(t, filepath.Join(e.Paths().KeysDir, p), "k", 0o600)
		testutil.WriteFile(t, filepath.Join(e.Paths().KeysDir, p+".pub"), "p", 0o644)
	}
	rs := doctor.Run(bg, d, doctor.Options{})
	for _, r := range rs {
		// the alice patterns are ambiguous by fixture design (acme/alice vs beta/alice)
		if r.Status == doctor.Fail || (r.Status == doctor.Warn && !strings.HasPrefix(r.ID, "ambiguous:")) {
			t.Errorf("%+v", r)
		}
	}
	if find(rs, "ambiguous:https://alice@github.com/**") == nil { // D8
		t.Fatalf("D8: %+v", rs)
	}
	if doctor.Worst(rs) != doctor.Warn {
		t.Fatal("worst")
	}
	if doctor.Worst([]doctor.Result{{Status: doctor.OK}, {Status: doctor.Info}}) != doctor.OK || doctor.Worst([]doctor.Result{{Status: doctor.Fail}, {Status: doctor.Warn}}) != doctor.Fail {
		t.Fatal("Worst")
	}
	if find(rs, "config").Status != doctor.OK || find(rs, "plan").Status != doctor.OK || find(rs, "ssh_include").Status != doctor.OK || find(rs, "ssh_conf").Status != doctor.OK {
		t.Fatalf("%+v", rs)
	}
}

func TestD4Keys(t *testing.T) {
	d, e := deps(t, fixtures.Basic(), goodFake())
	rs := doctor.Run(bg, d, doctor.Options{})
	r := find(rs, "key:acme/carol")
	if r == nil || r.Status != doctor.Fail || r.Msg != doctor.MsgKeyMissing || r.HintKey != doctor.HintKeyGen {
		t.Fatalf("%+v", r)
	}
	key := filepath.Join(e.Paths().KeysDir, "acme-carol")
	testutil.WriteFile(t, key, "k", 0o644)
	r = find(doctor.Run(bg, d, doctor.Options{}), "key:acme/carol")
	if r.Status != doctor.Warn || r.Msg != doctor.MsgKeyMode || r.Args[1] != "0644" {
		t.Fatalf("%+v", r)
	}
	if err := os.Chmod(key, 0o600); err != nil {
		t.Fatal(err)
	}
	r = find(doctor.Run(bg, d, doctor.Options{}), "key:acme/carol")
	if r.Status != doctor.Warn || r.Msg != doctor.MsgKeyNoPub {
		t.Fatalf("%+v", r)
	}
	testutil.WriteFile(t, key+".pub", "p", 0o644)
	if r = find(doctor.Run(bg, d, doctor.Options{}), "key:acme/carol"); r.Status != doctor.OK {
		t.Fatalf("%+v", r)
	}
}

func TestBaseDirsAliasBlockLastConfig(t *testing.T) {
	d, e := deps(t, fixtures.Basic(), goodFake())
	rs := doctor.Run(bg, d, doctor.Options{})
	if r := find(rs, "basedir:acme"); r.Status != doctor.Warn || r.Msg != doctor.MsgBaseDirMissing || r.HintKey != doctor.HintMkdir {
		t.Fatalf("%+v", r)
	}
	if err := os.MkdirAll(e.Expand("~/Code/acme"), 0o755); err != nil {
		t.Fatal(err)
	}
	if r := find(doctor.Run(bg, d, doctor.Options{}), "basedir:acme"); r.Status != doctor.OK {
		t.Fatalf("%+v", r)
	}
	// alias collision with the user's own Host
	testutil.WriteFile(t, e.Paths().SSHConfig, "Host github-acme-carol\n  HostName x\n", 0o600)
	rs = doctor.Run(bg, d, doctor.Options{})
	if r := find(rs, "alias:github-acme-carol"); r == nil || r.Status != doctor.Fail || r.Msg != doctor.MsgAliasClash {
		t.Fatalf("%+v", rs)
	}
	if r := find(rs, "config"); r.Status != doctor.Fail || r.Msg != doctor.MsgConfigInvalid {
		t.Fatalf("config check should report the collision: %+v", r)
	}
	// block_last: user content after the block is info
	d2, e2 := deps(t, fixtures.Basic(), goodFake())
	applyAll(t, d2, e2)
	if r := find(doctor.Run(bg, d2, doctor.Options{}), "block_last"); r.Status != doctor.OK {
		t.Fatalf("%+v", r)
	}
	g := testutil.ReadFile(t, e2.Paths().GitConfig)
	testutil.WriteFile(t, e2.Paths().GitConfig, g+"[core]\n\teditor = vim\n", 0o644)
	if r := find(doctor.Run(bg, d2, doctor.Options{}), "block_last"); r.Status != doctor.Info || r.Msg != doctor.MsgBlockLastInfo {
		t.Fatalf("%+v", r)
	}
	// corrupt blocks in gitconfig: block_last stays quiet, plan reports
	testutil.WriteFile(t, e2.Paths().GitConfig, "# >>> gitid managed block (do not edit; run `gitid apply`) >>>\n", 0o644)
	rs = doctor.Run(bg, d2, doctor.Options{})
	if find(rs, "block_last").Status != doctor.OK || find(rs, "plan").Status != doctor.Fail {
		t.Fatalf("%+v", rs)
	}
}

func TestD6SSHTestOptIn(t *testing.T) {
	f := goodFake()
	d, _ := deps(t, fixtures.Basic(), f)
	doctor.Run(bg, d, doctor.Options{})
	if f.Count("ssh") != 0 {
		t.Fatal("ssh must not run unless opted in")
	}
	f = goodFake().
		On("ssh -T -o BatchMode=yes -o ConnectTimeout=8 git@github-acme-carol", execx.Response{ExitCode: 1, Stderr: "Hi carol! You've successfully authenticated, but GitHub does not provide shell access.\n"}).
		On("ssh -T -o BatchMode=yes -o ConnectTimeout=8 git@gitlab-globex-dave", execx.Response{ExitCode: 255, Stderr: "git@x: Permission denied (publickey).\n"})
	d, _ = deps(t, fixtures.Basic(), f)
	rs := doctor.Run(bg, d, doctor.Options{SSHTest: true})
	if r := find(rs, "ssh_test:github-acme-carol"); r == nil || r.Status != doctor.OK {
		t.Fatalf("%+v", r)
	}
	r := find(rs, "ssh_test:gitlab-globex-dave")
	if r == nil || r.Status != doctor.Fail || !strings.Contains(r.Detail, "Permission denied") {
		t.Fatalf("%+v", r)
	}
	if strings.Join(doctor.SSHTestArgs("a"), " ") != "-T -o BatchMode=yes -o ConnectTimeout=8 git@a" {
		t.Fatal(doctor.SSHTestArgs("a"))
	}
	// gitlab banner with exit 0, and a start failure
	f2 := execx.NewFake().On("ssh", execx.Response{Stdout: "Welcome to GitLab, @dave!\n"})
	d.R = f2
	if r := doctor.SSHTest(bg, d, "gitlab-globex-dave"); r.Status != doctor.OK {
		t.Fatalf("%+v", r)
	}
	d.R = execx.NewFake().On("ssh", execx.Response{Err: os.ErrNotExist})
	if r := doctor.SSHTest(bg, d, "x"); r.Status != doctor.Fail || r.Detail != "ssh not found" {
		t.Fatalf("%+v", r)
	}
	d.R = execx.NewFake().On("ssh", execx.Response{Err: errors.New("boom")})
	if r := doctor.SSHTest(bg, d, "x"); r.Detail != "boom" {
		t.Fatalf("%+v", r)
	}
}

func TestD7Concurrency(t *testing.T) {
	block := make(chan struct{})
	var fast int32
	checks := []doctor.Check{
		{ID: "slow", Fn: func(ctx context.Context, d doctor.Deps) []doctor.Result {
			select {
			case <-block:
			case <-ctx.Done():
			}
			return []doctor.Result{{ID: "slow", Status: doctor.OK}}
		}},
		{ID: "fast1", Fn: func(context.Context, doctor.Deps) []doctor.Result {
			atomic.AddInt32(&fast, 1)
			return []doctor.Result{{ID: "fast1", Status: doctor.OK}}
		}},
		{ID: "fast2", Fn: func(context.Context, doctor.Deps) []doctor.Result {
			atomic.AddInt32(&fast, 1)
			return []doctor.Result{{ID: "fast2", Status: doctor.Warn}}
		}},
	}
	got := make(chan doctor.Result, 8)
	done := make(chan []doctor.Result)
	go func() { done <- doctor.RunChecks(bg, doctor.Deps{}, checks, func(r doctor.Result) { got <- r }) }()
	deadline := time.After(2 * time.Second)
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case r := <-got:
			seen[r.ID] = true
		case <-deadline:
			t.Fatalf("fast checks blocked by the slow one: %v", seen)
		}
	}
	if seen["slow"] {
		t.Fatal("slow finished early")
	}
	close(block)
	all := <-done
	if len(all) != 3 || all[0].ID != "slow" || all[1].ID != "fast1" || all[2].ID != "fast2" {
		t.Fatalf("order: %+v", all)
	}
	// cancellation stops the slow check
	ctx, cancel := context.WithCancel(bg)
	blocked := make(chan struct{})
	checks[0].Fn = func(ctx context.Context, d doctor.Deps) []doctor.Result {
		<-ctx.Done()
		close(blocked)
		return nil
	}
	fin := make(chan struct{})
	go func() { doctor.RunChecks(ctx, doctor.Deps{}, checks, nil); close(fin) }()
	cancel()
	select {
	case <-fin:
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not stop checks")
	}
	<-blocked
}

func TestRunnerCancelDuringRealChecks(t *testing.T) {
	block := make(chan struct{})
	f := execx.NewFake().On("git", execx.Response{Block: block})
	d, _ := deps(t, fixtures.Basic(), f)
	ctx, cancel := context.WithTimeout(bg, 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	rs := doctor.Run(ctx, d, doctor.Options{})
	if time.Since(start) > 3*time.Second {
		t.Fatal("did not return after cancel")
	}
	if find(rs, "git_version").Status != doctor.Fail {
		t.Fatalf("%+v", find(rs, "git_version"))
	}
	close(block)
}

func TestAllKeysUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, k := range doctor.AllKeys() {
		if seen[k] || !strings.HasPrefix(k, "doctor.") {
			t.Fatalf("bad key %q", k)
		}
		seen[k] = true
	}
}

func TestPlanBuildErrorReported(t *testing.T) {
	d, e := deps(t, fixtures.Basic(), goodFake())
	testutil.WriteFile(t, e.Paths().GitConfig, "# >>> gitid managed block (do not edit; run `gitid apply`) >>>\n", 0o644)
	if r := find(doctor.Run(bg, d, doctor.Options{}), "plan"); r.Status != doctor.Fail || r.Detail == "" {
		t.Fatalf("%+v", r)
	}
}

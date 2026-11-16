package app_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/doctor"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/gitops"
	"github.com/tinhnd1902/gitid/internal/importer"
	"github.com/tinhnd1902/gitid/internal/keys"
	"github.com/tinhnd1902/gitid/internal/platform"
	"github.com/tinhnd1902/gitid/internal/remote"
	"github.com/tinhnd1902/gitid/internal/testutil"
	"github.com/tinhnd1902/gitid/internal/testutil/fixtures"
)

var bg = context.Background()

func newSvc(t *testing.T, f *execx.Fake) (*app.Service, env.Env) {
	t.Helper()
	e := testutil.TempEnv(t)
	n := 0
	e.Now = func() time.Time { n++; return testutil.FixedTime.Add(time.Duration(n) * time.Second) }
	if f == nil {
		f = execx.NewFake()
	}
	return app.New(e, f), e
}

func applied(t *testing.T, s *app.Service, cfg config.Config) config.Config {
	t.Helper()
	cfg, _, err := s.Commit(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.BuildPlan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(bg, p); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestLoadCommitValidate(t *testing.T) {
	s, e := newSvc(t, nil)
	cfg, err := s.Load()
	if err != nil || len(cfg.Companies) != 0 {
		t.Fatalf("%+v %v", cfg, err)
	}
	if _, err := os.Stat(e.Paths().ConfigFile); err == nil {
		t.Fatal("Load must not create the file")
	}
	bad := fixtures.Basic()
	bad.Companies[0].Accounts[0].Email = "nope"
	_, _, err = s.Commit(bad)
	var ve *app.ValidationError
	if !errors.As(err, &ve) || len(ve.Errors) != 1 || ve.Errors[0].Code != config.ErrEmailInvalid || !strings.Contains(err.Error(), "invalid configuration") {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(e.Paths().ConfigFile); err == nil {
		t.Fatal("invalid config saved")
	}
	saved, dir, err := s.Commit(fixtures.Basic())
	if err != nil || dir != "" || saved.Companies[0].BaseDir != "~/Code/acme/" {
		t.Fatalf("%v %q", err, dir)
	}
	loaded, err := s.Load()
	if err != nil || len(loaded.Companies) != 3 {
		t.Fatalf("%v", err)
	}
	// ssh alias collision with the user's ssh config is a validation error
	testutil.WriteFile(t, e.Paths().SSHConfig, "Host github-acme-carol\n", 0o600)
	if errs := s.Validate(fixtures.Basic()); len(errs) != 1 || errs[0].Code != config.ErrAliasHostClash {
		t.Fatalf("%v", errs)
	}
}

func TestBuildApplyDoctorImportFacade(t *testing.T) {
	s, e := newSvc(t, execx.NewFake().On("git version", execx.Response{Stdout: "git version 2.56.0\n"}).On("git credential-manager", execx.Response{}))
	cfg := applied(t, s, fixtures.Basic())
	p, _ := s.BuildPlan(cfg)
	if !p.Empty() {
		t.Fatal("not in sync after apply")
	}
	rs := s.Doctor(bg, cfg, doctor.Options{})
	if len(rs) == 0 {
		t.Fatal("doctor")
	}
	r, err := s.Import(cfg)
	if err != nil || len(r.Proposals) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	testutil.Guard(t, e, e.Paths().GitConfig)
}

// ---- check ----

func repoFake(dir, top, url, emailLine string) *execx.Fake {
	f := execx.NewFake().On("git -C "+dir+" rev-parse --show-toplevel", execx.Response{Stdout: top + "\n"})
	if url != "" {
		f.On("git -C "+top+" config --get-regexp", execx.Response{Stdout: "remote.origin.url " + url + "\n"})
	} else {
		f.On("git -C "+top+" config --get-regexp", execx.Response{ExitCode: 1})
	}
	if emailLine != "" {
		f.On("git -C "+top+" config --show-origin --get user.email", execx.Response{Stdout: emailLine + "\n"})
	} else {
		f.On("git -C "+top+" config --show-origin --get user.email", execx.Response{ExitCode: 1})
	}
	return f
}

func hasW(r app.CheckResult, w string) bool {
	for _, x := range r.Warnings {
		if x == w {
			return true
		}
	}
	return false
}

func TestCheckScenarios(t *testing.T) {
	run := func(top, url, emailLine string, doApply bool) (app.CheckResult, error) {
		e := testutil.TempEnv(t)
		top = strings.ReplaceAll(top, "$H", e.Home)
		emailLine = strings.ReplaceAll(emailLine, "$H", e.Home)
		f := repoFake(top, top, url, emailLine)
		s := app.New(e, f)
		cfg, _, err := s.Commit(fixtures.Basic())
		if err != nil {
			t.Fatal(err)
		}
		if doApply {
			p, _ := s.BuildPlan(cfg)
			if _, err := s.Apply(bg, p); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.MkdirAll(top, 0o755); err != nil {
			t.Fatal(err)
		}
		return s.Check(bg, cfg, top)
	}
	// C-1 ok
	r, err := run("$H/Code/acme/r1", "https://bob@github.com/o/r.git", "file:$H/.config/gitid/git/acme-bob.gitconfig\tbob@acme.com", true)
	if err != nil || !r.OK() || r.Account != "acme/bob" || r.Reason != "username" || r.EffectiveEmail != "bob@acme.com" || r.ExpectedEmail != "bob@acme.com" || !strings.HasSuffix(r.Repo, "Code/acme/r1") {
		t.Fatalf("C-1 %+v %v", r, err)
	}
	// C-2 mismatch
	r, _ = run("$H/Code/acme/r1", "https://bob@github.com/o/r.git", "file:$H/.gitconfig\tglobal@x.io", true)
	if !hasW(r, app.WarnEmailMismatch) || r.OK() {
		t.Fatalf("C-2 %+v", r)
	}
	// C-3 local override
	r, _ = run("$H/Code/acme/r1", "https://bob@github.com/o/r.git", "file:.git/config\tbob@acme.com", true)
	if !hasW(r, app.WarnLocalOverride) || hasW(r, app.WarnEmailMismatch) {
		t.Fatalf("C-3 %+v", r)
	}
	// C-5 outside base dir, https without user
	r, _ = run("$H/elsewhere/r", "https://github.com/o/r.git", "file:$H/.gitconfig\tglobal@x.io", true)
	if !hasW(r, app.WarnNoUserOutside) || !hasW(r, app.WarnNoAccount) {
		t.Fatalf("C-5 %+v", r)
	}
	// C-6 pending plan
	r, _ = run("$H/Code/acme/r1", "https://bob@github.com/o/r.git", "file:$H/.gitconfig\tbob@acme.com", false)
	if !hasW(r, app.WarnPendingApply) {
		t.Fatalf("C-6 %+v", r)
	}
	// default via base dir, no username warning
	r, _ = run("$H/Code/acme/r1", "https://github.com/o/r.git", "file:$H/x\talice@acme.com", true)
	if r.Account != "acme/alice" || r.Reason != "default" || !hasW(r, remote.WarnNoUsername) {
		t.Fatalf("default %+v", r)
	}
	// ambiguous
	r, _ = run("$H/elsewhere/r", "https://alice@github.com/o/r.git", "file:$H/x\tglobal@x.io", true)
	if !hasW(r, remote.WarnAmbiguous) || hasW(r, app.WarnNoAccount) || r.Account != "" {
		t.Fatalf("ambiguous %+v", r)
	}
	// password in URL is masked and warned
	r, _ = run("$H/Code/acme/r1", "https://bob:ghp_SECRET@github.com/o/r.git", "file:$H/x\tbob@acme.com", true)
	if strings.Contains(r.Remote, "SECRET") || !hasW(r, remote.WarnPassword) {
		t.Fatalf("%+v", r)
	}
	// unparsable remote (local path) is masked/kept and warned
	r, _ = run("$H/Code/acme/r1", "/some/local/path", "file:$H/x\talice@acme.com", true)
	if !hasW(r, app.WarnRemoteUnparsed) {
		t.Fatalf("%+v", r)
	}
	// no remote inside a base dir: default account by directory
	r, _ = run("$H/Code/beta/r1", "", "file:$H/x\talice@beta.io", true)
	if r.Account != "beta/alice" || !hasW(r, app.WarnNoRemote) {
		t.Fatalf("%+v", r)
	}
	// no email at all
	r, _ = run("$H/Code/acme/r1", "https://bob@github.com/o/r.git", "", true)
	if !hasW(r, app.WarnNoEmail) {
		t.Fatalf("%+v", r)
	}
	if len(app.AllWarnings()) != 12 {
		t.Fatal("warning list")
	}
}

func TestCheckNotRepoAndErrors(t *testing.T) { // C-4
	e := testutil.TempEnv(t)
	f := execx.NewFake().On("git -C /x rev-parse --show-toplevel", execx.Response{ExitCode: 128})
	s := app.New(e, f)
	if _, err := s.Check(bg, fixtures.Basic(), "/x"); !errors.Is(err, gitops.ErrNotRepo) || !app.IsUserError(err) {
		t.Fatalf("%v", err)
	}
	f = repoFake("/r", "/r", "https://bob@github.com/o/r", "")
	f.On("git -C /r config --show-origin --get user.email", execx.Response{ExitCode: 128})
	if _, err := app.New(e, f).Check(bg, fixtures.Basic(), "/r"); err == nil {
		t.Fatal("email lookup failure should surface")
	}
	if app.IsUserError(errors.New("boom")) {
		t.Fatal("boom is not a user error")
	}
}

// ---- fix-remote ----

func fixFake(top, url string) *execx.Fake {
	return execx.NewFake().
		On("git -C "+top+" rev-parse --show-toplevel", execx.Response{Stdout: top + "\n"}).
		On("git -C "+top+" config --get-regexp", execx.Response{Stdout: "remote.origin.url " + url + "\nremote.upstream.url git@github.com:up/stream.git\n"}).
		On("git -C "+top+" remote set-url", execx.Response{})
}

func TestFixRemote(t *testing.T) {
	e := testutil.TempEnv(t)
	top := e.Home + "/Code/acme/r"
	// FR-1
	f := fixFake(top, "https://github.com/o/r.git")
	s := app.New(e, f)
	cfg := fixtures.Basic()
	res, err := s.FixRemote(bg, cfg, app.FixRequest{Dir: top, Account: "acme/bob"})
	if err != nil || !res.Changed || res.New != "https://bob@github.com/o/r.git" || res.Old != "https://github.com/o/r.git" || res.Account != "acme/bob" {
		t.Fatalf("%+v %v", res, err)
	}
	if f.Count("git -C "+top+" remote set-url origin https://bob@github.com/o/r.git") != 1 {
		t.Fatalf("%v", f.CallStrings())
	}
	// FR-1 dry run
	f = fixFake(top, "https://github.com/o/r.git")
	res, err = app.New(e, f).FixRemote(bg, cfg, app.FixRequest{Dir: top, Account: "acme/bob", DryRun: true})
	if err != nil || !res.Changed || !res.DryRun || f.Count("git -C "+top+" remote set-url") != 0 {
		t.Fatalf("%+v %v %v", res, err, f.CallStrings())
	}
	// FR-2 already correct
	f = fixFake(top, "https://bob@github.com/o/r.git")
	res, err = app.New(e, f).FixRemote(bg, cfg, app.FixRequest{Dir: top, Account: "acme/bob"})
	if err != nil || res.Changed || f.Count("git -C "+top+" remote set-url") != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	// FR-3 token masked
	f = fixFake(top, "https://bob:ghp_SECRET@github.com/o/r.git")
	res, err = app.New(e, f).FixRemote(bg, cfg, app.FixRequest{Dir: top, Account: "acme/bob", DryRun: true})
	if err != nil || strings.Contains(res.Old, "SECRET") || strings.Contains(res.New, "SECRET") || !res.Changed {
		t.Fatalf("%+v %v", res, err)
	}
	// FR-4 --remote upstream
	f = fixFake(top, "https://github.com/o/r.git")
	res, err = app.New(e, f).FixRemote(bg, cfg, app.FixRequest{Dir: top, Account: "acme/carol", Remote: "upstream"})
	if err != nil || res.New != "git@github-acme-carol:up/stream.git" || f.Count("git -C "+top+" remote set-url upstream git@github-acme-carol:up/stream.git") != 1 {
		t.Fatalf("%+v %v %v", res, err, f.CallStrings())
	}
	// FR-5 host mismatch unless force
	f = fixFake(top, "https://gitlab.com/o/r.git")
	if _, err := app.New(e, f).FixRemote(bg, cfg, app.FixRequest{Dir: top, Account: "acme/bob"}); !errors.Is(err, remote.ErrHostMismatch) || !app.IsUserError(err) {
		t.Fatalf("%v", err)
	}
	if res, err := app.New(e, f).FixRemote(bg, cfg, app.FixRequest{Dir: top, Account: "acme/bob", Force: true, DryRun: true}); err != nil || res.New != "https://bob@github.com/o/r.git" {
		t.Fatalf("%+v %v", res, err)
	}
	// account detection
	f = fixFake(top, "https://bob@github.com/o/r.git")
	if res, err := app.New(e, f).FixRemote(bg, cfg, app.FixRequest{Dir: top}); err != nil || res.Changed || res.Account != "acme/bob" {
		t.Fatalf("%+v %v", res, err)
	}
	f = fixFake(top, "https://github.com/o/r.git") // no user: default of acme by dir (dir must exist for symlink eval; IsUnder works lexically)
	if res, err := app.New(e, f).FixRemote(bg, cfg, app.FixRequest{Dir: top}); err != nil || res.Account != "acme/alice" || !res.Changed {
		t.Fatalf("%+v %v", res, err)
	}
	f = fixFake("/tmp/elsewhere", "https://alice@github.com/o/r.git")
	if _, err := app.New(e, f).FixRemote(bg, cfg, app.FixRequest{Dir: "/tmp/elsewhere"}); !errors.Is(err, app.ErrAmbiguous) {
		t.Fatalf("%v", err)
	}
	f = fixFake("/tmp/elsewhere", "https://github.com/o/r.git")
	if _, err := app.New(e, f).FixRemote(bg, cfg, app.FixRequest{Dir: "/tmp/elsewhere"}); !errors.Is(err, app.ErrNoAccount) {
		t.Fatalf("%v", err)
	}
	// errors
	if _, err := app.New(e, f).FixRemote(bg, cfg, app.FixRequest{Dir: "/tmp/elsewhere", Remote: "nope", Account: "acme/bob"}); !errors.Is(err, app.ErrNoRemote) {
		t.Fatalf("%v", err)
	}
	if _, err := app.New(e, f).FixRemote(bg, cfg, app.FixRequest{Dir: "/tmp/elsewhere", Account: "zzz/zzz"}); !errors.Is(err, config.ErrRefNotFound) {
		t.Fatalf("%v", err)
	}
	f = fixFake("/tmp/elsewhere", "/local/path")
	if _, err := app.New(e, f).FixRemote(bg, cfg, app.FixRequest{Dir: "/tmp/elsewhere", Account: "acme/bob"}); !errors.Is(err, remote.ErrInvalid) {
		t.Fatalf("%v", err)
	}
	f = execx.NewFake().On("git -C /nope rev-parse", execx.Response{ExitCode: 128})
	if _, err := app.New(e, f).FixRemote(bg, cfg, app.FixRequest{Dir: "/nope"}); !errors.Is(err, gitops.ErrNotRepo) {
		t.Fatalf("%v", err)
	}
	f = fixFake(top, "https://github.com/o/r.git").On("git -C "+top+" remote set-url", execx.Response{ExitCode: 1})
	if _, err := app.New(e, f).FixRemote(bg, cfg, app.FixRequest{Dir: top, Account: "acme/bob"}); err == nil {
		t.Fatal("set-url failure must surface")
	}
}

// ---- clone ----

func TestPlanClone(t *testing.T) {
	s, e := newSvc(t, nil)
	cfg := fixtures.Basic()
	p, err := s.PlanClone(cfg, app.CloneRequest{URL: "https://github.com/org/repo.git", Account: "acme/alice"})
	if err != nil || p.URL != "https://alice@github.com/org/repo.git" || p.Dir != filepath.Join(e.Home, "Code/acme/repo") || p.Account.String() != "acme/alice" {
		t.Fatalf("%+v %v", p, err)
	}
	// ssh account and nested group repo
	p, err = s.PlanClone(cfg, app.CloneRequest{URL: "https://git.example.com/group/sub/svc.git", Account: "globex/dave"})
	if err != nil || p.URL != "git@gitlab-globex-dave:group/sub/svc.git" || !strings.HasSuffix(p.Dir, "Code/globex/svc") {
		t.Fatalf("%+v %v", p, err)
	}
	// explicit dirs
	p, err = s.PlanClone(cfg, app.CloneRequest{URL: "https://github.com/org/repo", Account: "acme/alice", Dir: "~/work/x"})
	if err != nil || p.Dir != filepath.Join(e.Home, "work/x") {
		t.Fatalf("%+v %v", p, err)
	}
	p, err = s.PlanClone(cfg, app.CloneRequest{URL: "https://github.com/org/repo", Account: "acme/alice", Dir: "rel", Cwd: "/tmp/cwd"})
	if err != nil || p.Dir != "/tmp/cwd/rel" {
		t.Fatalf("%+v %v", p, err)
	}
	// token in URL: masked
	p, err = s.PlanClone(cfg, app.CloneRequest{URL: "https://alice:ghp_SECRET@github.com/org/repo.git", Account: "acme/alice"})
	if err != nil || strings.Contains(p.Original, "SECRET") || strings.Contains(p.URL, "SECRET") || len(p.Warnings) != 1 {
		t.Fatalf("%+v %v", p, err)
	}
	// errors
	bad := []struct {
		req app.CloneRequest
		is  error
	}{
		{app.CloneRequest{URL: "-oProxyCommand=x", Account: "acme/alice"}, remote.ErrInvalid},
		{app.CloneRequest{URL: "https://github.com/o/r", Account: "nobody/x"}, config.ErrRefNotFound},
		{app.CloneRequest{URL: "https://gitlab.com/o/r", Account: "acme/alice"}, remote.ErrHostMismatch},
		{app.CloneRequest{URL: "https://github.com/o/r", Account: "acme/alice", Dir: "rel"}, gitops.ErrBadTarget},
		{app.CloneRequest{URL: "https://github.com/o/r", Account: "acme/alice", Dir: "/tmp/-x"}, gitops.ErrBadTarget},
		{app.CloneRequest{URL: "https://github.com/o/r", Account: "acme/alice", Dir: "/"}, gitops.ErrBadTarget},
	}
	for _, c := range bad {
		if _, err := s.PlanClone(cfg, c.req); !errors.Is(err, c.is) {
			t.Errorf("%+v: %v", c.req, err)
		}
	}
	// existing target
	if err := os.MkdirAll(filepath.Join(e.Home, "Code/acme/repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlanClone(cfg, app.CloneRequest{URL: "https://github.com/org/repo.git", Account: "acme/alice"}); !errors.Is(err, gitops.ErrTargetExists) {
		t.Fatalf("%v", err)
	}
	// a repo name that cannot be a directory name
	if _, err := s.PlanClone(cfg, app.CloneRequest{URL: "https://github.com/org/a..b.git", Account: "acme/alice"}); !errors.Is(err, gitops.ErrBadTarget) {
		t.Fatalf("%v", err)
	}
}

func TestCloneRunAndVerify(t *testing.T) {
	e := testutil.TempEnv(t)
	dir := filepath.Join(e.Home, "Code/acme/repo")
	f := execx.NewFake().
		On("git clone", execx.Response{Stderr: "Receiving objects:  45% (1/2)\n"}).
		On("git -C "+dir+" config --show-origin --get user.email", execx.Response{Stdout: "file:/x\talice@acme.com\n"})
	s := app.New(e, f)
	p, err := s.PlanClone(fixtures.Basic(), app.CloneRequest{URL: "https://github.com/org/repo.git", Account: "acme/alice"})
	if err != nil {
		t.Fatal(err)
	}
	var prog []gitops.Progress
	res, err := s.Clone(bg, p, func(pr gitops.Progress) { prog = append(prog, pr) })
	if err != nil || !res.EmailOK || res.EffectiveEmail != "alice@acme.com" || len(prog) != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	if f.Count("git clone --progress -- https://alice@github.com/org/repo.git "+dir) != 1 {
		t.Fatalf("%v", f.CallStrings())
	}
	// wrong email after clone
	f.On("git -C "+dir+" config --show-origin --get user.email", execx.Response{Stdout: "file:/x\tother@x.io\n"})
	if res := s.VerifyClone(bg, p); res.EmailOK || res.EffectiveEmail != "other@x.io" {
		t.Fatalf("%+v", res)
	}
	f.On("git -C "+dir+" config --show-origin --get user.email", execx.Response{ExitCode: 1})
	if res := s.VerifyClone(bg, p); res.EmailOK || res.EffectiveEmail != "" {
		t.Fatalf("%+v", res)
	}
	// auth failure classified
	f2 := execx.NewFake().On("git clone", execx.Response{ExitCode: 128, Stderr: "Permission denied (publickey)."})
	if _, err := app.New(e, f2).Clone(bg, p, nil); !errors.Is(err, gitops.ErrAuth) {
		t.Fatalf("%v", err)
	}
	// terminal mode
	f3 := execx.NewFake().On("git clone", execx.Response{}).On("git -C "+dir+" config", execx.Response{Stdout: "file:/x\talice@acme.com\n"})
	s3 := app.New(e, f3)
	if res, err := s3.CloneTerminal(bg, p); err != nil || !res.EmailOK {
		t.Fatalf("%+v %v", res, err)
	}
	if c := f3.Calls()[0]; len(c.Env) != 0 {
		t.Fatalf("terminal mode must not force batch env: %v", c.Env)
	}
	cmd, err := s3.CloneCmd(p)
	if err != nil || cmd.Name != "git" {
		t.Fatal(err)
	}
	f4 := execx.NewFake().On("git clone", execx.Response{ExitCode: 1})
	if _, err := app.New(e, f4).CloneTerminal(bg, p); err == nil {
		t.Fatal("terminal clone failure must surface")
	}
	// directory appeared between plan and clone
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := s3.CloneTerminal(bg, p); !errors.Is(err, gitops.ErrTargetExists) {
		t.Fatalf("%v", err)
	}
}

func TestCandidates(t *testing.T) {
	s, _ := newSvc(t, nil)
	cfg := fixtures.Full()
	got := s.Candidates(cfg, "https://git.example.com/g/r.git")
	if got[0].String() != "globex/dave" || len(got) != len(cfg.All()) {
		t.Fatalf("%v", got[0])
	}
	got = s.Candidates(cfg, "https://github.com/o/r.git")
	if got[0].Account.Platform != platform.GitHub || got[4].Account.Platform == platform.GitHub {
		t.Fatalf("github accounts must come first")
	}
	got = s.Candidates(cfg, "git@github-acme-carol:o/r.git")
	if got[0].String() != "acme/alice" { // alias resolves to github.com
		t.Fatalf("%v", got[0])
	}
	if got := s.Candidates(cfg, "not a url"); len(got) != len(cfg.All()) {
		t.Fatal("unparsable URL still lists all accounts")
	}
}

// ---- keys, delete, edit ----

func keygen() *execx.Fake {
	return execx.NewFake().On("ssh-keygen", execx.Response{Do: func(c execx.Cmd) error {
		for i, a := range c.Args {
			if a == "-f" {
				p := c.Args[i+1]
				if err := os.WriteFile(p, []byte("PRIVATE"), 0o000); err != nil {
					return err
				}
				return os.WriteFile(p+".pub", []byte("ssh-ed25519 AAAA test\n"), 0o644)
			}
		}
		return errors.New("no -f")
	}})
}

func TestGenerateKey(t *testing.T) {
	s, e := newSvc(t, keygen())
	cfg, _, err := s.Commit(fixtures.Basic())
	if err != nil {
		t.Fatal(err)
	}
	// make the stored key path empty so FinishKey has to record it
	cfg.Companies[0].Accounts[2].SSHKey = ""
	ref, _ := cfg.ParseRef("acme/carol")
	pub, cfg2, err := s.GenerateKey(bg, cfg, ref, false)
	if err != nil {
		t.Fatal(err)
	}
	if pub.Content != "ssh-ed25519 AAAA test" || cfg2.Companies[0].Accounts[2].SSHKey != "~/.ssh/gitid/acme-carol" {
		t.Fatalf("%+v %+v", pub, cfg2.Companies[0].Accounts[2])
	}
	reloaded, _ := s.Load()
	if reloaded.Companies[0].Accounts[2].SSHKey != "~/.ssh/gitid/acme-carol" {
		t.Fatal("ssh_key not persisted")
	}
	// second time: key exists
	ref2, _ := cfg2.ParseRef("acme/carol")
	if _, _, err := s.GenerateKey(bg, cfg2, ref2, true); !errors.Is(err, keys.ErrKeyExists) {
		t.Fatalf("%v", err)
	}
	// https account
	alice, _ := cfg2.ParseRef("acme/alice")
	if _, _, err := s.GenerateKey(bg, cfg2, alice, false); !errors.Is(err, app.ErrNotSSH) {
		t.Fatalf("%v", err)
	}
	// copy + open through the runner
	f := execx.NewFake().On("pbcopy", execx.Response{}).On("open ", execx.Response{})
	s2 := app.New(e, f)
	if err := s2.CopyKey(bg, "ssh-ed25519 AAAA"); err != nil {
		t.Fatal(err)
	}
	if err := s2.OpenKeyURL(bg, ref2.Account); err != nil || f.Count("open https://github.com/settings/ssh/new") != 1 {
		t.Fatalf("%v %v", err, f.CallStrings())
	}
	// keygen failure
	s3 := app.New(e, execx.NewFake().On("ssh-keygen", execx.Response{ExitCode: 1}))
	cfg3 := fixtures.Basic()
	ref3, _ := cfg3.ParseRef("globex/dave")
	if _, _, err := s3.GenerateKey(bg, cfg3, ref3, false); err == nil {
		t.Fatal("expected failure")
	}
}

func TestPrepareKeyAndFinish(t *testing.T) {
	s, _ := newSvc(t, nil)
	cfg := fixtures.Basic()
	ref, _ := cfg.ParseRef("acme/carol")
	cmd, path, err := s.PrepareKey(ref, true)
	if err != nil || cmd.Name != "ssh-keygen" || !strings.HasSuffix(path, ".ssh/gitid/acme-carol") {
		t.Fatalf("%+v %q %v", cmd, path, err)
	}
	if _, _, err := s.FinishKey(cfg, ref); err == nil {
		t.Fatal("FinishKey without key files must fail")
	}
}

func TestDeleteAccount(t *testing.T) { // K9 + default handling
	s, e := newSvc(t, nil)
	cfg, _, _ := s.Commit(fixtures.Basic())
	keyDir := e.Paths().KeysDir
	testutil.WriteFile(t, filepath.Join(keyDir, "acme-carol"), "k", 0o600)
	testutil.WriteFile(t, filepath.Join(keyDir, "acme-carol.pub"), "p", 0o644)
	carol, _ := cfg.ParseRef("acme/carol")
	res, err := s.DeleteAccount(cfg, carol, true)
	if err != nil || len(res.KeysRemoved) != 0 || len(res.Config.Companies[0].Accounts) != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	if res.BackupDir == "" {
		t.Fatal("config.yaml should have been backed up")
	}
	for _, f := range []string{"acme-carol", "acme-carol.pub"} {
		if _, err := os.Stat(filepath.Join(keyDir, f)); err != nil {
			t.Fatalf("%s removed with keepKey=true", f)
		}
	}
	// keepKey=false removes the files
	res, err = s.DeleteAccount(cfg, carol, false)
	if err != nil || len(res.KeysRemoved) != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	for _, f := range []string{"acme-carol", "acme-carol.pub"} {
		if _, err := os.Stat(filepath.Join(keyDir, f)); err == nil {
			t.Fatalf("%s should be gone", f)
		}
	}
	// a key outside ~/.ssh/gitid is refused (config edited by hand)
	other := filepath.Join(e.Home, ".ssh/id_ed25519")
	testutil.WriteFile(t, other, "personal", 0o600)
	cfgHand := fixtures.Basic()
	cfgHand.Companies[0].Accounts[2].SSHKey = "~/.ssh/id_ed25519"
	cfgHand, _, err = s.Commit(cfgHand)
	if err != nil {
		t.Fatal(err)
	}
	ref, _ := cfgHand.ParseRef("acme/carol")
	if _, err := s.DeleteAccount(cfgHand, ref, false); err == nil {
		t.Fatal("deleting a key outside ~/.ssh/gitid must be refused")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("personal key deleted")
	}
	// deleting the default account moves the default; last account leaves none
	cfg, _, _ = s.Commit(fixtures.Basic())
	alice, _ := cfg.ParseRef("acme/alice")
	res, err = s.DeleteAccount(cfg, alice, true)
	if err != nil || res.Config.Companies[0].DefaultAccount != "bob" {
		t.Fatalf("%+v %v", res.Config.Companies[0], err)
	}
	only, _ := res.Config.ParseRef("globex/dave")
	res, err = s.DeleteAccount(res.Config, only, true)
	if err != nil || res.Config.Companies[1].DefaultAccount != "" || len(res.Config.Companies[1].Accounts) != 0 {
		t.Fatalf("%+v %v", res.Config.Companies[1], err)
	}
	// unknown
	if _, err := s.DeleteAccount(cfg, config.AccountRef{Company: fixtures.Acme(), Account: config.Account{ID: "zzz"}}, true); !errors.Is(err, config.ErrRefNotFound) {
		t.Fatal(err)
	}
	if _, err := s.DeleteAccount(cfg, config.AccountRef{Company: config.Company{Slug: "zzz"}}, true); !errors.Is(err, config.ErrRefNotFound) {
		t.Fatal(err)
	}
}

func TestDeleteCompany(t *testing.T) {
	s, e := newSvc(t, nil)
	cfg, _, _ := s.Commit(fixtures.Basic())
	testutil.WriteFile(t, filepath.Join(e.Paths().KeysDir, "acme-carol"), "k", 0o600)
	res, err := s.DeleteCompany(cfg, "acme", true)
	if err != nil || len(res.Config.Companies) != 2 || len(res.KeysRemoved) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(e.Paths().KeysDir, "acme-carol")); err != nil {
		t.Fatal("key removed")
	}
	res, err = s.DeleteCompany(cfg, "acme", false)
	if err != nil || len(res.KeysRemoved) != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(e.Paths().KeysDir, "acme-carol")); err == nil {
		t.Fatal("key should be removed")
	}
	if _, err := s.DeleteCompany(cfg, "zzz", true); !errors.Is(err, config.ErrRefNotFound) {
		t.Fatal(err)
	}
}

func TestEditHelpers(t *testing.T) {
	cfg := fixtures.Basic()
	acc := cfg.Companies[0].Accounts[0]
	acc.Name = "Renamed"
	out, err := app.UpsertAccount(cfg, "acme", acc)
	if err != nil || out.Companies[0].Accounts[0].Name != "Renamed" || cfg.Companies[0].Accounts[0].Name == "Renamed" {
		t.Fatalf("%v (input must not be mutated)", err)
	}
	acc.ID = "newbie"
	out, _ = app.UpsertAccount(cfg, "acme", acc)
	if len(out.Companies[0].Accounts) != 4 || len(cfg.Companies[0].Accounts) != 3 {
		t.Fatal("add")
	}
	if _, err := app.UpsertAccount(cfg, "zzz", acc); !errors.Is(err, config.ErrRefNotFound) {
		t.Fatal(err)
	}
	c := cfg.Companies[0]
	c.Name = "New Name"
	out, err = app.UpdateCompany(cfg, c)
	if err != nil || out.Companies[0].Name != "New Name" || cfg.Companies[0].Name == "New Name" {
		t.Fatal(err)
	}
	c.Slug = "renamed"
	if _, err := app.UpdateCompany(cfg, c); !errors.Is(err, app.ErrImmutableID) {
		t.Fatal(err)
	}
	out = app.AddCompany(cfg, fixtures.Azure())
	if len(out.Companies) != 4 || len(cfg.Companies) != 3 {
		t.Fatal("AddCompany")
	}
}

func TestProposalToCompany(t *testing.T) {
	s, e := newSvc(t, nil)
	testutil.WriteFile(t, e.Paths().GitConfig, "[includeIf \"gitdir:~/Code/work/initech-solutions/\"]\n\tpath = ~/.gitconfig-initech\n", 0o644)
	testutil.WriteFile(t, filepath.Join(e.Home, ".gitconfig-initech"), "[user]\n\tname = Dave\n\temail = x@initech.example\n", 0o644)
	cfg, _ := s.Load()
	r, err := s.Import(cfg)
	if err != nil || len(r.Proposals) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	c := s.ProposalToCompany(cfg, r.Proposals[0], app.ImportChoice{Platform: platform.GitLab, Username: "DaveD", Host: "git.initech.example"})
	if c.Slug != "initech-solutions" || c.Accounts[0].ID != "daved" || c.Accounts[0].Auth != config.AuthHTTPS || c.DefaultAccount != "daved" || c.Accounts[0].Email != "x@initech.example" {
		t.Fatalf("%+v", c)
	}
	saved, _, err := s.Commit(app.AddCompany(cfg, c))
	if err != nil || len(saved.Companies) != 1 {
		t.Fatalf("%v", err)
	}
	// an empty choice fails validation, never a half-written config
	c2 := s.ProposalToCompany(cfg, importer.Proposal{Slug: "x", Name: "x", BaseDir: "~/Code/x/"}, app.ImportChoice{})
	if _, _, err := s.Commit(app.AddCompany(cfg, c2)); err == nil {
		t.Fatal("incomplete import must be rejected")
	}
}

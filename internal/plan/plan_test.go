package plan

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/fsx"
	"github.com/tinhnd1902/gitid/internal/gitcfg"
	"github.com/tinhnd1902/gitid/internal/platform"
	"github.com/tinhnd1902/gitid/internal/testutil"
	"github.com/tinhnd1902/gitid/internal/testutil/fixtures"
)

const userGitconfig = "[user]\n\tname = dave\n\temail = alice@example.com\n[filter \"lfs\"]\n\tclean = git-lfs clean -- %f\n[includeIf \"gitdir:~/Code/work/x/\"]\n\tpath = ~/.gitconfig-x\n"
const userSSHConfig = "Host github.com\n    IdentityFile ~/.ssh/id_ed25519_personal\n    IdentitiesOnly yes\n"

// tick returns an Env whose clock advances 1s per call (distinct backup dirs).
func tick(e env.Env) env.Env {
	n := 0
	e.Now = func() time.Time { n++; return testutil.FixedTime.Add(time.Duration(n) * time.Second) }
	return e
}

func seedUser(t *testing.T, e env.Env) {
	t.Helper()
	testutil.WriteFile(t, e.Paths().GitConfig, userGitconfig, 0o644)
	if err := os.MkdirAll(e.Paths().SSHDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(e.Paths().SSHDir, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, e.Paths().SSHConfig, userSSHConfig, 0o644)
}

func norm(e env.Env, cfg config.Config) config.Config { return config.Normalize(cfg, e) }

func byKind(p Plan, k Kind) []FileChange {
	var out []FileChange
	for _, c := range p.Changes {
		if c.Kind == k {
			out = append(out, c)
		}
	}
	return out
}

func TestP1BuildFresh(t *testing.T) {
	e := testutil.TempEnv(t)
	p, err := Build(e, norm(e, fixtures.Basic()))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Changes) != 11 || len(byKind(p, KindGenerated)) != 8 || len(byKind(p, KindSSHConf)) != 1 || len(byKind(p, KindSSHConfig)) != 1 || len(byKind(p, KindGitConfig)) != 1 {
		t.Fatalf("changes: %s", p.Summary())
	}
	for _, c := range p.Changes {
		if c.Op != fsx.OpCreate || c.Existed {
			t.Errorf("%s should be create", c.Display)
		}
		testutil.Guard(t, e, c.Path, c.RealPath) // P12
	}
	for _, d := range p.Dirs {
		testutil.Guard(t, e, d.Path)
	}
	// apply order: generated first, ~/.gitconfig after ssh, deletes last
	last := p.Changes[len(p.Changes)-1]
	if last.Kind != KindGitConfig {
		t.Fatalf("last = %s", last.Display)
	}
	if p.Empty() || len(p.Paths()) != 11 {
		t.Fatal("Empty/Paths")
	}
}

func TestP2Diff(t *testing.T) {
	e := testutil.TempEnv(t)
	seedUser(t, e)
	cfg := norm(e, fixtures.Basic())
	p, _ := Build(e, cfg)
	d := p.Diff()
	for _, want := range []string{
		"--- /dev/null\n+++ b/~/.config/gitid/git/acme.gitconfig\n",
		"--- a/~/.gitconfig\n+++ b/~/.gitconfig\n",
		"--- a/~/.ssh/config\n+++ b/~/.ssh/config\n",
		"+Include config.d/*\n",
		"+[includeIf \"gitdir:~/Code/acme/\"]\n",
		"+++ b/~/.ssh/config.d/gitid.conf\n",
	} {
		if !strings.Contains(d, want) {
			t.Errorf("diff lacks %q", want)
		}
	}
	p2, _ := Build(e, cfg)
	if p2.Diff() != d {
		t.Fatal("diff not deterministic")
	}
	// delete diff
	ch := FileChange{Display: "~/x", Op: fsx.OpDelete, Old: []byte("a\n")}
	if got := ch.Diff(); !strings.Contains(got, "--- a/~/x\n+++ /dev/null\n") {
		t.Fatal(got)
	}
	if (FileChange{Display: "~/x", Op: fsx.OpUpdate}).Diff() != "" {
		t.Fatal("no-op diff must be empty")
	}
	// exact output for a create: no bogus context line, header counts right
	cr := FileChange{Display: "~/y", Op: fsx.OpCreate, New: []byte("a\nb\n")}
	if got := cr.Diff(); got != "--- /dev/null\n+++ b/~/y\n@@ -0,0 +1,2 @@\n+a\n+b\n" {
		t.Fatalf("%q", got)
	}
	// a missing trailing newline is visible
	nn := FileChange{Display: "~/z", Op: fsx.OpUpdate, Old: []byte("a\nb"), New: []byte("a\nb\n")}
	if got := nn.Diff(); !strings.Contains(got, "-b\n\\ No newline at end of file\n+b\n") {
		t.Fatalf("%q", got)
	}
}

func TestP3DryRunNoWrites(t *testing.T) {
	e := testutil.TempEnv(t)
	seedUser(t, e)
	before := testutil.Snapshot(t, e.Home)
	p, err := Build(e, norm(e, fixtures.Basic()))
	if err != nil {
		t.Fatal(err)
	}
	_ = p.Diff()
	_ = p.Summary()
	testutil.AssertSame(t, before, testutil.Snapshot(t, e.Home))
	for _, d := range []string{e.Paths().BackupsDir, e.Paths().GitDir, e.Paths().ConfigDir, e.Paths().SSHConfigD} {
		if _, err := os.Stat(d); err == nil {
			t.Errorf("%s must not exist after dry run", d)
		}
	}
	// fresh HOME too
	e2 := testutil.TempEnv(t)
	b2 := testutil.Snapshot(t, e2.Home)
	if _, err := Build(e2, norm(e2, fixtures.Basic())); err != nil {
		t.Fatal(err)
	}
	testutil.AssertSame(t, b2, testutil.Snapshot(t, e2.Home))
}

func TestP4Apply(t *testing.T) {
	e := tick(testutil.TempEnv(t))
	seedUser(t, e)
	cfg := norm(e, fixtures.Basic())
	p, _ := Build(e, cfg)
	res, err := Apply(context.Background(), e, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Written) != 11 || res.BackupDir == "" {
		t.Fatalf("%+v", res)
	}
	paths := e.Paths()
	// user content outside the blocks is byte-identical
	g := testutil.ReadFile(t, paths.GitConfig)
	if !strings.HasPrefix(g, userGitconfig) || !strings.Contains(g, gitcfg.Markers.Start) {
		t.Fatalf("gitconfig:\n%s", g)
	}
	s := testutil.ReadFile(t, paths.SSHConfig)
	if !strings.HasSuffix(s, userSSHConfig) || !strings.HasPrefix(s, "# >>>") {
		t.Fatalf("ssh config:\n%s", s)
	}
	// modes
	mode := func(p string) os.FileMode { fi, _ := os.Stat(p); return fi.Mode().Perm() }
	if mode(paths.SSHConfig) != 0o644 || mode(paths.GitConfig) != 0o644 || mode(paths.SSHGitidConf) != 0o600 || mode(filepath.Join(paths.GitDir, "acme.gitconfig")) != 0o600 {
		t.Fatalf("modes: ssh %v git %v conf %v", mode(paths.SSHConfig), mode(paths.GitConfig), mode(paths.SSHGitidConf))
	}
	if mode(paths.SSHDir) != 0o755 || mode(paths.SSHConfigD) != 0o700 || mode(paths.GitDir) != 0o700 || mode(paths.ConfigDir) != 0o700 {
		t.Fatalf("dir modes: %v %v %v", mode(paths.SSHDir), mode(paths.SSHConfigD), mode(paths.GitDir))
	}
	// backup holds the old bytes of pre-existing targets
	ents, _ := os.ReadDir(res.BackupDir)
	var gotG, gotS bool
	for _, en := range ents {
		b, _ := os.ReadFile(filepath.Join(res.BackupDir, en.Name()))
		gotG = gotG || string(b) == userGitconfig
		gotS = gotS || string(b) == userSSHConfig
	}
	if !gotG || !gotS {
		t.Fatalf("backup lacks originals (%v %v): %v", gotG, gotS, ents)
	}
	testutil.Guard(t, e, res.BackupDir)
}

func TestP5ApplyTwiceNoop(t *testing.T) {
	e := tick(testutil.TempEnv(t))
	seedUser(t, e)
	cfg := norm(e, fixtures.Basic())
	p, _ := Build(e, cfg)
	if _, err := Apply(context.Background(), e, p); err != nil {
		t.Fatal(err)
	}
	before := testutil.Snapshot(t, e.Home)
	nBackups := len(mustReadDir(t, e.Paths().BackupsDir))
	p2, err := Build(e, cfg)
	if err != nil || !p2.Empty() {
		t.Fatalf("second build not empty: %v %s", err, p2.Summary())
	}
	res, err := Apply(context.Background(), e, p2)
	if err != nil || res.BackupDir != "" {
		t.Fatalf("%+v %v", res, err)
	}
	if len(mustReadDir(t, e.Paths().BackupsDir)) != nBackups {
		t.Fatal("new backup dir created by a no-op apply")
	}
	testutil.AssertSame(t, before, testutil.Snapshot(t, e.Home))
}

func mustReadDir(t *testing.T, d string) []os.DirEntry {
	t.Helper()
	ents, err := os.ReadDir(d)
	if err != nil {
		t.Fatal(err)
	}
	return ents
}

func TestP6Stale(t *testing.T) {
	e := tick(testutil.TempEnv(t))
	seedUser(t, e)
	p, _ := Build(e, norm(e, fixtures.Basic()))
	testutil.WriteFile(t, e.Paths().GitConfig, userGitconfig+"[core]\n\teditor = vim\n", 0o644)
	before := testutil.Snapshot(t, e.Home)
	_, err := Apply(context.Background(), e, p)
	var se *StaleError
	if !errors.Is(err, ErrStale) || !errors.As(err, &se) || se.Path != "~/.gitconfig" || !strings.Contains(err.Error(), ".gitconfig") {
		t.Fatalf("err=%v", err)
	}
	after := testutil.Snapshot(t, e.Home)
	// the only difference allowed is the lock file created in the config dir
	for _, d := range before.Diff(after) {
		if !strings.Contains(d, ".config") && !strings.HasSuffix(d, ": .") {
			t.Errorf("unexpected change %s", d)
		}
	}
	if _, err := os.Stat(e.Paths().BackupsDir); err == nil {
		t.Fatal("backup made despite stale plan")
	}
	// a created file appearing is stale too
	e2 := tick(testutil.TempEnv(t))
	p2, _ := Build(e2, norm(e2, fixtures.Basic()))
	testutil.WriteFile(t, e2.Paths().SSHConfig, "Host x\n", 0o600)
	if _, err := Apply(context.Background(), e2, p2); !errors.Is(err, ErrStale) {
		t.Fatalf("err=%v", err)
	}
}

func TestP7GitconfigLock(t *testing.T) {
	e := tick(testutil.TempEnv(t))
	seedUser(t, e)
	p, _ := Build(e, norm(e, fixtures.Basic()))
	testutil.WriteFile(t, e.Paths().GitConfig+".lock", "", 0o644)
	before := testutil.Snapshot(t, e.Home)
	if _, err := Apply(context.Background(), e, p); !errors.Is(err, ErrGitConfigLocked) {
		t.Fatalf("err=%v", err)
	}
	for _, d := range before.Diff(testutil.Snapshot(t, e.Home)) {
		if !strings.Contains(d, ".config") && !strings.HasSuffix(d, ": .") {
			t.Errorf("unexpected change %s", d)
		}
	}
}

func TestProcessLock(t *testing.T) {
	e := tick(testutil.TempEnv(t))
	p, _ := Build(e, norm(e, fixtures.Basic()))
	l, err := fsx.AcquireLock(e.Paths().LockFile)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	if _, err := Apply(context.Background(), e, p); !errors.Is(err, ErrLocked) {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(e.Paths().GitConfig); err == nil {
		t.Fatal("written despite lock")
	}
}

func TestP8Orphans(t *testing.T) {
	e := tick(testutil.TempEnv(t))
	seedUser(t, e)
	cfg := norm(e, fixtures.Basic())
	p, _ := Build(e, cfg)
	if _, err := Apply(context.Background(), e, p); err != nil {
		t.Fatal(err)
	}
	gd := e.Paths().GitDir
	testutil.WriteFile(t, filepath.Join(gd, "notes.txt"), "mine", 0o600)
	testutil.WriteFile(t, filepath.Join(gd, "mine.gitconfig"), "[user]\n\tname = hand written\n", 0o600)
	testutil.WriteFile(t, filepath.Join(e.Home, "outside.gitconfig"), gitcfg.Header+"\n", 0o600)
	cfg.Companies[0].Accounts = append(cfg.Companies[0].Accounts[:1], cfg.Companies[0].Accounts[2:]...) // drop bob
	p, err := Build(e, cfg)
	if err != nil {
		t.Fatal(err)
	}
	var del []string
	for _, c := range p.Changes {
		if c.Op == fsx.OpDelete {
			del = append(del, filepath.Base(c.Path))
		}
	}
	if strings.Join(del, ",") != "acme-bob.gitconfig" {
		t.Fatalf("deletes: %v\n%s", del, p.Summary())
	}
	res, err := Apply(context.Background(), e, p)
	if err != nil || len(res.Deleted) != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	for _, keep := range []string{filepath.Join(gd, "notes.txt"), filepath.Join(gd, "mine.gitconfig"), filepath.Join(e.Home, "outside.gitconfig")} {
		if _, err := os.Stat(keep); err != nil {
			t.Errorf("%s was deleted", keep)
		}
	}
	if _, err := os.Stat(filepath.Join(gd, "acme-bob.gitconfig")); err == nil {
		t.Fatal("orphan not deleted")
	}
}

func TestP9RemoveAllRestoresOriginals(t *testing.T) {
	e := tick(testutil.TempEnv(t))
	seedUser(t, e)
	cfg := norm(e, fixtures.Full())
	p, _ := Build(e, cfg)
	if _, err := Apply(context.Background(), e, p); err != nil {
		t.Fatal(err)
	}
	p, err := Build(e, config.Default())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), e, p); err != nil {
		t.Fatal(err)
	}
	if testutil.ReadFile(t, e.Paths().GitConfig) != userGitconfig || testutil.ReadFile(t, e.Paths().SSHConfig) != userSSHConfig {
		t.Fatal("original bytes not restored")
	}
	ents, _ := os.ReadDir(e.Paths().GitDir)
	if len(ents) != 0 {
		t.Fatalf("generated files remain: %v", ents)
	}
	if _, err := os.Stat(e.Paths().SSHGitidConf); err == nil {
		t.Fatal("gitid.conf remains")
	}
	pf, _ := Build(e, config.Default())
	if !pf.Empty() {
		t.Fatalf("not clean: %s", pf.Summary())
	}
}

func TestSSHAccountsRemovedButGitKept(t *testing.T) {
	e := tick(testutil.TempEnv(t))
	seedUser(t, e)
	cfg := norm(e, fixtures.Basic())
	p, _ := Build(e, cfg)
	_, _ = Apply(context.Background(), e, p)
	cfg2 := norm(e, config.Config{Version: 1, Companies: []config.Company{fixtures.Beta()}})
	p, err := Build(e, cfg2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), e, p); err != nil {
		t.Fatal(err)
	}
	if testutil.ReadFile(t, e.Paths().SSHConfig) != userSSHConfig {
		t.Fatal("ssh include not removed")
	}
	if _, err := os.Stat(e.Paths().SSHGitidConf); err == nil {
		t.Fatal("gitid.conf should be deleted")
	}
	if !strings.Contains(testutil.ReadFile(t, e.Paths().GitConfig), "beta.gitconfig") {
		t.Fatal("beta missing")
	}
	// foreign gitid.conf is never deleted
	testutil.WriteFile(t, e.Paths().SSHGitidConf, "Host mine\n", 0o600)
	p, _ = Build(e, cfg2)
	if len(byKind(p, KindSSHConf)) != 0 || len(p.Warnings) != 1 {
		t.Fatalf("%s %v", p.Summary(), p.Warnings)
	}
}

func TestP10PartialFailure(t *testing.T) {
	e := tick(testutil.TempEnv(t))
	seedUser(t, e)
	p, _ := Build(e, norm(e, fixtures.Basic()))
	n := 0
	writeFn = func(path string, data []byte, mode os.FileMode) error {
		n++
		if n == 3 {
			return errors.New("disk full")
		}
		return fsx.WriteAtomic(path, data, mode)
	}
	defer func() { writeFn = fsx.WriteAtomic }()
	res, err := Apply(context.Background(), e, p)
	var pe *PartialError
	if !errors.As(err, &pe) || len(pe.Written) != 2 || pe.BackupDir == "" || !strings.Contains(err.Error(), pe.BackupDir) {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if testutil.ReadFile(t, e.Paths().GitConfig) != userGitconfig {
		t.Fatal("later files must be untouched")
	}
	// removal failure
	removeFn = func(string) error { return errors.New("busy") }
	defer func() { removeFn = os.Remove }()
	writeFn = fsx.WriteAtomic
	e2 := tick(testutil.TempEnv(t))
	pp, _ := Build(e2, norm(e2, fixtures.Basic()))
	_, _ = Apply(context.Background(), e2, pp)
	cfg := norm(e2, fixtures.Basic())
	cfg.Companies = cfg.Companies[:1]
	pp, _ = Build(e2, cfg)
	if _, err := Apply(context.Background(), e2, pp); err == nil {
		t.Fatal("expected removal error")
	}
}

func TestP11XDG(t *testing.T) {
	e := tick(testutil.TempEnv(t))
	e.XDGConfigHome = filepath.Join(e.Home, "xdg")
	cfg := norm(e, fixtures.Basic())
	p, _ := Build(e, cfg)
	if _, err := Apply(context.Background(), e, p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.Home, "xdg/gitid/git/acme.gitconfig")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(testutil.ReadFile(t, e.Paths().GitConfig), "path = ~/xdg/gitid/git/acme.gitconfig") {
		t.Fatal("block does not reference XDG location")
	}
	if _, err := os.Stat(filepath.Join(e.Home, ".config")); err == nil {
		t.Fatal("~/.config must not be created when XDG is set")
	}
}

func TestSymlinkedGitconfig(t *testing.T) {
	e := tick(testutil.TempEnv(t))
	target := filepath.Join(e.Home, "dotfiles/gitconfig")
	testutil.WriteFile(t, target, userGitconfig, 0o644)
	if err := os.Symlink(target, e.Paths().GitConfig); err != nil {
		t.Fatal(err)
	}
	p, _ := Build(e, norm(e, fixtures.Basic()))
	if _, err := Apply(context.Background(), e, p); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(e.Paths().GitConfig); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink replaced")
	}
	if !strings.Contains(testutil.ReadFile(t, target), gitcfg.Markers.Start) {
		t.Fatal("target not updated")
	}
	// lock file check follows the symlink
	testutil.WriteFile(t, target+".lock", "", 0o644)
	p, _ = Build(e, config.Default())
	if _, err := Apply(context.Background(), e, p); !errors.Is(err, ErrGitConfigLocked) {
		t.Fatalf("err=%v", err)
	}
}

func TestBuildErrors(t *testing.T) {
	e := testutil.TempEnv(t)
	testutil.WriteFile(t, e.Paths().GitConfig, gitcfg.Markers.Start+"\n", 0o644)
	if _, err := Build(e, norm(e, fixtures.Basic())); err == nil || !strings.Contains(err.Error(), ".gitconfig") {
		t.Fatalf("corrupt gitconfig block: %v", err)
	}
	e = testutil.TempEnv(t)
	testutil.WriteFile(t, e.Paths().SSHConfig, "# >>> gitid managed block (do not edit; run `gitid apply`) >>>\n", 0o600)
	if _, err := Build(e, norm(e, fixtures.Basic())); err == nil {
		t.Fatal("corrupt ssh block")
	}
	if _, err := Build(e, config.Default()); err == nil {
		t.Fatal("corrupt ssh block must also fail when removing")
	}
	e = testutil.TempEnv(t)
	cfg := norm(e, fixtures.Basic())
	cfg.Companies[0].Accounts[0].Name = "bad\nname"
	if _, err := Build(e, cfg); err == nil {
		t.Fatal("unsafe name")
	}
	cfg = norm(e, fixtures.Basic())
	cfg.Companies[0].Accounts[2].SSHHost = "evil\nhost"
	if _, err := Build(e, cfg); err == nil {
		t.Fatal("unsafe ssh host")
	}
	cfg = norm(e, fixtures.Basic())
	cfg.Companies[0].Accounts[2].ID = "c\"x"
	if _, err := Build(e, cfg); err == nil {
		t.Fatal("unsafe pattern")
	}
}

func TestExternalSSHInclude(t *testing.T) {
	e := tick(testutil.TempEnv(t))
	user := "Include ~/.ssh/config.d/*\n" + userSSHConfig
	testutil.WriteFile(t, e.Paths().SSHConfig, user, 0o600)
	p, _ := Build(e, norm(e, fixtures.Basic()))
	if len(byKind(p, KindSSHConfig)) != 0 {
		t.Fatal("ssh config must not change when the user already includes config.d")
	}
	if _, err := Apply(context.Background(), e, p); err != nil {
		t.Fatal(err)
	}
	if testutil.ReadFile(t, e.Paths().SSHConfig) != user {
		t.Fatal("changed")
	}
}

func TestSSHIncludeScopedWarning(t *testing.T) {
	e := testutil.TempEnv(t)
	testutil.WriteFile(t, e.Paths().SSHConfig, "Host x\n    Include config.d/*\n", 0o600)
	p, _ := Build(e, norm(e, fixtures.Basic()))
	if len(p.Warnings) != 1 {
		t.Fatalf("%v", p.Warnings)
	}
}

func TestPruneAfterApply(t *testing.T) {
	e := tick(testutil.TempEnv(t))
	for i := 0; i < 35; i++ {
		name := time.Date(2025, 1, 1, 0, 0, i, 0, time.UTC).Format(fsx.BackupDirTime)
		if err := os.MkdirAll(filepath.Join(e.Paths().BackupsDir, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	p, _ := Build(e, norm(e, fixtures.Basic()))
	if _, err := Apply(context.Background(), e, p); err != nil {
		t.Fatal(err)
	}
	if n := len(mustReadDir(t, e.Paths().BackupsDir)); n != KeepBackups {
		t.Fatalf("backups kept: %d", n)
	}
}

func TestApplyCancelled(t *testing.T) {
	e := tick(testutil.TempEnv(t))
	p, _ := Build(e, norm(e, fixtures.Basic()))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Apply(ctx, e, p); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(e.Paths().GitConfig); err == nil {
		t.Fatal("written after cancel")
	}
}

func TestUserHostPatterns(t *testing.T) {
	e := testutil.TempEnv(t)
	if got, err := UserHostPatterns(e); err != nil || got != nil {
		t.Fatalf("%v %v", got, err)
	}
	seedUser(t, e)
	got, err := UserHostPatterns(e)
	if err != nil || len(got) != 1 || got[0] != "github.com" {
		t.Fatalf("%v %v", got, err)
	}
}

func TestAzureAndMixedAccounts(t *testing.T) {
	e := tick(testutil.TempEnv(t))
	cfg := norm(e, fixtures.Full())
	if errs := config.Validate(cfg, e, config.ValidateOptions{}); len(errs) != 0 {
		t.Fatal(errs)
	}
	p, err := Build(e, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), e, p); err != nil {
		t.Fatal(err)
	}
	conf := testutil.ReadFile(t, e.Paths().SSHGitidConf)
	if !strings.Contains(conf, "Host azure-devops-az-devssh") || !strings.Contains(conf, "HostName ssh.dev.azure.com") {
		t.Fatal(conf)
	}
	_ = platform.Gitea
}

func BenchmarkBuild100(b *testing.B) { // N1: 100 accounts under 20 ms
	e := testutil.TempEnv(b)
	cfg := config.Default()
	for i := 0; i < 10; i++ {
		c := config.Company{Slug: fmt.Sprintf("c%d", i), Name: "C", BaseDir: fmt.Sprintf("~/Code/c%d/", i), DefaultAccount: "a0"}
		for j := 0; j < 10; j++ {
			auth := config.AuthHTTPS
			if j%3 == 0 {
				auth = config.AuthSSH
			}
			c.Accounts = append(c.Accounts, config.Account{ID: fmt.Sprintf("a%d", j), Platform: platform.GitHub, Username: fmt.Sprintf("u%d-%d", i, j), Name: "N", Email: "n@x.io", Auth: auth})
		}
		cfg.Companies = append(cfg.Companies, c)
	}
	cfg = config.Normalize(cfg, e)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Build(e, cfg); err != nil {
			b.Fatal(err)
		}
	}
}

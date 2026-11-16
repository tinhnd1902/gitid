//go:build integration

package app_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/gitops"
	"github.com/tinhnd1902/gitid/internal/platform"
	"github.com/tinhnd1902/gitid/internal/testutil"
	"github.com/tinhnd1902/gitid/internal/testutil/fixtures"
)

// End to end with real git (offline): apply through the real plan pipeline,
// then check, fix-remote and a local clone, all inside a temp HOME.
func TestIntegrationApplyCheckFixClone(t *testing.T) {
	e := testutil.TempEnv(t) // also points HOME / GIT_CONFIG_GLOBAL at the temp dir
	testutil.WriteFile(t, e.Paths().GitConfig, "[user]\n\tname = Global\n\temail = global@x.io\n", 0o644)
	svc := app.New(e, execx.OSRunner{})
	cfg := fixtures.Basic()
	cfg.Companies[0].Accounts = append(cfg.Companies[0].Accounts, config.Account{
		ID: "ab", Platform: platform.GitHub, Username: "a.b@corp.com", Name: "AB", Email: "ab@corp.com", Auth: config.AuthHTTPS,
	})
	cfg, _, err := svc.Commit(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p, err := svc.BuildPlan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if again, _ := svc.BuildPlan(cfg); !again.Empty() {
		t.Fatalf("second plan not empty: %s", again.Summary())
	}

	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	mk := func(rel string, remotes ...string) string {
		dir := filepath.Join(e.Home, rel)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		git(dir, "init", "-q")
		for i := 0; i+1 < len(remotes); i += 2 {
			git(dir, "remote", "add", remotes[i], remotes[i+1])
		}
		return dir
	}
	cases := []struct {
		rel     string
		remotes []string
		want    string
	}{
		{"Code/acme/r1", nil, "alice@acme.com"},
		{"Code/acme/r2", []string{"origin", "https://bob@github.com/o/r.git"}, "bob@acme.com"},
		{"Code/acme/r3", []string{"origin", "git@github-acme-carol:o/r.git"}, "carol@acme.com"},
		{"Code/beta/r1", []string{"origin", "https://alice@github.com/o/r.git"}, "alice@beta.io"},
		{"Code/acme/r4", []string{"origin", "https://alice@github.com/o/r.git"}, "alice@acme.com"},
		{"outside/r1", []string{"origin", "git@github-acme-carol:o/r.git"}, "carol@acme.com"},
		{"outside/r2", []string{"origin", "https://alice@github.com/o/r.git"}, "global@x.io"},
		{"Code/acme/r5", []string{"origin", "https://a.b%40corp.com@github.com/o/r.git"}, "ab@corp.com"},
		{"Code/globex/r1", nil, "dave@globex.example"},
	}
	for _, c := range cases {
		dir := mk(c.rel, c.remotes...)
		if got := git(dir, "config", "user.email"); got != c.want {
			t.Errorf("%s: user.email = %q want %q", c.rel, got, c.want)
		}
	}

	// check: a bob repo is fine, a repo whose email is forced locally warns
	bob := filepath.Join(e.Home, "Code/acme/r2")
	res, err := svc.Check(context.Background(), cfg, bob)
	if err != nil || !res.OK() || res.Account != "acme/bob" {
		t.Fatalf("%+v %v", res, err)
	}
	git(bob, "config", "user.email", "forced@x.io")
	res, _ = svc.Check(context.Background(), cfg, bob)
	if res.OK() || res.EmailOrigin == "" {
		t.Fatalf("local override not detected: %+v", res)
	}
	git(bob, "config", "--unset", "user.email")

	// fix-remote on a repo with an anonymous https remote
	anon := mk("Code/acme/r6", "origin", "https://github.com/o/r.git")
	fr, err := svc.FixRemote(context.Background(), cfg, app.FixRequest{Dir: anon, Account: "acme/bob"})
	if err != nil || !fr.Changed {
		t.Fatalf("%+v %v", fr, err)
	}
	if got := git(anon, "remote", "get-url", "origin"); got != "https://bob@github.com/o/r.git" {
		t.Fatalf("remote = %q", got)
	}
	if got := git(anon, "config", "user.email"); got != "bob@acme.com" {
		t.Fatalf("email after fix = %q", got)
	}

	// O5: clone from a local bare repository; the post-clone email check
	// must give the account's email. (file:// is used here directly through
	// gitops; the app layer only accepts https/ssh remotes.)
	bare := filepath.Join(e.Home, "src.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", bare).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	target := filepath.Join(e.Home, "Code/acme/cloned")
	if err := gitops.Clone(context.Background(), execx.OSRunner{}, "file://"+bare, target, nil); err != nil {
		t.Fatal(err)
	}
	vr := svc.VerifyClone(context.Background(), app.ClonePlan{Account: mustRef(t, cfg, "acme/alice"), Dir: target})
	if !vr.EmailOK || vr.EffectiveEmail != "alice@acme.com" {
		t.Fatalf("%+v", vr)
	}
	// and an existing directory is refused before git runs
	if err := gitops.Clone(context.Background(), execx.OSRunner{}, "file://"+bare, target, nil); err == nil {
		t.Fatal("clone into an existing directory must fail")
	}
}

func mustRef(t *testing.T, cfg config.Config, s string) config.AccountRef {
	t.Helper()
	r, err := cfg.ParseRef(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

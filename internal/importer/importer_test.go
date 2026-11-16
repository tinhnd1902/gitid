package importer_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinhnd1902/gitid/internal/block"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/gitcfg"
	"github.com/tinhnd1902/gitid/internal/importer"
	"github.com/tinhnd1902/gitid/internal/testutil"
	"github.com/tinhnd1902/gitid/internal/testutil/fixtures"
)

const observedGitconfig = `[user]
	name = dave
	email = alice@example.com
[filter "lfs"]
	clean = git-lfs clean -- %f
	process = git-lfs filter-process
[includeIf "gitdir:~/Code/work/main-job/initech-solutions/"]
	path = ~/.gitconfig-initech
`

const observedSSH = "Host github.com\n    IdentityFile ~/.ssh/id_ed25519_personal\n    IdentitiesOnly yes\n"

func TestI1Proposal(t *testing.T) {
	e := testutil.TempEnv(t)
	testutil.WriteFile(t, e.Paths().GitConfig, observedGitconfig, 0o644)
	testutil.WriteFile(t, filepath.Join(e.Home, ".gitconfig-initech"), "[user]\n\tname = Dana Initech\n\temail = x@initech.example\n", 0o644)
	r, err := importer.Import(e, config.Default())
	if err != nil || len(r.Proposals) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	p := r.Proposals[0]
	if p.Slug != "initech-solutions" || p.BaseDir != "~/Code/work/main-job/initech-solutions/" || p.UserName != "Dana Initech" || p.UserEmail != "x@initech.example" || p.Source != "~/.gitconfig-initech" || p.Name != "initech-solutions" || len(p.Warnings) != 0 {
		t.Fatalf("%+v", p)
	}
}

func TestI2MissingTarget(t *testing.T) {
	e := testutil.TempEnv(t)
	testutil.WriteFile(t, e.Paths().GitConfig, observedGitconfig, 0o644)
	r, _ := importer.Import(e, config.Default())
	if len(r.Proposals) != 1 || len(r.Proposals[0].Warnings) != 1 || r.Proposals[0].Warnings[0] != importer.WarnTargetMissing {
		t.Fatalf("%+v", r)
	}
	testutil.WriteFile(t, filepath.Join(e.Home, ".gitconfig-initech"), "[core]\n", 0o644)
	r, _ = importer.Import(e, config.Default())
	if r.Proposals[0].Warnings[0] != importer.WarnNoUser {
		t.Fatalf("%+v", r.Proposals[0].Warnings)
	}
}

func TestI3HostsInfoOnlyAndKeysNotOpened(t *testing.T) {
	e := testutil.TempEnv(t)
	testutil.WriteFile(t, e.Paths().SSHConfig, observedSSH+"\nHost *\n  ForwardAgent no\n", 0o644)
	key := filepath.Join(e.Home, ".ssh/id_ed25519_personal")
	testutil.WriteFile(t, key, "SECRET", 0o000)
	r, err := importer.Import(e, config.Default())
	if err != nil || len(r.Hosts) != 1 || r.Hosts[0].Patterns[0] != "github.com" || r.Hosts[0].IdentityFile[0] != "~/.ssh/id_ed25519_personal" {
		t.Fatalf("%+v %v", r, err)
	}
	_ = os.Chmod(key, 0o600)
}

func TestI4NoWrites(t *testing.T) {
	e := testutil.TempEnv(t)
	testutil.WriteFile(t, e.Paths().GitConfig, observedGitconfig, 0o644)
	testutil.WriteFile(t, filepath.Join(e.Home, ".gitconfig-initech"), "[user]\n\tname = X\n", 0o644)
	testutil.WriteFile(t, e.Paths().SSHConfig, observedSSH, 0o644)
	before := testutil.Snapshot(t, e.Home)
	if _, err := importer.Import(e, config.Default()); err != nil {
		t.Fatal(err)
	}
	testutil.AssertSame(t, before, testutil.Snapshot(t, e.Home))
	// empty HOME works too
	e2 := testutil.TempEnv(t)
	r, err := importer.Import(e2, config.Default())
	if err != nil || len(r.Proposals) != 0 || len(r.Hosts) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestI5ManagedBlockIgnoredAndDedupe(t *testing.T) {
	e := testutil.TempEnv(t)
	body, _ := gitcfg.RenderManagedBody(e, fixtures.Basic())
	text, _ := block.Upsert(observedGitconfig, body, gitcfg.Markers, block.AtEnd)
	testutil.WriteFile(t, e.Paths().GitConfig, text, 0o644)
	r, _ := importer.Import(e, fixtures.Basic())
	if len(r.Proposals) != 1 || r.Proposals[0].Slug != "initech-solutions" {
		t.Fatalf("block entries leaked: %+v", r.Proposals)
	}
}

func TestSlugHandlingAndRelativePaths(t *testing.T) {
	e := testutil.TempEnv(t)
	gc := `[includeIf "gitdir:~/Code/My Work_2/"]
	path = inc/a.gitconfig
[includeIf "gitdir:~/Code/acme/"]
	path = ~/x
[includeIf "gitdir/i:~/Code/acme/sub/"]
[includeIf "gitdir:relative/glob/**"]
	path = ~/y
[includeIf "hasconfig:remote.*.url:**"]
	path = ~/z
[includeIf "gitdir:/opt/work/ACME/"]
	path = ~/w
[includeIf "gitdir:~/Code/---/"]
	path = ~/w
`
	testutil.WriteFile(t, e.Paths().GitConfig, gc, 0o644)
	testutil.WriteFile(t, filepath.Join(e.Home, "inc/a.gitconfig"), "[user]\n\temail = a@b.c\n", 0o644)
	r, _ := importer.Import(e, fixtures.Basic())
	var slugs []string
	for _, p := range r.Proposals {
		slugs = append(slugs, p.Slug)
	}
	// acme exists already: acme-2 (flagged as configured); the /opt one becomes acme-3
	if got := strings.Join(slugs, ","); got != "my-work-2,acme-2,sub,acme-3,company" {
		t.Fatalf("slugs: %v", slugs)
	}
	first := r.Proposals[0]
	if first.UserEmail != "a@b.c" || first.Warnings[0] != importer.WarnSlugAdjusted {
		t.Fatalf("relative include path not resolved: %+v", first)
	}
	cfgd := false
	for _, w := range r.Proposals[1].Warnings {
		cfgd = cfgd || w == importer.WarnConfigured
	}
	if !cfgd {
		t.Fatalf("already configured not flagged: %+v", r.Proposals[1])
	}
	if r.Proposals[2].Warnings[len(r.Proposals[2].Warnings)-1] != importer.WarnNoPath {
		t.Fatalf("%+v", r.Proposals[2])
	}
	for _, p := range r.Proposals {
		if errs := config.ValidateField(e, config.FieldSlug, p.Slug); errs != "" {
			t.Errorf("slug %q invalid", p.Slug)
		}
	}
	if len(importer.AllWarnings()) != 5 {
		t.Fatal("warnings")
	}
}

func TestLongSlug(t *testing.T) {
	e := testutil.TempEnv(t)
	testutil.WriteFile(t, e.Paths().GitConfig, "[includeIf \"gitdir:~/Code/"+strings.Repeat("ab-", 20)+"/\"]\n\tpath = ~/x\n", 0o644)
	r, _ := importer.Import(e, config.Default())
	if len(r.Proposals) != 1 || len(r.Proposals[0].Slug) > 32 || strings.HasSuffix(r.Proposals[0].Slug, "-") {
		t.Fatalf("%+v", r.Proposals)
	}
}

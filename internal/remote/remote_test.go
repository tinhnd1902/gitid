package remote_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/platform"
	"github.com/tinhnd1902/gitid/internal/remote"
	"github.com/tinhnd1902/gitid/internal/testutil"
	"github.com/tinhnd1902/gitid/internal/testutil/fixtures"
)

func testCfg() config.Config {
	cfg := fixtures.Full()
	cfg.Companies = append(cfg.Companies, config.Company{
		Slug: "gl", Name: "GL", BaseDir: "~/Code/gl/", DefaultAccount: "g",
		Accounts: []config.Account{
			{ID: "g", Platform: platform.GitLab, Username: "glu", Name: "G", Email: "g@gl.io", Auth: config.AuthHTTPS},
			{ID: "ab", Platform: platform.GitHub, Username: "a.b@corp.com", Name: "AB", Email: "ab@corp.com", Auth: config.AuthHTTPS},
		},
	}, config.Company{
		Slug: "loc", Name: "Local", BaseDir: "~/Code/loc/", DefaultAccount: "h",
		Accounts: []config.Account{{ID: "h", Platform: platform.Gitea, Host: "git.local", Username: "hu", Name: "H", Email: "h@l.io", Auth: config.AuthHTTPS}},
	})
	return cfg
}

func ref(t *testing.T, cfg config.Config, s string) config.AccountRef {
	t.Helper()
	r, err := cfg.ParseRef(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRewriteTable(t *testing.T) { // R1..R25
	cfg := testCfg()
	cases := []struct{ name, in, acct, want string }{
		{"R1", "https://github.com/org/repo.git", "acme/alice", "https://alice@github.com/org/repo.git"},
		{"R2", "https://github.com/org/repo", "acme/alice", "https://alice@github.com/org/repo"},
		{"R3", "https://olduser@github.com/org/repo.git", "acme/bob", "https://bob@github.com/org/repo.git"},
		{"R4", "https://alice:ghp_SECRET@github.com/org/repo.git", "acme/alice", "https://alice@github.com/org/repo.git"},
		{"R5", "git@github.com:org/repo.git", "acme/carol", "git@github-acme-carol:org/repo.git"},
		{"R6", "ssh://git@github.com/org/repo.git", "acme/alice", "https://alice@github.com/org/repo.git"},
		{"R7", "git@github-acme-carol:org/repo.git", "acme/alice", "https://alice@github.com/org/repo.git"},
		{"R8", "https://GitHub.com/Org/Repo.git/", "acme/alice", "https://alice@github.com/Org/Repo.git"},
		{"R9", "ssh://git@git.example.com:2222/group/sub/repo.git", "globex/dave", "git@gitlab-globex-dave:group/sub/repo.git"},
		{"R10", "https://git.example.com/group/sub/repo.git", "globex/dave", "git@gitlab-globex-dave:group/sub/repo.git"},
		{"R11", "https://gitlab.com/a/b/c/d.git", "gl/g", "https://glu@gitlab.com/a/b/c/d.git"},
		{"R12", "https://joe@bitbucket.org/ws/repo.git", "bb/joe", "https://joe@bitbucket.org/ws/repo.git"},
		{"R13", "https://dev.azure.com/org/proj/_git/repo", "az/dev", "https://dev@dev.azure.com/org/proj/_git/repo"},
		{"R14", "https://org@dev.azure.com/org/proj/_git/repo", "az/dev", "https://dev@dev.azure.com/org/proj/_git/repo"},
		{"R15", "https://org.visualstudio.com/proj/_git/repo", "az/dev", "https://dev@dev.azure.com/org/proj/_git/repo"},
		{"R16", "https://org.visualstudio.com/DefaultCollection/proj/_git/repo", "az/dev", "https://dev@dev.azure.com/org/proj/_git/repo"},
		{"R17", "git@ssh.dev.azure.com:v3/org/proj/repo", "az/devssh", "git@azure-devops-az-devssh:v3/org/proj/repo"},
		{"R18", "ssh://git@ssh.dev.azure.com/v3/org/proj/repo", "az/dev", "https://dev@dev.azure.com/org/proj/_git/repo"},
		{"R19", "org@vs-ssh.visualstudio.com:v3/org/proj/repo", "az/devssh", "git@azure-devops-az-devssh:v3/org/proj/repo"},
		{"R20a", "https://dev.azure.com/org/My%20Project/_git/repo", "az/dev", "https://dev@dev.azure.com/org/My%20Project/_git/repo"},
		{"R20b", "https://dev.azure.com/org/My%20Project/_git/repo", "az/devssh", "git@azure-devops-az-devssh:v3/org/My%20Project/repo"},
		{"R21a", "https://gitea.example.com/owner/repo.git", "gt/sam", "https://sam@gitea.example.com/owner/repo.git"},
		{"R21b", "git@gitea.example.com:owner/repo.git", "gt/sam", "https://sam@gitea.example.com/owner/repo.git"},
		{"R23", "https://github.com/org/repo.git", "gl/ab", "https://a.b%40corp.com@github.com/org/repo.git"},
		{"R24", "  https://github.com/org/repo.git \n", "acme/alice", "https://alice@github.com/org/repo.git"},
		{"R25", "http://git.local/o/r.git", "loc/h", "http://hu@git.local/o/r.git"},
		{"alias to ssh account", "git@github-acme-carol:org/repo.git", "acme/carol", "git@github-acme-carol:org/repo.git"},
		{"bare alias scp", "github-acme-carol:org/repo.git", "acme/carol", "git@github-acme-carol:org/repo.git"},
		{"ssh:// alias", "ssh://github-acme-carol/org/repo.git", "acme/carol", "git@github-acme-carol:org/repo.git"},
		{"azure alias to https acct", "git@azure-devops-az-devssh:v3/org/proj/repo", "az/dev", "https://dev@dev.azure.com/org/proj/_git/repo"},
		{"azure short https", "https://dev.azure.com/org/_git/repo", "az/dev", "https://dev@dev.azure.com/org/repo/_git/repo"},
		{"azure legacy short", "https://org.visualstudio.com/_git/repo", "az/dev", "https://dev@dev.azure.com/org/repo/_git/repo"},
		{"git suffix twice", "https://github.com/o/r.git.git", "acme/alice", "https://alice@github.com/o/r.git.git"},
	}
	for _, c := range cases {
		got, r, err := remote.Rewrite(cfg, ref(t, cfg, c.acct), c.in, remote.Options{})
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
		if strings.Contains(got, "SECRET") || strings.Contains(r.String(), "SECRET") {
			t.Errorf("%s: secret leaked", c.name)
		}
	}
}

func TestR4PasswordMasked(t *testing.T) {
	r, err := remote.Parse("https://alice:ghp_SECRET@github.com/org/repo.git")
	if err != nil || !r.HadPassword {
		t.Fatalf("%+v %v", r, err)
	}
	if strings.Contains(r.String(), "SECRET") || !strings.Contains(r.String(), "alice:***@") {
		t.Fatal(r.String())
	}
	hasWarn := false
	for _, w := range r.Warnings {
		hasWarn = hasWarn || w == remote.WarnPassword
	}
	if !hasWarn {
		t.Fatal("no password warning")
	}
	if got := remote.Redact("fatal: unable to access 'https://u:tok@h/o/r.git/' and https://x@h/o"); strings.Contains(got, "tok") || !strings.Contains(got, "https://x@h/o") {
		t.Fatal(got)
	}
}

func TestR22HostMismatch(t *testing.T) {
	cfg := testCfg()
	_, _, err := remote.Rewrite(cfg, ref(t, cfg, "acme/alice"), "https://gitlab.com/org/repo.git", remote.Options{})
	if !errors.Is(err, remote.ErrHostMismatch) {
		t.Fatalf("err=%v", err)
	}
	got, _, err := remote.Rewrite(cfg, ref(t, cfg, "acme/alice"), "https://gitlab.com/org/repo.git", remote.Options{Force: true})
	if err != nil || got != "https://alice@github.com/org/repo.git" {
		t.Fatalf("force: %q %v", got, err)
	}
	// azure remote against non-azure account
	if _, _, err := remote.Rewrite(cfg, ref(t, cfg, "acme/alice"), "https://dev.azure.com/o/p/_git/r", remote.Options{}); !errors.Is(err, remote.ErrHostMismatch) {
		t.Fatalf("err=%v", err)
	}
	// port mismatch on https
	if _, _, err := remote.Rewrite(cfg, ref(t, cfg, "acme/alice"), "https://github.com:8443/o/r.git", remote.Options{}); !errors.Is(err, remote.ErrHostMismatch) {
		t.Fatalf("err=%v", err)
	}
}

func TestParseFields(t *testing.T) {
	r, _ := remote.Parse("https://git.example.com/group/sub/repo.git")
	if r.Owner != "group/sub" || r.Repo != "repo" || !r.HadGitSuffix || r.Kind != remote.KindHTTPS {
		t.Fatalf("%+v", r)
	}
	for in, repo := range map[string]string{
		"https://github.com/o/repo.git": "repo", "ssh://git@git.example.com:2222/group/sub/repo.git": "repo",
		"https://dev.azure.com/org/proj/_git/repo": "repo", "git@ssh.dev.azure.com:v3/org/proj/repo": "repo", "https://h/o/r.git.git": "r.git",
	} {
		r, err := remote.Parse(in)
		if err != nil || r.Repo != repo {
			t.Errorf("%s: repo=%q err=%v", in, r.Repo, err)
		}
	}
	r, _ = remote.Parse("ssh://git@git.example.com:2222/a/b.git")
	if r.Port != 2222 || r.HostPort() != "git.example.com:2222" || r.Kind != remote.KindSSH || r.String() != "ssh://git@git.example.com:2222/a/b.git" {
		t.Fatalf("%+v %s", r, r.String())
	}
	r, _ = remote.Parse("git@github.com:o/r.git")
	if r.Kind != remote.KindSCP || r.User != "git" || r.String() != "git@github.com:o/r.git" || r.Platform != platform.GitHub {
		t.Fatalf("%+v", r)
	}
	r, _ = remote.Parse("https://a.b%40corp.com@github.com/o/r")
	if r.User != "a.b@corp.com" || r.String() != "https://a.b%40corp.com@github.com/o/r" {
		t.Fatalf("%+v %s", r, r.String())
	}
	r, _ = remote.Parse("http://git.local/o/r.git")
	if len(r.Warnings) != 1 || r.Warnings[0] != remote.WarnHTTP {
		t.Fatalf("%v", r.Warnings)
	}
	// unknown host + v3 + 4 segments is tentatively azure, but generic must still work for 4-seg paths via fallback
	r, err := remote.Parse("git@git.example.com:v3/a/b/c")
	if err != nil || r.Owner == "" {
		t.Fatalf("%+v %v", r, err)
	}
	// on-prem azure style path
	r, err = remote.Parse("https://tfs.corp.local/DefaultCollection/proj/_git/repo")
	if err != nil || !r.Azure || r.AzOrg != "DefaultCollection" || r.AzProject != "proj" {
		t.Fatalf("%+v %v", r, err)
	}
}

var negatives = []string{
	"", "   ", "not a url", "-oProxyCommand=x", "--upload-pack=x", "file:///tmp/x", "/tmp/repo", "./repo", "ext::sh -c x", "ext::x",
	"https://github.com/", "https://github.com/org", "git@github.com:", "git@github.com:org", "ftp://h/o/r", "https://h/o/r with space",
	"https://dev.azure.com/org/proj/repo", "git@ssh.dev.azure.com:org/proj/repo", "https://h:99999/o/r", "https://h:x/o/r", "https://h/o/r?x=1",
	"https://h/o/../r", "https://h/o//r", "C:\\repo", "https://-h/o/r", "https://h_x/o/r", "https://:@/o/r", "https://u%zz@h/o/r", "git@h:./r", "~/x:y/z",
	"https://h/o/.git", "https://dev.azure.com/a/b/c/d/_git/r", "ssh://git@ssh.dev.azure.com/o/p/r", "https://h/o/r\x00", "git://h/o/r",
}

func TestParseNegatives(t *testing.T) {
	for _, in := range negatives {
		r, err := remote.Parse(in)
		if err == nil {
			t.Errorf("Parse(%q) should fail, got %+v", in, r)
			continue
		}
		if !errors.Is(err, remote.ErrInvalid) {
			t.Errorf("Parse(%q): error does not wrap ErrInvalid: %v", in, err)
		}
	}
}

func FuzzParse(f *testing.F) {
	seeds := append([]string{}, negatives...)
	seeds = append(seeds,
		"https://github.com/org/repo.git", "git@github.com:org/repo.git", "ssh://git@git.example.com:2222/group/sub/repo.git",
		"https://dev.azure.com/org/My%20Project/_git/repo", "org@vs-ssh.visualstudio.com:v3/org/proj/repo", "http://git.local/o/r.git",
		"https://a.b%40corp.com@github.com/o/r", "https://u:p@github.com/o/r.git")
	cfg := testCfg()
	acct, _ := cfg.ParseRef("acme/alice")
	f.Fuzz(func(t *testing.T, in string) {
		r, err := remote.Parse(in)
		if err != nil {
			return
		}
		// String() must re-parse
		if _, err := remote.Parse(r.String()); err != nil {
			t.Fatalf("String() %q of %q does not re-parse: %v", r.String(), in, err)
		}
		if out, _, err := remote.Rewrite(cfg, acct, in, remote.Options{Force: true}); err == nil {
			if _, err := remote.Parse(out); err != nil {
				t.Fatalf("rendered %q (from %q) does not re-parse: %v", out, in, err)
			}
		}
	})
}

func TestPatterns(t *testing.T) { // P-R1..P-R5
	cfg := testCfg()
	eq := func(got, want []string) bool { return strings.Join(got, "\n") == strings.Join(want, "\n") }
	if got := remote.Patterns(ref(t, cfg, "acme/alice")); !eq(got, []string{"https://alice@github.com/**"}) {
		t.Fatal(got)
	}
	a := ref(t, cfg, "acme/alice")
	a.Account.Orgs = []string{"acme-org", "acme-labs"}
	if got := remote.Patterns(a); !eq(got, []string{"https://alice@github.com/acme-org/**", "https://alice@github.com/acme-labs/**"}) {
		t.Fatal(got)
	}
	if got := remote.Patterns(ref(t, cfg, "acme/carol")); !eq(got, []string{"git@github-acme-carol:*/**", "github-acme-carol:*/**", "ssh://git@github-acme-carol/**", "ssh://github-acme-carol/**"}) {
		t.Fatal(got)
	}
	if got := remote.Patterns(ref(t, cfg, "gl/ab")); !eq(got, []string{"https://a.b%40corp.com@github.com/**"}) {
		t.Fatal(got)
	}
	p := ref(t, cfg, "acme/alice")
	p.Account.Host = "h.example:8443"
	if got := remote.Patterns(p); !eq(got, []string{"https://alice@h.example:8443/**"}) {
		t.Fatal(got)
	}
}

func TestMatch(t *testing.T) { // M1..M9
	e := testutil.TempEnv(t)
	cfg := fixtures.Basic()
	acme, beta := e.Home+"/Code/acme/r", e.Home+"/Code/beta/r"
	parse := func(s string) remote.Remote {
		r, err := remote.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	has := func(res remote.MatchResult, w string) bool {
		for _, x := range res.Warnings {
			if x == w {
				return true
			}
		}
		return false
	}
	res := remote.Match(cfg, e, parse("git@github-acme-carol:o/r.git"), "/anywhere")
	if res.Account == nil || res.Account.String() != "acme/carol" || res.Reason != remote.ReasonAlias {
		t.Fatalf("M1 %+v", res)
	}
	res = remote.Match(cfg, e, parse("https://bob@github.com/o/r.git"), "/anywhere")
	if res.Account == nil || res.Account.String() != "acme/bob" || res.Reason != remote.ReasonUsername {
		t.Fatalf("M2 %+v", res)
	}
	res = remote.Match(cfg, e, parse("https://alice@github.com/o/r.git"), acme)
	if res.Account == nil || res.Account.String() != "acme/alice" {
		t.Fatalf("M3 %+v", res)
	}
	res = remote.Match(cfg, e, parse("https://alice@github.com/o/r.git"), beta)
	if res.Account == nil || res.Account.String() != "beta/alice" {
		t.Fatalf("M4 %+v", res)
	}
	res = remote.Match(cfg, e, parse("https://alice@github.com/o/r.git"), "/tmp/x")
	if res.Account != nil || len(res.Ambiguous) != 2 || !has(res, remote.WarnAmbiguous) {
		t.Fatalf("M5 %+v", res)
	}
	cfg6 := fixtures.Basic()
	cfg6.Companies[2].Accounts[0].Orgs = []string{"beta-org"}
	res = remote.Match(cfg6, e, parse("https://alice@github.com/beta-org/r.git"), "/tmp/x")
	if res.Account == nil || res.Account.String() != "beta/alice" {
		t.Fatalf("M6 %+v", res)
	}
	// org that does not match: beta/alice is not a candidate, acme/alice is
	res = remote.Match(cfg6, e, parse("https://alice@github.com/other/r.git"), "/tmp/x")
	if res.Account == nil || res.Account.String() != "acme/alice" {
		t.Fatalf("M6b %+v", res)
	}
	res = remote.Match(cfg, e, parse("https://github.com/o/r.git"), acme)
	if res.Account == nil || res.Account.String() != "acme/alice" || res.Reason != remote.ReasonDefault || !has(res, remote.WarnNoUsername) {
		t.Fatalf("M7 %+v", res)
	}
	res = remote.Match(cfg, e, parse("https://github.com/o/r.git"), "/tmp/x")
	if res.Account != nil || len(res.Ambiguous) != 0 {
		t.Fatalf("M8 %+v", res)
	}
	res = remote.Match(cfg, e, parse("https://unknown@github.com/o/r.git"), "/anywhere")
	if res.Account != nil {
		t.Fatalf("M9 %+v", res)
	}
	// scp github.com remote in a base dir: default account, no username warning (ssh)
	res = remote.Match(cfg, e, parse("git@github.com:o/r.git"), acme)
	if res.Account == nil || res.Reason != remote.ReasonDefault || has(res, remote.WarnNoUsername) {
		t.Fatalf("scp default %+v", res)
	}
	// same username on the same company: orgs narrow, base dir narrows
	cfgFull := fixtures.Full()
	az := parse("https://org@dev.azure.com/org/proj/_git/repo")
	res = remote.Match(cfgFull, e, az, e.Home+"/Code/az/x")
	if res.Account == nil || res.Account.String() != "az/dev" || res.Reason != remote.ReasonDefault {
		t.Fatalf("azure org user is not an account user: %+v", res)
	}
	// http remote warns
	res = remote.Match(cfg, e, parse("http://bob@github.com/o/r.git"), "/x")
	if !has(res, remote.WarnHTTP) {
		t.Fatalf("%+v", res)
	}
}

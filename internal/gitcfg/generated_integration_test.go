//go:build integration

package gitcfg_test

import (
	"testing"

	"github.com/tinhnd1902/gitid/internal/block"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/gitcfg"
	"github.com/tinhnd1902/gitid/internal/platform"
	"github.com/tinhnd1902/gitid/internal/testutil/fixtures"
)

// TestGeneratedLayout runs the G-INT scenarios against files produced by the
// real renderers (offline: git init + git remote add only).
func TestGeneratedLayout(t *testing.T) {
	s := newSpike(t)
	e := env.New(s.home, "")
	cfg := fixtures.Basic()
	cfg.Companies[0].Accounts = append(cfg.Companies[0].Accounts, config.Account{
		ID: "ab", Platform: platform.GitHub, Username: "a.b@corp.com", Name: "AB", Email: "ab@corp.com", Auth: config.AuthHTTPS,
	})
	cfg = config.Normalize(cfg, e)
	if errs := config.Validate(cfg, e, config.ValidateOptions{}); len(errs) != 0 {
		t.Fatal(errs)
	}
	for _, c := range cfg.Companies {
		cf, err := gitcfg.RenderCompanyFile(e, c)
		if err != nil {
			t.Fatal(err)
		}
		s.write(e.Contract(gitcfg.CompanyFile(e, c))[2:], cf)
		for _, a := range c.Accounts {
			af, err := gitcfg.RenderAccountFile(a)
			if err != nil {
				t.Fatal(err)
			}
			s.write(e.Contract(gitcfg.AccountFile(e, c, a))[2:], af)
		}
	}
	body, err := gitcfg.RenderManagedBody(e, cfg)
	if err != nil {
		t.Fatal(err)
	}
	user := "[user]\n\tname = Global\n\temail = global@x.io\n"
	gc, err := block.Upsert(user, body, gitcfg.Markers, block.AtEnd)
	if err != nil {
		t.Fatal(err)
	}
	s.write(".gitconfig", gc)

	cases := []struct {
		name, dir string
		remotes   []string
		want      string
	}{
		{"GI1 default", "Code/acme/r1", nil, "alice@acme.com"},
		{"GI2 bob", "Code/acme/r2", []string{"origin", "https://bob@github.com/o/r.git"}, "bob@acme.com"},
		{"GI3 carol", "Code/acme/r3", []string{"origin", "git@github-acme-carol:o/r.git"}, "carol@acme.com"},
		{"GI4 carol nested", "Code/acme/r4", []string{"origin", "git@github-acme-carol:group/sub/r.git"}, "carol@acme.com"},
		{"GI5 carol ssh://", "Code/acme/r5", []string{"origin", "ssh://git@github-acme-carol/o/r.git"}, "carol@acme.com"},
		{"GI6 beta alice", "Code/beta/r1", []string{"origin", "https://alice@github.com/o/r.git"}, "alice@beta.io"},
		{"GI7 acme alice", "Code/acme/r6", []string{"origin", "https://alice@github.com/o/r.git"}, "alice@acme.com"},
		{"GI8 outside carol", "outside/r", []string{"origin", "git@github-acme-carol:o/r.git"}, "carol@acme.com"},
		{"GI9 outside alice (ambiguous omitted)", "outside/r2", []string{"origin", "https://alice@github.com/o/r.git"}, "global@x.io"},
		{"GI10 outside none", "outside/r3", nil, "global@x.io"},
		{"GI11 percent-encoded", "Code/acme/r7", []string{"origin", "https://a.b%40corp.com@github.com/o/r.git"}, "ab@corp.com"},
		{"GI11b percent-encoded outside", "outside/r5", []string{"origin", "https://a.b%40corp.com@github.com/o/r.git"}, "ab@corp.com"},
		{"GI12 two remotes, later include wins", "Code/acme/r8", []string{"origin", "https://bob@github.com/o/r.git", "upstream", "git@github-acme-carol:o/r.git"}, "carol@acme.com"},
		{"globex default", "Code/globex/r1", nil, "dave@globex.example"},
		{"globex alias outside", "outside/r6", []string{"origin", "git@gitlab-globex-dave:g/s/r.git"}, "dave@globex.example"},
		{"unrelated user inside acme", "Code/acme/r9", []string{"origin", "https://zed@github.com/o/r.git"}, "alice@acme.com"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := s.repo(c.dir, c.remotes...)
			got, err := s.git(dir, "config", "user.email")
			if err != nil || got != c.want {
				t.Fatalf("user.email = %q (%v), want %q", got, err, c.want)
			}
			if out, err := s.git(dir, "config", "--list"); err != nil {
				t.Fatalf("git config --list: %v\n%s", err, out)
			}
		})
	}
}

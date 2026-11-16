package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/platform"
	"github.com/tinhnd1902/gitid/internal/testutil"
	"github.com/tinhnd1902/gitid/internal/testutil/fixtures"
)

func TestLoadMissing(t *testing.T) { // C1
	e := testutil.TempEnv(t)
	before := testutil.Snapshot(t, e.Home)
	cfg, err := config.Load(e)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != 1 || cfg.Settings.Language != "vi" || cfg.Settings.CodeRoot != "~/Code" || len(cfg.Companies) != 0 {
		t.Fatalf("%+v", cfg)
	}
	testutil.AssertSame(t, before, testutil.Snapshot(t, e.Home))
}

func TestSaveLoadRoundTrip(t *testing.T) { // C2, C3
	e := testutil.TempEnv(t)
	cfg := fixtures.Full()
	dir, err := config.Save(e, cfg)
	if err != nil || dir != "" {
		t.Fatalf("first save: dir=%q err=%v", dir, err)
	}
	p := e.Paths().ConfigFile
	fi, err := os.Stat(p)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode: %v %v", fi, err)
	}
	got, err := config.Load(e)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Version = 1
	if !reflectEqual(got, cfg) {
		t.Fatalf("round trip differs:\n%+v\n%+v", got, cfg)
	}
	// unchanged save: no backup
	if dir, err := config.Save(e, cfg); err != nil || dir != "" {
		t.Fatalf("noop save: %q %v", dir, err)
	}
	if _, err := os.Stat(e.Paths().BackupsDir); err == nil {
		t.Fatal("no backup expected for identical save")
	}
	// changed save: previous file backed up
	cfg.Companies[0].Name = "Changed"
	old, _ := os.ReadFile(p)
	dir, err = config.Save(e, cfg)
	if err != nil || dir == "" {
		t.Fatalf("changed save: %q %v", dir, err)
	}
	ents, _ := os.ReadDir(dir)
	found := false
	for _, en := range ents {
		if strings.HasSuffix(en.Name(), "config.yaml") {
			b, _ := os.ReadFile(filepath.Join(dir, en.Name()))
			found = string(b) == string(old)
		}
	}
	if !found {
		t.Fatal("backup of previous config.yaml missing or different")
	}
	testutil.Guard(t, e, p, dir)
}

func TestSaveTightensLooseMode(t *testing.T) {
	e := testutil.TempEnv(t)
	testutil.WriteFile(t, e.Paths().ConfigFile, "version: 1\n", 0o644)
	if _, err := config.Save(e, fixtures.Basic()); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(e.Paths().ConfigFile)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}

func TestXDGRelativeIgnored(t *testing.T) { // C4
	e := testutil.TempEnv(t)
	e.XDGConfigHome = "rel"
	if _, err := config.Save(e, fixtures.Basic()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.Home, ".config/gitid/config.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestXDGAbsolute(t *testing.T) {
	e := testutil.TempEnv(t)
	e.XDGConfigHome = filepath.Join(e.Home, "xdg")
	if _, err := config.Save(e, fixtures.Basic()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.Home, "xdg/gitid/config.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestUnsupportedVersion(t *testing.T) { // C8
	e := testutil.TempEnv(t)
	body := "version: 2\ncompanies: []\n"
	testutil.WriteFile(t, e.Paths().ConfigFile, body, 0o600)
	if _, err := config.Load(e); !errors.Is(err, config.ErrUnsupportedVersion) {
		t.Fatalf("err=%v", err)
	}
	if _, err := config.Save(e, fixtures.Basic()); !errors.Is(err, config.ErrUnsupportedVersion) {
		t.Fatalf("save err=%v", err)
	}
	if testutil.ReadFile(t, e.Paths().ConfigFile) != body {
		t.Fatal("file overwritten")
	}
}

func TestParseErrors(t *testing.T) {
	for name, in := range map[string]string{
		"garbage":       ":::\n\t- [",
		"unknown field": "version: 1\nbogus: true\n",
		"no version":    "companies: []\n",
	} {
		if _, err := config.Parse([]byte(in)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestNormalize(t *testing.T) { // C6
	e := testutil.TempEnv(t)
	cfg := config.Default()
	cfg.Version = 0
	cfg.Companies = []config.Company{{
		Slug: "x", Name: "X", BaseDir: filepath.Join(e.Home, "Code", "x"),
		Accounts: []config.Account{{ID: "a", Platform: platform.GitHub, Host: " GitHub.COM ", Username: " a ", Name: " A ", Email: " a@b.c ", Auth: config.AuthHTTPS, SSHKey: filepath.Join(e.Home, ".ssh/gitid/x-a")}},
	}}
	n := config.Normalize(cfg, e)
	c := n.Companies[0]
	if n.Version != 1 || c.BaseDir != "~/Code/x/" || c.DefaultAccount != "a" {
		t.Fatalf("%+v", c)
	}
	a := c.Accounts[0]
	if a.Host != "github.com" || a.Username != "a" || a.Name != "A" || a.Email != "a@b.c" || a.SSHKey != "~/.ssh/gitid/x-a" {
		t.Fatalf("%+v", a)
	}
	if cfg.Companies[0].BaseDir == "~/Code/x/" {
		t.Fatal("input mutated")
	}
}

func TestBaseDirHelpers(t *testing.T) {
	e := env.New("/h", "")
	cases := map[string]string{"~/Code/a": "~/Code/a/", "~/Code/a//": "~/Code/a/", "/h/Code/a": "~/Code/a/", "/opt/x": "/opt/x/", "rel/x": "rel/x", "": "", "/": "/", "~": "~/"}
	for in, want := range cases {
		if got := config.NormalizeBaseDir(e, in); got != want {
			t.Errorf("NormalizeBaseDir(%q)=%q want %q", in, got, want)
		}
	}
	if got := config.DefaultBaseDir(e, "~/Code", "acme"); got != "~/Code/acme/" {
		t.Fatal(got)
	}
	if got := config.DefaultBaseDir(e, "", "acme"); got != "~/Code/acme/" {
		t.Fatal(got)
	}
}

func TestDefaultAccountID(t *testing.T) { // D6
	c := fixtures.Acme()
	if config.DefaultAccountID(c, platform.GitLab, "Dave") != "dave" {
		t.Fatal("plain")
	}
	if config.DefaultAccountID(c, platform.GitLab, "alice") != "alice-gitlab" {
		t.Fatal("dup")
	}
}

func TestAccessors(t *testing.T) {
	cfg := fixtures.Full()
	acme, _, _ := cfg.FindCompany("acme")
	if cfg.Empty() || (config.Config{}).Empty() == false {
		t.Fatal("Empty")
	}
	if _, _, ok := cfg.FindCompany("nope"); ok {
		t.Fatal("found nope")
	}
	if a, ok := acme.Default(); !ok || a.ID != "alice" {
		t.Fatal("default")
	}
	if _, ok := (config.Company{}).Default(); ok {
		t.Fatal("empty default")
	}
	carol, _, _ := acme.FindAccount("carol")
	if acme.Alias(carol) != "github-acme-carol" || acme.KeyBase(carol) != "acme-carol" || acme.DefaultKeyPath(carol) != "~/.ssh/gitid/acme-carol" {
		t.Fatal("alias/key")
	}
	az, _, _ := cfg.FindCompany("az")
	ds, _, _ := az.FindAccount("devssh")
	if az.Alias(ds) != "azure-devops-az-devssh" || ds.EffectiveHost() != "dev.azure.com" || ds.EffectiveSSHHost() != "ssh.dev.azure.com" {
		t.Fatalf("azure: %s %s %s", az.Alias(ds), ds.EffectiveHost(), ds.EffectiveSSHHost())
	}
	mb, _, _ := cfg.FindCompany("globex")
	if mb.Accounts[0].EffectiveHost() != "git.example.com" || mb.Accounts[0].HostName() != "git.example.com" {
		t.Fatal("globex host")
	}
	port := config.Account{Platform: platform.GitHub, Host: "h.example:8443"}
	if port.HostName() != "h.example" || port.EffectiveHost() != "h.example:8443" || port.EffectiveSSHHost() != "h.example" {
		t.Fatal("port host")
	}
	if len(cfg.All()) != 9 {
		t.Fatalf("All=%d", len(cfg.All()))
	}
	r := cfg.All()[0]
	if r.String() != "acme/alice" || r.Alias() != "github-acme-alice" {
		t.Fatal(r.String())
	}
}

func TestParseRef(t *testing.T) { // C7
	cfg := fixtures.Full()
	r, err := cfg.ParseRef("acme/alice")
	if err != nil || r.String() != "acme/alice" {
		t.Fatalf("%v %v", r, err)
	}
	r, err = cfg.ParseRef("carol")
	if err != nil || r.String() != "acme/carol" {
		t.Fatalf("%v %v", r, err)
	}
	if _, err := cfg.ParseRef("alice"); !errors.Is(err, config.ErrRefAmbiguous) || !strings.Contains(err.Error(), "beta/alice") {
		t.Fatalf("ambiguous: %v", err)
	}
	for _, in := range []string{"x/y", "acme/zzz", "nobody", "", "  "} {
		if _, err := cfg.ParseRef(in); !errors.Is(err, config.ErrRefNotFound) {
			t.Errorf("%q: %v", in, err)
		}
	}
}

func codes(errs []config.FieldError) map[string]bool {
	m := map[string]bool{}
	for _, e := range errs {
		m[e.Code] = true
	}
	return m
}

func TestValidateFixturesOK(t *testing.T) {
	e := testutil.TempEnv(t)
	cfg := config.Normalize(fixtures.Full(), e)
	if errs := config.Validate(cfg, e, config.ValidateOptions{}); len(errs) != 0 {
		t.Fatalf("fixtures invalid: %v", errs)
	}
	if errs := config.Validate(config.Default(), e, config.ValidateOptions{}); len(errs) != 0 {
		t.Fatal(errs)
	}
}

func TestValidateField(t *testing.T) { // C5 (field level)
	e := testutil.TempEnv(t)
	long := strings.Repeat("a", 33)
	cases := []struct {
		field, value, want string
	}{
		{config.FieldSlug, "acme", ""}, {config.FieldSlug, "a1-b", ""}, {config.FieldSlug, "Acme", config.ErrSlugInvalid}, {config.FieldSlug, "-a", config.ErrSlugInvalid},
		{config.FieldSlug, long, config.ErrSlugInvalid}, {config.FieldSlug, "a_b", config.ErrSlugInvalid}, {config.FieldSlug, "", config.ErrSlugInvalid},
		{config.FieldID, "Bad", config.ErrIDInvalid}, {config.FieldID, "ok", ""},
		{config.FieldHost, "", ""}, {config.FieldHost, "github.com", ""}, {config.FieldHost, "GitHub.com:8443", ""}, {config.FieldHost, "bad host", config.ErrHostInvalid},
		{config.FieldHost, "-x.com", config.ErrHostInvalid}, {config.FieldHost, "a..b", config.ErrHostInvalid}, {config.FieldHost, "h:99999", config.ErrHostInvalid},
		{config.FieldHost, "h:0", config.ErrHostInvalid}, {config.FieldHost, "h:08", config.ErrHostInvalid}, {config.FieldHost, "a-.com", config.ErrHostInvalid}, {config.FieldHost, "a_b.com", config.ErrHostInvalid},
		{config.FieldHost, strings.Repeat("a", 64) + ".com", config.ErrHostInvalid}, {config.FieldHost, strings.Repeat("a.", 130) + "com", config.ErrHostInvalid},
		{config.FieldSSHHost, "ssh.dev.azure.com", ""}, {config.FieldSSHHost, "", ""}, {config.FieldSSHHost, "h\nProxyCommand x", config.ErrSSHHostInvalid},
		{config.FieldSSHPort, "", ""}, {config.FieldSSHPort, "22", ""}, {config.FieldSSHPort, "70000", config.ErrSSHPortInvalid}, {config.FieldSSHPort, "x", config.ErrSSHPortInvalid}, {config.FieldSSHPort, "-1", config.ErrSSHPortInvalid},
		{config.FieldUsername, "alice", ""}, {config.FieldUsername, "a.b@corp.com", ""}, {config.FieldUsername, "a b", config.ErrUsernameInvalid}, {config.FieldUsername, "a/b", config.ErrUsernameInvalid},
		{config.FieldUsername, "a:b", config.ErrUsernameInvalid}, {config.FieldUsername, "a*", config.ErrUsernameInvalid}, {config.FieldUsername, "", config.ErrUsernameInvalid}, {config.FieldUsername, "a\nb", config.ErrUsernameInvalid},
		{config.FieldUsername, "a\\b", config.ErrUsernameInvalid}, {config.FieldUsername, "a[b", config.ErrUsernameInvalid}, {config.FieldUsername, "a?b", config.ErrUsernameInvalid},
		{config.FieldEmail, "a@b.c", ""}, {config.FieldEmail, "a", config.ErrEmailInvalid}, {config.FieldEmail, "Alice <a@b.c>", config.ErrEmailInvalid}, {config.FieldEmail, "a@b.c\n", config.ErrEmailInvalid},
		{config.FieldEmail, "", config.ErrEmailInvalid}, {config.FieldEmail, " a@b.c", config.ErrEmailInvalid}, {config.FieldEmail, "a@b.c\x00", config.ErrEmailInvalid},
		{config.FieldName, "Alice Nguyen", ""}, {config.FieldName, "", config.ErrNameInvalid}, {config.FieldName, "   ", config.ErrNameInvalid}, {config.FieldName, "A\nB", config.ErrNameInvalid},
		{config.FieldName, strings.Repeat("x", 101), config.ErrNameInvalid}, {config.FieldName, "Nguyễn Văn Á", ""}, {config.FieldName, "a b", config.ErrNameInvalid},
		{config.FieldCompanyName, "", config.ErrCompanyNameBad}, {config.FieldCompanyName, "ACME", ""},
		{config.FieldBaseDir, "~/Code/x", ""}, {config.FieldBaseDir, "/", config.ErrBaseDirReserved}, {config.FieldBaseDir, "~", config.ErrBaseDirReserved},
		{config.FieldBaseDir, e.Home, config.ErrBaseDirReserved}, {config.FieldBaseDir, "Code/x", config.ErrBaseDirInvalid}, {config.FieldBaseDir, "", config.ErrBaseDirInvalid},
		{config.FieldBaseDir, "~/a*b", config.ErrBaseDirInvalid}, {config.FieldBaseDir, "~/a\nb", config.ErrBaseDirInvalid},
		{config.FieldOrg, "acme-org", ""}, {config.FieldOrg, "grp/sub", ""}, {config.FieldOrg, "a*", config.ErrOrgInvalid}, {config.FieldOrg, "a b", config.ErrOrgInvalid}, {config.FieldOrg, "", config.ErrOrgInvalid}, {config.FieldOrg, "/a", config.ErrOrgInvalid},
		{config.FieldPlatform, "github", ""}, {config.FieldPlatform, "svn", config.ErrPlatformInvalid},
		{config.FieldAuth, "ssh", ""}, {config.FieldAuth, "https", ""}, {config.FieldAuth, "ftp", config.ErrAuthInvalid},
		{"unknown-field", "x", ""},
	}
	for _, c := range cases {
		if got := config.ValidateField(e, c.field, c.value); got != c.want {
			t.Errorf("ValidateField(%s,%q)=%q want %q", c.field, c.value, got, c.want)
		}
	}
}

func TestValidateWhole(t *testing.T) { // C5 (cross-record)
	e := testutil.TempEnv(t)
	base := func() config.Config { return config.Normalize(fixtures.Basic(), e) }
	check := func(name string, mutate func(*config.Config), opts config.ValidateOptions, want string) {
		t.Helper()
		cfg := base()
		mutate(&cfg)
		errs := config.Validate(cfg, e, opts)
		if !codes(errs)[want] {
			t.Errorf("%s: want %s, got %v", name, want, errs)
		}
	}
	check("dup slug", func(c *config.Config) { c.Companies[1].Slug = "acme" }, config.ValidateOptions{}, config.ErrSlugDuplicate)
	check("bad slug", func(c *config.Config) { c.Companies[0].Slug = "Acme" }, config.ValidateOptions{}, config.ErrSlugInvalid)
	check("dup id", func(c *config.Config) { c.Companies[0].Accounts[1].ID = "alice" }, config.ValidateOptions{}, config.ErrIDDuplicate)
	check("bad platform", func(c *config.Config) { c.Companies[0].Accounts[0].Platform = "svn" }, config.ValidateOptions{}, config.ErrPlatformInvalid)
	check("bad host", func(c *config.Config) { c.Companies[0].Accounts[0].Host = "bad host" }, config.ValidateOptions{}, config.ErrHostInvalid)
	check("gitea needs host", func(c *config.Config) {
		c.Companies[0].Accounts[0].Platform = platform.Gitea
		c.Companies[0].Accounts[0].Host = ""
	}, config.ValidateOptions{}, config.ErrHostRequired)
	check("bad port", func(c *config.Config) { c.Companies[0].Accounts[0].SSHPort = 70000 }, config.ValidateOptions{}, config.ErrSSHPortInvalid)
	check("bad ssh host", func(c *config.Config) { c.Companies[0].Accounts[2].SSHHost = "x y" }, config.ValidateOptions{}, config.ErrSSHHostInvalid)
	check("bad username", func(c *config.Config) { c.Companies[0].Accounts[0].Username = "a b" }, config.ValidateOptions{}, config.ErrUsernameInvalid)
	check("bad email", func(c *config.Config) { c.Companies[0].Accounts[0].Email = "Alice <a@b.c>" }, config.ValidateOptions{}, config.ErrEmailInvalid)
	check("bad name", func(c *config.Config) { c.Companies[0].Accounts[0].Name = "a\nb" }, config.ValidateOptions{}, config.ErrNameInvalid)
	check("empty company name", func(c *config.Config) { c.Companies[0].Name = "" }, config.ValidateOptions{}, config.ErrCompanyNameBad)
	check("bad auth", func(c *config.Config) { c.Companies[0].Accounts[0].Auth = "ftp" }, config.ValidateOptions{}, config.ErrAuthInvalid)
	check("bad org", func(c *config.Config) { c.Companies[0].Accounts[0].Orgs = []string{"a*"} }, config.ValidateOptions{}, config.ErrOrgInvalid)
	check("base dir /", func(c *config.Config) { c.Companies[0].BaseDir = "/" }, config.ValidateOptions{}, config.ErrBaseDirReserved)
	check("base dir home", func(c *config.Config) { c.Companies[0].BaseDir = e.Home }, config.ValidateOptions{}, config.ErrBaseDirReserved)
	check("base dir rel", func(c *config.Config) { c.Companies[0].BaseDir = "Code/x" }, config.ValidateOptions{}, config.ErrBaseDirInvalid)
	check("base dir nested", func(c *config.Config) { c.Companies[1].BaseDir = "~/Code/acme/sub/" }, config.ValidateOptions{}, config.ErrBaseDirNested)
	check("base dir equal", func(c *config.Config) { c.Companies[1].BaseDir = "~/Code/acme/" }, config.ValidateOptions{}, config.ErrBaseDirNested)
	check("base dir parent", func(c *config.Config) { c.Companies[1].BaseDir = "~/Code/" }, config.ValidateOptions{}, config.ErrBaseDirNested)
	check("default missing", func(c *config.Config) { c.Companies[0].DefaultAccount = "zzz" }, config.ValidateOptions{}, config.ErrDefaultMissing)
	check("alias vs existing host", func(c *config.Config) {}, config.ValidateOptions{ExistingHosts: []string{"github-acme-carol"}}, config.ErrAliasHostClash)
	check("alias dup across companies", func(c *config.Config) {
		// company a-b / account c  vs  company a / account b-c : both github-a-b-c
		c.Companies = []config.Company{
			{Slug: "a-b", Name: "x", BaseDir: "~/Code/ab/", DefaultAccount: "c", Accounts: []config.Account{{ID: "c", Platform: platform.GitHub, Username: "u", Name: "n", Email: "u@x.io", Auth: config.AuthSSH}}},
			{Slug: "a", Name: "y", BaseDir: "~/Code/a/", DefaultAccount: "b-c", Accounts: []config.Account{{ID: "b-c", Platform: platform.GitHub, Username: "v", Name: "n", Email: "v@x.io", Auth: config.AuthSSH}}},
		}
	}, config.ValidateOptions{}, config.ErrAliasDuplicate)
	check("file name collision", func(c *config.Config) {
		c.Companies = append(c.Companies, config.Company{Slug: "acme-bob", Name: "x", BaseDir: "~/Code/acmebob/"})
	}, config.ValidateOptions{}, config.ErrNameCollision)
	check("bad language", func(c *config.Config) { c.Settings.Language = "fr" }, config.ValidateOptions{}, config.ErrLanguageInvalid)
	check("bad key path", func(c *config.Config) { c.Companies[0].Accounts[2].SSHKey = "~/.ssh/../x" }, config.ValidateOptions{}, config.ErrKeyPathInvalid)
	check("key path relative", func(c *config.Config) { c.Companies[0].Accounts[2].SSHKey = "rel/key" }, config.ValidateOptions{}, config.ErrKeyPathInvalid)
	// FieldError formatting
	fe := config.FieldError{Path: "p", Code: "c", Detail: "d"}
	if fe.Error() != "p: c (d)" || (config.FieldError{Path: "p", Code: "c"}).Error() != "p: c" {
		t.Fatal(fe.Error())
	}
	// unique alias for non-ssh accounts is not checked
	cfg := base()
	if errs := config.Validate(cfg, e, config.ValidateOptions{ExistingHosts: []string{"github-acme-alice"}}); len(errs) != 0 {
		t.Fatalf("https alias must not collide: %v", errs)
	}
}

func TestKeyPathDeletable(t *testing.T) { // K9 guard
	e := testutil.TempEnv(t)
	ok := []string{"~/.ssh/gitid/acme-carol", filepath.Join(e.Home, ".ssh/gitid/x")}
	bad := []string{"", "~/.ssh/id_ed25519", "~/.ssh/gitid", "~/.ssh/gitid/../id_rsa", "/etc/passwd", "~/.ssh/gitidx/a"}
	for _, p := range ok {
		if !config.KeyPathDeletable(e, p) {
			t.Errorf("%q should be deletable", p)
		}
	}
	for _, p := range bad {
		if config.KeyPathDeletable(e, p) {
			t.Errorf("%q must not be deletable", p)
		}
	}
}

func TestAllErrorCodesUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range config.AllErrorCodes() {
		if c == "" || seen[c] || !strings.HasPrefix(c, "err.") {
			t.Fatalf("bad code %q", c)
		}
		seen[c] = true
	}
}

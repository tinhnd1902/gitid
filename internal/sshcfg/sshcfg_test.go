package sshcfg_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/sshcfg"
	"github.com/tinhnd1902/gitid/internal/testutil/fixtures"
)

const observed = "Host github.com\n    IdentityFile ~/.ssh/id_ed25519_personal\n    IdentitiesOnly yes\n"

func TestS1HostBlockGolden(t *testing.T) {
	c := fixtures.Globex()
	got, err := sshcfg.RenderHostBlock(c, c.Accounts[0])
	want := "Host gitlab-globex-dave\n    HostName git.example.com\n    User git\n    Port 2222\n    IdentityFile ~/.ssh/gitid/globex-dave\n    IdentitiesOnly yes\n    AddKeysToAgent yes\n    IgnoreUnknown UseKeychain\n    UseKeychain yes\n"
	if err != nil || got != want {
		t.Fatalf("%q %v", got, err)
	}
	// no port line when unset; default key path when ssh_key is empty; azure host
	az := fixtures.Azure()
	a := az.Accounts[1]
	a.SSHKey = ""
	got, _ = sshcfg.RenderHostBlock(az, a)
	if strings.Contains(got, "Port") || !strings.Contains(got, "HostName ssh.dev.azure.com\n") || !strings.Contains(got, "IdentityFile ~/.ssh/gitid/az-devssh\n") || !strings.HasPrefix(got, "Host azure-devops-az-devssh\n") {
		t.Fatal(got)
	}
}

func TestS2GitidConf(t *testing.T) {
	cfg := fixtures.Full()
	a, err := sshcfg.RenderGitidConf(cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := sshcfg.RenderGitidConf(cfg)
	if a != b {
		t.Fatal("not deterministic")
	}
	if !strings.HasPrefix(a, sshcfg.Header+"\n\nHost ") {
		t.Fatal(a)
	}
	var hosts []string
	for _, l := range strings.Split(a, "\n") {
		if strings.HasPrefix(l, "Host ") {
			hosts = append(hosts, strings.TrimPrefix(l, "Host "))
		}
	}
	want := []string{"azure-devops-az-devssh", "github-acme-carol", "gitlab-globex-dave"}
	if strings.Join(hosts, ",") != strings.Join(want, ",") {
		t.Fatalf("hosts %v", hosts)
	}
	// https-only: empty
	cfg2 := fixtures.Basic()
	cfg2.Companies = []config.Company{fixtures.Beta()}
	if out, _ := sshcfg.RenderGitidConf(cfg2); out != "" {
		t.Fatalf("expected empty, got %q", out)
	}
	if strings.Join(sshcfg.Aliases(cfg), ",") != strings.Join(want, ",") {
		t.Fatal(sshcfg.Aliases(cfg))
	}
}

func TestS3S4S7EnsureInclude(t *testing.T) {
	r, err := sshcfg.EnsureInclude("")
	blk := sshcfg.Markers.Start + "\nInclude config.d/*\n" + sshcfg.Markers.End + "\n"
	if err != nil || r.Text != blk || r.External {
		t.Fatalf("S3 %q %v", r.Text, err)
	}
	r, _ = sshcfg.EnsureInclude(observed)
	if r.Text != blk+observed { // S4: top, the rest identical
		t.Fatalf("S4 %q", r.Text)
	}
	r2, _ := sshcfg.EnsureInclude(r.Text) // S7
	if r2.Text != r.Text {
		t.Fatal("S7 not idempotent")
	}
	// no trailing newline fixture round-trips through Remove
	in := "Host x\n  HostName y"
	r, _ = sshcfg.EnsureInclude(in)
	back, err := sshcfg.RemoveInclude(r.Text)
	if err != nil || back != in {
		t.Fatalf("%q %v", back, err)
	}
	// a block that drifted lower is moved to the top
	low := observed + blk
	r, _ = sshcfg.EnsureInclude(low)
	if r.Text != blk+observed {
		t.Fatalf("%q", r.Text)
	}
	if _, err := sshcfg.EnsureInclude(sshcfg.Markers.Start + "\n"); err == nil {
		t.Fatal("corrupt block should error")
	}
}

func TestS5ExistingTopLevelInclude(t *testing.T) {
	for _, inc := range []string{"Include config.d/*", "Include ~/.ssh/config.d/*", "include   config.d/*  # mine", "Include=config.d/*", "Include \"config.d/*\""} {
		in := "# my config\n" + inc + "\n\n" + observed
		r, err := sshcfg.EnsureInclude(in)
		if err != nil || r.Text != in || !r.External || len(r.Warnings) != 0 {
			t.Errorf("%q: %+v %v", inc, r, err)
		}
	}
}

func TestS6IncludeInsideHost(t *testing.T) {
	in := "Host x\n    Include config.d/*\n"
	r, _ := sshcfg.EnsureInclude(in)
	if r.External || len(r.Warnings) != 1 || r.Warnings[0] != sshcfg.WarnIncludeScoped || !strings.HasPrefix(r.Text, sshcfg.Markers.Start) || !strings.HasSuffix(r.Text, in) {
		t.Fatalf("%+v", r)
	}
	in = "Match host y\n  Include ~/.ssh/config.d/*\n"
	r, _ = sshcfg.EnsureInclude(in)
	if len(r.Warnings) != 1 {
		t.Fatalf("%+v", r)
	}
	// unrelated include does not count
	r, _ = sshcfg.EnsureInclude("Include other.d/*\n")
	if r.External || !strings.HasSuffix(r.Text, "Include other.d/*\n") {
		t.Fatalf("%+v", r)
	}
}

func TestS8ParseHosts(t *testing.T) {
	text := "# c\nHost a b !c\n  IdentityFile ~/.ssh/ka\n  IdentityFile \"~/.ssh/k b\" # x\n\nMatch host z\n  IdentityFile ~/.ssh/ignored\n\nhost = github.com\n  identityfile=~/.ssh/kg\n\nHost *\n  ForwardAgent no\n"
	h := sshcfg.ParseHosts(text)
	if len(h) != 3 {
		t.Fatalf("%+v", h)
	}
	if strings.Join(h[0].Patterns, ",") != "a,b,!c" || strings.Join(h[0].IdentityFile, "|") != "~/.ssh/ka|~/.ssh/k b" || h[0].Line != 2 {
		t.Fatalf("%+v", h[0])
	}
	if h[1].Patterns[0] != "github.com" || h[1].IdentityFile[0] != "~/.ssh/kg" || len(h[2].IdentityFile) != 0 {
		t.Fatalf("%+v", h)
	}
	if strings.Join(sshcfg.HostPatterns(h), ",") != "a,b,!c,github.com,*" {
		t.Fatal(sshcfg.HostPatterns(h))
	}
	// managed block is ignored
	r, _ := sshcfg.EnsureInclude("Host q\n")
	if len(sshcfg.ParseHosts(r.Text)) != 1 {
		t.Fatal("block content leaked")
	}
}

func TestS10Injection(t *testing.T) {
	c := fixtures.Globex()
	for _, bad := range []string{"evil\n  ProxyCommand x", "a b", "a%h", "-x", "a;b", "a\tb"} {
		a := c.Accounts[0]
		a.SSHHost = bad
		if _, err := sshcfg.RenderHostBlock(c, a); !errors.Is(err, sshcfg.ErrUnsafeValue) {
			t.Errorf("host %q accepted", bad)
		}
	}
	c2 := fixtures.Globex()
	c2.Slug = "x\ny"
	if _, err := sshcfg.RenderHostBlock(c2, c2.Accounts[0]); !errors.Is(err, sshcfg.ErrUnsafeValue) {
		t.Error("alias with newline accepted")
	}
	for _, bad := range []string{"~/k\nProxyCommand x", "~/k%d", "~/k\"x", "~/k\\x", "\x00"} {
		a := c.Accounts[0]
		a.SSHKey = bad
		if _, err := sshcfg.RenderHostBlock(c, a); !errors.Is(err, sshcfg.ErrUnsafeValue) {
			t.Errorf("key %q accepted", bad)
		}
	}
	a := c.Accounts[0]
	a.SSHPort = 70000
	if _, err := sshcfg.RenderHostBlock(c, a); err == nil {
		t.Error("bad port accepted")
	}
	a = c.Accounts[0]
	a.SSHKey = "/opt/John Doe/.ssh/gitid/k"
	got, err := sshcfg.RenderHostBlock(c, a)
	if err != nil || !strings.Contains(got, "IdentityFile \"/opt/John Doe/.ssh/gitid/k\"\n") {
		t.Fatalf("%q %v", got, err)
	}
	cfg := fixtures.Basic()
	cfg.Companies[1].Accounts[0].SSHHost = "bad host"
	if _, err := sshcfg.RenderGitidConf(cfg); err == nil {
		t.Fatal("RenderGitidConf must propagate errors")
	}
}

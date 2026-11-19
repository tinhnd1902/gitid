package env

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPathsDefaultXDG(t *testing.T) {
	e := New("/h", "")
	p := e.Paths()
	if p.ConfigFile != "/h/.config/gitid/config.yaml" {
		t.Fatalf("ConfigFile = %q", p.ConfigFile)
	}
	if p.GitDir != "/h/.config/gitid/git" || p.BackupsDir != "/h/.config/gitid/backups" || p.LockFile != "/h/.config/gitid/.lock" {
		t.Fatalf("paths: %+v", p)
	}
	if p.GitConfig != "/h/.gitconfig" || p.SSHConfig != "/h/.ssh/config" || p.SSHGitidConf != "/h/.ssh/config.d/gitid.conf" || p.KeysDir != "/h/.ssh/gitid" || p.SSHConfigD != "/h/.ssh/config.d" || p.SSHDir != "/h/.ssh" {
		t.Fatalf("paths: %+v", p)
	}
}

func TestPathsXDG(t *testing.T) {
	e := New("/h", "/x/cfg")
	if got := e.Paths().ConfigDir; got != "/x/cfg/gitid" {
		t.Fatalf("ConfigDir = %q", got)
	}
}

func TestPathsXDGRelativeIgnored(t *testing.T) {
	e := New("/h", "rel")
	if got := e.Paths().ConfigDir; got != "/h/.config/gitid" {
		t.Fatalf("ConfigDir = %q", got)
	}
}

func TestExpandContract(t *testing.T) {
	e := New("/h", "")
	cases := []struct{ in, exp string }{
		{"~", "/h"}, {"~/a/b", "/h/a/b"}, {"/abs/x", "/abs/x"}, {"", ""}, {"rel/x", "rel/x"}, {"~/a/b/", "/h/a/b"},
	}
	for _, c := range cases {
		if got := e.Expand(c.in); got != c.exp {
			t.Errorf("Expand(%q) = %q want %q", c.in, got, c.exp)
		}
	}
	ct := []struct{ in, exp string }{
		{"/h", "~"}, {"/h/a/b", "~/a/b"}, {"/h/a/b/", "~/a/b/"}, {"/other/x", "/other/x"}, {"/hx/a", "/hx/a"}, {"/", "/"},
	}
	for _, c := range ct {
		if got := e.Contract(c.in); got != c.exp {
			t.Errorf("Contract(%q) = %q want %q", c.in, got, c.exp)
		}
	}
	if got := (Env{}).Contract("/a"); got != "/a" {
		t.Errorf("empty home contract = %q", got)
	}
}

func TestDefaults(t *testing.T) {
	e := New("/h", "")
	if e.Now == nil || e.GOOS == "" || e.LookPath == nil {
		t.Fatal("defaults missing")
	}
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	e.Now = func() time.Time { return fixed }
	if !e.Now().Equal(fixed) {
		t.Fatal("clock not injectable")
	}
}

func TestIsUnder(t *testing.T) {
	if !IsUnder("/a/b", "/a") || !IsUnder("/a", "/a") || IsUnder("/ab", "/a") || IsUnder("/b", "/a") {
		t.Fatal("IsUnder wrong")
	}
}

func TestFromOS(t *testing.T) {
	t.Setenv("HOME", "/tmp/fakehome")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/fakexdg")
	e, err := FromOS()
	if err != nil || e.Home != "/tmp/fakehome" || e.XDGConfigHome != "/tmp/fakexdg" {
		t.Fatalf("FromOS = %+v, %v", e, err)
	}
	t.Setenv("HOME", "")
	if _, err := FromOS(); err != nil {
		_ = err // UserHomeDir may fail without HOME; both outcomes are valid
	}
	_ = RealHome()
}

func TestKeyBindingsFilePath(t *testing.T) {
	for _, c := range []struct{ home, xdg, want string }{
		{"/h", "", "/h/.config/gitid/keys.yaml"},
		{"/h", "/x/cfg", "/x/cfg/gitid/keys.yaml"},
		{"/h", "rel", "/h/.config/gitid/keys.yaml"}, // relative XDG is ignored, like ConfigFile
	} {
		p := New(c.home, c.xdg).Paths()
		if p.KeyBindingsFile != c.want {
			t.Errorf("xdg %q: KeyBindingsFile = %q, want %q", c.xdg, p.KeyBindingsFile, c.want)
		}
		if filepath.Dir(p.KeyBindingsFile) != p.ConfigDir || filepath.Dir(p.KeyBindingsFile) != filepath.Dir(p.ConfigFile) {
			t.Errorf("keys.yaml is not next to config.yaml: %q vs %q", p.KeyBindingsFile, p.ConfigFile)
		}
	}
}

// ENV-1: New fills LookupEnv (the process environment by default).
func TestNewHasLookupEnv(t *testing.T) {
	e := New("/h", "")
	if e.LookupEnv == nil {
		t.Fatal("LookupEnv is nil")
	}
	t.Setenv("GITID_ENV_TEST_VAR", "x")
	if v, ok := e.LookupEnv("GITID_ENV_TEST_VAR"); !ok || v != "x" {
		t.Fatalf("LookupEnv = %q, %v", v, ok)
	}
	// the real network is the default
	if e.HTTP != nil {
		t.Fatal("HTTP is set by default")
	}
}

package gitops_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/gitops"
)

var bg = context.Background()

func TestVersion(t *testing.T) {
	f := execx.NewFake().On("git version", execx.Response{Stdout: "git version 2.39.5 (Apple Git-154)\n"})
	v, err := gitops.Version(bg, f)
	if err != nil || v.Minor != 39 {
		t.Fatalf("%v %v", v, err)
	}
	f = execx.NewFake().On("git version", execx.Response{Stdout: "oops"})
	if _, err := gitops.Version(bg, f); err == nil {
		t.Fatal("garbage")
	}
	if _, err := gitops.Version(bg, execx.NewFake()); err == nil {
		t.Fatal("unscripted")
	}
}

func TestToplevel(t *testing.T) {
	f := execx.NewFake().
		On("git -C /r rev-parse --show-toplevel", execx.Response{Stdout: "/r\n"}).
		On("git -C /x rev-parse --show-toplevel", execx.Response{ExitCode: 128, Stderr: "fatal: not a git repository"})
	if got, err := gitops.Toplevel(bg, f, "/r"); err != nil || got != "/r" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := gitops.Toplevel(bg, f, "/x"); !errors.Is(err, gitops.ErrNotRepo) {
		t.Fatal(err)
	}
	if _, err := gitops.Toplevel(bg, f, "/y"); err == nil || errors.Is(err, gitops.ErrNotRepo) {
		t.Fatalf("exec error should pass through: %v", err)
	}
}

func TestConfigGet(t *testing.T) {
	f := execx.NewFake().
		On("git -C /r config --show-origin --get user.email", execx.Response{Stdout: "file:/h/.config/gitid/git/acme-alice.gitconfig\talice@acme.com\n"}).
		On("git -C /l config --show-origin --get user.email", execx.Response{Stdout: "file:.git/config\tlocal@x.io\n"}).
		On("git -C /n config --show-origin --get user.email", execx.Response{ExitCode: 1}).
		On("git -C /bad config --show-origin --get user.email", execx.Response{ExitCode: 128}).
		On("git -C /plain config --show-origin --get user.email", execx.Response{Stdout: "x@y.z\n"})
	v, err := gitops.ConfigGet(bg, f, "/r", "user.email")
	if err != nil || v.Value != "alice@acme.com" || v.File() != "/h/.config/gitid/git/acme-alice.gitconfig" || v.IsRepoLocal() {
		t.Fatalf("%+v %v", v, err)
	}
	v, _ = gitops.ConfigGet(bg, f, "/l", "user.email")
	if !v.IsRepoLocal() || v.Value != "local@x.io" {
		t.Fatalf("%+v", v)
	}
	if _, err := gitops.ConfigGet(bg, f, "/n", "user.email"); !errors.Is(err, gitops.ErrNotSet) {
		t.Fatal(err)
	}
	if _, err := gitops.ConfigGet(bg, f, "/bad", "user.email"); !errors.Is(err, gitops.ErrNotRepo) {
		t.Fatal(err)
	}
	if v, _ := gitops.ConfigGet(bg, f, "/plain", "user.email"); v.Value != "x@y.z" || v.File() != "" {
		t.Fatalf("%+v", v)
	}
	if _, err := gitops.ConfigGet(bg, f, "/zz", "user.email"); err == nil {
		t.Fatal("unscripted")
	}
	if (gitops.ConfigValue{Origin: "file:/r/.git/config.worktree"}).IsRepoLocal() != true {
		t.Fatal("worktree config")
	}
}

func TestConfigGetAll(t *testing.T) {
	f := execx.NewFake().On("git config --get-all credential.helper", execx.Response{Stdout: "osxkeychain\n\n!helper\n"})
	got, err := gitops.ConfigGetAll(bg, f, "credential.helper")
	if err != nil || strings.Join(got, ",") != "osxkeychain,!helper" {
		t.Fatalf("%v %v", got, err)
	}
	f = execx.NewFake().On("git config --get-all", execx.Response{ExitCode: 1})
	if got, err := gitops.ConfigGetAll(bg, f, "x"); err != nil || got != nil {
		t.Fatalf("%v %v", got, err)
	}
	f = execx.NewFake().On("git config --get-all", execx.Response{ExitCode: 128})
	if _, err := gitops.ConfigGetAll(bg, f, "x"); err == nil {
		t.Fatal("exit 128 is an error")
	}
}

func TestRemotesAndSetURL(t *testing.T) {
	f := execx.NewFake().
		On("git -C /r config --get-regexp", execx.Response{Stdout: "remote.origin.url https://u@github.com/o/r.git\nremote.up.stream.url git@h:o/r.git\n"}).
		On("git -C /none config --get-regexp", execx.Response{ExitCode: 1}).
		On("git -C /bad config --get-regexp", execx.Response{ExitCode: 128}).
		On("git -C /r remote set-url", execx.Response{})
	rs, err := gitops.Remotes(bg, f, "/r")
	if err != nil || len(rs) != 2 || rs[0].Name != "origin" || rs[1].Name != "up.stream" || rs[1].URL != "git@h:o/r.git" {
		t.Fatalf("%+v %v", rs, err)
	}
	if rs, err := gitops.Remotes(bg, f, "/none"); err != nil || rs != nil {
		t.Fatal("no remotes")
	}
	if _, err := gitops.Remotes(bg, f, "/bad"); !errors.Is(err, gitops.ErrNotRepo) {
		t.Fatal(err)
	}
	if err := gitops.SetURL(bg, f, "/r", "origin", "https://a@h/o/r"); err != nil {
		t.Fatal(err)
	}
	if f.Count("git -C /r remote set-url origin https://a@h/o/r") != 1 {
		t.Fatal(f.CallStrings())
	}
	for _, c := range [][2]string{{"", "u"}, {"-x", "u"}, {"origin", "-oProxyCommand=x"}} {
		if err := gitops.SetURL(bg, f, "/r", c[0], c[1]); !errors.Is(err, gitops.ErrBadURL) {
			t.Errorf("%v: %v", c, err)
		}
	}
	if _, err := gitops.Remotes(bg, execx.NewFake(), "/q"); err == nil {
		t.Fatal("unscripted")
	}
}

func TestO1ProgressParser(t *testing.T) {
	sample := "Cloning into 'x'...\rremote: Enumerating objects: 1000, done.\rremote: Counting objects: 100% (1000/1000), done.\rremote: Compressing objects:  50% (5/10)\rReceiving objects:  45% (450/1000), 1.2 MiB | 2 MiB/s\rReceiving objects: 100% (1000/1000), done.\rResolving deltas: 100% (5/5), done.\r"
	var got []gitops.Progress
	for _, l := range strings.Split(sample, "\r") {
		if p, ok := gitops.ParseProgress(l); ok {
			got = append(got, p)
		}
	}
	want := []gitops.Progress{{"Counting objects", 100}, {"Compressing objects", 50}, {"Receiving objects", 45}, {"Receiving objects", 100}, {"Resolving deltas", 100}}
	if len(got) != len(want) {
		t.Fatalf("%+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %+v want %+v", i, got[i], want[i])
		}
	}
	if _, ok := gitops.ParseProgress("fatal: whatever 50% unrelated"); ok {
		t.Fatal("false positive")
	}
}

func TestO2CloneCmdAndProgress(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo")
	f := execx.NewFake().On("git clone", execx.Response{Stderr: "Cloning into 'repo'...\rReceiving objects:  45% (450/1000)\rResolving deltas: 100% (5/5), done.\n"})
	var got []gitops.Progress
	if err := gitops.Clone(bg, f, "https://alice@github.com/o/r.git", dir, func(p gitops.Progress) { got = append(got, p) }); err != nil {
		t.Fatal(err)
	}
	c := f.Calls()[0]
	if c.String() != "git clone --progress -- https://alice@github.com/o/r.git "+dir {
		t.Fatalf("%s", c.String())
	}
	env := strings.Join(c.Env, "|")
	if !strings.Contains(env, "GIT_TERMINAL_PROMPT=0") || !strings.Contains(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes") {
		t.Fatalf("env %v", c.Env)
	}
	if len(got) != 2 || got[0].Pct != 45 {
		t.Fatalf("%+v", got)
	}
	// terminal mode: no batch env
	tc, err := gitops.CloneCmd("https://a@h/o/r", dir, false)
	if err != nil || len(tc.Env) != 0 {
		t.Fatalf("%+v %v", tc, err)
	}
	if err := gitops.Clone(bg, f, "https://a@h/o/r", dir, nil); err != nil {
		t.Fatal(err)
	}
}

func TestO3AuthClassification(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "r")
	for _, stderr := range []string{
		"remote: Invalid username or password.\nfatal: Authentication failed for 'https://github.com/o/r.git/'",
		"git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository.",
		"fatal: could not read Username for 'https://github.com': terminal prompts disabled",
		"Host key verification failed.",
	} {
		f := execx.NewFake().On("git clone", execx.Response{ExitCode: 128, Stderr: stderr})
		err := gitops.Clone(bg, f, "https://a@h/o/r", dir, nil)
		if !errors.Is(err, gitops.ErrAuth) {
			t.Errorf("%q: %v", stderr, err)
		}
		var ce *gitops.CloneError
		if !errors.As(err, &ce) || ce.Stderr == "" || !strings.Contains(err.Error(), "authentication") {
			t.Errorf("CloneError: %v", err)
		}
	}
	f := execx.NewFake().On("git clone", execx.Response{ExitCode: 128, Stderr: "ERROR: Repository not found.\nfatal: Could not read from remote repository."})
	if err := gitops.Clone(bg, f, "https://a@h/o/r", dir, nil); !errors.Is(err, gitops.ErrNotFound) {
		t.Fatal(err)
	}
	f = execx.NewFake().On("git clone", execx.Response{ExitCode: 128, Stderr: "fatal: something else"})
	err := gitops.Clone(bg, f, "https://a@h/o/r", dir, nil)
	if err == nil || errors.Is(err, gitops.ErrAuth) || !strings.Contains(err.Error(), "something else") {
		t.Fatal(err)
	}
	// credentials in stderr are redacted
	f = execx.NewFake().On("git clone", execx.Response{ExitCode: 128, Stderr: "fatal: unable to access 'https://u:SECRETTOKEN@h/o/r.git/'"})
	err = gitops.Clone(bg, f, "https://a@h/o/r", dir, nil)
	var ce *gitops.CloneError
	if !errors.As(err, &ce) || strings.Contains(ce.Stderr, "SECRETTOKEN") || strings.Contains(err.Error(), "SECRETTOKEN") {
		t.Fatalf("secret leaked: %v", err)
	}
	// cancellation is reported as such
	ctx, cancel := context.WithCancel(bg)
	cancel()
	f = execx.NewFake().On("git clone", execx.Response{})
	if err := gitops.Clone(ctx, f, "https://a@h/o/r", dir, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestO4Targets(t *testing.T) {
	d := t.TempDir()
	f := execx.NewFake()
	if err := gitops.Clone(bg, f, "https://a@h/o/r", d, nil); !errors.Is(err, gitops.ErrTargetExists) {
		t.Fatalf("existing dir: %v", err)
	}
	for _, bad := range []string{"relative/dir", filepath.Join(d, "-x"), ""} {
		if err := gitops.Clone(bg, f, "https://a@h/o/r", bad, nil); !errors.Is(err, gitops.ErrBadTarget) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	for _, bad := range []string{"", ".", "..", "-x", "a/b", "a\\b", "a..b", "a\nb", "--upload-pack=x"} {
		if err := gitops.ValidateDirName(bad); !errors.Is(err, gitops.ErrBadTarget) {
			t.Errorf("ValidateDirName(%q): %v", bad, err)
		}
	}
	if err := gitops.ValidateDirName("my-repo.v2"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "-oProxyCommand=x", "--upload-pack=x", "a b", "a\nb"} {
		if err := gitops.Clone(bg, f, bad, filepath.Join(d, "new"), nil); !errors.Is(err, gitops.ErrBadURL) {
			t.Errorf("url %q: %v", bad, err)
		}
	}
	if len(f.Calls()) != 0 {
		t.Fatalf("git was executed: %v", f.CallStrings())
	}
	if _, err := os.Stat(filepath.Join(d, "new")); err == nil {
		t.Fatal("target created")
	}
}

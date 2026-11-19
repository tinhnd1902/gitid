package online

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/platform"
)

func resolver(t *testing.T, vars map[string]string, fake *execx.Fake) Resolver {
	t.Helper()
	return Resolver{Env: testEnv(t, vars), Run: fake, UseGitCredential: true}
}

// noSecretInCalls is SEC-3 for the token sources: the token is in no argv,
// env or stdin the fake saw.
func noSecretInCalls(t *testing.T, fake *execx.Fake) {
	t.Helper()
	for _, c := range fake.Calls() {
		blob := strings.Join(append(append([]string{c.Name, c.Stdin}, c.Args...), c.Env...), "\x00")
		if strings.Contains(blob, fixtureToken) {
			t.Errorf("token in the call %s", c.String())
		}
	}
}

// TK-1: the variable name.
func TestTK1EnvName(t *testing.T) {
	if got := ghAccount().EnvName(); got != "GITID_TOKEN_ACME__ALICE" {
		t.Errorf("got %q", got)
	}
	if got := EnvName("my-co", "alice-2"); got != "GITID_TOKEN_MY_CO__ALICE_2" {
		t.Errorf("got %q", got)
	}
}

// TK-2: two accounts with the same variable name: the variable is ignored
// for both, with a note; the next sources are tried.
func TestTK2EnvCollision(t *testing.T) {
	// "x-"/"y" and "x"/"-y" both give GITID_TOKEN_X___Y
	a := Account{Ref: "x-/y", Slug: "x-", ID: "y", Platform: platform.GitHub, Host: "github.com", Username: "u1"}
	b := Account{Ref: "x/-y", Slug: "x", ID: "-y", Platform: platform.GitHub, Host: "github.com", Username: "u2"}
	if a.EnvName() != b.EnvName() {
		t.Fatalf("fixture: %s vs %s", a.EnvName(), b.EnvName())
	}
	fake := execx.NewFake()
	r := resolver(t, map[string]string{a.EnvName(): fixtureToken}, fake)
	for _, acc := range []Account{a, b} {
		tok, err := r.Resolve(context.Background(), acc, []Account{a, b})
		if err != nil || tok.Source != SourceNone || tok.Note != NoteEnvAmbiguous {
			t.Errorf("%s: %+v %v", acc.Ref, tok, err)
		}
	}
	if fake.Count("gh") == 0 {
		t.Error("the later sources were not tried")
	}
	// the note appears only when the variable is set
	r2 := resolver(t, nil, execx.NewFake())
	tok, _ := r2.Resolve(context.Background(), a, []Account{a, b})
	if tok.Note != "" {
		t.Errorf("note %q without a variable", tok.Note)
	}
}

// TK-3 / TK-4: env wins with no subprocess; an empty value is unset.
func TestTK3And4Env(t *testing.T) {
	fake := execx.NewFake()
	a := ghAccount()
	r := resolver(t, map[string]string{a.EnvName(): "  " + fixtureToken + "\n"}, fake)
	tok, err := r.Resolve(context.Background(), a, []Account{a})
	if err != nil || tok.Source != SourceEnv || !tok.Found() || tok.Secret.reveal() != fixtureToken {
		t.Fatalf("%+v %v", tok, err)
	}
	if n := len(fake.Calls()); n != 0 {
		t.Errorf("%d subprocess calls", n)
	}
	r = resolver(t, map[string]string{a.EnvName(): ""}, execx.NewFake())
	tok, _ = r.Resolve(context.Background(), a, []Account{a})
	if tok.Source != SourceNone {
		t.Errorf("empty value gave %s", tok.Source)
	}
}

const kcCall = "security find-generic-password -s gitid -a github.com/alice -w"

// TK-5 / TK-6 / TK-7: the keychain.
func TestTK5To7Keychain(t *testing.T) {
	a := ghAccount()
	fake := execx.NewFake().On(kcCall, execx.Response{Stdout: fixtureToken + "\n"})
	tok, _ := resolver(t, nil, fake).Resolve(context.Background(), a, nil)
	if tok.Source != SourceKeychain || tok.Secret.reveal() != fixtureToken {
		t.Fatalf("%+v", tok)
	}
	if fake.Count("gh") != 0 {
		t.Error("later sources were asked after a hit")
	}
	noSecretInCalls(t, fake)

	// exit 44: not found, the next source is tried
	fake = execx.NewFake().On(kcCall, execx.Response{ExitCode: 44})
	tok, _ = resolver(t, nil, fake).Resolve(context.Background(), a, nil)
	if tok.Source != SourceNone || fake.Count("gh auth token") != 1 {
		t.Errorf("%+v %v", tok, fake.CallStrings())
	}

	// not macOS: security is never called
	e := testEnvLinux(t)
	fake = execx.NewFake().On(kcCall, execx.Response{Stdout: "x"})
	tok, _ = Resolver{Env: e, Run: fake}.Resolve(context.Background(), a, nil)
	if fake.Count("security") != 0 || tok.Source != SourceNone {
		t.Errorf("linux: %+v %v", tok, fake.CallStrings())
	}

	// security missing from PATH
	e = testEnv(t, nil)
	e.LookPath = func(f string) (string, error) {
		if f == "security" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + f, nil
	}
	fake = execx.NewFake()
	_, _ = Resolver{Env: e, Run: fake}.Resolve(context.Background(), a, nil)
	if fake.Count("security") != 0 {
		t.Error("security called although it is not installed")
	}
}

func testEnvLinux(t *testing.T) env.Env {
	e := testEnv(t, nil)
	e.GOOS = "linux"
	return e
}

// TK-8 .. TK-11: gh.
func TestTK8To11GH(t *testing.T) {
	a := ghAccount()
	const call = "gh auth token --hostname=github.com --user=alice"
	fake := execx.NewFake().On(call, execx.Response{Stdout: fixtureToken + "\n"})
	tok, _ := resolver(t, nil, fake).Resolve(context.Background(), a, nil)
	if tok.Source != SourceGH || tok.Secret.reveal() != fixtureToken {
		t.Fatalf("%+v", tok)
	}
	for _, c := range fake.Calls() {
		if c.Name == "gh" && (c.Args[2] != "--hostname=github.com" || c.Args[3] != "--user=alice") {
			t.Errorf("args %v are not in the = form", c.Args)
		}
	}
	noSecretInCalls(t, fake)

	// TK-9: an older gh does not know --user: asked again without it
	fake = execx.NewFake()
	fake.On(call, execx.Response{ExitCode: 1, Stderr: "unknown flag: --user"})
	fake.On("gh auth token --hostname=github.com", execx.Response{Stdout: fixtureToken})
	tok, _ = resolver(t, nil, fake).Resolve(context.Background(), a, nil)
	if tok.Source != SourceGH || tok.Secret.reveal() != fixtureToken {
		t.Fatalf("old gh: %+v %v", tok, fake.CallStrings())
	}
	if n := fake.Count("gh auth token"); n != 2 {
		t.Errorf("%d gh calls", n)
	}

	// TK-10: gh not installed
	e := testEnv(t, nil)
	e.LookPath = func(f string) (string, error) {
		if f == "gh" {
			return "", errors.New("nope")
		}
		return "/usr/bin/" + f, nil
	}
	fake = execx.NewFake()
	_, _ = Resolver{Env: e, Run: fake}.Resolve(context.Background(), a, nil)
	if fake.Count("gh") != 0 {
		t.Error("gh called although it is not installed")
	}

	// TK-11: not logged in
	fake = execx.NewFake().On("gh auth token", execx.Response{ExitCode: 1, Stderr: "no oauth token found"})
	tok, _ = resolver(t, nil, fake).Resolve(context.Background(), a, nil)
	if tok.Source != SourceNone {
		t.Errorf("%+v", tok)
	}
}

// TK-12: glab, for GitLab accounts only.
func TestTK12Glab(t *testing.T) {
	d := glAccount()
	fake := execx.NewFake().On("glab config get token --host=git.example.com", execx.Response{Stdout: fixtureToken + "\n"})
	tok, _ := resolver(t, nil, fake).Resolve(context.Background(), d, nil)
	if tok.Source != SourceGlab || tok.Secret.reveal() != fixtureToken {
		t.Fatalf("%+v", tok)
	}
	if fake.Count("gh") != 0 {
		t.Error("gh asked for a GitLab account")
	}
	fake = execx.NewFake().On("glab", execx.Response{Stdout: fixtureToken})
	_, _ = resolver(t, nil, fake).Resolve(context.Background(), ghAccount(), nil)
	if fake.Count("glab") != 0 {
		t.Error("glab asked for a GitHub account")
	}
	// an empty answer is no token
	fake = execx.NewFake().On("glab config get token", execx.Response{Stdout: "\n"})
	tok, _ = resolver(t, nil, fake).Resolve(context.Background(), d, nil)
	if tok.Source != SourceNone {
		t.Errorf("%+v", tok)
	}
}

func credCalls(fake *execx.Fake) []execx.Cmd {
	var out []execx.Cmd
	for _, c := range fake.Calls() {
		if c.Name == "git" {
			out = append(out, c)
		}
	}
	return out
}

// TK-13 / TK-14 / TK-15 / TK-16 / TK-17: git credential.
func TestTK13To17GitCredential(t *testing.T) {
	a := ghAccount()
	fake := execx.NewFake().On("git -c credential.interactive=never -c credential.interactive=false credential fill",
		execx.Response{Stdout: "protocol=https\nhost=github.com\nusername=alice\npassword=" + fixtureToken + "\n"})
	tok, _ := resolver(t, nil, fake).Resolve(context.Background(), a, nil)
	if tok.Source != SourceGit || tok.Secret.reveal() != fixtureToken {
		t.Fatalf("%+v", tok)
	}
	c := credCalls(fake)[0]
	if c.Stdin != "protocol=https\nhost=github.com\nusername=alice\n\n" {
		t.Errorf("stdin %q", c.Stdin)
	}
	want := map[string]bool{"GIT_TERMINAL_PROMPT=0": true, "GIT_ASKPASS=false": true, "SSH_ASKPASS=false": true, "GCM_INTERACTIVE=never": true}
	for _, e := range c.Env {
		delete(want, e)
	}
	if len(want) > 0 {
		t.Errorf("env lacks %v (has %v)", want, c.Env)
	}
	noSecretInCalls(t, fake)

	// TK-14: a port stays in the host
	ghe := Account{Ref: "x/y", Slug: "x", ID: "y", Platform: platform.GitHub, Host: "ghe.example.com:8443", Username: "alice"}
	fake = execx.NewFake().On("git -c", execx.Response{Stdout: "password=" + fixtureToken + "\n"})
	_, _ = resolver(t, nil, fake).Resolve(context.Background(), ghe, nil)
	if got := credCalls(fake)[0].Stdin; got != "protocol=https\nhost=ghe.example.com:8443\nusername=alice\n\n" {
		t.Errorf("stdin %q", got)
	}

	// TK-15: Azure adds the organization as the path
	az := Account{Ref: "az/dev", Slug: "az", ID: "dev", Platform: platform.AzureDevOps, Host: "dev.azure.com", Username: "dev", Orgs: []string{"acme"}}
	fake = execx.NewFake().On("git -c", execx.Response{Stdout: "password=" + fixtureToken + "\n"})
	_, _ = resolver(t, nil, fake).Resolve(context.Background(), az, nil)
	if got := credCalls(fake)[0].Stdin; !strings.Contains(got, "\npath=acme\n") || !strings.HasSuffix(got, "\n\n") {
		t.Errorf("stdin %q", got)
	}

	// TK-16: no password line
	fake = execx.NewFake().On("git -c", execx.Response{Stdout: "protocol=https\nhost=github.com\nusername=alice\n"})
	tok, _ = resolver(t, nil, fake).Resolve(context.Background(), a, nil)
	if tok.Source != SourceNone {
		t.Errorf("%+v", tok)
	}

	// TK-17: exit 128 "terminal prompts disabled": none, and the output is not an error
	fake = execx.NewFake().On("git -c", execx.Response{ExitCode: 128, Stdout: "password=" + fixtureToken, Stderr: "fatal: could not read Username: terminal prompts disabled"})
	tok, err := resolver(t, nil, fake).Resolve(context.Background(), a, nil)
	if tok.Source != SourceNone || err != nil || tok.Found() {
		t.Errorf("%+v %v", tok, err)
	}
}

// TK-18: git credential can be switched off.
func TestTK18GitCredentialOff(t *testing.T) {
	fake := execx.NewFake().On("git -c", execx.Response{Stdout: "password=x\n"})
	r := Resolver{Env: testEnv(t, nil), Run: fake, UseGitCredential: false}
	tok, _ := r.Resolve(context.Background(), ghAccount(), nil)
	if fake.Count("git") != 0 || tok.Source != SourceNone {
		t.Errorf("%+v %v", tok, fake.CallStrings())
	}
}

// TK-19 / TK-20: values that could inject a protocol line or a flag.
func TestTK19And20Injection(t *testing.T) {
	for _, user := range []string{"al\nice", "al\rice", "al\x00ice", "-x", ""} {
		a := ghAccount()
		a.Username = user
		fake := execx.NewFake()
		tok, err := resolver(t, nil, fake).Resolve(context.Background(), a, nil)
		if err != nil || tok.Source != SourceNone || tok.Note != NoteInvalidValue {
			t.Errorf("%q: %+v %v", user, tok, err)
		}
		if n := len(fake.Calls()); n != 0 {
			t.Errorf("%q: %d subprocess calls", user, n)
		}
	}
	a := ghAccount()
	a.Host = "-evil.example.com"
	fake := execx.NewFake()
	tok, _ := resolver(t, nil, fake).Resolve(context.Background(), a, nil)
	if tok.Note != NoteInvalidValue || len(fake.Calls()) != 0 {
		t.Errorf("host: %+v %v", tok, fake.CallStrings())
	}
}

// TK-21: env, then keychain, then gh, then git; a hit stops the chain.
func TestTK21Order(t *testing.T) {
	a := ghAccount()
	all := func() *execx.Fake {
		return execx.NewFake().
			On("security", execx.Response{Stdout: "kc"}).
			On("gh auth token", execx.Response{Stdout: "gh"}).
			On("git -c", execx.Response{Stdout: "password=git\n"})
	}
	fake := all()
	tok, _ := resolver(t, map[string]string{a.EnvName(): "env"}, fake).Resolve(context.Background(), a, nil)
	if tok.Source != SourceEnv || len(fake.Calls()) != 0 {
		t.Errorf("env: %+v %v", tok, fake.CallStrings())
	}
	fake = all()
	tok, _ = resolver(t, nil, fake).Resolve(context.Background(), a, nil)
	if tok.Source != SourceKeychain || fake.Count("gh") != 0 || fake.Count("git") != 0 {
		t.Errorf("keychain: %+v %v", tok, fake.CallStrings())
	}
	fake = all().On("security", execx.Response{ExitCode: 44})
	tok, _ = resolver(t, nil, fake).Resolve(context.Background(), a, nil)
	if tok.Source != SourceGH || fake.Count("git") != 0 {
		t.Errorf("gh: %+v %v", tok, fake.CallStrings())
	}
	fake = all().On("security", execx.Response{ExitCode: 44}).On("gh auth token", execx.Response{ExitCode: 1})
	tok, _ = resolver(t, nil, fake).Resolve(context.Background(), a, nil)
	if tok.Source != SourceGit {
		t.Errorf("git: %+v %v", tok, fake.CallStrings())
	}
}

// the chain stops on a cancelled context
func TestResolveCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := resolver(t, nil, execx.NewFake()).Resolve(ctx, ghAccount(), nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err %v", err)
	}
	// cancelled while a source runs
	ctx, cancel = context.WithCancel(context.Background())
	block := make(chan struct{})
	fake := execx.NewFake().On("security", execx.Response{Block: block})
	done := make(chan error, 1)
	go func() { _, err := resolver(t, nil, fake).Resolve(ctx, ghAccount(), nil); done <- err }()
	for fake.Count("security") == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Resolve did not stop")
	}
}

// TK-22: the cache keeps hits and misses, Forget asks again, concurrent
// resolves run the chain once.
func TestTK22Cache(t *testing.T) {
	a := ghAccount()
	fake := execx.NewFake().On("gh auth token", execx.Response{Stdout: fixtureToken})
	r := resolver(t, nil, fake)
	c := NewCache()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, err := c.Resolve(context.Background(), r, a, nil)
			if err != nil || tok.Source != SourceGH {
				t.Errorf("%+v %v", tok, err)
			}
		}()
	}
	wg.Wait()
	if n := fake.Count("gh auth token"); n != 1 {
		t.Fatalf("%d gh calls for 8 resolves", n)
	}
	if tok, ok := c.Peek(a.Key()); !ok || tok.Source != SourceGH {
		t.Errorf("peek %+v %v", tok, ok)
	}
	c.Forget(a.Key())
	if _, ok := c.Peek(a.Key()); ok {
		t.Error("Forget kept the entry")
	}
	_, _ = c.Resolve(context.Background(), r, a, nil)
	if n := fake.Count("gh auth token"); n != 2 {
		t.Errorf("after Forget: %d gh calls", n)
	}
	// a miss is cached too
	b := glAccount()
	missFake := execx.NewFake()
	rm := resolver(t, nil, missFake)
	_, _ = c.Resolve(context.Background(), rm, b, nil)
	n := len(missFake.Calls())
	_, _ = c.Resolve(context.Background(), rm, b, nil)
	if len(missFake.Calls()) != n {
		t.Error("a miss was resolved again")
	}
	// Keep drops the keys not listed
	c.Keep(map[string]bool{a.Key(): true})
	if _, ok := c.Peek(b.Key()); ok {
		t.Error("Keep kept an unlisted key")
	}
	// a cancelled resolve is not remembered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c2 := NewCache()
	_, _ = c2.Resolve(ctx, rm, a, nil)
	if _, ok := c2.Peek(a.Key()); ok {
		t.Error("a cancelled resolve was cached")
	}
}

// the key changes with the username, host and platform
func TestKeyChanges(t *testing.T) {
	a := ghAccount()
	k := a.Key()
	for _, mod := range []func(*Account){
		func(x *Account) { x.Username = "other" },
		func(x *Account) { x.Host = "ghe.example.com" },
		func(x *Account) { x.Platform = platform.GitLab },
	} {
		b := a
		mod(&b)
		if b.Key() == k {
			t.Errorf("key did not change: %+v", b)
		}
	}
}

// GH-12 helper: a git credential that is not in GitHub's token form
func TestLooksLikePassword(t *testing.T) {
	gh := platform.GitHub
	cases := []struct {
		tok  Token
		want bool
	}{
		{Token{Secret: NewSecret("hunter2"), Source: SourceGit}, true},
		{Token{Secret: NewSecret("ghp_abc"), Source: SourceGit}, false},
		{Token{Secret: NewSecret("github_pat_abc"), Source: SourceGit}, false},
		{Token{Secret: NewSecret("hunter2"), Source: SourceEnv}, false},
		{Token{Source: SourceGit}, false},
	}
	for _, c := range cases {
		if got := c.tok.LooksLikePassword(gh); got != c.want {
			t.Errorf("%+v: %v", c.tok.Source, got)
		}
	}
	if (Token{Secret: NewSecret("hunter2"), Source: SourceGit}).LooksLikePassword(platform.GitLab) {
		t.Error("GitLab")
	}
}

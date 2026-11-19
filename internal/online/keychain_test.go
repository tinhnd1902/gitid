package online

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/tinhnd1902/gitid/internal/execx"
)

// fakeKeychain is a fake `security` that keeps what `security -i` is told to
// add, so that a write can be read back through the same fake runner.
type fakeKeychain struct {
	mu    sync.Mutex
	items map[string]string // account -> token
}

func newFakeKeychain(f *execx.Fake) *fakeKeychain {
	k := &fakeKeychain{items: map[string]string{}}
	f.On("security -i", execx.Response{Do: func(c execx.Cmd) error {
		// add-generic-password -U -s gitid -a "host/user" -l gitid -X <hex>
		fields := strings.Fields(strings.TrimSpace(c.Stdin))
		var acct, hx string
		for i, w := range fields {
			switch w {
			case "-a":
				acct = strings.Trim(fields[i+1], `"`)
			case "-X":
				hx = fields[i+1]
			}
		}
		b, err := hex.DecodeString(hx)
		if err != nil || acct == "" {
			return errors.New("bad script")
		}
		k.mu.Lock()
		k.items[acct] = string(b)
		k.mu.Unlock()
		return nil
	}})
	f.On("security delete-generic-password", execx.Response{Do: func(c execx.Cmd) error {
		k.mu.Lock()
		defer k.mu.Unlock()
		for i, a := range c.Args {
			if a == "-a" {
				if _, ok := k.items[c.Args[i+1]]; ok {
					delete(k.items, c.Args[i+1])
					return nil
				}
			}
		}
		return nil
	}})
	return k
}

// KC-1: the token is in the stdin script (as hex), in no argument.
func TestKC1SetUsesStdin(t *testing.T) {
	fake := execx.NewFake()
	kc := newFakeKeychain(fake)
	k := Keychain{Env: testEnv(t, nil), Run: fake}
	a := ghAccount()
	if err := k.Set(context.Background(), a, fixtureToken); err != nil {
		t.Fatal(err)
	}
	calls := fake.Calls()
	if len(calls) != 1 || calls[0].String() != "security -i" {
		t.Fatalf("%v", fake.CallStrings())
	}
	c := calls[0]
	for _, part := range append(append([]string{c.Name}, c.Args...), c.Env...) {
		if strings.Contains(part, fixtureToken) || strings.Contains(part, hex.EncodeToString([]byte(fixtureToken))) {
			t.Errorf("the token is in argv/env: %q", part)
		}
	}
	if strings.Contains(c.Stdin, fixtureToken) {
		t.Errorf("the plain token is in the script: %q", c.Stdin)
	}
	want := `add-generic-password -U -s gitid -a "github.com/alice" -l gitid -X ` + hex.EncodeToString([]byte(fixtureToken)) + "\n"
	if c.Stdin != want {
		t.Errorf("script %q, want %q", c.Stdin, want)
	}
	if kc.items["github.com/alice"] != fixtureToken {
		t.Errorf("items %v", kc.items)
	}
}

// KC-3 / KC-4: tokens that cannot be stored; no call is made.
func TestKC3And4BadTokens(t *testing.T) {
	for _, tok := range []string{"", " ", "a b", "a\nb", "a\x00b", "a\tb", "tok\r", "é", strings.Repeat("a", MaxTokenBytes+1), "\x7f"} {
		fake := execx.NewFake()
		err := Keychain{Env: testEnv(t, nil), Run: fake}.Set(context.Background(), ghAccount(), tok)
		if !errors.Is(err, ErrBadToken) || len(fake.Calls()) != 0 {
			t.Errorf("%q: %v %v", tok, err, fake.CallStrings())
		}
	}
	// quotes and backslashes are fine: the token travels as hex
	for _, tok := range []string{`a"b`, `a\b`, "ghp_" + strings.Repeat("A", 36), strings.Repeat("z", MaxTokenBytes)} {
		fake := execx.NewFake()
		newFakeKeychain(fake)
		if err := (Keychain{Env: testEnv(t, nil), Run: fake}).Set(context.Background(), ghAccount(), tok); err != nil {
			t.Errorf("%q: %v", tok, err)
		}
	}
	// accounts that would break the script
	for _, host := range []string{`gi"thub.com`, "git hub.com", "-x.com", "a\nb", "gi\\t.com", ""} {
		a := ghAccount()
		a.Host = host
		fake := execx.NewFake()
		err := Keychain{Env: testEnv(t, nil), Run: fake}.Set(context.Background(), a, "tok")
		if !errors.Is(err, ErrBadKeychainAccount) || len(fake.Calls()) != 0 {
			t.Errorf("host %q: %v", host, err)
		}
	}
}

// KC-5: not on macOS (or without the security tool): nothing runs.
func TestKC5Unsupported(t *testing.T) {
	e := testEnv(t, nil)
	e.GOOS = "linux"
	fake := execx.NewFake()
	k := Keychain{Env: e, Run: fake}
	if err := k.Set(context.Background(), ghAccount(), "tok"); !errors.Is(err, ErrKeychainUnsupported) {
		t.Errorf("%v", err)
	}
	if _, err := k.Remove(context.Background(), ghAccount()); !errors.Is(err, ErrKeychainUnsupported) {
		t.Errorf("%v", err)
	}
	e.GOOS = "darwin"
	e.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	if err := (Keychain{Env: e, Run: fake}).Set(context.Background(), ghAccount(), "tok"); !errors.Is(err, ErrKeychainUnsupported) {
		t.Errorf("%v", err)
	}
	if len(fake.Calls()) != 0 {
		t.Errorf("%v", fake.CallStrings())
	}
}

// KC-6: remove; "not found" (44) is not an error.
func TestKC6Remove(t *testing.T) {
	fake := execx.NewFake()
	kc := newFakeKeychain(fake)
	k := Keychain{Env: testEnv(t, nil), Run: fake}
	if err := k.Set(context.Background(), ghAccount(), "tok"); err != nil {
		t.Fatal(err)
	}
	removed, err := k.Remove(context.Background(), ghAccount())
	if err != nil || !removed || len(kc.items) != 0 {
		t.Fatalf("%v %v", removed, err)
	}
	if got := fake.CallStrings()[1]; got != "security delete-generic-password -s gitid -a github.com/alice" {
		t.Errorf("%q", got)
	}
	f2 := execx.NewFake().On("security delete-generic-password", execx.Response{ExitCode: 44})
	removed, err = (Keychain{Env: testEnv(t, nil), Run: f2}).Remove(context.Background(), ghAccount())
	if err != nil || removed {
		t.Errorf("not found: %v %v", removed, err)
	}
	f3 := execx.NewFake().On("security delete-generic-password", execx.Response{ExitCode: 1, Stderr: "boom " + fixtureToken})
	_, err = (Keychain{Env: testEnv(t, nil), Run: f3}).Remove(context.Background(), ghAccount())
	if !errors.Is(err, ErrKeychainFailed) || strings.Contains(err.Error(), fixtureToken) {
		t.Errorf("%v", err)
	}
}

// A failing `security` is reported without its output.
func TestKCSetFailureHasNoOutput(t *testing.T) {
	fake := execx.NewFake().On("security -i", execx.Response{ExitCode: 2, Stdout: fixtureToken, Stderr: fixtureToken})
	err := Keychain{Env: testEnv(t, nil), Run: fake}.Set(context.Background(), ghAccount(), fixtureToken)
	if !errors.Is(err, ErrKeychainFailed) {
		t.Fatalf("%v", err)
	}
	noTokenIn(t, "keychain", err)
}

// KC-8 (online level): what Set writes, the read path of the Resolver finds,
// as the "keychain" source, for every platform's account.
func TestKC8RoundTrip(t *testing.T) {
	for _, a := range []Account{ghAccount(), glAccount(), gtAccount(), bbAccount(), azAccount("acme")} {
		fake := execx.NewFake()
		kc := newFakeKeychain(fake)
		k := Keychain{Env: testEnv(t, nil), Run: fake}
		if err := k.Set(context.Background(), a, fixtureToken); err != nil {
			t.Fatal(err)
		}
		// serve the stored item to the read path
		kc.mu.Lock()
		stored := kc.items[KeychainAccount(a)]
		kc.mu.Unlock()
		fake.On("security find-generic-password -s gitid -a "+KeychainAccount(a)+" -w", execx.Response{Stdout: stored + "\n"})
		r := Resolver{Env: testEnv(t, nil), Run: fake}
		tok, err := r.Resolve(context.Background(), a, []Account{a})
		if err != nil || tok.Source != SourceKeychain || tok.Secret.reveal() != fixtureToken {
			t.Errorf("%s: %+v %v", a.Ref, tok, err)
		}
		// no argument of any call holds the token
		for _, c := range fake.Calls() {
			for _, part := range append([]string{c.Name}, c.Args...) {
				if strings.Contains(part, fixtureToken) {
					t.Errorf("%s: token in argv of %s", a.Ref, c.String())
				}
			}
		}
	}
}

// Remove of an existing item succeeds.
func TestKCRemoveOK(t *testing.T) {
	fake := execx.NewFake().On("security delete-generic-password", execx.Response{})
	if _, err := (Keychain{Env: testEnv(t, nil), Run: fake}).Remove(context.Background(), ghAccount()); err != nil {
		t.Fatal(err)
	}
}

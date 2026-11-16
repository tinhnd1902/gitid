package keys_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/keys"
	"github.com/tinhnd1902/gitid/internal/platform"
	"github.com/tinhnd1902/gitid/internal/testutil"
	"github.com/tinhnd1902/gitid/internal/testutil/fixtures"
)

func carol() (config.Company, config.Account) {
	c := fixtures.Acme()
	return c, c.Accounts[2]
}

func keygenFake(privMode os.FileMode) *execx.Fake {
	return execx.NewFake().On("ssh-keygen", execx.Response{Do: func(c execx.Cmd) error {
		var path string
		for i, a := range c.Args {
			if a == "-f" {
				path = c.Args[i+1]
			}
		}
		if err := os.WriteFile(path, []byte("PRIVATE KEY MATERIAL"), 0o600); err != nil {
			return err
		}
		if err := os.Chmod(path, privMode); err != nil {
			return err
		}
		return os.WriteFile(path+".pub", []byte("ssh-ed25519 AAAAC3 carol@acme.com gitid:acme/carol\n"), 0o644)
	}})
}

func TestK1K2PreCheck(t *testing.T) {
	e := testutil.TempEnv(t)
	c, a := carol()
	path := keys.PrivatePath(e, c, a)
	if path != filepath.Join(e.Home, ".ssh/gitid/acme-carol") {
		t.Fatal(path)
	}
	if err := keys.PreCheck(path); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, path, "x", 0o600)
	f := keygenFake(0o600)
	if _, err := keys.Generate(context.Background(), e, f, c, a, false); !errors.Is(err, keys.ErrKeyExists) {
		t.Fatalf("K1 err=%v", err)
	}
	if len(f.Calls()) != 0 {
		t.Fatal("K1: runner called")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, path+".pub", "pub", 0o644)
	if _, err := keys.Generate(context.Background(), e, f, c, a, false); !errors.Is(err, keys.ErrKeyExists) || len(f.Calls()) != 0 {
		t.Fatalf("K2 err=%v", err)
	}
	// a dangling symlink also blocks
	if err := os.Remove(path + ".pub"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/nonexistent", path); err != nil {
		t.Fatal(err)
	}
	if err := keys.PreCheck(path); !errors.Is(err, keys.ErrKeyExists) {
		t.Fatalf("dangling symlink: %v", err)
	}
	// ENOTDIR-like error from Lstat surfaces
	testutil.WriteFile(t, filepath.Join(e.Home, "file"), "x", 0o600)
	if err := keys.PreCheck(filepath.Join(e.Home, "file/key")); err == nil || errors.Is(err, keys.ErrKeyExists) {
		t.Fatalf("err=%v", err)
	}
}

func TestK3K4Args(t *testing.T) {
	c, a := carol()
	e := testutil.TempEnv(t)
	cmd, err := keys.Cmd(e, c, a, false)
	if err != nil {
		t.Fatal(err)
	}
	s := cmd.String()
	for _, want := range []string{"ssh-keygen", "-t ed25519", "-a 100", "-f " + filepath.Join(e.Home, ".ssh/gitid/acme-carol"), "-C carol@acme.com gitid:acme/carol"} {
		if !strings.Contains(s, want) {
			t.Errorf("K3 missing %q in %q", want, s)
		}
	}
	for _, arg := range cmd.Args {
		if arg == "-N" {
			t.Fatal("K3: -N must not be passed in passphrase mode")
		}
	}
	cmd, _ = keys.Cmd(e, c, a, true)
	n := len(cmd.Args)
	if cmd.Args[n-2] != "-N" || cmd.Args[n-1] != "" {
		t.Fatalf("K4 args=%q", cmd.Args)
	}
	// ssh-keygen missing
	e.LookPath = func(string) (string, error) { return "", errors.New("nope") }
	if _, err := keys.Cmd(e, c, a, false); !errors.Is(err, keys.ErrNoKeygen) {
		t.Fatal(err)
	}
	if keys.Comment(c, a) != "carol@acme.com gitid:acme/carol" {
		t.Fatal(keys.Comment(c, a))
	}
	// path starting with '-' is refused
	e2 := testutil.TempEnv(t)
	e2.Home = "-weird"
	if _, err := keys.Cmd(e2, c, config.Account{ID: "x", SSHKey: "-oProxy"}, false); !errors.Is(err, keys.ErrInvalidAction) {
		t.Fatal(err)
	}
}

func TestK5CreatesKeysDir(t *testing.T) {
	e := testutil.TempEnv(t)
	c, a := carol()
	if _, err := keys.Generate(context.Background(), e, keygenFake(0o600), c, a, false); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(e.Paths().KeysDir)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("%v %v", fi, err)
	}
	// existing dir with other mode is not chmod'ed
	e2 := testutil.TempEnv(t)
	if err := os.MkdirAll(e2.Paths().KeysDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(e2.Paths().KeysDir, 0o755)
	if _, err := keys.Generate(context.Background(), e2, keygenFake(0o600), c, a, true); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(e2.Paths().KeysDir); fi.Mode().Perm() != 0o755 {
		t.Fatal("existing dir chmod'ed")
	}
}

func TestK6PrivateKeyNeverRead(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read mode 0000 files")
	}
	e := testutil.TempEnv(t)
	c, a := carol()
	f := keygenFake(0o000)
	pub, err := keys.Generate(context.Background(), e, f, c, a, false)
	if err != nil {
		t.Fatalf("generation must succeed without reading the private key: %v", err)
	}
	if !strings.HasPrefix(pub.Content, "ssh-ed25519 ") || pub.PubPath != pub.PrivatePath+".pub" {
		t.Fatalf("%+v", pub)
	}
	if _, err := os.ReadFile(pub.PrivatePath); err == nil {
		t.Fatal("test setup: private key should be unreadable")
	}
	in := keys.Inspect(e, c, a)
	if !in.Exists || in.Mode != 0 || !in.PubExists {
		t.Fatalf("%+v", in)
	}
	if got, err := keys.ReadPub(pub.PrivatePath + ".pub"); err != nil || got != pub.Content {
		t.Fatalf("%q %v", got, err)
	}
	_ = os.Chmod(pub.PrivatePath, 0o600)
}

func TestGenerateFailures(t *testing.T) {
	e := testutil.TempEnv(t)
	c, a := carol()
	f := execx.NewFake().On("ssh-keygen", execx.Response{ExitCode: 1})
	if _, err := keys.Generate(context.Background(), e, f, c, a, false); err == nil || !strings.Contains(err.Error(), "ssh-keygen") {
		t.Fatalf("err=%v", err)
	}
	f = execx.NewFake().On("ssh-keygen", execx.Response{}) // exits 0 but creates nothing
	if _, err := keys.Generate(context.Background(), e, f, c, a, false); !errors.Is(err, keys.ErrKeyMissing) {
		t.Fatalf("err=%v", err)
	}
	if _, err := keys.ReadPub(filepath.Join(e.Home, "none")); err == nil {
		t.Fatal("missing pub")
	}
}

func TestK7Copy(t *testing.T) {
	e := testutil.TempEnv(t)
	f := execx.NewFake().On("pbcopy", execx.Response{})
	if err := keys.Copy(context.Background(), e, f, "ssh-ed25519 AAAA c"); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if len(calls) != 1 || calls[0].Name != "pbcopy" || calls[0].Stdin != "ssh-ed25519 AAAA c" || len(calls[0].Args) != 0 {
		t.Fatalf("%+v", calls)
	}
	e.GOOS = "linux"
	if err := keys.Copy(context.Background(), e, f, "x"); !errors.Is(err, keys.ErrUnsupported) {
		t.Fatal(err)
	}
	e.GOOS = "darwin"
	e.LookPath = func(string) (string, error) { return "", errors.New("no") }
	if err := keys.Copy(context.Background(), e, f, "x"); !errors.Is(err, keys.ErrUnsupported) {
		t.Fatal(err)
	}
}

func TestOpenURL(t *testing.T) {
	e := testutil.TempEnv(t)
	f := execx.NewFake().On("open ", execx.Response{})
	if err := keys.OpenURL(context.Background(), e, f, "https://github.com/settings/ssh/new"); err != nil {
		t.Fatal(err)
	}
	if err := keys.OpenURL(context.Background(), e, f, "file:///etc/passwd"); !errors.Is(err, keys.ErrInvalidAction) {
		t.Fatal(err)
	}
	if err := keys.OpenURL(context.Background(), e, f, "-a Calculator"); !errors.Is(err, keys.ErrInvalidAction) {
		t.Fatal(err)
	}
	e.GOOS = "linux"
	if err := keys.OpenURL(context.Background(), e, f, "https://x"); !errors.Is(err, keys.ErrUnsupported) {
		t.Fatal(err)
	}
	if f.Count("open") != 1 {
		t.Fatalf("%v", f.CallStrings())
	}
}

func TestK8KeyURL(t *testing.T) {
	cases := []struct {
		a    config.Account
		want string
	}{
		{config.Account{Platform: platform.GitHub}, "https://github.com/settings/ssh/new"},
		{config.Account{Platform: platform.GitLab, Host: "git.example.com"}, "https://git.example.com/-/user_settings/ssh_keys"},
		{config.Account{Platform: platform.Bitbucket}, "https://bitbucket.org/account/settings/ssh-keys/"},
		{config.Account{Platform: platform.Gitea, Host: "gitea.example.com"}, "https://gitea.example.com/user/settings/keys"},
		{config.Account{Platform: platform.AzureDevOps}, "https://dev.azure.com/"},
		{config.Account{Platform: platform.AzureDevOps, Orgs: []string{"myorg"}}, "https://dev.azure.com/myorg/_usersSettings/keys"},
	}
	for _, c := range cases {
		if got := keys.KeyURL(c.a); got != c.want {
			t.Errorf("%v: %q want %q", c.a.Platform, got, c.want)
		}
	}
}

func TestK9RemoveFiles(t *testing.T) {
	e := testutil.TempEnv(t)
	keyDir := e.Paths().KeysDir
	testutil.WriteFile(t, filepath.Join(keyDir, "acme-carol"), "priv", 0o600)
	testutil.WriteFile(t, filepath.Join(keyDir, "acme-carol.pub"), "pub", 0o644)
	other := filepath.Join(e.Home, ".ssh/id_ed25519")
	testutil.WriteFile(t, other, "personal", 0o600)
	for _, bad := range []string{"~/.ssh/id_ed25519", other, "~/.ssh/gitid/../id_ed25519", "", "/etc/passwd", "~/.ssh/gitid"} {
		if err := keys.RemoveFiles(e, bad); !errors.Is(err, keys.ErrRefuseDelete) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("personal key deleted")
	}
	if err := keys.RemoveFiles(e, "~/.ssh/gitid/acme-carol"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"acme-carol", "acme-carol.pub"} {
		if _, err := os.Stat(filepath.Join(keyDir, p)); err == nil {
			t.Errorf("%s not removed", p)
		}
	}
	// already gone is fine
	if err := keys.RemoveFiles(e, "~/.ssh/gitid/acme-carol"); err != nil {
		t.Fatal(err)
	}
}

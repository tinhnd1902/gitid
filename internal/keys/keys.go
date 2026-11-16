// Package keys generates and inspects SSH keys for gitid accounts. Private
// key files are never opened: only Lstat is used on them, and only the .pub
// file is ever read.
package keys

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/fsx"
	"github.com/tinhnd1902/gitid/internal/platform"
)

// Errors.
var (
	ErrKeyExists     = errors.New("keys: key file already exists, refusing to overwrite")
	ErrNoKeygen      = errors.New("keys: ssh-keygen not found")
	ErrKeyMissing    = errors.New("keys: key file was not created")
	ErrUnsupported   = errors.New("keys: not supported on this platform")
	ErrRefuseDelete  = errors.New("keys: refusing to delete a key outside ~/.ssh/gitid")
	ErrInvalidAction = errors.New("keys: invalid argument")
)

// PrivatePath is the absolute path of the account's private key.
func PrivatePath(e env.Env, c config.Company, a config.Account) string {
	if a.SSHKey != "" {
		return e.Expand(a.SSHKey)
	}
	return e.Expand(c.DefaultKeyPath(a))
}

// Comment is the key comment: "<email> gitid:<company>/<account>".
func Comment(c config.Company, a config.Account) string {
	return a.Email + " gitid:" + c.Slug + "/" + a.ID
}

// PreCheck fails with ErrKeyExists if the private or public key file (or a
// dangling symlink of that name) is already there. It uses Lstat only.
func PreCheck(path string) error {
	for _, p := range []string{path, path + ".pub"} {
		if _, err := os.Lstat(p); err == nil {
			return fmt.Errorf("%w: %s", ErrKeyExists, p)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// GenArgs builds the ssh-keygen arguments. With noPass false no -N is
// passed, so ssh-keygen prompts for a passphrase itself; the passphrase
// never passes through gitid or argv.
func GenArgs(path, comment string, noPass bool) []string {
	args := []string{"-t", "ed25519", "-a", "100", "-C", comment, "-f", path}
	if noPass {
		args = append(args, "-N", "")
	}
	return args
}

// Cmd is the ssh-keygen command for the account.
func Cmd(e env.Env, c config.Company, a config.Account, noPass bool) (execx.Cmd, error) {
	if strings.HasPrefix(PrivatePath(e, c, a), "-") {
		return execx.Cmd{}, ErrInvalidAction
	}
	if _, err := e.LookPath("ssh-keygen"); err != nil {
		return execx.Cmd{}, ErrNoKeygen
	}
	return execx.Cmd{Name: "ssh-keygen", Args: GenArgs(PrivatePath(e, c, a), Comment(c, a), noPass)}, nil
}

// Prepare creates ~/.ssh/gitid (0700) and runs PreCheck. Call it right
// before running ssh-keygen.
func Prepare(e env.Env, c config.Company, a config.Account) (string, error) {
	path := PrivatePath(e, c, a)
	if err := PreCheck(path); err != nil {
		return path, err
	}
	if err := fsx.MkdirAll(e.Paths().KeysDir, 0o700); err != nil {
		return path, err
	}
	return path, nil
}

// Pub is the result of a successful generation.
type Pub struct {
	PrivatePath string
	PubPath     string
	Content     string // contents of the .pub file (one line)
}

// Finish verifies that both files exist after ssh-keygen ran (Lstat) and
// reads the .pub file.
func Finish(path string) (Pub, error) {
	for _, p := range []string{path, path + ".pub"} {
		if _, err := os.Lstat(p); err != nil {
			return Pub{}, fmt.Errorf("%w: %s", ErrKeyMissing, p)
		}
	}
	content, err := ReadPub(path)
	if err != nil {
		return Pub{}, err
	}
	return Pub{PrivatePath: path, PubPath: path + ".pub", Content: content}, nil
}

// ReadPub returns the public key (trimmed). It only ever opens path+".pub".
func ReadPub(path string) (string, error) {
	if strings.HasSuffix(path, ".pub") {
		path = strings.TrimSuffix(path, ".pub")
	}
	b, err := os.ReadFile(path + ".pub")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// Generate runs the whole flow with the process stdio attached (CLI use):
// pre-check, mkdir, ssh-keygen, verify, read the public key.
func Generate(ctx context.Context, e env.Env, r execx.Runner, c config.Company, a config.Account, noPass bool) (Pub, error) {
	path, err := Prepare(e, c, a)
	if err != nil {
		return Pub{}, err
	}
	cmd, err := Cmd(e, c, a, noPass)
	if err != nil {
		return Pub{}, err
	}
	if err := r.RunTTY(ctx, cmd); err != nil {
		return Pub{}, fmt.Errorf("ssh-keygen: %w", err)
	}
	return Finish(path)
}

// Copy puts the public key on the clipboard with pbcopy (macOS).
func Copy(ctx context.Context, e env.Env, r execx.Runner, pub string) error {
	if e.GOOS != "darwin" {
		return ErrUnsupported
	}
	if _, err := e.LookPath("pbcopy"); err != nil {
		return ErrUnsupported
	}
	_, err := r.Run(ctx, execx.Cmd{Name: "pbcopy", Stdin: pub})
	return err
}

// OpenURL opens an https URL in the default browser with `open` (macOS).
func OpenURL(ctx context.Context, e env.Env, r execx.Runner, url string) error {
	if e.GOOS != "darwin" {
		return ErrUnsupported
	}
	if !strings.HasPrefix(url, "https://") {
		return ErrInvalidAction
	}
	_, err := r.Run(ctx, execx.Cmd{Name: "open", Args: []string{url}})
	return err
}

// KeyURL is the page where the user adds the public key for the account.
func KeyURL(a config.Account) string {
	org := ""
	if a.Platform == platform.AzureDevOps && len(a.Orgs) > 0 {
		org = a.Orgs[0]
	}
	return platform.KeySettingsURL(a.Platform, a.EffectiveHost(), org)
}

// Info describes the key files of an account without reading them.
type Info struct {
	Path       string
	Exists     bool
	Mode       fs.FileMode
	PubExists  bool
	Configured bool // account has ssh_key set
}

// Inspect Lstat's the key files.
func Inspect(e env.Env, c config.Company, a config.Account) Info {
	path := PrivatePath(e, c, a)
	in := Info{Path: path, Configured: a.SSHKey != ""}
	if fi, err := os.Lstat(path); err == nil {
		in.Exists, in.Mode = true, fi.Mode().Perm()
	}
	if _, err := os.Lstat(path + ".pub"); err == nil {
		in.PubExists = true
	}
	return in
}

// RemoveFiles deletes the private and public key of keyPath, but only when
// it is inside ~/.ssh/gitid/. Anything else is refused.
func RemoveFiles(e env.Env, keyPath string) error {
	if !config.KeyPathDeletable(e, keyPath) {
		return fmt.Errorf("%w: %q", ErrRefuseDelete, keyPath)
	}
	abs := e.Expand(keyPath)
	var first error
	for _, p := range []string{abs, abs + ".pub"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) && first == nil {
			first = err
		}
	}
	return first
}

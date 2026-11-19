package online

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/execx"
)

// Writing and removing the gitid Keychain item (service "gitid", account
// "<host>/<username>"), the item Resolver reads as the "keychain" source.
//
// The token must never be in a program's argument list (any user can read it
// with ps). `security add-generic-password -w <token>` would put it there, and
// `-w` as the last option asks for the password on the terminal, which is no
// help for a piped token. So the command runs as `security -i`: it reads its
// commands from stdin, and the token is sent there, written as hexadecimal
// (`-X`), which also means no character of the token needs quoting.

// Keychain errors. None of them holds the token or the output of `security`.
var (
	ErrKeychainUnsupported = errors.New("online: the Keychain is available on macOS only")
	ErrKeychainFailed      = errors.New("online: the security command failed")
	ErrBadToken            = errors.New("online: the token must be non-empty printable ASCII without spaces")
	ErrBadKeychainAccount  = errors.New("online: the host or username cannot be stored in the Keychain")
)

// MaxTokenBytes is the longest token that is stored.
const MaxTokenBytes = 4096

var keychainAccountRe = regexp.MustCompile(`^[A-Za-z0-9._@:+/-]+$`)

// ValidTokenText reports whether s may be stored: 1 to MaxTokenBytes printable
// ASCII characters (0x21-0x7e). Tokens of every platform are of that form; a
// space, a control character, a newline or a NUL is a paste mistake.
func ValidTokenText(s string) bool {
	if s == "" || len(s) > MaxTokenBytes || !utf8.ValidString(s) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// Keychain writes the gitid item with the `security` tool.
type Keychain struct {
	Env env.Env
	Run execx.Runner
}

func (k Keychain) usable() error {
	if k.Env.GOOS != "darwin" {
		return ErrKeychainUnsupported
	}
	if _, err := k.Env.LookPath("security"); err != nil {
		return ErrKeychainUnsupported
	}
	return nil
}

func accountOK(a Account) bool {
	acc := KeychainAccount(a)
	return validValue(a.Host) && validValue(a.Username) && keychainAccountRe.MatchString(acc) && !strings.HasPrefix(acc, "-")
}

// Set stores token as the gitid item of a, replacing an earlier one.
func (k Keychain) Set(ctx context.Context, a Account, token string) error {
	if err := k.usable(); err != nil {
		return err
	}
	if !ValidTokenText(token) {
		return ErrBadToken
	}
	if !accountOK(a) {
		return ErrBadKeychainAccount
	}
	script := "add-generic-password -U -s " + KeychainService + ` -a "` + KeychainAccount(a) + `" -l gitid -X ` + hex.EncodeToString([]byte(token)) + "\n"
	cctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	// Args holds no secret: the script (and with it the token) is on stdin
	_, err := k.Run.Run(cctx, execx.Cmd{Name: "security", Args: []string{"-i"}, Stdin: script})
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		return fmt.Errorf("%w (%s)", ErrKeychainFailed, exitText(err))
	}
	return nil
}

// Remove deletes the gitid item of a. removed is false when there was none.
func (k Keychain) Remove(ctx context.Context, a Account) (removed bool, err error) {
	if err := k.usable(); err != nil {
		return false, err
	}
	if !accountOK(a) {
		return false, ErrBadKeychainAccount
	}
	cctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	_, err = k.Run.Run(cctx, execx.Cmd{Name: "security", Args: []string{"delete-generic-password", "-s", KeychainService, "-a", KeychainAccount(a)}})
	if err == nil {
		return true, nil
	}
	var ee *execx.ExitError
	if errors.As(err, &ee) && ee.Code == exitKeychainNotFound {
		return false, nil
	}
	if cerr := ctx.Err(); cerr != nil {
		return false, cerr
	}
	return false, fmt.Errorf("%w (%s)", ErrKeychainFailed, exitText(err))
}

// exitText names the failure without any output of the command.
func exitText(err error) string {
	var ee *execx.ExitError
	if errors.As(err, &ee) {
		return fmt.Sprintf("exit %d", ee.Code)
	}
	return "not started"
}

// Package gitops wraps the git commands gitid needs. Everything goes through
// execx.Runner so tests never exec a real git (except the integration tests).
package gitops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/gitcfg"
	"github.com/tinhnd1902/gitid/internal/remote"
)

// Errors.
var (
	ErrNotRepo      = errors.New("gitops: not a git repository")
	ErrDubious      = errors.New("gitops: dubious ownership (safe.directory)")
	ErrNotSet       = errors.New("gitops: config key is not set")
	ErrAuth         = errors.New("gitops: authentication failed")
	ErrNotFound     = errors.New("gitops: repository not found")
	ErrTargetExists = errors.New("gitops: target directory already exists")
	ErrBadTarget    = errors.New("gitops: invalid clone target")
	ErrBadURL       = errors.New("gitops: invalid clone URL")
)

// Timeout is the default limit for non-clone git calls.
const Timeout = 10 * time.Second

func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, Timeout)
}

func git(ctx context.Context, r execx.Runner, dir string, args ...string) (execx.Result, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	return r.Run(ctx, execx.Cmd{Name: "git", Args: args})
}

// Version returns the installed git version.
func Version(ctx context.Context, r execx.Runner) (gitcfg.Version, error) {
	res, err := git(ctx, r, "", "version")
	if err != nil {
		return gitcfg.Version{}, err
	}
	return gitcfg.ParseVersion(res.Stdout)
}

// Toplevel returns the repository root containing dir.
func Toplevel(ctx context.Context, r execx.Runner, dir string) (string, error) {
	res, err := git(ctx, r, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		var ee *execx.ExitError
		if errors.As(err, &ee) {
			return "", ErrNotRepo
		}
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// ConfigValue is a config value with the file it came from.
type ConfigValue struct {
	Value  string
	Origin string // "file:/abs/path" style origin as printed by --show-origin
}

// File returns the path part of a "file:<path>" origin ("" otherwise).
func (c ConfigValue) File() string {
	if p, ok := strings.CutPrefix(c.Origin, "file:"); ok {
		return p
	}
	return ""
}

// IsRepoLocal reports whether the value comes from the repository's own
// .git/config (or config.worktree).
func (c ConfigValue) IsRepoLocal() bool {
	f := c.File()
	return strings.HasSuffix(f, ".git/config") || strings.HasSuffix(f, ".git/config.worktree")
}

// ConfigGet returns the effective value of key in dir with its origin.
func ConfigGet(ctx context.Context, r execx.Runner, dir, key string) (ConfigValue, error) {
	res, err := git(ctx, r, dir, "config", "--show-origin", "--get", key)
	if err != nil {
		var ee *execx.ExitError
		if errors.As(err, &ee) {
			if ee.Code == 1 {
				return ConfigValue{}, ErrNotSet
			}
			return ConfigValue{}, ErrNotRepo
		}
		return ConfigValue{}, err
	}
	line := strings.TrimRight(res.Stdout, "\r\n")
	origin, val, ok := strings.Cut(line, "\t")
	if !ok {
		return ConfigValue{Value: strings.TrimSpace(line)}, nil
	}
	return ConfigValue{Value: strings.TrimSpace(val), Origin: origin}, nil
}

// ConfigGetAll returns every value of key across all scopes (system, global).
func ConfigGetAll(ctx context.Context, r execx.Runner, key string) ([]string, error) {
	res, err := git(ctx, r, "", "config", "--get-all", key)
	if err != nil {
		var ee *execx.ExitError
		if errors.As(err, &ee) && ee.Code == 1 {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out, nil
}

// Remote is a named remote URL.
type Remote struct{ Name, URL string }

var remoteURLRe = regexp.MustCompile(`^remote\.(.+)\.url\s+(.*)$`)

// Remotes lists the remotes of the repository at dir.
func Remotes(ctx context.Context, r execx.Runner, dir string) ([]Remote, error) {
	res, err := git(ctx, r, dir, "config", "--get-regexp", `^remote\..*\.url$`)
	if err != nil {
		var ee *execx.ExitError
		if errors.As(err, &ee) {
			if ee.Code == 1 {
				return nil, nil // repo without remotes
			}
			return nil, ErrNotRepo
		}
		return nil, err
	}
	var out []Remote
	for _, l := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		if m := remoteURLRe.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			out = append(out, Remote{Name: m[1], URL: m[2]})
		}
	}
	return out, nil
}

// RepoConfig is what one `git config` call reads from a repository: its
// remotes and the effective user.email.
type RepoConfig struct {
	Remotes  []Remote
	Email    ConfigValue
	HasEmail bool
}

// repoConfigRegexp selects the remote URLs and the email in one call.
const repoConfigRegexp = `^(remote\..*\.url|user\.email)$`

// ReadRepoConfig reads the remotes and the effective user.email of the
// repository at dir with a single git process:
//
//	git -C dir config -z --show-origin --get-regexp ^(remote\..*\.url|user\.email)$
//
// Exit 1 (nothing set) gives an empty RepoConfig. A repository git refuses
// because of its owner gives ErrDubious, any other git failure ErrNotRepo.
// The effective email is the last user.email value (includes and includeIf
// are applied by git, exactly as `git config --get` does).
func ReadRepoConfig(ctx context.Context, r execx.Runner, dir string) (RepoConfig, error) {
	res, err := git(ctx, r, dir, "config", "-z", "--show-origin", "--get-regexp", repoConfigRegexp)
	if err != nil {
		var ee *execx.ExitError
		if errors.As(err, &ee) {
			switch {
			case ee.Code == 1:
				return RepoConfig{}, nil
			case strings.Contains(res.Stderr, "dubious ownership"):
				return RepoConfig{}, ErrDubious
			}
			return RepoConfig{}, ErrNotRepo
		}
		return RepoConfig{}, err
	}
	return parseRepoConfig(res.Stdout), nil
}

// parseRepoConfig parses the -z --show-origin output: NUL-terminated
// "origin", then NUL-terminated "key\nvalue", repeated.
func parseRepoConfig(out string) RepoConfig {
	var rc RepoConfig
	toks := strings.Split(out, "\x00")
	for i := 0; i+1 < len(toks); i += 2 {
		origin, kv := toks[i], toks[i+1]
		key, val, ok := strings.Cut(kv, "\n")
		if !ok {
			continue
		}
		switch {
		case key == "user.email":
			rc.Email, rc.HasEmail = ConfigValue{Value: strings.TrimSpace(val), Origin: origin}, true
		case strings.HasPrefix(key, "remote.") && strings.HasSuffix(key, ".url") && len(key) > len("remote..url"):
			rc.Remotes = append(rc.Remotes, Remote{Name: key[len("remote.") : len(key)-len(".url")], URL: val})
		}
	}
	return rc
}

// SetURL runs `git remote set-url <name> <url>`.
func SetURL(ctx context.Context, r execx.Runner, dir, name, url string) error {
	if name == "" || strings.HasPrefix(name, "-") || strings.HasPrefix(url, "-") {
		return ErrBadURL
	}
	_, err := git(ctx, r, dir, "remote", "set-url", name, url)
	return err
}

// Progress is one parsed clone progress line.
type Progress struct {
	Phase string
	Pct   int
}

var progressRe = regexp.MustCompile(`^(?:remote:\s+)?([A-Za-z][A-Za-z ]*?):\s+(\d+)%`)

// ParseProgress parses `Receiving objects:  45% (450/1000)` style lines.
func ParseProgress(line string) (Progress, bool) {
	m := progressRe.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return Progress{}, false
	}
	pct, _ := strconv.Atoi(m[2])
	return Progress{Phase: m[1], Pct: pct}, true
}

// ValidateURL rejects clone URLs that could be taken for options.
func ValidateURL(url string) error {
	if url == "" || strings.HasPrefix(url, "-") || strings.IndexFunc(url, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return ErrBadURL
	}
	return nil
}

// ValidateDirName checks a single directory name used for a clone target.
func ValidateDirName(name string) error {
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, "-") || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") ||
		strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: %q", ErrBadTarget, name)
	}
	return nil
}

// CheckTarget verifies dir is an absolute path that does not exist yet.
func CheckTarget(dir string) error {
	if !filepath.IsAbs(dir) || strings.HasPrefix(filepath.Base(dir), "-") {
		return fmt.Errorf("%w: %q", ErrBadTarget, dir)
	}
	if _, err := os.Lstat(dir); err == nil {
		return fmt.Errorf("%w: %s", ErrTargetExists, dir)
	}
	return nil
}

// CloneCmd builds `git clone --progress -- <url> <dir>`. In batch mode git
// and ssh are forbidden to prompt (a prompt would corrupt the TUI).
func CloneCmd(url, dir string, batch bool) (execx.Cmd, error) {
	if err := ValidateURL(url); err != nil {
		return execx.Cmd{}, err
	}
	if err := CheckTarget(dir); err != nil {
		return execx.Cmd{}, err
	}
	c := execx.Cmd{Name: "git", Args: []string{"clone", "--progress", "--", url, dir}}
	if batch {
		c.Env = []string{"GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes"}
	}
	return c, nil
}

// CloneError carries the (credential-redacted) stderr of a failed clone.
type CloneError struct {
	Kind   error // ErrAuth, ErrNotFound or nil
	Stderr string
	Err    error
}

func (e *CloneError) Error() string {
	msg := "git clone failed"
	if e.Kind != nil {
		msg = e.Kind.Error()
	}
	if s := strings.TrimSpace(e.Stderr); s != "" {
		msg += ": " + lastLine(s)
	}
	return msg
}

// Unwrap exposes both the classification and the underlying error.
func (e *CloneError) Unwrap() []error {
	var out []error
	if e.Kind != nil {
		out = append(out, e.Kind)
	}
	if e.Err != nil {
		out = append(out, e.Err)
	}
	return out
}

func lastLine(s string) string {
	ls := strings.Split(strings.ReplaceAll(s, "\r", "\n"), "\n")
	for i := len(ls) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(ls[i]); t != "" {
			return t
		}
	}
	return s
}

var authMarkers = []string{
	"Authentication failed", "Permission denied (publickey", "could not read Username", "could not read Password",
	"terminal prompts disabled", "Host key verification failed", "Permission denied, please try again",
}

// Classify maps clone stderr to ErrAuth / ErrNotFound / nil.
func Classify(stderr string) error {
	for _, m := range authMarkers {
		if strings.Contains(stderr, m) {
			return ErrAuth
		}
	}
	if strings.Contains(stderr, "Repository not found") || strings.Contains(stderr, "does not appear to be a git repository") ||
		strings.Contains(stderr, "not found") && strings.Contains(stderr, "fatal: repository") {
		return ErrNotFound
	}
	return nil
}

// Clone runs a batch (non-interactive) clone, reporting progress. It has no
// timeout; cancel ctx to stop it.
func Clone(ctx context.Context, r execx.Runner, url, dir string, onProgress func(Progress)) error {
	cmd, err := CloneCmd(url, dir, true)
	if err != nil {
		return err
	}
	res, err := r.Stream(ctx, cmd, func(line string) {
		if onProgress != nil {
			if p, ok := ParseProgress(line); ok {
				onProgress(p)
			}
		}
	})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &CloneError{Kind: Classify(res.Stderr), Stderr: remote.Redact(res.Stderr), Err: err}
	}
	return nil
}

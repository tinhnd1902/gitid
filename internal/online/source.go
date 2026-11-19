package online

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/platform"
)

// Source says where a token came from.
type Source string

// Sources, in the order the chain asks them.
const (
	SourceEnv      Source = "env"      // GITID_TOKEN_<COMPANY>__<ACCOUNT>
	SourceKeychain Source = "keychain" // the gitid item in the macOS Keychain
	SourceGH       Source = "gh"       // gh auth token
	SourceGlab     Source = "glab"     // glab config get token
	SourceGit      Source = "git"      // git credential fill
	SourceNone     Source = "none"
)

// Notes: why a source was skipped (shown next to "token: none").
const (
	NoteEnvAmbiguous = "env_ambiguous" // two accounts map to the same variable name
	NoteInvalidValue = "invalid_value" // the host or username cannot be passed on safely
)

// Timeout is the limit for each subprocess a source runs.
const Timeout = 10 * time.Second

// KeychainService is the service name of the gitid Keychain item.
const KeychainService = "gitid"

const exitKeychainNotFound = 44 // errSecItemNotFound

// Account is the part of a configured account that the Online listing needs.
type Account struct {
	Ref      string // "company/account"
	Slug, ID string
	Platform platform.Platform
	Host     string // https host with the port, as the account is configured
	Username string
	Email    string
	Orgs     []string
}

// EnvName is the variable that holds the token of the account:
// GITID_TOKEN_<SLUG>__<ID>, upper case, "-" written as "_".
func (a Account) EnvName() string { return EnvName(a.Slug, a.ID) }

// EnvName builds the variable name for a company slug and an account id.
func EnvName(slug, id string) string {
	f := func(s string) string { return strings.ToUpper(strings.ReplaceAll(s, "-", "_")) }
	return "GITID_TOKEN_" + f(slug) + "__" + f(id)
}

// Key identifies the account for the caches. A change of the platform, the
// host or the username gives another key, so a stale token or list is never
// reused for a different identity.
func (a Account) Key() string {
	return a.Ref + "|" + string(a.Platform) + "|" + a.Host + "|" + a.Username
}

// Token is a token and where it came from. Note says why the chain skipped a
// source (an i18n suffix); it is empty for a plain miss.
type Token struct {
	Secret Secret
	Source Source
	Note   string
}

// Found reports whether the chain produced a token.
func (t Token) Found() bool { return t.Source != SourceNone && !t.Secret.IsZero() }

// LooksLikePassword reports whether a token that git's credential helper
// returned for a GitHub account is not in the form of a GitHub token. GitHub
// refuses a password for API calls, so the 401 hint says so. It never shows
// the value.
func (t Token) LooksLikePassword(p platform.Platform) bool {
	if t.Source != SourceGit || p != platform.GitHub {
		return false
	}
	return !t.Secret.IsZero() && !t.Secret.hasAnyPrefix("ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_")
}

// Resolver finds the token of an account: env, gitid keychain, gh or glab,
// then git's credential helper. The first hit wins. A source that fails is
// not an error, the next one is asked; there is no fallback after an API
// call fails.
type Resolver struct {
	Env env.Env
	Run execx.Runner
	// UseGitCredential allows the last source. It is on in gitid.
	UseGitCredential bool
}

// validValue is false for text that could be taken as a flag or could break
// the line protocol of `git credential`.
func validValue(s string) bool {
	return s != "" && !strings.HasPrefix(s, "-") && !strings.ContainsAny(s, "\n\r\x00")
}

// Resolve runs the chain for a. all lists every configured account (for the
// check that two accounts do not share a variable name). The only error is
// the cancellation of ctx.
func (r Resolver) Resolve(ctx context.Context, a Account, all []Account) (Token, error) {
	none := Token{Source: SourceNone}
	if !validValue(a.Host) || !validValue(a.Username) {
		none.Note = NoteInvalidValue
		return none, nil
	}
	// 1. the environment: free, and exact for one account
	name := a.EnvName()
	if v, ok := r.Env.LookupEnv(name); ok && strings.TrimSpace(v) != "" {
		if envCollides(a, all) {
			none.Note = NoteEnvAmbiguous
		} else {
			return Token{Secret: NewSecret(strings.TrimSpace(v)), Source: SourceEnv}, nil
		}
	}
	steps := []struct {
		src Source
		run func(context.Context, Account) string
	}{
		{SourceKeychain, r.keychain},
		{SourceGH, r.gh},
		{SourceGlab, r.glab},
	}
	if r.UseGitCredential {
		steps = append(steps, struct {
			src Source
			run func(context.Context, Account) string
		}{SourceGit, r.gitCredential})
	}
	for _, st := range steps {
		if err := ctx.Err(); err != nil {
			return none, err
		}
		if v := st.run(ctx, a); v != "" {
			return Token{Secret: NewSecret(v), Source: st.src}, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return none, err
	}
	return none, nil
}

// envCollides reports whether another configured account maps to the same
// variable name (slugs and ids may contain "-" and "_" only through "-", so
// "a-b"/"c" and "a"/"b__c" are the rare cases).
func envCollides(a Account, all []Account) bool {
	for _, o := range all {
		if o.Ref != a.Ref && o.EnvName() == a.EnvName() {
			return true
		}
	}
	return false
}

// run executes a source command under its own timeout. It returns stdout and
// whether the command succeeded; stderr is only returned for the "unknown
// flag" check of gh.
func (r Resolver) run(ctx context.Context, c execx.Cmd) (stdout, stderr string, code int, ok bool) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	res, err := r.Run.Run(ctx, c)
	code = res.ExitCode
	var ee *execx.ExitError
	if errors.As(err, &ee) {
		code = ee.Code
	}
	return res.Stdout, res.Stderr, code, err == nil
}

func (r Resolver) have(tool string) bool {
	_, err := r.Env.LookPath(tool)
	return err == nil
}

func clean(out string) string { return strings.TrimSpace(out) }

// keychain reads the gitid item: service "gitid", account "<host>/<username>".
// The token arrives on stdout; exit 44 means there is no item.
func (r Resolver) keychain(ctx context.Context, a Account) string {
	if r.Env.GOOS != "darwin" || !r.have("security") {
		return ""
	}
	out, _, code, ok := r.run(ctx, execx.Cmd{Name: "security", Args: []string{
		"find-generic-password", "-s", KeychainService, "-a", KeychainAccount(a), "-w",
	}})
	if !ok || code == exitKeychainNotFound {
		return ""
	}
	return clean(out)
}

// KeychainAccount is the account field of the gitid Keychain item of a.
func KeychainAccount(a Account) string { return a.Host + "/" + a.Username }

// gh asks `gh auth token` for the login of the account. An older gh does not
// know --user: it is asked again without it (the identity check that follows
// every listing catches a wrong login).
func (r Resolver) gh(ctx context.Context, a Account) string {
	if a.Platform != platform.GitHub || !r.have("gh") {
		return ""
	}
	out, errOut, _, ok := r.run(ctx, execx.Cmd{Name: "gh", Args: []string{
		"auth", "token", "--hostname=" + a.Host, "--user=" + a.Username,
	}})
	if !ok && strings.Contains(errOut, "unknown flag") {
		out, _, _, ok = r.run(ctx, execx.Cmd{Name: "gh", Args: []string{"auth", "token", "--hostname=" + a.Host}})
	}
	if !ok {
		return ""
	}
	return clean(out)
}

// glab asks `glab config get token --host=<host>`: glab keeps one token per
// host, so the identity check decides whether it is the right account.
func (r Resolver) glab(ctx context.Context, a Account) string {
	if a.Platform != platform.GitLab || !r.have("glab") {
		return ""
	}
	out, _, _, ok := r.run(ctx, execx.Cmd{Name: "glab", Args: []string{"config", "get", "token", "--host=" + a.Host}})
	if !ok {
		return ""
	}
	return clean(out)
}

// credentialEnv suppresses every prompt a credential helper could raise.
// GIT_ASKPASS and SSH_ASKPASS run `false`, which fails at once.
var credentialEnv = []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=false", "SSH_ASKPASS=false", "GCM_INTERACTIVE=never"}

// credentialArgs runs `git credential fill` with Git Credential Manager's
// prompts off. git itself defines no credential.interactive key; GCM does,
// and its documentation names both "false" and "never" as the values that
// block every prompt (an error is returned instead). Both are set: git passes
// the last -c value on, so "false", the documented one, is the one in effect.
var credentialArgs = []string{"-c", "credential.interactive=never", "-c", "credential.interactive=false", "credential", "fill"}

// gitCredential asks git's credential helper for the HTTPS credential of the
// account, the one git itself would use to push. Only the password line of
// the answer is kept; nothing else of the output is ever returned or shown.
// approve and reject are never called.
func (r Resolver) gitCredential(ctx context.Context, a Account) string {
	if !r.have("git") {
		return ""
	}
	var in strings.Builder
	in.WriteString("protocol=https\nhost=" + a.Host + "\nusername=" + a.Username + "\n")
	if a.Platform == platform.AzureDevOps && len(a.Orgs) > 0 && validValue(a.Orgs[0]) {
		in.WriteString("path=" + a.Orgs[0] + "\n")
	}
	in.WriteString("\n")
	out, _, _, ok := r.run(ctx, execx.Cmd{
		Name:  "git",
		Args:  credentialArgs,
		Env:   credentialEnv,
		Stdin: in.String(),
	})
	if !ok {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		if v, found := strings.CutPrefix(strings.TrimRight(line, "\r"), "password="); found {
			return v
		}
	}
	return ""
}

// Cache keeps the tokens found in this process, in memory only, so that a
// Keychain or helper dialog is not shown twice. Resolving the same key from
// several goroutines runs the chain once.
type Cache struct {
	mu sync.Mutex
	m  map[string]*cacheEntry
}

type cacheEntry struct {
	mu   sync.Mutex
	done bool
	tok  Token
}

// NewCache returns an empty cache.
func NewCache() *Cache { return &Cache{m: map[string]*cacheEntry{}} }

// Resolve returns the cached token of a or runs the chain and remembers the
// outcome (a miss too). A cancelled resolve is not remembered.
func (c *Cache) Resolve(ctx context.Context, r Resolver, a Account, all []Account) (Token, error) {
	key := a.Key()
	c.mu.Lock()
	e := c.m[key]
	if e == nil {
		e = &cacheEntry{}
		c.m[key] = e
	}
	c.mu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.done {
		return e.tok, nil
	}
	tok, err := r.Resolve(ctx, a, all)
	if err != nil {
		return tok, err
	}
	e.tok, e.done = tok, true
	return tok, nil
}

// Peek returns the cached token without resolving.
func (c *Cache) Peek(key string) (Token, bool) {
	c.mu.Lock()
	e := c.m[key]
	c.mu.Unlock()
	if e == nil {
		return Token{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.tok, e.done
}

// Forget drops the token of one key, so the next Resolve runs the chain
// again.
func (c *Cache) Forget(key string) {
	c.mu.Lock()
	delete(c.m, key)
	c.mu.Unlock()
}

// Keep drops every key that is not in keep.
func (c *Cache) Keep(keep map[string]bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.m {
		if !keep[k] {
			delete(c.m, k)
		}
	}
}

// Package config defines the gitid configuration model (companies and
// accounts), its persistence and its validation.
package config

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/platform"
)

// CurrentVersion is the only supported schema version.
const CurrentVersion = 1

// Auth is the authentication mode of an account.
type Auth string

// Auth modes.
const (
	AuthHTTPS Auth = "https"
	AuthSSH   Auth = "ssh"
)

// Config is the content of config.yaml.
type Config struct {
	Version   int       `yaml:"version"`
	Settings  Settings  `yaml:"settings"`
	Companies []Company `yaml:"companies"`
}

// Settings are global preferences.
type Settings struct {
	Language string `yaml:"language,omitempty"` // vi|en
	CodeRoot string `yaml:"code_root,omitempty"`
}

// Company groups accounts that live under one base directory.
type Company struct {
	Slug           string    `yaml:"slug"`
	Name           string    `yaml:"name"`
	BaseDir        string    `yaml:"base_dir"`
	DefaultAccount string    `yaml:"default_account,omitempty"`
	Accounts       []Account `yaml:"accounts"`
}

// Account is one git identity.
type Account struct {
	ID       string            `yaml:"id"`
	Platform platform.Platform `yaml:"platform"`
	Host     string            `yaml:"host,omitempty"`
	SSHHost  string            `yaml:"ssh_host,omitempty"`
	SSHPort  int               `yaml:"ssh_port,omitempty"`
	Username string            `yaml:"username"`
	Name     string            `yaml:"name"`
	Email    string            `yaml:"email"`
	Auth     Auth              `yaml:"auth"`
	Orgs     []string          `yaml:"orgs,omitempty"`
	SSHKey   string            `yaml:"ssh_key,omitempty"`
}

// Default returns the configuration used when no file exists.
func Default() Config {
	return Config{
		Version:  CurrentVersion,
		Settings: Settings{Language: "vi", CodeRoot: "~/Code"},
	}
}

// EffectiveHost is the https host of the account (platform default if empty).
func (a Account) EffectiveHost() string {
	if a.Host != "" {
		return strings.ToLower(a.Host)
	}
	return platform.DefaultHost(a.Platform)
}

// HostName returns EffectiveHost without a port.
func (a Account) HostName() string {
	h := a.EffectiveHost()
	if i := strings.LastIndexByte(h, ':'); i >= 0 {
		return h[:i]
	}
	return h
}

// EffectiveSSHHost is the host name written to ssh config (no port).
func (a Account) EffectiveSSHHost() string {
	if a.SSHHost != "" {
		return strings.ToLower(a.SSHHost)
	}
	return platform.DefaultSSHHost(a.Platform, a.HostName())
}

// Alias is the ssh Host alias: <platform>-<company>-<account>.
func (c Company) Alias(a Account) string {
	return string(a.Platform) + "-" + c.Slug + "-" + a.ID
}

// KeyBase is the file name of the ssh key: <company>-<account>.
func (c Company) KeyBase(a Account) string { return c.Slug + "-" + a.ID }

// DefaultKeyPath is the ~-relative key path gitid generates for the account.
func (c Company) DefaultKeyPath(a Account) string { return "~/.ssh/gitid/" + c.KeyBase(a) }

// Default returns the default account of the company, if it exists.
func (c Company) Default() (Account, bool) {
	for _, a := range c.Accounts {
		if a.ID == c.DefaultAccount {
			return a, true
		}
	}
	return Account{}, false
}

// FindAccount returns the account with the given id.
func (c Company) FindAccount(id string) (Account, int, bool) {
	for i, a := range c.Accounts {
		if a.ID == id {
			return a, i, true
		}
	}
	return Account{}, -1, false
}

// FindCompany returns the company with the given slug.
func (cfg Config) FindCompany(slug string) (Company, int, bool) {
	for i, c := range cfg.Companies {
		if c.Slug == slug {
			return c, i, true
		}
	}
	return Company{}, -1, false
}

// AccountRef points at one account of one company.
type AccountRef struct {
	Company Company
	Account Account
}

// String is "company/account".
func (r AccountRef) String() string { return r.Company.Slug + "/" + r.Account.ID }

// Alias is the ssh alias of the referenced account.
func (r AccountRef) Alias() string { return r.Company.Alias(r.Account) }

// All lists every account in config order.
func (cfg Config) All() []AccountRef {
	var out []AccountRef
	for _, c := range cfg.Companies {
		for _, a := range c.Accounts {
			out = append(out, AccountRef{Company: c, Account: a})
		}
	}
	return out
}

// Empty reports whether no company has any account.
func (cfg Config) Empty() bool {
	for _, c := range cfg.Companies {
		if len(c.Accounts) > 0 {
			return false
		}
	}
	return true
}

// NormalizeBaseDir converts a base dir into its stored form: "~/..." when
// under HOME, always with one trailing slash. Relative input is returned as
// typed (validation rejects it).
func NormalizeBaseDir(e env.Env, s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if s != "~" && !strings.HasPrefix(s, "~/") && !filepath.IsAbs(s) {
		return s
	}
	abs := e.Expand(s)
	c := e.Contract(abs)
	if c == "/" {
		return c
	}
	return strings.TrimRight(c, "/") + "/"
}

// DefaultBaseDir is <code_root>/<slug>/.
func DefaultBaseDir(e env.Env, codeRoot, slug string) string {
	if codeRoot == "" {
		codeRoot = "~/Code"
	}
	return NormalizeBaseDir(e, strings.TrimRight(codeRoot, "/")+"/"+slug)
}

// maxIDLen is the longest account id (see the id rule in validate.go).
const maxIDLen = 32

// sanitizeID turns a username into a valid id fragment: lowercase, every
// rune outside [a-z0-9-] becomes "-", runs of "-" collapse, leading and
// trailing "-" go, and nothing is left as "account".
func sanitizeID(username string) string {
	var b strings.Builder
	dash := true // swallows leading dashes
	for _, r := range strings.ToLower(username) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	id := strings.TrimRight(b.String(), "-")
	if id == "" {
		return "account"
	}
	return id
}

// cutID shortens id so that id+suffix is at most maxIDLen characters and the
// cut does not leave a trailing dash. id is ASCII (see sanitizeID).
func cutID(id string, suffixLen int) string {
	if n := maxIDLen - suffixLen; len(id) > n {
		id = strings.TrimRight(id[:n], "-")
	}
	return id
}

// DefaultAccountID implements D6: the username made into a valid id
// (lowercase a-z, 0-9 and "-", at most 32 characters), or <id>-<platform>
// (then -2, -3, ...) when the company already has an account with that id.
func DefaultAccountID(c Company, p platform.Platform, username string) string {
	base := sanitizeID(username)
	id := cutID(base, 0)
	if _, _, dup := c.FindAccount(id); !dup {
		return id
	}
	suffix := "-" + string(p)
	for n := 1; ; n++ {
		s := suffix
		if n > 1 {
			s = suffix + "-" + strconv.Itoa(n)
		}
		cand := cutID(base, len(s)) + s
		if _, _, dup := c.FindAccount(cand); !dup {
			return cand
		}
	}
}

// Normalize returns a normalized copy: lowercase hosts, stored form of base
// dirs and key paths, automatic default account when a company has exactly
// one account.
func Normalize(cfg Config, e env.Env) Config {
	out := cfg
	if out.Version == 0 {
		out.Version = CurrentVersion
	}
	out.Companies = make([]Company, len(cfg.Companies))
	for i, c := range cfg.Companies {
		c.BaseDir = NormalizeBaseDir(e, c.BaseDir)
		c.Accounts = append([]Account(nil), c.Accounts...)
		for j, a := range c.Accounts {
			a.Host = strings.ToLower(strings.TrimSpace(a.Host))
			a.SSHHost = strings.ToLower(strings.TrimSpace(a.SSHHost))
			a.Username = strings.TrimSpace(a.Username)
			a.Name = strings.TrimSpace(a.Name)
			a.Email = strings.TrimSpace(a.Email)
			if a.SSHKey != "" {
				a.SSHKey = e.Contract(e.Expand(a.SSHKey))
			}
			c.Accounts[j] = a
		}
		if len(c.Accounts) == 1 {
			c.DefaultAccount = c.Accounts[0].ID
		}
		out.Companies[i] = c
	}
	return out
}

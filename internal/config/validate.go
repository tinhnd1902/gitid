package config

import (
	"fmt"
	"net/mail"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/platform"
)

// Validation error codes. Their values are also the i18n keys of the
// user-facing messages.
const (
	ErrSlugInvalid      = "err.slug.invalid"
	ErrSlugDuplicate    = "err.slug.duplicate"
	ErrIDInvalid        = "err.id.invalid"
	ErrIDDuplicate      = "err.id.duplicate"
	ErrPlatformInvalid  = "err.platform.invalid"
	ErrHostInvalid      = "err.host.invalid"
	ErrHostRequired     = "err.host.required"
	ErrSSHHostInvalid   = "err.sshhost.invalid"
	ErrSSHPortInvalid   = "err.sshport.invalid"
	ErrUsernameInvalid  = "err.username.invalid"
	ErrEmailInvalid     = "err.email.invalid"
	ErrNameInvalid      = "err.name.invalid"
	ErrCompanyNameBad   = "err.company_name.invalid"
	ErrBaseDirInvalid   = "err.basedir.invalid"
	ErrBaseDirReserved  = "err.basedir.reserved"
	ErrBaseDirNested    = "err.basedir.nested"
	ErrDefaultMissing   = "err.default.missing"
	ErrAuthInvalid      = "err.auth.invalid"
	ErrOrgInvalid       = "err.org.invalid"
	ErrAliasDuplicate   = "err.alias.duplicate"
	ErrAliasHostClash   = "err.alias.host_collision"
	ErrLanguageInvalid  = "err.language.invalid"
	ErrNameCollision    = "err.name.collision"
	ErrKeyPathInvalid   = "err.keypath.invalid"
	slugMaxLen          = 32
	nameMaxRunes        = 100
	hostMaxLen          = 253
	labelMaxLen         = 63
	usernameForbidden   = "/:*?[]\\"
	orgForbidden        = "*?[]\\"
	keyPathMustNotBeDot = ".."
)

// AllErrorCodes lists every validation error code (used by the i18n test).
func AllErrorCodes() []string {
	return []string{
		ErrSlugInvalid, ErrSlugDuplicate, ErrIDInvalid, ErrIDDuplicate, ErrPlatformInvalid,
		ErrHostInvalid, ErrHostRequired, ErrSSHHostInvalid, ErrSSHPortInvalid, ErrUsernameInvalid,
		ErrEmailInvalid, ErrNameInvalid, ErrCompanyNameBad, ErrBaseDirInvalid, ErrBaseDirReserved,
		ErrBaseDirNested, ErrDefaultMissing, ErrAuthInvalid, ErrOrgInvalid, ErrAliasDuplicate,
		ErrAliasHostClash, ErrLanguageInvalid, ErrKeyPathInvalid, ErrNameCollision,
	}
}

// FieldError is one validation problem.
type FieldError struct {
	Path   string // e.g. companies[acme].accounts[alice].email
	Field  string // slug, id, host, ...
	Code   string // one of the Err* codes (an i18n key)
	Detail string // developer detail, not user-facing
}

func (f FieldError) Error() string {
	if f.Detail != "" {
		return fmt.Sprintf("%s: %s (%s)", f.Path, f.Code, f.Detail)
	}
	return fmt.Sprintf("%s: %s", f.Path, f.Code)
}

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// Field names accepted by ValidateField.
const (
	FieldSlug        = "slug"
	FieldID          = "id"
	FieldCompanyName = "company_name"
	FieldHost        = "host"
	FieldSSHHost     = "ssh_host"
	FieldSSHPort     = "ssh_port"
	FieldUsername    = "username"
	FieldEmail       = "email"
	FieldName        = "name"
	FieldBaseDir     = "base_dir"
	FieldOrg         = "org"
	FieldPlatform    = "platform"
	FieldAuth        = "auth"
)

// ValidateField validates a single value in isolation (used by forms so the
// TUI and CLI share rules). It returns "" when the value is acceptable or
// the error code otherwise. Cross-record rules (duplicates, nesting) are
// checked by Validate.
func ValidateField(e env.Env, field, value string) string {
	switch field {
	case FieldSlug:
		if !slugRe.MatchString(value) {
			return ErrSlugInvalid
		}
	case FieldID:
		if !slugRe.MatchString(value) {
			return ErrIDInvalid
		}
	case FieldCompanyName:
		if !validText(value, nameMaxRunes) {
			return ErrCompanyNameBad
		}
	case FieldName:
		if !validText(value, nameMaxRunes) {
			return ErrNameInvalid
		}
	case FieldHost:
		if value != "" && !validHostPort(value) {
			return ErrHostInvalid
		}
	case FieldSSHHost:
		if value != "" && !validHostname(strings.ToLower(value)) {
			return ErrSSHHostInvalid
		}
	case FieldSSHPort:
		if value != "" {
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 || n > 65535 {
				return ErrSSHPortInvalid
			}
		}
	case FieldUsername:
		if !validUsername(value) {
			return ErrUsernameInvalid
		}
	case FieldEmail:
		if !validEmail(value) {
			return ErrEmailInvalid
		}
	case FieldBaseDir:
		return validBaseDir(e, value)
	case FieldOrg:
		if !validOrg(value) {
			return ErrOrgInvalid
		}
	case FieldPlatform:
		if !platform.Platform(value).Valid() {
			return ErrPlatformInvalid
		}
	case FieldAuth:
		if value != string(AuthHTTPS) && value != string(AuthSSH) {
			return ErrAuthInvalid
		}
	}
	return ""
}

func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return true
		}
	}
	return false
}

func validText(s string, max int) bool {
	return strings.TrimSpace(s) != "" && !hasControl(s) && utf8.ValidString(s) && utf8.RuneCountInString(s) <= max
}

func validHostname(h string) bool {
	if h == "" || len(h) > hostMaxLen {
		return false
	}
	for _, l := range strings.Split(h, ".") {
		if l == "" || len(l) > labelMaxLen || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, r := range l {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}

func validHostPort(h string) bool {
	h = strings.ToLower(h)
	if i := strings.LastIndexByte(h, ':'); i >= 0 {
		port, err := strconv.Atoi(h[i+1:])
		if err != nil || port < 1 || port > 65535 || h[i+1:] != strconv.Itoa(port) {
			return false
		}
		h = h[:i]
	}
	return validHostname(h)
}

func validUsername(u string) bool {
	if u == "" || hasControl(u) || !utf8.ValidString(u) {
		return false
	}
	for _, r := range u {
		if unicode.IsSpace(r) || strings.ContainsRune(usernameForbidden, r) {
			return false
		}
	}
	return true
}

func validEmail(s string) bool {
	if s == "" || hasControl(s) || strings.TrimSpace(s) != s {
		return false
	}
	a, err := mail.ParseAddress(s)
	return err == nil && a.Name == "" && a.Address == s && !strings.ContainsAny(s, "<>\"")
}

func validOrg(o string) bool {
	if o == "" || hasControl(o) {
		return false
	}
	for _, r := range o {
		if unicode.IsSpace(r) || strings.ContainsRune(orgForbidden, r) {
			return false
		}
	}
	return !strings.HasPrefix(o, "/") && !strings.HasSuffix(o, "/")
}

// validBaseDir returns "" or an error code. Accepts the user-typed form.
func validBaseDir(e env.Env, s string) string {
	s = strings.TrimSpace(s)
	if s == "" || hasControl(s) {
		return ErrBaseDirInvalid
	}
	if s != "~" && !strings.HasPrefix(s, "~/") && !filepath.IsAbs(s) {
		return ErrBaseDirInvalid
	}
	abs := e.Expand(s)
	if abs == "/" || abs == filepath.Clean(e.Home) || abs == filepath.VolumeName(abs)+string(filepath.Separator) {
		return ErrBaseDirReserved
	}
	if strings.ContainsAny(abs, "*?[]\\\"") {
		return ErrBaseDirInvalid
	}
	return ""
}

// ValidateOptions carries context that is not part of the config.
type ValidateOptions struct {
	// ExistingHosts are Host patterns found in the user's own ssh config
	// (outside gitid's files); aliases must not collide with them.
	ExistingHosts []string
}

// Validate checks the whole configuration and returns every problem found,
// in deterministic order. The input should be Normalize()d first.
func Validate(cfg Config, e env.Env, opts ValidateOptions) []FieldError {
	var errs []FieldError
	add := func(path, field, code, detail string) {
		errs = append(errs, FieldError{Path: path, Field: field, Code: code, Detail: detail})
	}
	if cfg.Settings.Language != "" && cfg.Settings.Language != "vi" && cfg.Settings.Language != "en" {
		add("settings", "language", ErrLanguageInvalid, cfg.Settings.Language)
	}
	existing := map[string]bool{}
	for _, h := range opts.ExistingHosts {
		existing[h] = true
	}
	seenSlug := map[string]bool{}
	aliases := map[string]string{}
	fileBases := map[string]string{} // <company>-<account> and <company> -> owner, to keep generated file names unique
	for _, c := range cfg.Companies {
		fileBases[c.Slug] = "company " + c.Slug
	}
	var bases []struct{ slug, dir string }
	for _, c := range cfg.Companies {
		cp := "companies[" + c.Slug + "]"
		if code := ValidateField(e, FieldSlug, c.Slug); code != "" {
			add(cp, FieldSlug, code, c.Slug)
		} else if seenSlug[c.Slug] {
			add(cp, FieldSlug, ErrSlugDuplicate, c.Slug)
		}
		seenSlug[c.Slug] = true
		if code := ValidateField(e, FieldCompanyName, c.Name); code != "" {
			add(cp, "name", code, "")
		}
		if code := validBaseDir(e, c.BaseDir); code != "" {
			add(cp, FieldBaseDir, code, c.BaseDir)
		} else {
			bases = append(bases, struct{ slug, dir string }{c.Slug, NormalizeBaseDir(e, c.BaseDir)})
		}
		if len(c.Accounts) > 0 {
			if _, _, ok := c.FindAccount(c.DefaultAccount); !ok {
				add(cp, "default_account", ErrDefaultMissing, c.DefaultAccount)
			}
		}
		seenID := map[string]bool{}
		for _, a := range c.Accounts {
			ap := cp + ".accounts[" + a.ID + "]"
			if code := ValidateField(e, FieldID, a.ID); code != "" {
				add(ap, FieldID, code, a.ID)
			} else if seenID[a.ID] {
				add(ap, FieldID, ErrIDDuplicate, a.ID)
			}
			seenID[a.ID] = true
			if prev, dup := fileBases[c.KeyBase(a)]; dup {
				add(ap, "id", ErrNameCollision, c.KeyBase(a)+" also used by "+prev)
			} else {
				fileBases[c.KeyBase(a)] = c.Slug + "/" + a.ID
			}
			if !a.Platform.Valid() {
				add(ap, FieldPlatform, ErrPlatformInvalid, string(a.Platform))
			}
			if code := ValidateField(e, FieldHost, a.Host); code != "" {
				add(ap, FieldHost, code, a.Host)
			}
			if a.Platform == platform.Gitea && a.Host == "" {
				add(ap, FieldHost, ErrHostRequired, "")
			}
			if code := ValidateField(e, FieldSSHHost, a.SSHHost); code != "" {
				add(ap, FieldSSHHost, code, a.SSHHost)
			}
			if a.SSHPort < 0 || a.SSHPort > 65535 {
				add(ap, FieldSSHPort, ErrSSHPortInvalid, strconv.Itoa(a.SSHPort))
			}
			if code := ValidateField(e, FieldUsername, a.Username); code != "" {
				add(ap, FieldUsername, code, "")
			}
			if code := ValidateField(e, FieldEmail, a.Email); code != "" {
				add(ap, FieldEmail, code, "")
			}
			if code := ValidateField(e, FieldName, a.Name); code != "" {
				add(ap, FieldName, code, "")
			}
			if code := ValidateField(e, FieldAuth, string(a.Auth)); code != "" {
				add(ap, FieldAuth, code, string(a.Auth))
			}
			for _, o := range a.Orgs {
				if !validOrg(o) {
					add(ap, FieldOrg, ErrOrgInvalid, o)
				}
			}
			if a.SSHKey != "" && !validKeyPath(e, a.SSHKey) {
				add(ap, "ssh_key", ErrKeyPathInvalid, a.SSHKey)
			}
			if a.Auth == AuthSSH {
				al := c.Alias(a)
				if prev, dup := aliases[al]; dup {
					add(ap, "alias", ErrAliasDuplicate, al+" also used by "+prev)
				} else if existing[al] {
					add(ap, "alias", ErrAliasHostClash, al)
				}
				aliases[al] = c.Slug + "/" + a.ID
			}
		}
	}
	for i := range bases {
		for j := range bases {
			if i == j {
				continue
			}
			if env.IsUnder(bases[i].dir, bases[j].dir) && (i > j || bases[i].dir != bases[j].dir) {
				add("companies["+bases[i].slug+"]", FieldBaseDir, ErrBaseDirNested, bases[i].dir+" within "+bases[j].dir)
				break
			}
		}
	}
	return errs
}

// validKeyPath: the stored key path must be an absolute or ~ path without
// traversal. gitid only deletes keys under ~/.ssh/gitid/ (checked at delete
// time), but a hand-edited config must not smuggle control characters in.
func validKeyPath(e env.Env, p string) bool {
	if hasControl(p) || strings.Contains(p, keyPathMustNotBeDot) {
		return false
	}
	return p == "~" || strings.HasPrefix(p, "~/") || filepath.IsAbs(p)
}

// KeyPathDeletable reports whether p is a key gitid may delete: it must be
// under ~/.ssh/gitid/.
func KeyPathDeletable(e env.Env, p string) bool {
	if p == "" || strings.Contains(p, keyPathMustNotBeDot) {
		return false
	}
	return env.IsUnder(e.Expand(p), e.Paths().KeysDir) && e.Expand(p) != e.Paths().KeysDir
}

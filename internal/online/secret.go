// Package online lists the repositories an account can see on its hosting
// platform. It finds a token for the account (an environment variable, the
// gitid Keychain item, the gh or glab login, or git's credential helper),
// checks whose token it is and then makes read-only GET requests to the
// platform's REST API.
//
// A token lives in memory only. It is wrapped in a Secret that prints "***"
// whatever verb or encoder looks at it, it is never put in argv, in the URL
// or in an error, and the only place it is read back is the Authorization
// header of a request to the account's own API host.
package online

import (
	"fmt"
	"strings"
)

// Secret holds a token. Every way of printing it (fmt with any verb, JSON,
// text marshalling) gives "***". The value is behind a pointer on purpose: a
// struct that holds a Secret in an unexported field would otherwise show the
// raw string to fmt's reflection, a pointer shows only an address.
type Secret struct{ p *string }

const masked = "***"

// NewSecret wraps a token.
func NewSecret(v string) Secret { return Secret{p: &v} }

// IsZero reports whether the secret is empty.
func (s Secret) IsZero() bool { return s.p == nil || *s.p == "" }

// reveal returns the token. It is called to build an Authorization header and
// nowhere else (the secrecy test greps for it).
func (s Secret) reveal() string {
	if s.p == nil {
		return ""
	}
	return *s.p
}

// hasAnyPrefix reports whether the token starts with one of the prefixes. It
// answers without handing the token to the caller.
func (s Secret) hasAnyPrefix(prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s.reveal(), p) {
			return true
		}
	}
	return false
}

// String implements fmt.Stringer.
func (Secret) String() string { return masked }

// GoString implements fmt.GoStringer.
func (Secret) GoString() string { return masked }

// Format implements fmt.Formatter: every verb prints the mask.
func (Secret) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(masked)) }

// MarshalJSON implements json.Marshaler.
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + masked + `"`), nil }

// MarshalText implements encoding.TextMarshaler.
func (Secret) MarshalText() ([]byte, error) { return []byte(masked), nil }

// RedactSecret replaces the token in s. An empty token changes nothing.
func RedactSecret(s string, t Secret) string {
	v := t.reveal()
	if v == "" {
		return s
	}
	return strings.ReplaceAll(s, v, masked)
}

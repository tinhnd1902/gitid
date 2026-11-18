package keymap

import (
	"fmt"
	"strings"

	"github.com/tinhnd1902/gitid/internal/i18n"
)

// Issue codes (also i18n keys).
const (
	ErrRead          = "keys.err.read"
	ErrParse         = "keys.err.parse"
	ErrSection       = "keys.err.section"
	ErrUnknownAction = "keys.err.unknown_action"
	ErrBadValue      = "keys.err.bad_value"
	ErrEmpty         = "keys.err.empty"
	ErrInvalidKey    = "keys.err.invalid_key"
	ErrReserved      = "keys.err.reserved"
	ErrConflict      = "keys.err.conflict"
)

// AllIssueCodes lists every issue code (used by the i18n test).
func AllIssueCodes() []string {
	return []string{
		ErrRead, ErrParse, ErrSection, ErrUnknownAction, ErrBadValue,
		ErrEmpty, ErrInvalidKey, ErrReserved, ErrConflict,
	}
}

// Issue is one problem found in keys.yaml. The affected action keeps its
// default binding (or, for file-wide problems, all of them do).
//
// Strings that come from the file (Action of an unknown name, Key, Detail) are
// shortened to a few characters but not escaped here; Message() prints them
// with %q, so a crafted value can not inject terminal escape sequences. Use
// Message() for anything shown on a terminal.
type Issue struct {
	Code    string // one of the Err* codes
	Action  string // action name as written in the file
	Key     string // offending key, "" when not about a key
	Other   string // conflicting action
	Scope   Scope  // scope of a conflict or reserved key
	Default string // the default keys now in use ("" for file-wide issues)
	Detail  string // file-wide issues: the reason (read error, YAML message)
}

// Message returns the translated, terminal-safe text of the issue in the
// active language.
func (i Issue) Message() string {
	q := func(s string) string { return fmt.Sprintf("%q", s) }
	switch i.Code {
	case ErrRead, ErrParse:
		return i18n.T(i.Code, i.Detail)
	case ErrSection:
		return i18n.T(i.Code)
	case ErrUnknownAction:
		return i18n.T(i.Code, q(i.Action))
	case ErrBadValue, ErrEmpty:
		return i18n.T(i.Code, i.Action, i.Default)
	case ErrInvalidKey:
		return i18n.T(i.Code, i.Action, q(i.Key), i.Default)
	case ErrReserved:
		return i18n.T(i.Code, i.Action, q(i.Key), string(i.Scope), i.Default)
	case ErrConflict:
		return i18n.T(i.Code, i.Action, q(i.Key), i.Other, string(i.Scope), i.Default)
	}
	return i.Code
}

// short cuts a string from the file to at most maxEchoRunes runes.
func short(s string) string {
	r := []rune(s)
	if len(r) <= maxEchoRunes {
		return s
	}
	return string(r[:maxEchoRunes-1]) + "…"
}

// plain makes a library message safe to print: control characters become
// "?" and the text is cut to maxDetailRunes runes.
func plain(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n == maxDetailRunes {
			b.WriteString("…")
			break
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			r = '?'
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

// Escape replaces control characters (C0, DEL, C1) with \xNN (or \uNNNN) so
// a string from the file is safe to print or to put in JSON.
func Escape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r >= 0x80 && r <= 0x9f:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SafeAction is Action with control characters escaped.
func (i Issue) SafeAction() string { return Escape(i.Action) }

// SafeKey is Key with control characters escaped.
func (i Issue) SafeKey() string { return Escape(i.Key) }

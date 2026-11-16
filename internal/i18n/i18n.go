// Package i18n holds every user-facing string. Vietnamese (vi) is the
// default language; English (en) is available. Keys are plain strings so
// that other packages can declare their own message keys (for example the
// validation codes in internal/config) and have them checked by the tests
// here.
package i18n

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
)

// Language codes.
const (
	VI = "vi"
	EN = "en"
)

var current atomic.Value // string

func init() { current.Store(VI) }

// SetLang selects the language; anything unknown falls back to Vietnamese.
func SetLang(code string) {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case EN:
		current.Store(EN)
	default:
		current.Store(VI)
	}
}

// Lang returns the active language code.
func Lang() string { return current.Load().(string) }

// T translates key and formats it with args. A key missing in the active
// language falls back to Vietnamese; a key missing everywhere returns the
// key itself (so a gap is visible rather than blank).
func T(key string, args ...any) string {
	return In(Lang(), key, args...)
}

// In translates key into a specific language.
func In(lang, key string, args ...any) string {
	msg, ok := Messages(lang)[key]
	if !ok {
		if msg, ok = vi[key]; !ok {
			return key
		}
	}
	if len(args) == 0 {
		return strings.ReplaceAll(msg, "%%", "%")
	}
	return fmt.Sprintf(msg, args...)
}

// Messages returns the message table of a language (do not modify).
func Messages(lang string) map[string]string {
	if lang == EN {
		return en
	}
	return vi
}

// Keys returns every key defined in the Vietnamese table, sorted.
func Keys() []string {
	out := make([]string, 0, len(vi))
	for k := range vi {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var verbRe = regexp.MustCompile(`%(?:\[\d+\])?[-+# 0-9.*]*[a-zA-Z]`)

// Verbs counts the printf verbs of a format string (%% excluded).
func Verbs(s string) int {
	return len(verbRe.FindAllString(strings.ReplaceAll(s, "%%", ""), -1))
}

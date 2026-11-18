package keymap

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var fKeyRe = regexp.MustCompile(`^f([1-9]|1[0-2])$`)

// ctrlTaken are the ctrl+letter combinations that can not be bound: force
// quit (c), backspace on some terminals (h), tab (i), enter (m) and the fixed
// navigation keys (p n b f).
const ctrlTaken = "chimpnbf"

// ValidKey checks one key string of keys.yaml and returns its normalised
// form (the string bubbletea's KeyMsg.String() gives for that key).
//
// Valid: one printable ASCII character other than space; ctrl+a..ctrl+z
// except the combinations in ctrlTaken; alt+ and one printable ASCII
// character; f1..f12. The ctrl+/alt+/fN prefixes are case-insensitive.
func ValidKey(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	if utf8.RuneCountInString(s) == 1 {
		if r, _ := utf8.DecodeRuneInString(s); printable(r) {
			return s, true
		}
		return "", false
	}
	lower := strings.ToLower(s)
	switch {
	case strings.HasPrefix(lower, "ctrl+"):
		rest := lower[len("ctrl+"):]
		if len(rest) == 1 && rest[0] >= 'a' && rest[0] <= 'z' && !strings.Contains(ctrlTaken, rest) {
			return "ctrl+" + rest, true
		}
	case strings.HasPrefix(lower, "alt+"):
		// the character keeps its case; only the prefix is folded
		rest := s[len("alt+"):]
		if utf8.RuneCountInString(rest) == 1 {
			if r, _ := utf8.DecodeRuneInString(rest); printable(r) {
				return "alt+" + rest, true
			}
		}
	case fKeyRe.MatchString(lower):
		return lower, true
	}
	return "", false
}

func printable(r rune) bool { return r >= 0x21 && r <= 0x7e }

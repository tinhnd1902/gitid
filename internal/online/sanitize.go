package online

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxDetailRunes is the longest text taken from an API answer.
const MaxDetailRunes = 200

// Sanitize makes text from the network safe to draw in a terminal: escape
// sequences (CSI and OSC) are dropped whole, tabs and line breaks become a
// space, every other control and format character (C0, C1, DEL, bidi marks,
// zero-width characters) is dropped, invalid UTF-8 is replaced and the result
// is cut to MaxDetailRunes runes. Letters of any script, Vietnamese included,
// are kept.
func Sanitize(s string) string { return cut(cleanText(s)) }

// safeText is Sanitize for text that came from the platform and may echo the
// token (a login, a repository name, a link): the token is removed before the
// cleaning and the cut, and once more after the cleaning (dropping an escape
// sequence inside a token joins its two halves). The cut comes last, so it
// cannot leave a prefix of the token.
func safeText(s string, t Secret) string {
	return cut(RedactSecret(cleanText(RedactSecret(s, t)), t))
}

// cut keeps the first MaxDetailRunes runes.
func cut(s string) string {
	if utf8.RuneCountInString(s) > MaxDetailRunes {
		return string([]rune(s)[:MaxDetailRunes])
	}
	return s
}

// cleanText is Sanitize without the length cut.
func cleanText(s string) string {
	s = strings.ToValidUTF8(s, "�")
	var b strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == 0x1b || r == 0x9b: // ESC, or the single-character CSI
			i = skipEscape(rs, i)
		case r == '\t' || r == '\n' || r == '\r':
			b.WriteByte(' ')
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// skipEscape returns the index of the last rune of the escape sequence that
// starts at rs[i].
func skipEscape(rs []rune, i int) int {
	if rs[i] == 0x9b {
		return skipCSI(rs, i+1)
	}
	if i+1 >= len(rs) {
		return i
	}
	switch rs[i+1] {
	case '[':
		return skipCSI(rs, i+2)
	case ']', 'P', '_', '^', 'X': // OSC and the string-terminated kinds
		for j := i + 2; j < len(rs); j++ {
			if rs[j] == 0x07 || rs[j] == 0x9c {
				return j
			}
			if rs[j] == 0x1b && j+1 < len(rs) && rs[j+1] == '\\' {
				return j + 1
			}
		}
		return len(rs) - 1
	}
	return i + 1 // ESC plus one character
}

// skipCSI skips parameters and the final byte (0x40-0x7e) of a CSI sequence
// whose body starts at rs[j]; it returns the index of the final byte.
func skipCSI(rs []rune, j int) int {
	for ; j < len(rs); j++ {
		if rs[j] >= 0x40 && rs[j] <= 0x7e {
			return j
		}
	}
	return len(rs) - 1
}

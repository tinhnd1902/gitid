package i18n_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/tinhnd1902/gitid/internal/i18n"
)

// The glossary of the TUI voice. One word per concept, in both languages; the
// checks cover the messages the TUI shows (the "tui." family and the shared
// warnings, errors and doctor messages it reuses).
func shownInTUI(key string) bool {
	for _, p := range []string{"tui.", "scan.", "doctor.", "warn.", "err.", "online."} {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

func TestGlossaryVietnamese(t *testing.T) {
	banned := []struct {
		re   *regexp.Regexp
		why  string
		skip func(msg string) bool
	}{
		{regexp.MustCompile(`\b[Nn]hấn\b`), "the verb is Bấm", nil},
		// read:repository and the like are the platforms' own scope names
		{regexp.MustCompile(`\b[Rr]epository\b`), "a Git working copy is repo", func(msg string) bool { return strings.Contains(msg, "read:repository") }},
		{regexp.MustCompile(`\bfile\b`), "a file is tệp", nil},
		{regexp.MustCompile(`danh tính`), "one Git identity is tài khoản", nil},
		{regexp.MustCompile(`(Hãy nhập|đã nhập|nhập passphrase|nhập mật khẩu)`), "typing is gõ or điền; nhập is for import", nil},
		{regexp.MustCompile(`(chạy lại|tính lại|nạp lại)`), "try again is thử lại, read again is tải lại", nil},
	}
	for key, msg := range i18n.Messages("vi") {
		if !shownInTUI(key) {
			continue
		}
		for _, b := range banned {
			if b.re.MatchString(msg) && (b.skip == nil || !b.skip(msg)) {
				t.Errorf("vi %s: %q (%s)", key, msg, b.why)
			}
		}
		// "chép" alone: copying is sao chép
		if strings.Contains(strings.ReplaceAll(msg, "sao chép", ""), "chép") {
			t.Errorf("vi %s: %q (copying is sao chép)", key, msg)
		}
	}
}

func TestGlossaryEnglish(t *testing.T) {
	banned := []struct {
		re  *regexp.Regexp
		why string
	}{
		{regexp.MustCompile(`\(s\)`), "plurals use the _one and _many forms"},
		{regexp.MustCompile(`[Dd]irector(y|ies)`), "a folder is a folder (safe.directory is git's own word)"},
		{regexp.MustCompile(`[Rr]epositor(y|ies)`), "a Git working copy is a repo"},
		{regexp.MustCompile(`\b(profile|Profile)\b`), "one Git identity is an account"},
	}
	for key, msg := range i18n.Messages("en") {
		if !shownInTUI(key) {
			continue
		}
		msg = strings.ReplaceAll(msg, "safe.directory", "")
		for _, b := range banned {
			if b.re.MatchString(msg) {
				t.Errorf("en %s: %q (%s)", key, msg, b.why)
			}
		}
	}
}

// A message that needs a count has both forms in both languages, and the
// singular carries the count too (it is passed as the first argument).
func TestPluralFormsComeInPairs(t *testing.T) {
	for _, lang := range []string{"vi", "en"} {
		msgs := i18n.Messages(lang)
		for key := range msgs {
			if base, ok := strings.CutSuffix(key, "_one"); ok {
				if _, many := msgs[base+"_many"]; !many {
					t.Errorf("%s: %s has no _many form", lang, key)
				}
			}
			if base, ok := strings.CutSuffix(key, "_many"); ok && i18n.Verbs(msgs[key]) > 0 {
				if _, one := msgs[base+"_one"]; !one {
					t.Errorf("%s: %s has no _one form", lang, key)
				}
			}
		}
	}
}

// Words say what an arrow used to say ("nên ở acme", "Cách sửa"), and a
// count is written "2 bản clone", not "×2". The size "40×10" is a size.
func TestNoArrowsOrTimesInCopy(t *testing.T) {
	for _, lang := range []string{"vi", "en"} {
		for key, msg := range i18n.Messages(lang) {
			if !strings.HasPrefix(key, "tui.") || key == "tui.too_small" {
				continue
			}
			if strings.ContainsAny(msg, "→×▪◂") {
				t.Errorf("%s %s: %q", lang, key, msg)
			}
		}
	}
}

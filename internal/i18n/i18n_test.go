package i18n_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/doctor"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/importer"
	"github.com/tinhnd1902/gitid/internal/plan"
	"github.com/tinhnd1902/gitid/internal/sshcfg"
)

func TestL1EveryKeyHasBothLanguages(t *testing.T) {
	vi, en := i18n.Messages("vi"), i18n.Messages("en")
	for k, v := range vi {
		if strings.TrimSpace(v) == "" {
			t.Errorf("vi %q is empty", k)
		}
		if e, ok := en[k]; !ok || strings.TrimSpace(e) == "" {
			t.Errorf("en %q missing or empty", k)
		}
	}
	for k := range en {
		if _, ok := vi[k]; !ok {
			t.Errorf("en has extra key %q", k)
		}
	}
	if len(i18n.Keys()) != len(vi) {
		t.Fatal("Keys()")
	}
}

func TestL2VerbCountsMatch(t *testing.T) {
	vi, en := i18n.Messages("vi"), i18n.Messages("en")
	for k, v := range vi {
		if i18n.Verbs(v) != i18n.Verbs(en[k]) {
			t.Errorf("%q: vi has %d verbs, en has %d", k, i18n.Verbs(v), i18n.Verbs(en[k]))
		}
	}
	if i18n.Verbs("a %s b %d %% c %[2]s %5.2f") != 4 {
		t.Fatal("Verbs")
	}
}

func TestL3FallbackAndFormatting(t *testing.T) {
	defer i18n.SetLang("vi")
	i18n.SetLang("fr")
	if i18n.Lang() != "vi" {
		t.Fatal("unknown language must fall back to vi")
	}
	i18n.SetLang(" EN ")
	if i18n.Lang() != "en" || i18n.T("cli.apply.written", "x") != "Written: x" {
		t.Fatal(i18n.T("cli.apply.written", "x"))
	}
	if i18n.In("vi", "cli.apply.written", "x") != "Đã ghi: x" {
		t.Fatal("In")
	}
	if i18n.T("no.such.key") != "no.such.key" {
		t.Fatal("missing keys return the key")
	}
	if i18n.T("cli.error.prefix", "boom") != "Error: boom" {
		t.Fatal("format")
	}
	if !strings.Contains(i18n.T("doctor.hint.install_gcm"), "brew install --cask git-credential-manager") {
		t.Fatal("GCM hint")
	}
}

// Keys declared by other packages must be translated.
func TestExternalKeysAreTranslated(t *testing.T) {
	keys := map[string]bool{}
	for _, k := range config.AllErrorCodes() {
		keys[k] = true
	}
	for _, k := range doctor.AllKeys() {
		keys[k] = true
	}
	for _, k := range app.AllWarnings() {
		keys[k] = true
	}
	for _, k := range importer.AllWarnings() {
		keys[k] = true
	}
	keys[sshcfg.WarnIncludeScoped] = true
	keys[plan.WarnGitidConfForeign] = true
	for _, r := range []string{"alias", "username", "default"} {
		keys["cli.reason."+r] = true
	}
	for _, s := range []string{"ok", "warn", "fail", "skip", "info"} {
		keys["cli.status."+s] = true
	}
	vi := i18n.Messages("vi")
	for k := range keys {
		if _, ok := vi[k]; !ok {
			t.Errorf("key %q is used by code but not translated", k)
		}
	}
}

var tCallRe = regexp.MustCompile(`i18n\.T\("([^"]+)"`)

// Every i18n.T("literal") in the source tree must exist in the tables.
func TestKeysUsedInSourceExist(t *testing.T) {
	vi := i18n.Messages("vi")
	root := filepath.Join("..", "..")
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		b, _ := os.ReadFile(p)
		for _, m := range tCallRe.FindAllStringSubmatch(string(b), -1) {
			if strings.HasSuffix(m[1], ".") {
				continue // prefix of a computed key, covered by TestExternalKeysAreTranslated
			}
			if _, ok := vi[m[1]]; !ok {
				t.Errorf("%s: i18n.T(%q) has no translation", p, m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

var literalRe = regexp.MustCompile(`"[A-Za-zÀ-ỹ]+( [A-Za-zÀ-ỹ%()\[\]<>/.,:;-]+){1,}"`)

// L4 (heuristic): no user-facing prose literals in internal/cli and, later,
// internal/tui. Lines with i18n.T, errors.New, Use:, tags or comments are exempt.
func TestL4NoHardcodedProse(t *testing.T) {
	for _, dir := range []string{"cli", "tui"} {
		base := filepath.Join("..", dir)
		_ = filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			b, _ := os.ReadFile(p)
			for n, line := range strings.Split(string(b), "\n") {
				l := strings.TrimSpace(line)
				if strings.HasPrefix(l, "//") || strings.Contains(l, "i18n.T(") || strings.Contains(l, "errors.New(") ||
					strings.Contains(l, "Use:") || strings.Contains(l, "json:\"") || strings.Contains(l, "i18n.In(") {
					continue
				}
				if m := literalRe.FindString(l); m != "" && len(m) >= 14 {
					t.Errorf("%s:%d hard-coded prose %s", p, n+1, m)
				}
			}
			return nil
		})
	}
}

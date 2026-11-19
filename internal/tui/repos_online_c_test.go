package tui

import (
	"strings"
	"testing"

	"github.com/tinhnd1902/gitid/internal/i18n"
)

// The warnings Phase C adds name the organization, workspace or nothing, in
// both languages, and are never a bare key.
func TestWarnTextPhaseC(t *testing.T) {
	defer i18n.SetLang("vi")
	for _, lang := range []string{"vi", "en"} {
		i18n.SetLang(lang)
		for w, name := range map[string]string{
			"org_failed:acme": "acme", "ws_failed:team": "team", "orgs_failed": "", "no_orgs": "",
			"no_repo_scope": "", "sso_partial": "",
		} {
			got := warnText(w)
			if strings.Contains(got, "tui.repos.online.warn") || strings.Contains(got, "%!") || !strings.Contains(got, name) {
				t.Errorf("%s %q: %q", lang, w, got)
			}
		}
	}
}

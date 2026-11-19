package tui

import (
	"strings"
	"testing"

	"github.com/tinhnd1902/gitid/internal/i18n"
)

// onOneLine reports whether some line of the rendered view holds token whole.
func onOneLine(view, token string) bool {
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(plain(l), token) {
			return true
		}
	}
	return false
}

func (h *harness) mustHaveOnOneLine(label string, tokens ...string) {
	h.t.Helper()
	v := h.view()
	for _, tok := range tokens {
		if !onOneLine(v, tok) {
			h.t.Errorf("%s: %q is not whole on one line:\n%s", label, tok, v)
		}
	}
}

func TestWrapUnitsKeepsWordsWhole(t *testing.T) {
	got := wrapUnits("như trong URL hồ sơ: github.com/<login>. Không phải email", 30)
	for _, tok := range []string{"github.com/<login>."} {
		found := false
		for _, l := range got {
			if strings.Contains(l, tok) {
				found = true
			}
		}
		if !found {
			t.Errorf("%q split in %q", tok, got)
		}
	}
	for _, l := range got {
		if len([]rune(l)) > 30 {
			t.Errorf("line too wide: %q", l)
		}
	}
	// a URL wider than the width breaks after "/" only
	long := wrapUnits("https://tinhnd1902@github.com/<owner>/<repo>.git", 28)
	if strings.Join(long, "") != "https://tinhnd1902@github.com/<owner>/<repo>.git" {
		t.Errorf("text lost: %q", long)
	}
	for _, l := range long {
		if !strings.HasSuffix(l, "/") && l != long[len(long)-1] {
			t.Errorf("broke inside a segment: %q", long)
		}
	}
	if !strings.Contains(strings.Join(long, "\n"), "<owner>/") {
		t.Errorf("<owner> split: %q", long)
	}
	// a segment wider than the width is cut hard but loses nothing
	if got := strings.Join(wrapUnits(strings.Repeat("a", 25), 10), ""); got != strings.Repeat("a", 25) {
		t.Errorf("hard cut lost text: %q", got)
	}
}

func TestTruncWordsCutsAtAWord(t *testing.T) {
	for _, c := range []struct {
		in   string
		w    int
		want string
	}{
		{"xem trước & áp dụng", 19, "xem trước & áp dụng"},
		{"xem trước & áp dụng", 18, "xem trước & áp…"},
		{"nhập từ git sẵn có", 14, "nhập từ git…"},
		{"abcdefghij", 5, "abcd…"},
	} {
		if got := truncWords(c.in, c.w); got != c.want {
			t.Errorf("truncWords(%q, %d) = %q, want %q", c.in, c.w, got, c.want)
		}
	}
}

// D1: the username hint keeps github.com/<login> whole.
func TestFix2UsernameHintKeepsTheURLWhole(t *testing.T) {
	for _, lang := range []string{"vi", "en"} {
		i18n.SetLang(lang)
		for _, s := range [][2]int{{80, 24}, {72, 24}, {60, 20}, {100, 30}} {
			h := newHarness(t, emptyCompanyCfg("personal"))
			h.resize(s[0], s[1])
			h.key("a")
			h.key("enter") // focus: username
			h.mustHaveOnOneLine(lang, "github.com/<login>")
			assertFits(t, h.view(), s[0], s[1], "username")
		}
	}
	i18n.SetLang("vi")
}

// D2: the email hint is shown whole, never cut (it is one line now: the
// noreply address is named, not spelled out).
func TestFix2EmailHintKeepsTheAddressWhole(t *testing.T) {
	for _, lang := range []string{"vi", "en"} {
		i18n.SetLang(lang)
		for _, s := range [][2]int{{80, 24}, {72, 24}, {60, 20}, {100, 30}} {
			h := newHarness(t, emptyCompanyCfg("personal"))
			h.resize(s[0], s[1])
			h.key("a")
			h.key("enter")
			h.typeText("alice")
			h.key("enter")
			h.typeText("Alice Tran")
			h.key("enter") // focus: email
			h.mustContainSpaceless(tr("tui.form.email_hint"))
			h.mustHaveOnOneLine(lang, "noreply")
			assertFits(t, h.view(), s[0], s[1], "email")
		}
	}
	i18n.SetLang("vi")
}

// D3: the remote preview is never cut; it wraps to the next line instead.
func TestFix2RemotePreviewIsNotCut(t *testing.T) {
	for _, s := range [][2]int{{60, 20}, {72, 24}, {80, 24}, {100, 30}} {
		h := newHarness(t, emptyCompanyCfg("personal"))
		h.resize(s[0], s[1])
		h.key("a")
		h.key("enter")
		h.typeText("tinhnd1902")
		v := h.view()
		if strings.Contains(v, "…") {
			t.Errorf("%v: the preview is cut:\n%s", s, v)
		}
		h.mustHaveOnOneLine("preview", "https://tinhnd1902@github.com/<owner>/<repo>.git")
		assertFits(t, v, s[0], s[1], "account form")
		// the form keeps room for the focused field at the wrapped height
		f := h.top().(*accountForm)
		if inner := f.formRows(); inner < 4 {
			t.Errorf("%v: only %d rows left for the form", s, inner)
		}
	}
	// typing a longer name wraps the remote while the form is open
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.resize(60, 20)
	h.key("a")
	h.key("enter")
	h.typeText("a")
	h.typeText(strings.Repeat("b", 12))
	h.mustHaveOnOneLine("typed", "https://a"+strings.Repeat("b", 12)+"@github.com/<owner>/<repo>.git")
	assertFits(t, h.view(), 60, 20, "typed")
}

// D4: the side-pane remote breaks at "/", never inside <owner>.
func TestFix2SidePaneRemoteKeepsPlaceholdersWhole(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	h.resize(100, 30)
	h.selectKey("personal/tinhnd")
	h.mustHaveOnOneLine("detail 100x30", "<owner>/<repo>.git", "https://tinhnd@github.com/")
	assertFits(t, h.view(), 100, 30, "detail")
}

// D5: the help overlay never cuts a description in the middle of a word.
func TestFix2HelpOverlayCutsAtWords(t *testing.T) {
	for _, s := range [][2]int{{60, 20}, {72, 24}, {80, 24}, {100, 30}} {
		h := newHarness(t, oneAccCfg())
		h.resize(s[0], s[1])
		h.key("?")
		lines := strings.Split(h.view(), "\n")
		for _, b := range h.top().Help() {
			if !b.Enabled() {
				continue
			}
			hp := b.Help()
			var line string
			for _, l := range lines {
				if p := plain(l); strings.HasPrefix(p, hp.Key+" ") || strings.Contains(p, " "+hp.Key+" ") {
					if strings.Contains(p, hp.Key+" "+strings.Fields(hp.Desc)[0]) {
						line = p
					}
				}
			}
			if line == "" {
				t.Errorf("%v: %q missing", s, hp.Key)
				continue
			}
			if strings.Contains(line, hp.Desc) {
				continue
			}
			words := strings.Fields(hp.Desc)
			ok := false
			for n := len(words) - 1; n >= 1; n-- {
				if strings.Contains(line, strings.Join(words[:n], " ")+"…") {
					ok = true
				}
			}
			if !ok {
				t.Errorf("%v: %q is cut inside a word:\n%s", s, hp.Desc, h.view())
			}
		}
		assertFits(t, h.view(), s[0], s[1], "help")
	}
	// at 72x24 and wider every description is whole
	h := newHarness(t, oneAccCfg())
	h.resize(72, 24)
	h.key("?")
	for _, b := range h.top().Help() {
		if b.Enabled() && !strings.Contains(plain(h.view()), b.Help().Desc) {
			t.Errorf("72x24: %q is cut", b.Help().Desc)
		}
	}
}

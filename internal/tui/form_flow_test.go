package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/testutil"
)

func (h *harness) shiftTab() {
	h.t.Helper()
	h.send(tea.KeyMsg{Type: tea.KeyShiftTab})
}

func (h *harness) tab() {
	h.t.Helper()
	h.send(tea.KeyMsg{Type: tea.KeyTab})
}

// focusedTitle is the title of the focused field of the account form.
func focusedTitle(t *testing.T, h *harness) string {
	t.Helper()
	f, ok := h.top().(*accountForm)
	if !ok {
		t.Fatalf("top is %T", h.top())
	}
	return f.form.current().title
}

func TestH1ShiftTabGoesBackAndKeepsTheValues(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("a")
	h.key("enter")
	h.typeText("dora")
	h.key("enter")
	h.key("enter")
	h.typeText("dora@acme.com")
	h.key("enter") // auth
	if got := focusedTitle(t, h); got != tr("tui.f.auth") {
		t.Fatalf("focus on %q", got)
	}
	h.shiftTab()
	h.shiftTab()
	h.shiftTab()
	if got := focusedTitle(t, h); got != tr("tui.f.username") {
		t.Fatalf("focus on %q", got)
	}
	if f := h.top().(*accountForm); f.username != "dora" || f.email != "dora@acme.com" {
		t.Fatalf("values lost: %q %q", f.username, f.email)
	}
	h.mustContainPlain("> dora", tr("tui.form.preview_remote", "https://dora@github.com/<owner>/<repo>.git"))
}

func TestH2AfterGoingBackTheFlowStillSaves(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("a")
	h.key("enter")
	h.typeText("dora")
	h.key("enter")
	h.key("enter")
	h.typeText("dora@acme.com")
	h.key("enter")
	h.shiftTab()
	h.shiftTab()
	h.shiftTab()
	for i := 0; i < 12; i++ { // username, name, email, auth, advanced, default
		h.key("enter")
		if _, ok := h.top().(*confirmScreen); ok {
			break
		}
	}
	h.waitConfirm()
	if ref, err := h.m.sh.cfg.ParseRef("acme/dora"); err != nil || ref.Account.Email != "dora@acme.com" {
		t.Fatalf("saved account: %+v %v", ref, err)
	}
}

func TestH3LongFormKeepsTheFocusedFieldVisible(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.resize(72, 24)
	h.key("a")
	check := func(step string) {
		t.Helper()
		f, ok := h.top().(*accountForm)
		if !ok {
			t.Fatalf("%s: top is %T", step, h.top())
		}
		title := f.form.current().title
		v := h.view()
		if !strings.Contains(plain(v), plain(title)) {
			t.Fatalf("%s: focused field %q not visible:\n%s", step, title, v)
		}
		assertFits(t, v, 72, 24, step)
		if f.form.Overflows() {
			n, m := f.form.Position()
			if !strings.Contains(v, fmt.Sprintf("%d/%d", n, m)) {
				t.Fatalf("%s: no %d/%d in the title:\n%s", step, n, m, v)
			}
		}
	}
	check("start")
	h.key("enter")
	check("platform")
	h.typeText("erin")
	check("username")
	h.key("enter")
	check("name")
	h.key("enter")
	h.typeText("erin@acme.com")
	check("email")
	h.key("enter")
	check("auth")
	h.send(tea.KeyMsg{Type: tea.KeyDown})
	check("ssh")
	h.key("enter")
	check("advanced")
	h.key("y")
	for _, step := range []string{"host", "orgs", "ssh host", "ssh port", "make default", "gen key"} {
		check(step)
		if step == "gen key" {
			break
		}
		h.key("enter")
	}
	if got := focusedTitle(t, h); got != tr("tui.form.gen_key") {
		t.Fatalf("last field %q", got)
	}
	h.key("enter") // gen key: no -> submit
	h.waitConfirm()
}

func TestH4DuplicateLoginShowsUnderTheInputOnce(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	h.key("a")
	h.key("enter")
	h.typeText("tinhnd")
	h.key("enter")
	lines := h.lines()
	in, er := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "> tinhnd") {
			in = i
		}
		if strings.Contains(l, "✗") {
			er = i
		}
	}
	if in < 0 || er <= in {
		t.Fatalf("the error must be under the input (input row %d, error row %d):\n%s", in, er, h.view())
	}
	msg := plain(defKM.T("tui.form.err.dup_account", "tinhnd", "github.com"))
	if n := strings.Count(plain(h.view()), msg); n != 1 {
		t.Fatalf("the message is shown %d times:\n%s", n, h.view())
	}
}

func TestH5CustomizedAccountShowsHostFirstUnderTheAdvancedHeader(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.selectKey("globex/dave")
	h.key("e")
	for i := 0; i < 6; i++ {
		h.key("enter")
	}
	h.mustContainPlain(tr("tui.f.host"), "git.example.com", tr("tui.f.ssh_port"), "2222")
	f := h.top().(*accountForm)
	rows := strings.Split(ansi.Strip(f.form.View()), "\n")
	header := rowOf(rows, tr("tui.form.group_advanced"))
	host := rowOf(rows, tr("tui.f.host"))
	if header < 0 || host != header+1 {
		t.Fatalf("the Host field must follow the advanced header (header %d, host %d):\n%s", header, host, f.form.View())
	}
}

func TestH6FocusLeavesAFieldThatBecameHidden(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("a")
	h.key("enter")
	h.typeText("erin")
	h.key("enter")
	h.key("enter")
	h.typeText("erin@acme.com")
	h.key("enter")
	h.key("enter") // auth
	h.key("enter") // advanced: no -> make default
	if got := focusedTitle(t, h); got != tr("tui.f.make_default") {
		t.Fatalf("focus on %q", got)
	}
	other := cloneConfig(h.m.sh.cfg)
	for i := range other.Companies {
		if other.Companies[i].Slug == "acme" {
			other.Companies[i].Accounts = nil
			other.Companies[i].DefaultAccount = ""
		}
	}
	h.send(cfgChangedMsg{cfg: other})
	if got := focusedTitle(t, h); got != tr("tui.form.show_advanced") {
		t.Fatalf("after the other accounts went away focus is on %q", got)
	}
	assertFits(t, h.view(), 100, 30, "after the change")
	h.update(keyMsg("enter")) // the last visible field: submits (no drain: the save is in flight)
	if f, ok := h.top().(*accountForm); !ok || !f.saving {
		t.Fatalf("enter on the last visible field did not submit: %T", h.top())
	}
	h.drain()
}

func importerHarness(t *testing.T) *harness {
	t.Helper()
	e := testutil.TempEnv(t)
	testutil.WriteFile(t, e.Paths().GitConfig, "[user]\n\tname = x\n[includeIf \"gitdir:~/Code/work/initech-solutions/\"]\n\tpath = ~/.gitconfig-initech\n", 0o644)
	testutil.WriteFile(t, filepath.Join(e.Home, ".gitconfig-initech"), "[user]\n\tname = Dana Initech\n\temail = dana@initech.example\n", 0o644)
	h := newHarness(t, nil)
	h.m.sh.svc.Env = e
	h.key("i")
	h.waitFor("proposals", func() bool { return h.top().(*importScreen).phase == imList })
	return h
}

func TestH7ImporterShowsTheErrorInlineAndEscGoesBack(t *testing.T) {
	h := importerHarness(t)
	h.key("enter")
	h.key("enter") // platform
	h.key("enter") // empty username
	if s := h.top().(*importScreen); s.phase != imForm {
		t.Fatalf("phase %v", s.phase)
	}
	h.mustContain("✗ " + tr("tui.form.err.required", tr("tui.f.username")))
	h.key("esc")
	if s := h.top().(*importScreen); s.phase != imList {
		t.Fatalf("phase %v after esc", s.phase)
	}
	h.mustContain("initech-solutions", "dana@initech.example")
}

func TestH8VietnameseNameIsSavedExactly(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("A")
	h.typeText("vn")
	h.key("enter")
	h.typeText("Công ty Việt Nam")
	h.key("enter")
	h.key("enter") // base directory: default -> submit
	h.waitFor("saved", func() bool { _, _, ok := h.m.sh.cfg.FindCompany("vn"); return ok })
	c, _, _ := h.m.sh.cfg.FindCompany("vn")
	if c.Name != "Công ty Việt Nam" {
		t.Fatalf("name %q", c.Name)
	}
}

func TestH9TabOnTheLastFieldSubmits(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.key("a")
	h.key("enter")
	h.typeText("kim")
	h.key("enter")
	h.typeText("Kim Tran")
	h.key("enter")
	h.typeText("kim@example.com")
	h.key("enter")
	h.key("enter") // auth -> advanced, the last visible field
	if got := focusedTitle(t, h); got != tr("tui.form.show_advanced") {
		t.Fatalf("focus on %q", got)
	}
	h.tab()
	h.waitConfirm()
}

func TestH10EscLeavesTheDiskAlone(t *testing.T) {
	h := newHarness(t, basicCfg())
	before := testutil.Snapshot(t, h.e.Home)
	h.key("a")
	h.key("enter")
	h.typeText("zed")
	h.key("enter")
	h.typeText("Zed")
	h.key("esc")
	h.key("y") // discard what was typed
	if h.depth() != 1 {
		t.Fatalf("depth %d", h.depth())
	}
	testutil.AssertSame(t, before, testutil.Snapshot(t, h.e.Home))
}

func TestH11KeysAreIgnoredWhileSaving(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.key("a")
	h.key("enter")
	h.typeText("kim")
	h.key("enter")
	h.typeText("Kim Tran")
	h.key("enter")
	h.typeText("kim@example.com")
	h.key("enter")
	h.key("enter")
	h.update(keyMsg("enter")) // submit; no drain: the save is in flight
	f := h.top().(*accountForm)
	if !f.saving {
		t.Fatal("not saving")
	}
	focus := f.form.focus
	h.update(keyMsg("x"))
	h.update(keyMsg("enter"))
	h.update(keyMsg("shift+tab"))
	if f.username != "kim" || f.email != "kim@example.com" || f.form.focus != focus {
		t.Fatalf("a key got through: %q %q focus %d (was %d)", f.username, f.email, f.form.focus, focus)
	}
	h.waitConfirm()
}

// walk presses keys one by one and checks, before the first and after each,
// that the view fits and a control row of the focused field is shown.
func walk(t *testing.T, h *harness, w, hh int, label string, keys ...string) {
	t.Helper()
	check := func(step string) {
		t.Helper()
		v := h.view()
		assertFits(t, v, w, hh, label+" "+step)
		ok := false
		for _, l := range strings.Split(v, "\n") {
			if i := strings.Index(l, "┃"); i >= 0 {
				rest := l[i+len("┃"):]
				if strings.Contains(rest, "> ") || strings.Contains(rest, "●") || strings.Contains(rest, "○") {
					ok = true
				}
			}
		}
		if !ok {
			t.Fatalf("%s %s: no control row of the focused field:\n%s", label, step, v)
		}
	}
	check("start")
	for _, k := range keys {
		switch {
		case k == "down":
			h.send(tea.KeyMsg{Type: tea.KeyDown})
		case strings.HasPrefix(k, "type:"):
			h.typeText(strings.TrimPrefix(k, "type:"))
		default:
			h.key(k)
		}
		check("after " + k)
	}
}

func TestH12SmallTerminalsKeepTheFocusedControlVisible(t *testing.T) {
	for _, s := range [][2]int{{40, 10}, {41, 11}} {
		w, hh := s[0], s[1]
		h := newHarness(t, basicCfg())
		h.resize(w, hh)
		h.key("A")
		walk(t, h, w, hh, "company new", "type:zz", "enter", "type:ZZ", "enter")

		h = newHarness(t, basicCfg())
		h.resize(w, hh)
		h.key("e") // the first row is the company acme
		if _, ok := h.top().(*companyForm); !ok {
			t.Fatalf("top is %T", h.top())
		}
		walk(t, h, w, hh, "company edit", "enter")

		h = newHarness(t, basicCfg())
		h.resize(w, hh)
		h.key("a")
		walk(t, h, w, hh, "account new", "enter", "type:erin", "enter", "enter", "type:erin@acme.com", "enter", "down", "enter", "y",
			"enter", "enter", "enter", "enter", "enter")

		h = newHarness(t, basicCfg())
		h.resize(w, hh)
		h.selectKey("acme/alice")
		h.key("e")
		walk(t, h, w, hh, "account edit", "enter", "enter", "enter", "enter", "enter", "enter")

		h = importerHarness(t)
		h.resize(w, hh)
		h.key("enter")
		walk(t, h, w, hh, "importer", "enter", "type:dana", "enter", "enter")
	}
}

// ---- the render matrix ----

type matrixForm struct {
	name  string
	open  func(t *testing.T) *harness
	user  bool // has a username field
	email bool // has an email field
}

func matrixForms() []matrixForm {
	return []matrixForm{
		{"company new", func(t *testing.T) *harness { h := newHarness(t, basicCfg()); h.key("A"); return h }, false, false},
		{"company edit", func(t *testing.T) *harness { h := newHarness(t, basicCfg()); h.key("e"); return h }, false, false},
		{"account new (empty company)", func(t *testing.T) *harness { h := newHarness(t, emptyCompanyCfg("personal")); h.key("a"); return h }, true, true},
		{"account new (ssh, advanced)", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.key("a")
			f := h.top().(*accountForm)
			f.form.moveTo(4)
			f.form.Update(fk("down")) // SSH
			f.form.moveTo(5)
			f.form.Update(fk("y")) // advanced: yes
			return h
		}, true, true},
		{"account edit tinhnd", func(t *testing.T) *harness {
			h := newHarness(t, oneAccCfg())
			h.selectKey("personal/tinhnd")
			h.key("e")
			return h
		}, true, true},
		{"account edit globex/dave", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.selectKey("globex/dave")
			h.key("e")
			return h
		}, true, true},
		{"importer", func(t *testing.T) *harness { h := importerHarness(t); h.key("enter"); return h }, true, false},
	}
}

// focusPositions moves the focus of the open form through every visible field
// and calls fn for each (title of the field).
func focusPositions(t *testing.T, h *harness, fn func(title string)) {
	t.Helper()
	var f *form
	switch s := h.top().(type) {
	case *accountForm:
		f = s.form
	case *companyForm:
		f = s.form
	case *importScreen:
		f = s.form
	default:
		t.Fatalf("top is %T", h.top())
	}
	for i := range f.fields {
		if !f.visible(i) {
			continue
		}
		f.moveTo(i)
		fn(f.fields[i].title)
	}
}

func TestHMatrixEveryFormFitsEverywhere(t *testing.T) {
	defer i18n.SetLang("vi")
	sizes := [][2]int{{40, 10}, {41, 11}, {60, 20}, {72, 24}, {80, 24}, {100, 30}, {120, 40}}
	for _, lang := range []string{"vi", "en"} {
		i18n.SetLang(lang)
		for _, mf := range matrixForms() {
			h := mf.open(t)
			for _, s := range sizes {
				w, hh := s[0], s[1]
				h.resize(w, hh)
				focusPositions(t, h, func(title string) {
					v := h.view()
					label := fmt.Sprintf("%s %s focus %q", lang, mf.name, title)
					assertFits(t, v, w, hh, label)
					if w < 60 {
						return
					}
					if mf.user && title == tr("tui.f.username") && !onOneLine(v, "github.com/<login>") {
						t.Errorf("%s %dx%d: the profile URL is split:\n%s", label, w, hh, v)
					}
					if mf.email && title == tr("tui.f.email") && !onOneLine(v, "ID+login@users.noreply.github.com") {
						t.Errorf("%s %dx%d: the noreply address is split:\n%s", label, w, hh, v)
					}
				})
			}
		}
	}
}

func TestHMatrixRemoteStaysWhole(t *testing.T) {
	for _, s := range [][2]int{{60, 20}, {72, 24}, {80, 24}, {100, 30}, {120, 40}} {
		h := newHarness(t, emptyCompanyCfg("personal"))
		h.resize(s[0], s[1])
		h.key("a")
		h.key("enter")
		h.typeText("tinhnd1902")
		focusPositions(t, h, func(title string) {
			if !onOneLine(h.view(), "https://tinhnd1902@github.com/<owner>/<repo>.git") {
				t.Errorf("%v focus %q: the remote is split:\n%s", s, title, h.view())
			}
		})
	}
}

func TestHResizeKeepsTheEmailFieldFocusedAndRewrapped(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.resize(120, 40)
	h.key("a")
	h.key("enter")
	h.typeText("alice")
	h.key("enter")
	h.typeText("Alice Tran")
	h.key("enter")
	if got := focusedTitle(t, h); got != tr("tui.f.email") {
		t.Fatalf("focus on %q", got)
	}
	for _, s := range [][2]int{{60, 20}, {80, 24}} {
		h.update(tea.WindowSizeMsg{Width: s[0], Height: s[1]}) // no key follows
		if got := focusedTitle(t, h); got != tr("tui.f.email") {
			t.Fatalf("%v: focus on %q", s, got)
		}
		h.mustContainSpaceless(tr("tui.form.email_hint"))
		h.mustHaveOnOneLine("email", "ID+login@users.noreply.github.com")
		assertFits(t, h.view(), s[0], s[1], "resized form")
	}
}

func TestHStaleRefusalShowsAtTheTopAndFocusesTheFirstField(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	writeBehindTheBack(t, h)
	h.key("a")
	h.key("enter")
	h.typeText("carol")
	h.key("enter")
	h.key("enter")
	h.typeText("carol@example.com")
	h.key("enter")
	h.key("enter")
	h.key("enter")
	h.key("enter")
	msg := plain(tr("tui.err.config_changed"))
	h.waitFor("refused", func() bool { return strings.Contains(plain(h.view()), msg) })
	h.waitFor("reloaded", func() bool { return accountIDs(h.m.sh.cfg) == "tinhnd,bob" })
	f, ok := h.top().(*accountForm)
	if !ok {
		t.Fatalf("top is %T", h.top())
	}
	if f.form.focus != 0 || f.form.Err() != "" {
		t.Fatalf("focus %d err %q: the form must start over at the first field", f.form.focus, f.form.Err())
	}
	lines := h.lines()
	top, preview := -1, -1
	for i, l := range lines {
		if top < 0 && strings.Contains(l, "✗") {
			top = i
		}
		if preview < 0 && strings.Contains(l, "Remote") {
			preview = i
		}
	}
	if top < 0 || preview < 0 || top >= preview {
		t.Fatalf("the refusal must be above the preview (error row %d, preview row %d):\n%s", top, preview, h.view())
	}
	if len(f.errLines()) == 0 {
		t.Fatal("the form-level error area is empty")
	}
}

func TestFormGoldenAccount80x24(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.resize(80, 24)
	h.key("a")
	h.key("enter")
	h.typeText("dora")
	golden.RequireEqual(t, []byte(h.view()))
}

// pasteEmail pastes text into the focused input as one bracketed paste.
func (h *harness) pasteText(s string) {
	h.t.Helper()
	h.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Paste: true})
}

const longEmail84 = "very.long.local.part.for.testing.ui+1234567890@subdomain.example-company-name.co.uk"

// flat removes the frame, the gutter and all blanks so that a value wrapped
// over several rows can be searched whole.
func flat(v string) string {
	r := strings.NewReplacer("│", "", "┃", "", " ", "", "\n", "")
	return r.Replace(plain(v))
}

// D1: a blurred long value is shown whole (wrapped), not cut with an ellipsis.
func TestFixD1BlurredLongValueWraps(t *testing.T) {
	defer i18n.SetLang("vi")
	for _, lang := range []string{"vi", "en"} {
		i18n.SetLang(lang)
		for _, s := range [][2]int{{80, 24}, {72, 24}, {100, 30}, {60, 20}} {
			h := newHarness(t, basicCfg())
			h.resize(s[0], s[1])
			h.key("a")
			h.key("enter")
			h.typeText("dora")
			h.key("enter")
			h.key("enter")
			h.pasteText(longEmail84)
			h.key("enter")
			v := h.view()
			label := fmt.Sprintf("%s %v", lang, s)
			if f := h.top().(*accountForm); f.email != longEmail84 {
				t.Fatalf("%s: email %q", label, f.email)
			}
			if got := focusedTitle(t, h); got == tr("tui.f.email") {
				t.Fatalf("%s: focus did not leave the email", label)
			}
			if !strings.Contains(flat(v), longEmail84) {
				t.Errorf("%s: the blurred email is not whole:\n%s", label, v)
			}
			if strings.Contains(v, "…") {
				t.Errorf("%s: an ellipsis in a non-condensed form:\n%s", label, v)
			}
			assertFits(t, v, s[0], s[1], label)
		}
	}
}

func TestFixD1TinyBlurredValueFitsOrCutsCleanly(t *testing.T) {
	defer i18n.SetLang("vi")
	for _, lang := range []string{"vi", "en"} {
		i18n.SetLang(lang)
		for _, s := range [][2]int{{40, 10}, {41, 11}} {
			h := newHarness(t, basicCfg())
			h.resize(s[0], s[1])
			h.key("a")
			h.key("enter")
			h.typeText("dora")
			h.key("enter")
			h.key("enter")
			h.pasteText(longEmail84)
			h.key("enter")
			v := h.view()
			label := fmt.Sprintf("%s %v", lang, s)
			assertFits(t, v, s[0], s[1], label)
			if !strings.Contains(flat(v), longEmail84) && !strings.Contains(v, "…") {
				t.Errorf("%s: neither whole nor cleanly cut:\n%s", label, v)
			}
		}
	}
}

// titleShown reports whether the view has the title (or its word-cut form
// ending with an ellipsis) on a focused row.
func titleShown(v, title string) bool {
	for _, l := range strings.Split(ansi.Strip(v), "\n") {
		i := strings.Index(l, "┃ ")
		if i < 0 {
			continue
		}
		text := strings.TrimSpace(strings.TrimSuffix(strings.TrimRight(l[i+len("┃ "):], " "), "│"))
		if text == title {
			return true
		}
		if cut, ok := strings.CutSuffix(text, "…"); ok && cut != "" && strings.HasPrefix(title, strings.TrimSpace(cut)) {
			return true
		}
	}
	return false
}

// titleCannotFit is true when the control of the focused field fills the
// whole form area, so no row is left for a title (the control comes first).
func titleCannotFit(h *harness) bool {
	var f *form
	switch s := h.top().(type) {
	case *accountForm:
		f = s.form
	case *companyForm:
		f = s.form
	default:
		return false
	}
	p := f.parts(f.focus, "")
	return len(p.ctrl)+1 > f.rows
}

// D2 and D3: at the smallest sizes a focused title is never cut in the middle
// of a word without an ellipsis, and it stays when it fits above the control.
func TestFixD2D3TinyTitlesStayAndCutAtWords(t *testing.T) {
	defer i18n.SetLang("vi")
	for _, lang := range []string{"vi", "en"} {
		i18n.SetLang(lang)
		for _, mf := range matrixForms() {
			h := mf.open(t)
			for _, s := range [][2]int{{40, 10}, {41, 11}} {
				h.resize(s[0], s[1])
				focusPositions(t, h, func(title string) {
					v := h.view()
					label := fmt.Sprintf("%s %s %v focus %q", lang, mf.name, s, title)
					assertFits(t, v, s[0], s[1], label)
					if !titleShown(v, title) && !titleCannotFit(h) {
						t.Errorf("%s: the title is missing or cut without an ellipsis:\n%s", label, v)
					}
				})
			}
		}
	}
}

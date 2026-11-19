package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/keymap"
)

// Fix cycle 2 of the unslop passes: the help bar always has a next step in
// row 1, the next step is said once, and a few small facts the QA report found.
// Focused tests: each opens the guard's own state and looks at one thing.

func grdState1(t *testing.T, name string) *harness {
	t.Helper()
	defaultIdle = 15 * time.Millisecond
	t.Cleanup(func() { defaultIdle = 0 })
	for _, st := range grdStates() {
		if st.name == name {
			return st.setup(t, "")
		}
	}
	t.Fatalf("no guard state %q", name)
	return nil
}

// body is the frame without the status line and the help bar: the borders are
// blanked but the lines and the spacing stay (plain would join them).
func (h *harness) body() string {
	l := h.lines()
	return rawFrame(strings.Join(l[:len(l)-1-h.m.helpRowCount()], "\n"))
}

// D1: no help bar has a blank row 1; in these states row 1 holds a primary
// next step the screen handles (the guard presses every key of the bar).
func TestFix2RowOneHasANextStep(t *testing.T) {
	states := []string{"import none", "keygen error", "repos local empty", "repos local no companies",
		"repos local scanning", "repos online error", "repos online fetching", "repos online no token"}
	for _, name := range states {
		for _, sz := range [][2]int{{40, 10}, {60, 20}, {80, 24}, {120, 40}} {
			h := grdState1(t, name)
			h.resize(sz[0], sz[1])
			rows := h.helpRows()
			if strings.TrimSpace(rows[0]) == "" {
				t.Errorf("%s %v: row 1 is blank:\n%s", name, sz, h.view())
			}
			if !strings.HasPrefix(rows[0], "▸ ") {
				t.Errorf("%s %v: row 1 has no primary: %q", name, sz, rows[0])
			}
			assertFits(t, h.view(), sz[0], sz[1], name)
		}
	}
}

// D1: the primary of each of those states is the key that moves on.
func TestFix2RowOnePrimaryKeys(t *testing.T) {
	want := map[string]string{
		"import none":              "▸ esc " + tr("tui.key.to_accounts"),
		"keygen error":             "▸ esc " + tr("tui.key.back"),
		"repos local empty":        "▸ r ",
		"repos local no companies": "▸ esc " + tr("tui.key.to_accounts"),
		"repos local scanning":     "▸ esc " + tr("tui.key.stop_scan"),
		"repos online error":       "▸ r " + tr("tui.key.retry"),
		"repos online fetching":    "▸ esc " + tr("tui.key.stop_fetch"),
		"repos online no token":    "▸ r " + tr("tui.key.retry"),
	}
	for name, prefix := range want {
		h := grdState1(t, name)
		if hb := h.helpLine(); !strings.HasPrefix(hb, prefix) {
			t.Errorf("%s: help bar %q, want it to start with %q", name, hb, prefix)
		}
	}
}

// D1: an exit key that is the primary is not drawn a second time as the exit.
func TestFix2PrimaryExitIsNotRepeated(t *testing.T) {
	h := grdState1(t, "repos local scanning")
	if n := strings.Count(h.helpLine(), "esc "); n != 1 {
		t.Errorf("esc is drawn %d times: %q", n, h.helpLine())
	}
}

// D2: the help bar's primary names the key; the body does not say it again.
func TestFix2NextStepIsSaidOnce(t *testing.T) {
	for _, name := range []string{"confirm ready", "check result", "repos online list", "confirm done", "list account applied"} {
		h := grdState1(t, name)
		bar := h.m.helpBarItems()
		if len(bar.row) == 0 || !bar.row[0].primary {
			t.Errorf("%s: row 1 has no primary", name)
			continue
		}
		k := bar.row[0].b.Help().Key
		body := h.body()
		for _, pk := range proseKeys(body) {
			if pk == k {
				t.Errorf("%s: the body says %q again, the bar has it:\n%s", name, k, body)
			}
		}
		for _, w := range []string{"Bấm ", "Press "} {
			if strings.Contains(body, w+k+" ") {
				t.Errorf("%s: the body says %q:\n%s", name, w+k, body)
			}
		}
		if stepKeyRe.MatchString(body) {
			t.Errorf("%s: the body has a step that carries a key:\n%s", name, body)
		}
	}
}

// D2: the account row has c clone as the primary once the config is applied.
func TestFix2AppliedAccountRowPrimaryIsClone(t *testing.T) {
	h := grdState1(t, "list account applied")
	if hb := h.helpLine(); !strings.HasPrefix(hb, "▸ c "+tr("tui.key.clone")) {
		t.Errorf("help bar %q", hb)
	}
	// pending changes: apply stays the next step
	h = grdState1(t, "list pending")
	if hb := h.helpLine(); !strings.HasPrefix(hb, "▸ p ") {
		t.Errorf("help bar %q", hb)
	}
}

// D2: the empty company says it was left alone once in the pane, and not that
// it has no account (the row and the badge do).
func TestFix2EmptyCompanyDetailDoesNotRepeatTheFact(t *testing.T) {
	h := grdState1(t, "list company without accounts")
	d := h.detail()
	if strings.Contains(d, tr("tui.list.count_zero")) || strings.Contains(d, tr("tui.detail.no_default")) {
		t.Errorf("the detail says again that there is no account:\n%s", d)
	}
	if !strings.Contains(d, tr("tui.list.no_accounts_why", "personal")) {
		t.Errorf("the detail lost its explanation:\n%s", d)
	}
}

// D3: the help bar leads back to Accounts, not to "the list".
func TestFix2HelpBarSaysAccounts(t *testing.T) {
	defer i18n.SetLang("vi")
	for _, lang := range []string{"vi", "en"} {
		i18n.SetLang(lang)
		if got := tr("tui.key.to_accounts"); !strings.Contains(got, tr("tui.list.title")) {
			t.Errorf("%s: %q does not name %q", lang, got, tr("tui.list.title"))
		}
		h := grdState1(t, "confirm done")
		if hb := h.helpLine(); !strings.Contains(hb, "enter "+tr("tui.key.to_accounts")) {
			t.Errorf("%s: help bar %q", lang, hb)
		}
	}
}

// D7: an item of the doctor has one status glyph, in front of its name, even
// when it has several results.
func TestFix2DoctorOneGlyphPerItem(t *testing.T) {
	h := grdState1(t, "doctor")
	h.resize(100, 80)
	head := regexp.MustCompile(`^ {1,2}[✓✗!] `)
	indented := regexp.MustCompile(`^ {3,}[✓✗!] `)
	items := 0
	for _, l := range strings.Split(h.body(), "\n") {
		if indented.MatchString(l) {
			t.Errorf("a result carries its own glyph: %q", l)
		}
		if head.MatchString(l) {
			items++
		}
	}
	// the summary and the items (the SSH key item has two results) have theirs
	if items < 10 {
		t.Errorf("only %d lines carry a glyph:\n%s", items, h.body())
	}
}

// D8: the identity stamp of the account form appears only for a valid email.
func TestFix2FormStampNeedsAValidEmail(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.resize(100, 50)
	h.key("a")
	h.key("enter") // platform
	h.typeText("alice")
	h.key("enter")
	h.typeText("Alice Tran")
	h.key("enter") // focus: email
	h.typeText("not-an-email")
	if v := plain(h.view()); strings.Contains(v, "<not-an-email>") {
		t.Errorf("the stamp shows an invalid email:\n%s", v)
	}
	for range "not-an-email" {
		h.key("backspace")
	}
	h.typeText("alice@acme.com")
	if v := plain(h.view()); !strings.Contains(v, "<alice@acme.com>") {
		t.Errorf("the stamp is missing for a valid email:\n%s", v)
	}
}

// D9: while the scan has not reached a company, its row has no counts.
func TestFix2NoZeroCountsWhileScanning(t *testing.T) {
	h := grdState1(t, "repos local scanning")
	zero := tr("tui.repos.local.repos_many", 0)
	if v := h.body(); strings.Contains(v, zero) {
		t.Errorf("the scan shows %q before it found anything:\n%s", zero, v)
	}
	// a finished empty scan does say 0
	h = grdState1(t, "repos local empty")
	if v := h.body(); !strings.Contains(v, zero) {
		t.Errorf("a finished empty scan lost its count:\n%s", v)
	}
}

// D10: the empty URL tab offers nothing to choose; with a URL typed, enter does.
func TestFix2EmptyURLTabOffersNoChoice(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.act(keymap.Clone)
	if _, ok := h.top().(*reposScreen); !ok {
		t.Fatalf("top is %T", h.top())
	}
	choose := "enter " + tr("tui.key.select")
	rows := h.helpRows()
	if strings.Contains(strings.Join(rows, " "), choose) {
		t.Errorf("the empty URL tab says %q: %q", choose, rows)
	}
	if strings.TrimSpace(rows[0]) == "" {
		t.Errorf("row 1 is blank: %q", rows)
	}
	h.typeText("https://github.com/org/repo.git")
	if hb := h.helpLine(); !strings.Contains(hb, choose) {
		t.Errorf("a typed URL lost %q: %q", choose, hb)
	}
}

// D11: the done screen says "applied" once.
func TestFix2DoneSaysAppliedOnce(t *testing.T) {
	h := grdState1(t, "confirm done")
	h.waitFor("badge", func() bool { return h.m.sh.planned && h.m.sh.pending == 0 })
	if n := strings.Count(plain(h.view()), tr("tui.confirm.done")); n != 1 {
		t.Errorf("%q is on the frame %d times:\n%s", tr("tui.confirm.done"), n, h.view())
	}
	if strings.Contains(plain(h.view()), tr("tui.status.synced")) {
		t.Errorf("the badge repeats the done line:\n%s", h.view())
	}
	// back on the list the badge is there
	h.key("enter")
	if _, ok := h.top().(*listScreen); !ok {
		t.Fatalf("top is %T", h.top())
	}
	h.mustContainPlain(tr("tui.status.synced"))
}

package tui

// Tests of the searchable "?" overlay (OV) and its palette alias (PK-3).

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/keymap"
	"github.com/tinhnd1902/gitid/internal/keys"
)

// ---- helpers ----

func (h *harness) overlayOpen() bool { return h.m.showHelp }

func (h *harness) filterValue() string { return h.m.ov.filter.Value() }

// selectedRow is the row the overlay would run.
func (h *harness) selectedRow() (overlayRow, bool) {
	screen, _ := h.m.overlayRows()
	return h.m.overlaySelected(screen)
}

// runnableRows are the visible rows that can be selected.
func (h *harness) runnableRows() []overlayRow {
	screen, _ := h.m.overlayRows()
	var out []overlayRow
	for _, r := range screen {
		if r.runnable {
			out = append(out, r)
		}
	}
	return out
}

// sig is what OV-8 compares: the effect of a key, however it was pressed.
func (h *harness) sig() string {
	return fmt.Sprintf("%d|%T|%v|%d|%d|%s", h.depth(), h.top(), h.quit(), len(h.fake.Calls()), h.m.sh.nextID, h.status())
}

// ---- OV-1 .. OV-7 ----

func TestOV1OpenWithHelpAndPalette(t *testing.T) {
	for _, k := range []string{"?", "ctrl+k"} {
		h := newHarness(t, basicCfg())
		h.key(k)
		if !h.overlayOpen() {
			t.Fatalf("%s did not open the overlay", k)
		}
		h.mustContain(tr("tui.help.title"), tr("tui.help.filter_hint"))
		if k == "ctrl+k" {
			h.key("ctrl+k")
			if h.overlayOpen() {
				t.Fatal("the palette key did not close the overlay")
			}
		}
	}
}

func TestOV2FilterByDescriptionWithAndWithoutDiacritics(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("?")
	for name, typed := range map[string]func(){
		"ascii":    func() { h.typeText("cong ty") },
		"composed": func() { h.sendRunes("công ty") },
	} {
		typed()
		sel, ok := h.selectedRow()
		if !ok || sel.desc != tr("tui.key.add_company") {
			t.Fatalf("%s: selected %+v", name, sel)
		}
		h.mustContainPlain("▸ A")
		h.mustNotContain(tr("tui.key.delete"))
		h.key("esc")
		if h.filterValue() != "" {
			t.Fatalf("esc did not clear the filter: %q", h.filterValue())
		}
	}
	defer i18n.SetLang("vi")
	i18n.SetLang("en")
	e := newHarness(t, basicCfg())
	e.key("?")
	e.typeText("company")
	if sel, ok := e.selectedRow(); !ok || sel.desc != tr("tui.key.add_company") {
		t.Fatalf("en: selected %+v", sel)
	}
}

func TestOV3OneCharacterFilter(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("?")
	h.typeText("A")
	rows := h.runnableRows()
	if len(rows) < 3 {
		t.Fatalf("only %d rows match", len(rows))
	}
	for _, r := range rows {
		ok := false
		for _, p := range keyParts(r.keyText) {
			ok = ok || p == "a"
		}
		for _, w := range strings.Fields(foldText(r.desc)) {
			ok = ok || strings.HasPrefix(w, "a")
		}
		if !ok {
			t.Errorf("row %q %q must not match A", r.keyText, r.desc)
		}
	}
	if sel, _ := h.selectedRow(); sel.id() != rows[0].id() {
		t.Errorf("selected %q, want the first match %q", sel.desc, rows[0].desc)
	}
}

func TestOV4NoMatch(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("?")
	h.typeText("zzzq")
	h.mustContain(tr("tui.help.no_match", "zzzq"))
	// UX 1: nothing to select, so the help bar offers esc and the filter reset
	bar := strings.Join(h.helpRows(), "  ")
	if strings.Contains(bar, "↑↓") || strings.Contains(bar, "enter") || !strings.Contains(bar, "esc "+tr("tui.key.filter_clear")) {
		t.Errorf("help bar %q on an overlay without a match", bar)
	}
	before := h.snapshot()
	h.key("enter")
	if h.snapshot() != before || !h.overlayOpen() {
		t.Fatal("enter with no match must do nothing")
	}
}

func TestOV5SelectionStaysOnRunnableRows(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("?")
	run := h.runnableRows()
	if len(run) < 5 {
		t.Fatalf("%d runnable rows", len(run))
	}
	runIDs := map[string]bool{}
	for _, r := range run {
		runIDs[r.id()] = true
	}
	prev := -1
	for i := 0; i < len(run)+3; i++ {
		h.update(keyMsg("down"))
		sel, ok := h.selectedRow()
		if !ok || !runIDs[sel.id()] || sel.global {
			t.Fatalf("step %d: selected %+v", i, sel)
		}
		at := -1
		for j, r := range run {
			if r.id() == sel.id() {
				at = j
			}
		}
		if at < prev {
			t.Fatalf("step %d went backwards", i)
		}
		prev = at
	}
	if sel, _ := h.selectedRow(); sel.id() != run[len(run)-1].id() {
		t.Errorf("down did not stop at the last runnable row: %q", sel.desc)
	}
	for i := 0; i < len(run)+3; i++ {
		h.update(keyMsg("up"))
	}
	if sel, _ := h.selectedRow(); sel.id() != run[0].id() {
		t.Errorf("up did not stop at the first runnable row: %q", sel.desc)
	}
}

func TestOV6SelectionStartsOnTheNextStep(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.selectKey("acme/alice")
	h.key("?")
	sel, ok := h.selectedRow()
	if !ok || sel.desc != tr("tui.key.apply") {
		t.Fatalf("selected %+v, want the apply row", sel)
	}
	h.mustContainPlain("▸ p")
}

func TestOV7ReopenResets(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.selectKey("acme/alice")
	h.key("?")
	h.typeText("x")
	h.key("esc")
	h.key("esc")
	if h.overlayOpen() {
		t.Fatal("two esc must close")
	}
	h.key("?")
	if h.filterValue() != "" {
		t.Errorf("filter %q after reopening", h.filterValue())
	}
	if sel, _ := h.selectedRow(); sel.desc != tr("tui.key.apply") {
		t.Errorf("selected %q after reopening", sel.desc)
	}
	h.mustContain(tr("tui.help.filter_hint"))
}

// ---- OV-8: running a row is the same as pressing its key ----

// viaOverlay opens the overlay, filters to the row of the key and runs it.
func viaOverlay(t *testing.T, h *harness, desc, wantKey string) {
	t.Helper()
	h.key("?")
	h.typeText(desc)
	sel, ok := h.selectedRow()
	if !ok || sel.b.Keys()[0] != wantKey {
		t.Fatalf("filter %q selected %+v, want key %q", desc, sel, wantKey)
	}
	h.key("enter")
	if h.overlayOpen() {
		t.Fatal("the overlay is still open after running a row")
	}
}

func TestOV8OverlayEqualsKey(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T) *harness
		key   string
		desc  string
		check func(t *testing.T, h *harness)
	}{
		{"add company", func(t *testing.T) *harness { return newHarness(t, basicCfg()) }, "A", tr("tui.key.add_company"), nil},
		{"apply", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.selectKey("acme/alice")
			return h
		}, "p", tr("tui.key.apply"), nil},
		{"check from the detail", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.resize(50, 24)
			openDetail(h, "acme/carol")
			return h
		}, "k", tr("tui.key.check"), nil},
		{"copy the key", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.fake.On("pbcopy", execx.Response{})
			ref, _ := h.m.sh.cfg.ParseRef("acme/carol")
			h.send(pushMsg{newKeyResultScreen(h.m.sh, ref, &keys.Pub{Content: "ssh-ed25519 AAAA carol"})})
			return h
		}, "c", tr("tui.key.copy"), func(t *testing.T, h *harness) {
			h.waitFor("copied", func() bool { return h.fake.Count("pbcopy") == 1 })
		}},
	}
	for _, c := range cases {
		direct := c.setup(t)
		direct.key(c.key)
		overlay := c.setup(t)
		viaOverlay(t, overlay, c.desc, c.key)
		if c.check != nil {
			c.check(t, direct)
			c.check(t, overlay)
		}
		if a, b := direct.sig(), overlay.sig(); a != b {
			t.Errorf("%s: key %q gives %s, the overlay gives %s", c.name, c.key, a, b)
		}
		if overlay.overlayOpen() {
			t.Errorf("%s: the overlay is open", c.name)
		}
	}
}

// OV-22: a row that replaces the screen under the overlay does it the same.
func TestOV22ReplaceFromTheDetail(t *testing.T) {
	setup := func() *harness {
		h := newHarness(t, basicCfg())
		h.resize(50, 24)
		openDetail(h, "acme/bob")
		return h
	}
	direct := setup()
	direct.key("e")
	overlay := setup()
	viaOverlay(t, overlay, tr("tui.key.edit"), "e")
	if direct.depth() != 2 || overlay.depth() != 2 || fmt.Sprintf("%T", direct.top()) != fmt.Sprintf("%T", overlay.top()) {
		t.Errorf("direct %d %T, overlay %d %T", direct.depth(), direct.top(), overlay.depth(), overlay.top())
	}
	if _, ok := overlay.top().(*accountForm); !ok {
		t.Errorf("top is %T", overlay.top())
	}
}

// ---- OV-9 .. OV-13: keys inside the overlay ----

func TestOV9GlobalRowsCannotRun(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("?")
	h.typeText("pgup")
	if len(h.runnableRows()) != 0 {
		t.Fatalf("rows %+v", h.runnableRows())
	}
	h.mustContain("pgup")
	h.mustNotContain("▸ pgup")
	before := h.snapshot()
	h.key("enter")
	if h.snapshot() != before {
		t.Fatal("enter on a global row must do nothing")
	}
}

func TestOV10EscClearsThenCloses(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("?")
	h.typeText("ab")
	h.key("esc")
	if !h.overlayOpen() || h.filterValue() != "" {
		t.Fatalf("open=%v filter %q", h.overlayOpen(), h.filterValue())
	}
	h.key("esc")
	if h.overlayOpen() {
		t.Fatal("the second esc must close")
	}
}

func TestOV11QuitKeyInTheOverlay(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("?")
	h.key("q")
	if h.overlayOpen() || h.quit() {
		t.Fatalf("q on an empty filter: open=%v quit=%v", h.overlayOpen(), h.quit())
	}
	h = newHarness(t, basicCfg())
	h.key("?")
	h.typeText("x")
	h.key("q")
	if !h.overlayOpen() || h.filterValue() != "xq" || h.quit() {
		t.Fatalf("q after x: open=%v filter %q quit=%v", h.overlayOpen(), h.filterValue(), h.quit())
	}
}

func TestOV12HelpAndPaletteAlwaysClose(t *testing.T) {
	for _, k := range []string{"?", "ctrl+k"} {
		h := newHarness(t, basicCfg())
		h.key("?")
		h.typeText("ed")
		h.key(k)
		if h.overlayOpen() {
			t.Errorf("%s did not close the overlay with a filter", k)
		}
	}
}

func TestOV13ComposedInputGoesToTheFilter(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("?")
	h.sendRunes("â")
	if h.status() == imeVi || h.filterValue() != "â" || !h.overlayOpen() {
		t.Fatalf("status %q filter %q open=%v", h.status(), h.filterValue(), h.overlayOpen())
	}
	// without the overlay the warning stays
	h = newHarness(t, basicCfg())
	h.sendRunes("â")
	if h.status() != imeVi {
		t.Fatalf("status %q", h.status())
	}
}

// ---- OV-14 .. OV-19 ----

func TestOV14OverlayHelpBar(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("?")
	bar := strings.Join(h.helpRows(), "  ")
	for _, want := range []string{"enter " + tr("tui.key.run"), "↑↓", "esc " + tr("tui.key.close")} {
		if !strings.Contains(bar, want) {
			t.Errorf("help bar %q lacks %q", bar, want)
		}
	}
	h.typeText("a")
	bar = strings.Join(h.helpRows(), "  ")
	if !strings.Contains(bar, "esc "+tr("tui.key.filter_clear")) {
		t.Errorf("help bar %q lacks the clear-filter key", bar)
	}
	for _, not := range []string{"p " + tr("tui.key.apply"), "? " + tr("tui.key.all_keys")} {
		if strings.Contains(bar, not) {
			t.Errorf("help bar %q still has %q", bar, not)
		}
	}
}

func TestOV15SelectionVisibleOnSmallScreens(t *testing.T) {
	for _, sz := range [][2]int{{40, 10}, {60, 20}} {
		h := newHarness(t, basicCfg())
		h.resize(sz[0], sz[1])
		h.key("?")
		n := len(h.runnableRows())
		for i := 0; i < n+1; i++ {
			h.update(keyMsg("down"))
		}
		sel, _ := h.selectedRow()
		if sel.id() != h.runnableRows()[n-1].id() {
			t.Fatalf("%v: not on the last row: %q", sz, sel.desc)
		}
		v := h.view()
		assertFits(t, v, sz[0], sz[1], "overlay")
		line := ""
		for _, l := range strings.Split(plain(v), "\n") {
			if strings.Contains(l, "▸ "+sel.keyText) {
				line = l
			}
		}
		first := strings.Fields(sel.desc)[0]
		if line == "" || !strings.Contains(line, strings.TrimSuffix(first, "…")) {
			t.Errorf("%v: the selected row %q %q is not on screen:\n%s", sz, sel.keyText, sel.desc, v)
		}
	}
}

func TestOV16OverlayStillShowsEverythingAndThePalette(t *testing.T) {
	for _, sz := range [][2]int{{60, 20}, {80, 24}, {100, 30}} {
		h := newHarness(t, basicCfg())
		h.resize(sz[0], sz[1])
		h.key("?")
		v := plain(h.view())
		for _, want := range []string{tr("tui.help.global"), "esc/q", "↑/↓", "pgup/pgdn", "home", "ctrl+c", "ctrl+k", "? " + tr("tui.key.help")} {
			if !strings.Contains(v, want) {
				t.Errorf("%v: no %q:\n%s", sz, want, h.view())
			}
		}
		if sz[0] >= 100 && !strings.Contains(v, "ctrl+k "+tr("tui.key.palette")) {
			t.Errorf("%v: no palette row:\n%s", sz, v)
		}
		assertFits(t, h.view(), sz[0], sz[1], "overlay")
	}
}

func TestOV17ResizeKeepsFilterAndSelection(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("?")
	h.typeText("e")
	h.update(keyMsg("down"))
	sel, _ := h.selectedRow()
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	if h.filterValue() != "e" {
		t.Errorf("filter %q", h.filterValue())
	}
	if now, _ := h.selectedRow(); now.id() != sel.id() {
		t.Errorf("selection moved from %q to %q", sel.desc, now.desc)
	}
	assertFits(t, h.view(), 80, 24, "overlay")
}

func TestOV18SelectionSurvivesLateResults(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.resize(100, 12)
	h.update(keyMsg(h.m.sh.km.keys.Label(keymap.Doctor)))
	d, ok := h.top().(*doctorScreen)
	if !ok {
		t.Fatalf("top is %T", h.top())
	}
	h.update(keyMsg("?"))
	h.update(keyMsg("down"))
	sel, ok := h.selectedRow()
	if !ok {
		t.Fatal("nothing selected")
	}
	if first := h.runnableRows()[0]; first.id() == sel.id() {
		t.Fatal("down did not move to the second runnable row")
	}
	h.waitFor("checks", func() bool { return !d.pending() })
	if now, ok := h.selectedRow(); !ok || now.id() != sel.id() {
		t.Errorf("selection %+v, want %q", now, sel.desc)
	}
}

func TestOV19BackAndQuitRowsRun(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.resize(50, 24)
	openDetail(h, "acme/carol")
	// (a filter that starts with q would close the overlay: q is the back key)
	viaOverlay(t, h, "lai", "esc")
	if h.depth() != 1 {
		t.Fatalf("depth %d", h.depth())
	}
	if _, ok := h.top().(*listScreen); !ok {
		t.Fatalf("top is %T", h.top())
	}
	viaOverlay(t, h, "thoat", defKM.keys.Label(keymap.Quit))
	if !h.quit() {
		t.Fatal("the quit row did not quit")
	}
}

// ---- OV-20 / OV-21: units ----

func TestOV20KeyMsgForRoundTrip(t *testing.T) {
	var all []string
	for _, y := range []string{"", func() string { y, _ := sentinelKeys(); return y }()} {
		m := keymap.Defaults()
		if y != "" {
			m, _ = keymap.Parse([]byte(y))
		}
		for _, d := range keymap.Defs() {
			all = append(all, m.Keys(d.Action)...)
		}
	}
	all = append(all, "enter", "esc", "tab", "shift+tab", "up", "down", "left", "right", "home", "end", "pgup", "pgdown",
		"ctrl+p", "ctrl+n", "ctrl+b", "ctrl+f", "ctrl+c", "f1", "f12", "alt+?", "alt+Z", "ctrl+k", "ctrl+z", "!", "/", "?", "A")
	for _, s := range all {
		k, ok := keyMsgFor(s)
		if !ok || k.String() != s {
			t.Errorf("keyMsgFor(%q) = %q, %v", s, k.String(), ok)
		}
	}
	for _, s := range []string{"enterx", "", "f0", "f13", "ctrl+", "alt+ab", "ctrl+1"} {
		if _, ok := keyMsgFor(s); ok {
			t.Errorf("keyMsgFor(%q) must fail", s)
		}
	}
}

func TestOV21FoldAndMatch(t *testing.T) {
	if got := foldText("Thêm Công Ty Đã Xóa"); got != "them cong ty da xoa" {
		t.Errorf("foldText = %q", got)
	}
	// the composed and the decomposed form fold the same
	if foldText("công") != foldText("co\u0302ng") {
		t.Error("composed and decomposed forms differ")
	}
	company := overlayRow{b: defKM.AddCompany, keyText: "A", desc: tr("tui.key.add_company"), runnable: true}
	check := overlayRow{b: defKM.Check, keyText: "k", desc: tr("tui.key.check"), runnable: true}
	if overlayMatch("cty", company) {
		t.Error("cty must not match: the fuzzy match stays inside one word")
	}
	for _, term := range []string{"cong", "công ty", "CONG TY", "A"} {
		if !overlayMatch(term, company) {
			t.Errorf("%q must match add company", term)
		}
	}
	if !overlayMatch("k", check) {
		t.Error("k must match the check row (its key)")
	}
	if !overlayMatch("k", overlayRow{b: defKM.GenKey, keyText: "g", desc: "key to make", runnable: true}) {
		t.Error("k must match a row with a word starting with k")
	}
	if overlayMatch("k", company) {
		t.Error("k must not match add company")
	}
}

// ---- PK-3: the palette key can be rebound ----

func TestPK3PaletteRebound(t *testing.T) {
	h := newHarnessKeys(t, basicCfg(), "palette: alt+p\n")
	h.key("alt+p")
	if !h.overlayOpen() {
		t.Fatal("alt+p did not open the overlay")
	}
	h.mustContainPlain("alt+p " + tr("tui.key.palette"))
	h.key("alt+p")
	if h.overlayOpen() {
		t.Fatal("alt+p did not close the overlay")
	}
	before := h.snapshot()
	h.key("ctrl+k")
	if h.snapshot() != before {
		t.Fatal("ctrl+k must do nothing once the palette is rebound")
	}
}

// UX 2: the close hint names the effective palette key too, and once there is
// a filter it no longer claims that q closes.
func TestOVCloseHintNamesPaletteKey(t *testing.T) {
	defer i18n.SetLang("vi")
	for _, lang := range []string{"vi", "en"} {
		i18n.SetLang(lang)
		h := newHarness(t, basicCfg())
		h.resize(100, 30)
		h.key("?")
		v := plain(h.view())
		if want := h.m.sh.km.T("tui.help.close"); !strings.Contains(v, want) || !strings.Contains(want, "ctrl+k") {
			t.Errorf("%s: no close hint with the palette key %q:\n%s", lang, want, v)
		}
		h.typeText("a")
		v = plain(h.view())
		want := h.m.sh.km.T("tui.help.close_filter")
		if !strings.Contains(v, want) || !strings.Contains(want, "ctrl+k") {
			t.Errorf("%s: no filter hint %q:\n%s", lang, want, v)
		}
		if strings.Contains(want, " q ") || strings.Contains(want, "q,") || strings.Contains(want, "/q") {
			t.Errorf("%s: the filter hint claims q closes: %q", lang, want)
		}
	}
}

// D4: when descriptions are cut, no two rows of a section come out the same
// and each keeps its first two words.
func TestOVCutDescriptionsStayDistinct(t *testing.T) {
	defer i18n.SetLang("vi")
	sep := regexp.MustCompile(`\s{2,}`)
	for _, lang := range []string{"vi", "en"} {
		i18n.SetLang(lang)
		for _, sz := range [][2]int{{60, 20}, {70, 20}, {80, 24}, {100, 30}} {
			h := newHarness(t, basicCfg())
			h.resize(sz[0], sz[1])
			h.key("?")
			screen, global := h.m.overlayAllRows()
			full := map[string]string{} // key text -> description
			for _, r := range append(append([]overlayRow{}, screen...), global...) {
				full[r.keyText+"\x00"+r.desc] = r.desc
			}
			section := ""
			seen := map[string]map[string]string{}
			for _, l := range strings.Split(ansi.Strip(h.view()), "\n") {
				l = strings.TrimSpace(strings.Trim(strings.TrimSpace(l), "│╭╮╰╯─"))
				l = strings.TrimSpace(strings.ReplaceAll(l, "▸ ", "  "))
				toks := sep.Split(l, -1)
				if len(toks) == 1 {
					section = l
					continue
				}
				if seen[section] == nil {
					seen[section] = map[string]string{}
				}
				for i := 0; i+1 < len(toks); i += 2 {
					k, d := toks[i], toks[i+1]
					if !strings.HasSuffix(d, "…") {
						continue
					}
					if n := len(strings.Fields(strings.TrimSuffix(d, "…"))); n < 2 {
						t.Errorf("%s %v: %q keeps %d word(s)", lang, sz, d, n)
					}
					if other, dup := seen[section][d]; dup {
						t.Errorf("%s %v: %q and %q both render %q in %q", lang, sz, other, k, d, section)
					}
					seen[section][d] = k
				}
			}
		}
	}
}

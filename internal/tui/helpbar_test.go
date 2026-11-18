package tui

// Tests of the two-row help bar (HB), the layout matrix of the reference
// sizes (LAY) and the focus indicator (FOC). The HB cases are pure: they feed
// renderHelpBar items built from the key map and need no harness.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/keymap"
)

// ---- helpers ----

func bar(items helpBarItems, w, rows int) []string {
	return renderHelpBar(NewStyles(), items, w, rows)
}

func cells(s string) int { return lipgloss.Width(s) }

func hi(b key.Binding) helpItem             { return helpItem{b: b} }
func hp(b key.Binding) helpItem             { return helpItem{b: b, primary: true} }
func hprio(b key.Binding, p int) helpItem   { return helpItem{b: b, prio: p} }
func hptr(b key.Binding) *helpItem          { it := hi(b); return &it }
func moveBinding(km *KeyMap) key.Binding    { return withDesc(km.Up, "↑↓", tr("tui.key.move")) }
func allKeysBinding(km *KeyMap) key.Binding { return relabel(km.Help, tr("tui.key.all_keys")) }
func itemText(it helpItem) string {
	h := it.b.Help()
	if it.primary {
		return "▸ " + h.Key + " " + h.Desc
	}
	return h.Key + " " + h.Desc
}

// listPendingItems is the list in the pending-plan state with an account row
// selected: a long row 1 (so that it overflows) and the always-on keys.
func listPendingItems(km *KeyMap) helpBarItems {
	return helpBarItems{
		row: []helpItem{hp(km.Apply), hi(km.Edit), hi(km.Delete), hi(km.Check), hi(km.Clone), hi(km.GenKey)},
		always: []helpItem{
			hprio(moveBinding(km), 2), hprio(km.AddCompany, 3), hprio(km.AddAccount, 1),
		},
		more:     hptr(allKeysBinding(km)),
		exit:     &helpItem{b: km.Quit, role: roleExit},
		rowLabel: "tui.help.row_this",
	}
}

func formItems(km *KeyMap) helpBarItems {
	return helpBarItems{
		row:      []helpItem{hp(km.Save), hi(km.Next)},
		exit:     hptr(km.Cancel),
		rowLabel: "tui.help.row_step",
	}
}

func discardItems(km *KeyMap) helpBarItems {
	return helpBarItems{row: []helpItem{hp(km.Yes), hi(km.No)}, rowLabel: "tui.help.row_step"}
}

func overlayItems(km *KeyMap) helpBarItems {
	return helpBarItems{
		row:      []helpItem{hp(withDesc(km.Enter, "enter", tr("tui.key.run"))), hi(withDesc(km.Up, "↑↓", tr("tui.key.select")))},
		exit:     hptr(withDesc(km.Back, "esc", tr("tui.key.close"))),
		rowLabel: "tui.help.row_step",
	}
}

// sentinelKM is the key map of the sentinel keys.yaml (every action on its own
// alt+letter).
func sentinelKM(t testing.TB) *KeyMap {
	t.Helper()
	y, _ := sentinelKeys()
	m, issues := keymap.Parse([]byte(y))
	if len(issues) > 0 {
		t.Fatalf("sentinel keys: %+v", issues)
	}
	return NewKeyMap(m)
}

func bothLangs(t *testing.T, f func(t *testing.T, lang string)) {
	t.Helper()
	defer i18n.SetLang("vi")
	for _, lang := range []string{"vi", "en"} {
		i18n.SetLang(lang)
		f(t, lang)
	}
}

// inOrder fails unless every part occurs in s, left to right.
func inOrder(t *testing.T, s string, parts ...string) {
	t.Helper()
	at := 0
	for _, p := range parts {
		i := strings.Index(s[at:], p)
		if i < 0 {
			t.Errorf("%q is not found (in order) after offset %d in %q", p, at, s)
			return
		}
		at += i + len(p)
	}
}

// startCol is the cell column where sub starts in line.
func startCol(line, sub string) int {
	i := strings.Index(line, sub)
	if i < 0 {
		return -1
	}
	return cells(line[:i])
}

// ---- HB: help bar builder ----

func TestHB1TwoRowsWithLabels(t *testing.T) {
	km := NewKeyMap(keymap.Defaults())
	lines := bar(listPendingItems(km), 120, 2)
	if len(lines) != 2 {
		t.Fatalf("%d lines", len(lines))
	}
	if !strings.HasPrefix(lines[0], " "+tr("tui.help.row_this")) || !strings.HasPrefix(lines[1], " "+tr("tui.help.row_always")) {
		t.Fatalf("labels missing:\n%s\n%s", lines[0], lines[1])
	}
	if a, b := startCol(lines[0], "▸ p "), startCol(lines[1], "↑↓ "); a != b || a < 0 {
		t.Errorf("the item text starts at columns %d and %d", a, b)
	}
	if !strings.Contains(lines[0], itemText(hp(km.Apply))) {
		t.Errorf("row 1 lacks the primary item: %q", lines[0])
	}
	inOrder(t, lines[1], itemText(hi(moveBinding(km))), itemText(hi(km.AddCompany)), itemText(hi(km.AddAccount)),
		itemText(hi(allKeysBinding(km))), itemText(hi(km.Quit)))
	if !strings.HasSuffix(strings.TrimRight(lines[1], " "), "q "+tr("tui.key.quit")) {
		t.Errorf("row 2 does not end with the quit item: %q", lines[1])
	}
	if strings.Contains(lines[0]+lines[1], "…") {
		t.Errorf("nothing may be dropped at 120 columns:\n%s\n%s", lines[0], lines[1])
	}
}

func TestHB2NoLabelsBelow80(t *testing.T) {
	km := NewKeyMap(keymap.Defaults())
	for _, w := range []int{79, 60, 50} {
		lines := bar(listPendingItems(km), w, 2)
		for i, l := range lines {
			if strings.Contains(l, tr("tui.help.row_this")) || strings.Contains(l, tr("tui.help.row_always")) {
				t.Errorf("w=%d: line %d shows a label: %q", w, i, l)
			}
			if cells(l) > w {
				t.Errorf("w=%d: line %d is %d cells", w, i, cells(l))
			}
		}
		inOrder(t, lines[1], itemText(hi(allKeysBinding(km))), itemText(hi(km.Quit)))
	}
}

func TestHB3Width60(t *testing.T) {
	km := NewKeyMap(keymap.Defaults())
	lines := bar(listPendingItems(km), 60, 2)
	l1 := strings.TrimSpace(lines[0])
	if !strings.HasPrefix(l1, "▸ p") || !strings.HasSuffix(l1, "…") || cells(lines[0]) > 60 {
		t.Errorf("row 1 %q", lines[0])
	}
	want := strings.Join([]string{itemText(hi(moveBinding(km))), itemText(hi(km.AddCompany)), itemText(hi(allKeysBinding(km))), itemText(hi(km.Quit))}, "  ")
	if strings.TrimSpace(lines[1]) != want {
		t.Errorf("row 2 %q, want %q", strings.TrimSpace(lines[1]), want)
	}
}

// HB-4 (D3 as overridden by the coordinator): a row label shows only while
// every key of the row fits with it; otherwise the label goes first and every
// key stays. On the list at 80 columns the "a add account" key must be there.
func TestHB4LabelsGoBeforeKeys(t *testing.T) {
	bothLangs(t, func(t *testing.T, lang string) {
		km := NewKeyMap(keymap.Defaults())
		items := listPendingItems(km)
		items.row = items.row[:1] // a short row 1, so only row 2 decides
		row2 := []string{itemText(hi(moveBinding(km))), itemText(hi(km.AddCompany)), itemText(hi(km.AddAccount)),
			itemText(hi(allKeysBinding(km))), itemText(hi(km.Quit))}
		need := 1 + cells(strings.Join(row2, "  "))
		labelW := maxInt(cells(tr("tui.help.row_this")), cells(tr("tui.help.row_always"))) + 2
		for _, w := range []int{need, need + labelW - 2, need + labelW - 1, need + labelW, 100} {
			lines := bar(items, w, 2)
			shown := strings.Contains(lines[1], tr("tui.help.row_always"))
			if want := w >= need+labelW-1; shown != want {
				t.Errorf("%s w=%d: label shown=%v, want %v:\n%s", lang, w, shown, want, strings.Join(lines, "\n"))
			}
			if shown != strings.Contains(lines[0], tr("tui.help.row_this")) {
				t.Errorf("%s w=%d: the two labels disagree:\n%s", lang, w, strings.Join(lines, "\n"))
			}
			inOrder(t, lines[1], row2...)
			if cells(lines[0]) > w || cells(lines[1]) > w {
				t.Errorf("%s w=%d: a line is too wide", lang, w)
			}
		}
	})
}

// The coordinator's rule on the real list: at 80x24 in vi, row 2 shows
// A, a, ? and q (and the arrows), and no label.
func TestHB4ListRow2At80(t *testing.T) {
	km := NewKeyMap(keymap.Defaults())
	for _, w := range []int{80, 81, 100} {
		lines := bar(listPendingItems(km), w, 2)
		inOrder(t, lines[1], itemText(hi(moveBinding(km))), itemText(hi(km.AddCompany)), itemText(hi(km.AddAccount)),
			itemText(hi(allKeysBinding(km))), itemText(hi(km.Quit)))
	}
	lines := bar(listPendingItems(km), 80, 2)
	if strings.Contains(lines[0]+lines[1], tr("tui.help.row_always")) {
		t.Errorf("a label at 80 columns:\n%s", strings.Join(lines, "\n"))
	}
}

func TestHB5Width40(t *testing.T) {
	km := NewKeyMap(keymap.Defaults())
	lines := bar(listPendingItems(km), 40, 2)
	want := strings.Join([]string{itemText(hi(km.AddCompany)), itemText(hi(allKeysBinding(km))), itemText(hi(km.Quit))}, "  ")
	if strings.TrimSpace(lines[1]) != want || cells(lines[1]) > 40 {
		t.Errorf("row 2 %q, want %q", strings.TrimSpace(lines[1]), want)
	}
	l1 := strings.TrimSpace(lines[0])
	if !strings.HasPrefix(l1, "▸ p "+tr("tui.key.apply")) || !strings.HasSuffix(l1, "…") || cells(lines[0]) > 40 {
		t.Errorf("row 1 %q", lines[0])
	}
}

func TestHB6OneRowAt40(t *testing.T) {
	bothLangs(t, func(t *testing.T, lang string) {
		km := NewKeyMap(keymap.Defaults())
		lines := bar(listPendingItems(km), 40, 1)
		if len(lines) != 1 || cells(lines[0]) > 40 {
			t.Fatalf("%s: %q", lang, lines)
		}
		l := lines[0]
		inOrder(t, l, "▸ p", "? ", "q ")
		if !strings.HasSuffix(strings.TrimRight(l, " "), "q "+tr("tui.key.quit")) {
			t.Errorf("%s: q is not last: %q", lang, l)
		}
	})
}

func TestHB7OneRowOrder(t *testing.T) {
	km := NewKeyMap(keymap.Defaults())
	lines := bar(listPendingItems(km), 160, 1)
	l := lines[0]
	inOrder(t, l, itemText(hp(km.Apply)), itemText(hi(km.AddCompany)), itemText(hi(km.Edit)), itemText(hi(km.Delete)),
		itemText(hi(km.Check)), itemText(hi(km.Clone)), itemText(hi(km.GenKey)), itemText(hi(moveBinding(km))),
		itemText(hi(km.AddAccount)), itemText(hi(allKeysBinding(km))), itemText(hi(km.Quit)))
	if strings.Contains(l, "…") {
		t.Errorf("nothing may be dropped at 160 columns: %q", l)
	}
}

func TestHB8MandatoryOverflow(t *testing.T) {
	km := NewKeyMap(keymap.Defaults())
	long := withDesc(km.Apply, "p", strings.Repeat("dai ", 15))
	items := helpBarItems{
		row:  []helpItem{hp(long)},
		more: hptr(allKeysBinding(km)),
		exit: hptr(withDesc(km.Back, "esc", tr("tui.key.back"))),
	}
	for _, rows := range []int{2, 1} {
		lines := bar(items, 30, rows)
		if len(lines) != rows {
			t.Fatalf("rows=%d: %d lines", rows, len(lines))
		}
		for _, l := range lines {
			if cells(l) > 30 {
				t.Errorf("rows=%d: %d cells: %q", rows, cells(l), l)
			}
		}
	}
	one := bar(items, 30, 1)[0]
	if !strings.Contains(one, "▸ p") || !strings.Contains(one, "? ") {
		t.Errorf("one row lost the start of the primary label or the ?: %q", one)
	}
}

// ---- model-level partition (stub screen, no harness) ----

type barStub struct {
	ctx       []helpItem
	always    []helpItem
	capturing bool
}

func (s *barStub) Init() tea.Cmd                    { return nil }
func (s *barStub) Update(tea.Msg) (Screen, tea.Cmd) { return s, nil }
func (s *barStub) View() string                     { return "" }
func (s *barStub) Title() string                    { return "stub" }
func (s *barStub) Help() []key.Binding              { return nil }
func (s *barStub) Resize(int, int)                  {}
func (s *barStub) ContextHelp() []helpItem          { return s.ctx }
func (s *barStub) Capturing() bool                  { return s.capturing }
func (s *barStub) AlwaysHelp() []helpItem           { return s.always }

// stubModel is a root model over stub screens: depth screens deep.
func stubModel(km *KeyMap, depth int, top *barStub) *Model {
	m := &Model{sh: &shared{km: km, st: NewStyles()}}
	for i := 1; i < depth; i++ {
		m.stack = append(m.stack, &barStub{})
	}
	m.stack = append(m.stack, top)
	return m
}

func keysOfItems(its []helpItem) []string {
	var out []string
	for _, it := range its {
		out = append(out, it.b.Help().Key)
	}
	return out
}

func TestHB9Dedupe(t *testing.T) {
	km := NewKeyMap(keymap.Defaults())
	always := []helpItem{hprio(km.AddCompany, 3), hprio(km.AddAccount, 1), {b: km.Quit, role: roleExit}}
	// the primary item stays in row 1 and leaves row 2
	m := stubModel(km, 1, &barStub{ctx: []helpItem{hp(km.AddAccount), hi(relabel(km.Edit, tr("tui.key.edit_company"))), hi(km.Delete)}, always: always})
	b := m.helpBarItems()
	if got := keysOfItems(b.row); len(got) == 0 || got[0] != "a" {
		t.Errorf("row 1 %v", got)
	}
	for _, k := range keysOfItems(b.always) {
		if k == "a" {
			t.Errorf("row 2 still has a: %v", keysOfItems(b.always))
		}
	}
	// a non-primary duplicate leaves row 1 and stays in row 2
	m = stubModel(km, 1, &barStub{ctx: []helpItem{hi(km.AddAccount), hi(km.Edit), hi(km.Delete)}, always: always})
	b = m.helpBarItems()
	for _, k := range keysOfItems(b.row) {
		if k == "a" {
			t.Errorf("row 1 still has a: %v", keysOfItems(b.row))
		}
	}
	if !strings.Contains(strings.Join(keysOfItems(b.always), " "), "a") {
		t.Errorf("row 2 lost a: %v", keysOfItems(b.always))
	}
}

func TestHB10IsExit(t *testing.T) {
	km := NewKeyMap(keymap.Defaults())
	for name, b := range map[string]key.Binding{
		"back": km.Back, "back esc": withDesc(km.Back, "esc", "x"), "cancel": km.Cancel, "quit": km.Quit,
		"no n/esc": withDesc(km.No, "n/esc", "x"),
	} {
		if !isExit(km, b) {
			t.Errorf("%s must be an exit", name)
		}
	}
	for name, b := range map[string]key.Binding{"yes": km.Yes, "enter": km.Enter, "toggle": km.Toggle} {
		if isExit(km, b) {
			t.Errorf("%s must not be an exit", name)
		}
	}
}

func TestHB11IsNav(t *testing.T) {
	km := NewKeyMap(keymap.Defaults())
	page := key.NewBinding(key.WithKeys(append(append([]string{}, km.PageUp.Keys()...), km.PageDown.Keys()...)...), key.WithHelp("pgup/pgdn", "x"))
	for name, b := range map[string]key.Binding{
		"up": km.Up, "down": km.Down, "up/down": withDesc(km.Up, "↑/↓", "x"), "page": page,
		"home/end": key.NewBinding(key.WithKeys("home", "end"), key.WithHelp("home/end", "x")),
		"group":    key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", "x")),
	} {
		if !isNav(b) {
			t.Errorf("%s must be navigation", name)
		}
	}
	for name, b := range map[string]key.Binding{"toggle": km.Toggle, "enter": km.Enter, "filter": km.Filter} {
		if isNav(b) {
			t.Errorf("%s must not be navigation", name)
		}
	}
}

func TestHB12Partition(t *testing.T) {
	km := NewKeyMap(keymap.Defaults())
	ctx := []helpItem{hp(km.Yes), hi(withDesc(km.No, "n/esc", tr("tui.key.cancel"))), hi(withDesc(km.Up, "↑/↓", tr("tui.key.scroll")))}
	b := stubModel(km, 2, &barStub{ctx: ctx}).helpBarItems()
	if got := keysOfItems(b.row); len(got) != 1 || got[0] != "y" {
		t.Errorf("row %v", got)
	}
	if len(b.always) != 1 || b.always[0].b.Help().Key != "↑/↓" || b.always[0].prio != 2 {
		t.Errorf("always %+v", keysOfItems(b.always))
	}
	if b.exit == nil || b.exit.b.Help().Key != "n/esc" || b.more == nil {
		t.Errorf("exit %v more %v", b.exit, b.more)
	}
}

func TestHB13SynthesizedExit(t *testing.T) {
	km := NewKeyMap(keymap.Defaults())
	ctx := []helpItem{hi(km.Retry)}
	b := stubModel(km, 2, &barStub{ctx: ctx}).helpBarItems()
	if b.exit == nil || b.exit.b.Help().Key != "esc" || b.exit.b.Help().Desc != tr("tui.key.back") {
		t.Errorf("exit %+v", b.exit)
	}
	if b := stubModel(km, 1, &barStub{ctx: ctx}).helpBarItems(); b.exit != nil {
		t.Errorf("the root screen has no esc: %+v", b.exit)
	}
	if b := stubModel(km, 2, &barStub{ctx: ctx, capturing: true}).helpBarItems(); b.exit != nil || b.more != nil {
		t.Errorf("a capturing screen: exit %v more %v", b.exit, b.more)
	}
}

func TestHB14RowLabel(t *testing.T) {
	h := newHarness(t, basicCfg())
	if got := h.m.helpBarItems().rowLabel; got != "tui.help.row_this" {
		t.Errorf("list: %q", got)
	}
	h.act(keymap.Clone)
	r := reposOf(h)
	for tab, want := range map[reposTab]string{tabLocal: "tui.help.row_this", tabURL: "tui.help.row_step", tabOnline: "tui.help.row_step"} {
		r.tab = tab
		if got := h.m.helpBarItems().rowLabel; got != want {
			t.Errorf("repos tab %d: %q, want %q", tab, got, want)
		}
	}
	h.m.shutdown()
	c := readyHarness(t)
	if got := c.m.helpBarItems().rowLabel; got != "tui.help.row_step" {
		t.Errorf("confirm: %q", got)
	}
}

// HB-15: whatever the width, no line is wider than the terminal, the line
// count is the row count, and once the mandatory items fit they are there.
func TestHB15WidthProperty(t *testing.T) {
	bothLangs(t, func(t *testing.T, lang string) {
		km := NewKeyMap(keymap.Defaults())
		sk := sentinelKM(t)
		fixtures := map[string]helpBarItems{
			"list": listPendingItems(km), "form": formItems(km), "discard": discardItems(km),
			"overlay": overlayItems(km), "sentinel list": listPendingItems(sk),
		}
		for name, items := range fixtures {
			for rows := 1; rows <= 2; rows++ {
				var tail []barEnt
				if items.more != nil {
					tail = append(tail, barEnt{it: *items.more})
				}
				if items.exit != nil {
					tail = append(tail, barEnt{it: *items.exit})
				}
				first := 0
				if len(items.row) > 0 {
					first = cells(items.row[0].b.Help().Key) + 6 // "▸ k …" and the mark
				}
				mandatory := 1 + entsWidth(tail)
				if rows == 1 {
					mandatory += first + 2 + 1 + 2 // the start of the primary, "…" and the separators
				}
				for w := 20; w <= 140; w++ {
					lines := bar(items, w, rows)
					if len(lines) != rows {
						t.Fatalf("%s %s w=%d rows=%d: %d lines", lang, name, w, rows, len(lines))
					}
					for _, l := range lines {
						if cells(l) > w {
							t.Fatalf("%s %s w=%d rows=%d: %d cells: %q", lang, name, w, rows, cells(l), l)
						}
					}
					if w < mandatory {
						continue
					}
					last := lines[len(lines)-1]
					if items.more != nil && !strings.Contains(last, itemText(*items.more)) {
						t.Errorf("%s %s w=%d rows=%d: no ? in %q", lang, name, w, rows, last)
					}
					if items.exit != nil && !strings.Contains(last, itemText(*items.exit)) {
						t.Errorf("%s %s w=%d rows=%d: no exit in %q", lang, name, w, rows, last)
					}
				}
			}
		}
	})
}

func TestHB16EmptyRow1(t *testing.T) {
	km := NewKeyMap(keymap.Defaults())
	items := helpBarItems{
		always: []helpItem{hprio(km.Toggle, 3)}, more: hptr(allKeysBinding(km)), exit: hptr(km.Back),
		rowLabel: "tui.help.row_this",
	}
	lines := bar(items, 100, 2)
	if len(lines) != 2 || strings.TrimSpace(lines[0]) != "" {
		t.Errorf("row 1 is not empty: %q", lines)
	}
	if !strings.Contains(lines[1], itemText(hi(km.Toggle))) {
		t.Errorf("row 2 %q", lines[1])
	}
	one := bar(items, 100, 1)
	if len(one) != 1 || strings.HasPrefix(one[0], "  ") {
		t.Errorf("one row: %q", one)
	}
}

// ---- FOC-5 ----

func TestFOC5ActivePanelHasTheSameFrame(t *testing.T) {
	st := NewStyles()
	if st.PanelActive.GetHorizontalFrameSize() != st.Panel.GetHorizontalFrameSize() ||
		st.PanelActive.GetVerticalFrameSize() != st.Panel.GetVerticalFrameSize() {
		t.Fatalf("frames differ: %d,%d vs %d,%d", st.PanelActive.GetHorizontalFrameSize(), st.PanelActive.GetVerticalFrameSize(),
			st.Panel.GetHorizontalFrameSize(), st.Panel.GetVerticalFrameSize())
	}
}

// ---- LAY: layout matrix ----

var laySizes = [][2]int{{40, 10}, {60, 20}, {80, 24}, {100, 30}, {120, 40}}

type layState struct {
	name  string
	build func(t *testing.T) *harness
	// pane is "heavy" for the list screen (heavy border), "single" otherwise
	heavy     bool
	capturing bool
	overlay   bool
}

func layStates() []layState {
	return []layState{
		{name: "list welcome", heavy: true, build: func(t *testing.T) *harness { return harnessFor(t, nil, nil) }},
		{name: "list pending", heavy: true, build: func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.selectKey("acme/alice")
			return h
		}},
		{name: "list company without accounts", heavy: true, build: func(t *testing.T) *harness { return newHarness(t, emptyCompanyCfg("personal")) }},
		{name: "detail", build: func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.resize(50, 24)
			openDetail(h, "acme/carol")
			return h
		}},
		{name: "confirm ready", build: func(t *testing.T) *harness { return readyHarness(t) }},
		{name: "repos local", build: func(t *testing.T) *harness {
			h, _ := localHarness(t)
			openLocal(h)
			return h
		}},
		{name: "company form", capturing: true, build: func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.act(keymap.AddCompany)
			return h
		}},
		{name: "overlay", overlay: true, build: func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.selectKey("acme/alice")
			h.key("?")
			return h
		}},
	}
}

const heavyChars = "┏┓┗┛━"

// LAY-1: every state at every reference size, in both languages.
func TestLAY1LayoutMatrix(t *testing.T) {
	bothLangs(t, func(t *testing.T, lang string) {
		for _, st := range layStates() {
			h := st.build(t)
			for _, sz := range laySizes {
				w, hh := sz[0], sz[1]
				h.resize(w, hh)
				label := lang + " " + st.name
				v := h.view()
				assertFits(t, v, w, hh, label)
				lines := strings.Split(v, "\n")
				if len(lines) != hh {
					t.Errorf("%s %dx%d: %d lines", label, w, hh, len(lines))
				}
				wantRows := 1
				if hh >= 14 {
					wantRows = 2
				}
				if h.m.helpRowCount() != wantRows {
					t.Errorf("%s %dx%d: %d help rows, want %d", label, w, hh, h.m.helpRowCount(), wantRows)
				}
				if _, bh := h.m.bodySize(); bh != hh-h.m.chromeRows() {
					t.Errorf("%s %dx%d: body %d", label, w, hh, bh)
				}
				last := lines[len(lines)-1]
				items := h.m.helpBarItems()
				switch {
				case st.capturing:
					if strings.Contains(last, tr("tui.key.all_keys")) {
						t.Errorf("%s %dx%d: a capturing screen offers ?: %q", label, w, hh, last)
					}
					if want := "esc " + tr("tui.key.cancel"); !strings.Contains(last, want) {
						t.Errorf("%s %dx%d: no %q in %q", label, w, hh, want, last)
					}
				default:
					if items.exit == nil {
						t.Errorf("%s %dx%d: no exit item", label, w, hh)
					} else if want := itemText(*items.exit); !strings.Contains(last, want) {
						t.Errorf("%s %dx%d: no %q in %q", label, w, hh, want, last)
					}
					if !st.overlay {
						if want := h.m.sh.km.keys.Label(keymap.Help) + " " + tr("tui.key.all_keys"); !strings.Contains(last, want) {
							t.Errorf("%s %dx%d: no %q in %q", label, w, hh, want, last)
						}
					}
				}
				// FOC-1/FOC-4: only the list pane has the heavy border
				for _, c := range heavyChars {
					if has := strings.ContainsRune(v, c); has != st.heavy {
						t.Errorf("%s %dx%d: heavy border char %q present=%v, want %v", label, w, hh, string(c), has, st.heavy)
					}
				}
			}
		}
	})
}

// AC1 spot check: the list at 80x24 and 100x30 in Vietnamese.
func TestLAY1AC1Spot(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.selectKey("acme/alice")
	row2 := func(withA bool) string {
		km := defKM
		parts := []string{"↑↓ " + tr("tui.key.move"), "A " + tr("tui.key.add_company")}
		if withA {
			parts = append(parts, km.AddAccount.Help().Key+" "+tr("tui.key.add_account"))
		}
		parts = append(parts, "? "+tr("tui.key.all_keys"), "q "+tr("tui.key.quit"))
		return strings.Join(parts, "  ")
	}
	for _, sz := range [][2]int{{80, 24}, {100, 30}} {
		h.resize(sz[0], sz[1])
		rows := h.helpRows()
		if len(rows) != 2 {
			t.Fatalf("%v: %d rows", sz, len(rows))
		}
		if !strings.HasPrefix(rows[0], "▸ p "+tr("tui.key.apply")) {
			t.Errorf("%v: row 1 %q", sz, rows[0])
		}
		// the coordinator's rule: A, a, ? and q are all visible at 80 columns
		if rows[1] != row2(true) {
			t.Errorf("%v: row 2 %q, want %q", sz, rows[1], row2(true))
		}
	}
	// 80 columns: no label, because not every key of the row fits with it
	h.resize(80, 24)
	if v := h.view(); strings.Contains(v, tr("tui.help.row_this")) || strings.Contains(v, tr("tui.help.row_always")) {
		t.Errorf("labels at 80 columns:\n%s", v)
	}
	// 100 columns: both rows fit with their labels
	h.resize(100, 30)
	if v := h.view(); !strings.Contains(v, tr("tui.help.row_this")) || !strings.Contains(v, tr("tui.help.row_always")) {
		t.Errorf("no labels at 100 columns:\n%s", v)
	}
}

// The override of D3: on the list at 80x24 in vi, row 2 shows A, a, ? and q,
// for every state of the list that offers them.
func TestLAYRow2At80x24Vi(t *testing.T) {
	i18n.SetLang("vi")
	for name, h := range map[string]*harness{
		"pending": func() *harness { h := newHarness(t, basicCfg()); h.selectKey("acme/alice"); return h }(),
		"company": newHarness(t, basicCfg()),
	} {
		h.resize(80, 24)
		rows := h.helpRows()
		if len(rows) != 2 {
			t.Fatalf("%s: %d rows", name, len(rows))
		}
		for _, want := range []string{
			"A " + defKM.T("tui.key.add_company"), "a " + defKM.T("tui.key.add_account"),
			"? " + tr("tui.key.all_keys"), "q " + tr("tui.key.quit"),
		} {
			if !strings.Contains(rows[1], want) {
				t.Errorf("%s: row 2 %q lacks %q", name, rows[1], want)
			}
		}
	}
}

// A company without account has "a add account" as its next step: it stays
// in row 1 and leaves row 2 (FR4), so it is on the bar once.
func TestLAYNoAccountCompanyShowsAOnce(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.resize(80, 24)
	rows := h.helpRows()
	want := "a " + defKM.T("tui.key.add_account")
	if !strings.HasPrefix(rows[0], "▸ "+want) || strings.Contains(rows[1], want) {
		t.Errorf("rows %q", rows)
	}
	for _, w := range []string{"A " + defKM.T("tui.key.add_company"), "? " + tr("tui.key.all_keys"), "q " + tr("tui.key.quit")} {
		if !strings.Contains(rows[1], w) {
			t.Errorf("row 2 %q lacks %q", rows[1], w)
		}
	}
}

func TestLAY2OneOrTwoHelpRows(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.selectKey("acme/alice")
	for _, c := range []struct{ h, rows int }{{13, 1}, {14, 2}} {
		h.resize(80, c.h)
		if h.m.helpRowCount() != c.rows {
			t.Errorf("80x%d: %d help rows, want %d", c.h, h.m.helpRowCount(), c.rows)
		}
		if _, bh := h.m.bodySize(); bh != 10 {
			t.Errorf("80x%d: body %d, want 10", c.h, bh)
		}
		if n := len(strings.Split(h.view(), "\n")); n != c.h {
			t.Errorf("80x%d: %d lines", c.h, n)
		}
	}
}

func TestLAY3LayoutTiers(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.selectKey("acme/alice")
	for _, c := range []struct {
		w, h int
		want layoutMode
	}{{60, 20, layoutList}, {80, 24, layoutStacked}, {100, 30, layoutSide}, {120, 40, layoutSide}} {
		h.resize(c.w, c.h)
		if got := h.listScreen().layout(); got != c.want {
			t.Errorf("%dx%d: layout %v, want %v", c.w, c.h, got, c.want)
		}
	}
	h.resize(80, 24)
	h.mustContainPlain(defKM.T("tui.detail.how_pending"))
}

func TestLAY4ReposDetailAt60x20(t *testing.T) {
	h, _ := localHarness(t)
	r := openLocal(h)
	h.resize(60, 20)
	if g := r.geom(); g.detail != 6 || g.listH < 6 {
		t.Errorf("60x20: detail %d, list %d rows", g.detail, g.listH)
	}
}

func TestLAY5FormsKeepTheirTier(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.act(keymap.AddAccount)
	f := accountFormTop(t, h)
	for _, c := range []struct {
		w, h    int
		compact bool
	}{{80, 24, false}, {100, 30, false}, {60, 20, true}} {
		h.resize(c.w, c.h)
		if got := f.compact(); got != c.compact {
			t.Errorf("%dx%d: compact=%v, want %v", c.w, c.h, got, c.compact)
		}
		if c.h >= 20 && f.formRows() < 4 {
			t.Errorf("%dx%d: %d form rows", c.w, c.h, f.formRows())
		}
	}
}

func TestLAY6StatusAndHelpRows(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.selectKey("acme/alice")
	h.resize(80, 24)
	if got := h.statusLine(); !strings.Contains(got, strings.TrimSpace(h.m.syncBadge())) {
		t.Errorf("status line %q lacks the badge %q", got, h.m.syncBadge())
	}
	if rows := h.helpRows(); len(rows) != 2 || rows[0] == "" || rows[1] == "" {
		t.Errorf("help rows %q", rows)
	}
}

// ---- FOC: focus indicator ----

func TestFOC1HeavyBorderOnTheListPane(t *testing.T) {
	h := newHarness(t, basicCfg())
	for _, c := range []struct {
		w, h   int
		detail bool
	}{{100, 30, true}, {80, 24, true}, {59, 30, false}} {
		h.resize(c.w, c.h)
		v := h.view()
		for _, ch := range heavyChars {
			if !strings.ContainsRune(v, ch) {
				t.Errorf("%dx%d: no %q:\n%s", c.w, c.h, string(ch), v)
			}
		}
		if has := strings.Contains(v, "╭") && strings.Contains(v, "╰"); has != c.detail {
			t.Errorf("%dx%d: rounded detail pane present=%v, want %v", c.w, c.h, has, c.detail)
		}
	}
	w := newHarness(t, nil)
	w.resize(80, 24)
	for _, ch := range heavyChars {
		if !strings.ContainsRune(w.view(), ch) {
			t.Errorf("welcome: no %q", string(ch))
		}
	}
}

func TestFOC2TitleHint(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.mustContain("(" + tr("tui.list.panel_hint") + ")")
}

func TestFOC3HintAbsent(t *testing.T) {
	hint := tr("tui.list.panel_hint")
	absent := func(name string, h *harness) {
		t.Helper()
		v := h.view()
		if strings.Contains(v, hint) || strings.Contains(v, "(↑↓") {
			t.Errorf("%s: the hint is shown:\n%s", name, v)
		}
	}
	absent("one row", newHarness(t, emptyCompanyCfg("personal")))
	absent("welcome", newHarness(t, nil))
	// a pane too narrow for the whole hint: the hint is left out, never cut
	defer i18n.SetLang("vi")
	i18n.SetLang("en")
	hint = tr("tui.list.panel_hint")
	h := newHarness(t, basicCfg())
	h.resize(40, 20)
	absent("narrow", h)
}

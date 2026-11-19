package tui

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
	"golang.org/x/text/unicode/norm"
)

// The "?" overlay is a searchable command list. Typing edits a filter, the
// arrow keys move a selection over the runnable rows of the screen section and
// enter runs the selected row on the screen underneath, through the same path
// as a real key press.

// overlayRow is one row of the overlay.
type overlayRow struct {
	b        key.Binding
	keyText  string // m.overlayKey(b)
	desc     string
	runnable bool // can be selected and run (screen rows except navigation)
	global   bool // belongs to the global section
}

// id identifies a row across re-renders: its first key and its description.
func (r overlayRow) id() string {
	k := ""
	if ks := r.b.Keys(); len(ks) > 0 {
		k = ks[0]
	}
	return k + "\x00" + r.desc
}

// helpOverlay is the state of the overlay, kept in the root model.
type helpOverlay struct {
	filter textinput.Model
	// sel is the id of the selected row; "" (or a row that is gone) means the
	// first runnable row.
	sel string
}

func newOverlayFilter(st *Styles) textinput.Model {
	fi := textinput.New()
	fi.Prompt = "> "
	fi.PromptStyle = st.Accent
	fi.PlaceholderStyle = st.Faint
	fi.Cursor.Style = st.Accent
	fi.CharLimit = 40
	return fi
}

// openOverlay shows the overlay with an empty filter. The selection starts on
// the row of the help bar's next step when the overlay lists it, else on the
// first runnable row.
func (m *Model) openOverlay() tea.Cmd {
	var prim *helpItem
	for _, it := range m.helpBarItems().row {
		if it.primary {
			it := it
			prim = &it
			break
		}
	}
	m.ov.filter.SetValue("")
	m.ov.filter.Placeholder = tr("tui.help.filter_hint")
	m.ov.sel = ""
	m.showHelp = true
	if prim != nil {
		rows, _ := m.overlayRows()
		pk := ""
		if ks := prim.b.Keys(); len(ks) > 0 {
			pk = ks[0]
		}
		pick := func(match func(overlayRow) bool) bool {
			for _, r := range rows {
				if r.runnable && match(r) {
					m.ov.sel = r.id()
					return true
				}
			}
			return false
		}
		_ = pick(func(r overlayRow) bool { ks := r.b.Keys(); return pk != "" && ks[0] == pk }) ||
			pick(func(r overlayRow) bool { return r.desc == prim.b.Help().Desc })
	}
	return m.ov.filter.Focus()
}

func (m *Model) closeOverlay() {
	m.showHelp = false
	m.ov.filter.SetValue("")
	m.ov.filter.Blur()
	m.ov.sel = ""
}

// overlayAllRows are the rows before the filter.
func (m *Model) overlayAllRows() (screen, global []overlayRow) {
	for _, b := range m.top().Help() {
		if b.Enabled() {
			screen = append(screen, overlayRow{b: b, keyText: m.overlayKey(b), desc: b.Help().Desc, runnable: !isNav(b)})
		}
	}
	for _, b := range m.globalHelp() {
		if b.Enabled() {
			global = append(global, overlayRow{b: b, keyText: m.overlayKey(b), desc: b.Help().Desc, global: true})
		}
	}
	return screen, global
}

// overlayRows are the rows that match the filter.
func (m *Model) overlayRows() (screen, global []overlayRow) {
	screen, global = m.overlayAllRows()
	term := strings.TrimSpace(m.ov.filter.Value())
	if term == "" {
		return screen, global
	}
	keep := func(rows []overlayRow) []overlayRow {
		var out []overlayRow
		for _, r := range rows {
			if overlayMatch(term, r) {
				out = append(out, r)
			}
		}
		return out
	}
	return keep(screen), keep(global)
}

// overlaySelected is the selected row among screen (the visible screen rows):
// the remembered one, else the first runnable row.
func (m *Model) overlaySelected(screen []overlayRow) (overlayRow, bool) {
	first := -1
	for i, r := range screen {
		if !r.runnable {
			continue
		}
		if first < 0 {
			first = i
		}
		if r.id() == m.ov.sel {
			return r, true
		}
	}
	if first >= 0 {
		return screen[first], true
	}
	return overlayRow{}, false
}

// moveOverlay moves the selection d runnable rows, clamped at both ends.
func (m *Model) moveOverlay(d int) {
	screen, _ := m.overlayRows()
	var run []overlayRow
	for _, r := range screen {
		if r.runnable {
			run = append(run, r)
		}
	}
	if len(run) == 0 {
		return
	}
	cur, _ := m.overlaySelected(screen)
	at := 0
	for i, r := range run {
		if r.id() == cur.id() {
			at = i
		}
	}
	m.ov.sel = run[clamp(at+d, 0, len(run)-1)].id()
}

// overlayBarItems are the help bar items while the overlay is open.
func (m *Model) overlayBarItems() helpBarItems {
	km := m.sh.km
	screen, _ := m.overlayRows()
	var out helpBarItems
	if _, ok := m.overlaySelected(screen); ok {
		out.row = append(out.row, helpItem{b: withDesc(km.Enter, "enter", tr("tui.key.run")), primary: true})
	}
	if len(out.row) > 0 {
		// nothing to select when no row matches
		out.row = append(out.row, helpItem{b: withDesc(km.Up, "↑↓", tr("tui.key.select"))})
	}
	desc := tr("tui.key.close")
	if m.ov.filter.Value() != "" {
		desc = tr("tui.key.filter_clear")
	}
	out.exit = &helpItem{b: withDesc(km.Back, "esc", desc)}
	out.rowLabel = "tui.help.row_step"
	return out
}

// typesRune reports whether k is a plain character the filter would take.
func typesRune(k tea.KeyMsg) bool {
	return k.Type == tea.KeyRunes && !k.Alt && len(k.Runes) > 0
}

// overlayUpdate handles a key while the overlay is open. ctrl+c was handled
// by the caller.
func (m *Model) overlayUpdate(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	km := m.sh.km
	empty := m.ov.filter.Value() == ""
	switch {
	case key.Matches(k, km.Help), key.Matches(k, km.Palette):
		m.closeOverlay()
		return m, nil
	case k.Type == tea.KeyEsc:
		if !empty {
			m.ov.filter.SetValue("")
			m.ov.sel = ""
			return m, nil
		}
		m.closeOverlay()
		return m, nil
	case key.Matches(k, km.Back), key.Matches(k, km.Quit):
		// a back key that is a plain character is text once there is a filter
		if empty || !typesRune(k) {
			m.closeOverlay()
			return m, nil
		}
	case key.Matches(k, km.Up):
		m.moveOverlay(-1)
		return m, nil
	case key.Matches(k, km.Down):
		m.moveOverlay(1)
		return m, nil
	case k.Type == tea.KeyEnter:
		screen, _ := m.overlayRows()
		if r, ok := m.overlaySelected(screen); ok {
			return m.runOverlayRow(r)
		}
		return m, nil
	}
	prev := m.ov.filter.Value()
	var cmd tea.Cmd
	m.ov.filter, cmd = m.ov.filter.Update(k)
	if m.ov.filter.Value() != prev {
		m.ov.sel = ""
	}
	return m, cmd
}

// runOverlayRow closes the overlay and presses the row's key on the screen
// underneath, exactly as a real key press would arrive.
func (m *Model) runOverlayRow(r overlayRow) (tea.Model, tea.Cmd) {
	ks := r.b.Keys()
	m.closeOverlay()
	if len(ks) == 0 {
		return m, nil
	}
	msg, ok := keyMsgFor(ks[0])
	if !ok {
		return m, nil
	}
	return m.update(msg)
}

var namedKeys = map[string]tea.KeyType{
	"enter": tea.KeyEnter, "esc": tea.KeyEsc, "tab": tea.KeyTab, "shift+tab": tea.KeyShiftTab,
	"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
	"home": tea.KeyHome, "end": tea.KeyEnd, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
	"backspace": tea.KeyBackspace, "delete": tea.KeyDelete,
}

// keyMsgFor is the inverse of tea.KeyMsg.String() for every key gitid binds:
// named keys, ctrl+<letter>, alt+<character>, f1..f12 and one printable
// character. ok is false for anything else (and for a name whose message would
// read back as another key, such as ctrl+i).
func keyMsgFor(s string) (msg tea.KeyMsg, ok bool) {
	switch {
	case s == "":
		return msg, false
	case namedKeys[s] != 0:
		msg = tea.KeyMsg{Type: namedKeys[s]}
	case strings.HasPrefix(s, "ctrl+") && len(s) == 6 && s[5] >= 'a' && s[5] <= 'z':
		msg = tea.KeyMsg{Type: tea.KeyCtrlA + tea.KeyType(s[5]-'a')}
	case strings.HasPrefix(s, "alt+") && utf8.RuneCountInString(s) == 5:
		r, _ := utf8.DecodeRuneInString(s[4:])
		if !unicode.IsPrint(r) {
			return msg, false
		}
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: true}
	case len(s) >= 2 && len(s) <= 3 && s[0] == 'f' && s[1] >= '1' && s[1] <= '9':
		n := int(s[1] - '0')
		if len(s) == 3 {
			if s[2] < '0' || s[2] > '9' {
				return msg, false
			}
			n = n*10 + int(s[2]-'0')
		}
		if n < 1 || n > 12 {
			return msg, false
		}
		// the function keys count downwards from KeyF1
		msg = tea.KeyMsg{Type: tea.KeyF1 - tea.KeyType(n-1)}
	case utf8.RuneCountInString(s) == 1:
		r, _ := utf8.DecodeRuneInString(s)
		if !unicode.IsPrint(r) {
			return msg, false
		}
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
	default:
		return msg, false
	}
	if msg.String() != s {
		return tea.KeyMsg{}, false
	}
	return msg, true
}

// foldText lower-cases s and removes the diacritics, so "Công Ty" and
// "cong ty" compare equal; đ becomes d.
func foldText(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if r == 'đ' {
			r = 'd'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// keyParts are the folded keys of a key text: "esc/q" gives esc and q.
func keyParts(keyText string) []string {
	var out []string
	for _, p := range strings.Split(keyText, "/") {
		if p != "" {
			out = append(out, foldText(p))
		}
	}
	if len(out) == 0 {
		out = []string{foldText(keyText)}
	}
	return out
}

// overlayMatch reports whether a row matches the filter term. Every word of
// the term must match. A one-character word matches a row whose key is that
// character or that has a description word starting with it; a longer word
// matches a key it appears in, or fuzzily inside one description word (so
// "cty" does not match "công ty").
func overlayMatch(term string, r overlayRow) bool {
	words := strings.Fields(foldText(term))
	if len(words) == 0 {
		return true
	}
	toks := strings.Fields(foldText(r.desc))
	keys := keyParts(r.keyText)
	whole := foldText(r.keyText)
	for _, w := range words {
		if utf8.RuneCountInString(w) == 1 {
			if slices.Contains(keys, w) || slices.ContainsFunc(toks, func(t string) bool { return strings.HasPrefix(t, w) }) {
				continue
			}
			return false
		}
		if strings.Contains(whole, w) || len(fuzzy.Find(w, toks)) > 0 {
			continue
		}
		return false
	}
	return true
}

// helpView lists the keys of the current screen, then the global keys, under a
// filter line. When the bindings do not fit the height they go in two or three
// columns, and as a last resort the blank separator lines go. When even that
// is not enough (a very short terminal) the overlay shows the filter and a
// one-column window of the screen rows that always holds the selection.
func (m *Model) helpView(w, h int) string {
	st := m.sh.st
	screen, global := m.overlayRows()
	sel, hasSel := m.overlaySelected(screen)
	selID := ""
	if hasSel {
		selID = sel.id()
	}
	allScreen, allGlobal := m.overlayAllRows()
	kw := 10 // key column: the widest key text plus one space, so it never touches its description
	for _, l := range [][]overlayRow{allScreen, allGlobal} {
		for _, r := range l {
			kw = maxInt(kw, minInt(lipgloss.Width(r.keyText)+1, 18))
		}
	}
	// colKW is the key column of each of n columns of a list: the same width
	// everywhere, unless tight, which gives each column the room of its own keys
	// (what keeps the descriptions readable in a narrow terminal)
	tight := false
	perCol := func(n, cols int) int { return (n + cols - 1) / cols }
	colKW := func(list []overlayRow, n int) []int {
		out := make([]int, n)
		rows := perCol(len(list), n)
		for c := range out {
			out[c] = kw
			if !tight {
				continue
			}
			out[c] = 2
			for r := 0; r < rows; r++ {
				if idx := c*rows + r; idx < len(list) {
					out[c] = maxInt(out[c], minInt(lipgloss.Width(list[idx].keyText)+1, 18))
				}
			}
		}
		return out
	}
	inner := maxInt(w-4, 10)
	avail := maxInt(h-2, 1) // panel border
	m.ov.filter.Width = maxInt(inner-3, 4)

	warn := len(m.sh.keyIssues) > 0
	// fixed lines around the bindings: title, filter, the section titles, the
	// rows of the global section, the close hint, plus blank separators
	hint := true // the close hint line; it gives way before descriptions are cut
	fixed := func(compact bool, gcols int) int {
		n := 3 // title, filter, close hint
		if len(screen) > 0 {
			n++
		}
		if len(global) > 0 {
			n += 1 + perCol(len(global), gcols)
		}
		if len(screen) == 0 && len(global) == 0 {
			n++ // the "no match" line
		}
		if warn {
			n++
		}
		if !compact {
			n += 2
			if len(screen) > 0 && len(global) > 0 {
				n++
			}
		}
		if !hint {
			n--
			if !compact {
				n-- // and the blank row before it
			}
		}
		return n
	}
	// colWidths gives each of n columns the room its longest description
	// needs (cell: 2 indent + the key + the text); fits says whether all of
	// them are shown in full. When they are not, the widest columns shrink.
	colWidths := func(list []overlayRow, n int) (ws []int, fits bool) {
		kws := colKW(list, n)
		rows := perCol(len(list), n)
		ws = make([]int, n)
		total := 0
		for c := range ws {
			ws[c] = kws[c] + 3
			for r := 0; r < rows; r++ {
				if idx := c*rows + r; idx < len(list) {
					ws[c] = maxInt(ws[c], kws[c]+2+lipgloss.Width(list[idx].desc))
				}
			}
			if c < n-1 {
				ws[c] += 2 // gap before the next column
			}
			total += ws[c]
		}
		fits = total <= inner
		// shrink the widest column first, so every column keeps a few words:
		// down to its first two words, and only then below that
		floor := make([]int, n)
		for c := range floor {
			floor[c] = kws[c] + 3
			for r := 0; r < rows; r++ {
				if idx := c*rows + r; idx < len(list) {
					floor[c] = maxInt(floor[c], kws[c]+2+twoWords(list[idx].desc))
				}
			}
			if c < n-1 {
				floor[c] += 2
			}
			floor[c] = minInt(floor[c], ws[c])
		}
		for _, lim := range [][]int{floor, nil} {
			for total > inner {
				widest := -1
				for c := range ws {
					low := kws[c] + 3
					if lim != nil {
						low = lim[c]
					}
					if ws[c] > low && (widest < 0 || ws[c] >= ws[widest]) {
						widest = c
					}
				}
				if widest < 0 {
					break
				}
				ws[widest]--
				total--
			}
		}
		return ws, fits
	}
	roomy := func(n, g int) bool {
		_, a := colWidths(screen, n)
		_, b := colWidths(global, g)
		return a && b
	}
	// readable says whether, with n columns, every cut description keeps at
	// least its first two words (a one-word description is never cut) and no two cut
	// descriptions of one section come out the same
	readable := func(list []overlayRow, n int) bool {
		kws := colKW(list, n)
		ws, _ := colWidths(list, n)
		rows := perCol(len(list), n)
		seen := map[string]bool{}
		for idx, r := range list {
			d := truncWords(r.desc, maxInt(ws[idx/rows]-(kws[idx/rows]+2), 1))
			if d == r.desc {
				continue
			}
			if len(strings.Fields(strings.TrimSuffix(d, "…"))) < 2 || seen[d] {
				return false
			}
			seen[d] = true
		}
		return true
	}
	// the screen rows go in cols columns and the global ones in gcols (at most
	// as many), whichever fewest columns fit the height
	cols, gcols, compact, found := 1, 1, false, false
	pick := func(ok func(n, g int) bool) bool {
		for _, c := range []bool{false, true} {
			for n := 1; n <= 3; n++ {
				for g := 1; g <= n; g++ {
					if !ok(n, g) {
						continue
					}
					if fixed(c, g)+perCol(len(screen), n) <= avail {
						cols, gcols, compact = n, g, c
						return true
					}
				}
			}
		}
		return false
	}
	// prefer full descriptions, then cut ones that stay readable; else the
	// fewest columns that fit the height (their text is then cut at a word,
	// see cell)
	readableCols := func(n, g int) bool { return readable(screen, n) && readable(global, g) }
	found = pick(roomy) || pick(readableCols)
	if !found {
		// narrower key columns, then no close hint, before cutting to a word
		tight = true
		found = pick(readableCols)
		if !found {
			hint = false
			found = pick(readableCols)
			hint = !found
		}
		tight = found
	}
	found = found || pick(func(int, int) bool { return true })

	cell := func(r overlayRow, cw, kw int) string {
		desc := r.desc
		if cw > 0 {
			desc = truncWords(desc, maxInt(cw-(kw+2), 1))
		}
		tail := func(s string) string {
			if cw > 0 {
				return s + spaces(cw-lipgloss.Width(s))
			}
			return s
		}
		switch {
		case hasSel && r.runnable && r.id() == selID:
			return st.HelpSelected.Render(tail("▌ " + pad(r.keyText, kw) + desc))
		}
		// the global rows read like the others: they work, only the cursor
		// does not stop on them
		return tail("  " + st.Text.Render(pad(r.keyText, kw)) + st.Muted.Render(desc))
	}
	blank := func(b *strings.Builder) {
		if !compact {
			b.WriteString("\n")
		}
	}
	// section writes a list of rows in cols columns (one: a plain list)
	section := func(b *strings.Builder, list []overlayRow, cols int) {
		if cols == 1 {
			kw := colKW(list, 1)[0]
			for _, r := range list {
				b.WriteString(cell(r, 0, kw) + "\n")
			}
			return
		}
		cws, _ := colWidths(list, cols)
		kws := colKW(list, cols)
		rows := perCol(len(list), cols)
		for r := 0; r < rows; r++ {
			var line strings.Builder
			for c := 0; c < cols; c++ {
				if idx := c*rows + r; idx < len(list) {
					line.WriteString(cell(list[idx], cws[c], kws[c]))
				}
			}
			b.WriteString(strings.TrimRight(line.String(), " ") + "\n")
		}
	}
	noMatch := func(b *strings.Builder) {
		b.WriteString(st.Muted.Render(trunc(tr("tui.help.no_match", strings.TrimSpace(m.ov.filter.Value())), inner)) + "\n")
	}
	var b strings.Builder
	b.WriteString(st.PanelTitle.Render(tr("tui.help.title")) + "\n")
	b.WriteString(m.ov.filter.View() + "\n")

	if !found {
		// a window of the screen rows around the selection; the global
		// section and the close hint give way
		n := maxInt(avail-2, 1)
		if len(screen) == 0 {
			noMatch(&b)
			return m.sh.panel("", strings.TrimRight(b.String(), "\n"), w, h, true)
		}
		at := 0
		for i, r := range screen {
			if hasSel && r.id() == selID && r.runnable {
				at = i
			}
		}
		start := clamp(at-n/2, 0, maxInt(len(screen)-n, 0))
		for _, r := range screen[start:minInt(start+n, len(screen))] {
			b.WriteString(cell(r, inner, kw) + "\n")
		}
		return m.sh.panel("", strings.TrimRight(b.String(), "\n"), w, h, true)
	}

	blank(&b)
	if len(screen) == 0 && len(global) == 0 {
		noMatch(&b)
	}
	if len(screen) > 0 {
		b.WriteString(st.Bold.Render(m.top().Title()) + "\n")
		section(&b, screen, cols)
	}
	if len(screen) > 0 && len(global) > 0 {
		blank(&b)
	}
	if len(global) > 0 {
		b.WriteString(st.Bold.Render(tr("tui.help.global")) + "\n")
		section(&b, global, gcols)
	}
	if hint {
		blank(&b)
		closeText := m.sh.km.T("tui.help.close")
		if m.ov.filter.Value() != "" {
			closeText = m.sh.km.T("tui.help.close_filter")
		}
		// the version of gitid ends the footer, when it fits
		if v := m.sh.version; v != "" && lipgloss.Width(closeText+"  ·  gitid "+v) <= inner {
			closeText += "  ·  gitid " + v
		}
		b.WriteString(st.Muted.Render(closeText))
	}
	if warn {
		b.WriteString("\n" + st.Warn.Render("! "+tr("tui.help.keys_warn", len(m.sh.keyIssues))))
	}
	return m.sh.panel("", b.String(), w, h, true)
}

// twoWords is the width of the first two words of a description with the
// ellipsis that cuts it (the whole text when it has no more than two words).
func twoWords(desc string) int {
	f := strings.Fields(desc)
	if len(f) <= 2 {
		return lipgloss.Width(desc)
	}
	return lipgloss.Width(f[0]+" "+f[1]) + 1
}

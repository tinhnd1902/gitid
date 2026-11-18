package tui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

// The two-row help bar. Row 1 holds the keys of the selected row or of the
// current step (the next step first, marked with "▸"); row 2 holds the keys
// that are always available on the screen, then "? all keys", then the exit
// key last. Below helpTwoRowMinH terminal rows the bar is a single row.
const (
	helpTwoRowMinH = 14 // from this terminal height the help bar has two rows
	prioKeep       = 1 << 30
	sepWidth       = 2 // cells between two items
)

// helpBarItems is the help bar before it is fitted to a width.
type helpBarItems struct {
	row    []helpItem // row 1, primary first
	always []helpItem // row 2 before "?" and the exit
	more   *helpItem  // "? all keys"; nil while the top screen captures keys
	exit   *helpItem  // nil when none applies
	// rowLabel is the i18n key of the label of row 1: tui.help.row_this or
	// tui.help.row_step (empty: row_step).
	rowLabel string
}

// all returns every item before fitting: both rows, "?" and the exit.
func (b helpBarItems) all() []helpItem {
	out := slices.Clone(b.row)
	out = append(out, b.always...)
	if b.more != nil {
		out = append(out, *b.more)
	}
	if b.exit != nil {
		out = append(out, *b.exit)
	}
	return out
}

func (m *Model) helpRowCount() int {
	if m.h >= helpTwoRowMinH {
		return 2
	}
	return 1
}

// chromeRows are the rows around the body: header, status bar, help bar.
func (m *Model) chromeRows() int { return 2 + m.helpRowCount() }

// isExit reports whether b leaves the screen (esc, the quit key, or an
// "x/esc" label such as the confirmation's "n/esc").
func isExit(km *KeyMap, b key.Binding) bool {
	ks := b.Keys()
	if slices.Contains(ks, "esc") || (len(ks) > 0 && slices.Equal(ks, km.Quit.Keys())) {
		return true
	}
	return strings.HasSuffix(b.Help().Key, "/esc")
}

// navKeys are the keys that move inside a screen.
var navKeys = []string{"up", "down", "ctrl+p", "ctrl+n", "pgup", "pgdown", "home", "end", "left", "right"}

// isNav reports whether b only moves the selection or the viewport.
func isNav(b key.Binding) bool {
	if h := b.Help().Key; strings.HasPrefix(h, "↑") || strings.HasPrefix(h, "←") {
		return true
	}
	ks := b.Keys()
	return len(ks) > 0 && slices.Contains(navKeys, ks[0])
}

// helpBarItems is the only producer of the help bar: it splits the screen's
// context help into row 1, row 2, "?" and the exit key.
func (m *Model) helpBarItems() helpBarItems {
	if m.showHelp {
		return m.overlayBarItems()
	}
	km := m.sh.km
	top := m.top()
	capturing := isCapturing(top)
	ctx := m.helpItems()
	var al []helpItem
	if ah, ok := top.(alwaysHelper); ok {
		for _, it := range ah.AlwaysHelp() {
			if it.b.Enabled() {
				al = append(al, it)
			}
		}
	}
	var out helpBarItems

	// the exit key: the screen's own, else the first exit-class item of row 1,
	// else esc when there is a screen to go back to
	var rest []helpItem
	for _, it := range al {
		if it.role == roleExit && out.exit == nil {
			e := it
			out.exit = &e
			continue
		}
		rest = append(rest, it)
	}
	al = rest
	if out.exit == nil {
		for i, it := range ctx {
			if isExit(km, it.b) {
				e := it
				e.primary = false
				out.exit = &e
				ctx = slices.Delete(slices.Clone(ctx), i, i+1)
				break
			}
		}
	}
	if out.exit == nil && !capturing && len(m.stack) > 1 {
		out.exit = &helpItem{b: withDesc(km.Back, "esc", tr("tui.key.back"))}
	}

	// navigation moves to the front of row 2
	var nav, row []helpItem
	for _, it := range ctx {
		if isNav(it.b) && !it.primary {
			if !slices.ContainsFunc(al, func(a helpItem) bool { return slices.Equal(a.b.Keys(), it.b.Keys()) }) {
				if it.prio == 0 {
					it.prio = 2
				}
				nav = append(nav, it)
			}
			continue
		}
		row = append(row, it)
	}
	always := append(nav, al...)

	if !capturing {
		more := helpItem{b: relabel(km.Help, tr("tui.key.all_keys"))}
		out.more = &more
	}

	// an item that would appear twice appears once: a non-primary duplicate
	// leaves row 1, a primary one leaves row 2
	sameKeys := func(a, b helpItem) bool { return slices.Equal(a.b.Keys(), b.b.Keys()) }
	var row2 []helpItem
	for _, it := range always {
		if slices.ContainsFunc(row, func(r helpItem) bool { return r.primary && sameKeys(r, it) }) {
			continue
		}
		if slices.ContainsFunc(row2, func(r helpItem) bool { return sameKeys(r, it) }) {
			continue
		}
		row2 = append(row2, it)
	}
	for _, it := range row {
		if !it.primary {
			dup := slices.ContainsFunc(row2, func(r helpItem) bool { return sameKeys(r, it) }) ||
				(out.more != nil && sameKeys(*out.more, it)) || (out.exit != nil && sameKeys(*out.exit, it))
			if dup {
				continue
			}
		}
		out.row = append(out.row, it)
	}
	out.always = row2

	switch top.(type) {
	case *listScreen:
		out.rowLabel = "tui.help.row_this"
	case *reposScreen:
		if top.(*reposScreen).tab == tabLocal {
			out.rowLabel = "tui.help.row_this"
		}
	}
	if out.rowLabel == "" {
		out.rowLabel = "tui.help.row_step"
	}
	return out
}

// barEnt is one item on its way to a line.
type barEnt struct {
	it   helpItem
	row2 bool // comes from row 2 (its own colour while the labels are hidden)
	keep int  // one-row mode: higher survives longer
	ell  bool // the "…" mark
}

func (e barEnt) text() string {
	if e.ell {
		return "…"
	}
	h := e.it.b.Help()
	if e.it.primary {
		return "▸ " + h.Key + " " + h.Desc
	}
	return h.Key + " " + h.Desc
}

func (e barEnt) render(st *Styles, tint bool) string {
	if e.ell {
		return st.Help.Ellipsis.Render("…")
	}
	h := e.it.b.Help()
	if e.it.primary {
		return st.HelpPrimary.Render("▸ " + h.Key + " " + h.Desc)
	}
	if e.row2 && tint {
		return st.HelpAlwaysKey.Render(h.Key) + " " + st.HelpAlwaysDesc.Render(h.Desc)
	}
	return st.Help.ShortKey.Render(h.Key) + " " + st.Help.ShortDesc.Render(h.Desc)
}

func entsWidth(es []barEnt) int {
	w := 0
	for _, e := range es {
		w += lipgloss.Width(e.text())
	}
	if len(es) > 1 {
		w += sepWidth * (len(es) - 1)
	}
	return w
}

func entsOf(items []helpItem, row2 bool) []barEnt {
	out := make([]barEnt, len(items))
	for i, it := range items {
		out[i] = barEnt{it: it, row2: row2}
	}
	return out
}

// cutFirst shortens the description of the first entry (its key stays) so
// that the entries fit in w cells, as far as a description can shrink.
func cutFirst(es []barEnt, w int) {
	if len(es) == 0 || es[0].ell || entsWidth(es) <= w {
		return
	}
	b := es[0].it.b
	h := b.Help()
	room := lipgloss.Width(h.Desc) - (entsWidth(es) - w)
	b.SetHelp(h.Key, truncWords(h.Desc, maxInt(room, 1)))
	es[0].it.b = b
}

// joinEnts renders the entries on one line behind prefix.
func joinEnts(st *Styles, prefix string, es []barEnt, tint bool) string {
	var b strings.Builder
	b.WriteString(prefix)
	sep := st.Help.ShortSeparator.Render(strings.Repeat(" ", sepWidth))
	for i, e := range es {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(e.render(st, tint))
	}
	return b.String()
}

// renderHelpBar fits the bar to w cells. It returns exactly rows lines (one or
// two), each at most w cells wide. Row labels show only while every key of
// both rows fits with them; they go before any key is dropped.
func renderHelpBar(st *Styles, b helpBarItems, w, rows int) []string {
	if rows <= 1 {
		return []string{oneRowBar(st, b, w)}
	}
	return twoRowBar(st, b, w)
}

func twoRowBar(st *Styles, b helpBarItems, w int) []string {
	r1 := entsOf(b.row, false)
	r2 := entsOf(b.always, true)
	for i := range r2 {
		r2[i].keep = b.always[i].prio
	}
	if b.more != nil {
		r2 = append(r2, barEnt{it: *b.more, row2: true, keep: prioKeep})
	}
	if b.exit != nil {
		r2 = append(r2, barEnt{it: *b.exit, row2: true, keep: prioKeep})
	}
	label1 := tr(b.rowLabel)
	if b.rowLabel == "" {
		label1 = tr("tui.help.row_step")
	}
	label2 := tr("tui.help.row_always")
	lw := maxInt(lipgloss.Width(label1), lipgloss.Width(label2))
	labelW := lw + 2 // leading space, padded label, one space

	// the labels come first to go: shown only when every key of both rows fits
	// with them
	labels := (len(r1) == 0 || labelW+entsWidth(r1) <= w) && (len(r2) == 0 || labelW+entsWidth(r2) <= w)

	prefix := func(label string, empty bool) (string, int) {
		if labels && !empty {
			return " " + st.HelpLabel.Render(pad(label, lw)) + " ", labelW
		}
		return " ", 1
	}

	// row 1: drop from the end (never the first item), "…" when anything went
	line1 := ""
	if len(r1) > 0 {
		p, pw := prefix(label1, false)
		es, dropped := slices.Clone(r1), false
		for {
			ell := 0
			if dropped {
				ell = sepWidth + 1
			}
			if pw+entsWidth(es)+ell <= w || len(es) <= 1 {
				break
			}
			es = es[:len(es)-1]
			dropped = true
		}
		room := w - pw
		if dropped {
			room -= sepWidth + 1
		}
		cutFirst(es, room)
		if dropped {
			es = append(es, barEnt{ell: true})
		}
		line1 = joinEnts(st, p, es, !labels)
	}

	// row 2: drop the lowest priority first, never "?" or the exit
	line2 := ""
	if len(r2) > 0 {
		p, pw := prefix(label2, false)
		es := slices.Clone(r2)
		for pw+entsWidth(es) > w {
			at := -1
			for i, e := range es {
				if e.keep >= prioKeep {
					continue
				}
				if at < 0 || e.keep <= es[at].keep {
					at = i
				}
			}
			if at < 0 {
				break
			}
			es = slices.Delete(es, at, at+1)
		}
		line2 = joinEnts(st, p, es, !labels)
	}
	return []string{trunc(line1, w), trunc(line2, w)}
}

// oneRowBar is the single-row bar of short terminals: the next step, the most
// useful always-on key, the rest of row 1, the rest of row 2, "…" when
// anything was dropped, then "?" and the exit key (both always kept).
func oneRowBar(st *Styles, b helpBarItems, w int) string {
	var ents []barEnt
	row := slices.Clone(b.row)
	if len(row) > 0 && row[0].primary {
		ents = append(ents, barEnt{it: row[0], keep: prioKeep})
		row = row[1:]
	}
	top := -1
	for i, it := range b.always {
		if top < 0 || it.prio > b.always[top].prio {
			top = i
		}
	}
	keepOf := func(prio int) int {
		if prio >= 3 {
			return 100
		}
		return prio
	}
	if top >= 0 {
		ents = append(ents, barEnt{it: b.always[top], row2: true, keep: keepOf(b.always[top].prio)})
	}
	for i, it := range row {
		ents = append(ents, barEnt{it: it, keep: 50 - i})
	}
	for i, it := range b.always {
		if i != top {
			ents = append(ents, barEnt{it: it, row2: true, keep: keepOf(it.prio)})
		}
	}
	var tail []barEnt
	if b.more != nil {
		tail = append(tail, barEnt{it: *b.more, row2: true, keep: prioKeep})
	}
	if b.exit != nil {
		tail = append(tail, barEnt{it: *b.exit, row2: true, keep: prioKeep})
	}
	assemble := func(dropped bool) []barEnt {
		out := slices.Clone(ents)
		if dropped {
			out = append(out, barEnt{ell: true})
		}
		return append(out, tail...)
	}
	dropped := false
	for 1+entsWidth(assemble(dropped)) > w {
		at := -1
		for i, e := range ents {
			if e.keep >= prioKeep {
				continue
			}
			if at < 0 || e.keep <= ents[at].keep {
				at = i
			}
		}
		if at < 0 {
			break
		}
		ents = slices.Delete(ents, at, at+1)
		dropped = true
	}
	all := assemble(dropped)
	if 1+entsWidth(all) > w {
		// even the mandatory items do not fit: cut the first description
		cutFirst(all, w-1)
	}
	return trunc(joinEnts(st, " ", all, true), w)
}

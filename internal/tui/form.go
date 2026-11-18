package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The form component: a list of fields (text input, select, yes/no) shown on
// one scrolling page, with the focused field marked by a gutter. Everything
// runs on the Update goroutine: the form starts no goroutine, its cursor does
// not blink (so there is no timer), and View reads the state the last Update
// left, including the bound values and the hint functions.

type fieldKind int

const (
	fieldInput fieldKind = iota
	fieldSelect
	fieldConfirm
)

// formOption is one choice of a select.
type formOption struct{ label, value string }

// formField is one field of a form. The bound pointer is written on every
// change, so a live preview always sees the current value.
type formField struct {
	kind        fieldKind
	title, hint string
	// hintFn, when set, replaces hint and is evaluated every time the form is
	// drawn (for a hint that follows another field).
	hintFn      func() string
	placeholder string
	str         *string // input and select
	flag        *bool   // confirm
	options     []formOption
	inline      bool // select: options side by side, moved with left/right
	yes, no     string
	// validate checks the raw value of an input (spaces kept).
	validate func(string) error
	// hidden, evaluated live, removes the field from the form: it is skipped
	// and not validated, but its bound value stays what it was.
	hidden func() bool
	// section is a header drawn before the first visible field of the section.
	section string

	err   error
	input textinput.Model
	cur   int // select: index of the chosen option
}

// inputField is a single-line text field.
func inputField(title, hint, placeholder string, v *string, validate func(string) error) *formField {
	return &formField{kind: fieldInput, title: title, hint: hint, placeholder: placeholder, str: v, validate: validate}
}

// selectField is a choice among options; inline puts them side by side.
func selectField(title, hint string, v *string, inline bool, opts ...formOption) *formField {
	return &formField{kind: fieldSelect, title: title, hint: hint, str: v, inline: inline, options: opts}
}

// confirmField is a yes/no question.
func confirmField(title, hint string, v *bool) *formField {
	return &formField{kind: fieldConfirm, title: title, hint: hint, flag: v, yes: tr("tui.yes"), no: tr("tui.no")}
}

// hintf makes the hint a function, evaluated when the form is drawn.
func (ff *formField) hintf(fn func() string) *formField { ff.hintFn = fn; return ff }

// hideWhen hides the field while fn returns true.
func (ff *formField) hideWhen(fn func() bool) *formField { ff.hidden = fn; return ff }

// inSection puts the field in a named section.
func (ff *formField) inSection(s string) *formField { ff.section = s; return ff }

func (ff *formField) hintText() string {
	if ff.hintFn != nil {
		return ff.hintFn()
	}
	return ff.hint
}

// form is the component. Create it with newForm, move it with Update, draw it
// with View after SetSize.
type form struct {
	st     *Styles
	km     *KeyMap
	fields []*formField
	focus  int
	w      int // width of the area, gutter included
	rows   int // height of the area
	// compact drops the blank rows between fields (a short terminal).
	compact bool
	// starts and focusEnd are set by layout: the first row of every visible
	// field and the row after the focused one.
	starts   []int
	vstarts  []int // per field of starts: the row of its wrapped value, else -1
	focusEnd int
}

func newForm(st *Styles, km *KeyMap, fields ...*formField) *form {
	f := &form{st: st, km: km, fields: fields, w: 80, rows: 30}
	for _, ff := range fields {
		f.prepare(ff)
	}
	f.Reset()
	return f
}

// prepare gives a field its widgets and takes the initial value from the
// bound pointer.
func (f *form) prepare(ff *formField) {
	if ff.kind == fieldInput {
		ti := textinput.New()
		ti.Prompt = "> "
		ti.PromptStyle = f.st.FormPrompt
		ti.TextStyle = f.st.FormValue
		ti.PlaceholderStyle = f.st.FormPlaceholder
		ti.Cursor.Style = f.st.FormCursor
		ti.Cursor.TextStyle = f.st.FormPlaceholder
		ti.Cursor.SetMode(cursor.CursorStatic) // no blink timer
		ti.Placeholder = ff.placeholder
		ff.input = ti
	}
	f.syncFromBound(ff)
}

// syncFromBound makes the widgets show what the bound pointer holds.
func (f *form) syncFromBound(ff *formField) {
	switch ff.kind {
	case fieldInput:
		ff.input.SetValue(*ff.str)
		ff.input.CursorEnd()
	case fieldSelect:
		ff.cur = 0
		for i, o := range ff.options {
			if o.value == *ff.str {
				ff.cur = i
				break
			}
		}
		if len(ff.options) > 0 {
			*ff.str = ff.options[ff.cur].value
		}
	}
}

// SetSize sets the area: w cells wide (gutter included), rows high.
func (f *form) SetSize(w, rows int) {
	f.w, f.rows = w, rows
	for _, ff := range f.fields {
		if ff.kind == fieldInput {
			width := maxInt(f.cw()-3, 1) // the prompt takes two cells, the cursor one
			if ff.input.Width != width {
				ff.input.Width = width
				ff.input.SetCursor(ff.input.Position())
			}
		}
	}
}

// cw is the width of the content: the area less the gutter.
func (f *form) cw() int { return maxInt(f.w-2, 4) }

func (f *form) visible(i int) bool {
	return f.fields[i].hidden == nil || !f.fields[i].hidden()
}

func (f *form) nextVisible(from int) int {
	for i := from + 1; i < len(f.fields); i++ {
		if f.visible(i) {
			return i
		}
	}
	return -1
}

func (f *form) prevVisible(from int) int {
	for i := from - 1; i >= 0; i-- {
		if f.visible(i) {
			return i
		}
	}
	return -1
}

func (f *form) current() *formField {
	if f.focus < 0 || f.focus >= len(f.fields) {
		return nil
	}
	return f.fields[f.focus]
}

// Init focuses the first visible field.
func (f *form) Init() tea.Cmd { return f.Reset() }

// Reset starts the form over with the values it is bound to: the first
// visible field gets the focus, the errors are cleared and the inputs show
// the bound values again.
func (f *form) Reset() tea.Cmd {
	for _, ff := range f.fields {
		ff.err = nil
		f.syncFromBound(ff)
		if ff.kind == fieldInput {
			ff.input.Blur()
		}
	}
	f.focus = 0
	if len(f.fields) > 0 && !f.visible(0) {
		if n := f.nextVisible(0); n >= 0 {
			f.focus = n
		}
	}
	if ff := f.current(); ff != nil && ff.kind == fieldInput {
		return ff.input.Focus()
	}
	return nil
}

// moveTo gives the focus to field i.
func (f *form) moveTo(i int) {
	if i == f.focus {
		return
	}
	if old := f.current(); old != nil {
		old.err = nil
		if old.kind == fieldInput {
			old.input.Blur()
		}
	}
	f.focus = i
	if ff := f.current(); ff != nil && ff.kind == fieldInput {
		ff.input.Focus() // static cursor: no command
	}
}

// check validates one field; it records the error.
func (f *form) check(ff *formField) bool {
	if ff.kind != fieldInput || ff.validate == nil {
		ff.err = nil
		return true
	}
	ff.err = ff.validate(*ff.str)
	return ff.err == nil
}

// advance validates the focused field and moves to the next visible one. On
// the last one it submits when allowed: every visible field is validated, the
// first one that fails gets the focus and its error. It reports whether the
// form was submitted.
func (f *form) advance(submit bool) bool {
	ff := f.current()
	if ff == nil || !f.check(ff) {
		return false
	}
	if n := f.nextVisible(f.focus); n >= 0 {
		f.moveTo(n)
		return false
	}
	if !submit {
		return false
	}
	for i, other := range f.fields {
		if !f.visible(i) {
			continue
		}
		if !f.check(other) {
			err := other.err
			f.moveTo(i)
			other.err = err
			return false
		}
	}
	return true
}

// retreat moves to the previous visible field without validating.
func (f *form) retreat() {
	if n := f.prevVisible(f.focus); n >= 0 {
		f.moveTo(n)
	}
}

// repair moves the focus off a field that has become hidden.
func (f *form) repair() {
	if len(f.fields) == 0 || (f.focus >= 0 && f.focus < len(f.fields) && f.visible(f.focus)) {
		return
	}
	n := f.nextVisible(f.focus)
	if n < 0 {
		n = f.prevVisible(f.focus)
	}
	if n >= 0 {
		f.moveTo(n)
	}
}

// Update handles one message. It reports whether the form was submitted (all
// visible fields valid). Esc is not handled: the screen decides what it does.
func (f *form) Update(msg tea.Msg) (tea.Cmd, bool) {
	var cmd tea.Cmd
	done := false
	switch m := msg.(type) {
	case tea.KeyMsg:
		cmd, done = f.updateKey(m)
	default:
		// only a pasted text (the textinput's own message) is of interest
		if ff := f.current(); ff != nil && ff.kind == fieldInput {
			ff.input, cmd = ff.input.Update(msg)
			f.syncInput(ff)
		}
	}
	f.repair()
	return cmd, done
}

// syncInput copies the text of an input to its bound value.
func (f *form) syncInput(ff *formField) {
	if v := ff.input.Value(); v != *ff.str {
		*ff.str = v
		ff.err = nil
	}
}

func (f *form) updateKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	ff := f.current()
	if ff == nil {
		return nil, false
	}
	s := msg.String()
	switch s {
	case "esc":
		return nil, false
	case "enter", "tab":
		return nil, f.advance(true)
	case "shift+tab":
		f.retreat()
		return nil, false
	}
	vertical := ff.kind == fieldSelect && !ff.inline
	switch {
	case key.Matches(msg, f.km.Down):
		if vertical {
			f.moveOption(ff, 1)
			return nil, false
		}
		f.advance(false)
		return nil, false
	case key.Matches(msg, f.km.Up):
		if vertical {
			f.moveOption(ff, -1)
			return nil, false
		}
		f.retreat()
		return nil, false
	}
	switch ff.kind {
	case fieldInput:
		var cmd tea.Cmd
		ff.input, cmd = ff.input.Update(msg)
		f.syncInput(ff)
		return cmd, false
	case fieldSelect:
		switch {
		case ff.inline && (s == "left" || s == "h"), !ff.inline && s == "k":
			f.moveOption(ff, -1)
		case ff.inline && (s == "right" || s == "l"), !ff.inline && s == "j":
			f.moveOption(ff, 1)
		case s == "home":
			f.setOption(ff, 0)
		case s == "end":
			f.setOption(ff, len(ff.options)-1)
		}
	case fieldConfirm:
		switch s {
		case "y", "Y":
			*ff.flag = true
			return nil, f.advance(true)
		case "n", "N":
			*ff.flag = false
			return nil, f.advance(true)
		case "left", "right", "h", "l", " ":
			*ff.flag = !*ff.flag
		}
	}
	return nil, false
}

// moveOption moves the choice of a select by delta, wrapping around.
func (f *form) moveOption(ff *formField, delta int) {
	if n := len(ff.options); n > 0 {
		f.setOption(ff, ((ff.cur+delta)%n+n)%n)
	}
}

func (f *form) setOption(ff *formField, i int) {
	if len(ff.options) == 0 {
		return
	}
	ff.cur = clamp(i, 0, len(ff.options)-1)
	*ff.str = ff.options[ff.cur].value
	ff.err = nil
}

// Err is the error of the focused field ("" when there is none).
func (f *form) Err() string {
	if ff := f.current(); ff != nil && ff.err != nil {
		return ff.err.Error()
	}
	return ""
}

// Position is the place of the focused field among the visible ones.
func (f *form) Position() (n, m int) {
	for i := range f.fields {
		if !f.visible(i) {
			continue
		}
		m++
		if i == f.focus {
			n = m
		}
	}
	return n, m
}

// Overflows reports whether the fields need more rows than the area has.
func (f *form) Overflows() bool {
	lines, _, condensed := f.layout()
	return condensed || len(lines) > f.rows
}

// ---- drawing ----

// fparts are the rows of one field, before the gutter is added.
type fparts struct {
	section string
	title   string
	hints   []string
	ctrl    []string // the control, already styled
	selRow  int      // the row of ctrl that holds the chosen option
	errs    []string
}

func (f *form) parts(i int, section string) fparts {
	ff := f.fields[i]
	focused := i == f.focus
	cw := f.cw()
	p := fparts{section: section, title: ff.title}
	if h := ff.hintText(); h != "" {
		p.hints = wrapUnits(h, cw)
	}
	switch ff.kind {
	case fieldInput:
		p.ctrl = f.inputRows(ff, focused)
	case fieldSelect:
		labels := make([]string, len(ff.options))
		for k, o := range ff.options {
			labels[k] = o.label
		}
		p.ctrl, p.selRow = f.optionRows(labels, ff.cur, ff.inline, focused)
	case fieldConfirm:
		cur := 1
		if *ff.flag {
			cur = 0
		}
		p.ctrl, p.selRow = f.optionRows([]string{ff.yes, ff.no}, cur, true, focused)
	}
	if ff.err != nil {
		for k, l := range wrapUnits(ff.err.Error(), maxInt(cw-2, 4)) {
			if k == 0 {
				l = "✗ " + l
			} else {
				l = "  " + l
			}
			p.errs = append(p.errs, f.st.Err.Render(l))
		}
	}
	return p
}

// maxValueRows caps the rows a blurred value may wrap onto.
const maxValueRows = 8

// maxCompactValueRows is the smaller cap of a compact layout, so the focused
// field still fits.
const maxCompactValueRows = 3

// inputRows is the control of a text field. The focused input scrolls inside
// one row. A blurred value wraps onto more rows so that all of it shows (an
// address or URL moves whole); only a compact layout cuts it with an ellipsis.
func (f *form) inputRows(ff *formField, focused bool) []string {
	if focused {
		return []string{ff.input.View()}
	}
	cw := f.cw()
	v := *ff.str
	if v == "" {
		return []string{"  " + f.st.FormPlaceholder.Render(trunc(ff.placeholder, cw-2))}
	}
	limit := maxValueRows
	if f.compact {
		limit = maxCompactValueRows
	}
	lines := wrapUnits(v, cw-2)
	if len(lines) > limit {
		lines = lines[:limit]
		lines[limit-1] = ellipsize(lines[limit-1], cw-2)
	}
	rows := make([]string, len(lines))
	for i, l := range lines {
		rows[i] = "  " + f.st.FormValueBlur.Render(l)
	}
	return rows
}

// optionRows lays the options out as radio marks. Inline options share rows
// and wrap to the next one when they do not fit; none is ever cut. It
// returns the rows and the row of the chosen option.
func (f *form) optionRows(labels []string, cur int, inline, focused bool) ([]string, int) {
	cw := f.cw()
	on, off := f.st.FormOptionOn, f.st.FormOption
	if !focused {
		on = f.st.FormValueBlur
	}
	var rows []string
	selRow := 0
	line, lineW := "", 0
	flush := func() {
		if line != "" {
			rows = append(rows, line)
		}
		line, lineW = "", 0
	}
	for i, label := range labels {
		mark, style := "○ ", off
		if i == cur {
			mark, style = "● ", on
		}
		text := mark + label
		w := lipgloss.Width(text)
		if w > cw {
			// an option wider than the area wraps by itself
			flush()
			for k, l := range wrapUnits(text, cw) {
				if k > 0 {
					l = "  " + l
				}
				if i == cur && k == 0 {
					selRow = len(rows)
				}
				rows = append(rows, style.Render(l))
			}
			continue
		}
		if inline && line != "" && lineW+2+w <= cw {
			line += "  " + style.Render(text)
			lineW += 2 + w
		} else {
			flush()
			line, lineW = style.Render(text), w
		}
		if i == cur {
			selRow = len(rows)
		}
		if !inline {
			flush()
		}
	}
	flush()
	return rows, selRow
}

// assemble puts the gutter in front of the rows of a field and clips them.
func (f *form) assemble(p fparts, focused bool) []string {
	gutter := "  "
	if focused {
		gutter = f.st.FormGutter.Render("┃") + " "
	}
	var out []string
	add := func(prefix, s string) { out = append(out, f.clip(prefix+s)) }
	if p.section != "" {
		add("  ", f.st.FormSection.Render(p.section))
	}
	if p.title != "" {
		style := f.st.FormTitleBlur
		if focused {
			style = f.st.FormTitle
		}
		add(gutter, style.Render(truncWords(p.title, f.cw())))
	}
	for _, l := range p.hints {
		add(gutter, f.st.FormHint.Render(l))
	}
	for _, l := range p.ctrl {
		add(gutter, l)
	}
	for _, l := range p.errs {
		add(gutter, l)
	}
	return out
}

func (f *form) clip(row string) string {
	if f.w <= 0 {
		return row
	}
	return ansi.Truncate(row, f.w, "")
}

func (p fparts) height() int {
	n := len(p.hints) + len(p.ctrl) + len(p.errs)
	if p.title != "" {
		n++
	}
	if p.section != "" {
		n++
	}
	return n
}

// ellipsize ends a hint row with an ellipsis within cw cells.
func ellipsize(line string, cw int) string {
	line = strings.TrimRight(line, " ")
	if lipgloss.Width(line)+1 <= cw {
		return line + "…"
	}
	return truncWords(line, cw)
}

// condense makes the rows of the focused field fit in rows: the hint loses
// its last rows (the last one kept ends with an ellipsis), then the errors
// but the first, then the title; the control stays (a long one shows the rows
// around the chosen option).
func (f *form) condense(p fparts, rows int) fparts {
	p.section = ""
	cw := f.cw()
	if over := p.height() - rows; over > 0 && len(p.hints) > 0 {
		keep := maxInt(len(p.hints)-over, 0)
		if keep > 0 {
			hints := append([]string(nil), p.hints[:keep]...)
			hints[keep-1] = ellipsize(hints[keep-1], cw)
			p.hints = hints
		} else {
			p.hints = nil
		}
	}
	if over := p.height() - rows; over > 0 && len(p.errs) > 1 {
		p.errs = p.errs[:maxInt(len(p.errs)-over, 1)]
	}
	title := p.title
	if p.height() > rows {
		p.title = ""
	}
	if p.height() > rows {
		p.errs = nil
	}
	// the title stays (cut at a word) when the control and it fit
	if p.title == "" && title != "" && len(p.ctrl)+1 <= rows && p.height() < rows {
		p.title = title
	}
	if avail := maxInt(rows, 1); len(p.ctrl) > avail {
		start := clamp(p.selRow-avail/2, 0, len(p.ctrl)-avail)
		p.ctrl = p.ctrl[start : start+avail]
	}
	return p
}

// layout builds the rows of every visible field and says where the focused
// one starts.
func (f *form) layout() (lines []string, focusStart int, condensed bool) {
	prevSection := ""
	first := true
	f.starts, f.vstarts = f.starts[:0], f.vstarts[:0]
	for i := range f.fields {
		if !f.visible(i) {
			continue
		}
		ff := f.fields[i]
		section := ""
		if ff.section != "" && ff.section != prevSection {
			section = ff.section
		}
		prevSection = ff.section
		focused := i == f.focus
		p := f.parts(i, section)
		if focused && p.height() > f.rows {
			p = f.condense(p, f.rows)
			condensed = true
		}
		if !first && !f.compact {
			lines = append(lines, "")
		}
		first = false
		if focused {
			focusStart = len(lines)
		}
		f.starts = append(f.starts, len(lines))
		asm := f.assemble(p, focused)
		vs := -1
		if !focused && ff.kind == fieldInput && len(p.ctrl) > 1 {
			vs = len(lines) + len(asm) - len(p.errs) - len(p.ctrl)
		}
		f.vstarts = append(f.vstarts, vs)
		lines = append(lines, asm...)
		if focused {
			f.focusEnd = len(lines)
		}
	}
	return lines, focusStart, condensed
}

// View draws the visible part of the form: at most rows rows, each at most w
// cells. The area scrolls so that the focused field is at its top.
func (f *form) View() string {
	if f.rows <= 0 {
		return ""
	}
	lines, focusStart, _ := f.layout()
	if len(lines) > f.rows {
		off := clamp(focusStart, 0, len(lines)-f.rows)
		off = f.wholeAbove(off)
		lines = lines[off : off+f.rows]
	}
	return joinLines(lines)
}

// wholeAbove moves the top of the window up so that a wrapped value just above
// it is shown whole, when the focused field still fits below: from the start of
// the field, else (a tall focused field) from the first row of the value.
func (f *form) wholeAbove(off int) int {
	at := -1
	for i, s := range f.starts {
		if s < off {
			at = i
		}
	}
	if at < 0 || f.vstarts[at] < 0 || f.vstarts[at] >= off {
		return off
	}
	for _, c := range []int{f.starts[at], f.vstarts[at]} {
		if f.focusEnd <= c+f.rows {
			return c
		}
	}
	return off
}

package tui

import (
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/tinhnd1902/gitid/internal/keymap"
)

// ---- helpers ----

func fk(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	case "ctrl+n":
		return tea.KeyMsg{Type: tea.KeyCtrlN}
	case "ctrl+p":
		return tea.KeyMsg{Type: tea.KeyCtrlP}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// press sends keys in order and reports whether any of them submitted.
func press(f *form, keys ...string) (done bool) {
	for _, k := range keys {
		_, d := f.Update(fk(k))
		done = done || d
	}
	return done
}

// typeInto types s one rune at a time, as a terminal delivers it.
func typeInto(f *form, s string) {
	for _, r := range s {
		f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func testForm(fields ...*formField) *form {
	f := newForm(NewStyles(), NewKeyMap(keymap.Defaults()), fields...)
	f.SetSize(60, 40)
	return f
}

func viewLines(f *form) []string { return strings.Split(ansi.Strip(f.View()), "\n") }

func rowOf(lines []string, sub string) int {
	for i, l := range lines {
		if strings.Contains(l, sub) {
			return i
		}
	}
	return -1
}

func widest(s string) int {
	w := 0
	for _, l := range strings.Split(s, "\n") {
		if n := lipgloss.Width(l); n > w {
			w = n
		}
	}
	return w
}

// three plain inputs bound to a, b, c.
func threeInputs() (*form, *[3]string) {
	var v [3]string
	return testForm(
		inputField("A", "", "", &v[0], nil),
		inputField("B", "", "", &v[1], nil),
		inputField("C", "", "", &v[2], nil),
	), &v
}

func required(v string) error {
	if strings.TrimSpace(v) == "" {
		return errors.New("required")
	}
	return nil
}

// ---- navigation ----

func TestFNav1EnterMovesThroughTheFields(t *testing.T) {
	f, _ := threeInputs()
	for want := 1; want <= 2; want++ {
		if press(f, "enter") || f.focus != want {
			t.Fatalf("focus %d, want %d", f.focus, want)
		}
	}
	if !press(f, "enter") {
		t.Fatal("enter on the last field must submit")
	}
}

func TestFNav3TabIsLikeEnter(t *testing.T) {
	f, _ := threeInputs()
	press(f, "tab", "tab")
	if f.focus != 2 {
		t.Fatalf("focus %d", f.focus)
	}
	if !press(f, "tab") {
		t.Fatal("tab on the last field submits (as huh did)")
	}
}

func TestFNav4ShiftTabDoesNotValidate(t *testing.T) {
	var a, b, c string
	f := testForm(inputField("A", "", "", &a, nil), inputField("B", "", "", &b, nil), inputField("C", "", "", &c, required))
	press(f, "enter", "enter")
	if press(f, "shift+tab") || f.focus != 1 {
		t.Fatalf("focus %d", f.focus)
	}
	if f.fields[2].err != nil {
		t.Fatal("going back must not validate")
	}
}

func TestFNav5FirstFieldHasNoPrevious(t *testing.T) {
	f, _ := threeInputs()
	press(f, "shift+tab", "up")
	if f.focus != 0 {
		t.Fatalf("focus %d", f.focus)
	}
}

func TestFNav6DownAndUpMoveBetweenFields(t *testing.T) {
	f, _ := threeInputs()
	press(f, "down")
	if f.focus != 1 {
		t.Fatalf("down: %d", f.focus)
	}
	press(f, "ctrl+n")
	if f.focus != 2 {
		t.Fatalf("ctrl+n: %d", f.focus)
	}
	press(f, "up", "ctrl+p")
	if f.focus != 0 {
		t.Fatalf("up: %d", f.focus)
	}
}

func TestFNav7DownOnTheLastFieldNeverSubmits(t *testing.T) {
	var a string
	f := testForm(inputField("A", "", "", &a, required))
	if press(f, "down") {
		t.Fatal("down must not submit")
	}
	if f.fields[0].err == nil {
		t.Fatal("down validates the field it leaves")
	}
	a = "x"
	f.fields[0].input.SetValue("x")
	if press(f, "down") || f.focus != 0 {
		t.Fatal("down on the last field stays")
	}
}

func TestFNav8HiddenFieldsAreSkipped(t *testing.T) {
	var a, b, c string
	f := testForm(inputField("A", "", "", &a, nil), inputField("B", "", "", &b, nil).hideWhen(func() bool { return true }), inputField("C", "", "", &c, nil))
	press(f, "enter")
	if f.focus != 2 {
		t.Fatalf("A to %d", f.focus)
	}
	press(f, "shift+tab")
	if f.focus != 0 {
		t.Fatalf("C to %d", f.focus)
	}
}

func TestFNav9EscIsNotConsumed(t *testing.T) {
	f, v := threeInputs()
	typeInto(f, "ab")
	cmd, done := f.Update(fk("esc"))
	if cmd != nil || done || f.focus != 0 || v[0] != "ab" {
		t.Fatalf("esc changed the form: cmd %v done %v focus %d %q", cmd != nil, done, f.focus, v[0])
	}
}

// ---- validation ----

func TestFVal1ErrorIsShownUnderTheInput(t *testing.T) {
	var a string
	f := testForm(inputField("A", "", "", &a, required))
	press(f, "enter")
	if f.Err() != "required" {
		t.Fatalf("Err %q", f.Err())
	}
	lines := viewLines(f)
	in, er := rowOf(lines, "> "), rowOf(lines, "✗ required")
	if in < 0 || er <= in {
		t.Fatalf("the error must be below the input:\n%s", f.View())
	}
	if f.focus != 0 {
		t.Fatal("focus moved")
	}
}

func TestFVal2TypingClearsTheError(t *testing.T) {
	var a string
	f := testForm(inputField("A", "", "", &a, required))
	press(f, "enter")
	typeInto(f, "x")
	if f.Err() != "" || strings.Contains(f.View(), "✗") {
		t.Fatalf("error not cleared:\n%s", f.View())
	}
}

func TestFVal3FixedValueAdvances(t *testing.T) {
	var a, b string
	f := testForm(inputField("A", "", "", &a, required), inputField("B", "", "", &b, nil))
	press(f, "enter")
	typeInto(f, "x")
	press(f, "enter")
	if f.focus != 1 {
		t.Fatalf("focus %d", f.focus)
	}
}

func TestFVal4SubmitFocusesTheFirstInvalidField(t *testing.T) {
	var a, b string
	f := testForm(inputField("A", "", "", &a, nil), inputField("B", "", "", &b, required))
	press(f, "enter") // to B, nothing typed
	press(f, "shift+tab")
	if f.fields[1].err != nil {
		t.Fatal("no error yet")
	}
	// jump to the last field the cheap way, then submit
	f.moveTo(1)
	if press(f, "enter") {
		t.Fatal("B is invalid: must not submit")
	}
	if f.focus != 1 || f.Err() != "required" {
		t.Fatalf("focus %d err %q", f.focus, f.Err())
	}
}

func TestFVal4bSubmitChecksEarlierFields(t *testing.T) {
	var a, b string
	f := testForm(inputField("A", "", "", &a, required), inputField("B", "", "", &b, nil))
	f.moveTo(1) // reached without passing A's check
	if press(f, "enter") {
		t.Fatal("A is invalid: must not submit")
	}
	if f.focus != 0 || f.Err() != "required" || !strings.Contains(f.View(), "✗ required") {
		t.Fatalf("focus %d err %q\n%s", f.focus, f.Err(), f.View())
	}
}

func TestFVal5HiddenFieldsAreNotValidated(t *testing.T) {
	var a, b string
	f := testForm(inputField("A", "", "", &a, nil), inputField("B", "", "", &b, required).hideWhen(func() bool { return true }))
	if !press(f, "enter") {
		t.Fatal("a hidden invalid field must not block the submit")
	}
}

func TestFVal6ValidatorSeesTheRawValue(t *testing.T) {
	var a string
	var seen string
	f := testForm(inputField("A", "", "", &a, func(v string) error { seen = v; return nil }))
	typeInto(f, "  x ")
	press(f, "enter")
	if seen != "  x " {
		t.Fatalf("validator saw %q", seen)
	}
}

func TestFVal7ErrorKeepsTokensWhole(t *testing.T) {
	var a string
	msg := "Dùng github.com/<login> hoặc ID+login@users.noreply.github.com thay vì email khác nữa nhé"
	f := testForm(inputField("A", "", "", &a, func(string) error { return errors.New(msg) }))
	f.SetSize(40, 40)
	press(f, "enter")
	lines := viewLines(f)
	for _, tok := range []string{"github.com/<login>", "ID+login@users.noreply.github.com"} {
		if rowOf(lines, tok) < 0 {
			t.Errorf("%q is split:\n%s", tok, f.View())
		}
	}
	first := rowOf(lines, "✗ ")
	for i, l := range lines[first+1:] {
		if strings.TrimSpace(l) == "" {
			break
		}
		if !strings.HasPrefix(l, "┃   ") {
			t.Errorf("continuation %d is not indented by two: %q", i, l)
		}
	}
}

// ---- select ----

func platformOptions() []formOption {
	return []formOption{{"GitHub", "github"}, {"GitLab", "gitlab"}, {"Bitbucket", "bitbucket"}, {"Gitea", "gitea"}, {"Azure DevOps", "azure-devops"}}
}

func TestFSel1InlineSelectWritesTheValueAtOnce(t *testing.T) {
	v := "github"
	f := testForm(selectField("P", "", &v, true, platformOptions()...))
	press(f, "right")
	if v != "gitlab" {
		t.Fatalf("1: %q", v)
	}
	press(f, "right", "right")
	if v != "gitea" {
		t.Fatalf("3: %q", v)
	}
	press(f, "right")
	if v != "azure-devops" {
		t.Fatalf("4: %q", v)
	}
}

func TestFSel2InlineWrapsAround(t *testing.T) {
	v := "github"
	f := testForm(selectField("P", "", &v, true, platformOptions()...))
	press(f, "left")
	if v != "azure-devops" {
		t.Fatalf("left at first: %q", v)
	}
	press(f, "right")
	if v != "github" {
		t.Fatalf("right at last: %q", v)
	}
	press(f, "l")
	if v != "gitlab" {
		t.Fatalf("l: %q", v)
	}
	press(f, "h")
	if v != "github" {
		t.Fatalf("h: %q", v)
	}
}

func TestFSel3VerticalSelectKeys(t *testing.T) {
	v := "https"
	f := testForm(selectField("A", "", &v, false, formOption{"HTTPS", "https"}, formOption{"SSH", "ssh"}))
	for _, c := range []struct {
		key  string
		want string
	}{
		{"down", "ssh"}, {"down", "https"}, {"up", "ssh"}, {"up", "https"}, {"j", "ssh"}, {"k", "https"},
		{"ctrl+n", "ssh"}, {"ctrl+p", "https"}, {"end", "ssh"}, {"home", "https"},
	} {
		press(f, c.key)
		if v != c.want {
			t.Fatalf("%s: %q, want %q", c.key, v, c.want)
		}
	}
	if f.focus != 0 {
		t.Fatal("a vertical select keeps the focus on up/down")
	}
}

func TestFSel4UnknownValueFallsBackToTheFirstOption(t *testing.T) {
	v := "nope"
	f := testForm(selectField("P", "", &v, true, platformOptions()...))
	if v != "github" || f.fields[0].cur != 0 {
		t.Fatalf("value %q cur %d", v, f.fields[0].cur)
	}
}

func TestFSel5CursorFollowsTheBoundValue(t *testing.T) {
	v := "gitlab"
	f := testForm(selectField("P", "", &v, true, platformOptions()...))
	if f.fields[0].cur != 1 {
		t.Fatalf("cur %d", f.fields[0].cur)
	}
	if !strings.Contains(ansi.Strip(f.View()), "● GitLab") || !strings.Contains(ansi.Strip(f.View()), "○ GitHub") {
		t.Fatalf("marks:\n%s", f.View())
	}
}

func TestFSel6OtherRunesDoNothingOnASelect(t *testing.T) {
	v := "github"
	f := testForm(selectField("P", "", &v, true, platformOptions()...), inputField("B", "", "", new(string), nil))
	press(f, "x", "y", " ", "j", "k", "n")
	if v != "github" || f.focus != 0 {
		t.Fatalf("value %q focus %d", v, f.focus)
	}
}

func TestFSel7InlineUpDownMoveTheFocus(t *testing.T) {
	v := "github"
	var b string
	f := testForm(inputField("A", "", "", new(string), nil), selectField("P", "", &v, true, platformOptions()...), inputField("B", "", "", &b, nil))
	f.moveTo(1)
	press(f, "down")
	if f.focus != 2 || v != "github" {
		t.Fatalf("down: focus %d value %q", f.focus, v)
	}
	press(f, "up", "up")
	if f.focus != 0 || v != "github" {
		t.Fatalf("up: focus %d value %q", f.focus, v)
	}
}

func TestFSel8InlineOptionsWrapAtNarrowWidths(t *testing.T) {
	v := "github"
	f := testForm(selectField("P", "", &v, true, platformOptions()...))
	f.SetSize(34, 20)
	out := ansi.Strip(f.View())
	if strings.Contains(out, "…") || widest(out) > 34 {
		t.Fatalf("cut or too wide:\n%s", out)
	}
	n := 0
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "○") || strings.Contains(l, "●") {
			n++
		}
	}
	if n < 2 {
		t.Fatalf("options did not wrap:\n%s", out)
	}
	for _, o := range platformOptions() {
		if !strings.Contains(out, o.label) {
			t.Errorf("%q missing:\n%s", o.label, out)
		}
	}
}

// ---- confirm ----

func TestFConf1YAdvancesAndSets(t *testing.T) {
	var on bool
	f := testForm(confirmField("Q", "", &on), inputField("B", "", "", new(string), nil))
	if press(f, "y") || !on || f.focus != 1 {
		t.Fatalf("on %v focus %d", on, f.focus)
	}
}

func TestFConf2NAndUppercase(t *testing.T) {
	on := true
	f := testForm(confirmField("Q", "", &on), confirmField("R", "", new(bool)), inputField("B", "", "", new(string), nil))
	press(f, "n")
	if on || f.focus != 1 {
		t.Fatalf("n: on %v focus %d", on, f.focus)
	}
	press(f, "shift+tab")
	press(f, "Y")
	if !on || f.focus != 1 {
		t.Fatalf("Y: on %v focus %d", on, f.focus)
	}
	press(f, "shift+tab", "N")
	if on || f.focus != 1 {
		t.Fatalf("N: on %v focus %d", on, f.focus)
	}
}

func TestFConf3YOnTheLastFieldSubmits(t *testing.T) {
	var on bool
	f := testForm(inputField("A", "", "", new(string), nil), confirmField("Q", "", &on))
	press(f, "enter")
	if !press(f, "y") || !on {
		t.Fatalf("y did not submit (on %v)", on)
	}
}

func TestFConf4TogglesKeepTheFocus(t *testing.T) {
	var on bool
	f := testForm(confirmField("Q", "", &on), inputField("B", "", "", new(string), nil))
	for _, k := range []string{"right", "left", "l", "h", " "} {
		before := on
		press(f, k)
		if on == before || f.focus != 0 {
			t.Fatalf("%q: on %v (was %v) focus %d", k, on, before, f.focus)
		}
	}
}

func TestFConf5RadioMarksFollowTheFlag(t *testing.T) {
	var on bool
	f := testForm(confirmField("Q", "", &on))
	if out := ansi.Strip(f.View()); !strings.Contains(out, "○ Có") || !strings.Contains(out, "● Không") {
		t.Fatalf("off:\n%s", out)
	}
	on = true
	if out := ansi.Strip(f.View()); !strings.Contains(out, "● Có") || !strings.Contains(out, "○ Không") {
		t.Fatalf("on:\n%s", out)
	}
}

// ---- conditional fields ----

func TestFCond1YRevealsTheDependentField(t *testing.T) {
	var adv bool
	var host string
	f := testForm(confirmField("Adv", "", &adv), inputField("Host", "", "", &host, nil).hideWhen(func() bool { return !adv }))
	if strings.Contains(f.View(), "Host") {
		t.Fatal("host visible before")
	}
	press(f, "y")
	if f.focus != 1 || !strings.Contains(f.View(), "Host") {
		t.Fatalf("focus %d:\n%s", f.focus, f.View())
	}
}

func TestFCond2NLeavesTheDependentFieldHidden(t *testing.T) {
	var adv bool
	f := testForm(confirmField("Adv", "", &adv), inputField("Host", "", "", new(string), nil).hideWhen(func() bool { return !adv }))
	if !press(f, "n") {
		t.Fatal("n on the last visible field submits")
	}
}

func TestFCond3FocusLeavesAFieldThatBecameHidden(t *testing.T) {
	var hide bool
	var a, b, c string
	f := testForm(
		inputField("A", "", "", &a, nil),
		inputField("B", "", "", &b, nil).hideWhen(func() bool { return hide }),
		inputField("C", "", "", &c, nil).hideWhen(func() bool { return hide }),
	)
	f.moveTo(1)
	hide = true
	f.Update(struct{}{})
	if f.focus != 0 {
		t.Fatalf("nothing after: focus %d, want the previous visible (0)", f.focus)
	}
	hide = false
	f.moveTo(1)
	hide = true
	f.fields[2].hidden = nil // C stays
	f.Update(struct{}{})
	if f.focus != 2 {
		t.Fatalf("focus %d, want the next visible (2)", f.focus)
	}
}

func TestFCond4HintFunctionIsEvaluatedAtDrawTime(t *testing.T) {
	hint := "one"
	f := testForm(inputField("A", "", "", new(string), nil).hintf(func() string { return hint }))
	if !strings.Contains(f.View(), "one") {
		t.Fatal("first hint")
	}
	hint = "two"
	if v := f.View(); !strings.Contains(v, "two") || strings.Contains(v, "one") {
		t.Fatalf("hint not refreshed:\n%s", v)
	}
}

func TestFCond5ResetFocusesTheFirstFieldAndReloadsValues(t *testing.T) {
	var a, b string
	f := testForm(inputField("A", "", "", &a, nil), inputField("B", "", "", &b, required))
	f.moveTo(1)
	press(f, "enter") // error on B
	if f.Err() == "" {
		t.Fatal("setup: no error")
	}
	a, b = "alpha", "beta"
	f.Reset()
	if f.focus != 0 || f.Err() != "" || f.fields[1].err != nil {
		t.Fatalf("focus %d err %q", f.focus, f.Err())
	}
	if f.fields[0].input.Value() != "alpha" || f.fields[1].input.Value() != "beta" || f.fields[1].input.Position() != 4 {
		t.Fatal("inputs do not show the bound values with the cursor at the end")
	}
	// the focused input takes the typing
	typeInto(f, "!")
	if a != "alpha!" {
		t.Fatalf("typed into %q", a)
	}
}

// ---- scrolling and resize ----

func manyFields(n int) (*form, []*string) {
	vals := make([]*string, n)
	var fields []*formField
	for i := range vals {
		vals[i] = new(string)
		fields = append(fields, inputField("F"+string(rune('0'+i)), "hint "+string(rune('0'+i)), "", vals[i], nil))
	}
	return testForm(fields...), vals
}

func TestFScr1FocusedFieldIsAlwaysVisible(t *testing.T) {
	f, _ := manyFields(10)
	f.SetSize(40, 12)
	for k := 0; k < 10; k++ {
		out := ansi.Strip(f.View())
		if n := len(strings.Split(out, "\n")); n > 12 {
			t.Fatalf("step %d: %d rows", k, n)
		}
		if !strings.Contains(out, "F"+string(rune('0'+k))) || !strings.Contains(out, "> ") {
			t.Fatalf("step %d: focused field not visible:\n%s", k, out)
		}
		press(f, "enter")
	}
}

func TestFScr2FocusedBlockStartsAtTheTop(t *testing.T) {
	f, _ := manyFields(10)
	f.SetSize(40, 12)
	press(f, "enter", "enter", "enter")
	if first := viewLines(f)[0]; !strings.Contains(first, "F3") {
		t.Fatalf("first row %q", first)
	}
}

func TestFScr3NoBlankRowsAfterTheLastField(t *testing.T) {
	f, _ := manyFields(10)
	f.SetSize(40, 12)
	for range 9 {
		press(f, "enter")
	}
	lines := viewLines(f)
	if strings.TrimSpace(lines[len(lines)-1]) == "" || len(lines) != 12 {
		t.Fatalf("%d rows, last %q", len(lines), lines[len(lines)-1])
	}
}

func TestFScr4BackToTheFirstFieldScrollsToTheTop(t *testing.T) {
	f, _ := manyFields(10)
	f.SetSize(40, 12)
	press(f, "enter", "enter", "enter", "enter")
	press(f, "shift+tab", "shift+tab", "shift+tab", "shift+tab")
	if first := viewLines(f)[0]; !strings.Contains(first, "F0") {
		t.Fatalf("first row %q", first)
	}
}

func TestFScr5TallFocusedBlockIsCondensed(t *testing.T) {
	long := "uno dos tres cuatro cinco seis siete ocho nueve diez once doce trece catorce quince dieciseis diecisiete"
	for _, rows := range []int{2, 3, 4} {
		var a string
		f := testForm(inputField("Title", long, "", &a, nil))
		f.SetSize(30, rows)
		lines := viewLines(f)
		if len(lines) > rows {
			t.Fatalf("rows=%d: %d rows", rows, len(lines))
		}
		if !strings.Contains(lines[len(lines)-1], "> ") {
			t.Fatalf("rows=%d: the control row is missing:\n%s", rows, f.View())
		}
		if !strings.Contains(lines[0], "Title") {
			t.Fatalf("rows=%d: the title is missing:\n%s", rows, f.View())
		}
		if rows >= 3 && !strings.HasSuffix(strings.TrimRight(lines[len(lines)-2], " "), "…") {
			t.Fatalf("rows=%d: the last hint row must end with an ellipsis:\n%s", rows, f.View())
		}
	}
}

func TestFScr6OneRowShowsTheChosenOption(t *testing.T) {
	v := "ssh"
	f := testForm(selectField("A", "hint", &v, false, formOption{"HTTPS", "https"}, formOption{"SSH", "ssh"}, formOption{"Other", "o"}))
	f.SetSize(30, 1)
	lines := viewLines(f)
	if len(lines) != 1 || !strings.Contains(lines[0], "● SSH") {
		t.Fatalf("%q", lines)
	}
}

func TestFScr7OverflowAndPosition(t *testing.T) {
	f, _ := manyFields(10)
	f.SetSize(40, 60)
	if f.Overflows() {
		t.Fatal("everything fits")
	}
	f.SetSize(40, 12)
	press(f, "enter", "enter")
	if !f.Overflows() {
		t.Fatal("must overflow")
	}
	if n, m := f.Position(); n != 3 || m != 10 {
		t.Fatalf("position %d/%d", n, m)
	}
}

func TestFSize1ResizeRewrapsWithoutAnUpdate(t *testing.T) {
	hint := "Tên đăng nhập như trong URL hồ sơ: github.com/<login>. Không phải email của bạn"
	f := testForm(inputField("A", hint, "", new(string), nil))
	f.SetSize(80, 20)
	a := len(viewLines(f))
	f.SetSize(44, 20)
	lines := viewLines(f)
	if len(lines) <= a {
		t.Fatalf("not re-wrapped: %d then %d rows", a, len(lines))
	}
	if rowOf(lines, "github.com/<login>.") < 0 {
		t.Fatalf("token split:\n%s", f.View())
	}
}

func TestFSize2EveryRowFitsEverySize(t *testing.T) {
	var s1, s2, s3, s4 string
	var adv bool
	mk := func() *form {
		return testForm(
			selectField("Platform", "Where the repos live.", &s1, true, platformOptions()...),
			inputField("Username", "Tên đăng nhập trên github.com/<login> không phải ID+login@users.noreply.github.com", "vd: alice", &s2, required),
			inputField("Email", "", "vd: alice@acme.com", &s3, required),
			selectField("Auth", "HTTPS or SSH, the long hint wraps", &s4, false, formOption{"HTTPS (khuyên dùng)", "https"}, formOption{"SSH (SSH key riêng)", "ssh"}),
			confirmField("Advanced? (custom host, orgs, SSH host/port)", "Most accounts do not need this.", &adv),
		)
	}
	for _, w := range []int{22, 34, 40, 54, 66, 74, 94, 114} {
		for _, rows := range []int{1, 2, 3, 5, 8, 14, 30} {
			for _, compact := range []bool{false, true} {
				f := mk()
				f.compact = compact
				f.SetSize(w, rows)
				for focus := range f.fields {
					f.moveTo(focus)
					if focus == 1 {
						f.check(f.fields[1]) // an error row
					}
					out := f.View()
					if n := len(strings.Split(out, "\n")); n > rows {
						t.Fatalf("w=%d rows=%d focus=%d: %d rows\n%s", w, rows, focus, n, out)
					}
					if got := widest(out); got > w {
						t.Fatalf("w=%d rows=%d focus=%d: row of %d cells\n%s", w, rows, focus, got, out)
					}
				}
			}
		}
	}
}

// ---- unicode ----

func TestFUni1PrecomposedVietnamese(t *testing.T) {
	var v string
	f := testForm(inputField("A", "", "", &v, nil))
	f.SetSize(40, 10)
	typeInto(f, "Công ty Cá nhân")
	if v != "Công ty Cá nhân" {
		t.Fatalf("value %q", v)
	}
	if widest(f.View()) > 40 {
		t.Fatalf("too wide:\n%s", f.View())
	}
}

func TestFUni2DecomposedCJKAndEmojiStayIntact(t *testing.T) {
	var v string
	f := testForm(inputField("A", "", "", &v, nil))
	f.SetSize(22, 10)
	text := "e\u0301 株式会社 😀 e\u0301e\u0301e\u0301 株式会社株式会社 😀😀😀"
	typeInto(f, text)
	if v != text {
		t.Fatalf("value %q, want %q", v, text)
	}
	if got := widest(f.View()); got > 22 {
		t.Fatalf("a row is %d cells wide:\n%s", got, f.View())
	}
}

func TestFUni3BackspaceRemovesAWholePrecomposedRune(t *testing.T) {
	var v string
	f := testForm(inputField("A", "", "", &v, nil))
	f.SetSize(30, 10)
	typeInto(f, "Nguyễn")
	press(f, "left", "backspace")
	if v != "Nguyn" {
		t.Fatalf("value %q", v)
	}
	if widest(f.View()) > 30 {
		t.Fatal("too wide")
	}
}

func TestFUni4VietnameseHintWrapsWithinTheWidth(t *testing.T) {
	hint := "Tên đăng nhập (login) trên nền tảng, như trong URL hồ sơ: github.com/<login>. Không phải email, không phải tên hiển thị."
	f := testForm(inputField("Tên", hint, "", new(string), nil))
	f.SetSize(32, 20)
	out := f.View()
	if widest(out) > 32 {
		t.Fatalf("too wide:\n%s", out)
	}
	if rowOf(strings.Split(ansi.Strip(out), "\n"), "github.com/<login>.") < 0 {
		t.Fatalf("the URL is split:\n%s", out)
	}
}

// ---- styling ----

func TestFSty1GutterMarksOnlyTheFocusedField(t *testing.T) {
	var a, b, c string
	f := testForm(inputField("F0", "hint0", "", &a, nil), inputField("F1", "hint1", "", &b, nil), inputField("F2", "hint2", "", &c, nil))
	press(f, "enter")
	n := 0
	for _, l := range viewLines(f) {
		has := strings.Contains(l, "┃")
		switch {
		case strings.Contains(l, "F0") || strings.Contains(l, "hint0") || strings.Contains(l, "F2") || strings.Contains(l, "hint2"):
			if has {
				t.Errorf("gutter on a blurred row %q", l)
			}
		case strings.Contains(l, "F1") || strings.Contains(l, "hint1") || strings.Contains(l, "> "):
			if !has {
				t.Errorf("no gutter on a focused row %q", l)
			}
			n++
		}
	}
	if n != 3 {
		t.Fatalf("%d focused rows", n)
	}
}

func TestFSty2PlaceholderShownUntilThereIsAValue(t *testing.T) {
	var a, b string
	f := testForm(inputField("A", "", "vd: alice", &a, nil), inputField("B", "", "vd: bob", &b, nil))
	out := ansi.Strip(f.View())
	if !strings.Contains(out, "vd: alice") || !strings.Contains(out, "vd: bob") {
		t.Fatalf("placeholders (focused and blurred):\n%s", out)
	}
	typeInto(f, "x")
	if out := ansi.Strip(f.View()); strings.Contains(out, "vd: alice") || !strings.Contains(out, "vd: bob") {
		t.Fatalf("after typing:\n%s", out)
	}
	press(f, "enter")
	if out := ansi.Strip(f.View()); strings.Contains(out, "vd: alice") {
		t.Fatalf("blurred filled field shows the placeholder:\n%s", out)
	}
}

func TestFSty3LightAndDarkDifferOnlyInColor(t *testing.T) {
	dark := lipgloss.HasDarkBackground()
	t.Cleanup(func() {
		lipgloss.SetColorProfile(termenv.Ascii)
		lipgloss.SetHasDarkBackground(dark)
	})
	var a, b string
	build := func() *form {
		f := testForm(inputField("A", "hint", "vd: x", &a, nil), selectField("B", "", &b, true, formOption{"One", "1"}, formOption{"Two", "2"}))
		f.moveTo(0)
		return f
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)
	d := build().View()
	lipgloss.SetHasDarkBackground(false)
	l := build().View()
	if d == l {
		t.Fatal("light and dark render the same SGR sequences")
	}
	if ansi.Strip(d) != ansi.Strip(l) {
		t.Fatalf("the text differs:\n%s\n%s", ansi.Strip(d), ansi.Strip(l))
	}
}

func TestFSty4FormStylesUseThePalette(t *testing.T) {
	st := NewStyles()
	palette := []lipgloss.TerminalColor{colAccent, colTeal, colText, colMuted, colFaint, colErr}
	styles := map[string]lipgloss.Style{
		"FormTitle": st.FormTitle, "FormTitleBlur": st.FormTitleBlur, "FormHint": st.FormHint, "FormGutter": st.FormGutter,
		"FormPrompt": st.FormPrompt, "FormCursor": st.FormCursor, "FormValue": st.FormValue, "FormValueBlur": st.FormValueBlur,
		"FormPlaceholder": st.FormPlaceholder, "FormOptionOn": st.FormOptionOn, "FormOption": st.FormOption, "FormSection": st.FormSection,
	}
	for name, s := range styles {
		fg := s.GetForeground()
		ok := false
		for _, c := range palette {
			if fg == c {
				ok = true
			}
		}
		if !ok {
			t.Errorf("%s: foreground %v is not a palette color", name, fg)
		}
	}
}

func TestFSty5NoHuhInTheSource(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range af.Imports {
			if strings.Contains(imp.Path.Value, "charmbracelet/huh") {
				t.Errorf("%s imports %s", name, imp.Path.Value)
			}
		}
	}
	src, err := os.ReadFile("form.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"go func", "sync/atomic", "tea.Tick", "textinput.Blink", "CursorBlink"} {
		if strings.Contains(string(src), bad) {
			t.Errorf("form.go contains %q: the form must not start goroutines or timers", bad)
		}
	}
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "go ") {
			t.Errorf("form.go starts a goroutine: %q", line)
		}
	}
}

func TestFSty6HuhIsGoneFromTheModule(t *testing.T) {
	for _, name := range []string{"go.mod", "go.sum"} {
		b, err := os.ReadFile(filepath.Join("..", "..", name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "charmbracelet/huh") {
			t.Errorf("%s still mentions huh", name)
		}
	}
}

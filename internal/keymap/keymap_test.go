package keymap_test

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/keymap"
	"github.com/tinhnd1902/gitid/internal/testutil"
)

func init() { i18n.SetLang("vi") }

func parse(t *testing.T, y string) (keymap.Map, []keymap.Issue) {
	t.Helper()
	return keymap.Parse([]byte(y))
}

func keysOf(m keymap.Map, a keymap.Action) string { return strings.Join(m.Keys(a), ",") }

func codes(is []keymap.Issue) []string {
	var out []string
	for _, i := range is {
		out = append(out, i.Code)
	}
	return out
}

func assertDefaults(t *testing.T, m keymap.Map) {
	t.Helper()
	d := keymap.Defaults()
	for _, def := range keymap.Defs() {
		if !slices.Equal(m.Keys(def.Action), d.Keys(def.Action)) {
			t.Errorf("%s = %v, want default %v", def.Action, m.Keys(def.Action), d.Keys(def.Action))
		}
		if m.Custom(def.Action) {
			t.Errorf("%s marked custom", def.Action)
		}
	}
}

// ---- KM-1, KM-2 ----

func TestKM1DefaultsInvariant(t *testing.T) {
	snake := regexp.MustCompile(`^[a-z]+(_[a-z]+)*$`)
	seen := map[keymap.Action]bool{}
	for _, d := range keymap.Defs() {
		if !snake.MatchString(string(d.Action)) || seen[d.Action] {
			t.Errorf("bad or duplicate action name %q", d.Action)
		}
		seen[d.Action] = true
		if len(d.Default) == 0 || len(d.Scopes) == 0 || d.DescKey == "" {
			t.Errorf("%s: incomplete definition %+v", d.Action, d)
		}
		for _, k := range d.Default {
			if _, ok := keymap.ValidKey(k); !ok {
				t.Errorf("%s: default %q is not a valid key", d.Action, k)
			}
		}
	}
	for _, sc := range keymap.Scopes() {
		owner := map[string]keymap.Action{}
		for _, d := range keymap.Defs() {
			if !slices.Contains(d.Scopes, sc) {
				continue
			}
			for _, k := range d.Default {
				if o, dup := owner[k]; dup {
					t.Errorf("scope %s: %q is the default of %s and %s", sc, k, o, d.Action)
				}
				owner[k] = d.Action
				if strings.Contains(keymap.Reserved(sc), k) && len(k) == 1 {
					t.Errorf("scope %s: default %q of %s is reserved", sc, k, d.Action)
				}
			}
		}
	}
	// the plan's table has 25 actions (23, plus problems of the repo browser
	// and palette, the alias of the help overlay)
	if n := len(keymap.Defs()); n != 25 {
		t.Errorf("%d actions", n)
	}
}

func TestKM2DescriptionsTranslated(t *testing.T) {
	for _, d := range keymap.Defs() {
		for _, lang := range []string{"vi", "en"} {
			if v, ok := i18n.Messages(lang)[d.DescKey]; !ok || v == "" {
				t.Errorf("%s: %s missing in %s", d.Action, d.DescKey, lang)
			}
		}
	}
	for _, c := range keymap.AllIssueCodes() {
		for _, lang := range []string{"vi", "en"} {
			if _, ok := i18n.Messages(lang)[c]; !ok {
				t.Errorf("issue code %s missing in %s", c, lang)
			}
		}
	}
}

func TestDefsReturnsCopy(t *testing.T) {
	d := keymap.Defs()
	d[0].Default[0] = "Z"
	d[0].Scopes[0] = "zz"
	if keymap.Defs()[0].Default[0] == "Z" || keymap.Defs()[0].Scopes[0] == "zz" {
		t.Fatal("Defs exposes the registry")
	}
	m := keymap.Defaults()
	m.Keys(keymap.Quit)[0] = "Z"
	if m.Label(keymap.Quit) != "q" {
		t.Fatal("Keys exposes the map")
	}
	var zero keymap.Map
	if zero.Label(keymap.Check) != "k" || zero.Custom(keymap.Check) {
		t.Fatal("zero Map is not the defaults")
	}
}

// ---- KM-3, KM-4 ----

func TestKM3ValidKey(t *testing.T) {
	for in, want := range map[string]string{
		"v": "v", "V": "V", "/": "/", "?": "?", "5": "5", "ctrl+x": "ctrl+x", "CTRL+X": "ctrl+x",
		"alt+v": "alt+v", "alt+V": "alt+V", "ALT+V": "alt+V", "F5": "f5", " v ": "v", "f12": "f12", "f1": "f1",
		"ctrl+s": "ctrl+s", "~": "~", "!": "!", "alt+?": "alt+?",
	} {
		got, ok := keymap.ValidKey(in)
		if !ok || got != want {
			t.Errorf("ValidKey(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestKM4InvalidKey(t *testing.T) {
	for _, in := range []string{
		"", " ", "â", "đ", "ab", "enter", "esc", "tab", "shift+tab", "up", "pgdown", "space",
		"ctrl+c", "ctrl+h", "ctrl+i", "ctrl+m", "ctrl+p", "ctrl+n", "ctrl+b", "ctrl+f", "ctrl+", "alt+", "alt+ab",
		"f0", "f13", "\x1b[2J", "\x1b", "\t", "ctrl+1", "ctrl+ab", "alt+â", "alt+ ", "home", "end", "pgup", "backspace", "delete",
	} {
		if got, ok := keymap.ValidKey(in); ok {
			t.Errorf("ValidKey(%q) = %q, want invalid", in, got)
		}
	}
}

// ---- KM-5 .. KM-21 ----

func TestKM5Single(t *testing.T) {
	m, is := parse(t, "check: v\n")
	if len(is) != 0 || keysOf(m, keymap.Check) != "v" || !m.Custom(keymap.Check) || m.Label(keymap.Check) != "v" {
		t.Fatalf("%v %v", m.Keys(keymap.Check), is)
	}
	if m.Label(keymap.Clone) != "c" || m.Custom(keymap.Clone) || m.CustomCount() != 1 {
		t.Fatal("other actions changed")
	}
}

func TestKM6ClonePlusN(t *testing.T) {
	m, is := parse(t, "clone: [c, n]\n")
	if len(is) != 0 || keysOf(m, keymap.Clone) != "c,n" {
		t.Fatalf("%v %v", m.Keys(keymap.Clone), is)
	}
}

func TestKM6bCloneCR(t *testing.T) {
	m, is := parse(t, "clone: [c, r]\n")
	if keysOf(m, keymap.Clone) != "c" || len(is) != 1 {
		t.Fatalf("%v %v", m.Keys(keymap.Clone), is)
	}
	if is[0].Code != keymap.ErrConflict || is[0].Action != "clone" || is[0].Key != "r" || is[0].Other != "reload" || is[0].Scope != keymap.ScopeList {
		t.Fatalf("%+v", is[0])
	}
}

func TestKM7OrderAndLabel(t *testing.T) {
	m, is := parse(t, "clone: [x, c]\n")
	if len(is) != 0 || keysOf(m, keymap.Clone) != "x,c" || m.Label(keymap.Clone) != "x" {
		t.Fatalf("%v %v", m.Keys(keymap.Clone), is)
	}
}

func TestKM8Duplicates(t *testing.T) {
	m, is := parse(t, "clone: [c, c, x]\n")
	if len(is) != 0 || keysOf(m, keymap.Clone) != "c,x" {
		t.Fatalf("%v %v", m.Keys(keymap.Clone), is)
	}
}

func TestKM9Conflict(t *testing.T) {
	m, is := parse(t, "check: c\n")
	if keysOf(m, keymap.Check) != "k" || keysOf(m, keymap.Clone) != "c" || m.Custom(keymap.Check) {
		t.Fatal("conflict not reverted")
	}
	if len(is) != 1 {
		t.Fatalf("%+v", is)
	}
	want := keymap.Issue{Code: keymap.ErrConflict, Action: "check", Key: "c", Other: "clone", Scope: keymap.ScopeList, Default: "k"}
	if is[0] != want {
		t.Fatalf("%+v\nwant %+v", is[0], want)
	}
	if msg := is[0].Message(); !strings.Contains(msg, "check") || !strings.Contains(msg, "clone") || !strings.Contains(msg, `"c"`) {
		t.Fatalf("message %q", msg)
	}
}

func TestKM10Swap(t *testing.T) {
	// clone and edit swap keys (check and copy share the repos scope since the
	// repo browser, so a swap with check would conflict there)
	m, is := parse(t, "clone: e\nedit: c\n")
	if len(is) != 0 || keysOf(m, keymap.Clone) != "e" || keysOf(m, keymap.Edit) != "c" {
		t.Fatalf("%v %v %v", m.Keys(keymap.Clone), m.Keys(keymap.Edit), is)
	}
}

func TestKM11Cascade(t *testing.T) {
	m, is := parse(t, "clone: x\ncheck: x\nedit: c\n")
	assertDefaults(t, m)
	if len(is) != 3 {
		t.Fatalf("%+v", is)
	}
	var got []string
	for _, i := range is {
		got = append(got, i.Action+":"+i.Code+":"+i.Other)
	}
	want := []string{"edit:keys.err.conflict:clone", "clone:keys.err.conflict:check", "check:keys.err.conflict:clone"}
	if !slices.Equal(got, want) {
		t.Fatalf("%v\nwant %v", got, want)
	}
}

func TestKM12Reserved(t *testing.T) {
	for _, tc := range []struct {
		yaml  string
		scope keymap.Scope
		act   string
	}{
		{"back: h\n", keymap.ScopeDialog, "back"},
		{"yes: l\n", keymap.ScopeDialog, "yes"},
		{"back: j\n", keymap.ScopeForm, "back"},
	} {
		m, is := parse(t, tc.yaml)
		assertDefaults(t, m)
		if len(is) != 1 || is[0].Code != keymap.ErrReserved || is[0].Scope != tc.scope || is[0].Action != tc.act {
			t.Errorf("%q: %+v", tc.yaml, is)
		}
	}
}

func TestKM13ReservedOnlyInOwnScopes(t *testing.T) {
	m, is := parse(t, "yes: j\n")
	if len(is) != 0 || keysOf(m, keymap.Yes) != "j" {
		t.Fatalf("%v %v", m.Keys(keymap.Yes), is)
	}
}

func TestKM14Unknown(t *testing.T) {
	m, is := parse(t, "chekc: v\n")
	assertDefaults(t, m)
	if len(is) != 1 || is[0].Code != keymap.ErrUnknownAction || is[0].Action != "chekc" {
		t.Fatalf("%+v", is)
	}
}

func TestKM15BadValue(t *testing.T) {
	for _, y := range []string{"check: {a: b}\n", "check: [[v]]\n", "check: null\n", "check:\n", "check: [v, {a: b}]\n", "check: [v, null]\n"} {
		m, is := parse(t, y)
		assertDefaults(t, m)
		if len(is) != 1 || is[0].Code != keymap.ErrBadValue || is[0].Action != "check" {
			t.Errorf("%q: %+v", y, is)
		}
	}
}

func TestKM16Empty(t *testing.T) {
	for _, y := range []string{`check: ""`, "check: []", "check: ''"} {
		m, is := parse(t, y)
		assertDefaults(t, m)
		if len(is) != 1 || is[0].Code != keymap.ErrEmpty {
			t.Errorf("%q: %+v", y, is)
		}
	}
}

func TestKM17InvalidKey(t *testing.T) {
	m, is := parse(t, "check: [v, â]\n")
	assertDefaults(t, m)
	if len(is) != 1 || is[0].Code != keymap.ErrInvalidKey || is[0].Key != "â" || keysOf(m, keymap.Check) != "k" {
		t.Fatalf("%+v", is)
	}
	m, is = parse(t, "check: ' '\n")
	assertDefaults(t, m)
	if len(is) != 1 || is[0].Code != keymap.ErrInvalidKey {
		t.Fatalf("%+v", is)
	}
}

func TestKM18TopLevelShape(t *testing.T) {
	for _, y := range []string{"oops\n", "[a, b]\n", "- a\n- b\n", "42\n"} {
		m, is := parse(t, y)
		assertDefaults(t, m)
		if len(is) != 1 || is[0].Code != keymap.ErrSection {
			t.Errorf("%q: %+v", y, is)
		}
	}
}

func TestKM19EmptyInputs(t *testing.T) {
	for _, y := range []string{"", "\n", "~\n", "null\n", "{}\n", "# only a comment\n", "# a\n# b\n", "---\n", "--- {}\n"} {
		m, is := parse(t, y)
		assertDefaults(t, m)
		if len(is) != 0 {
			t.Errorf("%q: %+v", y, is)
		}
	}
	m, is := keymap.Resolve(nil)
	assertDefaults(t, m)
	if len(is) != 0 {
		t.Fatal(is)
	}
}

func TestKM20ScalarTags(t *testing.T) {
	m, is := parse(t, "retry: 5\n")
	if len(is) != 0 || keysOf(m, keymap.Retry) != "5" {
		t.Fatalf("%v %v", m.Keys(keymap.Retry), is)
	}
	m, is = parse(t, "yes: true\n")
	if len(is) != 1 || is[0].Code != keymap.ErrInvalidKey || keysOf(m, keymap.Yes) != "y" {
		t.Fatalf("%v %v", m.Keys(keymap.Yes), is)
	}
	m, is = parse(t, "no: n\n")
	if len(is) != 0 || keysOf(m, keymap.No) != "n" {
		t.Fatalf("%v %v", m.Keys(keymap.No), is)
	}
}

func TestKM21Alias(t *testing.T) {
	// (check and fix share the repos scope, so the alias goes to retry)
	m, is := parse(t, "check: &k v\nretry: *k\n")
	if len(is) != 0 || keysOf(m, keymap.Check) != "v" || keysOf(m, keymap.Retry) != "v" {
		t.Fatalf("%v %v %v", m.Keys(keymap.Check), m.Keys(keymap.Retry), is)
	}
	// an alias to a non-key is a normal bad value, not a crash
	_, is = parse(t, "a: &x {b: c}\ncheck: *x\n")
	if len(is) != 2 {
		t.Fatalf("%+v", is)
	}
}

// ---- KM-22: determinism ----

func TestKM22Determinism(t *testing.T) {
	lines := []string{
		"clone: x", "check: x", "edit: c", "zeta: 1", "alpha: 2", "back: h", "yes: [j, ab]", "retry: [r, 5]",
		"doctor: \"\"", "fix: {a: b}", "filter: ctrl+x", "apply: [p, P]", "open: zz", "quit: \"?\"",
	}
	var first keymap.Map
	var firstIssues []keymap.Issue
	rng := rand.New(rand.NewSource(1))
	for n := 0; n < 50; n++ {
		rng.Shuffle(len(lines), func(i, j int) { lines[i], lines[j] = lines[j], lines[i] })
		m, is := parse(t, strings.Join(lines, "\n")+"\n")
		if n == 0 {
			first, firstIssues = m, is
			continue
		}
		for _, d := range keymap.Defs() {
			if !slices.Equal(m.Keys(d.Action), first.Keys(d.Action)) || m.Custom(d.Action) != first.Custom(d.Action) {
				t.Fatalf("run %d: %s differs", n, d.Action)
			}
		}
		if !reflect.DeepEqual(is, firstIssues) {
			t.Fatalf("run %d: issues differ:\n%+v\n%+v", n, is, firstIssues)
		}
	}
	if len(firstIssues) < 8 {
		t.Fatalf("expected many issues, got %+v", firstIssues)
	}
	// repeated resolution of one input
	for n := 0; n < 50; n++ {
		_, is := parse(t, strings.Join(lines, "\n"))
		if !reflect.DeepEqual(is, firstIssues) {
			t.Fatal("issues differ between runs")
		}
	}
}

// ---- KM-23: robustness ----

func corpus() map[string]string {
	var big strings.Builder
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&big, "action%d: x\n", i)
	}
	var nested strings.Builder
	nested.WriteString("check: ")
	nested.WriteString(strings.Repeat("[", 2000))
	nested.WriteString(strings.Repeat("]", 2000))
	nested.WriteString("\n")
	return map[string]string{
		"1000 entries":   big.String(),
		"10KB value":     "check: " + strings.Repeat("v", 10_000) + "\n",
		"deep nesting":   nested.String(),
		"anchors":        "a: &a [*a]\ncheck: *a\n",
		"anchor map":     "a: &a {b: *a}\ncheck: *a\n",
		"merge":          "base: &b {check: v}\n<<: *b\n",
		"control action": "\"che\\x1b[2Jck\": v\n\"\\x00\": v\n",
		"escape key":     "check: \"\\x1b[2J\"\n",
		"tags":           "check: !!binary dg==\nfix: !!set {a, b}\nyes: !!int 5\nno: !custom x\n",
		"complex key":    "? [a, b]\n: v\n? {x: y}\n: w\ncheck: v\n",
		"binary":         "\x00\x01\x02\xff\xfe",
		"tabs":           "\tcheck: v\n",
		"unicode":        "check: \"🙂\"\nfix: \"ｖ\"\n",
		"doc stream":     "check: v\n---\nfix: w\n",
		"long sequence":  "check: [" + strings.Repeat("v,", 5000) + "v]\n",
		"scalar doc":     "just text",
		"only anchor":    "&a\n",
		"flow garbage":   "{{{{[[[[",
	}
}

func TestKM23Robustness(t *testing.T) {
	for name, y := range corpus() {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: panic %v", name, r)
				}
			}()
			m, is := keymap.Parse([]byte(y))
			for _, i := range is {
				msg := i.Message()
				if strings.ContainsRune(msg, 0x1b) {
					t.Errorf("%s: raw ESC in %q", name, msg)
				}
				for _, s := range []string{i.Action, i.Key} {
					if n := len([]rune(s)); n > 24 {
						t.Errorf("%s: echoed string of %d runes", name, n)
					}
				}
				if n := len([]rune(i.Detail)); n > 121 {
					t.Errorf("%s: detail of %d runes", name, n)
				}
				if !slices.Contains(keymap.AllIssueCodes(), i.Code) {
					t.Errorf("%s: unknown code %s", name, i.Code)
				}
			}
			if len(is) > 1001 {
				t.Errorf("%s: %d issues", name, len(is))
			}
			// every effective key is valid and conflict free
			for _, d := range keymap.Defs() {
				for _, k := range m.Keys(d.Action) {
					if _, ok := keymap.ValidKey(k); !ok {
						t.Errorf("%s: %s bound to invalid key %q", name, d.Action, k)
					}
				}
			}
			checkNoConflicts(t, m)
		}()
	}
}

func checkNoConflicts(t *testing.T, m keymap.Map) {
	t.Helper()
	for _, sc := range keymap.Scopes() {
		owner := map[string]keymap.Action{}
		for _, d := range keymap.Defs() {
			if !slices.Contains(d.Scopes, sc) {
				continue
			}
			for _, k := range m.Keys(d.Action) {
				if o, dup := owner[k]; dup {
					t.Errorf("scope %s: %q bound to %s and %s", sc, k, o, d.Action)
				}
				owner[k] = d.Action
				if len(k) == 1 && strings.Contains(keymap.Reserved(sc), k) {
					t.Errorf("scope %s: %s uses reserved %q", sc, d.Action, k)
				}
			}
		}
	}
}

func TestResolvedMapsNeverConflict(t *testing.T) {
	// every pair of single-key bindings of the 25 actions, resolved together
	letters := []string{"a", "c", "k", "q", "r", "y", "n", "h", "j", "x", "?", "/"}
	rng := rand.New(rand.NewSource(7))
	defs := keymap.Defs()
	for n := 0; n < 400; n++ {
		var b strings.Builder
		for _, d := range defs {
			if rng.Intn(3) == 0 {
				fmt.Fprintf(&b, "%s: %q\n", d.Action, letters[rng.Intn(len(letters))])
			}
		}
		m, is := parse(t, b.String())
		checkNoConflicts(t, m)
		for _, i := range is {
			if i.Message() == i.Code {
				t.Fatalf("issue %+v has no translation", i)
			}
		}
	}
}

// ---- KM-24 ----

func TestKM24Example(t *testing.T) {
	ex := keymap.Example("~/.config/gitid/keys.yaml")
	m, is := keymap.Parse([]byte(ex))
	assertDefaults(t, m)
	if len(is) != 0 {
		t.Fatalf("%+v", is)
	}
	if !strings.Contains(ex, "~/.config/gitid/keys.yaml") {
		t.Fatal("no path in the header")
	}
	for _, d := range keymap.Defs() {
		if !strings.Contains(ex, string(d.Action)+":") {
			t.Errorf("%s missing from the example", d.Action)
		}
	}
	// uncommenting every action line changes nothing
	var lines []string
	n := 0
	for _, l := range strings.Split(ex, "\n") {
		if strings.HasPrefix(l, "# ") {
			l = strings.TrimPrefix(l, "# ")
			n++
		}
		lines = append(lines, l)
	}
	if n != len(keymap.Defs()) {
		t.Fatalf("%d action lines", n)
	}
	m, is = keymap.Parse([]byte(strings.Join(lines, "\n")))
	if len(is) != 0 {
		t.Fatalf("%+v", is)
	}
	for _, d := range keymap.Defs() {
		if !slices.Equal(m.Keys(d.Action), d.Default) {
			t.Errorf("%s = %v want %v", d.Action, m.Keys(d.Action), d.Default)
		}
	}
	// stripping every "# " anywhere keeps it valid too
	m, is = keymap.Parse([]byte(strings.ReplaceAll(ex, "# ", "")))
	if len(is) != 0 {
		t.Fatalf("%+v", is)
	}
	for _, d := range keymap.Defs() {
		if !slices.Equal(m.Keys(d.Action), d.Default) {
			t.Errorf("%s = %v want %v", d.Action, m.Keys(d.Action), d.Default)
		}
	}
}

// ---- KF: file loading ----

func writeKeys(t *testing.T, e env.Env, content string) string {
	t.Helper()
	p := e.Paths().KeyBindingsFile
	testutil.WriteFile(t, p, content, 0o644)
	return p
}

func TestKF2Missing(t *testing.T) {
	e := testutil.TempEnv(t)
	m, is := keymap.Load(e)
	assertDefaults(t, m)
	if len(is) != 0 {
		t.Fatal(is)
	}
	if _, err := os.Stat(e.Paths().KeyBindingsFile); !os.IsNotExist(err) {
		t.Fatalf("Load created the file: %v", err)
	}
	if _, err := os.Stat(e.Paths().ConfigDir); !os.IsNotExist(err) {
		t.Fatalf("Load created the config dir: %v", err)
	}
}

func TestKF3BlockStyle(t *testing.T) {
	e := testutil.TempEnv(t)
	writeKeys(t, e, "check: v\nclone: [c, n]\n")
	m, is := keymap.Load(e)
	if len(is) != 0 || keysOf(m, keymap.Check) != "v" || keysOf(m, keymap.Clone) != "c,n" {
		t.Fatalf("%v %v %v", m.Keys(keymap.Check), m.Keys(keymap.Clone), is)
	}
}

func TestKF4FlowStyleWithComments(t *testing.T) {
	e := testutil.TempEnv(t)
	writeKeys(t, e, "# phím tắt của tôi\n{check: v}  # kiểm tra repo\n")
	m, is := keymap.Load(e)
	if len(is) != 0 || keysOf(m, keymap.Check) != "v" {
		t.Fatalf("%v %v", m.Keys(keymap.Check), is)
	}
}

func TestKF5ParseErrors(t *testing.T) {
	for name, y := range map[string]string{
		"unclosed":  "check: [v\n",
		"indent":    "check: v\n  fix: w\n clone: x\n",
		"duplicate": "check: v\ncheck: w\n",
		"tab":       "check:\n\t- v\n",
	} {
		m, is := parse(t, y)
		assertDefaults(t, m)
		if len(is) != 1 || is[0].Code != keymap.ErrParse {
			t.Errorf("%s: %+v", name, is)
			continue
		}
		if !strings.Contains(is[0].Detail, "line") && !strings.Contains(is[0].Detail, "dòng") {
			t.Errorf("%s: detail without a line: %q", name, is[0].Detail)
		}
		if strings.ContainsRune(is[0].Message(), 0x1b) {
			t.Errorf("%s: raw ESC", name)
		}
	}
}

func TestKF6ReadErrors(t *testing.T) {
	e := testutil.TempEnv(t)
	if err := os.MkdirAll(e.Paths().KeyBindingsFile, 0o755); err != nil {
		t.Fatal(err)
	}
	m, is := keymap.Load(e)
	assertDefaults(t, m)
	if len(is) != 1 || is[0].Code != keymap.ErrRead {
		t.Fatalf("directory: %+v", is)
	}

	if os.Geteuid() == 0 {
		t.Skip("running as root: mode 0000 is readable")
	}
	e2 := testutil.TempEnv(t)
	p := writeKeys(t, e2, "check: v\n")
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
	m, is = keymap.Load(e2)
	assertDefaults(t, m)
	if len(is) != 1 || is[0].Code != keymap.ErrRead {
		t.Fatalf("mode 0: %+v", is)
	}
}

func TestKF7TooLarge(t *testing.T) {
	head := "check: v\n"
	at := head + strings.Repeat("#", keymap.MaxFileSize-len(head)) // exactly the limit
	e := testutil.TempEnv(t)
	writeKeys(t, e, at+"#")
	m, is := keymap.Load(e)
	assertDefaults(t, m)
	if len(is) != 1 || is[0].Code != keymap.ErrRead {
		t.Fatalf("%+v", is)
	}
	if !strings.Contains(is[0].Message(), "65536") {
		t.Fatalf("message without the limit: %q", is[0].Message())
	}
	// exactly at the limit is fine
	e2 := testutil.TempEnv(t)
	writeKeys(t, e2, at)
	m, is = keymap.Load(e2)
	if len(is) != 0 || keysOf(m, keymap.Check) != "v" {
		t.Fatalf("%v %+v", m.Keys(keymap.Check), is)
	}
	// Parse applies the same cap to bytes handed over directly
	_, is = keymap.Parse([]byte(at + "#"))
	if len(is) != 1 || is[0].Code != keymap.ErrRead {
		t.Fatalf("%+v", is)
	}
}

func TestKF9Symlink(t *testing.T) {
	e := testutil.TempEnv(t)
	real := filepath.Join(e.Home, "dotfiles", "keys.yaml")
	testutil.WriteFile(t, real, "check: v\n", 0o644)
	p := e.Paths().KeyBindingsFile
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, p); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	m, is := keymap.Load(e)
	if len(is) != 0 || keysOf(m, keymap.Check) != "v" {
		t.Fatalf("%v %v", m.Keys(keymap.Check), is)
	}
}

func TestKF8FIFONoHang(t *testing.T) {
	e := testutil.TempEnv(t)
	fifo := filepath.Join(e.Home, "fifo")
	if err := mkfifo(fifo); err != nil {
		t.Skip("mkfifo unavailable:", err)
	}
	p := e.Paths().KeyBindingsFile
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fifo, p); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	done := make(chan []keymap.Issue, 1)
	go func() { _, is := keymap.Load(e); done <- is }()
	select {
	case is := <-done:
		if len(is) != 1 || is[0].Code != keymap.ErrRead {
			t.Fatalf("%+v", is)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Load hangs on a FIFO")
	}
}

func TestKF10CorpusAsFiles(t *testing.T) {
	for name, y := range corpus() {
		e := testutil.TempEnv(t)
		writeKeys(t, e, y)
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: panic %v", name, r)
				}
			}()
			m, _ := keymap.Load(e)
			checkNoConflicts(t, m)
		}()
	}
}

func TestLoadIsReadOnly(t *testing.T) {
	e := testutil.TempEnv(t)
	p := writeKeys(t, e, "check: [v\n")
	before := testutil.Snapshot(t, e.Home)
	keymap.Load(e)
	keymap.Load(e)
	testutil.AssertSame(t, before, testutil.Snapshot(t, e.Home))
	if b, _ := os.ReadFile(p); string(b) != "check: [v\n" {
		t.Fatal("file changed")
	}
}

func TestIssueMessagesEnglishAndVietnamese(t *testing.T) {
	t.Cleanup(func() { i18n.SetLang("vi") })
	_, is := parse(t, "check: c\nchekc: v\nfix: â\nback: h\nretry: []\nedit: {a: b}\n")
	if len(is) != 6 {
		t.Fatalf("%+v", is)
	}
	seen := map[string]bool{}
	for _, lang := range []string{"vi", "en"} {
		i18n.SetLang(lang)
		for _, i := range is {
			msg := i.Message()
			if strings.Contains(msg, "%!") || msg == i.Code || strings.Contains(msg, "keys.err.") {
				t.Errorf("%s %s: %q", lang, i.Code, msg)
			}
			seen[lang+msg] = true
		}
	}
	if len(seen) != 12 {
		t.Errorf("messages are not distinct: %d", len(seen))
	}
}

// D2: only keys that differ from the default count as custom.
func TestCustomOnlyWhenDifferent(t *testing.T) {
	ex := keymap.Example("~/.config/gitid/keys.yaml")
	var lines []string
	for _, l := range strings.Split(ex, "\n") {
		lines = append(lines, strings.TrimPrefix(l, "# "))
	}
	m, is := keymap.Parse([]byte(strings.Join(lines, "\n")))
	if len(is) != 0 || m.CustomCount() != 0 {
		t.Fatalf("uncommented example: %d custom, %+v", m.CustomCount(), is)
	}
	for _, d := range keymap.Defs() {
		if m.Custom(d.Action) {
			t.Errorf("%s marked custom", d.Action)
		}
	}
	// the same set in another order is not custom (clone default is c, n or similar)
	for _, d := range keymap.Defs() {
		if len(d.Default) < 2 {
			continue
		}
		rev := slices.Clone(d.Default)
		slices.Reverse(rev)
		m, is := parse(t, string(d.Action)+": ["+strings.Join(rev, ", ")+"]\n")
		if len(is) != 0 || m.Custom(d.Action) {
			t.Errorf("%s reordered: custom=%v %+v", d.Action, m.Custom(d.Action), is)
		}
	}
	m, _ = parse(t, "check: v\nfix: f\n")
	if !m.Custom("check") || m.Custom("fix") || m.CustomCount() != 1 {
		t.Errorf("custom = %d", m.CustomCount())
	}
}

// D3: Escape.
func TestEscape(t *testing.T) {
	if got := keymap.Escape("a\x1b[2J\x07\x7f\u0085z"); got != `a\x1b[2J\x07\x7f\u0085z` {
		t.Errorf("%q", got)
	}
	is := keymap.Issue{Action: "\x1b[1m", Key: "\x1b"}
	if is.SafeAction() != `\x1b[1m` || is.SafeKey() != `\x1b` {
		t.Errorf("%q %q", is.SafeAction(), is.SafeKey())
	}
}

// D5: a YAML syntax error shows a localized generic text and the line only.
func TestParseErrorIsLocalized(t *testing.T) {
	for _, lang := range []string{"vi", "en"} {
		i18n.SetLang(lang)
		_, is := keymap.Parse([]byte("check: [v\n"))
		i18n.SetLang("vi")
		if len(is) != 1 {
			t.Fatalf("%+v", is)
		}
		d := is[0].Detail
		if strings.Contains(d, "expected") || strings.Contains(d, "did not find") {
			t.Errorf("%s: raw yaml text: %q", lang, d)
		}
		if !strings.Contains(d, "1") {
			t.Errorf("%s: no line number: %q", lang, d)
		}
	}
}

func TestExampleFits80Columns(t *testing.T) {
	for i, l := range strings.Split(keymap.Example("~/.config/gitid/keys.yaml"), "\n") {
		if n := len([]rune(l)); n > 80 {
			t.Errorf("line %d has %d columns: %q", i+1, n, l)
		}
	}
}

// ---- repo browser (K-1 .. K-6) ----

// K-1: the repos scope holds the documented actions with unique defaults.
func TestK1ReposScope(t *testing.T) {
	var keysSeen []string
	owner := map[string]keymap.Action{}
	for _, d := range keymap.Defs() {
		if !slices.Contains(d.Scopes, keymap.ScopeRepos) {
			continue
		}
		for _, k := range d.Default {
			if o, dup := owner[k]; dup {
				t.Errorf("repos: %q is the default of %s and %s", k, o, d.Action)
			}
			owner[k] = d.Action
			keysSeen = append(keysSeen, k)
		}
	}
	slices.Sort(keysSeen)
	want := []string{"!", "/", "?", "c", "ctrl+k", "f", "k", "q", "r"}
	if !slices.Equal(keysSeen, want) {
		t.Fatalf("repos defaults %v, want %v", keysSeen, want)
	}
	if !slices.Contains(keymap.Scopes(), keymap.ScopeRepos) {
		t.Fatal("repos is not in Scopes()")
	}
	if keymap.DefaultKeys(keymap.Problems)[0] != "!" || !keymap.Known("problems") {
		t.Fatal("problems action")
	}
}

// K-2: problems can be rebound.
func TestK2ProblemsRebound(t *testing.T) {
	m, is := parse(t, "problems: x\n")
	if len(is) != 0 || m.Label(keymap.Problems) != "x" || !m.Custom(keymap.Problems) {
		t.Fatalf("%v %v", m.Keys(keymap.Problems), is)
	}
}

// K-3: copy on k conflicts with check in the repos scope; both keep defaults.
func TestK3CopyConflictsWithCheck(t *testing.T) {
	m, is := parse(t, "copy: k\n")
	if keysOf(m, keymap.Copy) != "c" || keysOf(m, keymap.Check) != "k" {
		t.Fatal("conflict not reverted")
	}
	if len(is) != 1 || is[0].Code != keymap.ErrConflict || is[0].Scope != keymap.ScopeRepos || is[0].Other != "check" {
		t.Fatalf("%+v", is)
	}
}

// K-4: the example lists problems and parses back to the defaults.
func TestK4ExampleHasProblems(t *testing.T) {
	ex := keymap.Example("~/.config/gitid/keys.yaml")
	if !strings.Contains(ex, `problems: "!"`) {
		t.Fatalf("no problems line:\n%s", ex)
	}
	if !strings.Contains(ex, "repos") {
		t.Fatal("no repos scope in the example")
	}
}

// K-5: the sentinel space (alt+a..z, alt+A..Z) holds every action.
func TestK5SentinelKeysFit(t *testing.T) {
	if n := len(keymap.Defs()); n > 52 {
		t.Fatalf("%d actions do not fit 52 sentinel keys", n)
	}
	for _, k := range []string{"alt+A", "alt+Z", "alt+a", "alt+z"} {
		if got, ok := keymap.ValidKey(k); !ok || got != k {
			t.Errorf("ValidKey(%q) = %q, %v", k, got, ok)
		}
	}
}

// K-6: the problems description exists in both languages.
func TestK6ProblemsTranslated(t *testing.T) {
	for _, lang := range []string{"vi", "en"} {
		if v := i18n.Messages(lang)["tui.key.problems"]; v == "" {
			t.Errorf("tui.key.problems missing in %s", lang)
		}
	}
}

// PK-1: palette is the last action, bound to ctrl+k in the scopes of help.
func TestPK1PaletteDef(t *testing.T) {
	defs := keymap.Defs()
	last := defs[len(defs)-1]
	if last.Action != keymap.Palette || string(last.Action) != "palette" || !slices.Equal(last.Default, []string{"ctrl+k"}) || last.DescKey != "tui.key.palette" {
		t.Fatalf("last def %+v", last)
	}
	var help keymap.Def
	for _, d := range defs {
		if d.Action == keymap.Help {
			help = d
		}
	}
	if !slices.Equal(last.Scopes, help.Scopes) {
		t.Fatalf("palette scopes %v, help scopes %v", last.Scopes, help.Scopes)
	}
}

// PK-2: no other action shares ctrl+k with palette in any scope.
func TestPK2NoDefaultSharesCtrlK(t *testing.T) {
	for _, d := range keymap.Defs() {
		if d.Action == keymap.Palette {
			continue
		}
		if slices.Contains(d.Default, "ctrl+k") {
			t.Errorf("%s defaults to ctrl+k", d.Action)
		}
	}
	if k, ok := keymap.ValidKey("ctrl+k"); !ok || k != "ctrl+k" {
		t.Error("ctrl+k must be a valid key")
	}
}

// PK-4: another action on a key the palette shares a scope with is refused.
func TestPK4PaletteConflict(t *testing.T) {
	m, is := parse(t, "palette: k\n")
	if keysOf(m, keymap.Palette) != "ctrl+k" || m.Custom(keymap.Palette) {
		t.Fatalf("palette keys %v", m.Keys(keymap.Palette))
	}
	if len(is) != 1 || is[0].Code != keymap.ErrConflict || is[0].Scope != keymap.ScopeList || is[0].Other != "check" {
		t.Fatalf("%+v", is)
	}
}

// PK-5: an old keys.yaml that binds ctrl+k to another action conflicts with
// the new default (documented in the changelog); the other action reverts.
func TestPK5OldCtrlKBindingReverts(t *testing.T) {
	m, is := parse(t, "check: ctrl+k\n")
	if keysOf(m, keymap.Check) != "k" || keysOf(m, keymap.Palette) != "ctrl+k" {
		t.Fatalf("check %v palette %v", m.Keys(keymap.Check), m.Keys(keymap.Palette))
	}
	if len(is) != 1 || is[0].Code != keymap.ErrConflict || is[0].Action != "check" || is[0].Other != "palette" {
		t.Fatalf("%+v", is)
	}
}

// PK-6: the example lists the palette.
func TestPK6ExampleListsPalette(t *testing.T) {
	if ex := keymap.Example("~/x/keys.yaml"); !strings.Contains(ex, `palette: "ctrl+k"`) {
		t.Fatalf("no palette line in:\n%s", ex)
	}
}

// PK-7: the descriptions exist in both languages.
func TestPK7PaletteTranslated(t *testing.T) {
	defer i18n.SetLang("vi")
	for _, lang := range []string{"vi", "en"} {
		i18n.SetLang(lang)
		for _, k := range []string{"tui.key.palette", "tui.key.run", "tui.key.close", "tui.help.row_this", "tui.help.row_step",
			"tui.help.row_always", "tui.help.filter_hint", "tui.help.close_filter", "tui.help.no_match", "tui.list.panel_hint"} {
			if got := i18n.T(k, "x"); got == k || strings.TrimSpace(got) == "" {
				t.Errorf("%s: %q is not translated", lang, k)
			}
		}
	}
}

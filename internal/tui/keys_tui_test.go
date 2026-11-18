package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/keymap"
	"github.com/tinhnd1902/gitid/internal/testutil"
)

// Tests of the TUI with a keys.yaml (TK-*), of the messages that name keys
// (I18N-2 .. I18N-5) and of the keymap in the stale-config flows.

// noEffect fails when pressing key changes anything the user can see: the
// stack, the frame (with the status), the overlay, the quit flag.
func noEffect(t *testing.T, h *harness, k string) {
	t.Helper()
	before := h.snapshot()
	h.key(k)
	if h.snapshot() != before {
		t.Fatalf("key %q has an effect:\n%s", k, h.view())
	}
}

// keysStatus is the status text the keys.yaml problem announcement has for n
// problems.
func keysStatus(n int) string { return tr("tui.status.keys_invalid", n) }

func TestTK0BadKeysYAMLNeverBlocksTheStart(t *testing.T) {
	// a file that is not YAML: the list renders, the default keys work
	h := newHarnessKeys(t, basicCfg(), "check: [v\n")
	if _, ok := h.top().(*listScreen); !ok || h.depth() != 1 {
		t.Fatalf("top is %T", h.top())
	}
	if !strings.Contains(h.status(), keysStatus(1)) {
		t.Fatalf("status %q, want %q", h.status(), keysStatus(1))
	}
	h.mustContain(tr("tui.list.panel", 3, 5))
	h.key("k")
	if _, ok := h.top().(*checkScreen); !ok {
		t.Fatalf("k did not open the check, top is %T", h.top())
	}

	// config.yaml that cannot be loaded: a valid keys.yaml still applies
	h = buildHarness(t, nil, func(e env.Env) {
		testutil.WriteFile(t, e.Paths().ConfigFile, "version: [\n", 0o600)
		testutil.WriteFile(t, e.Paths().KeyBindingsFile, "check: v\n", 0o644)
	})
	if h.m.sh.loaded {
		t.Fatal("the broken config.yaml loaded")
	}
	if got := h.m.sh.km.keys.Label(keymap.Check); got != "v" {
		t.Fatalf("check label %q with an unloadable config.yaml, want v", got)
	}
	if !h.m.sh.km.Check.Enabled() || h.m.sh.km.Check.Keys()[0] != "v" {
		t.Fatalf("check binding %v", h.m.sh.km.Check.Keys())
	}
}

func TestTK1To3CheckOnV(t *testing.T) {
	h := newHarnessKeys(t, oneAccCfg(), "check: v\n")
	h.apply()
	h.selectKey("personal/tinhnd")

	// TK-3: the help bar names v
	bar := plain(h.helpLine())
	if !strings.Contains(bar, "v "+tr("tui.key.check")) || strings.Contains(bar, "k "+tr("tui.key.check")) {
		t.Fatalf("help bar %q", bar)
	}
	// TK-1: k does nothing
	noEffect(t, h, "k")
	if h.depth() != 1 {
		t.Fatalf("depth %d", h.depth())
	}
	// TK-2: v opens the check
	h.key("v")
	if _, ok := h.top().(*checkScreen); !ok || h.depth() != 2 {
		t.Fatalf("v did not open the check: %T depth %d", h.top(), h.depth())
	}
}

func TestTK4OverlayShowsTheKey(t *testing.T) {
	h := newHarnessKeys(t, oneAccCfg(), "check: v\n")
	h.apply()
	h.selectKey("personal/tinhnd")
	h.key("?")
	if !h.m.showHelp {
		t.Fatal("overlay not open")
	}
	v := h.view()
	if !regexp.MustCompile(`\bv +` + regexp.QuoteMeta(tr("tui.key.check"))).MatchString(v) {
		t.Fatalf("overlay has no `v kiểm tra repo` row:\n%s", v)
	}
	if regexp.MustCompile(`\bk +` + regexp.QuoteMeta(tr("tui.key.check"))).MatchString(v) {
		t.Fatalf("overlay still shows k for check:\n%s", v)
	}
	h.mustContain(tr("tui.help.global"))
}

func TestTK5DetailHintNamesTheKey(t *testing.T) {
	h := newHarnessKeys(t, oneAccCfg(), "check: v\n")
	h.apply()
	h.resize(60, 24)
	// the company detail says how to verify
	openDetail(h, "personal")
	want := h.m.sh.km.T("tui.detail.how_applied")
	if !strings.Contains(want, "bấm v") {
		t.Fatalf("hint %q", want)
	}
	h.mustContainPlain(want)
	h.key("esc")
	// the account detail names the key in its next-step line
	openDetail(h, "personal/tinhnd")
	want = h.m.sh.km.T("tui.detail.acc_next_applied", "~/Code/personal/")
	if !strings.Contains(want, "hoặc v ") {
		t.Fatalf("hint %q", want)
	}
	h.mustContainPlain(want)
}

func TestTK6DoneScreenStepAndKey(t *testing.T) {
	h := newHarnessKeys(t, basicCfg(), "check: v\n")
	h.applyToDone()
	km := h.m.sh.km
	if !strings.Contains(km.T("tui.confirm.next_check", "x"), "bấm v để kiểm tra") {
		t.Fatalf("hint %q", km.T("tui.confirm.next_check", "x"))
	}
	h.mustContainPlain("bấm v để kiểm tra")
	// the step label is the key: "2  v  Kiểm tra: ..."
	if !regexp.MustCompile(`2\s+v\s+Kiểm tra`).MatchString(plain(h.view())) {
		t.Fatalf("the check step does not carry the label v:\n%s", h.view())
	}
	if !regexp.MustCompile(`1\s+c\s+`).MatchString(plain(h.view())) {
		t.Fatalf("the clone step does not carry the label c:\n%s", h.view())
	}
	noEffect(t, h, "k")
	h.key("v")
	if _, ok := h.top().(*checkScreen); !ok {
		t.Fatalf("v on the done screen: top is %T", h.top())
	}
}

func TestTK7PendingBadgeAndApplyKey(t *testing.T) {
	h := newHarnessKeys(t, basicCfg(), "apply: P\n")
	want := h.m.sh.km.T("tui.status.pending", h.m.sh.pending)
	if !strings.Contains(want, "bấm P") || !strings.Contains(h.badge(), want) {
		t.Fatalf("badge %q, want %q", h.badge(), want)
	}
	noEffect(t, h, "p")
	h.key("P")
	if _, ok := h.top().(*confirmScreen); !ok {
		t.Fatalf("P: top is %T", h.top())
	}
}

func TestTK8WelcomeUsesAddCompanyKey(t *testing.T) {
	h := newHarnessKeys(t, nil, "add_company: N\n")
	v := plain(h.view())
	if !regexp.MustCompile(`1\s+N\s+`).MatchString(v) {
		t.Fatalf("welcome step 1 has no label N:\n%s", h.view())
	}
	if want := h.m.sh.km.T("tui.status.no_company"); !strings.Contains(want, "bấm N") || !strings.Contains(h.badge(), want) {
		t.Fatalf("badge %q, want %q", h.badge(), want)
	}
	noEffect(t, h, "A")
	h.key("N")
	if _, ok := h.top().(*companyForm); !ok {
		t.Fatalf("N: top is %T", h.top())
	}
}

func TestTK9ConflictKeepsDefaultsAndSaysSo(t *testing.T) {
	h := newHarnessKeys(t, basicCfg(), "check: c\n")
	if !strings.Contains(h.status(), keysStatus(1)) {
		t.Fatalf("status %q", h.status())
	}
	h.key("c")
	if _, ok := h.top().(*cloneScreen); !ok {
		t.Fatalf("c: top is %T", h.top())
	}
	h.key("esc")
	h.key("k")
	if _, ok := h.top().(*checkScreen); !ok {
		t.Fatalf("k: top is %T", h.top())
	}
}

func TestTK10SavesDoNotRepeatTheWarning(t *testing.T) {
	h := newHarnessKeys(t, emptyCompanyCfg("personal"), "check: c\n")
	shown := h.m.keyShown
	if len(shown) != 1 {
		t.Fatalf("keyShown %v", shown)
	}
	h.send(statusExpireMsg{h.m.statSeq})
	h.key("A")
	h.typeText("x")
	h.key("enter")
	h.typeText("X Corp")
	h.key("enter")
	h.key("enter")
	h.waitFor("company created", func() bool { _, ok := h.top().(*listScreen); return ok && h.depth() == 1 })
	if strings.Contains(h.status(), keysStatus(1)) {
		t.Fatalf("the warning came back after a save: %q", h.status())
	}
	if len(h.m.keyShown) != 1 || len(h.m.sh.keyIssues) != 1 {
		t.Fatalf("a save reloaded the keymap: shown %v issues %v", h.m.keyShown, h.m.sh.keyIssues)
	}
}

func TestTK11BackKey(t *testing.T) {
	h := newHarnessKeys(t, basicCfg(), "back: x\n")
	h.resize(50, 24)
	openDetail(h, "acme/bob")
	noEffect(t, h, "q")
	if h.depth() != 2 {
		t.Fatalf("depth %d", h.depth())
	}
	h.key("x")
	if h.depth() != 1 {
		t.Fatalf("x did not go back, depth %d", h.depth())
	}
	openDetail(h, "acme/bob")
	h.key("esc")
	if h.depth() != 1 {
		t.Fatalf("esc did not go back, depth %d", h.depth())
	}
}

func TestTK12QuitKey(t *testing.T) {
	h := newHarnessKeys(t, basicCfg(), "quit: Q\n")
	h.key("q")
	if h.quit() {
		t.Fatal("q still quits")
	}
	h.key("Q")
	if !h.quit() {
		t.Fatal("Q did not quit")
	}
}

func TestTK13YesNoKeys(t *testing.T) {
	h := newHarnessKeys(t, basicCfg(), "yes: o\nno: m\n")
	h.selectKey("acme/bob")
	h.key("d")
	if _, ok := h.top().(*deleteScreen); !ok {
		t.Fatalf("top is %T", h.top())
	}
	bar := plain(h.helpLine())
	if !strings.Contains(bar, "o "+tr("tui.key.yes")) || !strings.Contains(bar, "m "+tr("tui.key.no")) {
		t.Fatalf("help bar %q", bar)
	}
	noEffect(t, h, "y")
	h.key("o")
	h.waitReady()
	h.mustContainPlain(h.m.sh.km.T("tui.confirm.question"))
	if q := h.m.sh.km.T("tui.confirm.question"); !strings.Contains(q, "o ") || !strings.Contains(q, "m ") {
		t.Fatalf("question %q", q)
	}
}

func TestTK14FilterKey(t *testing.T) {
	h := newHarnessKeys(t, basicCfg(), "filter: f\n")
	h.mustContainPlain("f " + tr("tui.list.filter_hint"))
	noEffect(t, h, "/")
	h.key("f")
	if h.listScreen().mode != filterTyping {
		t.Fatal("f did not start the filter")
	}
	h.mustContainPlain("f ")
}

func TestTK15ReloadAppliesNewKeys(t *testing.T) {
	h := newHarnessKeys(t, oneAccCfg(), "check: v\n")
	testutil.WriteFile(t, h.e.Paths().KeyBindingsFile, "check: w\n", 0o644)
	h.key("r")
	if h.status() != tr("tui.status.reloaded") {
		t.Fatalf("status %q", h.status())
	}
	noEffect(t, h, "v")
	h.key("w")
	if _, ok := h.top().(*checkScreen); !ok {
		t.Fatalf("w: top is %T", h.top())
	}
}

func TestTK15ReloadWithANewProblemWarns(t *testing.T) {
	h := newHarnessKeys(t, oneAccCfg(), "check: v\n")
	testutil.WriteFile(t, h.e.Paths().KeyBindingsFile, "check: c\n", 0o644)
	h.key("r")
	if !strings.Contains(h.status(), keysStatus(1)) {
		t.Fatalf("status %q", h.status())
	}
	// the same problem again is not announced again
	h.send(statusExpireMsg{h.m.statSeq})
	h.key("r")
	if h.status() != tr("tui.status.reloaded") {
		t.Fatalf("status %q", h.status())
	}
}

func TestTK16English(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang("vi")
	h := newHarnessKeys(t, oneAccCfg(), "check: v\napply: P\n")
	if !strings.Contains(h.badge(), "press P") {
		t.Fatalf("badge %q", h.badge())
	}
	h.apply()
	h.resize(60, 24)
	openDetail(h, "personal")
	h.mustContainPlain("press v")
}

func TestTK17OverlayFooterWarns(t *testing.T) {
	h := newHarnessKeys(t, basicCfg(), "check: c\n")
	h.key("?")
	h.mustContainPlain(tr("tui.help.keys_warn", 1))
	// the close hint names the effective keys
	h.mustContainPlain(h.m.sh.km.T("tui.help.close"))
	// small screens keep the footer and the global section
	for _, sz := range [][2]int{{80, 24}, {60, 20}, {40, 10}} {
		h.resize(sz[0], sz[1])
		if v := plain(h.view()); !strings.Contains(v, plain(tr("tui.help.keys_warn", 1))[:20]) {
			t.Errorf("%dx%d: no keys.yaml warning in the overlay:\n%s", sz[0], sz[1], h.view())
		}
	}
}

// ---- DIS-13: the discard question with custom yes/no ----

func TestDIS13DiscardQuestionUsesYesNoKeys(t *testing.T) {
	h := newHarnessKeys(t, basicCfg(), "yes: o\nno: m\n")
	h.key("A")
	h.typeText("acme")
	h.key("esc")
	q := h.m.sh.km.T("tui.form.discard_q")
	if !strings.HasSuffix(q, " o/m") {
		t.Fatalf("question %q", q)
	}
	h.mustContainPlain(q)
	bar := plain(h.helpLine())
	if !strings.Contains(bar, "o "+tr("tui.key.discard")) || !strings.Contains(bar, "m "+tr("tui.key.keep_editing")) {
		t.Fatalf("help bar %q", bar)
	}
	noEffect(t, h, "y") // the default key is ignored while the question is shown
	h.mustContainPlain(q)
	h.key("o")
	if h.depth() != 1 {
		t.Fatalf("o did not discard, depth %d", h.depth())
	}
}

// ---- STALE-2 / STALE-3 / STALE-4: the keymap and the stale-config guard ----

func addCompanyX(h *harness) {
	h.key("A")
	h.typeText("x")
	h.key("enter")
	h.typeText("X Corp")
	h.key("enter")
	h.key("enter")
	h.waitFor("company created", func() bool {
		_, ok := h.top().(*listScreen)
		return ok && h.depth() == 1 && h.listScreen().currentKey() == "x"
	})
}

func TestSTALE2And3KeysEditKeepsOldKeymapUntilReload(t *testing.T) {
	h := newHarnessKeys(t, emptyCompanyCfg("personal"), "check: v\n")
	p := h.e.Paths().KeyBindingsFile
	testutil.WriteFile(t, p, "check: w\n", 0o644)
	// STALE-2: the save succeeds (not stale) and the old keymap stays
	addCompanyX(h)
	if strings.Contains(plain(h.view()), plain(tr("tui.err.config_changed"))) {
		t.Fatalf("an edit of keys.yaml made the save stale:\n%s", h.view())
	}
	if got := h.m.sh.km.keys.Label(keymap.Check); got != "v" {
		t.Fatalf("the keymap changed without a reload: %q", got)
	}
	if _, changed, err := h.svc.ChangedSince(h.m.sh.cfg); err != nil || changed {
		t.Fatalf("config out of step: changed=%v err=%v", changed, err)
	}
	// STALE-3: r loads the new keys
	h.key("r")
	if h.status() != tr("tui.status.reloaded") {
		t.Fatalf("status %q", h.status())
	}
	if got := h.m.sh.km.keys.Label(keymap.Check); got != "w" {
		t.Fatalf("after r the check key is %q, want w", got)
	}
	h.selectKey("x")
	noEffect(t, h, "v")
	h.key("w")
	if _, ok := h.top().(*checkScreen); !ok {
		t.Fatalf("w: top is %T", h.top())
	}
}

func TestSTALE4StaleRefusalKeepsTheCustomKeymap(t *testing.T) {
	h := newHarnessKeys(t, oneAccCfg(), "check: v\n")
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
	h.waitFor("refused", func() bool { return strings.Contains(plain(h.view()), plain(tr("tui.err.config_changed"))) })
	if got := h.m.sh.km.keys.Label(keymap.Check); got != "v" {
		t.Fatalf("the reload after a refused save dropped the custom keymap: %q", got)
	}
	if b, _ := os.ReadFile(h.e.Paths().KeyBindingsFile); string(b) != "check: v\n" {
		t.Fatalf("keys.yaml changed: %q", b)
	}
}

// ---- I18N-2 .. I18N-5 ----

// hintArgs gives each message of hintKeys a dummy for every argument the call
// site passes after the key labels, of the kind the verb wants.
var hintArgs = map[string][]any{
	"tui.status.pending":             {3},
	"tui.status.pending_short":       {3},
	"tui.status.account_saved":       {"acme/alice"},
	"tui.status.empty_company":       {"acme"},
	"tui.status.company_created":     {"acme"},
	"tui.confirm.empty_no_accounts":  {"acme"},
	"tui.confirm.next_check":         {"~/Code/acme"},
	"tui.check.not_repo_next":        {"~/Code/acme/"},
	"tui.list.no_accounts_hint":      {"acme"},
	"tui.detail.acc_next_applied":    {"~/Code/acme/"},
	"tui.detail.dup":                 {"acme/alice"},
	"tui.form.err.dup_account":       {"alice", "github.com"},
	"tui.form.warn.username_changed": {"olduser"},
}

var unindexedVerb = regexp.MustCompile(`%[-+# 0-9.*]*[a-zA-Z]`)
var indexedVerb = regexp.MustCompile(`%\[(\d+)\]`)

func TestI18N2HintKeyVerbCounts(t *testing.T) {
	if len(hintKeys) == 0 {
		t.Fatal("no hint keys")
	}
	for key, acts := range hintKeys {
		want := len(acts) + len(hintArgs[key])
		for _, lang := range []string{"vi", "en"} {
			msg, ok := i18n.Messages(lang)[key]
			if !ok {
				t.Errorf("%s %s: missing", lang, key)
				continue
			}
			bare := strings.ReplaceAll(msg, "%%", "")
			if loc := unindexedVerb.FindString(bare); loc != "" {
				t.Errorf("%s %s: verb %q has no explicit index: %s", lang, key, loc, msg)
			}
			if got := i18n.Verbs(msg); got < want {
				t.Errorf("%s %s: %d verbs, want at least %d (%d keys + %d args)", lang, key, got, want, len(acts), len(hintArgs[key]))
			}
			used := map[int]bool{}
			for _, m := range indexedVerb.FindAllStringSubmatch(bare, -1) {
				var n int
				fmt.Sscanf(m[1], "%d", &n)
				used[n] = true
			}
			for n := 1; n <= want; n++ {
				if !used[n] {
					t.Errorf("%s %s: argument %%[%d] is never used: %s", lang, key, n, msg)
				}
			}
			for n := range used {
				if n > want {
					t.Errorf("%s %s: uses argument %d but only %d are passed: %s", lang, key, n, want, msg)
				}
			}
		}
	}
	// the arguments table names only messages of hintKeys
	for key := range hintArgs {
		if _, ok := hintKeys[key]; !ok {
			t.Errorf("hintArgs has %q, which is not a hint message", key)
		}
	}
}

func TestI18N3HintKeysRenderWithEveryKeymap(t *testing.T) {
	y, label := sentinelKeys()
	sent, issues := keymap.Parse([]byte(y))
	if len(issues) > 0 {
		t.Fatalf("sentinel issues: %v", issues)
	}
	maps := map[string]keymap.Map{"default": keymap.Defaults(), "sentinel": sent}
	defer i18n.SetLang("vi")
	for _, lang := range []string{"vi", "en"} {
		i18n.SetLang(lang)
		for name, m := range maps {
			km := NewKeyMap(m)
			for key, acts := range hintKeys {
				out := km.T(key, hintArgs[key]...)
				if strings.Contains(out, "%!") {
					t.Errorf("%s/%s %s: %q", lang, name, key, out)
				}
				for _, a := range acts {
					if !strings.Contains(out, m.Label(a)) {
						t.Errorf("%s/%s %s: %q lacks the label %q of %s", lang, name, key, out, m.Label(a), a)
					}
					if name == "sentinel" && !strings.Contains(out, label[a]) {
						t.Errorf("%s/sentinel %s: %q lacks %s", lang, key, out, label[a])
					}
				}
				// the original arguments are in the text too
				for _, a := range hintArgs[key] {
					if s, ok := a.(string); ok && !strings.Contains(out, s) {
						t.Errorf("%s/%s %s: %q lacks the argument %q", lang, name, key, out, s)
					}
				}
			}
		}
	}
}

// The _plain badge variants take the plain arguments only: a key label in
// front of them would print as %!(EXTRA ...).
func TestI18N3PlainBadgesTakeNoKeyArgs(t *testing.T) {
	km := NewKeyMap(keymap.Defaults())
	for _, c := range []struct {
		key  string
		args []any
	}{
		{"tui.status.pending_plain", []any{3}}, {"tui.status.pending_short_plain", []any{3}},
		{"tui.status.no_company_plain", nil}, {"tui.status.empty_company_plain", []any{"acme"}},
		{"tui.status.plan_failed_plain", nil},
	} {
		if _, ok := hintKeys[c.key]; ok {
			t.Errorf("%s must not be a hint message", c.key)
		}
		for _, lang := range []string{"vi", "en"} {
			if out := i18n.In(lang, c.key, c.args...); strings.Contains(out, "%!") || out == "" {
				t.Errorf("%s %s: %q", lang, c.key, out)
			}
		}
		if out := km.T(c.key, c.args...); strings.Contains(out, "%!") {
			t.Errorf("%s through km.T: %q", c.key, out)
		}
	}
}

// I18N-4: no tui message names a key with a fixed letter. Every key letter in
// prose is a verb now. Word boundaries are Unicode aware, so the Vietnamese
// "phím tắt" is not read as the key "t".
func TestI18N4NoFixedKeyLettersInMessages(t *testing.T) {
	named := regexp.MustCompile(`(?i)(bấm|nhấn|phím|press|key)\s+[A-Za-z?/]([^\p{L}\p{N}_]|$)`)
	// "(d)" after a space is a key; "change(s)" is a plural
	paren := regexp.MustCompile(`\s\([a-zA-Z]\)`)
	for _, lang := range []string{"vi", "en"} {
		for k, v := range i18n.Messages(lang) {
			if !strings.HasPrefix(k, "tui.") {
				continue
			}
			if m := named.FindString(v); m != "" {
				t.Errorf("%s %s names a fixed key (%q): %s", lang, k, m, v)
			}
			if m := paren.FindString(v); m != "" {
				t.Errorf("%s %s names a fixed key (%q): %s", lang, k, m, v)
			}
		}
	}
}

// I18N-5: no screen labels a rebindable binding with a fixed letter, and no
// hint message is formatted without the key map (tr instead of km.T).
func TestI18N5NoFixedLabelsInSource(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]bool{}
	for _, f := range actionField {
		fields[f] = true
	}
	withDescLit := regexp.MustCompile(`withDesc\(\s*[\w.]*\.(\w+)\s*,\s*"([^"])"`)
	trHint := regexp.MustCompile(`\btr\("(tui\.[a-z_.]+)"`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for n, line := range strings.Split(string(b), "\n") {
			if m := withDescLit.FindStringSubmatch(line); m != nil && fields[m[1]] {
				t.Errorf("%s:%d labels km.%s with the fixed letter %q; use relabel", f, n+1, m[1], m[2])
			}
			if m := trHint.FindStringSubmatch(line); m != nil {
				if _, hint := hintKeys[m[1]]; hint {
					t.Errorf("%s:%d formats %s with tr; it names keys, use km.T", f, n+1, m[1])
				}
			}
		}
	}
}

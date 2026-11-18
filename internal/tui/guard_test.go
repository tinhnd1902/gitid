package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/keymap"
	"github.com/tinhnd1902/gitid/internal/keys"
	"github.com/tinhnd1902/gitid/internal/testutil"
)

// The help-key-handled guard (GRD-1 .. GRD-5): every key a screen shows must
// work on that screen. It opens every screen state, takes each key of the
// help bar, of the "?" overlay and of the hints in the frame, presses it on a
// fresh copy of the state and fails when nothing happens.
//
// It runs twice: with the default keys (GRD-1) and with a sentinel keymap
// (GRD-2) in which every rebindable action sits on its own alt+<letter>
// (lower-case first, then upper-case). In
// the second run a hint that still names a hard-coded letter, or a key shown
// that the screen does not handle, shows up at once, because the label of
// every key is then a unique, recognisable token.

// sentinelKeys writes every rebindable action on its own alt+<letter> and
// returns the keys.yaml text and the label of each action.
func sentinelKeys() (yaml string, label map[keymap.Action]string) {
	label = map[keymap.Action]string{}
	var b strings.Builder
	for i, d := range keymap.Defs() {
		// 52 labels: alt+a..z, then alt+A..Z (the registry outgrew 26 actions
		// before; it holds 24 today)
		l := "alt+" + string(rune('a'+i))
		if i >= 26 {
			l = "alt+" + string(rune('A'+i-26))
		}
		label[d.Action] = l
		fmt.Fprintf(&b, "%s: %q\n", d.Action, l)
	}
	return b.String(), label
}

var sentinelLabelRe = regexp.MustCompile(`alt\+[a-zA-Z]`)

// grdState is one screen state of the guard. keysYAML is the keys.yaml of the
// run (empty: the default keys); setup builds a fresh harness in the state.
type grdState struct {
	name  string
	setup func(t *testing.T, keysYAML string) *harness
}

// importTwo is the importer with two proposals.
func importTwo(t *testing.T, keysYAML ...string) *harness {
	t.Helper()
	h := buildHarness(t, nil, func(e env.Env) {
		testutil.WriteFile(t, e.Paths().GitConfig, "[user]\n\tname = x\n"+
			"[includeIf \"gitdir:~/Code/work/initech-solutions/\"]\n\tpath = ~/.gitconfig-initech\n"+
			"[includeIf \"gitdir:~/Code/work/hooli/\"]\n\tpath = ~/.gitconfig-hooli\n", 0o644)
		testutil.WriteFile(t, filepath.Join(e.Home, ".gitconfig-initech"), "[user]\n\tname = Dana Initech\n\temail = dana@initech.example\n", 0o644)
		testutil.WriteFile(t, filepath.Join(e.Home, ".gitconfig-hooli"), "[user]\n\tname = Gavin\n\temail = gavin@hooli.example\n", 0o644)
		if len(keysYAML) > 0 && keysYAML[0] != "" {
			testutil.WriteFile(t, e.Paths().KeyBindingsFile, keysYAML[0], 0o644)
		}
	})
	h.act(keymap.Import)
	h.waitFor("proposals", func() bool { return h.top().(*importScreen).phase == imList })
	return h
}

// grdOverlay are the states whose "?" overlay list is checked too; one state
// per screen is enough, the help bar of every state is always checked.
var grdOverlay = map[string]bool{
	"list welcome": true, "list account applied": true, "list pending": true, "detail ssh account": true,
	"confirm ready": true, "confirm done": true, "check result": true, "clone pick": true,
	"key result": true, "doctor": true, "import proposals": true, "delete step 0": true, "keygen mode": true,
	"repos local list": true,
}

func grdStates() []grdState {
	hk := harnessFor
	wide := func(h *harness) *harness { h.resize(100, 30); return h }
	return []grdState{
		{"list welcome", func(t *testing.T, y string) *harness { return hk(t, nil, []string{y}) }},
		{"list company without accounts", func(t *testing.T, y string) *harness { return hk(t, emptyCompanyCfg("personal"), []string{y}) }},
		{"list account applied", func(t *testing.T, y string) *harness {
			h := hk(t, oneAccCfg(), []string{y})
			h.apply()
			h.selectKey("personal/tinhnd")
			return h
		}},
		{"list pending", func(t *testing.T, y string) *harness {
			h := hk(t, basicCfg(), []string{y})
			h.selectKey("acme/alice")
			return h
		}},
		{"list ssh account without key", func(t *testing.T, y string) *harness {
			h := hk(t, sshCfg(), []string{y})
			h.apply()
			h.selectKey("personal/alice")
			return h
		}},
		{"list narrow", func(t *testing.T, y string) *harness {
			h := hk(t, basicCfg(), []string{y})
			h.resize(50, 24)
			h.selectKey("acme/bob")
			return h
		}},
		{"list filter typing", func(t *testing.T, y string) *harness {
			h := hk(t, basicCfg(), []string{y})
			h.act(keymap.Filter)
			return h
		}},
		{"list filter applied", func(t *testing.T, y string) *harness {
			h := hk(t, basicCfg(), []string{y})
			h.act(keymap.Filter)
			h.typeText("a")
			h.key("enter")
			return h
		}},
		{"detail company", func(t *testing.T, y string) *harness {
			h := hk(t, emptyCompanyCfg("personal"), []string{y})
			h.resize(50, 24)
			h.key("enter")
			return h
		}},
		{"detail ssh account", func(t *testing.T, y string) *harness {
			h := hk(t, basicCfg(), []string{y})
			h.resize(50, 24)
			openDetail(h, "acme/carol")
			return h
		}},
		{"confirm ready", func(t *testing.T, y string) *harness { return readyHarness(t, y) }},
		{"confirm done", func(t *testing.T, y string) *harness {
			h := hk(t, basicCfg(), []string{y})
			h.applyToDone()
			return h
		}},
		{"confirm empty", func(t *testing.T, y string) *harness {
			h := hk(t, oneAccCfg(), []string{y})
			h.apply()
			h.act(keymap.Apply)
			h.waitFor("empty", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confEmpty })
			// the "applied, verify with <check key>" message of the apply that
			// made this state outlives the screen it was meant for (10 s); wait
			// it out so the frame shows only this screen's own text
			h.send(statusExpireMsg{h.m.statSeq})
			return h
		}},
		{"confirm failed", func(t *testing.T, y string) *harness {
			h := hk(t, basicCfg(), []string{y})
			c := newConfirmScreen(h.m.sh, "x")
			h.send(pushMsg{c})
			h.waitFor("loaded", func() bool { return c.state != confLoading })
			c.state, c.err = confFailed, errors.New("boom")
			return h
		}},
		{"check result", func(t *testing.T, y string) *harness {
			h, _ := repoHarness(t, "bob", y)
			h.act(keymap.Check)
			h.waitFor("result", func() bool { c, ok := h.top().(*checkScreen); return ok && c.phase == chResult })
			return h
		}},
		{"check not a repo", func(t *testing.T, y string) *harness {
			h := hk(t, basicCfg(), []string{y})
			h.fake.On("git -C "+h.e.Home+" rev-parse --show-toplevel", execx.Response{ExitCode: 128})
			h.act(keymap.Check)
			h.waitFor("error", func() bool { c, ok := h.top().(*checkScreen); return ok && c.phase == chError })
			return h
		}},
		{"check fix confirm", func(t *testing.T, y string) *harness { return fixConfirmHarness(t, y) }},
		{"clone url", func(t *testing.T, y string) *harness {
			h := hk(t, basicCfg(), []string{y})
			h.act(keymap.Clone)
			h.typeText("https://github.com/org/repo.git")
			return h
		}},
		{"clone pick", func(t *testing.T, y string) *harness {
			h := hk(t, basicCfg(), []string{y})
			h.act(keymap.Clone)
			h.typeText("https://github.com/org/repo.git")
			h.key("enter")
			return h
		}},
		{"clone failed", func(t *testing.T, y string) *harness {
			h := hk(t, basicCfg(), []string{y})
			h.act(keymap.Clone)
			h.typeText("https://github.com/org/repo.git")
			h.key("enter")
			cs := cloneOf(h)
			cs.phase, cs.err = clFailed, app.ErrAuth
			return h
		}},
		{"clone done", func(t *testing.T, y string) *harness {
			h := hk(t, basicCfg(), []string{y})
			h.act(keymap.Clone)
			h.typeText("https://github.com/org/repo.git")
			h.key("enter")
			cs := cloneOf(h)
			cs.phase = clDone
			cs.res = app.CloneResult{Plan: cs.preview, EmailOK: true}
			return h
		}},
		{"repos local list", func(t *testing.T, y string) *harness {
			h, p := localHarness(t, y)
			h.resize(100, 30)
			r := openLocal(h)
			h.walkTo(r, p["acme/org/x"]) // a repository with a fix, so every key is offered
			return h
		}},
		{"repos local problems", func(t *testing.T, y string) *harness {
			h, _ := localHarness(t, y)
			openLocal(h)
			h.act(keymap.Problems)
			return h
		}},
		{"repos local filter typing", func(t *testing.T, y string) *harness {
			h, _ := localHarness(t, y)
			openLocal(h)
			h.act(keymap.Filter)
			return h
		}},
		{"repos local filter applied", func(t *testing.T, y string) *harness {
			h, _ := localHarness(t, y)
			openLocal(h)
			h.act(keymap.Filter)
			h.typeText("a")
			h.key("enter")
			return h
		}},
		{"repos local empty", func(t *testing.T, y string) *harness {
			h := harnessFor(t, basicCfg(), []string{y})
			t.Cleanup(h.m.shutdown)
			for _, d := range []string{"acme", "globex", "beta"} {
				mustMkdir(t, filepath.Join(h.e.Home, "Code", d))
			}
			openLocal(h)
			return h
		}},
		{"repos local no companies", func(t *testing.T, y string) *harness {
			h := harnessFor(t, nil, []string{y})
			t.Cleanup(h.m.shutdown)
			openLocal(h)
			return h
		}},
		{"repos local scanning", func(t *testing.T, y string) *harness { return scanningHarness(t, y) }},
		{"delete step 0", func(t *testing.T, y string) *harness {
			h := hk(t, basicCfg(), []string{y})
			h.selectKey("acme/bob")
			h.act(keymap.Delete)
			return h
		}},
		{"delete step 1", func(t *testing.T, y string) *harness {
			h := hk(t, basicCfg(), []string{y})
			ref, _ := h.m.sh.cfg.ParseRef("acme/carol")
			ref.Account.SSHKey = ref.Company.DefaultKeyPath(ref.Account)
			d := newDeleteScreen(h.m.sh, ref.Company, &ref.Account)
			d.keys = []string{ref.Account.SSHKey}
			h.send(pushMsg{d})
			h.act(keymap.Yes)
			return h
		}},
		{"keygen mode", func(t *testing.T, y string) *harness { return keygenHarness(t, y) }},
		{"keygen no-pass confirm", func(t *testing.T, y string) *harness {
			h := keygenHarness(t, y)
			h.key("tab")
			h.key("enter")
			return h
		}},
		{"keygen error", func(t *testing.T, y string) *harness {
			h := keygenHarness(t, y)
			k := h.top().(*keygenScreen)
			k.phase, k.errMsg = kgError, "boom"
			return h
		}},
		{"key result", func(t *testing.T, y string) *harness {
			h := hk(t, basicCfg(), []string{y})
			ref, _ := h.m.sh.cfg.ParseRef("acme/carol")
			h.send(pushMsg{newKeyResultScreen(h.m.sh, ref, &keys.Pub{Content: "ssh-ed25519 AAAA carol"})})
			return h
		}},
		{"doctor", func(t *testing.T, y string) *harness {
			h := hk(t, basicCfg(), []string{y})
			h.resize(100, 12) // short, so that scrolling has an effect
			h.act(keymap.Doctor)
			h.waitFor("checks", func() bool { return !h.top().(*doctorScreen).pending() })
			return h
		}},
		{"import proposals", func(t *testing.T, y string) *harness { return importTwo(t, y) }},
		{"import none", func(t *testing.T, y string) *harness {
			h := hk(t, nil, []string{y})
			h.act(keymap.Import)
			h.waitFor("loaded", func() bool { return h.top().(*importScreen).phase == imList })
			return h
		}},
		{"import form", func(t *testing.T, y string) *harness {
			h := importTwo(t, y)
			h.key("enter")
			return h
		}},
		{"company form", func(t *testing.T, y string) *harness {
			h := wide(hk(t, basicCfg(), []string{y}))
			h.act(keymap.AddCompany)
			return h
		}},
		{"account form", func(t *testing.T, y string) *harness {
			h := wide(hk(t, basicCfg(), []string{y}))
			h.act(keymap.AddAccount)
			return h
		}},
		{"form discard prompt", func(t *testing.T, y string) *harness {
			h := hk(t, basicCfg(), []string{y})
			h.act(keymap.AddCompany)
			h.typeText("acme")
			h.key("esc")
			return h
		}},
	}
}

// grdAllowed are the (state, action) pairs whose key a hint names although it
// is not a single press on that screen: the text itself says that a step
// comes first. GRD-3 holds this list to the four cases below; every entry
// needs a reason.
var grdAllowed = map[string]string{
	"confirm empty/add_account": "tui.confirm.empty_no_accounts says: press esc, then the add-account key",
	"import none/add_company":   "tui.import.none_next says: press esc, then the add-company key",
	"confirm done/ssh_key":      "tui.confirm.next_ssh says: enter to the list, select the account, then the key",
	"account form/check":        "tui.form.warn.username_changed names the keys to press in the repo after saving",
	"account form/fix":          "tui.form.warn.username_changed names the keys to press in the repo after saving",
}

// grdAllowedMax is the most the allowlist may ever hold (GRD-3).
var grdAllowedMax = map[string]bool{
	"confirm empty/add_account": true, "import none/add_company": true, "confirm done/ssh_key": true,
	"account form/check": true, "account form/fix": true,
}

// allowedHint reports whether the hint key (a label found in the frame) is
// one of the allowlisted pairs of the state, judged by the labels of the
// keymap in effect.
func allowedHint(h *harness, state, label string) bool {
	for entry := range grdAllowed {
		parts := strings.SplitN(entry, "/", 2)
		if parts[0] == state && h.m.sh.km.keys.Label(keymap.Action(parts[1])) == label {
			return true
		}
	}
	return false
}

type grdProbe struct{ key, why string }

// guardState runs the guard on one state and returns what is wrong with it.
func guardState(t *testing.T, st grdState, keysYAML string) []string {
	t.Helper()
	var fails []string
	failf := func(format string, a ...any) { fails = append(fails, fmt.Sprintf(format, a...)) }

	probe := st.setup(t, keysYAML)
	top := probe.top()
	capturing := isCapturing(top)
	helpKey := probe.m.sh.km.keys.Label(keymap.Help)
	var todo []grdProbe
	seen := map[string]bool{}
	add := func(k, why string) {
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		todo = append(todo, grdProbe{k, why})
	}
	for _, it := range probe.m.helpItems() {
		add(it.b.Keys()[0], "help bar "+it.b.Help().Key)
	}
	if !capturing && grdOverlay[st.name] {
		for _, b := range top.Help() {
			if b.Enabled() {
				add(b.Keys()[0], "overlay "+b.Help().Key)
			}
		}
	}
	if !capturing {
		add(helpKey, "all keys")
	}
	frame := plain(probe.view())
	if ls, ok := top.(*listScreen); ok && ls.mode == filterTyping {
		// the filter key is the mark in front of the text being typed; it is
		// not an instruction, so it is not scanned as a hint
		frame = strings.Replace(frame, probe.m.sh.km.keys.Label(keymap.Filter), "", 1)
	}
	if rs, ok := top.(*reposScreen); ok && rs.tab == tabLocal && rs.fmode == filterTyping {
		frame = strings.Replace(frame, probe.m.sh.km.keys.Label(keymap.Filter), "", 1) // the same for the repo browser
	}
	if strings.Contains(frame, "%!") {
		failf("%s: the frame shows a format error:\n%s", st.name, probe.view())
	}
	// hints in the frame: a key named in prose ("press p"), and in the
	// sentinel run every alt+<letter> token
	hints := proseKeys(frame)
	if keysYAML != "" {
		for _, k := range hints {
			failf("%s: the frame names the hard-coded key %q in prose, but every key is an alt+letter here", st.name, k)
		}
		hints = sentinelLabelRe.FindAllString(frame, -1)
	}
	if keysYAML != "" {
		for _, f := range staleLabels(t, st, probe, keysYAML, rawFrame(probe.view()), capturing) {
			failf("%s: %s", st.name, f)
		}
	}
	for _, k := range hints {
		if allowedHint(probe, st.name, k) {
			continue
		}
		add(k, "hint in the frame")
	}

	for _, pk := range todo {
		h := st.setup(t, keysYAML)
		switch pk.key {
		case "up":
			h.key("down")
			before := h.snapshot()
			h.key("up")
			if h.snapshot() == before {
				failf("%s (%s): up has no effect", st.name, pk.why)
			}
			continue
		case "down", "ctrl+n":
			before := h.snapshot()
			h.send(keyMsg(pk.key))
			if h.snapshot() == before {
				failf("%s (%s): %s has no effect", st.name, pk.why, pk.key)
			}
			continue
		}
		before := h.snapshot()
		h.send(keyMsg(pk.key))
		if v := h.view(); strings.Contains(v, "%!") {
			failf("%s: after %q the frame shows a format error:\n%s", st.name, pk.key, v)
		}
		if pk.key == helpKey {
			if !h.m.showHelp {
				failf("%s (%s): %s did not open the overlay", st.name, pk.why, pk.key)
			}
			continue
		}
		if h.snapshot() == before {
			failf("%s (%s): key %q has no effect:\n%s", st.name, pk.why, pk.key, h.view())
		}
	}
	return fails
}

func runGuard(t *testing.T, keysYAML string) {
	defaultIdle = 15 * time.Millisecond // many harnesses: commands are tracked, so a short idle wait is safe
	defer func() { defaultIdle = 0 }()
	for _, st := range grdStates() {
		t.Run(st.name, func(t *testing.T) {
			for _, f := range guardState(t, st, keysYAML) {
				t.Error(f)
			}
		})
	}
}

// GRD-1: every advertised key of every state is handled, default keys.
func TestGRD1EveryAdvertisedKeyIsHandled(t *testing.T) { runGuard(t, "") }

// GRD-2: the same with every rebindable action on its own alt+<letter>. Any
// hint that still names a fixed letter, and any key shown but not handled,
// fails.
func TestGRD2SentinelKeymapEveryShownKeyIsHandled(t *testing.T) {
	y, label := sentinelKeys()
	// the sentinel keymap itself must be valid and complete
	m, issues := keymap.Parse([]byte(y))
	if len(issues) > 0 {
		t.Fatalf("the sentinel keys.yaml has issues: %+v", issues)
	}
	seen := map[string]keymap.Action{}
	for _, d := range keymap.Defs() {
		if m.Label(d.Action) != label[d.Action] || !strings.HasPrefix(label[d.Action], "alt+") {
			t.Fatalf("%s: label %q", d.Action, m.Label(d.Action))
		}
		if o, dup := seen[label[d.Action]]; dup {
			t.Fatalf("%s and %s share %s", d.Action, o, label[d.Action])
		}
		seen[label[d.Action]] = d.Action
	}
	runGuard(t, y)
}

// GRD-3: the allowlist holds at most the four documented cases (five pairs:
// the account form names two keys) and every entry has a reason.
func TestGRD3AllowlistIsShortAndJustified(t *testing.T) {
	for entry, why := range grdAllowed {
		if !grdAllowedMax[entry] {
			t.Errorf("%q is not one of the documented allowlist entries", entry)
		}
		if strings.TrimSpace(why) == "" {
			t.Errorf("%q has no reason", entry)
		}
		parts := strings.SplitN(entry, "/", 2)
		if !keymap.Known(parts[1]) {
			t.Errorf("%q: %q is not an action", entry, parts[1])
		}
	}
	if len(grdAllowed) > 5 {
		t.Errorf("allowlist has %d entries", len(grdAllowed))
	}
}

// blindDetail is the detail screen with the add-account key made to do
// nothing: the mutation GRD-4 uses to prove the guard can fail.
type blindDetail struct{ *detailScreen }

func (b *blindDetail) Update(msg tea.Msg) (Screen, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && keyMatchesAction(b.sh.km, k, keymap.AddAccount) {
		return b, nil
	}
	ns, cmd := b.detailScreen.Update(msg)
	if d, ok := ns.(*detailScreen); ok {
		b.detailScreen = d
		return b, cmd
	}
	return ns, cmd
}

func keyMatchesAction(km *KeyMap, k tea.KeyMsg, a keymap.Action) bool {
	for _, key := range km.keys.Keys(a) {
		if k.String() == key {
			return true
		}
	}
	return false
}

// GRD-4: remove the AddAccount case from the detail screen (a wrapper that
// swallows the key) and the guard reports it, with the default keys and with
// the sentinel keymap. Nothing in the repository is mutated.
func TestGRD4TheGuardCanFail(t *testing.T) {
	y, _ := sentinelKeys()
	for _, keysYAML := range []string{"", y} {
		blind := grdState{"detail ssh account (blind)", func(t *testing.T, y string) *harness {
			h := harnessFor(t, basicCfg(), []string{y})
			h.resize(50, 24)
			openDetail(h, "acme/carol")
			h.m.stack[len(h.m.stack)-1] = &blindDetail{h.top().(*detailScreen)}
			return h
		}}
		fails := guardState(t, blind, keysYAML)
		found := false
		for _, f := range fails {
			if strings.Contains(f, "has no effect") && strings.Contains(f, keymapLabel(keysYAML, keymap.AddAccount)) {
				found = true
			}
		}
		if !found {
			t.Errorf("sentinel=%v: the guard did not report the swallowed add-account key; got %v", keysYAML != "", fails)
		}
	}
}

func keymapLabel(keysYAML string, a keymap.Action) string {
	if keysYAML == "" {
		return keymap.DefaultKeys(a)[0]
	}
	m, _ := keymap.Parse([]byte(keysYAML))
	return m.Label(a)
}

// proseKeys finds the single-character keys the frame names in prose: "press
// p", "bấm k", "phím A".
func proseKeys(frame string) []string {
	words := strings.Fields(frame)
	var out []string
	for i := 0; i+1 < len(words); i++ {
		switch strings.ToLower(words[i]) {
		case "press", "bấm", "nhấn", "phím", "key":
		default:
			continue
		}
		w := strings.TrimRight(words[i+1], ".,;:)")
		if len([]rune(w)) == 1 && strings.ContainsAny(w, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ?") {
			out = append(out, w)
		}
	}
	return out
}

// GRD-5: no frame of any state, with the default keys or the sentinel keys,
// shows a broken format verb.
func TestGRD5NoBrokenFormatVerbsInFrames(t *testing.T) {
	defaultIdle = 15 * time.Millisecond
	defer func() { defaultIdle = 0 }()
	y, _ := sentinelKeys()
	for _, keysYAML := range []string{"", y} {
		for _, st := range grdStates() {
			name := st.name
			if keysYAML != "" {
				name += " (sentinel)"
			}
			t.Run(name, func(t *testing.T) {
				h := st.setup(t, keysYAML)
				if strings.Contains(h.view(), "%!") {
					t.Fatalf("a frame shows a format error:\n%s", h.view())
				}
				h.act(keymap.Help)
				if strings.Contains(h.view(), "%!") {
					t.Fatalf("the overlay shows a format error:\n%s", h.view())
				}
			})
		}
	}
}

// defaultLetters maps every single-character default key to its action.
func defaultLetters() map[string]keymap.Action {
	out := map[string]keymap.Action{}
	for _, d := range keymap.Defs() {
		for _, k := range d.Default {
			if len([]rune(k)) == 1 {
				out[k] = d.Action
			}
		}
	}
	return out
}

// rawFrame is a frame with the border characters blanked but the spacing
// kept (plain collapses runs of spaces, which hides "k  text").
func rawFrame(s string) string {
	for _, c := range []string{"│", "┃", "╭", "╮", "╰", "╯", "─"} {
		s = strings.ReplaceAll(s, c, " ")
	}
	return s
}

// buttonLabelRe finds a key label standing in front of its text, as in a
// button or a step line: "t  Run again", "| c  Clone".
var buttonLabelRe = regexp.MustCompile(`(?:^|\s\s|▸ )([A-Za-z?+#])  +\S`)

// staleLabels is the GRD-6 check, for the sentinel run: no help-bar item,
// button or overlay row may show the default single-letter key of an action
// whose sentinel binding is another key. A label hard-coded in the source
// (a button "t  ...", a help item "k") shows up here, prose or not.
func staleLabels(t *testing.T, st grdState, probe *harness, keysYAML, frame string, capturing bool) []string {
	t.Helper()
	letters := defaultLetters()
	var out []string
	check := func(where, text string) {
		for _, part := range strings.Split(text, "/") {
			if a, ok := letters[part]; ok {
				out = append(out, fmt.Sprintf("%s shows the default key %q of %s, but its binding is %s", where, part, a, keymapLabel(keysYAML, a)))
			}
		}
	}
	for _, it := range probe.m.helpItems() {
		check("help bar item "+it.b.Help().Desc, it.b.Help().Key)
	}
	scan := func(where, text string) {
		for _, line := range strings.Split(text, "\n") {
			for _, m := range buttonLabelRe.FindAllStringSubmatch(line, -1) {
				check(where+" label in "+strings.TrimSpace(line), m[1])
			}
		}
	}
	scan("frame", frame)
	if !capturing {
		h := st.setup(t, keysYAML)
		h.act(keymap.Help)
		if h.m.showHelp {
			ov := rawFrame(h.view())
			scan("overlay", ov)
			for _, b := range append(h.top().Help(), h.m.globalHelp()...) {
				if b.Enabled() {
					check("overlay row "+b.Help().Desc, h.m.overlayKey(b))
				}
			}
		}
	}
	return out
}

// GRD-7: the "?" overlay shows every key of an action, and its key column
// never touches the description.
func TestGRD7OverlayAllKeysAndColumnGap(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {80, 24}, {100, 30}} {
		for _, lang := range []string{"en", "vi"} {
			i18n.SetLang(lang)
			h := harnessFor(t, basicCfg(), []string{"clone: [c, n]\n"})
			h.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			h.act(keymap.Help)
			v := plain(h.view())
			if !h.m.showHelp {
				t.Fatalf("%dx%d: overlay not open", size[0], size[1])
			}
			if !regexp.MustCompile(`\bc/n +\S`).MatchString(v) {
				t.Errorf("%s %dx%d: the overlay does not show c/n:\n%s", lang, size[0], size[1], v)
			}
			if regexp.MustCompile(`pgup/pgdn\S`).MatchString(v) {
				t.Errorf("%s %dx%d: pgup/pgdn touches its description:\n%s", lang, size[0], size[1], v)
			}
			if !regexp.MustCompile(`pgup/pgdn +\S`).MatchString(v) {
				t.Errorf("%s %dx%d: no pgup/pgdn row:\n%s", lang, size[0], size[1], v)
			}
			assertFits(t, h.view(), size[0], size[1], lang+" overlay")
		}
	}
	i18n.SetLang("vi")
}

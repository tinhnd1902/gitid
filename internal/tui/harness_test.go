package tui

import (
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/keymap"
	"github.com/tinhnd1902/gitid/internal/testutil"
	"github.com/tinhnd1902/gitid/internal/testutil/fixtures"
)

func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii)
	i18n.SetLang("vi")
	os.Exit(m.Run())
}

// defaultIdle is the idle wait of new harnesses (0: 60ms). Tests run one at a
// time, so a test that builds many harnesses may shorten it while it runs.
var defaultIdle time.Duration

// harness drives the root model with messages, the way Bubble Tea does:
// commands run in goroutines and their results are fed back to Update.
type harness struct {
	t    *testing.T
	m    *Model
	e    env.Env
	fake *execx.Fake
	svc  *app.Service
	msgs chan tea.Msg

	mu       sync.Mutex
	inflight map[int]time.Time // commands that have started and not returned yet
	nextCmd  int
	quitSeen bool // a tea.QuitMsg was produced
	// idle is how long drain waits for a late message once nothing is in
	// flight (0: the default).
	idle time.Duration
}

// newHarness builds a model over a temp HOME. cfg (optional) is saved first.
func newHarness(t *testing.T, cfg *config.Config) *harness {
	t.Helper()
	return buildHarness(t, cfg, nil)
}

// newHarnessKeys is newHarness with a keys.yaml (the content is written
// verbatim) in the temp config dir before the model starts.
func newHarnessKeys(t *testing.T, cfg *config.Config, keysYAML string) *harness {
	t.Helper()
	return buildHarness(t, cfg, func(e env.Env) {
		testutil.WriteFile(t, e.Paths().KeyBindingsFile, keysYAML, 0o644)
	})
}

// buildHarness is the common builder; before (optional) runs after cfg was
// saved and before the model starts.
func buildHarness(t *testing.T, cfg *config.Config, before func(e env.Env)) *harness {
	t.Helper()
	e := testutil.TempEnv(t)
	fake := execx.NewFake()
	svc := app.New(e, fake)
	if cfg != nil {
		if _, _, err := svc.Commit(*cfg); err != nil {
			t.Fatalf("seed config: %v", err)
		}
	}
	if before != nil {
		before(e)
	}
	h := &harness{t: t, e: e, fake: fake, svc: svc, msgs: make(chan tea.Msg, 256), inflight: map[int]time.Time{}, idle: defaultIdle}
	h.m = New(svc, Options{Cwd: e.Home, Version: "test"})
	h.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	h.run(h.m.Init())
	h.drain()
	return h
}

func basicCfg() *config.Config {
	c := fixtures.Basic()
	return &c
}

// run executes cmd (and any batch it expands to) in the background.
func (h *harness) run(cmd tea.Cmd) {
	if cmd == nil || animation(cmd) {
		return
	}
	h.mu.Lock()
	h.nextCmd++
	id := h.nextCmd
	h.inflight[id] = time.Now()
	h.mu.Unlock()
	go func() {
		msg := cmd()
		// deliver before the command stops counting as in flight, so drain
		// never sees "nothing running, nothing queued" in between
		h.deliver(msg)
		h.mu.Lock()
		delete(h.inflight, id)
		h.mu.Unlock()
	}()
}

// animation reports whether cmd only drives cursor blinking, a spinner or a
// timer. Such commands are never run in tests: they sleep, and the bubbles
// implementations read their model from the command's goroutine while Update
// writes it (harmless in a program, but the race detector rightly flags it).
// Tests that need a tick send the tick message themselves.
func animation(cmd tea.Cmd) bool {
	f := runtime.FuncForPC(reflect.ValueOf(cmd).Pointer())
	if f == nil {
		return false
	}
	n := f.Name()
	for _, part := range []string{"bubbles/cursor.", "bubbles/spinner.", "bubbles/textinput.Blink", "bubbletea.Tick", "bubbletea.Every"} {
		if strings.Contains(n, part) {
			return true
		}
	}
	return false
}

// busy reports whether a command that started recently is still running.
// Commands older than sleeperAge are timers (cursor blink, spinner ticks) and
// are not waited for.
func (h *harness) busy() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, at := range h.inflight {
		if time.Since(at) < sleeperAge {
			return true
		}
	}
	return false
}

const sleeperAge = 100 * time.Millisecond

func (h *harness) deliver(msg tea.Msg) {
	if msg == nil {
		return
	}
	if _, ok := msg.(tea.QuitMsg); ok {
		h.mu.Lock()
		h.quitSeen = true
		h.mu.Unlock()
	}
	rv := reflect.ValueOf(msg)
	if rv.Kind() == reflect.Slice && rv.Type().Elem() == reflect.TypeOf((tea.Cmd)(nil)) {
		name := rv.Type().Name()
		for i := 0; i < rv.Len(); i++ {
			c, _ := rv.Index(i).Interface().(tea.Cmd)
			if c == nil || animation(c) {
				continue
			}
			if name == "sequenceMsg" {
				h.deliver(c()) // in order
			} else {
				h.run(c)
			}
		}
		return
	}
	h.msgs <- msg
}

// noise are messages that only drive animations or terminal queries.
func noise(msg tea.Msg) bool {
	s := fmt.Sprintf("%T", msg)
	return strings.Contains(s, "TickMsg") || strings.Contains(s, "link") || strings.Contains(s, "windowSize")
}

// drain feeds queued results to the model until nothing arrives for a while
// and no recently started command is still running.
func (h *harness) drain() {
	h.t.Helper()
	for {
		select {
		case msg := <-h.msgs:
			if noise(msg) {
				continue
			}
			h.update(msg)
		case <-time.After(h.idleWait()):
			if !h.busy() {
				return
			}
		}
	}
}

func (h *harness) idleWait() time.Duration {
	if h.idle > 0 {
		return h.idle
	}
	return 60 * time.Millisecond
}

func (h *harness) update(msg tea.Msg) {
	_, cmd := h.m.Update(msg)
	h.run(cmd)
}

// send delivers a message and settles the resulting commands.
func (h *harness) send(msg tea.Msg) {
	h.t.Helper()
	h.update(msg)
	h.drain()
}

func (h *harness) key(s string) {
	h.t.Helper()
	h.send(keyMsg(s))
}

// keys types plain text without settling between runes.
func (h *harness) typeText(s string) {
	h.t.Helper()
	for _, r := range s {
		h.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	h.drain()
}

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	}
	if r := []rune(s); len(r) == 6 && strings.HasPrefix(s, "ctrl+") && r[5] >= 'a' && r[5] <= 'z' {
		return tea.KeyMsg{Type: tea.KeyType(r[5]-'a') + tea.KeyCtrlA}
	}
	if r := []rune(s); len(r) == 5 && strings.HasPrefix(s, "alt+") {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: r[4:], Alt: true}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func (h *harness) view() string { return h.m.View() }

// quit reports whether the program was asked to quit.
func (h *harness) quit() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.quitSeen
}

func (h *harness) mustContain(parts ...string) {
	h.t.Helper()
	v := h.view()
	for _, p := range parts {
		if !strings.Contains(v, p) {
			h.t.Errorf("view does not contain %q:\n%s", p, v)
		}
	}
}

func (h *harness) mustNotContain(parts ...string) {
	h.t.Helper()
	v := h.view()
	for _, p := range parts {
		if strings.Contains(v, p) {
			h.t.Errorf("view unexpectedly contains %q:\n%s", p, v)
		}
	}
}

// waitFor polls until pred is true (feeding late results) or fails.
func (h *harness) waitFor(what string, pred func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		h.drain()
		if pred() {
			return
		}
	}
	h.t.Fatalf("timeout waiting for %s; view:\n%s", what, h.view())
}

func (h *harness) top() Screen { return h.m.top() }

func (h *harness) depth() int { return len(h.m.stack) }

// defKM is the key map with the default keys. Messages that name keys take
// them as arguments, so a test that asserts their text builds it with
// defKM.T instead of tr.
var defKM = NewKeyMap(keymap.Defaults())

// harnessFor is newHarness, or newHarnessKeys when a keys.yaml is given.
func harnessFor(t *testing.T, cfg *config.Config, keysYAML []string) *harness {
	t.Helper()
	if len(keysYAML) > 0 && keysYAML[0] != "" {
		return newHarnessKeys(t, cfg, keysYAML[0])
	}
	return newHarness(t, cfg)
}

// act presses the key bound to an action in the keymap in effect, so a
// helper works with the default keys and with a keys.yaml alike.
func (h *harness) act(a keymap.Action) {
	h.t.Helper()
	h.key(h.m.sh.km.keys.Label(a))
}

// cloneOf is the clone flow of the repo browser when the browser is on top
// and shows its URL tab, nil otherwise. The URL flow lives inside the browser
// (the screen the clone key opens), so the tests reach it through here.
func cloneOf(h *harness) *cloneScreen {
	if r, ok := h.top().(*reposScreen); ok && r.tab == tabURL {
		return r.url
	}
	return nil
}

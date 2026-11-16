package tui

import (
	"fmt"
	"os"
	"reflect"
	"strings"
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
	"github.com/tinhnd1902/gitid/internal/testutil"
	"github.com/tinhnd1902/gitid/internal/testutil/fixtures"
)

func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii)
	i18n.SetLang("vi")
	os.Exit(m.Run())
}

// harness drives the root model with messages, the way Bubble Tea does:
// commands run in goroutines and their results are fed back to Update.
type harness struct {
	t    *testing.T
	m    *Model
	e    env.Env
	fake *execx.Fake
	svc  *app.Service
	msgs chan tea.Msg
}

// newHarness builds a model over a temp HOME. cfg (optional) is saved first.
func newHarness(t *testing.T, cfg *config.Config) *harness {
	t.Helper()
	e := testutil.TempEnv(t)
	fake := execx.NewFake()
	svc := app.New(e, fake)
	if cfg != nil {
		if _, _, err := svc.Commit(*cfg); err != nil {
			t.Fatalf("seed config: %v", err)
		}
	}
	h := &harness{t: t, e: e, fake: fake, svc: svc, msgs: make(chan tea.Msg, 256)}
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
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		h.deliver(msg)
	}()
}

func (h *harness) deliver(msg tea.Msg) {
	if msg == nil {
		return
	}
	rv := reflect.ValueOf(msg)
	if rv.Kind() == reflect.Slice && rv.Type().Elem() == reflect.TypeOf((tea.Cmd)(nil)) {
		name := rv.Type().Name()
		for i := 0; i < rv.Len(); i++ {
			c, _ := rv.Index(i).Interface().(tea.Cmd)
			if c == nil {
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

// drain feeds queued results to the model until nothing arrives for a while.
func (h *harness) drain() {
	h.t.Helper()
	for {
		select {
		case msg := <-h.msgs:
			if noise(msg) {
				continue
			}
			h.update(msg)
		case <-time.After(60 * time.Millisecond):
			return
		}
	}
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
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func (h *harness) view() string { return h.m.View() }

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
	deadline := time.Now().Add(5 * time.Second)
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

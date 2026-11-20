package tui

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/testutil"
)

// T13: run the real program under teatest at a fixed size and compare the
// final list screen with a golden file (run with -update to refresh it).
func TestT13GoldenListScreen(t *testing.T) {
	e := testutil.TempEnv(t)
	svc := app.New(e, execx.NewFake())
	if _, _, err := svc.Commit(*basicCfg()); err != nil {
		t.Fatal(err)
	}
	tm := teatest.NewTestModel(t, New(svc, Options{Cwd: e.Home, Version: "v0.0.0-test"}), teatest.WithInitialTermSize(100, 30))
	// the program opens on the start menu: 1 opens the Accounts screen, and
	// ctrl+c (q goes back to the menu there) ends the run on that screen
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Duyệt repo")) && bytes.Contains(b, []byte("thay đổi chưa áp dụng"))
	}, teatest.WithDuration(5*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("alice@beta.io"))
	}, teatest.WithDuration(5*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	fm := tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second))
	golden.RequireEqual(t, []byte(fm.View()))
}

func TestListScrollsToTheLastRow(t *testing.T) {
	var accs []config.Account
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("user%02d", i)
		accs = append(accs, config.Account{ID: id, Platform: "github", Username: id, Name: "U " + id, Email: id + "@example.com", Auth: config.AuthHTTPS})
	}
	cfg := &config.Config{Version: 1, Companies: []config.Company{{Slug: "big", Name: "Big", BaseDir: "~/Code/big/", DefaultAccount: "user00", Accounts: accs}}}
	h := newHarness(t, cfg)
	h.update(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.key("end")
	h.mustContain("user39", "▌")
	h.key("home")
	h.mustContain("big", "user00")
	h.mustNotContain("user39")
}

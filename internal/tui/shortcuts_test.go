package tui

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/keys"
	"github.com/tinhnd1902/gitid/internal/testutil"
)

// Tests of the shortcut fixes: q goes back, esc goes one step, the discard
// question of forms, the input-method hint, and the guard that every key a
// screen advertises is handled by it.

// ---- helpers ----

func runesMsg(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func (h *harness) status() string { return h.m.status.text }

// sendRunes delivers composed (or burst) input the way an input method does.
func (h *harness) sendRunes(s string) { h.t.Helper(); h.send(runesMsg(s)) }

// snapshot is everything a key can change: the stack, the frame, the quit
// flag, the commands run and the ids handed out.
func (h *harness) snapshot() string {
	h.mu.Lock()
	quit := h.quitSeen
	h.mu.Unlock()
	return fmt.Sprintf("%d|%p|%v|%v|%d|%d|%s", h.depth(), h.top(), h.m.showHelp, quit, len(h.fake.Calls()), h.m.sh.nextID, h.view())
}

// repoHarness is a basic configuration with a git repository under acme whose
// remote user is wrong; the fake git answers for it.
func repoHarness(t *testing.T, remoteUser string) (*harness, string) {
	t.Helper()
	h := newHarness(t, basicCfg())
	top := h.e.Home + "/Code/acme/proj"
	h.m.sh.cwd = top
	h.fake.On("git -C "+top+" rev-parse --show-toplevel", execx.Response{Stdout: top + "\n"})
	h.fake.On("git -C "+top+" config --get-regexp", execx.Response{Stdout: "remote.origin.url https://" + remoteUser + "@github.com/org/repo.git\n"})
	h.fake.On("git -C "+top+" config --show-origin --get user.email", execx.Response{Stdout: "file:/home/x/.gitconfig\twrong@example.com\n"})
	h.fake.On("git -C "+top+" remote set-url", execx.Response{})
	return h, top
}

// fixConfirmHarness leaves the check screen in its fix confirmation.
func fixConfirmHarness(t *testing.T) *harness {
	t.Helper()
	h, _ := repoHarness(t, "olduser")
	h.key("k")
	h.waitFor("result", func() bool { c, ok := h.top().(*checkScreen); return ok && c.phase == chResult })
	h.top().(*checkScreen).res.Account = "acme/alice"
	h.key("f")
	h.waitFor("fix confirm", func() bool { return h.top().(*checkScreen).phase == chFixConfirm })
	return h
}

// keygenHarness opens the key generation of acme/carol (ssh, no key yet).
func keygenHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, basicCfg())
	h.selectKey("acme/carol")
	h.key("g")
	if _, ok := h.top().(*keygenScreen); !ok {
		t.Fatalf("top is %T", h.top())
	}
	return h
}

func readyHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, basicCfg())
	h.key("p")
	h.waitReady()
	return h
}

// ---- IME ----

const imeVi = "Bộ gõ tiếng Việt đang bật — chuyển sang EN để dùng phím tắt"

func TestIME1ComposedRuneOnTheList(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.sendRunes("â")
	if got := h.status(); got != imeVi {
		t.Fatalf("status %q", got)
	}
	if h.depth() != 1 || h.quit() {
		t.Fatal("the list must not react")
	}
	for _, r := range []string{"đ", "ơ"} {
		h.m.status = statusMsg{}
		h.sendRunes(r)
		if h.status() != imeVi {
			t.Fatalf("%q: status %q", r, h.status())
		}
	}
}

func TestIME3BurstOfRunesIsAHintNotShortcuts(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.selectKey("acme")
	h.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a', 'a'}})
	if h.status() != imeVi || h.depth() != 1 {
		t.Fatalf("status %q depth %d", h.status(), h.depth())
	}
}

func TestIME4PasteIsNotAHint(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("https://x"), Paste: true})
	if h.status() == imeVi || h.depth() != 1 {
		t.Fatalf("status %q depth %d", h.status(), h.depth())
	}
}

func TestIME5To7TextInputsTakeComposedText(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("/")
	h.sendRunes("đ")
	if h.status() == imeVi || h.listScreen().filter.Value() != "đ" {
		t.Fatalf("filter %q status %q", h.listScreen().filter.Value(), h.status())
	}
	h.key("esc")

	h.key("c")
	h.sendRunes("â")
	if h.status() == imeVi || h.top().(*cloneScreen).input.Value() != "â" {
		t.Fatalf("clone input %q", h.top().(*cloneScreen).input.Value())
	}
	h.key("esc")

	h.key("a")
	h.key("enter") // platform -> username
	h.sendRunes("ơ")
	f := h.top().(*accountForm)
	if h.status() == imeVi || f.username != "ơ" {
		t.Fatalf("username %q status %q", f.username, h.status())
	}
}

func TestIME8SelectFieldGetsTheHint(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("a")
	f := h.top().(*accountForm)
	plat := f.plat
	h.sendRunes("â")
	if h.status() != imeVi || f.plat != plat {
		t.Fatalf("status %q plat %q->%q", h.status(), plat, f.plat)
	}
	if h.depth() != 2 {
		t.Fatal("the form must stay open")
	}
}

func TestIME9To11ConfirmDeleteAndOverlay(t *testing.T) {
	h := readyHarness(t)
	h.sendRunes("ý")
	if h.status() != imeVi || h.top().(*confirmScreen).state != confReady {
		t.Fatalf("confirm: status %q", h.status())
	}

	h = newHarness(t, basicCfg())
	h.selectKey("acme/bob")
	h.key("d")
	h.sendRunes("ý")
	if h.status() != imeVi || h.depth() != 2 {
		t.Fatalf("delete: status %q depth %d", h.status(), h.depth())
	}
	if _, err := h.m.sh.cfg.ParseRef("acme/bob"); err != nil {
		t.Fatal("nothing may be deleted")
	}

	h = newHarness(t, basicCfg())
	h.key("?")
	h.sendRunes("â")
	if h.status() != imeVi || !h.m.showHelp {
		t.Fatalf("overlay: status %q open=%v", h.status(), h.m.showHelp)
	}
}

func TestIME12English(t *testing.T) {
	defer i18n.SetLang("vi")
	i18n.SetLang("en")
	h := newHarness(t, basicCfg())
	h.sendRunes("â")
	if h.status() != tr("tui.status.ime") || !strings.Contains(h.status(), "switch to EN") {
		t.Fatalf("status %q", h.status())
	}
}

func TestIME13DeleteKeyPromptTakesText(t *testing.T) {
	h := newHarness(t, basicCfg())
	ref, _ := h.m.sh.cfg.ParseRef("acme/carol")
	ref.Account.SSHKey = ref.Company.DefaultKeyPath(ref.Account)
	d := newDeleteScreen(h.m.sh, ref.Company, &ref.Account)
	d.keys = []string{ref.Account.SSHKey}
	h.send(pushMsg{d})
	h.key("y")
	if d.step != 1 {
		t.Fatalf("step %d", d.step)
	}
	h.sendRunes("đ")
	if h.status() == imeVi || d.input.Value() != "đ" {
		t.Fatalf("input %q status %q", d.input.Value(), h.status())
	}
}

// ---- q goes back ----

func TestQB1QQuitsOnTheList(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("q")
	if !h.quit() {
		t.Fatal("q must quit on the list")
	}
}

func TestQB2To11QGoesBackOnSubScreens(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T) *harness
	}{
		{"detail", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.resize(50, 24)
			openDetail(h, "acme/bob")
			return h
		}},
		{"check result", func(t *testing.T) *harness {
			h, _ := repoHarness(t, "bob")
			h.key("k")
			h.waitFor("result", func() bool { c, ok := h.top().(*checkScreen); return ok && c.phase == chResult })
			return h
		}},
		{"keygen mode", keygenHarness},
		{"confirm ready", readyHarness},
		{"confirm done", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.applyToDone()
			return h
		}},
		{"key result", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			ref, _ := h.m.sh.cfg.ParseRef("acme/carol")
			h.send(pushMsg{newKeyResultScreen(h.m.sh, ref, &keys.Pub{Content: "ssh-ed25519 AAAA carol"})})
			return h
		}},
		{"doctor", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.key("D")
			return h
		}},
		{"import list", func(t *testing.T) *harness {
			h := importerHarness(t)
			return h
		}},
		{"delete step 0", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.selectKey("acme/bob")
			h.key("d")
			return h
		}},
		{"clone failed", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.key("c")
			h.top().(*cloneScreen).phase = clFailed
			return h
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := c.setup(t)
			if h.depth() != 2 {
				t.Fatalf("depth %d before", h.depth())
			}
			h.key("q")
			if h.depth() != 1 || h.quit() {
				t.Fatalf("depth %d quit %v after q", h.depth(), h.quit())
			}
			if c.name == "confirm ready" && h.m.sh.pending == 0 {
				t.Fatal("nothing may be applied")
			}
		})
	}
}

func TestQB4And5QStepsBackInsideTheScreen(t *testing.T) {
	h := fixConfirmHarness(t)
	h.key("q")
	if c := h.top().(*checkScreen); c.phase != chResult || h.depth() != 2 {
		t.Fatalf("check: phase %v depth %d", c.phase, h.depth())
	}
	h = keygenHarness(t)
	h.key("tab")
	h.key("enter")
	k := h.top().(*keygenScreen)
	if k.phase != kgNoPassConfirm {
		t.Fatalf("phase %v", k.phase)
	}
	h.key("q")
	if k.phase != kgMode || h.depth() != 2 || h.top() != Screen(k) || k.mode.idx != 1 {
		t.Fatalf("keygen: phase %v depth %d mode %d", k.phase, h.depth(), k.mode.idx)
	}
}

func TestQB12To13CloneURLTypesQAndPickGoesBack(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("c")
	h.key("q")
	cs := h.top().(*cloneScreen)
	if cs.phase != clURL || cs.input.Value() != "q" || h.depth() != 2 {
		t.Fatalf("phase %v input %q depth %d", cs.phase, cs.input.Value(), h.depth())
	}
	cs.input.SetValue("https://github.com/org/repo.git")
	h.key("enter")
	if cs.phase != clPick {
		t.Fatalf("phase %v", cs.phase)
	}
	h.key("q")
	if cs.phase != clURL || h.depth() != 2 || !cs.input.Focused() {
		t.Fatalf("phase %v depth %d focused %v", cs.phase, h.depth(), cs.input.Focused())
	}
}

func TestQB15To17FormsTypeQButGoBackOnSelects(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("A")
	h.key("q") // slug input
	if f := h.top().(*companyForm); f.slug != "q" || h.depth() != 2 {
		t.Fatalf("slug %q depth %d", f.slug, h.depth())
	}

	h = newHarness(t, basicCfg())
	h.key("a") // platform select focused, unchanged
	h.key("q")
	if h.depth() != 1 {
		t.Fatalf("depth %d: q on an unchanged select must go back", h.depth())
	}

	h.key("a")
	h.key("enter")
	h.typeText("dora")
	h.shiftTab() // back on the platform select
	h.key("q")
	if h.depth() != 2 || !h.top().(*accountForm).guard.asking {
		t.Fatalf("depth %d: a changed form must ask first", h.depth())
	}
}

func TestQB18And19DeleteStepsAndTheOverlay(t *testing.T) {
	h := newHarness(t, basicCfg())
	ref, _ := h.m.sh.cfg.ParseRef("acme/carol")
	ref.Account.SSHKey = ref.Company.DefaultKeyPath(ref.Account)
	d := newDeleteScreen(h.m.sh, ref.Company, &ref.Account)
	d.keys = []string{ref.Account.SSHKey}
	h.send(pushMsg{d})
	h.key("y")
	h.key("q") // the key prompt is a text input
	if d.input.Value() != "q" || h.depth() != 2 {
		t.Fatalf("input %q depth %d", d.input.Value(), h.depth())
	}

	h = newHarness(t, basicCfg())
	h.key("?")
	h.key("q")
	if h.m.showHelp || h.quit() {
		t.Fatalf("overlay open=%v quit=%v", h.m.showHelp, h.quit())
	}
}

func TestQB21BusyStatesIgnoreQ(t *testing.T) {
	h := newHarness(t, basicCfg())
	release := make(chan struct{})
	h.fake.On("git clone", execx.Response{Block: release})
	h.key("c")
	h.typeText("https://github.com/org/repo.git")
	h.key("enter")
	h.key("enter")
	cs := h.top().(*cloneScreen)
	if cs.phase != clRunning {
		t.Fatalf("phase %v", cs.phase)
	}
	h.key("q")
	if cs.phase != clRunning || h.depth() != 2 {
		t.Fatalf("phase %v depth %d", cs.phase, h.depth())
	}
	close(release)
	h.waitFor("finished", func() bool { return cs.phase != clRunning })
}

// ---- esc goes one step back (bug C5) ----

func TestESC1FixConfirmEscStaysOnTheCheck(t *testing.T) {
	h := fixConfirmHarness(t)
	c := h.top().(*checkScreen)
	h.key("esc")
	if h.top() != Screen(c) || c.phase != chResult || h.depth() != 2 {
		t.Fatalf("phase %v depth %d", c.phase, h.depth())
	}
	h.mustContain("wrong@example.com")
	if h.fake.Count("git -C "+h.m.sh.cwd+" remote set-url") != 0 {
		t.Fatal("esc must not fix anything")
	}
	h.key("esc") // ESC-3: now the screen closes
	if h.depth() != 1 {
		t.Fatalf("depth %d", h.depth())
	}
}

func TestESC2FixConfirmNoStillWorks(t *testing.T) {
	h := fixConfirmHarness(t)
	h.key("n")
	if c := h.top().(*checkScreen); c.phase != chResult || h.depth() != 2 {
		t.Fatalf("phase %v depth %d", c.phase, h.depth())
	}
}

func TestESC4To5KeygenNoPassEscGoesToTheModeChoice(t *testing.T) {
	h := keygenHarness(t)
	k := h.top().(*keygenScreen)
	h.key("tab")
	h.key("enter")
	if k.phase != kgNoPassConfirm {
		t.Fatalf("phase %v", k.phase)
	}
	h.key("esc")
	if h.top() != Screen(k) || k.phase != kgMode || k.mode.idx != 1 {
		t.Fatalf("phase %v mode %d", k.phase, k.mode.idx)
	}
	h.key("esc")
	if h.depth() != 1 {
		t.Fatalf("depth %d", h.depth())
	}
}

func TestESC6DeleteEscCancels(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.selectKey("acme/bob")
	h.key("d")
	h.key("esc")
	if h.depth() != 1 {
		t.Fatalf("depth %d", h.depth())
	}
	if _, err := h.m.sh.cfg.ParseRef("acme/bob"); err != nil {
		t.Fatal("bob must still exist")
	}
}

// ---- discard question in forms ----

const discardVi = "Bỏ những gì đã nhập? y/n"

func TestDIS1UnchangedFormClosesAtOnce(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("A")
	h.key("esc")
	if h.depth() != 1 {
		t.Fatalf("depth %d", h.depth())
	}
}

func TestDIS2To7TheQuestion(t *testing.T) {
	h := newHarness(t, nil)
	h.key("A")
	h.typeText("acme")
	h.key("esc")
	h.mustContainPlain(discardVi)
	if h.depth() != 2 {
		t.Fatalf("depth %d", h.depth())
	}
	f := h.top().(*companyForm)
	focus := f.form.focus
	// the help bar offers the two answers, not "enter save"
	if hb := h.helpLine(); !strings.Contains(hb, "y "+tr("tui.key.discard")) || !strings.Contains(hb, "n "+tr("tui.key.keep_editing")) || strings.Contains(hb, tr("tui.key.save")) {
		t.Fatalf("help bar %q", hb)
	}
	h.key("z") // DIS-7
	if !f.guard.asking || f.slug != "acme" {
		t.Fatalf("asking=%v slug %q", f.guard.asking, f.slug)
	}
	h.key("n") // DIS-3
	if f.guard.asking || f.slug != "acme" || f.form.focus != focus || h.depth() != 2 {
		t.Fatalf("asking=%v slug %q focus %d depth %d", f.guard.asking, f.slug, f.form.focus, h.depth())
	}
	h.mustNotContain(discardVi)
	h.key("esc")
	h.key("esc") // DIS-5
	if f.guard.asking || h.depth() != 2 {
		t.Fatalf("asking=%v depth %d", f.guard.asking, h.depth())
	}
	h.key("esc")
	h.key("enter") // DIS-6: nothing is submitted
	if f.guard.asking || h.depth() != 2 || f.saving {
		t.Fatalf("asking=%v depth %d saving %v", f.guard.asking, h.depth(), f.saving)
	}
	h.key("esc")
	h.key("q")
	if f.guard.asking || h.depth() != 2 {
		t.Fatalf("q keeps editing: asking=%v depth %d", f.guard.asking, h.depth())
	}
	h.key("esc")
	h.key("y") // DIS-4
	if h.depth() != 1 {
		t.Fatalf("depth %d", h.depth())
	}
	if len(h.m.sh.cfg.Companies) != 0 {
		t.Fatal("nothing may be saved")
	}
	if cfg, err := h.svc.Load(); err != nil || len(cfg.Companies) != 0 {
		t.Fatalf("disk: %v %v", cfg.Companies, err)
	}
}

func TestDIS8TypingAndDeletingIsNotAChange(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("A")
	h.typeText("ab")
	h.key("backspace")
	h.key("backspace")
	h.key("esc")
	if h.depth() != 1 {
		t.Fatalf("depth %d", h.depth())
	}
}

func TestDIS9To10EditAccount(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.selectKey("acme/bob")
	h.key("e")
	h.key("esc")
	if h.depth() != 1 {
		t.Fatalf("an unchanged edit must close at once, depth %d", h.depth())
	}
	h.key("e")
	f := h.top().(*accountForm)
	h.key("right") // platform select: another option
	if f.plat == string("github") {
		t.Fatalf("platform did not change: %q", f.plat)
	}
	h.key("esc")
	if !f.guard.asking || h.depth() != 2 {
		t.Fatalf("asking=%v depth %d", f.guard.asking, h.depth())
	}
}

func TestDIS11FormAfterFailedSaveStillAsks(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("A")
	h.typeText("zeta")
	h.key("enter")
	h.typeText("Zeta")
	h.key("enter") // name -> base dir
	h.writeBehind()
	h.key("enter") // base dir (last): submits
	h.waitFor("save failed", func() bool { f, ok := h.top().(*companyForm); return ok && !f.saving && f.errMsg != "" })
	h.key("esc")
	if !h.top().(*companyForm).guard.asking {
		t.Fatal("the values differ from the first open: ask")
	}
}

// writeBehind changes config.yaml behind the TUI's back so the next save is
// refused as stale.
func (h *harness) writeBehind() {
	h.t.Helper()
	cfg := cloneConfig(h.m.sh.cfg)
	cfg.Companies = append(cfg.Companies, config.Company{Slug: "other", Name: "Other", BaseDir: "~/Code/other/"})
	if _, _, err := app.New(h.e, execx.NewFake()).Commit(cfg); err != nil {
		h.t.Fatal(err)
	}
}

func TestDIS12ImportFormAsksAndGoesBackToTheList(t *testing.T) {
	h := importerHarness(t)
	h.key("enter") // pick: form opens on the platform
	s := h.top().(*importScreen)
	h.key("enter")
	h.typeText("dana")
	h.key("esc")
	if !s.guard.asking || s.phase != imForm {
		t.Fatalf("asking=%v phase %v", s.guard.asking, s.phase)
	}
	h.mustContainPlain(discardVi)
	h.key("y")
	if s.phase != imList || h.depth() != 2 {
		t.Fatalf("phase %v depth %d", s.phase, h.depth())
	}
	h.key("enter") // reopen: an unchanged form closes at once
	h.key("esc")
	if s.phase != imList {
		t.Fatalf("phase %v", s.phase)
	}
}

func TestDIS14English(t *testing.T) {
	defer i18n.SetLang("vi")
	i18n.SetLang("en")
	h := newHarness(t, nil)
	h.key("A")
	h.typeText("acme")
	h.key("esc")
	h.mustContainPlain("Discard what you typed? y/n")
}

func TestDIS15SavingIgnoresEsc(t *testing.T) {
	h := newHarness(t, nil)
	h.key("A")
	h.typeText("acme")
	h.key("enter")
	h.typeText("Acme")
	h.update(keyMsg("enter")) // base dir: submits, the save is in flight
	f := h.top().(*companyForm)
	if !f.saving {
		t.Skip("save finished too fast to observe")
	}
	h.update(keyMsg("esc"))
	if f.guard.asking {
		t.Fatal("esc while saving must be ignored")
	}
	h.drain()
}

// ---- other included items ----

func TestUX1EnterOnWelcomeAddsACompany(t *testing.T) {
	h := newHarness(t, nil)
	h.key("enter")
	if _, ok := h.top().(*companyForm); !ok || h.depth() != 2 {
		t.Fatalf("top %T depth %d", h.top(), h.depth())
	}
}

func TestUX2WelcomeHelpBarLeadsWithEnter(t *testing.T) {
	h := newHarness(t, nil)
	if hb := h.helpLine(); !strings.HasPrefix(hb, "▸ enter "+tr("tui.key.add_company")) {
		t.Fatalf("help bar %q", hb)
	}
}

func TestUX3ButtonsCarryAMarker(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.selectKey("acme/bob")
	h.key("d")
	no, yes := tr("tui.delete.no"), tr("tui.delete.yes")
	h.mustContain("▸ "+no, "  "+yes)
	h.mustNotContain("▸ " + yes)
	h.key("tab")
	h.mustContain("▸ "+yes, "  "+no)
	h.mustNotContain("▸ " + no)
	c := h.top().(*deleteScreen).pick
	// both states have the same width: the marker takes the place of two blanks
	c.idx = 0
	w0 := lipgloss.Width(c.view(h.m.sh.st))
	c.idx = 1
	if w1 := lipgloss.Width(c.view(h.m.sh.st)); w0 != w1 {
		t.Fatalf("widths differ: %d vs %d", w0, w1)
	}
	// UX-4: the fix confirmation and the keygen views too
	h = fixConfirmHarness(t)
	h.mustContain("▸ " + tr("tui.fix.no"))
	h = keygenHarness(t)
	h.mustContain("▸ " + tr("tui.keygen.with_pass"))
}

func TestUX5OverlayGlobalKeys(t *testing.T) {
	for _, s := range [][2]int{{80, 24}, {60, 20}, {100, 30}, {40, 10}} {
		h := newHarness(t, basicCfg())
		h.resize(s[0], s[1])
		h.key("?")
		v := h.view()
		assertFits(t, v, s[0], s[1], "overlay")
		if s == [2]int{40, 10} {
			continue // too small to show every row; only the fit is required
		}
		for _, want := range []string{tr("tui.help.global"), "esc/q", "↑/↓", "pgup", "home", "ctrl+c", tr("tui.help.close")} {
			if !strings.Contains(v, want) {
				t.Errorf("%v: overlay lacks %q:\n%s", s, want, v)
			}
		}
		if !strings.Contains(plain(v), "? "+tr("tui.key.help")) {
			t.Errorf("%v: no ? row:\n%s", s, v)
		}
	}
	if !strings.Contains(tr("tui.help.close"), "q") {
		t.Fatal("the close hint must mention q")
	}
}

func TestUX6ReloadPicksUpAnExternalChange(t *testing.T) {
	h := newHarness(t, basicCfg())
	cfg := cloneConfig(h.m.sh.cfg)
	cfg.Companies = append(cfg.Companies, config.Company{Slug: "newco", Name: "Newco", BaseDir: "~/Code/newco/"})
	if _, _, err := app.New(h.e, execx.NewFake()).Commit(cfg); err != nil {
		t.Fatal(err)
	}
	h.mustNotContain("newco")
	h.key("r")
	h.mustContain("newco")
	if h.status() != tr("tui.status.reloaded") {
		t.Fatalf("status %q", h.status())
	}
	// a save after the reload is not refused as stale
	h.key("A")
	h.typeText("third")
	h.key("enter")
	h.typeText("Third")
	h.key("enter") // name -> base dir
	h.key("enter") // submits
	h.waitFor("saved", func() bool {
		c, _, ok := h.m.sh.cfg.FindCompany("third")
		return ok && c.Slug == "third"
	})
}

func TestUX7ReloadWithBrokenYAMLKeepsTheList(t *testing.T) {
	h := newHarness(t, basicCfg())
	testutil.WriteFile(t, h.e.Paths().ConfigFile, "version: [unclosed\n", 0o644)
	h.key("r")
	if h.m.status.level != levelErr {
		t.Fatalf("status %+v", h.m.status)
	}
	h.mustContain("acme", "alice")
}

func TestUX8To10ClonePreselectsTheHighlightedAccount(t *testing.T) {
	h := newHarness(t, basicCfg())
	pickURL := func() *cloneScreen {
		h.typeText("https://github.com/org/repo.git")
		h.key("enter")
		return h.top().(*cloneScreen)
	}
	h.selectKey("acme/bob")
	h.key("c")
	cs := pickURL()
	if cs.phase != clPick || cs.cands[cs.cur].String() != "acme/bob" {
		t.Fatalf("cursor on %s", cs.cands[cs.cur])
	}
	h.key("esc") // pick -> url
	h.key("esc") // url -> list
	// a company row: its default account
	h.selectKey("acme")
	h.key("c")
	cs = pickURL()
	if got := cs.cands[cs.cur].String(); got != "acme/alice" {
		t.Fatalf("company row: cursor on %s", got)
	}
	h.key("esc")
	h.key("esc")
	// from the detail screen
	h.resize(50, 24)
	openDetail(h, "acme/bob")
	h.key("c")
	cs = pickURL()
	if got := cs.cands[cs.cur].String(); got != "acme/bob" {
		t.Fatalf("detail: cursor on %s", got)
	}
}

func TestUX10ClonePreselectDefaultsToTheFirstCandidate(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.applyToDone()
	h.key("c") // from the Done screen: nothing highlighted
	h.typeText("https://github.com/org/repo.git")
	h.key("enter")
	if cs := h.top().(*cloneScreen); cs.cur != 0 {
		t.Fatalf("cur %d", cs.cur)
	}
}

func TestUX11FaintContrast(t *testing.T) {
	lum := func(hex string) float64 {
		var r, g, b int
		if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil {
			t.Fatal(err)
		}
		f := func(c int) float64 {
			v := float64(c) / 255
			if v <= 0.03928 {
				return v / 12.92
			}
			return math.Pow((v+0.055)/1.055, 2.4)
		}
		return 0.2126*f(r) + 0.7152*f(g) + 0.0722*f(b)
	}
	ratio := func(a, b string) float64 {
		la, lb := lum(a), lum(b)
		if la < lb {
			la, lb = lb, la
		}
		return (la + 0.05) / (lb + 0.05)
	}
	if r := ratio(colFaint.Light, "#FFFFFF"); r < 4.5 {
		t.Errorf("light faint contrast %.2f", r)
	}
	if r := ratio(colFaint.Dark, "#1E1E1E"); r < 4.5 {
		t.Errorf("dark faint contrast %.2f", r)
	}
}

func TestUX12EnterOnTheApplyReviewSaysWhichKeyApplies(t *testing.T) {
	h := readyHarness(t)
	h.key("enter")
	if h.status() != tr("tui.status.press_yes", "y") || h.top().(*confirmScreen).state != confReady {
		t.Fatalf("status %q", h.status())
	}
}

func TestUX13DoneSSHHintIsAMultiStepInstruction(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.applyToDone()
	h.resize(120, 50)
	h.mustContainPlain(tr("tui.confirm.next_ssh"))
	if !strings.Contains(tr("tui.confirm.next_ssh"), "enter") {
		t.Fatal("the hint must say how to get back to the list")
	}
}

func TestUX14AllKeysItemIsNotOfferedWhereKeysAreCaptured(t *testing.T) {
	all := "? " + tr("tui.key.all_keys")
	h := newHarness(t, basicCfg())
	if !strings.Contains(h.helpLine(), all) {
		t.Fatalf("list: %q", h.helpLine())
	}
	for name, open := range map[string]func(*harness){
		"company form": func(h *harness) { h.key("A") },
		"clone url":    func(h *harness) { h.key("c") },
		"filter":       func(h *harness) { h.key("/") },
		"delete key": func(h *harness) {
			ref, _ := h.m.sh.cfg.ParseRef("acme/carol")
			ref.Account.SSHKey = ref.Company.DefaultKeyPath(ref.Account)
			d := newDeleteScreen(h.m.sh, ref.Company, &ref.Account)
			d.keys = []string{ref.Account.SSHKey}
			h.send(pushMsg{d})
			h.key("y")
		},
	} {
		h := newHarness(t, basicCfg())
		open(h)
		if hb := h.helpLine(); strings.Contains(hb, all) {
			t.Errorf("%s: %q still offers the overlay", name, hb)
		}
	}
}

// ---- guard: every advertised key is handled ----

type grdState struct {
	name  string
	setup func(t *testing.T) *harness
}

// importTwo is the importer with two proposals.
func importTwo(t *testing.T) *harness {
	t.Helper()
	e := testutil.TempEnv(t)
	testutil.WriteFile(t, e.Paths().GitConfig, "[user]\n\tname = x\n"+
		"[includeIf \"gitdir:~/Code/work/initech-solutions/\"]\n\tpath = ~/.gitconfig-initech\n"+
		"[includeIf \"gitdir:~/Code/work/hooli/\"]\n\tpath = ~/.gitconfig-hooli\n", 0o644)
	testutil.WriteFile(t, filepath.Join(e.Home, ".gitconfig-initech"), "[user]\n\tname = Dana Initech\n\temail = dana@initech.example\n", 0o644)
	testutil.WriteFile(t, filepath.Join(e.Home, ".gitconfig-hooli"), "[user]\n\tname = Gavin\n\temail = gavin@hooli.example\n", 0o644)
	h := newHarness(t, nil)
	h.m.sh.svc.Env = e
	h.key("i")
	h.waitFor("proposals", func() bool { return h.top().(*importScreen).phase == imList })
	return h
}

// grdOverlay are the states whose "?" overlay list is checked too; one state
// per screen is enough, the help bar of every state is always checked.
var grdOverlay = map[string]bool{
	"list welcome": true, "list account applied": true, "list pending": true, "detail ssh account": true,
	"confirm ready": true, "confirm done": true, "check result": true, "clone pick": true,
	"key result": true, "doctor": true, "import proposals": true, "delete step 0": true, "keygen mode": true,
}

func grdStates() []grdState {
	wide := func(h *harness) *harness { h.resize(100, 30); return h }
	return []grdState{
		{"list welcome", func(t *testing.T) *harness { return newHarness(t, nil) }},
		{"list company without accounts", func(t *testing.T) *harness { return newHarness(t, emptyCompanyCfg("personal")) }},
		{"list account applied", func(t *testing.T) *harness {
			h := newHarness(t, oneAccCfg())
			h.apply()
			h.selectKey("personal/tinhnd")
			return h
		}},
		{"list pending", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.selectKey("acme/alice")
			return h
		}},
		{"list ssh account without key", func(t *testing.T) *harness {
			h := newHarness(t, sshCfg())
			h.apply()
			h.selectKey("personal/alice")
			return h
		}},
		{"list narrow", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.resize(50, 24)
			h.selectKey("acme/bob")
			return h
		}},
		{"list filter typing", func(t *testing.T) *harness { h := newHarness(t, basicCfg()); h.key("/"); return h }},
		{"list filter applied", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.key("/")
			h.typeText("a")
			h.key("enter")
			return h
		}},
		{"detail company", func(t *testing.T) *harness {
			h := newHarness(t, emptyCompanyCfg("personal"))
			h.resize(50, 24)
			h.key("enter")
			return h
		}},
		{"detail ssh account", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.resize(50, 24)
			openDetail(h, "acme/carol")
			return h
		}},
		{"confirm ready", readyHarness},
		{"confirm done", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.applyToDone()
			return h
		}},
		{"confirm empty", func(t *testing.T) *harness {
			h := newHarness(t, oneAccCfg())
			h.apply()
			h.key("p")
			h.waitFor("empty", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confEmpty })
			return h
		}},
		{"confirm failed", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			c := newConfirmScreen(h.m.sh, "x")
			h.send(pushMsg{c})
			h.waitFor("loaded", func() bool { return c.state != confLoading })
			c.state, c.err = confFailed, errors.New("boom")
			return h
		}},
		{"check result", func(t *testing.T) *harness {
			h, _ := repoHarness(t, "bob")
			h.key("k")
			h.waitFor("result", func() bool { c, ok := h.top().(*checkScreen); return ok && c.phase == chResult })
			return h
		}},
		{"check not a repo", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.fake.On("git -C "+h.e.Home+" rev-parse --show-toplevel", execx.Response{ExitCode: 128})
			h.key("k")
			h.waitFor("error", func() bool { c, ok := h.top().(*checkScreen); return ok && c.phase == chError })
			return h
		}},
		{"check fix confirm", fixConfirmHarness},
		{"clone url", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.key("c")
			h.typeText("https://github.com/org/repo.git")
			return h
		}},
		{"clone pick", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.key("c")
			h.typeText("https://github.com/org/repo.git")
			h.key("enter")
			return h
		}},
		{"clone failed", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.key("c")
			h.typeText("https://github.com/org/repo.git")
			h.key("enter")
			cs := h.top().(*cloneScreen)
			cs.phase, cs.err = clFailed, app.ErrAuth
			return h
		}},
		{"clone done", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.key("c")
			h.typeText("https://github.com/org/repo.git")
			h.key("enter")
			cs := h.top().(*cloneScreen)
			cs.phase = clDone
			cs.res = app.CloneResult{Plan: cs.preview, EmailOK: true}
			return h
		}},
		{"delete step 0", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.selectKey("acme/bob")
			h.key("d")
			return h
		}},
		{"delete step 1", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			ref, _ := h.m.sh.cfg.ParseRef("acme/carol")
			ref.Account.SSHKey = ref.Company.DefaultKeyPath(ref.Account)
			d := newDeleteScreen(h.m.sh, ref.Company, &ref.Account)
			d.keys = []string{ref.Account.SSHKey}
			h.send(pushMsg{d})
			h.key("y")
			return h
		}},
		{"keygen mode", keygenHarness},
		{"keygen no-pass confirm", func(t *testing.T) *harness {
			h := keygenHarness(t)
			h.key("tab")
			h.key("enter")
			return h
		}},
		{"keygen error", func(t *testing.T) *harness {
			h := keygenHarness(t)
			k := h.top().(*keygenScreen)
			k.phase, k.errMsg = kgError, "boom"
			return h
		}},
		{"key result", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			ref, _ := h.m.sh.cfg.ParseRef("acme/carol")
			h.send(pushMsg{newKeyResultScreen(h.m.sh, ref, &keys.Pub{Content: "ssh-ed25519 AAAA carol"})})
			return h
		}},
		{"doctor", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.resize(100, 12) // short, so that scrolling has an effect
			h.key("D")
			h.waitFor("checks", func() bool { return !h.top().(*doctorScreen).pending() })
			return h
		}},
		{"import proposals", importTwo},
		{"import none", func(t *testing.T) *harness {
			h := newHarness(t, nil)
			h.key("i")
			h.waitFor("loaded", func() bool { return h.top().(*importScreen).phase == imList })
			return h
		}},
		{"import form", func(t *testing.T) *harness {
			h := importTwo(t)
			h.key("enter")
			return h
		}},
		{"company form", func(t *testing.T) *harness { return wide(newHarness(t, basicCfg())).withKey("A") }},
		{"account form", func(t *testing.T) *harness { return wide(newHarness(t, basicCfg())).withKey("a") }},
		{"form discard prompt", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.key("A")
			h.typeText("acme")
			h.key("esc")
			return h
		}},
	}
}

func (h *harness) withKey(k string) *harness { h.key(k); return h }

// grdKey is the message for the first key of a binding.
func grdKey(k string) tea.KeyMsg { return keyMsg(k) }

// grdAllowed lists hints that name a key which is deliberately not a single
// press on that screen: each needs a step first, which the text says.
var grdAllowed = map[string]string{
	"confirm empty/a":                 "tui.confirm.empty_no_accounts says: press esc, then a",
	"import none/A":                   "tui.import.none_next says: press esc, then A",
	"confirm done/g":                  "tui.confirm.next_ssh says: enter to the list, select the account, then g",
	"account form/k":                  "tui.form.warn.username_changed names a key to press after saving",
	"account form/f":                  "tui.form.warn.username_changed names a key to press after saving",
	"company form/a":                  "the company form hint names a key for after saving",
	"list welcome/a":                  "the welcome steps list the keys of steps 2 and 3, which need a company first",
	"list welcome/p":                  "the welcome steps list the keys of steps 2 and 3, which need a company first",
	"list welcome/A":                  "the welcome step 1 names A, which works; enter is the primary item",
	"list company without accounts/p": "the no-accounts text names the next steps",
}

// TestGRD1EveryAdvertisedKeyIsHandled opens every screen state, takes each key
// of its help bar, of the "?" overlay and of the next-step hints in the frame,
// and checks that pressing it has an effect there.
func TestGRD1EveryAdvertisedKeyIsHandled(t *testing.T) {
	defaultIdle = 15 * time.Millisecond // many harnesses: commands are tracked, so a short idle wait is safe
	defer func() { defaultIdle = 0 }()
	for _, st := range grdStates() {
		t.Run(st.name, func(t *testing.T) {
			probe := st.setup(t)
			top := probe.top()
			capturing := isCapturing(top)
			type probeKey struct{ key, why string }
			var todo []probeKey
			seen := map[string]bool{}
			add := func(k, why string) {
				if k == "" || seen[k] {
					return
				}
				seen[k] = true
				todo = append(todo, probeKey{k, why})
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
				add("?", "all keys")
			}
			for _, k := range hintKeys(plain(probe.view())) {
				if _, ok := grdAllowed[st.name+"/"+k]; ok {
					continue
				}
				add(k, "hint in the frame")
			}
			for _, pk := range todo {
				h := st.setup(t)
				switch pk.key {
				case "up":
					h.key("down")
					before := h.snapshot()
					h.key("up")
					if h.snapshot() == before {
						t.Errorf("%s (%s): up has no effect", st.name, pk.why)
					}
					continue
				case "down", "ctrl+n":
					before := h.snapshot()
					h.send(grdKey(pk.key))
					if h.snapshot() == before {
						t.Errorf("%s (%s): %s has no effect", st.name, pk.why, pk.key)
					}
					continue
				}
				before := h.snapshot()
				h.send(grdKey(pk.key))
				if pk.key == "?" {
					if !h.m.showHelp {
						t.Errorf("%s (%s): ? did not open the overlay", st.name, pk.why)
					}
					continue
				}
				if h.snapshot() == before {
					t.Errorf("%s (%s): key %q has no effect:\n%s", st.name, pk.why, pk.key, h.view())
				}
			}
		})
	}
}

// hintKeys finds the single-character keys the frame names in prose: "press
// p", "bấm k", "phím A".
func hintKeys(frame string) []string {
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

func TestGRD5NoBrokenFormatVerbsInFrames(t *testing.T) {
	for _, st := range grdStates() {
		t.Run(st.name, func(t *testing.T) {
			h := st.setup(t)
			if strings.Contains(h.view(), "%!") {
				t.Fatalf("a frame shows a format error:\n%s", h.view())
			}
		})
	}
}

func TestOverlayClosesWithHelpBackAndQuit(t *testing.T) {
	for _, k := range []string{"?", "esc", "q"} {
		h := newHarness(t, basicCfg())
		h.key("?")
		if !h.m.showHelp {
			t.Fatal("overlay did not open")
		}
		h.key(k)
		if h.m.showHelp || h.quit() {
			t.Fatalf("%s: open=%v quit=%v", k, h.m.showHelp, h.quit())
		}
	}
}

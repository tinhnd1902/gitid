package tui

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/keymap"
	"github.com/tinhnd1902/gitid/internal/testutil"
)

// The start menu (home.go): H1..H22 of the test plan.

func onHome(h *harness) *homeScreen {
	h.t.Helper()
	s, ok := h.top().(*homeScreen)
	if !ok {
		h.t.Fatalf("top is %T, want *homeScreen", h.top())
	}
	return s
}

func withEnglish(t *testing.T) {
	t.Helper()
	i18n.SetLang("en")
	t.Cleanup(func() { i18n.SetLang("vi") })
}

// H1: the first screen is the start menu with logo, tagline, version and items.
func TestH1HomeIsTheFirstScreen(t *testing.T) {
	h := newHomeHarness(t, basicCfg())
	onHome(h)
	if len(h.m.stack) != 1 || h.depth() != 0 {
		t.Fatalf("stack %d", len(h.m.stack))
	}
	v := h.view()
	for _, l := range logoLines {
		if !strings.Contains(v, strings.TrimRight(l, " ")) {
			t.Errorf("the logo row %q is missing:\n%s", l, v)
		}
	}
	h.mustContain("Đúng tài khoản Git cho từng repo.", "vtest")
	for i, it := range homeItems {
		h.mustContain(string(rune('1'+i))+"  "+tr("tui.home.item."+it.name), tr("tui.home.item."+it.name+".desc"))
	}
	if strings.Contains(h.lines()[0], "Trang chủ") {
		t.Errorf("the header names the start menu: %q", h.lines()[0])
	}
}

// What each digit opens.
func TestH2DigitsOpenTheItems(t *testing.T) {
	want := []func(Screen) bool{
		func(s Screen) bool { _, ok := s.(*listScreen); return ok },
		func(s Screen) bool { r, ok := s.(*reposScreen); return ok && r.tab == tabURL },
		func(s Screen) bool { _, ok := s.(*checkScreen); return ok },
		func(s Screen) bool { r, ok := s.(*reposScreen); return ok && r.tab == tabLocal },
		func(s Screen) bool { _, ok := s.(*confirmScreen); return ok },
		func(s Screen) bool { _, ok := s.(*doctorScreen); return ok },
		func(s Screen) bool { _, ok := s.(*importScreen); return ok },
	}
	for i, ok := range want {
		h := newHomeHarness(t, basicCfg())
		h.key(string(rune('1' + i)))
		if !ok(h.top()) || h.depth() != 1 {
			t.Errorf("digit %d opened %T (depth %d)", i+1, h.top(), h.depth())
		}
		if h.m.stack[0].(*homeScreen).sel != i {
			t.Errorf("digit %d: the cursor is on row %d", i+1, h.m.stack[0].(*homeScreen).sel)
		}
	}
}

// H3: other keys that look like digits do nothing (8 is the last item since the Settings screen; 9 is the first one past the menu).
func TestH3OtherDigitsDoNothing(t *testing.T) {
	h := newHomeHarness(t, basicCfg())
	before := h.snapshot()
	for _, k := range []string{"9", "0", "alt+1", "!", "x"} {
		h.key(k)
		if h.snapshot() != before {
			t.Errorf("%q changed the screen", k)
		}
	}
	if h.quit() {
		t.Fatal("quit")
	}
}

// H4: the cursor clamps at both ends and enter opens the row under it.
func TestH4CursorAndEnter(t *testing.T) {
	h := newHomeHarness(t, basicCfg())
	s := onHome(h)
	h.key("up")
	if s.sel != 0 {
		t.Fatalf("up at the top moved to %d", s.sel)
	}
	for i := 0; i < 10; i++ {
		h.key("down")
	}
	if s.sel != len(homeItems)-1 {
		t.Fatalf("down x10: %d", s.sel)
	}
	h.key("home")
	if s.sel != 0 {
		t.Fatalf("home: %d", s.sel)
	}
	h.key("end")
	if s.sel != len(homeItems)-1 {
		t.Fatalf("end: %d", s.sel)
	}
	h.key("home")
	h.key("ctrl+n")
	h.key("down")
	h.key("ctrl+p")
	h.key("pgdown")
	h.key("pgup")
	if s.sel != 0 {
		t.Fatalf("pages go back and forth: %d", s.sel)
	}
	h.key("down")
	h.key("down")
	h.key("enter")
	if _, ok := h.top().(*checkScreen); !ok || h.depth() != 1 {
		t.Fatalf("enter on row 3 opened %T", h.top())
	}
}

// H5: the old letters still open the same screens from the menu.
func TestH5LetterAliases(t *testing.T) {
	for k, want := range map[string]reflect.Type{
		"c": reflect.TypeOf(&reposScreen{}), "k": reflect.TypeOf(&checkScreen{}), "p": reflect.TypeOf(&confirmScreen{}),
		"D": reflect.TypeOf(&doctorScreen{}), "i": reflect.TypeOf(&importScreen{}),
	} {
		h := newHomeHarness(t, basicCfg())
		h.key(k)
		if reflect.TypeOf(h.top()) != want || h.depth() != 1 {
			t.Errorf("%q opened %T", k, h.top())
		}
	}
}

// H5b: with every action on its own alt+letter the letters follow and the
// plain letters are dead.
func TestH5bAliasesFollowTheKeymap(t *testing.T) {
	y, label := sentinelKeys()
	for a, want := range map[keymap.Action]reflect.Type{
		keymap.Clone: reflect.TypeOf(&reposScreen{}), keymap.Check: reflect.TypeOf(&checkScreen{}), keymap.Apply: reflect.TypeOf(&confirmScreen{}),
		keymap.Doctor: reflect.TypeOf(&doctorScreen{}), keymap.Import: reflect.TypeOf(&importScreen{}),
	} {
		h := newHomeHarness(t, basicCfg(), y)
		h.key(label[a])
		if reflect.TypeOf(h.top()) != want {
			t.Errorf("%s opened %T", label[a], h.top())
		}
	}
	h := newHomeHarness(t, basicCfg(), y)
	before := h.snapshot()
	for _, k := range []string{"c", "k", "p", "D", "i", "q"} {
		h.key(k)
	}
	if h.snapshot() != before || h.quit() {
		t.Fatal("a plain letter did something")
	}
	h.key(label[keymap.Quit])
	if !h.quit() {
		t.Fatal("the quit key does not quit")
	}
}

// H6: q quits, esc does not, ctrl+c quits from anywhere.
func TestH6Quit(t *testing.T) {
	h := newHomeHarness(t, basicCfg())
	before := h.snapshot()
	h.key("esc")
	if h.snapshot() != before || h.quit() {
		t.Fatal("esc on the start menu did something")
	}
	h.key("q")
	if !h.quit() {
		t.Fatal("q did not quit")
	}

	h = newHarness(t, basicCfg())
	h.selectKey("acme/alice")
	h.key("enter")
	if _, ok := h.top().(*detailScreen); !ok || h.depth() != 2 {
		t.Fatalf("top %T depth %d", h.top(), h.depth())
	}
	h.key("ctrl+c")
	if !h.quit() {
		t.Fatal("ctrl+c did not quit from the detail")
	}
}

// H7: back from Accounts goes to the menu; a filter clears first.
func TestH7BackFromAccounts(t *testing.T) {
	h := newHomeHarness(t, basicCfg())
	h.key("1")
	h.key("esc")
	onHome(h)
	h.key("1")
	h.key("q")
	onHome(h)
	if h.quit() {
		t.Fatal("q on Accounts quit the program")
	}

	h.key("1")
	h.key("/")
	h.typeText("ali")
	h.key("enter")
	l := h.listScreen()
	if l.mode != filterApplied {
		t.Fatalf("filter mode %v", l.mode)
	}
	h.key("esc")
	if h.top() != Screen(l) || l.mode != filterOff || l.filter.Value() != "" {
		t.Fatalf("the first esc: top %T mode %v value %q", h.top(), l.mode, l.filter.Value())
	}
	h.key("esc")
	onHome(h)

	// q clears a filter first as well
	h.key("1")
	h.key("/")
	h.typeText("bob")
	h.key("enter")
	h.key("q")
	if _, ok := h.top().(*listScreen); !ok {
		t.Fatalf("q with a filter left the list: %T", h.top())
	}
	h.key("q")
	onHome(h)
}

// H8: digits are text wherever text is typed.
func TestH8DigitsAreTextInInputs(t *testing.T) {
	h := newHomeHarness(t, basicCfg())
	h.key("1")
	h.key("/")
	h.typeText("12q")
	if l := h.listScreen(); l.filter.Value() != "12q" || h.depth() != 1 {
		t.Fatalf("list filter %q, depth %d", l.filter.Value(), h.depth())
	}
	h.key("esc")

	h = newHomeHarness(t, basicCfg())
	h.key("1")
	h.selectKey("acme/alice")
	h.act(keymap.AddAccount)
	f := accountFormTop(t, h)
	h.key("enter") // past the platform choice, onto the username
	h.typeText("123")
	if !strings.Contains(h.view(), "123") || h.top() != Screen(f) {
		t.Fatalf("the form did not take the digits:\n%s", h.view())
	}

	h = newHomeHarness(t, basicCfg())
	h.key("4")
	r, ok := h.top().(*reposScreen)
	if !ok || r.tab != tabLocal {
		t.Fatalf("top %T", h.top())
	}
	h.key("/")
	h.typeText("7")
	if r.filter.Value() != "7" || h.depth() != 1 {
		t.Fatalf("repo filter %q, depth %d", r.filter.Value(), h.depth())
	}

	h = newHomeHarness(t, basicCfg())
	h.key("?")
	if !h.m.showHelp {
		t.Fatal("the overlay did not open")
	}
	h.key("1")
	if !h.m.showHelp || h.m.ov.filter.Value() != "1" {
		t.Fatalf("overlay open %v, filter %q", h.m.showHelp, h.m.ov.filter.Value())
	}
	onHome(h)
	if h.depth() != 0 {
		t.Fatal("the digit navigated under the overlay")
	}
}

// H9: apply, then enter, goes back to where the flow started.
func TestH9ReturnAfterApply(t *testing.T) {
	h := newHarness(t, basicCfg()) // [home, list]
	h.act(keymap.Apply)
	h.waitFor("diff ready", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confReady })
	h.act(keymap.Yes)
	h.waitFor("apply done", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confDone })
	h.key("enter")
	if _, ok := h.top().(*listScreen); !ok || h.depth() != 1 {
		t.Fatalf("from the list: top %T depth %d", h.top(), h.depth())
	}

	h = newHomeHarness(t, basicCfg())
	h.key("5")
	h.waitFor("diff ready", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confReady })
	h.act(keymap.Yes)
	h.waitFor("apply done", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confDone })
	h.key("enter")
	onHome(h)
	if h.depth() != 0 {
		t.Fatalf("from home: depth %d", h.depth())
	}
}

// H10: the menu is not a crumb; the overlay names it.
func TestH10HeaderAndOverlayHeading(t *testing.T) {
	h := newHomeHarness(t, basicCfg())
	h.key("1")
	head := plain(h.lines()[0])
	if !strings.HasPrefix(head, "gitid Tài khoản") || strings.Contains(head, "Trang chủ") {
		t.Fatalf("header %q", head)
	}
	h.key("esc")
	h.key("?")
	h.mustContain("Trang chủ")

	withEnglish(t)
	h = newHomeHarness(t, basicCfg())
	h.key("?")
	h.mustContain("Home")
	h.key("esc")
	h.key("1")
	if head := plain(h.lines()[0]); !strings.HasPrefix(head, "gitid Accounts") {
		t.Fatalf("header %q", head)
	}
}

// H11: every frame is exactly w x h, the logo shows by the rule, and the last
// row stays reachable.
func TestH11Sizes(t *testing.T) {
	n := len(homeItems)
	for _, lang := range []string{"vi", "en"} {
		i18n.SetLang(lang)
		for _, tc := range []struct {
			w, h   int
			update bool
		}{
			{80, 24, false}, {80, 24, true}, {100, 30, true}, {80, 17, false}, {80, 17, true}, {80, 16, false}, {60, 16, false},
			{44, 12, false}, {44, 12, true}, {40, 10, false}, {40, 10, true}, {50, 40, false}, {200, 60, true},
		} {
			h := newHomeHarness(t, basicCfg())
			if tc.update {
				h.send(updateMsg{notice: app.UpdateNotice{Latest: "0.1.9", Method: app.UpdateViaScript}, ok: true})
			}
			h.resize(tc.w, tc.h)
			v := h.view()
			lines := strings.Split(v, "\n")
			if len(lines) != tc.h {
				t.Errorf("%s %dx%d update=%v: %d lines", lang, tc.w, tc.h, tc.update, len(lines))
			}
			for i, l := range lines {
				if lw := lipgloss.Width(l); lw > tc.w {
					t.Errorf("%s %dx%d update=%v: line %d is %d wide: %q", lang, tc.w, tc.h, tc.update, i, lw, l)
				}
			}
			u := 0
			if tc.update {
				u = 1
			}
			_, bh := h.m.bodySize()
			wantLogo := bh >= 10+n+u && tc.w >= homeFullMinW()
			if got := strings.Contains(v, strings.TrimRight(logoLines[1], " ")); got != wantLogo {
				t.Errorf("%s %dx%d update=%v: logo shown %v, want %v", lang, tc.w, tc.h, tc.update, got, wantLogo)
			}
			// the whole menu is there when it fits, and the last row after end
			h.key("end")
			last := tr("tui.home.item." + homeItems[n-1].name)
			if !strings.Contains(h.view(), string(rune('0'+n))+"  "+last) {
				t.Errorf("%s %dx%d update=%v: the last row is not visible after end:\n%s", lang, tc.w, tc.h, tc.update, h.view())
			}
			if bh >= 2+n+u && !strings.Contains(h.view(), "1  "+tr("tui.home.item.accounts")) {
				t.Errorf("%s %dx%d update=%v: a menu that fits lacks its first row", lang, tc.w, tc.h, tc.update)
			}
		}
	}
	i18n.SetLang("vi")

	// a resize from the smallest to a large frame
	h := newHomeHarness(t, basicCfg())
	h.resize(40, 10)
	h.resize(100, 30)
	h.mustContain(strings.TrimRight(logoLines[1], " "), "Đúng tài khoản Git cho từng repo.")
}

// A window shorter than the menu scrolls with the cursor.
func TestH11MenuTallerThanTheBodyScrolls(t *testing.T) {
	h := newHomeHarness(t, basicCfg())
	s := onHome(h)
	s.Resize(40, 4) // not reachable through the root (it needs 10 rows); the screen copes anyway
	for i := 0; i < len(homeItems); i++ {
		s.sel = i
		v := s.View()
		if got := len(strings.Split(v, "\n")); got != 4 {
			t.Fatalf("sel %d: %d lines", i, got)
		}
		if !strings.Contains(v, string(rune('1'+i))+"  ") {
			t.Fatalf("sel %d is not visible:\n%s", i, v)
		}
	}
}

func updateNoticeLine(h *harness) string {
	for _, l := range h.lines() {
		if strings.Contains(l, "0.1.9") {
			return strings.TrimSpace(l)
		}
	}
	return ""
}

// H12: the update line, per method and language, only on the menu.
func TestH12UpdateLine(t *testing.T) {
	for _, tc := range []struct {
		lang   string
		method app.UpdateNotice
		want   string
	}{
		{"vi", app.UpdateNotice{Latest: "0.1.9", Method: app.UpdateViaBrew}, "Có bản mới 0.1.9 · chạy brew upgrade gitid"},
		{"vi", app.UpdateNotice{Latest: "0.1.9", Method: app.UpdateViaScript}, "Có bản mới 0.1.9 · cập nhật bằng install.sh như lúc cài"},
		{"vi", app.UpdateNotice{Latest: "0.1.9", Method: app.UpdateViaUnknown}, "Có bản mới 0.1.9 · xem github.com/tinhnd1902/gitid/releases"},
		{"en", app.UpdateNotice{Latest: "0.1.9", Method: app.UpdateViaBrew}, "gitid 0.1.9 is out · run brew upgrade gitid"},
		{"en", app.UpdateNotice{Latest: "0.1.9", Method: app.UpdateViaScript}, "gitid 0.1.9 is out · update with install.sh, as when you installed it"},
		{"en", app.UpdateNotice{Latest: "0.1.9", Method: app.UpdateViaUnknown}, "gitid 0.1.9 is out · see github.com/tinhnd1902/gitid/releases"},
	} {
		i18n.SetLang(tc.lang)
		h := newHomeHarness(t, basicCfg())
		if updateNoticeLine(h) != "" {
			t.Fatal("an update line before any notice")
		}
		h.send(updateMsg{notice: tc.method, ok: true})
		if got := updateNoticeLine(h); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.lang, got, tc.want)
		}
		h.key("1")
		if updateNoticeLine(h) != "" {
			t.Errorf("%s: the update line shows on another screen", tc.lang)
		}
	}
	i18n.SetLang("vi")
}

// H12b: no notice, no line, and the same layout.
func TestH12bNoNoticeNoLine(t *testing.T) {
	h := newHomeHarness(t, basicCfg())
	base := h.view()
	h.send(updateMsg{ok: false})
	if h.view() != base {
		t.Fatal("an empty update changed the frame")
	}
	// text that is not a version never reaches the screen
	h.send(updateMsg{notice: app.UpdateNotice{Latest: "9.9.9\x1b[2J"}, ok: true})
	if h.view() != base {
		t.Fatalf("a hostile version changed the frame:\n%s", h.view())
	}
}

// The update line cuts at a word on a narrow terminal and keeps the width.
func TestUpdateLineIsCutNotWrapped(t *testing.T) {
	h := newHomeHarness(t, basicCfg())
	h.send(updateMsg{notice: app.UpdateNotice{Latest: "0.1.9", Method: app.UpdateViaScript}, ok: true})
	h.resize(44, 24)
	for i, l := range h.lines() {
		if lipgloss.Width(l) > 44 {
			t.Errorf("line %d too wide: %q", i, l)
		}
	}
	h.mustContain("Có bản mới 0.1.9")
}

// H13: with the check on, a newer release shows up through the real service.
func TestH13UpdateThroughTheService(t *testing.T) {
	var mu sync.Mutex
	var hits int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.Header().Set("Location", "https://github.com/tinhnd1902/gitid/releases/tag/v0.2.0")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	h := buildHarnessOpts(t, basicCfg(), func(e *env.Env) { e.HTTP = testutil.RedirectTransport(srv) }, nil,
		Options{CheckUpdates: true}, func(svc *app.Service) {
			svc.Version = "0.1.0"
			svc.Executable = func() (string, error) { return "/opt/homebrew/Caskroom/gitid/0.1.0/gitid", nil }
		})
	h.waitFor("the update line", func() bool { return strings.Contains(h.view(), "Có bản mới 0.2.0 · chạy brew upgrade gitid") })
	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Fatalf("%d requests", hits)
	}
}

// H13b: without CheckUpdates nothing is requested (the transport of TempEnv
// fails the test on a request).
func TestH13bNoCheckNoRequest(t *testing.T) {
	h := buildHarnessOpts(t, basicCfg(), nil, nil, Options{}, func(svc *app.Service) { svc.Version = "0.1.0" })
	h.drain()
	if updateNoticeLine(h) != "" || h.m.sh.hasUpdate {
		t.Fatal("an update without a check")
	}
}

// H14: a stalled check never delays the first frame or the keys.
func TestH14StalledCheckDoesNotBlock(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) }) // runs first: lets the handler go before srv.Close waits for it
	e := testutil.TempEnv(t)
	e.HTTP = testutil.RedirectTransport(srv)
	svc := app.New(e, nil)
	svc.Version = "0.1.0"

	start := time.Now()
	m := New(svc, Options{Cwd: e.Home, Version: "0.1.0", CheckUpdates: true})
	cmd := m.Init()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if v := m.View(); !strings.Contains(v, "Tài khoản") {
		t.Fatalf("first frame:\n%s", v)
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("Init and the first frame took %v", d)
	}
	// the commands run in the background, as Bubble Tea does
	var wg sync.WaitGroup
	var run func(c tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil || animation(c) {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if b, ok := c().(tea.BatchMsg); ok {
				for _, sub := range b {
					run(sub)
				}
			}
		}()
	}
	run(cmd)
	time.Sleep(50 * time.Millisecond)
	m.Update(keyMsg("1"))
	if len(m.stack) != 2 {
		t.Fatalf("a key while the request is pending: stack %d", len(m.stack))
	}
	if !strings.Contains(m.View(), "gitid") {
		t.Fatal("no frame")
	}
}

// H15: the status badge shows on the menu.
func TestH15StatusBadgeOnHome(t *testing.T) {
	h := newHomeHarness(t, basicCfg())
	h.waitFor("badge", func() bool { return strings.Contains(h.badge(), "thay đổi chưa áp dụng") })
	h.mustContain("● ")

	h = newHomeHarness(t, nil)
	h.waitFor("badge", func() bool { return strings.Contains(h.badge(), "Chưa có công ty") })
	h.mustContain(tr("tui.home.item.accounts.desc_empty"))
	h.mustNotContain(tr("tui.home.item.accounts.desc"))
}

// H16: English.
func TestH16English(t *testing.T) {
	withEnglish(t)
	h := newHomeHarness(t, basicCfg())
	h.mustContain("The right Git account for every repo.", "Clone a repo", "Import from git")
	hb := h.helpLine()
	for _, want := range []string{"enter open", "1-8 jump", "q quit", "all keys"} {
		if !strings.Contains(hb, want) {
			t.Errorf("help bar %q lacks %q", hb, want)
		}
	}
}

// The help bar of the menu: next step in row 1, the digits, the arrows and the
// exit in row 2, "? all keys" before the exit.
func TestHelpBarOfTheMenu(t *testing.T) {
	h := newHomeHarness(t, basicCfg())
	rows := h.helpRows()
	if len(rows) != 2 || !strings.HasPrefix(rows[0], "▸ enter mở") {
		t.Fatalf("rows %q", rows)
	}
	want := []string{"1-8 chọn nhanh", "↑↓ di chuyển", "? tất cả phím", "q thoát"}
	prev := -1
	for _, w := range want {
		i := strings.Index(rows[1], w)
		if i < 0 || i < prev {
			t.Fatalf("row 2 %q: %q is missing or out of order", rows[1], w)
		}
		prev = i
	}
}

// H19: a custom quit key works on the menu only.
func TestH19CustomQuit(t *testing.T) {
	h := newHomeHarness(t, basicCfg(), "quit: Q\n")
	h.key("q")
	if h.quit() {
		t.Fatal("q quit although quit is Q")
	}
	h.key("Q")
	if !h.quit() {
		t.Fatal("Q did not quit")
	}

	h2 := newHarnessKeys(t, basicCfg(), "quit: Q\n")
	h2.key("q")
	onHome(h2)
	h2.key("1")
	h2.key("Q")
	if h2.quit() {
		t.Fatal("Q quit from the Accounts screen")
	}
}

// H20: a digit bound to a menu action is refused; the defaults stay.
func TestH20DigitKeyIsRefused(t *testing.T) {
	h := newHomeHarness(t, basicCfg(), "check: \"1\"\n")
	if len(h.m.sh.keyIssues) != 1 || h.m.sh.keyIssues[0].Code != keymap.ErrReserved {
		t.Fatalf("issues %+v", h.m.sh.keyIssues)
	}
	h.mustContain("keys.yaml")
	h.key("1")
	if _, ok := h.top().(*listScreen); !ok {
		t.Fatalf("1 opened %T", h.top())
	}
	h.key("esc")
	h.key("k")
	if _, ok := h.top().(*checkScreen); !ok {
		t.Fatalf("k opened %T", h.top())
	}
}

// H21: a panic in the update command is no notice and does not stop the TUI.
func TestH21PanicInTheUpdateCommand(t *testing.T) {
	h := buildHarnessOpts(t, basicCfg(), nil, nil, Options{CheckUpdates: true}, func(svc *app.Service) {
		svc.Version = "0.1.0"
		svc.Executable = func() (string, error) { panic("boom") }
		// a fresh cache names a newer release, so the method is looked up
		testutil.WriteFile(t, svc.Env.Paths().UpdateCheckFile,
			`{"checked":"`+svc.Env.Now().UTC().Format(time.RFC3339)+`","latest":"0.2.0"}`, 0o600)
	})
	h.drain()
	if h.m.sh.hasUpdate || updateNoticeLine(h) != "" {
		t.Fatal("an update after a panic")
	}
	h.key("1")
	if _, ok := h.top().(*listScreen); !ok {
		t.Fatalf("the TUI stopped working: %T", h.top())
	}
}

// H22: composed input on the menu warns and goes nowhere.
func TestH22IMEInput(t *testing.T) {
	h := newHomeHarness(t, basicCfg())
	before := h.m.top()
	h.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'â'}})
	if h.m.top() != before || h.depth() != 0 {
		t.Fatal("the composed rune navigated")
	}
	h.mustContain(tr("tui.status.ime"))
}

// The logo: six rows of block letters, 30 to 34 wide, accent colour only.
func TestLogoShape(t *testing.T) {
	if len(logoLines) != 6 {
		t.Fatalf("%d rows", len(logoLines))
	}
	if w := logoWidth(); w > 34 || w < 30 {
		t.Fatalf("width %d", w)
	}
	for _, l := range logoLines {
		for _, r := range l {
			if !strings.ContainsRune(" █═║╔╗╚╝", r) {
				t.Errorf("%q has the unexpected rune %q", l, r)
			}
		}
	}
	st := NewStyles()
	if st.Logo.GetBold() || st.Logo.GetItalic() || st.Logo.GetUnderline() {
		t.Error("the logo is bold, italic or underlined")
	}
	if _, none := st.Logo.GetBackground().(lipgloss.NoColor); !none {
		t.Error("the logo has a background")
	}
	if st.Logo.GetForeground() != lipgloss.TerminalColor(colAccent) {
		t.Errorf("logo colour %v", st.Logo.GetForeground())
	}
}

// Rendering the menu never produces an escape sequence the app did not mean:
// the whole frame survives a strip with nothing but styling removed.
func TestHomeFrameHasOnlyStyling(t *testing.T) {
	h := newHomeHarness(t, basicCfg())
	v := h.view()
	if strings.ContainsRune(v, 0x1b) {
		t.Fatalf("escape in an Ascii-profile frame: %q", v)
	}
	if ansi.Strip(v) != v {
		t.Fatal("a frame with the Ascii profile has styling")
	}
}

// keymap help: the menu rows of the overlay, with the item names.
func TestOverlayOnHomeNamesTheItems(t *testing.T) {
	h := newHomeHarness(t, basicCfg())
	h.key("?")
	h.mustContain("1-8", "Clone repo", "Kiểm tra repo", "Áp dụng", "Chẩn đoán", "Nhập từ git")
	h.mustNotContain(tr("tui.key.back_global"))
}

// H17: the start menu at 80x24, as goldens (run with -update to refresh).
func homeGoldenHarness(t *testing.T) *harness {
	t.Helper()
	h := buildHarnessOpts(t, basicCfg(), nil, nil, Options{Version: "0.1.8"}, nil)
	h.resize(80, 24)
	h.waitFor("the pending badge", func() bool { return h.m.sh.planned })
	return h
}

func TestHomeGolden80x24(t *testing.T) {
	h := homeGoldenHarness(t)
	golden.RequireEqual(t, []byte(h.view()))
}

func TestHomeGolden80x24Update(t *testing.T) {
	h := homeGoldenHarness(t)
	h.send(updateMsg{notice: app.UpdateNotice{Latest: "0.1.9", Method: app.UpdateViaBrew}, ok: true})
	golden.RequireEqual(t, []byte(h.view()))
}

func TestHomeGolden80x24En(t *testing.T) {
	withEnglish(t)
	h := homeGoldenHarness(t)
	golden.RequireEqual(t, []byte(h.view()))
}

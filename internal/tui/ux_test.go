package tui

// Tests of the first-time-user experience: onboarding, next steps after every
// action, the status badge, the contextual help bar, the responsive layout,
// the form hints and the apply summary. Every expectation is built from the
// same i18n keys the UI uses, so the tests follow the wording and the
// language.

import (
	"errors"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/plan"
)

// ---- helpers ----

// emptyCompanyCfg is one company without account.
func emptyCompanyCfg(slugs ...string) *config.Config {
	cfg := &config.Config{Version: 1}
	for _, s := range slugs {
		cfg.Companies = append(cfg.Companies, config.Company{Slug: s, Name: strings.ToUpper(s[:1]) + s[1:], BaseDir: "~/Code/" + s + "/"})
	}
	return cfg
}

func (h *harness) resize(w, hh int) {
	h.t.Helper()
	h.send(tea.WindowSizeMsg{Width: w, Height: hh})
}

func (h *harness) lines() []string { return strings.Split(h.view(), "\n") }

// helpLine is the help bar, statusLine the status bar.
func (h *harness) helpLine() string   { l := h.lines(); return strings.TrimSpace(l[len(l)-1]) }
func (h *harness) statusLine() string { l := h.lines(); return l[len(l)-2] }

// badge is the right part of the status bar as plain text.
func (h *harness) badge() string { return strings.TrimSpace(h.m.syncBadge()) }

// plain flattens box drawing and whitespace so that text wrapped by the UI
// can be compared with the translated sentence.
func plain(s string) string {
	for _, c := range []string{"│", "┃", "╭", "╮", "╰", "╯", "─"} {
		s = strings.ReplaceAll(s, c, " ")
	}
	return strings.Join(strings.Fields(s), " ")
}

func (h *harness) mustContainPlain(parts ...string) {
	h.t.Helper()
	v := plain(h.view())
	for _, p := range parts {
		if !strings.Contains(v, plain(p)) {
			h.t.Errorf("view does not contain %q:\n%s", p, h.view())
		}
	}
}

// fits fails when a line is wider than w or the view is taller than hh.
func assertFits(t *testing.T, view string, w, hh int, label string) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) > hh {
		t.Errorf("%s %dx%d: %d lines", label, w, hh, len(lines))
	}
	for i, l := range lines {
		if lw := lipgloss.Width(l); lw > w {
			t.Errorf("%s %dx%d line %d is %d cells wide: %q", label, w, hh, i, lw, l)
		}
	}
}

// listScreenOf returns the home screen (the bottom of the stack).
func (h *harness) listScreen() *listScreen { return h.m.stack[0].(*listScreen) }

// detail is the detail text of the selected row as the pane would show it.
func (h *harness) detail() string {
	l := h.listScreen()
	_, _, dw, dh := l.dims()
	if dw == 0 {
		dw, dh = l.w, l.h
	}
	return plain(l.detailBody(dw-4, dh-3))
}

func (h *harness) apply() {
	h.t.Helper()
	h.key("p")
	h.waitFor("diff ready", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confReady })
	h.key("y")
	h.waitFor("apply done", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confDone })
	h.waitFor("synced", func() bool { return h.m.sh.planned && h.m.sh.pending == 0 })
	h.key("enter")
}

func (h *harness) selectKey(k string) {
	h.t.Helper()
	h.key("home")
	for i := 0; i < 20 && h.listScreen().currentKey() != k; i++ {
		h.key("down")
	}
	if got := h.listScreen().currentKey(); got != k {
		h.t.Fatalf("cannot select %q (at %q)", k, got)
	}
}

// ---- A. onboarding ----

func TestA1WelcomeGuide(t *testing.T) {
	h := newHarness(t, nil)
	h.mustContain(tr("tui.empty.title"), tr("tui.empty.steps"), tr("tui.empty.step1"), tr("tui.empty.step2"),
		tr("tui.empty.step3"), tr("tui.empty.after"), tr("tui.empty.import"))
	h.mustNotContain(tr("tui.detail.title"))
	if b := h.badge(); !strings.Contains(b, tr("tui.status.no_company")) || strings.Contains(b, "✓") {
		t.Fatalf("badge %q", b)
	}
	// the three steps name their keys
	v := h.view()
	for _, k := range []string{"  1  A  ", "  2  a  ", "  3  p  "} {
		if !strings.Contains(v, k) {
			t.Errorf("welcome does not show %q:\n%s", k, v)
		}
	}
}

func TestA2WelcomeHelpBar(t *testing.T) {
	h := newHarness(t, nil)
	hb := h.helpLine()
	if !strings.HasPrefix(hb, "▸ A") || !strings.Contains(hb, " i ") || !strings.Contains(hb, "? "+tr("tui.key.all_keys")) || strings.Contains(hb, "…") {
		t.Fatalf("help bar %q", hb)
	}
}

func TestA4WelcomeFitsSmallSizes(t *testing.T) {
	h := newHarness(t, nil)
	for _, s := range [][2]int{{40, 10}, {59, 30}, {60, 24}, {84, 24}, {100, 30}, {200, 50}} {
		h.update(tea.WindowSizeMsg{Width: s[0], Height: s[1]})
		assertFits(t, h.view(), s[0], s[1], "welcome")
	}
	// at 59 columns the steps wrap instead of being cut
	h.resize(59, 30)
	h.mustContain("1  A", "2  a", "3  p")
	if !strings.Contains(plain(h.view()), plain(tr("tui.empty.step3"))) {
		t.Fatalf("step 3 cut:\n%s", h.view())
	}
}

func TestA5WelcomeEnglish(t *testing.T) {
	defer i18n.SetLang("vi")
	i18n.SetLang("en")
	h := newHarness(t, nil)
	h.mustContain("Welcome to gitid", "Get started in 3 steps:", "Press i to import")
	if !strings.HasPrefix(h.helpLine(), "▸ A add company") {
		t.Fatalf("help bar %q", h.helpLine())
	}
}

// ---- B. company without account ----

func TestB1RowCountLabel(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.mustContain("personal", "Personal", tr("tui.list.count_zero"))
	for _, l := range h.lines() {
		if strings.Contains(l, "personal") && strings.Contains(l, "▌") && !strings.Contains(l, tr("tui.list.count_zero")) {
			t.Fatalf("row without count label: %q", l)
		}
	}
}

func TestB2NoAccountDetail(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	d := h.detail()
	for _, want := range []string{tr("tui.list.no_accounts_hint", "personal"), tr("tui.list.no_accounts_why", "personal"), tr("tui.detail.how")} {
		if !strings.Contains(d, plain(want)) {
			t.Errorf("detail lacks %q:\n%s", want, d)
		}
	}
	h.mustContain(tr("tui.detail.how"))
}

func TestB3NoAccountBadge(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.waitFor("planned", func() bool { return h.m.sh.planned })
	b := h.badge()
	if !strings.Contains(b, tr("tui.status.empty_company", "personal")) || strings.Contains(b, "✓") || strings.Contains(b, tr("tui.status.synced")) {
		t.Fatalf("badge %q", b)
	}
	if !strings.HasPrefix(b, "!") {
		t.Fatalf("the warning needs its own marker: %q", b)
	}
	if !strings.Contains(h.statusLine(), tr("tui.status.empty_company", "personal")) {
		t.Fatalf("status line %q", h.statusLine())
	}
}

func TestB4NoAccountHelpBar(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	hb := h.helpLine()
	if !strings.HasPrefix(hb, "▸ a") || !strings.Contains(hb, tr("tui.key.edit_company")) {
		t.Fatalf("help bar %q", hb)
	}
}

func TestB5SeveralEmptyCompanies(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("one", "two"))
	if b := h.badge(); !strings.Contains(b, tr("tui.status.empty_companies", 2)) {
		t.Fatalf("badge %q", b)
	}
}

func TestB6AppliedIsSynced(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.apply()
	b := h.badge()
	if !strings.Contains(b, tr("tui.status.synced")) || !strings.HasPrefix(b, "✓") {
		t.Fatalf("badge %q", b)
	}
	if strings.Contains(b, tr("tui.status.empty_company", "acme")) {
		t.Fatal("no company is empty")
	}
}

func TestB7ListOnlyShowsHintUnderTheRow(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.resize(59, 30)
	if h.listScreen().layout() != layoutList {
		t.Fatal("expected list-only layout")
	}
	h.mustContain(tr("tui.list.no_accounts_hint", "personal"))
	// a short terminal has no room for it: the badge and the help bar remain
	h.resize(59, 10)
	h.mustNotContain(tr("tui.list.no_accounts_hint", "personal"))
	if !strings.Contains(h.statusLine(), "personal") || !strings.HasPrefix(h.helpLine(), "▸ a") {
		t.Fatalf("status %q help %q", h.statusLine(), h.helpLine())
	}
	assertFits(t, h.view(), 59, 10, "empty company")
}

func TestB8CountLabels(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.mustContain(tr("tui.list.count_many", 3), tr("tui.list.count_one"))
	defer i18n.SetLang("vi")
	i18n.SetLang("en")
	if tr("tui.list.count_one") == tr("tui.list.count_many", 1) {
		t.Fatal("English singular must have its own text")
	}
}

func TestB9NarrowRowKeepsTheCount(t *testing.T) {
	cfg := emptyCompanyCfg("personal")
	cfg.Companies[0].BaseDir = "~/Code/a-very-long-base-directory-name-for-the-personal-company/"
	h := newHarness(t, cfg)
	h.resize(59, 30)
	row := ""
	for _, l := range h.lines() {
		if strings.Contains(l, "▌") {
			row = l
		}
	}
	if !strings.Contains(row, tr("tui.list.count_zero")) || strings.Contains(row, "a-very-long") {
		t.Fatalf("row %q", row)
	}
	// wide: the base directory is shown
	h.resize(100, 30)
	h.mustContain("a-very-long")
}

// ---- C. filter ----

func TestC1FilterNoMatch(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("/")
	h.typeText("zzzz")
	h.waitFor("no match", func() bool { return strings.Contains(plain(h.view()), plain(tr("tui.list.no_match", "zzzz"))) })
	h.mustContainPlain(tr("tui.list.no_match_hint"))
	h.mustNotContain("globex")
}

func TestC2FilterHelpBar(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("/")
	hb := h.helpLine()
	if !strings.HasPrefix(hb, "▸ enter "+tr("tui.key.filter_keep")) || !strings.Contains(hb, "esc "+tr("tui.key.filter_clear")) {
		t.Fatalf("help bar %q", hb)
	}
}

func TestC4AppliedFilterHelpBar(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("/")
	h.typeText("carol")
	h.waitFor("filtered", func() bool { return !strings.Contains(h.view(), "bob@acme.com") })
	h.key("enter")
	hb := h.helpLine()
	if !strings.Contains(hb, "esc "+tr("tui.key.filter_clear")) || !strings.HasPrefix(hb, "▸ p") {
		t.Fatalf("help bar %q", hb)
	}
	// nothing from the other companies leaks into the new texts
	h.mustNotContain("globex")
}

// ---- D. next steps after save ----

func createCompany(h *harness, slug, name string) {
	h.key("A")
	h.typeText(slug)
	h.key("enter")
	h.typeText(name)
	h.key("enter")
	h.key("enter")
	h.waitFor("back on the list", func() bool { _, ok := h.top().(*listScreen); return ok && h.depth() == 1 && h.m.sh.loaded })
}

func TestD1FirstCompanyFlow(t *testing.T) {
	h := newHarness(t, nil)
	createCompany(h, "personal", "Cá nhân")
	if h.listScreen().currentKey() != "personal" {
		t.Fatalf("selected %q", h.listScreen().currentKey())
	}
	if !strings.Contains(h.m.status.text, tr("tui.status.company_created", "personal")) {
		t.Fatalf("status %q", h.m.status.text)
	}
	b := h.badge()
	if !strings.Contains(b, tr("tui.status.empty_company", "personal")) || strings.Contains(b, "✓") {
		t.Fatalf("badge %q", b)
	}
	for _, s := range h.m.stack {
		if _, ok := s.(*confirmScreen); ok {
			t.Fatal("no confirm screen for a company without accounts")
		}
	}
	h.mustContain(tr("tui.list.count_zero"), tr("tui.status.company_created", "personal"))
	if !strings.HasPrefix(h.helpLine(), "▸ a") {
		t.Fatalf("help bar %q", h.helpLine())
	}
	if !strings.Contains(h.detail(), plain(tr("tui.list.no_accounts_hint", "personal"))) {
		t.Fatalf("detail %q", h.detail())
	}
}

func addDora(h *harness) {
	h.key("a") // acme is selected
	h.key("enter")
	h.typeText("dora")
	h.key("enter")
	h.key("enter") // name prefilled
	h.typeText("dora@acme.com")
	h.key("enter")
	h.key("enter") // auth: https
	for i := 0; i < 8; i++ {
		h.key("enter")
		if _, ok := h.top().(*confirmScreen); ok {
			break
		}
	}
	h.waitFor("confirm ready", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confReady })
}

func TestD4D5AccountSavedThenLater(t *testing.T) {
	h := newHarness(t, basicCfg())
	addDora(h)
	if !strings.Contains(h.m.status.text, tr("tui.status.account_saved", "acme/dora")) {
		t.Fatalf("status %q", h.m.status.text)
	}
	h.mustContain(tr("tui.confirm.question"))
	if !strings.HasPrefix(h.helpLine(), "▸ y") {
		t.Fatalf("help bar %q", h.helpLine())
	}
	// the diff is computed from the saved configuration (dora has a file)
	h.mustContain("acme-dora")
	h.key("n")
	if h.depth() != 1 {
		t.Fatalf("depth %d", h.depth())
	}
	if h.m.status.text != tr("tui.status.apply_later") {
		t.Fatalf("status %q", h.m.status.text)
	}
	h.waitFor("pending", func() bool { return h.m.sh.pending > 0 })
	if !strings.HasPrefix(h.badge(), "●") || !strings.HasPrefix(h.helpLine(), "▸ p") {
		t.Fatalf("badge %q help %q", h.badge(), h.helpLine())
	}
}

func TestD6HintLastsTenSeconds(t *testing.T) {
	msg, ok := hint("x")().(statusMsg)
	if !ok || msg.ttl != 10*time.Second || msg.level != levelInfo {
		t.Fatalf("%+v", msg)
	}
	if got := say(levelOK, "x")().(statusMsg); got.ttl != 0 {
		t.Fatal("plain status messages keep the default expiry")
	}
	h := newHarness(t, basicCfg())
	h.send(msg)
	h.mustContain("x")
	h.send(statusExpireMsg{seq: h.m.statSeq - 1}) // stale
	h.mustContain("x")
	h.send(statusExpireMsg{seq: h.m.statSeq})
	if h.m.status.text != "" {
		t.Fatalf("status %q", h.m.status.text)
	}
	// the tick of a hint is longer than the default expiry
	cmd := h.m.setStatus(msg)
	if cmd == nil {
		t.Fatal("a hint must expire")
	}
}

func TestD7FirstAccountOfEmptyCompany(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.key("a")
	h.key("enter")
	h.typeText("dora")
	h.key("enter")
	h.typeText("Dora D")
	h.key("enter")
	h.typeText("dora@example.com")
	h.key("enter")
	h.key("enter")
	for i := 0; i < 8; i++ {
		h.key("enter")
		if _, ok := h.top().(*confirmScreen); ok {
			break
		}
	}
	h.waitFor("confirm ready", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confReady })
	c, _, _ := h.m.sh.cfg.FindCompany("personal")
	if c.DefaultAccount != "dora" || len(c.Accounts) != 1 {
		t.Fatalf("company %+v", c)
	}
	h.mustContain("personal.gitconfig", "personal-dora.gitconfig")
	if h.top().(*confirmScreen).plan.Empty() {
		t.Fatal("the plan must not be empty any more")
	}
}

func TestD8SSHAccountWithKeyGoesToKeygen(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("a")
	h.key("enter")
	h.typeText("erin")
	h.key("enter")
	h.key("enter")
	h.typeText("erin@acme.com")
	h.key("enter")
	h.send(tea.KeyMsg{Type: tea.KeyDown}) // auth: ssh
	h.key("enter")
	h.key("enter") // advanced settings: no (the host, orgs and ssh fields stay hidden)
	h.key("enter") // make default (acme already has accounts)
	h.mustContain(tr("tui.form.gen_key"))
	h.key("y") // generate the key right after saving
	h.waitFor("keygen", func() bool { _, ok := h.top().(*keygenScreen); return ok })
}

// ---- E. status badge ----

func TestE1E2PendingBadge(t *testing.T) {
	h := newHarness(t, basicCfg())
	n := h.m.sh.pending
	if n == 0 {
		t.Fatal("fixture must have pending changes")
	}
	if b := h.badge(); b != "● "+tr("tui.status.pending", n) {
		t.Fatalf("badge %q", b)
	}
	h.resize(69, 30)
	if b := h.badge(); b != "● "+tr("tui.status.pending_short", n) {
		t.Fatalf("badge %q", b)
	}
	h.mustContain(tr("tui.status.pending_short", n))
}

func TestE4PlanFailed(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.send(planBuiltMsg{seq: h.m.planSeq, err: errors.New("x")})
	b := h.badge()
	if !strings.Contains(b, tr("tui.status.plan_failed")) || strings.Contains(b, "✓") {
		t.Fatalf("badge %q", b)
	}
	h.mustContain(tr("tui.status.plan_failed"))
	// the next good plan clears it
	h.send(refreshPlanMsg{})
	h.waitFor("recovered", func() bool { return !h.m.sh.planErr })
}

func TestE5PendingWinsOverEmptyCompany(t *testing.T) {
	cfg := basicCfg()
	cfg.Companies = append(cfg.Companies, config.Company{Slug: "zeta", Name: "Zeta", BaseDir: "~/Code/zeta/"})
	h := newHarness(t, cfg)
	if b := h.badge(); !strings.Contains(b, tr("tui.status.pending", h.m.sh.pending)) {
		t.Fatalf("badge %q", b)
	}
	h.apply()
	if b := h.badge(); !strings.Contains(b, tr("tui.status.empty_company", "zeta")) {
		t.Fatalf("after apply: %q", b)
	}
}

func TestE6NoBadgeBeforeTheFirstPlan(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.m.sh.planned = false
	if b := h.badge(); b != "" {
		t.Fatalf("badge %q", b)
	}
	h.m.sh.loaded = false
	if b := h.badge(); b != "" {
		t.Fatalf("badge %q", b)
	}
}

func TestE8LongToastDropsTheBadgeInsteadOfCuttingIt(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.resize(80, 24)
	h.send(hintMsg(tr("tui.status.account_saved", "acme/dora")))
	st := h.statusLine()
	if !strings.Contains(st, tr("tui.status.account_saved", "acme/dora")) {
		t.Fatalf("toast cut: %q", st)
	}
	if lipgloss.Width(st) > 80 {
		t.Fatalf("status line too wide: %q", st)
	}
}

// ---- F. help bar ----

func TestF1PendingHelpBar(t *testing.T) {
	h := newHarness(t, basicCfg())
	hb := h.helpLine()
	if !strings.HasPrefix(hb, "▸ p "+tr("tui.key.apply")) || strings.Contains(hb, "ctrl+c") || strings.Contains(hb, "…") {
		t.Fatalf("help bar %q", hb)
	}
	if n := len(strings.Split(hb, "  ")); n > 7 {
		t.Fatalf("%d items: %q", n, hb)
	}
}

func TestF2F3HelpBarWhenApplied(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.apply()
	hb := h.helpLine()
	if strings.Contains(hb, "▸") || !strings.Contains(hb, "a "+tr("tui.key.add_account")) || !strings.Contains(hb, "e "+tr("tui.key.edit")) || !strings.Contains(hb, "d "+tr("tui.key.delete")) {
		t.Fatalf("company row: %q", hb)
	}
	h.selectKey("acme/carol")
	if hb := h.helpLine(); !strings.HasPrefix(hb, "▸ g "+tr("tui.key.genkey")) {
		t.Fatalf("ssh account without key: %q", hb)
	}
	h.selectKey("acme/bob")
	if hb := h.helpLine(); strings.Contains(hb, "▸") || strings.Contains(hb, "g ") {
		t.Fatalf("https account: %q", hb)
	}
}

func TestF4HelpBarFitsEveryWidth(t *testing.T) {
	type state struct {
		name    string
		setup   func(t *testing.T) *harness
		primary string
	}
	states := []state{
		{"welcome", func(t *testing.T) *harness { return newHarness(t, nil) }, "▸ A"},
		{"empty company", func(t *testing.T) *harness { return newHarness(t, emptyCompanyCfg("personal")) }, "▸ a"},
		{"pending", func(t *testing.T) *harness { return newHarness(t, basicCfg()) }, "▸ p"},
		{"filter", func(t *testing.T) *harness { h := newHarness(t, basicCfg()); h.key("/"); return h }, "▸ enter"},
		{"applied ssh", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.apply()
			h.selectKey("acme/carol")
			return h
		}, "▸ g"},
		{"form", func(t *testing.T) *harness { h := newHarness(t, basicCfg()); h.key("a"); return h }, "▸ enter"},
		{"confirm", func(t *testing.T) *harness {
			h := newHarness(t, basicCfg())
			h.key("p")
			h.waitFor("ready", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confReady })
			return h
		}, "▸ y"},
		{"doctor", func(t *testing.T) *harness { h := newHarness(t, basicCfg()); h.key("D"); return h }, ""},
	}
	for _, s := range states {
		t.Run(s.name, func(t *testing.T) {
			h := s.setup(t)
			for _, w := range []int{40, 60, 80, 100, 120, 200} {
				h.update(tea.WindowSizeMsg{Width: w, Height: 30})
				hb := h.helpLine()
				if strings.Contains(hb, "…") {
					t.Errorf("w=%d: ellipsis in %q", w, hb)
				}
				if lipgloss.Width(" "+hb) > w {
					t.Errorf("w=%d: help bar is %d wide: %q", w, lipgloss.Width(hb), hb)
				}
				if !strings.Contains(hb, "? "+tr("tui.key.all_keys")) {
					t.Errorf("w=%d: no \"all keys\" in %q", w, hb)
				}
				if s.primary != "" && !strings.HasPrefix(hb, s.primary) {
					t.Errorf("w=%d: primary %q missing in %q", w, s.primary, hb)
				}
			}
		})
	}
}

func TestF5ListOnlyHelpBarOffersDetails(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.resize(59, 30)
	if hb := h.helpLine(); !strings.Contains(hb, "enter "+tr("tui.key.detail")) {
		t.Fatalf("help bar %q", hb)
	}
	h.resize(83, 23)
	if hb := h.helpLine(); !strings.Contains(hb, "enter "+tr("tui.key.detail")) {
		t.Fatalf("help bar %q", hb)
	}
	h.resize(100, 30)
	if hb := h.helpLine(); strings.Contains(hb, "enter") {
		t.Fatalf("with a detail pane enter is not advertised: %q", hb)
	}
}

func TestF6HelpOverlayListsEverything(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("?")
	v := h.view()
	for _, b := range h.listScreen().Help() {
		hh := b.Help()
		if !strings.Contains(v, hh.Key) || !strings.Contains(v, hh.Desc) {
			t.Errorf("overlay lacks %q %q", hh.Key, hh.Desc)
		}
	}
	h.mustContain(tr("tui.key.clone"), tr("tui.key.doctor"), tr("tui.key.import"), tr("tui.key.genkey"), tr("tui.key.quit"), "ctrl+c")
}

func TestF7ConfirmHelpBar(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("p")
	h.waitFor("ready", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confReady })
	if !strings.HasPrefix(h.helpLine(), "▸ y") {
		t.Fatalf("ready: %q", h.helpLine())
	}
	h.key("y")
	h.waitFor("done", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confDone })
	// the next step is to clone; check and the way back follow
	if hb := h.helpLine(); !strings.HasPrefix(hb, "▸ c "+tr("tui.key.clone")) ||
		!strings.Contains(hb, "k "+tr("tui.key.check")) || !strings.Contains(hb, "enter "+tr("tui.key.to_list")) ||
		strings.Contains(hb, "enter "+tr("tui.key.select")) {
		t.Fatalf("done: %q", hb)
	}
}

func TestF8FormHelpBar(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("A")
	if !strings.HasPrefix(h.helpLine(), "▸ enter") {
		t.Fatalf("help bar %q", h.helpLine())
	}
}

// ---- G. layout ----

func TestG1DetailExplainsTheCompany(t *testing.T) {
	h := newHarness(t, basicCfg())
	d := h.detail()
	for _, want := range []string{
		tr("tui.detail.how"),
		tr("tui.detail.how_default", "~/Code/acme/", "Alice Nguyen", "alice@acme.com"),
		tr("tui.detail.how_others"),
		tr("tui.detail.how_pending"),
	} {
		if !strings.Contains(d, plain(want)) {
			t.Errorf("detail lacks %q:\n%s", want, d)
		}
	}
	h.mustContain(tr("tui.detail.title"))
}

func TestG2G3G4Breakpoints(t *testing.T) {
	titleRow := func(h *harness, s string) int {
		for i, l := range h.lines() {
			if strings.Contains(l, s) {
				return i
			}
		}
		return -1
	}
	for _, s := range [][2]int{{96, 30}, {84, 24}, {84, 40}} {
		h := newHarness(t, basicCfg())
		h.resize(s[0], s[1])
		lt, dt := titleRow(h, tr("tui.list.panel", 3, 5)), titleRow(h, tr("tui.detail.title"))
		if lt < 0 || lt != dt {
			t.Errorf("%v: list title on line %d, detail title on line %d", s, lt, dt)
		}
		assertFits(t, h.view(), s[0], s[1], "side")
	}
	for _, s := range [][2]int{{83, 24}, {60, 24}, {70, 40}} {
		h := newHarness(t, basicCfg())
		h.resize(s[0], s[1])
		lt, dt := titleRow(h, tr("tui.list.panel", 3, 5)), titleRow(h, tr("tui.detail.title"))
		if lt < 0 || dt <= lt+3 {
			t.Errorf("%v: list title on line %d, detail title on line %d (want stacked)", s, lt, dt)
		}
		assertFits(t, h.view(), s[0], s[1], "stacked")
		// the explanation is visible below the list
		if !strings.Contains(plain(h.view()), plain(tr("tui.detail.how_default", "~/Code/acme/", "Alice Nguyen", "alice@acme.com"))) {
			t.Errorf("%v: no \"what happens\" in the stacked pane:\n%s", s, h.view())
		}
	}
	for _, s := range [][2]int{{83, 23}, {59, 30}, {40, 10}} {
		h := newHarness(t, basicCfg())
		h.resize(s[0], s[1])
		if strings.Contains(h.view(), tr("tui.detail.title")) {
			t.Errorf("%v: list only expected:\n%s", s, h.view())
		}
		assertFits(t, h.view(), s[0], s[1], "list only")
	}
}

func TestG5AccountDetail(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.selectKey("acme/alice")
	if d := h.detail(); !strings.Contains(d, plain(tr("tui.detail.acc_default", "~/Code/acme/"))) {
		t.Fatalf("default account: %s", d)
	}
	h.selectKey("acme/bob")
	d := h.detail()
	if !strings.Contains(d, plain(tr("tui.detail.acc_other", "https://bob@github.com/…", "~/Code/acme/", "alice"))) {
		t.Fatalf("other account: %s", d)
	}
	// an ssh account is selected by its alias
	h.selectKey("acme/carol")
	if d := strings.ReplaceAll(h.detail(), "- ", "-"); !strings.Contains(d, "git@github-acme-carol:…") || !strings.Contains(d, tr("tui.key.state_missing")) {
		t.Fatalf("ssh account: %s", d)
	}
}

func TestG6DetailFollowsApplyState(t *testing.T) {
	h := newHarness(t, basicCfg())
	if d := h.detail(); !strings.Contains(d, plain(tr("tui.detail.how_pending"))) || strings.Contains(d, plain(tr("tui.detail.how_applied"))) {
		t.Fatalf("pending: %s", d)
	}
	h.apply()
	if d := h.detail(); !strings.Contains(d, plain(tr("tui.detail.how_applied"))) || strings.Contains(d, plain(tr("tui.detail.how_pending"))) {
		t.Fatalf("applied: %s", d)
	}
}

func TestG8LayoutMatrixFits(t *testing.T) {
	cfgs := map[string]*config.Config{"basic": basicCfg(), "welcome": nil, "empty company": emptyCompanyCfg("personal")}
	for name, cfg := range cfgs {
		h := newHarness(t, cfg)
		for _, w := range []int{59, 60, 83, 84, 100} {
			for _, hh := range []int{10, 23, 24, 30} {
				h.update(tea.WindowSizeMsg{Width: w, Height: hh})
				assertFits(t, h.view(), w, hh, name)
			}
		}
	}
}

// ---- H. forms ----

func TestH1RequiredSlug(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("A")
	h.key("enter")
	h.mustContain(tr("tui.form.err.required", tr("tui.f.slug")))
	h.mustNotContain(tr(config.ErrSlugInvalid))
}

func TestH4H6CompanyFormHints(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("A")
	h.mustContainPlain(tr("tui.form.eg", "acme"), tr("tui.form.slug_hint"), tr("tui.form.base_dir_hint"),
		tr("tui.form.base_dir_default", "~/Code/<slug>/"))
	h.mustContain(tr("tui.form.eg", "ACME Corp"))
}

// H5/H10: the base directory hint follows the slug while it is typed. The
// description function runs in another goroutine, so this test is also the
// race check for the atomic snapshot (run with -race).
func TestH5BaseDirHintFollowsTheSlug(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("A")
	h.typeText("zeta")
	h.waitFor("hint follows", func() bool {
		return strings.Contains(plain(h.view()), plain(tr("tui.form.base_dir_default", "~/Code/zeta/")))
	})
	for range "zeta" {
		h.update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	h.drain()
	h.typeText("ab")
	h.waitFor("hint follows again", func() bool {
		return strings.Contains(plain(h.view()), plain(tr("tui.form.base_dir_default", "~/Code/ab/")))
	})
	// a custom code root is honored
	cfg := basicCfg()
	cfg.Settings.CodeRoot = "~/Work"
	h2 := newHarness(t, cfg)
	h2.key("A")
	h2.typeText("q")
	h2.waitFor("code root", func() bool {
		return strings.Contains(plain(h2.view()), plain(tr("tui.form.base_dir_default", "~/Work/q/")))
	})
}

func TestH7AccountFormHints(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("a")
	h.resize(100, 50)
	h.mustContainPlain(tr("tui.form.platform_hint"), tr("tui.form.email_hint"), tr("tui.form.auth_hint"),
		"HTTPS ("+tr("tui.form.auth_https")+")", tr("tui.form.eg", "alice@acme.com"), tr("tui.form.eg", "alice"))
}

func TestH8RequiredEmail(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("a")
	h.key("enter") // platform
	h.typeText("dora")
	h.key("enter")
	h.key("enter") // git name is prefilled
	h.key("enter") // empty email
	h.mustContain(tr("tui.form.err.required", tr("tui.f.email")))
}

func TestH11EditCompanyForm(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("e")
	h.mustContain(tr("tui.form.immutable"), "ACME Corp")
	h.mustContainPlain(tr("tui.form.base_dir_default", "~/Code/acme/"))
	h.mustNotContain(tr("tui.form.slug_hint"))
}

func TestH12PlaceholdersAreMarked(t *testing.T) {
	// the "e.g." prefix is what tells an example from a typed value when the
	// terminal shows no colors
	for _, lang := range []string{"vi", "en"} {
		if got := i18n.In(lang, "tui.form.eg", "acme"); !strings.Contains(got, "acme") || got == "acme" {
			t.Errorf("%s: %q", lang, got)
		}
	}
}

// ---- I. confirm and apply ----

func TestI1ConfirmSummary(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("p")
	h.waitFor("ready", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confReady })
	c := h.top().(*confirmScreen)
	gen := 0
	for _, ch := range c.plan.Changes {
		if ch.Kind == plan.KindGenerated {
			gen++
		}
	}
	h.mustContain(tr("tui.confirm.summary"), tr("tui.confirm.sum_gitconfig", "~/.gitconfig"),
		tr("tui.confirm.sum_generated", gen, "~/.config/gitid/git"), tr("tui.confirm.sum_backup", "~/.config/gitid/backups"),
		tr("tui.confirm.files", len(c.plan.Changes)), tr("tui.confirm.question"))
	if c.vp.Height < 3 {
		t.Fatalf("the diff needs room: %d lines", c.vp.Height)
	}
	// the diff still scrolls
	h.key("down")
	if c.vp.YOffset == 0 {
		t.Fatal("the diff must scroll")
	}
}

func TestI2ConfirmFitsSmallSizes(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("p")
	h.waitFor("ready", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confReady })
	c := h.top().(*confirmScreen)
	for _, s := range [][2]int{{60, 20}, {40, 10}, {80, 24}, {100, 30}, {200, 60}} {
		h.update(tea.WindowSizeMsg{Width: s[0], Height: s[1]})
		assertFits(t, h.view(), s[0], s[1], "confirm")
		if c.vp.Height < 1 {
			t.Errorf("%v: viewport %d", s, c.vp.Height)
		}
		h.mustContain(tr("tui.confirm.sum_backup", "~/.config/gitid/backups")[:12])
	}
}

func TestI3DoneTellsWhatChanged(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("p")
	h.waitFor("ready", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confReady })
	h.key("y")
	h.waitFor("done", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confDone })
	h.mustContain(tr("tui.confirm.done"), tr("tui.confirm.backup"), tr("tui.confirm.effect"),
		tr("tui.confirm.effect_line", "~/Code/acme/", "Alice Nguyen", "alice@acme.com"),
		tr("tui.confirm.effect_line", "~/Code/globex/", "Dave D", "dave@globex.example"),
		tr("tui.confirm.effect_line", "~/Code/beta/", "Alice B", "alice@beta.io"),
		"gitid check")
	h.mustContainPlain(tr("tui.confirm.override_note"))
	if !strings.Contains(h.m.status.text, tr("tui.status.applied")) {
		t.Fatalf("status %q", h.m.status.text)
	}
	// on a short terminal the files give way, not the explanation
	h.resize(80, 24)
	h.mustContain(tr("tui.confirm.effect"), tr("tui.confirm.backup"), tr("tui.confirm.back_hint"), "gitid check")
	h.resize(60, 20)
	h.mustContain(tr("tui.confirm.effect"), tr("tui.confirm.back_hint"))
	for _, s := range [][2]int{{40, 10}, {60, 20}, {80, 24}, {100, 30}} {
		h.update(tea.WindowSizeMsg{Width: s[0], Height: s[1]})
		assertFits(t, h.view(), s[0], s[1], "done")
	}
}

func TestI4DoneOpensTheCheck(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.fake.On("git -C "+h.e.Home+" rev-parse --show-toplevel", execx.Response{ExitCode: 128})
	h.key("p")
	h.waitFor("ready", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confReady })
	h.key("y")
	h.waitFor("done", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confDone })
	h.key("k")
	if _, ok := h.top().(*checkScreen); !ok {
		t.Fatalf("top is %T", h.top())
	}
	h.waitFor("check ran", func() bool { return h.top().(*checkScreen).phase != chLoading })
}

func TestI5EmptyBecauseNoAccount(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.key("p")
	h.waitFor("empty", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confEmpty })
	h.mustContainPlain(tr("tui.confirm.empty_no_accounts", "personal"))
	h.mustNotContain("✓ " + tr("tui.confirm.empty"))
}

func TestI6EmptyBecauseApplied(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.apply()
	h.key("p")
	h.waitFor("empty", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confEmpty })
	h.mustContain("✓ " + tr("tui.confirm.empty"))
	h.mustNotContain(tr("tui.confirm.empty_no_accounts", "acme"))
}

func TestI9DeletesAreSummarized(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.apply()
	h.key("d") // acme
	h.key("tab")
	h.key("enter")
	h.waitFor("ready", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confReady })
	c := h.top().(*confirmScreen)
	del := 0
	for _, ch := range c.plan.Changes {
		if string(ch.Op) == "delete" {
			del++
		}
	}
	if del == 0 {
		t.Fatal("deleting a company must delete its generated files")
	}
	h.mustContain(tr("tui.confirm.sum_delete", del))
}

// ---- J. architecture ----

// The TUI talks to the rest of gitid only through internal/app (plus value
// types): no direct use of the git, ssh or file-system layers.
func TestJ3TUIDoesNotImportLowLevelPackages(t *testing.T) {
	banned := []string{"internal/gitcfg", "internal/fsx", "internal/sshcfg", "internal/execx"}
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range af.Imports {
			for _, b := range banned {
				if strings.Contains(imp.Path.Value, b) {
					t.Errorf("%s imports %s", f, imp.Path.Value)
				}
			}
		}
	}
}

func TestEnglishFlowWords(t *testing.T) {
	defer i18n.SetLang("vi")
	i18n.SetLang("en")
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.mustContain("no accounts yet", "! personal has no account — press a", "▸ a add account")
	h.mustNotContain("chưa", "tài khoản")
}

// ---- QA cycle 1 fixes ----

// D-2: the next-step guidance survives short stacked panes.
func TestD2StackedPaneKeepsTheNextStep(t *testing.T) {
	for _, w := range []int{60, 70, 83} {
		h := newHarness(t, basicCfg())
		h.resize(w, 24)
		if h.listScreen().layout() != layoutStacked {
			t.Fatalf("%d: not stacked", w)
		}
		h.mustContainPlain(tr("tui.detail.how_pending"))
		h.mustContainPlain(tr("tui.detail.how_default", "~/Code/acme/", "Alice Nguyen", "alice@acme.com"))
		assertFits(t, h.view(), w, 24, "stacked")
	}
	// applied: the verify sentence stays
	h := newHarness(t, basicCfg())
	h.apply()
	for _, w := range []int{60, 70} {
		h.resize(w, 24)
		h.mustContainPlain(tr("tui.detail.how_applied"))
	}
	// a company without account keeps its "press a" hint
	h2 := newHarness(t, emptyCompanyCfg("personal"))
	for _, w := range []int{60, 70} {
		h2.resize(w, 24)
		h2.mustContainPlain(tr("tui.list.no_accounts_hint", "personal"))
	}
	// even a pane of 2 lines shows the next step
	for _, n := range []int{2, 3, 4, 5} {
		d := plain(detailText(h.m.sh, rowItem{company: h.m.sh.cfg.Companies[0]}, 56, n, h.listScreen().keyInfo, false))
		if !strings.Contains(d, plain(tr("tui.detail.how_applied"))[:20]) {
			t.Errorf("height %d lost the next step: %q", n, d)
		}
	}
}

// D-3: no label of the detail pane is cut, in either language.
func TestD3DetailLabelsAreNotCut(t *testing.T) {
	defer i18n.SetLang("vi")
	for _, lang := range []string{"en", "vi"} {
		i18n.SetLang(lang)
		h := newHarness(t, basicCfg())
		for _, k := range []string{"acme", "acme/carol"} {
			h.selectKey(k)
			d := h.listScreen().detailBody(41, 26)
			// the remote examples legitimately end in "…"; a label never may
			if strings.Contains(strings.NewReplacer(":…", "", "/…", "", "@…", "").Replace(d), "…") {
				t.Errorf("%s %s: cut text in detail:\n%s", lang, k, d)
			}
		}
		h.selectKey("acme")
		if d := h.detail(); !strings.Contains(d, tr("tui.f.base_dir")+" ~/Code/acme/") {
			t.Errorf("%s: base dir label not intact: %s", lang, d)
		}
	}
}

// D-4 and copy fixes.
func TestVietnameseCopyFixes(t *testing.T) {
	if got := i18n.In("vi", "tui.confirm.question"); got != "Bấm y để áp dụng, n để làm sau." {
		t.Fatal(got)
	}
	if got := i18n.In("vi", "tui.form.auth_hint"); !strings.Contains(got, "gitid có thể tạo giúp") || strings.Contains(got, "tạo giúp được") {
		t.Fatal(got)
	}
}

// D-7: the base directory is shown on every company row or on none.
func TestD7BaseDirColumnIsConsistent(t *testing.T) {
	h := newHarness(t, basicCfg())
	for w := 60; w <= 110; w++ {
		h.update(tea.WindowSizeMsg{Width: w, Height: 40})
		withBase, companies := 0, 0
		for _, l := range h.lines() {
			if strings.Contains(l, "▪ ") || strings.Contains(l, "▌ ") {
				companies++
				if strings.Contains(l, "~/Code/") {
					withBase++
				}
			}
		}
		if companies != 3 || (withBase != 0 && withBase != companies) {
			t.Fatalf("w=%d: %d of %d company rows show the base dir:\n%s", w, withBase, companies, h.view())
		}
		for _, l := range h.lines() {
			if (strings.Contains(l, "▪ ") || strings.Contains(l, "▌ ")) && !strings.Contains(l, "account") && !strings.Contains(l, "tài khoản") {
				t.Fatalf("w=%d: row lost its count: %q", w, l)
			}
		}
	}
}

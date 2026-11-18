package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/tinhnd1902/gitid/internal/i18n"
)

func hasDetail(h *harness) bool {
	for _, s := range h.m.stack {
		if _, ok := s.(*detailScreen); ok {
			return true
		}
	}
	return false
}

func accountFormTop(t *testing.T, h *harness) *accountForm {
	t.Helper()
	f, ok := h.top().(*accountForm)
	if !ok {
		t.Fatalf("top is %T, want *accountForm", h.top())
	}
	return f
}

// ---- key race (R4) ----

func TestRACE1AThenEnterWithoutDrain(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.resize(72, 24)
	h.update(keyMsg("a"))
	h.update(keyMsg("enter"))
	h.drain()
	if h.depth() != 2 {
		t.Fatalf("depth %d, want 2", h.depth())
	}
	if f := accountFormTop(t, h); f.companySlug != "personal" {
		t.Fatalf("slug %q", f.companySlug)
	}
	if hasDetail(h) {
		t.Fatal("a detail screen is in the stack")
	}
	if strings.HasSuffix(strings.TrimSpace(plain(h.lines()[0])), "› personal") {
		t.Fatalf("breadcrumb %q", h.lines()[0])
	}
}

func TestRACE2TypedTextDoesNotReachTheList(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.update(keyMsg("a"))
	h.typeText("dora") // "d" would open the delete screen if the list got it
	accountFormTop(t, h)
	if h.depth() != 2 {
		t.Fatalf("depth %d", h.depth())
	}
}

func TestRACE3DetailThenA(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.selectKey("acme")
	h.update(keyMsg("enter"))
	h.update(keyMsg("a"))
	h.drain()
	if h.depth() != 2 {
		t.Fatalf("depth %d", h.depth())
	}
	f := accountFormTop(t, h)
	if f.companySlug != "acme" || f.edit != nil {
		t.Fatalf("form %+v", f)
	}
}

func TestRACE4EscThenA(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("a")
	first := accountFormTop(t, h)
	h.update(keyMsg("esc"))
	h.update(keyMsg("a"))
	h.drain()
	if h.depth() != 2 {
		t.Fatalf("depth %d", h.depth())
	}
	if second := accountFormTop(t, h); second == first {
		t.Fatal("the old form is still on top")
	}
}

func TestRACE5NavigationIsSynchronous(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.m.Update(keyMsg("a"))
	if len(h.m.stack) != 2 {
		t.Fatalf("stack %d, want 2 right after Update", len(h.m.stack))
	}
}

// ---- detail screen keys ----

func openDetail(h *harness, key string) {
	h.t.Helper()
	h.selectKey(key)
	h.key("enter")
	if _, ok := h.top().(*detailScreen); !ok {
		h.t.Fatalf("top is %T", h.top())
	}
}

func TestDET1AddAccountFromEmptyCompany(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.resize(72, 24)
	h.key("enter")
	h.key("a")
	if h.depth() != 2 {
		t.Fatalf("depth %d", h.depth())
	}
	accountFormTop(t, h)
	h.mustContain(tr("tui.form.account_new", "personal"))
}

func TestDET2AccountRowAddsToItsCompany(t *testing.T) {
	h := newHarness(t, basicCfg())
	openDetail(h, "acme/bob")
	h.key("a")
	f := accountFormTop(t, h)
	if f.edit != nil || f.companySlug != "acme" {
		t.Fatalf("form %+v", f)
	}
	h.mustContain(tr("tui.form.account_new", "acme"))
	h.mustNotContain(tr("tui.form.account_edit", "acme/bob"))
}

func TestDET3EscReturnsToTheList(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.key("enter")
	h.key("a")
	h.key("esc")
	if h.depth() != 1 {
		t.Fatalf("depth %d", h.depth())
	}
	if _, ok := h.top().(*listScreen); !ok {
		t.Fatalf("top %T", h.top())
	}
}

func TestDET4AddFromDetailThenLater(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.key("enter")
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
	h.key("n")
	if h.depth() != 1 {
		t.Fatalf("depth %d", h.depth())
	}
	if got := h.listScreen().currentKey(); got != "personal/dora" {
		t.Fatalf("current %q", got)
	}
	c, _, _ := h.m.sh.cfg.FindCompany("personal")
	if len(c.Accounts) != 1 {
		t.Fatalf("accounts %d", len(c.Accounts))
	}
}

func TestDET5ApplyFromDetail(t *testing.T) {
	h := newHarness(t, basicCfg())
	openDetail(h, "acme")
	h.key("p")
	if _, ok := h.top().(*confirmScreen); !ok || h.depth() != 3 {
		t.Fatalf("top %T depth %d", h.top(), h.depth())
	}
	h.waitFor("confirm ready", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confReady })
	h.key("n")
	if _, ok := h.top().(*detailScreen); !ok || h.depth() != 2 {
		t.Fatalf("top %T depth %d", h.top(), h.depth())
	}
}

func TestDET6CheckFromDetail(t *testing.T) {
	h := newHarness(t, basicCfg())
	openDetail(h, "acme")
	h.key("k")
	if _, ok := h.top().(*checkScreen); !ok {
		t.Fatalf("top %T", h.top())
	}
	h.key("esc")
	if _, ok := h.top().(*detailScreen); !ok {
		t.Fatalf("top %T", h.top())
	}
}

func TestDET7DoctorFromDetail(t *testing.T) {
	h := newHarness(t, basicCfg())
	openDetail(h, "acme")
	h.key("D")
	if _, ok := h.top().(*doctorScreen); !ok {
		t.Fatalf("top %T", h.top())
	}
}

func TestDET9EmptyCompanyHelpBar(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.resize(72, 24)
	h.key("enter")
	if !strings.HasPrefix(h.helpLine(), "▸ a") {
		t.Fatalf("help %q", h.helpLine())
	}
	for i, row := range h.helpRows() {
		if w := lipgloss.Width(row); w > 72 {
			t.Fatalf("help row %d is %d wide", i, w)
		}
	}
	assertFits(t, h.view(), 72, 24, "detail of empty company")
}

func TestDET10PendingHelpBar(t *testing.T) {
	h := newHarness(t, basicCfg())
	openDetail(h, "acme")
	if !strings.HasPrefix(h.helpLine(), "▸ p") {
		t.Fatalf("help %q", h.helpLine())
	}
}

func TestDET11KeyMissingHelpBar(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.apply()
	openDetail(h, "acme/carol")
	if !strings.HasPrefix(h.helpLine(), "▸ g") {
		t.Fatalf("help %q", h.helpLine())
	}
	h.key("esc")
	openDetail(h, "acme/bob")
	for _, it := range h.m.top().(*detailScreen).ContextHelp() {
		if it.b.Help().Key == "g" {
			t.Fatal("g offered for an https account")
		}
	}
}

func TestDET12HelpOverlay(t *testing.T) {
	h := newHarness(t, basicCfg())
	openDetail(h, "acme")
	h.key("?")
	got := map[string]bool{}
	for _, b := range h.m.top().Help() {
		got[b.Help().Key] = true
	}
	for _, k := range []string{"a", "p", "k", "D", "e", "d", "g", "esc"} {
		if !got[k] {
			t.Fatalf("help misses %q", k)
		}
	}
	h.mustContain(tr("tui.key.add_account"))
}

func TestDET13DetailSizes(t *testing.T) {
	for _, sz := range [][2]int{{40, 10}, {59, 30}} {
		h := newHarness(t, basicCfg())
		h.resize(sz[0], sz[1])
		h.key("enter")
		assertFits(t, h.view(), sz[0], sz[1], "detail")
		h.key("k")
		if _, ok := h.top().(*checkScreen); !ok {
			t.Fatalf("%v: top %T", sz, h.top())
		}
	}
}

// ---- badge (R5) ----

func TestBAD2FormBadgeHasNoKey(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.key("a")
	b := h.badge()
	if !strings.Contains(b, tr("tui.status.empty_company_plain", "personal")) || strings.Contains(b, "bấm a") {
		t.Fatalf("badge %q", b)
	}
}

func TestBAD3DetailBadgeKeepsKey(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.key("enter")
	if !strings.Contains(h.badge(), "bấm a") {
		t.Fatalf("badge %q", h.badge())
	}
}

func TestBAD4ConfirmBadgeHasNoKey(t *testing.T) {
	for _, w := range []int{100, 60} {
		h := newHarness(t, basicCfg())
		h.resize(w, 30)
		h.key("p")
		b := h.badge()
		if b == "" || strings.Contains(b, "bấm p") || strings.Contains(b, "· p") {
			t.Fatalf("w=%d badge %q", w, b)
		}
	}
}

func TestBAD5PlanFailedOnForm(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("a")
	h.m.sh.planErr = true
	b := h.badge()
	if !strings.Contains(b, tr("tui.status.plan_failed_plain")) || strings.Contains(b, "bấm D") {
		t.Fatalf("badge %q", b)
	}
}

func TestBAD6EnglishFormBadge(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang("vi")
	h := newHarness(t, emptyCompanyCfg("personal"))
	h.key("a")
	b := h.badge()
	if strings.Contains(b, "press a") || !strings.Contains(b, "personal") {
		t.Fatalf("badge %q", b)
	}
}

// ---- return focus (R6) ----

func TestFOC1FocusAfterLater(t *testing.T) {
	h := newHarness(t, basicCfg())
	addDora(h)
	h.key("n")
	if got := h.listScreen().currentKey(); got != "acme/dora" {
		t.Fatalf("current %q", got)
	}
}

func TestFOC2FocusAfterApply(t *testing.T) {
	h := newHarness(t, basicCfg())
	addDora(h)
	h.key("y")
	h.waitFor("apply done", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confDone })
	h.key("enter")
	if h.depth() != 1 || h.listScreen().currentKey() != "acme/dora" {
		t.Fatalf("depth %d current %q", h.depth(), h.listScreen().currentKey())
	}
}

func TestFOC3FocusAfterEdit(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.selectKey("acme/bob")
	h.key("e")
	f := accountFormTop(t, h)
	if f.edit == nil {
		t.Fatal("not an edit form")
	}
	h.key("enter") // platform
	h.key("enter") // username
	h.key("enter") // name
	h.typeText("x")
	for i := 0; i < 12; i++ {
		h.key("enter")
		if _, ok := h.top().(*confirmScreen); ok {
			break
		}
	}
	h.waitFor("confirm ready", func() bool { c, ok := h.top().(*confirmScreen); return ok && c.state == confReady })
	h.key("n")
	if got := h.listScreen().currentKey(); got != "acme/bob" {
		t.Fatalf("current %q", got)
	}
}

// ---- negative ----

func TestNEG1PopAtRootIsHarmless(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.send(popMsg{})
	h.send(popRootMsg{})
	if h.depth() != 1 {
		t.Fatalf("depth %d", h.depth())
	}
}

func TestNEG2AddAccountWithoutRows(t *testing.T) {
	h := newHarness(t, nil)
	h.key("a")
	if h.depth() != 1 || !strings.Contains(h.m.status.text, defKM.T("tui.status.need_company")) {
		t.Fatalf("depth %d status %q", h.depth(), h.m.status.text)
	}
}

func TestNEG3CapturedKeyDoesNotNavigate(t *testing.T) {
	h := newHarness(t, basicCfg())
	h.key("a")
	h.key("a")
	if h.depth() != 2 {
		t.Fatalf("depth %d", h.depth())
	}
}

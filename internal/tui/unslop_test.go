package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/keymap"
)

// Pure helpers of the visual direction: no harness, no model.

func TestKvColWidestLabelPlusTwoClamped(t *testing.T) {
	cases := []struct {
		name   string
		w      int
		labels []string
		want   int
	}{
		{"short labels take the floor", 76, []string{"Repo", "Remote"}, 10},
		{"widest label plus two", 76, []string{"Username", "Tên trong commit", "Email"}, 18},
		{"no labels", 76, nil, 10},
		{"a third of the width, unless that would cut the label", 45, []string{"Tên trong commit"}, 18},
		{"a label wider than half the width is cut at half", 30, []string{"Tên trong commit"}, 15},
		{"never under the floor, even in a narrow block", 16, []string{"Tên trong commit"}, 10},
		{"a third is plenty for short labels", 90, []string{"Username"}, 10},
		{"wide characters count their cells", 76, []string{"日本語日本語"}, 14},
	}
	for _, c := range cases {
		if got := kvCol(c.w, c.labels...); got != c.want {
			t.Errorf("%s: kvCol(%d, %q) = %d, want %d", c.name, c.w, c.labels, got, c.want)
		}
	}
}

func TestKvRowAlignsTheValueAndSkipsEmptyOnes(t *testing.T) {
	st := NewStyles()
	if got := kvRow(st, 10, 40, "Remote", ""); got != "" {
		t.Fatalf("an empty value draws nothing: %q", got)
	}
	got := ansi.Strip(kvRow(st, 12, 40, "Remote", "https://x"))
	if got != "Remote      https://x" {
		t.Fatalf("%q", got)
	}
	// the value is cut to the width, the label column is not
	cut := ansi.Strip(kvRow(st, 12, 20, "Remote", "https://example.com/a/very/long/path"))
	if lipgloss.Width(cut) > 20 || !strings.HasPrefix(cut, "Remote      https") {
		t.Fatalf("%q", cut)
	}
}

func TestKvRowsWrapUnderTheValueColumn(t *testing.T) {
	st := NewStyles()
	rows := kvRows(st, 10, 30, "Remote", "https://alice@github.com/<owner>/<repo>.git")
	if len(rows) < 2 {
		t.Fatalf("a long remote wraps: %q", rows)
	}
	for i, r := range rows {
		plain := ansi.Strip(r)
		if lipgloss.Width(plain) > 30 {
			t.Errorf("row %d is %d wide", i, lipgloss.Width(plain))
		}
		if i == 0 && !strings.HasPrefix(plain, "Remote    ") {
			t.Errorf("first row %q", plain)
		}
		if i > 0 && !strings.HasPrefix(plain, strings.Repeat(" ", 10)) {
			t.Errorf("row %d does not continue under the value column: %q", i, plain)
		}
	}
}

func TestProseMeasure(t *testing.T) {
	if proseW(40) != 40 || proseW(72) != 72 || proseW(110) != measure {
		t.Fatalf("proseW: %d %d %d", proseW(40), proseW(72), proseW(110))
	}
}

func TestTrCountPicksTheForm(t *testing.T) {
	defer i18n.SetLang("vi")
	i18n.SetLang("en")
	if got := trCount("tui.status.pending", 1); got != "1 unapplied change" {
		t.Errorf("one: %q", got)
	}
	if got := trCount("tui.status.pending", 11); got != "11 unapplied changes" {
		t.Errorf("many: %q", got)
	}
	if got := trCount("tui.delete.q_company", 1, "acme"); !strings.Contains(got, "acme") || !strings.Contains(got, "1 account ") {
		t.Errorf("an argument after the count: %q", got)
	}
	i18n.SetLang("vi")
	if got := trCount("tui.status.pending", 1); got != "1 thay đổi chưa áp dụng" {
		t.Errorf("vi: %q", got)
	}
}

func TestAgeTextIsWordsInTheActiveLanguage(t *testing.T) {
	defer i18n.SetLang("vi")
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	day := 24 * time.Hour
	cases := []struct {
		t        time.Time
		vi, en   string
		describe string
	}{
		{time.Time{}, "", "", "unknown"},
		{ago(10 * time.Second), "vừa xong", "now", "seconds"},
		{ago(5 * time.Minute), "5 phút", "5m", "minutes"},
		{ago(3 * time.Hour), "3 giờ", "3h", "hours"},
		{ago(3 * day), "3 ngày", "3d", "days"},
		{ago(120 * day), "4 tháng", "4mo", "months"},
		{ago(800 * day), "2 năm", "2y", "years"},
	}
	for _, c := range cases {
		i18n.SetLang("vi")
		if got := ageText(c.t, now); got != c.vi {
			t.Errorf("%s vi: %q, want %q", c.describe, got, c.vi)
		}
		i18n.SetLang("en")
		if got := ageText(c.t, now); got != c.en {
			t.Errorf("%s en: %q, want %q", c.describe, got, c.en)
		}
	}
}

func TestRightAlign(t *testing.T) {
	if got := rightAlign("abc", "1/7", 10); got != "abc    1/7" {
		t.Errorf("%q", got)
	}
	if got := rightAlign("abc", "", 10); got != "abc" {
		t.Errorf("no right part: %q", got)
	}
	if got := rightAlign("abcdefgh", "1/7", 10); got != "abcdefgh" {
		t.Errorf("no room, no right part: %q", got)
	}
}

func stampRef() config.AccountRef {
	return config.AccountRef{
		Company: config.Company{Slug: "acme"},
		Account: config.Account{ID: "alice", Name: "Alice Nguyen", Email: "alice@acme.com"},
	}
}

func TestIdentityStampIsGitsOwnAuthorNotation(t *testing.T) {
	st := NewStyles()
	want := "acme/alice · Alice Nguyen <alice@acme.com>"
	if got := ansi.Strip(st.identityStamp(stampRef())); got != want {
		t.Errorf("styled: %q", got)
	}
	if got := identityStampPlain(stampRef()); got != want {
		t.Errorf("plain: %q", got)
	}
	// a form that has no name or email yet shows what it knows
	ref := stampRef()
	ref.Account.Name, ref.Account.Email = "", ""
	if got := identityStampPlain(ref); got != "acme/alice" {
		t.Errorf("bare: %q", got)
	}
}

func TestStampLinesWrapsInsteadOfCutting(t *testing.T) {
	st := NewStyles()
	if got := stampLines(st, stampRef(), 60); len(got) != 1 {
		t.Fatalf("fits: %q", got)
	}
	got := stampLines(st, stampRef(), 30)
	if len(got) < 2 {
		t.Fatalf("narrow: %q", got)
	}
	joined := ansi.Strip(strings.Join(got, " "))
	if strings.Contains(joined, "…") || !strings.Contains(joined, "<alice@acme.com>") {
		t.Errorf("cut: %q", joined)
	}
}

func TestOrphanColCarriesTheCompanyWhenTheRowIsHidden(t *testing.T) {
	c := config.Company{Slug: "globex", Accounts: []config.Account{{ID: "dave"}}}
	acc := c.Accounts[0]
	company := rowItem{company: c}
	account := rowItem{company: c, account: &acc, last: true}
	// the company row is shown: nothing special
	if got := orphanCol([]list.Item{company, account}, account); got != 0 {
		t.Errorf("parent shown: %d", got)
	}
	// a filter hid it: the id carries "globex/", and the column is wide enough
	if got := orphanCol([]list.Item{account}, account); got < len("globex/dave") || got < idCol {
		t.Errorf("parent hidden: %d", got)
	}
	if got := orphanCol([]list.Item{company}, company); got != 0 {
		t.Errorf("a company row is never an orphan: %d", got)
	}
}

// The glyph vocabulary: one meaning per glyph. The retired glyphs are gone
// from the code and the survivors live in the one file that owns them.
func TestGlyphVocabulary(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	owner := map[string][]string{
		"▪": nil,                     // retired: a company row is told apart by its bold slug
		"◂": nil,                     // retired: the cyclers are ← and →
		"×": nil,                     // retired as a count: "2 bản clone"
		"●": {"root.go"},             // the not-applied badge only
		"◉": {"form.go"},             // the chosen radio only
		"›": {"root.go"},             // the breadcrumb separator only
		"┃": {"form.go", "theme.go"}, // the focused field gutter and the heavy list border
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			code := line
			if i := strings.Index(code, "//"); i >= 0 {
				code = code[:i] // comments may name a glyph
			}
			for glyph, owners := range owner {
				if !strings.Contains(code, glyph) {
					continue
				}
				ok := false
				for _, o := range owners {
					ok = ok || o == f
				}
				if !ok {
					t.Errorf("%s uses %q, which belongs to %v: %s", f, glyph, owners, strings.TrimSpace(line))
				}
			}
		}
	}
}

// An error in the status bar is never cut to keep the badge: the badge gives
// way. A message that expires does the same, and with room both are shown.
func TestStatusErrorWinsOverTheBadge(t *testing.T) {
	cfg := config.Config{Companies: []config.Company{{Slug: "acme", Accounts: []config.Account{{ID: "alice"}}}}}
	sh := &shared{st: NewStyles(), km: NewKeyMap(keymap.Defaults()), cfg: cfg, loaded: true, planned: true, pending: 11}
	m := &Model{sh: sh, stack: []Screen{&listScreen{sh: sh}}, w: 70, h: 24}
	long := "open /home/x/.gitconfig: permission denied"
	m.status = statusMsg{level: levelErr, text: long}
	got := ansi.Strip(m.statusView())
	if !strings.Contains(got, "✗ "+long) {
		t.Fatalf("the error was cut: %q", got)
	}
	if strings.Contains(got, "●") {
		t.Fatalf("the badge stayed beside a long error: %q", got)
	}
	if lipgloss.Width(got) != m.w {
		t.Fatalf("the line is %d cells wide, want %d", lipgloss.Width(got), m.w)
	}
	// room for both: both are shown
	m.w = 120
	if got := ansi.Strip(m.statusView()); !strings.Contains(got, "✗ "+long) || !strings.Contains(got, "●") {
		t.Fatalf("both fit: %q", got)
	}
	// a short error is not cut either; only an error wider than the line is
	m.w = 40
	m.status = statusMsg{level: levelErr, text: strings.Repeat("x", 80)}
	if got := ansi.Strip(m.statusView()); lipgloss.Width(got) > m.w || strings.Contains(got, "●") {
		t.Fatalf("40 cells: %q", got)
	}
}

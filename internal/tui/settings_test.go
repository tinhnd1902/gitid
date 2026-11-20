package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/testutil"
)

// The Settings screen and the language switch. Every test runs on a temp HOME
// (internal/testutil); the harness puts the process language back when the
// test ends.

// envWith makes GITID_LANG read as value (and nothing else).
func envWith(value string) func(*env.Env) {
	return func(e *env.Env) {
		e.LookupEnv = func(k string) (string, bool) {
			if k == envLang {
				return value, true
			}
			return "", false
		}
	}
}

// settingsHarness opens the Settings screen from the start menu with the
// digit. envLang (optional) is the value GITID_LANG reads as; rawConfig
// (optional) is written verbatim as config.yaml instead of seeding cfg.
func settingsHarness(t *testing.T, cfg *config.Config, envLang, rawConfig string) *harness {
	t.Helper()
	var mutate func(*env.Env)
	if envLang != "" {
		mutate = envWith(envLang)
	}
	var before func(e env.Env)
	if rawConfig != "" {
		before = func(e env.Env) { testutil.WriteFile(t, e.Paths().ConfigFile, rawConfig, 0o600) }
	}
	h := buildHarnessOpts(t, cfg, mutate, before, Options{}, nil)
	h.key("8")
	if _, ok := h.top().(*settingsScreen); !ok {
		t.Fatalf("the digit 8 opened %T", h.top())
	}
	return h
}

// settingsHarnessKeys is settingsHarness with a keys.yaml (empty: none), for
// the guard.
func settingsHarnessKeys(t *testing.T, envLang, keysYAML string) *harness {
	t.Helper()
	var mutate func(*env.Env)
	if envLang != "" {
		mutate = envWith(envLang)
	}
	var before func(e env.Env)
	if keysYAML != "" {
		before = func(e env.Env) { testutil.WriteFile(t, e.Paths().KeyBindingsFile, keysYAML, 0o644) }
	}
	h := buildHarnessOpts(t, basicCfg(), mutate, before, Options{}, nil)
	h.key("8")
	return h
}

func (h *harness) savedLang() string {
	h.t.Helper()
	cfg, err := config.Load(h.e)
	if err != nil {
		h.t.Fatalf("load config.yaml: %v", err)
	}
	return cfg.Settings.Language
}

func (h *harness) backupCount() int {
	ents, err := os.ReadDir(h.e.Paths().BackupsDir)
	if err != nil {
		return 0
	}
	return len(ents)
}

// S1: the menu has the eighth row; the digit and enter open it.
func TestS1MenuRowOpensSettings(t *testing.T) {
	h := newHomeHarness(t, basicCfg())
	h.resize(80, 24)
	if len(homeItems) != 8 {
		t.Fatalf("%d menu rows", len(homeItems))
	}
	h.mustContainSpaceless("8Càiđặt", "1-8")
	h.key("8")
	if _, ok := h.top().(*settingsScreen); !ok || h.depth() != 1 {
		t.Fatalf("top %T depth %d", h.top(), h.depth())
	}
	if hdr := h.lines()[0]; !strings.Contains(hdr, "gitid") || !strings.Contains(hdr, "Cài đặt") {
		t.Fatalf("header %q", hdr)
	}
	h.key("esc")
	for i := 0; i < 10; i++ {
		h.key("down")
	}
	h.key("enter")
	if _, ok := h.top().(*settingsScreen); !ok {
		t.Fatalf("enter on the last row opened %T", h.top())
	}
}

// S2: the arrow switches the whole interface on the next frame.
func TestS2SwitchAppliesAtOnce(t *testing.T) {
	h := settingsHarness(t, basicCfg(), "", "")
	h.mustContain("Ngôn ngữ", "◉ Tiếng Việt", "○ English")
	h.key("right")
	if i18n.Lang() != "en" {
		t.Fatalf("language %q", i18n.Lang())
	}
	h.mustContain("Language", "◉ English", "○ Tiếng Việt")
	h.mustNotContain("Ngôn ngữ")
	if hdr := h.lines()[0]; !strings.Contains(hdr, "Settings") {
		t.Fatalf("header %q", hdr)
	}
	if hb := h.helpLine(); !strings.Contains(hb, "←/→ change") || !strings.Contains(hb, "esc back") || !strings.Contains(hb, "all keys") {
		t.Fatalf("help bar %q", hb)
	}
	if st := h.status(); st != "Language saved: English" {
		t.Fatalf("status %q", st)
	}
	h.key("?")
	h.mustContain("change", "next option")
	h.key("esc")
	h.key("esc")
	if h.depth() != 0 {
		t.Fatalf("depth %d", h.depth())
	}
	h.mustContain("Accounts", "Settings", "1-8 jump")
	h.mustNotContain("Tài khoản")
	// the left arrow switches back
	h.key("8")
	h.key("left")
	if i18n.Lang() != "vi" {
		t.Fatalf("language %q", i18n.Lang())
	}
	h.mustContain("Ngôn ngữ", "◉ Tiếng Việt")
}

// S3: the choice is in config.yaml; nothing else changed.
func TestS3SwitchIsSaved(t *testing.T) {
	seed := basicCfg()
	h := settingsHarness(t, seed, "", "")
	before, err := os.ReadFile(h.e.Paths().ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	pendingBefore := h.m.sh.pending
	h.key("right")
	if got := h.savedLang(); got != "en" {
		t.Fatalf("saved language %q", got)
	}
	want := *seed
	want.Settings.Language = "en"
	gotCfg, err := config.Load(h.e)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := config.Marshal(config.Normalize(want, h.e))
	b, _ := config.Marshal(config.Normalize(gotCfg, h.e))
	if string(a) != string(b) {
		t.Fatalf("other fields changed:\nwant %s\ngot  %s", a, b)
	}
	fi, err := os.Stat(h.e.Paths().ConfigFile)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", fi, err)
	}
	if h.backupCount() != 1 {
		t.Fatalf("%d backups", h.backupCount())
	}
	// the backup holds the previous file
	var found bool
	_ = filepath.Walk(h.e.Paths().BackupsDir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			if data, _ := os.ReadFile(p); string(data) == string(before) {
				found = true
			}
		}
		return nil
	})
	if !found {
		t.Fatal("no backup of the previous config.yaml")
	}
	// R-5: not a pending change; nothing written outside config.yaml
	if h.m.sh.pending != pendingBefore || !h.m.sh.planned {
		t.Fatalf("pending %d -> %d", pendingBefore, h.m.sh.pending)
	}
	for _, p := range []string{h.e.Paths().GitConfig, h.e.Paths().SSHDir} {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("%s exists", p)
		}
	}
}

// S4: enter goes to the next option and wraps.
func TestS4EnterCycles(t *testing.T) {
	h := settingsHarness(t, basicCfg(), "", "")
	h.key("enter")
	if i18n.Lang() != "en" || h.savedLang() != "en" {
		t.Fatalf("after one enter: %q, file %q", i18n.Lang(), h.savedLang())
	}
	h.key("enter")
	if i18n.Lang() != "vi" || h.savedLang() != "vi" {
		t.Fatalf("after two enters: %q, file %q", i18n.Lang(), h.savedLang())
	}
}

// S5: the note about GITID_LANG; the value is never echoed.
func TestS5EnvNote(t *testing.T) {
	h := settingsHarness(t, basicCfg(), "en", "")
	h.mustContain("GITID_LANG đang được đặt")
	h.key("right")
	h.mustContain("GITID_LANG is set")
	if h.savedLang() != "en" {
		t.Fatalf("saved %q", h.savedLang())
	}

	h = settingsHarness(t, basicCfg(), "\x1b[31mx", "")
	h.mustContain("GITID_LANG")
	if strings.ContainsRune(h.view(), 0x1b) {
		t.Fatal("the value of GITID_LANG reached the screen")
	}
	h.mustNotContain("31mx")

	h = settingsHarness(t, basicCfg(), "", "")
	h.mustNotContain("GITID_LANG")
}

// S6: a config.yaml of another version: the run switches, the file stays.
func TestS6UnsupportedConfigKeepsTheFile(t *testing.T) {
	raw := "version: 99\n"
	h := settingsHarness(t, nil, "", raw)
	h.key("right")
	if i18n.Lang() != "en" {
		t.Fatalf("language %q", i18n.Lang())
	}
	if st := h.status(); !strings.HasPrefix(st, "Could not save the language") {
		t.Fatalf("status %q", st)
	}
	if h.m.status.level != levelErr {
		t.Fatalf("level %v", h.m.status.level)
	}
	data, err := os.ReadFile(h.e.Paths().ConfigFile)
	if err != nil || string(data) != raw {
		t.Fatalf("file %q %v", data, err)
	}
	if h.backupCount() != 0 {
		t.Fatalf("%d backups", h.backupCount())
	}
}

// S7: a config.yaml that does not validate: error, language still switched.
func TestS7InvalidConfigReportsAnError(t *testing.T) {
	raw := "version: 1\ncompanies:\n  - slug: \"Not A Slug!\"\n    name: Bad\n"
	h := settingsHarness(t, nil, "", raw)
	h.key("right")
	if i18n.Lang() != "en" {
		t.Fatalf("language %q", i18n.Lang())
	}
	if st := h.status(); !strings.HasPrefix(st, "Could not save the language") || h.m.status.level != levelErr {
		t.Fatalf("status %q level %v", st, h.m.status.level)
	}
	data, _ := os.ReadFile(h.e.Paths().ConfigFile)
	if string(data) != raw {
		t.Fatalf("file changed: %q", data)
	}
}

// S8: up and down with one row do nothing.
func TestS8OneRowIgnoresUpDown(t *testing.T) {
	h := settingsHarness(t, basicCfg(), "", "")
	s := h.top().(*settingsScreen)
	for _, k := range []string{"up", "down", "up", "down", "down"} {
		h.key(k)
	}
	if s.sel != 0 || i18n.Lang() != "vi" {
		t.Fatalf("sel %d lang %q", s.sel, i18n.Lang())
	}
	// the arrows up and down are not advertised while there is one row
	h.mustNotContain("↑↓")
	for _, b := range h.top().Help() {
		if strings.HasPrefix(b.Help().Key, "↑") {
			t.Fatalf("Help lists %q", b.Help().Key)
		}
	}
}

// S9: choosing the language that is active saves nothing.
func TestS9SameLanguageIsNoSave(t *testing.T) {
	h := settingsHarness(t, basicCfg(), "", "")
	before, _ := os.ReadFile(h.e.Paths().ConfigFile)
	h.send(setLangMsg{code: "vi"})
	if h.m.langSaving || h.backupCount() != 0 {
		t.Fatalf("saving %v, %d backups", h.m.langSaving, h.backupCount())
	}
	after, _ := os.ReadFile(h.e.Paths().ConfigFile)
	if string(before) != string(after) {
		t.Fatal("config.yaml changed")
	}
	// a code gitid does not have is not a language either
	h.send(setLangMsg{code: "fr"})
	if i18n.Lang() != "vi" || h.m.langSaving {
		t.Fatalf("lang %q saving %v", i18n.Lang(), h.m.langSaving)
	}
}

// S10: every frame is exactly w x h, in both languages, with and without the
// note.
func TestS10FrameSizes(t *testing.T) {
	sizes := [][2]int{{40, 10}, {44, 12}, {80, 24}, {120, 40}}
	for _, envLang := range []string{"", "en"} {
		h := settingsHarness(t, basicCfg(), envLang, "")
		for _, lang := range []string{"vi", "en"} {
			i18n.SetLang(lang)
			for _, sz := range sizes {
				h.resize(sz[0], sz[1])
				lines := h.lines()
				if len(lines) != sz[1] {
					t.Errorf("env %q %s %dx%d: %d lines", envLang, lang, sz[0], sz[1], len(lines))
				}
				for i, l := range lines {
					if w := lipgloss.Width(l); w > sz[0] {
						t.Errorf("env %q %s %dx%d: line %d is %d wide: %q", envLang, lang, sz[0], sz[1], i, w, l)
					}
				}
				h.mustContain(tr("tui.settings.lang"))
			}
		}
	}
}

// S11: without a config.yaml the switch creates one.
func TestS11CreatesTheConfig(t *testing.T) {
	h := settingsHarness(t, nil, "", "")
	if _, err := os.Stat(h.e.Paths().ConfigFile); err == nil {
		t.Fatal("config.yaml exists before the switch")
	}
	h.key("right")
	cfg, err := config.Load(h.e)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != 1 || cfg.Settings.Language != "en" {
		t.Fatalf("version %d language %q", cfg.Version, cfg.Settings.Language)
	}
	fi, err := os.Stat(h.e.Paths().ConfigFile)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", fi, err)
	}
}

// S12: choices that arrive while a save runs: one save at a time, and the
// file ends with the last choice.
func TestS12FastToggling(t *testing.T) {
	h := settingsHarness(t, basicCfg(), "", "")
	h.update(setLangMsg{code: "en"})
	if !h.m.langSaving {
		t.Fatal("no save started")
	}
	h.update(setLangMsg{code: "vi"})
	h.update(setLangMsg{code: "en"})
	h.update(setLangMsg{code: "vi"})
	if !h.m.langSaving || h.m.langWant != "vi" {
		t.Fatalf("saving %v want %q", h.m.langSaving, h.m.langWant)
	}
	h.drain()
	if h.m.langSaving {
		t.Fatal("a save is still marked in flight")
	}
	if i18n.Lang() != "vi" || h.savedLang() != "vi" {
		t.Fatalf("lang %q file %q", i18n.Lang(), h.savedLang())
	}

	// the other way round: the last choice is the first one's neighbour
	h.update(setLangMsg{code: "en"})
	h.update(setLangMsg{code: "vi"})
	h.update(setLangMsg{code: "en"})
	h.drain()
	if i18n.Lang() != "en" || h.savedLang() != "en" || h.m.langSaving {
		t.Fatalf("lang %q file %q saving %v", i18n.Lang(), h.savedLang(), h.m.langSaving)
	}
}

// The keys of the screen are rebuilt in the new language.
func TestSettingsKeyMapFollowsTheLanguage(t *testing.T) {
	h := settingsHarness(t, basicCfg(), "", "")
	if got := h.m.sh.km.Help.Help().Desc; got != "trợ giúp" {
		t.Fatalf("help desc %q", got)
	}
	h.key("right")
	if got := h.m.sh.km.Help.Help().Desc; got != "help" {
		t.Fatalf("help desc %q", got)
	}
}

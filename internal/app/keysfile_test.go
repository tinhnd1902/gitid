package app_test

import (
	"os"
	"strings"
	"testing"

	"github.com/tinhnd1902/gitid/internal/testutil"
	"github.com/tinhnd1902/gitid/internal/testutil/fixtures"
)

// NOWRITE-4: config.yaml written by Commit has no key bindings, whether or
// not a keys.yaml exists next to it.
func TestNOWRITE4ConfigYAMLHasNoKeys(t *testing.T) {
	for _, withKeys := range []bool{false, true} {
		svc, e := newSvc(t, nil)
		if withKeys {
			testutil.WriteFile(t, e.Paths().KeyBindingsFile, "check: v\n", 0o644)
		}
		if _, _, err := svc.Commit(fixtures.Basic()); err != nil {
			t.Fatal(err)
		}
		cfg := testutil.ReadFile(t, e.Paths().ConfigFile)
		for _, l := range strings.Split(cfg, "\n") {
			if strings.HasPrefix(l, "keys:") || strings.Contains(l, "keys.yaml") {
				t.Errorf("config.yaml mentions key bindings: %q", l)
			}
		}
		if withKeys && testutil.ReadFile(t, e.Paths().KeyBindingsFile) != "check: v\n" {
			t.Error("Commit changed keys.yaml")
		}
		if !withKeys {
			if _, err := os.Stat(e.Paths().KeyBindingsFile); !os.IsNotExist(err) {
				t.Errorf("Commit created keys.yaml: %v", err)
			}
		}
	}
}

// NOWRITE-5: a top-level keys: section in config.yaml is still a load error
// (the key bindings live in keys.yaml, not in config.yaml).
func TestNOWRITE5KeysSectionInConfigStillRejected(t *testing.T) {
	svc, e := newSvc(t, nil)
	testutil.WriteFile(t, e.Paths().ConfigFile, "version: 1\nkeys:\n  check: v\ncompanies: []\n", 0o644)
	if _, err := svc.Load(); err == nil {
		t.Fatal("config.yaml with a keys: section loaded")
	}
}

// STALE: ChangedSince looks at config.yaml only. Creating, editing or
// removing keys.yaml never makes the loaded configuration stale.
func TestSTALEKeysYAMLDoesNotAffectChangedSince(t *testing.T) {
	svc, e := newSvc(t, nil)
	cfg, _, err := svc.Commit(fixtures.Basic())
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := svc.Load()
	if err != nil {
		t.Fatal(err)
	}
	p := e.Paths().KeyBindingsFile
	check := func(step string) {
		t.Helper()
		if _, changed, err := svc.ChangedSince(loaded); err != nil || changed {
			t.Fatalf("%s: changed=%v err=%v", step, changed, err)
		}
	}
	check("no keys.yaml")
	testutil.WriteFile(t, p, "check: v\n", 0o644)
	check("keys.yaml created")
	testutil.WriteFile(t, p, "check: [v\n", 0o644)
	check("keys.yaml broken")
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	check("keys.yaml removed")
	// and a real edit of config.yaml is still seen
	cfg.Companies = cfg.Companies[:1]
	if _, _, err := svc.Commit(cfg); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := svc.ChangedSince(loaded); err != nil || !changed {
		t.Fatalf("config.yaml edit not detected: changed=%v err=%v", changed, err)
	}
}

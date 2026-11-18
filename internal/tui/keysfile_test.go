package tui

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/tinhnd1902/gitid/internal/keymap"
	"github.com/tinhnd1902/gitid/internal/testutil"
)

// KM-2: the registry in internal/keymap and the KeyMap of the TUI describe
// the same actions with the same default keys. (Phase C builds the KeyMap
// from the registry; until then this keeps the two from drifting.)
var actionField = map[keymap.Action]string{
	keymap.Quit: "Quit", keymap.Help: "Help", keymap.Back: "Back", keymap.Filter: "Filter",
	keymap.AddAccount: "AddAccount", keymap.AddCompany: "AddCompany", keymap.Edit: "Edit", keymap.Delete: "Delete",
	keymap.Clone: "Clone", keymap.Check: "Check", keymap.Doctor: "Doctor", keymap.Apply: "Apply",
	keymap.SSHKey: "GenKey", keymap.Import: "Import", keymap.Reload: "Reload", keymap.Yes: "Yes", keymap.No: "No",
	keymap.Fix: "Fix", keymap.Retry: "Retry", keymap.Copy: "Copy", keymap.Open: "Open", keymap.SSHTest: "SSHTest",
	keymap.Terminal: "TerminalMode",
}

func TestKM2RegistryMatchesKeyMap(t *testing.T) {
	km := NewKeyMap()
	rv := reflect.ValueOf(km).Elem()
	seen := map[string]bool{}
	for _, d := range keymap.Defs() {
		name, ok := actionField[d.Action]
		if !ok {
			t.Errorf("action %s has no KeyMap field in this test", d.Action)
			continue
		}
		f := rv.FieldByName(name)
		if !f.IsValid() {
			t.Errorf("KeyMap has no field %s (action %s)", name, d.Action)
			continue
		}
		seen[name] = true
		keys := f.Interface().(interface{ Keys() []string }).Keys()
		want := d.Default
		if d.Action == keymap.Back {
			want = append([]string{"esc"}, d.Default...) // esc is fixed and always goes back
		}
		if !slices.Equal(keys, want) {
			t.Errorf("%s: KeyMap.%s keys %v, registry default %v", d.Action, name, keys, want)
		}
	}
	if len(actionField) != len(keymap.Defs()) {
		t.Errorf("%d mapped actions, %d in the registry", len(actionField), len(keymap.Defs()))
	}
}

// keysFlow runs the save paths of the TUI (new company, new account, edit,
// delete, reload, apply) and fails on a stale-config report after any of them.
func keysFlow(t *testing.T, h *harness, mid func()) {
	t.Helper()
	notStale := func(step string) {
		t.Helper()
		if strings.Contains(plain(h.view()), plain(tr("tui.err.config_changed"))) {
			t.Fatalf("%s: false stale report:\n%s", step, h.view())
		}
	}
	back := func() {
		h.waitReady()
		h.key("n")
		if _, ok := h.top().(*listScreen); !ok {
			t.Fatalf("top is %T", h.top())
		}
	}
	h.key("A")
	h.typeText("x")
	h.key("enter")
	h.typeText("X Corp")
	h.key("enter")
	h.key("enter")
	h.waitFor("company created", func() bool {
		_, ok := h.top().(*listScreen)
		return ok && h.depth() == 1 && h.listScreen().currentKey() == "x"
	})
	notStale("add company")
	if mid != nil {
		mid() // keys.yaml edited from outside while the window is open
	}
	h.key("a")
	fillAccount(h, "alice", "Alice", "alice@example.com")
	h.waitConfirm()
	notStale("add account")
	back()
	h.selectKey("x/alice")
	h.key("e")
	for i := 0; i < 6; i++ {
		h.key("enter")
	}
	h.waitConfirm()
	notStale("edit account")
	back()
	h.selectKey("x")
	h.key("e")
	h.key("enter")
	h.key("enter")
	h.waitConfirm()
	notStale("edit company")
	back()
	h.key("r")
	notStale("reload")
	h.applyToDone()
	h.key("esc")
	if _, ok := h.top().(*listScreen); !ok {
		t.Fatalf("top is %T", h.top())
	}
	h.selectKey("x/alice")
	h.key("d")
	h.key("y")
	h.waitConfirm()
	notStale("delete account")
	if _, changed, err := h.svc.ChangedSince(h.m.sh.cfg); err != nil || changed {
		t.Fatalf("the TUI's view of the config differs from the disk: changed=%v err=%v", changed, err)
	}
}

func fileState(t *testing.T, p string) (data []byte, mtime int64, exists bool) {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, false
		}
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return b, fi.ModTime().UnixNano(), true
}

// NOWRITE-1, NOWRITE-2 and STALE-2: with a keys.yaml present every save path
// leaves its bytes and mtime alone and never copies it into backups/; without
// one no file appears; an outside edit of keys.yaml never makes a save stale.
func TestNOWRITETUIKeysYAML(t *testing.T) {
	for _, withFile := range []bool{true, false} {
		name := "without keys.yaml"
		if withFile {
			name = "with keys.yaml"
		}
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, emptyCompanyCfg("personal"))
			p := h.e.Paths().KeyBindingsFile
			if withFile {
				testutil.WriteFile(t, p, "# my keys\ncheck: v\n", 0o644)
			}
			var editedAt int64
			mid := func() {}
			if withFile {
				mid = func() {
					testutil.WriteFile(t, p, "# edited outside gitid\ncheck: w\nclone: [c, n]\n", 0o644)
					old := h.e.Now().Add(-48 * 3600) // an old mtime makes any later touch visible
					if err := os.Chtimes(p, old, old); err != nil {
						t.Fatal(err)
					}
					_, editedAt, _ = fileState(t, p)
				}
			}
			keysFlow(t, h, mid)

			data, mtime, exists := fileState(t, p)
			if !withFile {
				if exists {
					t.Fatalf("gitid created keys.yaml: %q", data)
				}
			} else {
				if string(data) != "# edited outside gitid\ncheck: w\nclone: [c, n]\n" {
					t.Fatalf("keys.yaml changed: %q", data)
				}
				if mtime != editedAt {
					t.Fatalf("keys.yaml touched after the outside edit: mtime %d -> %d", editedAt, mtime)
				}
			}
			// never copied into a backup, and config.yaml has no keys section
			_ = filepath.WalkDir(h.e.Paths().BackupsDir, func(path string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() && filepath.Base(path) == "keys.yaml" {
					t.Errorf("keys.yaml was backed up: %s", path)
				}
				return nil
			})
			cfg, err := os.ReadFile(h.e.Paths().ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			for _, l := range strings.Split(string(cfg), "\n") {
				if strings.HasPrefix(l, "keys:") || strings.Contains(l, "keys.yaml") {
					t.Errorf("config.yaml mentions the key bindings: %q", l)
				}
			}
		})
	}
}

// With the file unchanged by the test, bytes and mtime stay identical across
// all the flows (no touch, no rewrite with the same bytes).
func TestNOWRITETUIKeysYAMLBytesAndMtime(t *testing.T) {
	h := newHarness(t, emptyCompanyCfg("personal"))
	p := h.e.Paths().KeyBindingsFile
	testutil.WriteFile(t, p, "check: v\nbroken: [\n", 0o644)
	// an old mtime makes any rewrite visible
	old := h.e.Now().Add(-48 * 3600)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	before, mt, _ := fileState(t, p)
	keysFlow(t, h, nil)
	after, mt2, _ := fileState(t, p)
	if !bytes.Equal(before, after) || mt != mt2 {
		t.Fatalf("keys.yaml touched: bytes equal=%v mtime %d -> %d", bytes.Equal(before, after), mt, mt2)
	}
}

// STALE-4 (the part that exists before the TUI reads keys.yaml): with a
// keys.yaml present an outside edit of config.yaml is still refused, exactly
// as without one.
func TestSTALE4ConfigEditStillRefusedWithKeysYAML(t *testing.T) {
	h := newHarness(t, oneAccCfg())
	p := h.e.Paths().KeyBindingsFile
	testutil.WriteFile(t, p, "check: v\n", 0o644)
	writeBehindTheBack(t, h)
	h.key("a")
	h.key("enter")
	h.typeText("carol")
	h.key("enter")
	h.key("enter")
	h.typeText("carol@example.com")
	h.key("enter")
	h.key("enter")
	h.key("enter")
	h.key("enter")
	h.waitFor("refused", func() bool { return strings.Contains(plain(h.view()), plain(tr("tui.err.config_changed"))) })
	disk, err := h.svc.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := accountIDs(disk); got != "tinhnd,bob" {
		t.Fatalf("disk has %s", got)
	}
	if b, _ := os.ReadFile(p); string(b) != "check: v\n" {
		t.Fatalf("keys.yaml changed: %q", b)
	}
}

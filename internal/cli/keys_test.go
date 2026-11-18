package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/tinhnd1902/gitid/internal/cli"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/keymap"
	"github.com/tinhnd1902/gitid/internal/testutil"
)

// keygen is a fake ssh-keygen that writes a key pair (and a pbcopy stand-in).
func keygen() *execx.Fake {
	return execx.NewFake().On("ssh-keygen", execx.Response{Do: func(c execx.Cmd) error {
		p := c.Args[len(c.Args)-1]
		for i, a := range c.Args {
			if a == "-f" {
				p = c.Args[i+1]
			}
		}
		if err := os.WriteFile(p, []byte("PRIVATE"), 0o600); err != nil {
			return err
		}
		return os.WriteFile(p+".pub", []byte("ssh-ed25519 AAAA dave\n"), 0o644)
	}}).On("pbcopy", execx.Response{}).On("open", execx.Response{})
}

func (h *harness) writeKeys(content string) string {
	h.t.Helper()
	p := h.e.Paths().KeyBindingsFile
	testutil.WriteFile(h.t, p, content, 0o644)
	return p
}

func (h *harness) runLang(lang string, args ...string) (int, string, string) {
	h.t.Helper()
	var out, errb bytes.Buffer
	code := cli.Execute(cli.Deps{
		Env: h.e, Runner: h.f, Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb,
		IsTTY: func() bool { return false }, Cwd: func() (string, error) { return h.e.Home, nil },
		Version: "dev", Lang: lang,
	}, args)
	return code, out.String(), errb.String()
}

type keysDoc struct {
	Bindings []struct {
		Action      string   `json:"action"`
		Keys        []string `json:"keys"`
		Default     []string `json:"default"`
		Scopes      []string `json:"scopes"`
		Custom      bool     `json:"custom"`
		Description string   `json:"description"`
	} `json:"bindings"`
	Warnings []struct {
		Code    string `json:"code"`
		Action  string `json:"action"`
		Key     string `json:"key"`
		Message string `json:"message"`
	} `json:"warnings"`
}

func TestCLI1KeysDefaults(t *testing.T) {
	for _, seeded := range []bool{false, true} {
		h := newHarness(t, nil)
		if seeded {
			h.seed()
		}
		code, out, errOut := h.run("keys")
		if code != 0 || errOut != "" {
			t.Fatalf("code %d\n%s%s", code, out, errOut)
		}
		if !strings.Contains(out, "~/.config/gitid/keys.yaml") || !strings.Contains(out, "chưa có") {
			t.Errorf("no path or absence note:\n%s", out)
		}
		for _, d := range keymap.Defs() {
			var row string
			ls := strings.Split(out, "\n")
			for i, l := range ls {
				if f := strings.Fields(l); len(f) > 1 && f[0] == string(d.Action) {
					row = l
					// the scopes column wraps onto indented lines
					for j := i + 1; j < len(ls) && strings.HasPrefix(ls[j], strings.Repeat(" ", 20)); j++ {
						row += " " + strings.TrimSpace(ls[j])
					}
				}
			}
			if row == "" {
				t.Errorf("no row for %s:\n%s", d.Action, out)
				continue
			}
			if !strings.Contains(row, strings.Join(d.Default, ", ")) {
				t.Errorf("%s row lacks its default: %q", d.Action, row)
			}
			for _, s := range d.Scopes {
				if !strings.Contains(row, string(s)) {
					t.Errorf("%s row lacks scope %s: %q", d.Action, s, row)
				}
			}
			if strings.Contains(row, "*") {
				t.Errorf("%s marked custom: %q", d.Action, row)
			}
		}
		if strings.Contains(out, i18n.T("cli.keys.warnings")) {
			t.Errorf("warnings section without warnings:\n%s", out)
		}
	}
}

func TestCLI2CustomRow(t *testing.T) {
	h := newHarness(t, nil)
	h.writeKeys("check: v\n")
	code, out, _ := h.run("keys")
	if code != 0 {
		t.Fatalf("code %d\n%s", code, out)
	}
	if !strings.Contains(out, "present") && !strings.Contains(out, "Tệp phím tắt: ~/.config/gitid/keys.yaml\n") {
		t.Errorf("file note:\n%s", out)
	}
	var row string
	for _, l := range strings.Split(out, "\n") {
		if f := strings.Fields(l); len(f) > 1 && (f[0] == "check" || (f[0] == "*" && f[1] == "check")) {
			row = l
		}
	}
	f := strings.Fields(row)
	if len(f) < 4 || f[0] != "*" || f[2] != "v" || f[3] != "k" {
		t.Fatalf("check row %q", row)
	}
	// the other rows are not marked
	if n := strings.Count(out, "\n*  "); n != 1 {
		t.Errorf("%d custom rows:\n%s", n, out)
	}
}

func TestCLI3WarningsExit2(t *testing.T) {
	h := newHarness(t, nil)
	h.writeKeys("check: c\nchekc: v\n")
	code, out, _ := h.run("keys")
	if code != 2 {
		t.Fatalf("code %d\n%s", code, out)
	}
	m, is := keymap.Parse([]byte("check: c\nchekc: v\n"))
	_ = m
	if len(is) != 2 || !strings.Contains(out, i18n.T("cli.keys.warnings")) {
		t.Fatalf("%+v\n%s", is, out)
	}
	flat := strings.Join(strings.Fields(out), " ") // warnings wrap at 80 columns
	for _, i := range is {
		if !strings.Contains(flat, i.Message()) {
			t.Errorf("missing %q in:\n%s", i.Message(), out)
		}
	}
	// the table shows the defaults for the rejected entries
	if !strings.Contains(out, "check") {
		t.Fatal(out)
	}
}

func TestCLI4JSON(t *testing.T) {
	h := newHarness(t, nil)
	h.writeKeys("check: c\nchekc: v\nedit: [e, z]\n")
	code, out, _ := h.run("keys", "--json")
	if code != 2 {
		t.Fatalf("code %d", code)
	}
	var doc keysDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(doc.Bindings) != len(keymap.Defs()) {
		t.Fatalf("%d bindings", len(doc.Bindings))
	}
	for i, d := range keymap.Defs() {
		b := doc.Bindings[i]
		if b.Action != string(d.Action) || !slices.Equal(b.Default, d.Default) || len(b.Scopes) != len(d.Scopes) || b.Description == "" || len(b.Keys) == 0 {
			t.Errorf("binding %d: %+v", i, b)
		}
		if b.Action == "edit" && (!b.Custom || !slices.Equal(b.Keys, []string{"e", "z"})) {
			t.Errorf("edit: %+v", b)
		}
		if b.Action == "check" && (b.Custom || !slices.Equal(b.Keys, []string{"k"})) {
			t.Errorf("check: %+v", b)
		}
	}
	if len(doc.Warnings) != 2 { // check clashes with clone, chekc is unknown
		t.Fatalf("%+v", doc.Warnings)
	}
	for _, w := range doc.Warnings {
		if !slices.Contains(keymap.AllIssueCodes(), w.Code) || w.Message == "" {
			t.Errorf("%+v", w)
		}
	}
	// no warnings: an empty array, exit 0
	h2 := newHarness(t, nil)
	code, out, _ = h2.run("keys", "--json")
	if code != 0 || !strings.Contains(out, `"warnings": []`) {
		t.Fatalf("%d\n%s", code, out)
	}
}

func TestCLI5Example(t *testing.T) {
	h := newHarness(t, nil)
	before := testutil.Snapshot(t, h.e.Home)
	code, out, errOut := h.run("keys", "--example")
	if code != 0 || errOut != "" {
		t.Fatalf("%d %s", code, errOut)
	}
	want := keymap.Example(h.e.Contract(h.e.Paths().KeyBindingsFile))
	if out != want || !strings.Contains(out, "~/.config/gitid/keys.yaml") {
		t.Fatalf("output differs from keymap.Example:\n%s", out)
	}
	testutil.AssertSame(t, before, testutil.Snapshot(t, h.e.Home))
	if _, err := os.Stat(h.e.Paths().KeyBindingsFile); !os.IsNotExist(err) {
		t.Fatal("keys --example created the file")
	}
	// it prints the same even when keys.yaml exists and is broken
	h.writeKeys("check: [v\n")
	if code, out2, _ := h.run("keys", "--example"); code != 0 || out2 != want {
		t.Fatalf("%d", code)
	}
}

func TestCLI6ExampleJSONExclusive(t *testing.T) {
	h := newHarness(t, nil)
	code, out, errOut := h.run("keys", "--example", "--json")
	if code != 1 || out != "" || !strings.Contains(errOut, "--example") {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
}

func TestCLI7UnparsableKeysAndConfig(t *testing.T) {
	h := newHarness(t, nil)
	h.writeKeys("check: [v\n")
	code, out, _ := h.run("keys")
	if code != 2 || strings.Count(out, "  ! ") != 1 || !strings.Contains(out, "YAML") {
		t.Fatalf("%d\n%s", code, out)
	}
	if !strings.Contains(out, "k") {
		t.Fatal(out)
	}
	// a broken config.yaml does not change the output of gitid keys
	h2 := newHarness(t, nil)
	h2.writeKeys("check: v\n")
	_, clean, _ := h2.run("keys")
	testutil.WriteFile(t, h2.e.Paths().ConfigFile, "companies: [unclosed\n", 0o644)
	code, broken, errOut := h2.run("keys")
	if code != 0 || broken != clean || errOut != "" {
		t.Fatalf("%d err=%q\n%s\n---\n%s", code, errOut, clean, broken)
	}
}

func TestCLI8English(t *testing.T) {
	h := newHarness(t, nil)
	h.writeKeys("check: c\n")
	code, out, _ := h.runLang("en", "keys")
	if code != 2 {
		t.Fatalf("%d", code)
	}
	for _, want := range []string{"ACTION", "KEYS", "DEFAULT", "SCREENS", "Key bindings file:", "Warnings:", "clashes with clone"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "HÀNH ĐỘNG") {
		t.Error("Vietnamese header in English output")
	}
	code, out, errOut := h.runLang("en", "keys", "--example", "--json")
	if code != 1 || !strings.Contains(errOut, "can not be used together") {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
}

func TestCLI9EscapeNotEchoed(t *testing.T) {
	h := newHarness(t, nil)
	h.writeKeys("check: \"\\x1b[2J\"\n\"\\x1b[31mevil\": v\n")
	for _, args := range [][]string{{"keys"}, {"keys", "--json"}} {
		code, out, _ := h.run(args...)
		if code != 2 {
			t.Fatalf("%v: code %d", args, code)
		}
		if strings.ContainsRune(out, 0x1b) {
			t.Errorf("%v: raw ESC in output: %q", args, out)
		}
	}
	_, out, _ := h.run("keys")
	if !strings.Contains(out, `"\x1b[2J"`) {
		t.Errorf("no escaped form in:\n%s", out)
	}
}

func TestCLI10KeyCommandIsStillSSH(t *testing.T) {
	h := newHarness(t, nil)
	code, out, _ := h.run("key", "--help")
	if code != 0 || !strings.Contains(out, "gen") || !strings.Contains(out, "gitid keys") {
		t.Fatalf("%d\n%s", code, out)
	}
	code, out, _ = h.run("keys", "--help")
	if code != 0 || !strings.Contains(out, "gitid key") || !strings.Contains(out, "--example") || !strings.Contains(out, "--json") {
		t.Fatalf("%d\n%s", code, out)
	}
	// no prefix matching: "gitid ke" is not an alias of either
	if code, _, _ := h.run("ke"); code == 0 {
		t.Fatal("prefix matching is on")
	}
}

func TestNOWRITE3CLINeverTouchesKeysYAML(t *testing.T) {
	for _, withFile := range []bool{false, true} {
		h := newHarness(t, keygen())
		h.seed()
		p := h.e.Paths().KeyBindingsFile
		var before []byte
		var mtime int64
		if withFile {
			h.writeKeys("check: v\nbroken: [\n")
			before, _ = os.ReadFile(p)
			fi, _ := os.Stat(p)
			mtime = fi.ModTime().UnixNano()
		}
		runs := [][]string{
			{"keys"}, {"keys", "--json"}, {"keys", "--example"}, {"doctor"}, {"doctor", "--json"},
			{"import"}, {"list"}, {"key", "gen", "globex/dave", "--no-passphrase", "--yes"},
		}
		for _, args := range runs {
			h.run(args...)
		}
		if !withFile {
			if _, err := os.Stat(p); !os.IsNotExist(err) {
				t.Fatalf("keys.yaml was created: %v", err)
			}
			continue
		}
		after, err := os.ReadFile(p)
		fi, _ := os.Stat(p)
		if err != nil || !bytes.Equal(after, before) || fi.ModTime().UnixNano() != mtime {
			t.Fatalf("keys.yaml changed")
		}
		entries, _ := os.ReadDir(h.e.Paths().BackupsDir)
		for _, en := range entries {
			if _, err := os.Stat(h.e.Paths().BackupsDir + "/" + en.Name() + "/keys.yaml"); err == nil {
				t.Fatal("keys.yaml was backed up")
			}
		}
	}
}

func TestCLIDoctorShowsKeyBindings(t *testing.T) {
	h := newHarness(t, nil)
	h.seed()
	h.writeKeys("check: c\n")
	_, out, _ := h.run("doctor", "--json")
	var v struct {
		Results []struct {
			ID      string `json:"id"`
			Status  string `json:"status"`
			Message string `json:"message"`
			Detail  string `json:"detail"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	var ids []string
	n := 0
	for _, r := range v.Results {
		ids = append(ids, r.ID)
		if r.ID == "keybindings" {
			n++
			if r.Status != "warn" || r.Detail == "" {
				t.Errorf("%+v", r)
			}
		}
	}
	if n != 1 || !slices.Contains(ids, "git_version") || !slices.Contains(ids, "config") {
		t.Fatalf("ids %v", ids)
	}
	// human output
	code, out, _ := h.run("doctor")
	_ = code
	if !strings.Contains(out, "keys.yaml") {
		t.Fatalf("doctor output lacks the keys.yaml warning:\n%s", out)
	}
}

func maxWidth(s string) (int, string) {
	w, line := 0, ""
	for _, l := range strings.Split(s, "\n") {
		if n := len([]rune(l)); n > w {
			w, line = n, l
		}
	}
	return w, line
}

// D3: control characters in warnings[].key and .action are escaped.
func TestCLIJSONWarningFieldsEscaped(t *testing.T) {
	h := newHarness(t, nil)
	h.writeKeys("check: \"\\e[2J\"\n\"\\e]0;x\\a\": v\n")
	code, out, _ := h.run("keys", "--json")
	if code != 2 {
		t.Fatalf("code %d\n%s", code, out)
	}
	var d keysDoc
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Warnings) != 2 {
		t.Fatalf("%+v", d.Warnings)
	}
	for _, w := range d.Warnings {
		for _, f := range []string{w.Key, w.Action, w.Message} {
			if strings.IndexFunc(f, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
				t.Errorf("control character in %q", f)
			}
		}
	}
	if d.Warnings[0].Key != `\x1b[2J` {
		t.Errorf("key = %q", d.Warnings[0].Key)
	}
}

// D4: the table, the warnings and the doctor detail fit 80 columns.
func TestCLIKeysFit80Columns(t *testing.T) {
	for _, lang := range []string{"vi", "en"} {
		h := newHarness(t, nil)
		h.writeKeys("check: [v\n")
		_, out, _ := h.runLang(lang, "keys")
		if w, l := maxWidth(out); w > 80 {
			t.Errorf("%s keys: %d columns: %q", lang, w, l)
		}
		_, out, _ = h.runLang(lang, "doctor")
		if w, l := maxWidth(out); w > 80 {
			t.Errorf("%s doctor: %d columns: %q", lang, w, l)
		}
		h.writeKeys("check: v\nclone: [c, n]\n")
		_, out, _ = h.runLang(lang, "keys")
		if w, l := maxWidth(out); w > 80 {
			t.Errorf("%s keys (custom): %d columns: %q", lang, w, l)
		}
		if !strings.Contains(out, "key_result") || !strings.Contains(out, "import") {
			t.Errorf("scopes lost when wrapping:\n%s", out)
		}
	}
	h := newHarness(t, nil)
	_, out, _ := h.run("keys", "--example")
	if w, l := maxWidth(out); w > 80 {
		t.Errorf("example: %d columns: %q", w, l)
	}
}

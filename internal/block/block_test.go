package block

import (
	"errors"
	"strings"
	"testing"
)

var mk = Markers{Start: "# >>> test >>>", End: "# <<< test <<<"}

const body = "[a]\n\tb = c\n"

func blk(eol string, term bool) string {
	s := "# >>> test >>>" + eol + "[a]" + eol + "\tb = c" + eol + "# <<< test <<<"
	if term {
		s += eol
	}
	return s
}

func TestB1EmptyEnd(t *testing.T) {
	got, err := Upsert("", body, mk, AtEnd)
	if err != nil || got != blk("\n", true) {
		t.Fatalf("%q %v", got, err)
	}
}

func TestB2NoTrailingNewline(t *testing.T) {
	in := "[user]\n\tname = x"
	got, err := Upsert(in, body, mk, AtEnd)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, in+"\n# >>> test >>>\n") {
		t.Fatalf("%q", got)
	}
	back, _ := Remove(got, mk)
	if back != in {
		t.Fatalf("round trip %q", back)
	}
}

func TestB3ReplaceInMiddle(t *testing.T) {
	pre, suf := "# top\n[x]\n\ty=1\n", "[z]\n\tq=2\n"
	in := pre + blk("\n", true) + suf
	got, err := Upsert(in, "[new]\n\tv = 1\n", mk, AtEnd)
	if err != nil {
		t.Fatal(err)
	}
	want := pre + "# >>> test >>>\n[new]\n\tv = 1\n# <<< test <<<\n" + suf
	if got != want {
		t.Fatalf("%q", got)
	}
}

func TestB4Idempotent(t *testing.T) {
	for _, place := range []Placement{AtEnd, AtTop} {
		for _, in := range []string{"", "a\n", "a", "a\r\nb\r\n", "# c\n[x]\n"} {
			once, err := Upsert(in, body, mk, place)
			if err != nil {
				t.Fatal(err)
			}
			twice, err := Upsert(once, body, mk, place)
			if err != nil || once != twice {
				t.Fatalf("not idempotent for %q: %q vs %q", in, once, twice)
			}
		}
	}
}

func TestB5RoundTrip(t *testing.T) {
	fixtures := []string{
		"", "\n", "a", "a\n", "a\n\n", "\n\na", "[user]\n\tname = x\n", "[user]\n\tname = x", "x\r\ny\r\n", "x\r\ny",
		"# comment only", "Host a\n  HostName b\n\nHost c\n  HostName d\n",
	}
	for _, place := range []Placement{AtEnd, AtTop} {
		for _, in := range fixtures {
			up, err := Upsert(in, body, mk, place)
			if err != nil {
				t.Fatalf("%q: %v", in, err)
			}
			back, err := Remove(up, mk)
			if err != nil || back != in {
				t.Fatalf("place %d fixture %q: upsert %q -> remove %q (%v)", place, in, up, back, err)
			}
		}
	}
}

func TestCorruptB6B7B8(t *testing.T) {
	cases := map[string]string{
		"two starts":   mk.Start + "\n" + mk.Start + "\n" + mk.End + "\n",
		"two ends":     mk.Start + "\n" + mk.End + "\n" + mk.End + "\n",
		"start only":   "a\n" + mk.Start + "\nb\n",
		"end only":     "a\n" + mk.End + "\n",
		"end<start":    mk.End + "\n" + mk.Start + "\n",
		"two complete": blk("\n", true) + blk("\n", true),
	}
	for name, in := range cases {
		if _, _, err := Find(in, mk); !errors.Is(err, ErrCorruptBlock) {
			t.Errorf("%s: Find err=%v", name, err)
		}
		if _, err := Upsert(in, body, mk, AtEnd); !errors.Is(err, ErrCorruptBlock) {
			t.Errorf("%s: Upsert err=%v", name, err)
		}
		if _, err := Remove(in, mk); !errors.Is(err, ErrCorruptBlock) {
			t.Errorf("%s: Remove err=%v", name, err)
		}
		if _, _, err := Body(in, mk); !errors.Is(err, ErrCorruptBlock) {
			t.Errorf("%s: Body err=%v", name, err)
		}
	}
}

func TestB9CRLF(t *testing.T) {
	in := "[user]\r\n\tname = x\r\n"
	got, err := Upsert(in, body, mk, AtEnd)
	if err != nil {
		t.Fatal(err)
	}
	if got != in+blk("\r\n", true) {
		t.Fatalf("%q", got)
	}
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatal("bare LF in CRLF file")
	}
	// replace keeps CRLF
	got2, _ := Upsert(got, "[n]\n", mk, AtEnd)
	if got2 != in+"# >>> test >>>\r\n[n]\r\n# <<< test <<<\r\n" {
		t.Fatalf("%q", got2)
	}
	back, _ := Remove(got, mk)
	if back != in {
		t.Fatal("crlf roundtrip")
	}
	// CRLF without trailing newline
	in2 := "a\r\nb"
	up, _ := Upsert(in2, body, mk, AtEnd)
	if back, _ := Remove(up, mk); back != in2 {
		t.Fatalf("%q", back)
	}
}

func TestB10Top(t *testing.T) {
	in := "# comment\n[user]\n\tname = x\n"
	got, err := Upsert(in, body, mk, AtTop)
	if err != nil || got != blk("\n", true)+in {
		t.Fatalf("%q %v", got, err)
	}
	// move from the bottom to the top, other content order unchanged
	low := in + blk("\n", true) + "[z]\n"
	moved, err := Upsert(low, body, mk, AtTop)
	if err != nil {
		t.Fatal(err)
	}
	if moved != blk("\n", true)+in+"[z]\n" {
		t.Fatalf("%q", moved)
	}
	// already on top: replaced in place
	again, _ := Upsert(moved, "[q]\n", mk, AtTop)
	if !strings.HasPrefix(again, "# >>> test >>>\n[q]\n# <<< test <<<\n# comment") {
		t.Fatalf("%q", again)
	}
	// moving an unterminated trailing block keeps original bytes
	un, _ := Upsert("a", body, mk, AtEnd)
	top, _ := Upsert(un, body, mk, AtTop)
	if top != blk("\n", true)+"a" {
		t.Fatalf("%q", top)
	}
}

func TestB11ExactLineMatch(t *testing.T) {
	in := "key = # >>> test >>>\n  # >>> test >>> \n# >>> test >>> extra\n"
	if _, found, err := Find(in, mk); found || err != nil {
		t.Fatalf("found=%v err=%v", found, err)
	}
	got, _ := Upsert(in, body, mk, AtEnd)
	if !strings.HasPrefix(got, in) {
		t.Fatal("user bytes changed")
	}
}

func TestEmptyBodyRemoves(t *testing.T) { // D16
	in := "a\n"
	up, _ := Upsert(in, body, mk, AtEnd)
	got, err := Upsert(up, "  \n", mk, AtEnd)
	if err != nil || got != in {
		t.Fatalf("%q %v", got, err)
	}
	// nothing to remove
	got, _ = Upsert(in, "", mk, AtEnd)
	if got != in {
		t.Fatal("changed")
	}
}

func TestBody(t *testing.T) {
	up, _ := Upsert("x\n", body, mk, AtEnd)
	b, ok, err := Body(up, mk)
	if err != nil || !ok || b != body {
		t.Fatalf("%q %v %v", b, ok, err)
	}
	if _, ok, _ := Body("x\n", mk); ok {
		t.Fatal("no block")
	}
	empty := mk.Start + "\n" + mk.End + "\n"
	if b, ok, _ := Body(empty, mk); !ok || b != "" {
		t.Fatal("empty block body")
	}
	if got := render(mk, "", "\n", true); got != empty {
		t.Fatalf("render empty: %q", got)
	}
}

func TestEOL(t *testing.T) {
	if EOL("a\nb") != "\n" || EOL("a\r\nb") != "\r\n" {
		t.Fatal("EOL")
	}
}

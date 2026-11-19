package online

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// O-1: a Secret prints *** with every verb, alone, in a struct and in JSON.
func TestO1SecretNeverPrints(t *testing.T) {
	s := secret()
	type holder struct {
		Tok Token
		sec Secret // unexported field: reflection must not leak the string
		P   *Secret
	}
	h := holder{Tok: Token{Secret: s, Source: SourceEnv}, sec: s, P: &s}
	for _, f := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d", "%T-%v"} {
		for _, v := range []any{s, &s, h, &h, []Secret{s}, map[string]Secret{"k": s}, Token{Secret: s}} {
			out := fmt.Sprintf(f, v)
			if strings.Contains(out, fixtureToken) {
				t.Errorf("%s of %T leaks: %s", f, v, out)
			}
		}
	}
	if got := fmt.Sprintf("%v %s %q", s, s, s); got != "*** *** ***" {
		t.Errorf("got %q", got)
	}
	b, err := json.Marshal(h)
	if err != nil || strings.Contains(string(b), fixtureToken) || !strings.Contains(string(b), `"***"`) {
		t.Errorf("json %s %v", b, err)
	}
	if txt, _ := s.MarshalText(); string(txt) != "***" {
		t.Errorf("text %q", txt)
	}
	if s.reveal() != fixtureToken || s.IsZero() || !(Secret{}).IsZero() || !NewSecret("").IsZero() {
		t.Error("reveal / IsZero")
	}
	if (Secret{}).reveal() != "" {
		t.Error("zero reveal")
	}
}

// O-2: RedactSecret replaces the token everywhere; an empty token is a no-op.
func TestO2RedactSecret(t *testing.T) {
	in := "a " + fixtureToken + " b " + fixtureToken
	if got := RedactSecret(in, secret()); got != "a *** b ***" {
		t.Errorf("got %q", got)
	}
	if got := RedactSecret(in, Secret{}); got != in {
		t.Errorf("empty token changed the text: %q", got)
	}
}

// O-3 / SEC-4: no Error() holds the token, even when the API message echoes it.
func TestO3ErrorsHoldNoToken(t *testing.T) {
	kinds := []Kind{KindAuth, KindScope, KindSSO, KindOTP, KindRateLimit, KindNetwork, KindDNS, KindTimeout, KindTLS, KindProxy,
		KindServer, KindBadResponse, KindWrongUser, KindUnsupported, KindInsecureHost, KindCancelled}
	for _, k := range kinds {
		e := newError(k, 401, "bad credentials "+fixtureToken+" \x1b[31m", secret())
		if strings.Contains(e.Error(), fixtureToken) || strings.Contains(e.Detail, "\x1b") {
			t.Errorf("%s: %q", k, e.Error())
		}
		if !strings.Contains(e.Error(), string(k)) {
			t.Errorf("%s not in %q", k, e.Error())
		}
	}
	// a transport error that echoes the token is redacted too
	e := classify(errors.New("dial "+fixtureToken), secret())
	if strings.Contains(e.Error(), fixtureToken) {
		t.Errorf("transport error leaks: %q", e.Error())
	}
}

// O-4: Sanitize.
func TestO4Sanitize(t *testing.T) {
	cases := map[string]string{
		"\x1b[31mred\x1b[0m":          "red",
		"bell\x07":                    "bell",
		"c1\u009b31mx":                "c1x",
		"line\r\nbreak":               "line  break",
		"osc\x1b]0;pwn\x07done":       "oscdone",
		"osc\x1b]0;pwn\x1b\\done":     "oscdone",
		"tab\there":                   "tab here",
		"bidi‮evil":                   "bidievil",
		"zero​width":                  "zerowidth",
		"Tiếng Việt: Công ty Đêm":     "Tiếng Việt: Công ty Đêm",
		"bad\xffbyte":                 "bad�byte",
		"\x7fdel":                     "del",
		"lone esc at end \x1b":        "lone esc at end ",
		"unterminated \x1b[31;1":      "unterminated ",
		"unterminated osc \x1b]0;abc": "unterminated osc ",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("ế", 300)
	if got := []rune(Sanitize(long)); len(got) != MaxDetailRunes {
		t.Errorf("length %d", len(got))
	}
}

// O-6..O-8: classification of transport errors.
func TestO6ClassifyTransportErrors(t *testing.T) {
	dnsErr := &net.DNSError{Err: "no such host", Name: "x.example", IsNotFound: true}
	cases := []struct {
		name string
		err  error
		want Kind
	}{
		{"dns", fmt.Errorf("Get: %w", dnsErr), KindDNS},
		{"dns timeout", &net.DNSError{Err: "i/o timeout", IsTimeout: true}, KindTimeout},
		{"x509 unknown authority", fmt.Errorf("Get: %w", x509.UnknownAuthorityError{}), KindTLS},
		{"x509 hostname", fmt.Errorf("Get: %w", x509.HostnameError{Host: "h"}), KindTLS},
		{"x509 invalid", fmt.Errorf("Get: %w", x509.CertificateInvalidError{}), KindTLS},
		{"canceled", fmt.Errorf("Get: %w", context.Canceled), KindCancelled},
		{"deadline", fmt.Errorf("Get: %w", context.DeadlineExceeded), KindTimeout},
		{"proxy", &net.OpError{Op: "proxyconnect", Net: "tcp", Err: errors.New("refused")}, KindProxy},
		{"refused", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}, KindNetwork},
		{"os deadline", os.ErrDeadlineExceeded, KindTimeout},
		{"redirect", &redirectError{"other host"}, KindBadResponse},
		{"other", errors.New("boom"), KindNetwork},
	}
	for _, c := range cases {
		if got := classify(c.err, secret()); got.Kind != c.want {
			t.Errorf("%s: kind %s, want %s", c.name, got.Kind, c.want)
		}
	}
	if !errors.Is(classify(context.Canceled, secret()), context.Canceled) {
		t.Error("the cause is not unwrapped")
	}
	// an *Error passes through
	pre := newError(KindWrongUser, 0, "", secret())
	if classify(pre, secret()) != pre {
		t.Error("an *Error was wrapped")
	}
}

// the Timeout kind for a net timeout error
func TestClassifyNetTimeout(t *testing.T) {
	if k := classify(timeoutErr{}, secret()).Kind; k != KindTimeout {
		t.Errorf("kind %s", k)
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// SEC-12: the online package never writes a file.
func TestSEC12NoWrites(t *testing.T) {
	re := regexp.MustCompile(`os\.(WriteFile|Create|OpenFile|Mkdir|MkdirAll|Rename|Remove|RemoveAll)\b|fsx\.`)
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		if m := re.FindString(string(b)); m != "" {
			t.Errorf("%s uses %s", f, m)
		}
	}
}

// the secret is read back in two places only: the Authorization header and
// the helpers of secret.go (checklist 11.1).
func TestRevealCallSites(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	var sites []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		for n, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, ".reveal()") && !strings.HasPrefix(strings.TrimSpace(line), "//") {
				sites = append(sites, fmt.Sprintf("%s:%d", f, n+1))
			}
		}
	}
	var outside []string
	for _, s := range sites {
		if !strings.HasPrefix(s, "secret.go") && !strings.HasPrefix(s, "client.go") {
			outside = append(outside, s)
		}
	}
	if len(outside) > 0 {
		t.Errorf("reveal() outside secret.go and client.go: %v", outside)
	}
	n := 0
	for _, s := range sites {
		if strings.HasPrefix(s, "client.go") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("client.go reveals the token at %d places, want 1 (the Authorization header)", n)
	}
}

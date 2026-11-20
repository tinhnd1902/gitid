package online

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tinhnd1902/gitid/internal/platform"
	"github.com/tinhnd1902/gitid/internal/testutil"
)

func repoPages(n int) [][]map[string]any {
	var out [][]map[string]any
	for p := 0; p < n; p++ {
		out = append(out, []map[string]any{ghRepoJSON("acme", fmt.Sprintf("r%d", p), nil)})
	}
	return out
}

func kindOf(err error) Kind {
	var oe *Error
	if errors.As(err, &oe) {
		return oe.Kind
	}
	return ""
}

// O-12: three pages through Link, onPage in order.
func TestO12Pagination(t *testing.T) {
	srv := ghServer(t, "alice", repoPages(3), nil)
	c, _ := srv.client()
	_, repos, pages, err := listAll(t, c, ghAccount(), tokenOf(SourceGH))
	if err != nil || len(pages) != 3 || len(repos) != 3 {
		t.Fatalf("%v %d", err, len(pages))
	}
	if n := len(srv.requests()); n != 4 { // /user + 3 pages
		t.Errorf("%d requests", n)
	}
	if repos[0].Name != "r0" || repos[2].Name != "r2" {
		t.Errorf("order %+v", repos)
	}
}

func linkServer(t *testing.T, next string) *api {
	return newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/user") {
			writeJSON(w, 200, map[string]any{"login": "alice"})
			return
		}
		w.Header().Set("Link", `<`+next+`>; rel="next"`)
		writeJSON(w, 200, []map[string]any{ghRepoJSON("a", "b", nil)})
	})
}

// O-13 / O-14: a next link off the host, or without https, ends the walk and
// is never requested.
func TestO13And14NextLinkPinning(t *testing.T) {
	for _, next := range []string{
		"https://evil.example/user/repos?page=2",
		"http://api.github.com/user/repos?page=2",
		"https://user:pw@api.github.com/user/repos?page=2",
		"https://api.github.com.evil.example/user/repos",
	} {
		srv := linkServer(t, next)
		c, _ := srv.client()
		_, repos, _, err := listAll(t, c, ghAccount(), tokenOf(SourceGH))
		if kindOf(err) != KindBadResponse {
			t.Errorf("%s: %v", next, err)
		}
		if len(repos) != 1 {
			t.Errorf("%s: the first page was lost (%d)", next, len(repos))
		}
		for _, h := range srv.rt.Hosts() {
			if h != "api.github.com" {
				t.Errorf("%s: a request went to %s", next, h)
			}
		}
		if n := len(srv.requests()); n != 2 {
			t.Errorf("%s: %d requests", next, n)
		}
	}
	// a relative link stays on the host
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/user"):
			writeJSON(w, 200, map[string]any{"login": "alice"})
		case r.URL.Query().Get("page") == "2":
			writeJSON(w, 200, []map[string]any{ghRepoJSON("a", "c", nil)})
		default:
			w.Header().Set("Link", `</user/repos?page=2>; rel="next"`)
			writeJSON(w, 200, []map[string]any{ghRepoJSON("a", "b", nil)})
		}
	})
	c, _ := srv.client()
	_, repos, _, err := listAll(t, c, ghAccount(), tokenOf(SourceGH))
	if err != nil || len(repos) != 2 {
		t.Errorf("relative link: %v %d", err, len(repos))
	}
}

// O-15 / O-16 / SEC-10: redirects.
func TestO15And16Redirects(t *testing.T) {
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/user" && r.URL.Query().Get("r") == "":
			http.Redirect(w, r, "https://evil.example/steal", http.StatusFound)
		default:
			writeJSON(w, 200, map[string]any{"login": "alice"})
		}
	})
	c, _ := srv.client()
	_, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceGH))
	if kindOf(err) != KindBadResponse {
		t.Fatalf("cross-host redirect: %v", err)
	}
	for _, h := range srv.rt.Hosts() {
		if h == "evil.example" {
			t.Error("the redirect target was requested")
		}
	}
	for _, r := range srv.requests() {
		if r.Host != "api.github.com" {
			t.Errorf("a request reached %s", r.Host)
		}
	}
	// an http redirect on the same host is refused as well
	srv = newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://api.github.com/user", http.StatusMovedPermanently)
	})
	c, _ = srv.client()
	if _, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceGH)); kindOf(err) != KindBadResponse {
		t.Errorf("downgrade redirect: %v", err)
	}
	// O-16: the same host over https is followed
	srv = newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/user" && r.URL.Query().Get("r") == "":
			http.Redirect(w, r, "https://api.github.com/user?r=1", http.StatusMovedPermanently)
		case r.URL.Path == "/user":
			writeJSON(w, 200, map[string]any{"login": "alice"})
		default:
			writeJSON(w, 200, []map[string]any{})
		}
	})
	c, _ = srv.client()
	if _, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceGH)); err != nil {
		t.Errorf("same-host redirect: %v", err)
	}
	for _, r := range srv.requests() {
		if r.Auth == "" {
			t.Error("the redirected request lost its Authorization")
		}
	}
}

// O-17: the page cap truncates.
func TestO17PageCap(t *testing.T) {
	srv := ghServer(t, "alice", repoPages(5), nil)
	c, _ := srv.client()
	c.MaxPages = 2
	res, repos, _, err := listAll(t, c, ghAccount(), tokenOf(SourceGH))
	if err != nil || !res.Truncated || len(repos) != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	if n := len(srv.requests()); n != 3 {
		t.Errorf("%d requests", n)
	}
	// the cap itself is bounded
	c.MaxPages = 100000
	if c.maxPages() != MaxPagesLimit {
		t.Errorf("max pages %d", c.maxPages())
	}
	c.MaxPages = 0
	if c.maxPages() != DefaultMaxPages {
		t.Errorf("default max pages %d", c.maxPages())
	}
	// a list that ends exactly at the cap is not truncated
	srv = ghServer(t, "alice", repoPages(2), nil)
	c, _ = srv.client()
	c.MaxPages = 2
	if res, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceGH)); err != nil || res.Truncated {
		t.Errorf("%+v %v", res, err)
	}
}

// O-18: a body over the limit is refused.
func TestO18BodyLimit(t *testing.T) {
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"login":"alice","pad":"`))
		_, _ = w.Write([]byte(strings.Repeat("x", 9<<20)))
		_, _ = w.Write([]byte(`"}`))
	})
	c, _ := srv.client()
	_, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceGH))
	if kindOf(err) != KindBadResponse {
		t.Fatalf("%v", err)
	}
}

// O-19: HTML (a captive portal, Azure's sign-in page) is not JSON.
func TestO19NotJSON(t *testing.T) {
	for _, ct := range []string{"text/html", "", "text/plain; charset=utf-8"} {
		ct := ct
		srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
			if ct != "" {
				w.Header().Set("Content-Type", ct)
			}
			_, _ = w.Write([]byte(`<html>sign in</html>`))
		})
		c, _ := srv.client()
		_, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceGH))
		if kindOf(err) != KindBadResponse {
			t.Errorf("%q: %v", ct, err)
		}
	}
	// a 401 page in HTML is still an auth failure
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`<html>nope</html>`))
	})
	c, _ := srv.client()
	if _, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceGH)); kindOf(err) != KindAuth {
		t.Errorf("html 401: %v", err)
	}
}

// O-20: 401 keeps a sanitized message.
func TestO20Auth(t *testing.T) {
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 401, map[string]any{"message": "Bad credentials \x1b[31m" + fixtureToken})
	})
	c, _ := srv.client()
	_, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceGH))
	var oe *Error
	if !errors.As(err, &oe) || oe.Kind != KindAuth || oe.Detail != "Bad credentials ***" {
		t.Fatalf("%+v", oe)
	}
}

// rateServer answers the first n requests of /user with a rate limit.
func rateServer(t *testing.T, n int32, set func(h http.Header)) (*api, *int32) {
	var hits int32
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/user") {
			if atomic.AddInt32(&hits, 1) <= n {
				set(w.Header())
				writeJSON(w, 429, map[string]any{"message": "rate limited"})
				return
			}
			writeJSON(w, 200, map[string]any{"login": "alice"})
			return
		}
		writeJSON(w, 200, []map[string]any{})
	})
	return srv, &hits
}

// O-21: a short Retry-After is waited out once.
func TestO21RetryAfterShort(t *testing.T) {
	srv, hits := rateServer(t, 1, func(h http.Header) { h.Set("Retry-After", "2") })
	c, slept := srv.client()
	if _, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceGH)); err != nil {
		t.Fatal(err)
	}
	if len(*slept) != 1 || (*slept)[0] != 2*time.Second || atomic.LoadInt32(hits) != 2 {
		t.Errorf("slept %v, hits %d", *slept, *hits)
	}
	// only once: a second 429 is reported
	srv, _ = rateServer(t, 5, func(h http.Header) { h.Set("Retry-After", "1") })
	c, slept = srv.client()
	_, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceGH))
	if kindOf(err) != KindRateLimit || len(*slept) != 1 {
		t.Errorf("%v %v", err, *slept)
	}
}

// O-22: a long Retry-After is reported, not slept.
func TestO22RetryAfterLong(t *testing.T) {
	srv, _ := rateServer(t, 5, func(h http.Header) { h.Set("Retry-After", "120") })
	c, slept := srv.client()
	_, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceGH))
	var oe *Error
	if !errors.As(err, &oe) || oe.Kind != KindRateLimit || len(*slept) != 0 {
		t.Fatalf("%v %v", err, *slept)
	}
	if !oe.Reset.Equal(c.Now().Add(120 * time.Second)) {
		t.Errorf("reset %v", oe.Reset)
	}
}

// O-23: 403 with an exhausted quota; pages fetched before are kept.
func TestO23ExhaustedQuotaKeepsPages(t *testing.T) {
	reset := c0().Add(30 * time.Minute).Unix()
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/user"):
			writeJSON(w, 200, map[string]any{"login": "alice"})
		case r.URL.Query().Get("page") == "2":
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", fmt.Sprint(reset))
			writeJSON(w, 403, map[string]any{"message": "API rate limit exceeded"})
		default:
			w.Header().Set("Link", fmt.Sprintf(`<https://%s%s?page=2>; rel="next"`, r.Host, r.URL.Path))
			writeJSON(w, 200, []map[string]any{ghRepoJSON("a", "first", nil)})
		}
	})
	c, slept := srv.client()
	res, repos, _, err := listAll(t, c, ghAccount(), tokenOf(SourceGH))
	var oe *Error
	if !errors.As(err, &oe) || oe.Kind != KindRateLimit || oe.Page != 2 {
		t.Fatalf("%+v", oe)
	}
	if len(repos) != 1 || res.Count != 1 || len(*slept) != 0 {
		t.Errorf("repos %d slept %v", len(repos), *slept)
	}
	if oe.Reset.Unix() != reset {
		t.Errorf("reset %v", oe.Reset)
	}
}

func c0() time.Time { return testutil.FixedTime }

// O-24: a cancel during the wait is Cancelled.
func TestO24RetryRespectsContext(t *testing.T) {
	srv, _ := rateServer(t, 5, func(h http.Header) { h.Set("Retry-After", "5") })
	c, _ := srv.client()
	ctx, cancel := context.WithCancel(context.Background())
	c.Sleep = func(ctx context.Context, d time.Duration) error { cancel(); return ctx.Err() }
	_, err := c.List(ctx, ghAccount(), tokenOf(SourceGH), nil)
	if kindOf(err) != KindCancelled {
		t.Fatalf("%v", err)
	}
}

// the default Sleep is context aware
func TestSleepCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("%v", err)
	}
	if err := sleepCtx(context.Background(), time.Millisecond); err != nil {
		t.Errorf("%v", err)
	}
}

// O-25: 5xx.
func TestO25ServerError(t *testing.T) {
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	c, _ := srv.client()
	_, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceGH))
	if kindOf(err) != KindServer {
		t.Fatalf("%v", err)
	}
}

// O-26: a slow answer is a timeout.
func TestO26Timeout(t *testing.T) {
	release := make(chan struct{})
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	c, _ := srv.client()
	c.HTTP.Timeout = 50 * time.Millisecond
	_, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceGH))
	if kindOf(err) != KindTimeout {
		t.Fatalf("%v", err)
	}
}

// the caller's cancel stops a request at once
func TestCancelDuringRequest(t *testing.T) {
	started := make(chan struct{})
	var once atomic.Bool
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if once.CompareAndSwap(false, true) {
			close(started)
		}
		<-r.Context().Done()
	})
	c, _ := srv.client()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := c.List(ctx, ghAccount(), tokenOf(SourceGH), nil); done <- err }()
	<-started
	cancel()
	select {
	case err := <-done:
		if kindOf(err) != KindCancelled {
			t.Errorf("%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("List did not return after the cancel")
	}
}

// O-27: a host that would need plain http, or is not a plain host.
func TestO27InsecureHost(t *testing.T) {
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("a request was made") })
	c, _ := srv.client()
	for host, kind := range map[string]Kind{
		"http://ghe.example.com":  KindInsecureHost,
		"HTTP://ghe.example.com":  KindInsecureHost,
		"https://ghe.example.com": KindUnsupported,
		"ghe.example.com/path":    KindUnsupported,
		"user@ghe.example.com":    KindUnsupported,
		"ghe example":             KindUnsupported,
		"":                        KindUnsupported,
	} {
		for _, p := range []platform.Platform{platform.GitHub, platform.GitLab} {
			a := ghAccount()
			a.Platform, a.Host = p, host
			_, err := c.List(context.Background(), a, tokenOf(SourceGH), nil)
			if kindOf(err) != kind {
				t.Errorf("%s %q: %v", p, host, err)
			}
		}
	}
	// Bitbucket and Azure DevOps have one cloud host each; another host is
	// refused before any request
	a := ghAccount()
	a.Platform = platform.Bitbucket
	if _, err := c.List(context.Background(), a, tokenOf(SourceGH), nil); kindOf(err) != KindUnsupported {
		t.Errorf("bitbucket: %v", err)
	}
	// every platform of gitid has a listing (Phase C)
	for _, p := range platform.All() {
		if !Supported(p) {
			t.Errorf("Supported(%s)", p)
		}
	}
	if Supported(platform.Platform("svn")) {
		t.Error("Supported(svn)")
	}
}

// realServer is a TLS server on 127.0.0.1 for tests of the real transport.
func realServer(t *testing.T) (*httptest.Server, Account) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/user"):
			writeJSON(w, 200, map[string]any{"login": "alice"})
		default:
			writeJSON(w, 200, []map[string]any{ghRepoJSON("a", "b", nil)})
		}
	}))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0) // an untrusted client aborts the handshake on purpose
	srv.StartTLS()
	t.Cleanup(srv.Close)
	a := ghAccount()
	a.Host = srv.Listener.Addr().String() // 127.0.0.1:port, a GHE style host
	return srv, a
}

// O-TLS-1: the real transport does not trust an unknown CA.
func TestOTLS1UntrustedCA(t *testing.T) {
	_, a := realServer(t)
	e := testutil.TempEnv(t)
	e.HTTP = nil
	c, err := NewClient(e)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.List(context.Background(), a, tokenOf(SourceGH), nil)
	if kindOf(err) != KindTLS {
		t.Fatalf("%v", err)
	}
}

// O-TLS-2: SSL_CERT_FILE adds the CA. (Go on macOS ignores the variable by
// itself; NewClient loads the file.)
func TestOTLS2SSLCertFile(t *testing.T) {
	srv, a := realServer(t)
	pemPath := filepath.Join(t.TempDir(), "ca.pem")
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(pemPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	e := testutil.MapEnv(testutil.TempEnv(t), map[string]string{"SSL_CERT_FILE": pemPath})
	e.HTTP = nil
	c, err := NewClient(e)
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.List(context.Background(), a, tokenOf(SourceGH), nil)
	if err != nil || res.Count != 1 {
		t.Fatalf("%s: %v", runtime.GOOS, err)
	}
	// a missing or broken file is an error of NewClient, without the path
	for _, p := range []string{filepath.Join(t.TempDir(), "missing.pem"), func() string {
		f := filepath.Join(t.TempDir(), "bad.pem")
		_ = os.WriteFile(f, []byte("not a certificate"), 0o600)
		return f
	}()} {
		e := testutil.MapEnv(testutil.TempEnv(t), map[string]string{"SSL_CERT_FILE": p})
		e.HTTP = nil
		_, err := NewClient(e)
		if kindOf(err) != KindTLS || strings.Contains(err.Error(), p) {
			t.Errorf("%q: %v", p, err)
		}
	}
}

// O-TLS-3 / O-PX-1: TLS 1.2 or newer, verification on, the proxy from the
// environment.
func TestOTLS3AndPX1Transport(t *testing.T) {
	e := testutil.TempEnv(t)
	e.HTTP = nil
	c, err := NewClient(e)
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := c.HTTP.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport %T", c.HTTP.Transport)
	}
	cfg := tr.TLSClientConfig
	if cfg == nil || cfg.MinVersion < tls.VersionTLS12 || cfg.InsecureSkipVerify {
		t.Errorf("tls config %+v", cfg)
	}
	name := func(f any) string { return runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name() }
	if tr.Proxy == nil || name(tr.Proxy) != name(http.ProxyFromEnvironment) {
		t.Error("the transport does not use ProxyFromEnvironment")
	}
	if tr.TLSHandshakeTimeout == 0 || tr.ResponseHeaderTimeout == 0 || c.HTTP.Timeout != RequestTimeout {
		t.Error("timeouts are not set")
	}
	if c.HTTP.CheckRedirect == nil {
		t.Error("no redirect check")
	}
}

// a proxy that refuses connections is a Proxy error
func TestProxyFailure(t *testing.T) {
	e := testutil.TempEnv(t)
	e.HTTP = nil
	c, err := NewClient(e)
	if err != nil {
		t.Fatal(err)
	}
	tr := c.HTTP.Transport.(*http.Transport)
	tr.Proxy = func(*http.Request) (*url.URL, error) { return url.Parse("http://127.0.0.1:1") }
	a := ghAccount()
	a.Host = "ghe.example.com"
	_, err = c.List(context.Background(), a, tokenOf(SourceGH), nil)
	if kindOf(err) != KindProxy {
		t.Fatalf("%v", err)
	}
}

// SEC-6: the Authorization header goes to the account's API host only.
func TestSEC6AuthOnlyToTheAPIHost(t *testing.T) {
	srv := ghServer(t, "alice", repoPages(3), nil)
	c, _ := srv.client()
	if _, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceGH)); err != nil {
		t.Fatal(err)
	}
	for _, h := range srv.rt.Hosts() {
		if h != "api.github.com" {
			t.Errorf("request to %s", h)
		}
	}
}

// SEC-9: no non-test source file turns certificate verification off.
func TestSEC9NoInsecureSkipVerify(t *testing.T) {
	root := filepath.Join("..", "..")
	bad := "Insecure" + "SkipVerify" // spelled in two parts so this file does not match itself
	var hits []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, _ := os.ReadFile(p)
		if strings.Contains(string(b), bad) {
			hits = append(hits, p)
		}
		return nil
	})
	if len(hits) > 0 {
		t.Errorf("%s in %v", bad, hits)
	}
}

// Transport is e.HTTP when a test set it, else the real transport with the TLS
// floor.
func TestTransportExport(t *testing.T) {
	e := testutil.TempEnv(t)
	rt, err := Transport(e)
	if err != nil || rt != e.HTTP {
		t.Fatalf("with e.HTTP set: %v, %v", rt, err)
	}
	e.HTTP = nil
	rt, err = Transport(e)
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := rt.(*http.Transport)
	if !ok || tr.TLSClientConfig == nil || tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("real transport %T", rt)
	}
}

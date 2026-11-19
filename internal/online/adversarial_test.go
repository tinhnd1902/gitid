package online

// Adversarial tests (B2): a hostile or broken platform, and every way a token
// could end up in an output. The fixture token (fixtureToken) must appear in
// no error text, no repository, no log line and no panic message. Everything
// runs against httptest servers; there is no network, no real HOME and no
// subprocess.

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tinhnd1902/gitid/internal/platform"
)

// dump prints v the ways a program prints errors and values.
func dump(v any) string {
	var b strings.Builder
	for _, f := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d", "%T"} {
		fmt.Fprintf(&b, f+"\n", v)
	}
	return b.String()
}

// noTokenIn fails when the fixture token (or a 6-character prefix of it, which
// is what a cut in the middle of a token leaves) is in anything printed from v.
// For an error it also walks the wrapped errors and prints every *Error.
func noTokenIn(t *testing.T, what string, v any) {
	t.Helper()
	check := func(label, s string) {
		t.Helper()
		if strings.Contains(s, fixtureToken) || strings.Contains(s, fixtureToken[:8]) {
			t.Errorf("%s: the token (or a prefix) is in %s:\n%s", what, label, s)
		}
	}
	check("the printed value", dump(v))
	if err, ok := v.(error); ok {
		check("Error()", err.Error())
		check("a wrapped error", dump(fmt.Errorf("while listing: %w", err)))
		check("errors.Join", dump(errors.Join(err, io.EOF)))
		for e := err; e != nil; e = errors.Unwrap(e) {
			check("the chain", dump(e))
		}
		var oe *Error
		if errors.As(err, &oe) {
			check("the Error struct", dump(*oe))
			check("Detail/SSOURL/Login", oe.Detail+"\n"+oe.SSOURL+"\n"+oe.Login)
		}
	}
}

// listErr lists with the fixture token and returns the repositories and the error.
func listErr(t *testing.T, srv *api, a Account) (*Client, Result, []Repo, error) {
	t.Helper()
	c, _ := srv.client()
	res, repos, _, err := listAll(t, c, a, tokenOf(SourceEnv))
	return c, res, repos, err
}

// A server that echoes the token in its error body and in its response
// headers, including Location, in every failure status.
func TestAdvServerEchoesToken(t *testing.T) {
	for _, st := range []int{400, 401, 403, 404, 422, 429, 500, 502, 503} {
		st := st
		t.Run(fmt.Sprint(st), func(t *testing.T) {
			srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
				h := w.Header()
				h.Set("X-Echo", fixtureToken)
				h.Set("Location", "http://github.com/login?access_token="+fixtureToken)
				h.Set("WWW-Authenticate", `Bearer realm="`+fixtureToken+`"`)
				h.Set("X-GitHub-SSO", "required; url=https://github.com/orgs/x/sso?t="+fixtureToken)
				h.Set("Link", "<https://evil.example/?t="+fixtureToken+`>; rel="next"`)
				h.Set("Retry-After", "7200")
				writeJSON(w, st, map[string]any{
					"message":           "bad " + fixtureToken + " here",
					"error":             fixtureToken,
					"error_description": "token " + fixtureToken,
					"documentation_url": "https://x.example/" + fixtureToken,
				})
			})
			_, _, _, err := listErr(t, srv, ghAccount())
			if err == nil {
				t.Fatal("want an error")
			}
			noTokenIn(t, fmt.Sprint(st), err)
			for _, r := range srv.requests() {
				if r.Host != "api.github.com" {
					t.Errorf("a request went to %q", r.Host)
				}
			}
		})
	}
}

// The token echoed where the client takes text from a successful answer: the
// login, the repository names, the paths of a GitLab project.
func TestAdvServerEchoesTokenInData(t *testing.T) {
	t.Run("github wrong user", func(t *testing.T) {
		srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, map[string]any{"login": fixtureToken})
		})
		_, res, _, err := listErr(t, srv, ghAccount())
		if kindOf(err) != KindWrongUser {
			t.Fatalf("%v", err)
		}
		noTokenIn(t, "login", err)
		noTokenIn(t, "result", res)
	})
	t.Run("github names", func(t *testing.T) {
		srv := ghServer(t, "alice", [][]map[string]any{{
			ghRepoJSON(fixtureToken, "r", nil),
			ghRepoJSON("acme", fixtureToken, nil),
			ghRepoJSON("acme", "ok", nil),
		}}, nil)
		_, res, repos, err := listErr(t, srv, ghAccount())
		if err != nil {
			t.Fatal(err)
		}
		noTokenIn(t, "repos", repos)
		noTokenIn(t, "result", res)
		for _, r := range repos {
			if strings.Contains(r.FullPath, "***") && r.CloneURL != "" {
				t.Errorf("a redacted name got a clone URL: %+v", r)
			}
		}
	})
	t.Run("gitlab login and paths", func(t *testing.T) {
		srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/user") {
				writeJSON(w, 200, map[string]any{"username": "dave"})
				return
			}
			writeJSON(w, 200, []map[string]any{
				{"path_with_namespace": "g/" + fixtureToken, "visibility": "private"},
				{"path_with_namespace": fixtureToken + "/p", "visibility": "private"},
			})
		})
		_, res, repos, err := listErr(t, srv, glAccount())
		if err != nil {
			t.Fatal(err)
		}
		noTokenIn(t, "repos", repos)
		noTokenIn(t, "result", res)
	})
	t.Run("gitlab wrong user", func(t *testing.T) {
		srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, map[string]any{"username": fixtureToken})
		})
		_, res, _, err := listErr(t, srv, glAccount())
		if kindOf(err) != KindWrongUser {
			t.Fatalf("%v", err)
		}
		noTokenIn(t, "login", err)
		noTokenIn(t, "result", res)
	})
}

// DEF-1: a token that straddles the 200-rune cut leaves no prefix.
func TestAdvTokenAcrossTheCut(t *testing.T) {
	for _, pad := range []int{0, 1, 150, 185, 190, 193, 195, 199, 200, 201, 250} {
		msg := strings.Repeat("a", pad) + fixtureToken + " tail"
		e := newError(KindAuth, 401, msg, secret())
		noTokenIn(t, fmt.Sprintf("pad %d", pad), e)
		if n := len([]rune(e.Detail)); n > MaxDetailRunes {
			t.Errorf("pad %d: %d runes", pad, n)
		}
	}
	// multi-byte padding: the cut is counted in runes, not bytes
	msg := strings.Repeat("ế", 190) + fixtureToken
	noTokenIn(t, "runes", newError(KindAuth, 401, msg, secret()))
	// a token with an escape sequence inside it is joined by the cleaning
	msg = strings.Repeat("b", 185) + "tok_SECRET\x1b[0m_9f3c" + "zzz"
	noTokenIn(t, "joined", newError(KindAuth, 401, msg, secret()))

	// through a real answer
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 401, map[string]any{"message": strings.Repeat("x", 190) + fixtureToken})
	})
	_, _, _, err := listErr(t, srv, ghAccount())
	if kindOf(err) != KindAuth {
		t.Fatalf("%v", err)
	}
	noTokenIn(t, "answer", err)

	// Sanitize itself still cuts (the cut is not what newError relies on)
	if got := Sanitize(strings.Repeat("a", 300)); len([]rune(got)) != MaxDetailRunes {
		t.Errorf("Sanitize cut to %d", len([]rune(got)))
	}
}

// A redirect to http:// is refused and nothing is sent there.
func TestAdvRedirectToHTTP(t *testing.T) {
	for _, loc := range []string{
		"http://api.github.com/user?t=" + fixtureToken,
		"http://evil.example/x",
		"https://evil.example/x",
		"//evil.example/x",
		"ftp://api.github.com/x",
	} {
		loc := loc
		srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, loc, http.StatusFound)
		})
		c, _ := srv.client()
		_, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceEnv))
		if kindOf(err) != KindBadResponse {
			t.Errorf("%s: %v", loc, err)
			continue
		}
		noTokenIn(t, loc, err)
		if n := len(srv.requests()); n != 1 {
			t.Errorf("%s: %d requests (a redirect was followed)", loc, n)
		}
		for _, h := range srv.rt.Hosts() {
			if h != "api.github.com" {
				t.Errorf("%s: a request went to %q", loc, h)
			}
		}
	}
}

// A Link "next" that points to another host, plain http, a look-alike host or
// carries credentials is never requested; the pages before it are kept.
func TestAdvLinkNextOtherHost(t *testing.T) {
	for _, next := range []string{
		"https://evil.example/user/repos?page=2",
		"https://github.com/user/repos?page=2", // the API host is api.github.com
		"https://api.github.com.evil.example/user/repos?page=2",
		"http://api.github.com/user/repos?page=2",
		"https://user:pw@api.github.com/user/repos?page=2",
		"https://api.github.com:8443/user/repos?page=2",
		"//evil.example/user/repos?page=2",
		"https://evil.example/?t=" + fixtureToken,
	} {
		next := next
		srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/user") {
				writeJSON(w, 200, map[string]any{"login": "alice"})
				return
			}
			w.Header().Set("Link", "<"+next+`>; rel="next"`)
			writeJSON(w, 200, []map[string]any{ghRepoJSON("acme", "first", nil)})
		})
		_, res, repos, err := listErr(t, srv, ghAccount())
		if kindOf(err) != KindBadResponse {
			t.Errorf("%s: %v", next, err)
			continue
		}
		noTokenIn(t, next, err)
		if len(repos) != 1 || res.Count != 1 {
			t.Errorf("%s: the first page was lost: %d repos", next, len(repos))
		}
		for _, h := range srv.rt.Hosts() {
			if h != "api.github.com" {
				t.Errorf("%s: a request went to %q", next, h)
			}
		}
		if n := len(srv.requests()); n != 2 { // /user and page 1
			t.Errorf("%s: %d requests", next, n)
		}
	}
}

// A 50 MB body is refused; the client reads no more than the cap (plus one
// byte) into memory.
func TestAdvBody50MB(t *testing.T) {
	const total = 50 << 20
	var wrote atomic.Int64
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/user") {
			writeJSON(w, 200, map[string]any{"login": "alice"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		chunk := bytes.Repeat([]byte("a"), 64<<10)
		_, _ = w.Write([]byte(`[{"name":"`))
		for n := 0; n < total; n += len(chunk) {
			k, err := w.Write(chunk)
			wrote.Add(int64(k))
			if err != nil {
				return // the client hung up, as it should
			}
		}
		_, _ = w.Write([]byte(`"}]`))
	})
	start := time.Now()
	_, _, repos, err := listErr(t, srv, ghAccount())
	var oe *Error
	if !errors.As(err, &oe) || oe.Kind != KindBadResponse || !strings.Contains(oe.Detail, "too large") {
		t.Fatalf("%v", err)
	}
	if len(repos) != 0 {
		t.Errorf("%d repos from a refused page", len(repos))
	}
	noTokenIn(t, "50MB", err)
	if d := time.Since(start); d > 25*time.Second {
		t.Errorf("took %v", d)
	}
	t.Logf("the server wrote %d of %d bytes before the client hung up", wrote.Load(), total)
}

// A gzip bomb (a few KB that unpack to far more than the cap) is refused too:
// the cap counts the unpacked bytes.
func TestAdvGzipBomb(t *testing.T) {
	var zbuf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&zbuf, gzip.BestCompression)
	_, _ = zw.Write([]byte(`[{"name":"`))
	zeros := make([]byte, 1<<20)
	for i := range zeros {
		zeros[i] = 'a'
	}
	for i := 0; i < 96; i++ { // 96 MiB unpacked
		_, _ = zw.Write(zeros)
	}
	_, _ = zw.Write([]byte(`"}]`))
	_ = zw.Close()
	bomb := zbuf.Bytes()
	if len(bomb) > 1<<20 {
		t.Fatalf("the bomb is %d bytes, not small", len(bomb))
	}
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/user") {
			writeJSON(w, 200, map[string]any{"login": "alice"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(bomb)
	})
	_, _, repos, err := listErr(t, srv, ghAccount())
	var oe *Error
	if !errors.As(err, &oe) || oe.Kind != KindBadResponse || !strings.Contains(oe.Detail, "too large") {
		t.Fatalf("%v", err)
	}
	if len(repos) != 0 {
		t.Errorf("%d repos", len(repos))
	}
	noTokenIn(t, "gzip bomb", err)
}

// A runaway chain of 1,000 pages stops at the cap, however high the caller
// sets it, and the repositories of the pages read are kept.
func TestAdvRunaway1000Pages(t *testing.T) {
	var pageHits atomic.Int64
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/user") {
			writeJSON(w, 200, map[string]any{"login": "alice"})
			return
		}
		page := 1
		fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
		pageHits.Add(1)
		if page < 1000 {
			w.Header().Set("Link", fmt.Sprintf(`<https://%s%s?page=%d>; rel="next"`, r.Host, r.URL.Path, page+1))
		}
		writeJSON(w, 200, []map[string]any{ghRepoJSON("acme", fmt.Sprintf("r%d", page), nil)})
	})
	for _, c := range []struct{ ask, want int }{{100000, MaxPagesLimit}, {0, DefaultMaxPages}, {-5, DefaultMaxPages}, {3, 3}} {
		pageHits.Store(0)
		cl, _ := srv.client()
		cl.MaxPages = c.ask
		res, repos, pages, err := listAll(t, cl, ghAccount(), tokenOf(SourceEnv))
		if err != nil {
			t.Fatalf("ask %d: %v", c.ask, err)
		}
		if !res.Truncated || len(pages) != c.want || len(repos) != c.want || res.Pages != c.want {
			t.Errorf("ask %d: truncated=%v pages=%d repos=%d, want %d", c.ask, res.Truncated, len(pages), len(repos), c.want)
		}
		if got := int(pageHits.Load()); got != c.want {
			t.Errorf("ask %d: the server saw %d page requests, want %d", c.ask, got, c.want)
		}
	}
}

// 429 with Retry-After 5: waited out once. With 3600 (and values that
// overflow): reported, never slept.
func TestAdvRetryAfter(t *testing.T) {
	t.Run("5 recovers", func(t *testing.T) {
		srv, hits := rateServer(t, 1, func(h http.Header) { h.Set("Retry-After", "5") })
		c, slept := srv.client()
		if _, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceEnv)); err != nil {
			t.Fatal(err)
		}
		if len(*slept) != 1 || (*slept)[0] != 5*time.Second || atomic.LoadInt32(hits) != 2 {
			t.Errorf("slept %v hits %d", *slept, atomic.LoadInt32(hits))
		}
	})
	t.Run("5 stays limited", func(t *testing.T) {
		srv, hits := rateServer(t, 9, func(h http.Header) { h.Set("Retry-After", "5"); h.Set("X-Echo", fixtureToken) })
		c, slept := srv.client()
		_, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceEnv))
		if kindOf(err) != KindRateLimit || len(*slept) != 1 || atomic.LoadInt32(hits) != 2 {
			t.Errorf("%v slept %v hits %d", err, *slept, atomic.LoadInt32(hits))
		}
		noTokenIn(t, "429", err)
	})
	t.Run("3600 is reported", func(t *testing.T) {
		srv, hits := rateServer(t, 9, func(h http.Header) { h.Set("Retry-After", "3600") })
		c, slept := srv.client()
		_, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceEnv))
		var oe *Error
		if !errors.As(err, &oe) || oe.Kind != KindRateLimit || len(*slept) != 0 || atomic.LoadInt32(hits) != 1 {
			t.Fatalf("%v slept %v hits %d", err, *slept, atomic.LoadInt32(hits))
		}
		if !oe.Reset.Equal(c.Now().Add(time.Hour)) {
			t.Errorf("reset %v", oe.Reset)
		}
	})
	t.Run("10 is the longest wait", func(t *testing.T) {
		srv, _ := rateServer(t, 1, func(h http.Header) { h.Set("Retry-After", "10") })
		c, slept := srv.client()
		if _, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceEnv)); err != nil || len(*slept) != 1 {
			t.Errorf("%v %v", err, *slept)
		}
		srv, _ = rateServer(t, 1, func(h http.Header) { h.Set("Retry-After", "11") })
		c, slept = srv.client()
		if _, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceEnv)); kindOf(err) != KindRateLimit || len(*slept) != 0 {
			t.Errorf("%v %v", err, *slept)
		}
	})
	// values that overflow time.Duration (n * time.Second) must not turn into
	// a short wait, a negative one or a panic
	for _, v := range []string{"18446744074", "9223372036854775807", "99999999999999999999999", "-5", "abc", "Wed, 21 Oct 2099 07:28:00 GMT"} {
		v := v
		t.Run("overflow "+v, func(t *testing.T) {
			srv, hits := rateServer(t, 9, func(h http.Header) { h.Set("Retry-After", v) })
			c, slept := srv.client()
			_, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceEnv))
			if kindOf(err) != KindRateLimit || len(*slept) != 0 || atomic.LoadInt32(hits) != 1 {
				t.Fatalf("%v slept %v hits %d", err, *slept, atomic.LoadInt32(hits))
			}
			var oe *Error
			errors.As(err, &oe)
			if !oe.Reset.IsZero() && oe.Reset.Before(c.Now()) {
				t.Errorf("the reset is in the past: %v", oe.Reset)
			}
		})
	}
	t.Run("overflow reset header", func(t *testing.T) {
		srv, _ := rateServer(t, 9, func(h http.Header) {
			h.Set("X-RateLimit-Remaining", "0")
			h.Set("X-RateLimit-Reset", "9223372036854775807")
		})
		c, slept := srv.client()
		_, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceEnv))
		if kindOf(err) != KindRateLimit || len(*slept) != 0 {
			t.Errorf("%v %v", err, *slept)
		}
	})
}

// GitHub's SAML 403: the authorization link is kept only on the account's own
// host (github.com for GitHub cloud); anything else gives no link (INFO-1).
func TestAdvSSO403(t *testing.T) {
	ghe := ghAccount()
	ghe.Host = "ghe.example.com"
	for _, c := range []struct {
		name, hdr string
		acc       Account
		want      string
	}{
		{"cloud ok", "required; url=https://github.com/orgs/x/sso?authorization_request=A", ghAccount(), "https://github.com/orgs/x/sso?authorization_request=A"},
		{"cloud case", "required; url=https://GitHub.com/orgs/x/sso", ghAccount(), "https://GitHub.com/orgs/x/sso"},
		{"cloud other host", "required; url=https://evil.example/orgs/x/sso", ghAccount(), ""},
		{"cloud lookalike", "required; url=https://github.com.evil.example/sso", ghAccount(), ""},
		{"cloud userinfo trick", "required; url=https://github.com@evil.example/sso", ghAccount(), ""},
		{"cloud api host", "required; url=https://api.github.com/x", ghAccount(), ""},
		{"cloud http", "required; url=http://github.com/orgs/x/sso", ghAccount(), ""},
		{"cloud token in link", "required; url=https://github.com/sso?t=" + fixtureToken, ghAccount(), ""},
		{"ghe ok", "required; url=https://ghe.example.com/orgs/x/sso", ghe, "https://ghe.example.com/orgs/x/sso"},
		{"ghe github.com", "required; url=https://github.com/orgs/x/sso", ghe, ""},
		{"ghe other", "required; url=https://evil.example/sso", ghe, ""},
		{"no url", "required", ghAccount(), ""},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/user") {
					writeJSON(w, 200, map[string]any{"login": "alice"})
					return
				}
				w.Header().Set("X-GitHub-SSO", c.hdr)
				writeJSON(w, 403, map[string]any{"message": "Resource protected by organization SAML enforcement. " + fixtureToken})
			})
			_, _, _, err := listErr(t, srv, c.acc)
			var oe *Error
			if !errors.As(err, &oe) || oe.Kind != KindSSO || oe.Status != 403 || oe.SSOURL != c.want {
				t.Fatalf("want link %q: %+v", c.want, oe)
			}
			noTokenIn(t, c.name, err)
			// the same through the first request (/user) is not special: the
			// pinning also covers a 403 on /user
		})
	}
	t.Run("on /user", func(t *testing.T) {
		srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-GitHub-SSO", "required; url=https://evil.example/sso")
			writeJSON(w, 403, map[string]any{"message": "SAML"})
		})
		_, _, _, err := listErr(t, srv, ghAccount())
		var oe *Error
		if !errors.As(err, &oe) || oe.Kind != KindSSO || oe.SSOURL != "" {
			t.Fatalf("%+v", oe)
		}
	})
}

// 401: the token is refused. Nothing but the platform's own words is shown.
func TestAdvUnauthorized(t *testing.T) {
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token", error_description="`+fixtureToken+`"`)
		writeJSON(w, 401, map[string]any{"message": "Bad credentials"})
	})
	_, _, repos, err := listErr(t, srv, ghAccount())
	var oe *Error
	if !errors.As(err, &oe) || oe.Kind != KindAuth || oe.Status != 401 || oe.Detail != "Bad credentials" || len(repos) != 0 {
		t.Fatalf("%+v", oe)
	}
	noTokenIn(t, "401", err)
	if n := len(srv.requests()); n != 1 {
		t.Errorf("%d requests after a 401", n)
	}
	// GitLab says it differently
	srv = newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 401, map[string]any{"error": "invalid_token", "error_description": "Token is expired. " + fixtureToken})
	})
	_, _, _, err = listErr(t, srv, glAccount())
	if kindOf(err) != KindAuth {
		t.Fatalf("%v", err)
	}
	noTokenIn(t, "gitlab 401", err)
}

// An HTML login page with status 200 (a captive portal, an SSO proxy, a wrong
// host) is not an answer: no HTML is parsed, shown or kept.
func TestAdvHTMLLoginPage200(t *testing.T) {
	page := "<html><body><form action=/login>Sign in \x1b[31m " + fixtureToken + "</form></body></html>"
	for _, ct := range []string{"text/html; charset=utf-8", "text/plain", "", "application/octet-stream", "application/xhtml+xml"} {
		ct := ct
		for _, onUser := range []bool{true, false} {
			srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
				isUser := strings.HasSuffix(r.URL.Path, "/user")
				if isUser != onUser {
					writeJSON(w, 200, map[string]any{"login": "alice"})
					return
				}
				if ct != "" {
					w.Header().Set("Content-Type", ct)
				} else {
					w.Header()["Content-Type"] = nil
				}
				w.WriteHeader(200)
				_, _ = io.WriteString(w, page)
			})
			_, _, repos, err := listErr(t, srv, ghAccount())
			var oe *Error
			if !errors.As(err, &oe) || oe.Kind != KindBadResponse || len(repos) != 0 {
				t.Errorf("ct=%q user=%v: %v", ct, onUser, err)
				continue
			}
			if strings.Contains(oe.Detail, "Sign in") || strings.Contains(oe.Detail, "<") {
				t.Errorf("ct=%q: the page is in the message: %q", ct, oe.Detail)
			}
			noTokenIn(t, "html "+ct, err)
		}
	}
	// HTML under a JSON content type
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, page)
	})
	_, _, _, err := listErr(t, srv, ghAccount())
	if kindOf(err) != KindBadResponse {
		t.Fatalf("%v", err)
	}
	noTokenIn(t, "html as json", err)
	// a redirect (same host, https) to a login page that answers 200 HTML
	srv = newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, page)
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	})
	_, _, _, err = listErr(t, srv, ghAccount())
	if kindOf(err) != KindBadResponse {
		t.Fatalf("%v", err)
	}
	noTokenIn(t, "login redirect", err)
}

// Names with ../, absolute paths, escape sequences and bidi overrides never
// get a clone URL, and nothing they hold reaches a terminal.
func TestAdvHostileRepoNames(t *testing.T) {
	hostile := []struct{ owner, name string }{
		{"acme", "../../etc/passwd"},
		{"..", "x"},
		{"acme", ".."},
		{"/etc", "passwd"},
		{"acme", "/abs/path"},
		{"acme", `..\win`},
		{"acme", "a/b"},
		{"acme", "evil\x1b]8;;https://evil.example\x07link\x1b]8;;\x07"},
		{"acme", "evil\x1b[2Jname"},
		{"acme", "re\u202etxt.exe"},
		{"acme\u2066", "iso"},
		{"acme", "zero\u200bwidth"},
		{"acme", "tab\tname"},
		{"acme", "nl\nname"},
		{"acme", "nul\x00name"},
		{"acme", "c1\u009bname"},
		{"acme", ".git"},
		{"acme", ".GIT"},
		{"acme", "."},
		{".git", "x"},
		{"acme", "-rf"},
	}
	var raw []map[string]any
	for _, h := range hostile {
		raw = append(raw, ghRepoJSON(h.owner, h.name, nil))
	}
	raw = append(raw, ghRepoJSON("acme", "good", nil))
	srv := ghServer(t, "alice", [][]map[string]any{raw}, nil)
	_, _, repos, err := listErr(t, srv, ghAccount())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range repos {
		for _, s := range []string{r.Owner, r.Name, r.FullPath, r.CloneURL, r.WebURL} {
			assertClean(t, r.FullPath, s)
		}
		if r.Name == "good" {
			if r.CloneURL != "https://github.com/acme/good.git" {
				t.Errorf("the good repo: %+v", r)
			}
			continue
		}
		if r.Name == "-rf" {
			continue // a plain name; PlanClone refuses it (app AO-8)
		}
		if r.CloneURL != "" || r.WebURL != "" {
			t.Errorf("a hostile name got a URL: %+v", r)
		}
	}
	// GitLab: the path_with_namespace
	srv = newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/user") {
			writeJSON(w, 200, map[string]any{"username": "dave"})
			return
		}
		var ps []map[string]any
		for _, p := range []string{"g/../../etc/x", "/abs/path", "g//x", "g/\x1b[31mred", "g/\u202eevil", "g/a\\b", "..", "g/", "/g", "g/ok"} {
			ps = append(ps, map[string]any{"path_with_namespace": p, "visibility": "private"})
		}
		writeJSON(w, 200, ps)
	})
	_, _, repos, err = listErr(t, srv, glAccount())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range repos {
		for _, s := range []string{r.Owner, r.Name, r.FullPath, r.CloneURL, r.WebURL} {
			assertClean(t, r.FullPath, s)
		}
		if r.FullPath != "g/ok" && (r.CloneURL != "" || r.WebURL != "") {
			t.Errorf("a hostile path got a URL: %+v", r)
		}
		if r.FullPath == "g/ok" && r.CloneURL != "https://git.example.com/g/ok.git" {
			t.Errorf("the good project: %+v", r)
		}
	}
}

// assertClean fails when s holds a control, escape or format character.
func assertClean(t *testing.T, label, s string) {
	t.Helper()
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || r == 0x202e || r == 0x2066 || r == 0x200b || r == 0x2069 {
			t.Errorf("%q: %q holds U+%04X", label, s, r)
			return
		}
	}
	if strings.ContainsAny(s, "\x1b") {
		t.Errorf("%q: escape in %q", label, s)
	}
}

// The secret printed every way a program prints: log, %p, %#v, errors.Join,
// wrapped errors, and a recovered panic message.
func TestAdvSecretNeverPrinted(t *testing.T) {
	s := secret()
	tok := Token{Secret: s, Source: SourceEnv, Note: "n"}
	pt := &tok
	type holder struct {
		Tok    Token
		P      *Token
		S      Secret
		M      map[string]Secret
		List   []Secret
		hidden Secret
	}
	h := holder{Tok: tok, P: pt, S: s, M: map[string]Secret{"k": s}, List: []Secret{s}, hidden: s}
	var buf bytes.Buffer
	lg := log.New(&buf, "", log.Lshortfile)
	lg.Printf("%v %+v %#v %s %q %x %d %p", s, s, s, s, s, s, s, s)
	lg.Printf("%v %+v %#v", tok, pt, h)
	lg.Println(s, &s, tok, pt, h)
	lg.Print(s, h)
	lg.Printf("%p %p", &s, pt)
	if strings.Contains(buf.String(), fixtureToken) || strings.Contains(buf.String(), fixtureToken[:8]) {
		t.Errorf("log output holds the token:\n%s", buf.String())
	}

	// errors that carry a Secret as an argument
	e1 := fmt.Errorf("auth with %v failed", s)
	e2 := fmt.Errorf("wrapped: %w", fmt.Errorf("inner %#v: %w", s, newError(KindAuth, 401, "bad "+fixtureToken, s)))
	e3 := errors.Join(e1, e2, fmt.Errorf("%+v", h))
	for i, e := range []error{e1, e2, e3, fmt.Errorf("outer: %w", e3)} {
		noTokenIn(t, fmt.Sprint("err", i), e)
	}

	// a recovered panic, whatever was thrown
	for name, v := range map[string]any{
		"secret":   s,
		"token":    tok,
		"holder":   h,
		"error":    e3,
		"sprintf":  fmt.Sprintf("boom %v %#v", s, h),
		"errorf":   fmt.Errorf("boom %w", e2),
		"newError": newError(KindAuth, 401, fixtureToken, s),
	} {
		msg := recovered(v)
		if strings.Contains(msg, fixtureToken) || strings.Contains(msg, fixtureToken[:8]) {
			t.Errorf("panic(%s) message holds the token: %s", name, msg)
		}
	}
}

// recovered panics with v and returns what a recover handler would print.
func recovered(v any) (msg string) {
	defer func() {
		r := recover()
		msg = fmt.Sprintf("%v | %+v | %#v | %s", r, r, r, r)
		if e, ok := r.(error); ok {
			msg += " | " + e.Error()
		}
	}()
	panic(v)
}

// SSO and the host check are about the account's host, so the account used
// by the tests above stays the one the fixtures say it is.
func TestAdvFixturesAreWhatTheyShouldBe(t *testing.T) {
	if a := ghAccount(); a.Host != "github.com" || a.Platform != platform.GitHub {
		t.Fatalf("%+v", a)
	}
}

// Real names that start with a dot stay cloneable (.github is a common
// repository); only "." and ".git" segments are refused.
func TestAdvDotNames(t *testing.T) {
	for name, ok := range map[string]bool{".github": true, "a.b": true, "dotfiles": true, ".git": false, ".GIT": false, ".": false, "..": false, "x.git": true} {
		c, w := urls("github.com", "acme", name)
		if (c != "") != ok || (w != "") != ok {
			t.Errorf("%q: clone %q web %q, want cloneable=%v", name, c, w, ok)
		}
	}
	if c, _ := urls("git.example.com", ".git/sub", "p"); c != "" {
		t.Errorf("a .git group: %q", c)
	}
}

package update

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/testutil"
)

const tagURL = "https://github.com/tinhnd1902/gitid/releases/tag/v"

// fixture is a temp env with a fake clock, an optional fake release server and
// the checker over them. Without a server every request fails the test.
type fixture struct {
	t    *testing.T
	e    env.Env
	rt   *testutil.RedirectRT
	hits atomic.Int32
	c    Checker
}

func newFixture(t *testing.T, current string, h http.HandlerFunc) *fixture {
	t.Helper()
	f := &fixture{t: t, e: testutil.TempEnv(t)}
	if err := os.MkdirAll(f.e.Paths().ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if h != nil {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			f.hits.Add(1)
			h(w, r)
		}))
		t.Cleanup(srv.Close)
		f.rt = testutil.RedirectTransport(srv)
		f.e.HTTP = f.rt
	}
	f.c = Checker{Env: f.e, Current: current, Timeout: 3 * time.Second}
	return f
}

// sync makes the checker see the current f.e (after a change of the env).
func (f *fixture) sync() { f.c.Env = f.e }

func (f *fixture) setVars(vars map[string]string) {
	f.e = testutil.MapEnv(f.e, vars)
	f.sync()
}

func (f *fixture) cachePath() string { return f.e.Paths().UpdateCheckFile }

func (f *fixture) writeCache(content string) {
	f.t.Helper()
	testutil.WriteFile(f.t, f.cachePath(), content, 0o600)
}

func (f *fixture) cacheJSON(age time.Duration, latest string) string {
	b, _ := json.Marshal(cacheFile{Checked: f.e.Now().Add(-age).UTC().Format(time.RFC3339), Latest: latest})
	return string(b)
}

func (f *fixture) readCache() cacheFile {
	f.t.Helper()
	var cf cacheFile
	if err := json.Unmarshal([]byte(testutil.ReadFile(f.t, f.cachePath())), &cf); err != nil {
		f.t.Fatalf("cache is not JSON: %v", err)
	}
	return cf
}

func redirectTo(loc string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", loc)
		w.WriteHeader(http.StatusFound)
	}
}

func (f *fixture) check() (Notice, bool) {
	f.t.Helper()
	return f.c.Check(context.Background())
}

// U1
func TestU1ParseRelease(t *testing.T) {
	for s, want := range map[string][3]int{
		"0.1.8": {0, 1, 8}, "v0.1.8": {0, 1, 8}, "1.2.3": {1, 2, 3}, "0.0.0": {0, 0, 0}, "10.20.30": {10, 20, 30},
	} {
		if v, ok := ParseRelease(s); !ok || v != want {
			t.Errorf("%q: %v %v", s, v, ok)
		}
	}
	for _, s := range []string{
		"", "dev", "test", "0.1", "1.2.3.4", "01.2.3", "1.02.3", "1.2.3-rc.1", "v0.0.0-test", "1.2.3+meta",
		"１.2.3", "1.2.3\x1b[31m", " 1.2.3", "1.2.3\n", "123456.0.0", "v", "vv1.2.3", "V1.2.3", "1.2.3 ", "-1.2.3",
	} {
		if _, ok := ParseRelease(s); ok {
			t.Errorf("%q was accepted", s)
		}
	}
}

// U2
func TestU2Compare(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"0.1.9", "0.1.8", true}, {"0.2.0", "0.1.10", true}, {"1.0.0", "0.99.99", true},
		{"0.1.8", "0.1.8", false}, {"0.1.7", "0.1.8", false}, {"0.1.10", "0.2.0", false},
	} {
		a, _ := ParseRelease(tc.a)
		b, _ := ParseRelease(tc.b)
		if got := newer(a, b); got != tc.want {
			t.Errorf("%s > %s = %v", tc.a, tc.b, got)
		}
	}
}

// U3: the opt-out makes no request and does not touch the cache.
func TestU3OptOut(t *testing.T) {
	for _, v := range []string{"1", "true", "yes", "TRUE", "True", "on", " 1 "} {
		f := newFixture(t, "0.1.8", nil) // a request would fail the test
		f.setVars(map[string]string{OptOutVar: v})
		f.writeCache(f.cacheJSON(48*time.Hour, "0.1.9"))
		past := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		if err := os.Chtimes(f.cachePath(), past, past); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(f.cachePath())
		if n, ok := f.check(); ok {
			t.Errorf("%q: notice %+v", v, n)
		}
		after, _ := os.ReadFile(f.cachePath())
		fi, _ := os.Stat(f.cachePath())
		if !bytes.Equal(before, after) || !fi.ModTime().Equal(past) {
			t.Errorf("%q: the cache was touched", v)
		}
	}
}

// U3b: values that do not opt out run the check.
func TestU3bNotOptedOut(t *testing.T) {
	for _, v := range []string{"0", "false", "FALSE", ""} {
		f := newFixture(t, "0.1.8", redirectTo(tagURL+"0.2.0"))
		f.setVars(map[string]string{OptOutVar: v})
		if n, ok := f.check(); !ok || n.Latest != "0.2.0" {
			t.Errorf("%q: %+v %v", v, n, ok)
		}
		if f.hits.Load() != 1 {
			t.Errorf("%q: %d requests", v, f.hits.Load())
		}
	}
}

// U4: a build that is not a plain release never reads, writes or asks.
func TestU4NotARelease(t *testing.T) {
	for _, cur := range []string{"dev", "test", "", "0.1.9-SNAPSHOT-abc", "0.1.8-3-gabc", "0.1.8-rc.1", "v0.0.0-test"} {
		f := newFixture(t, cur, nil)
		if n, ok := f.check(); ok {
			t.Errorf("%q: notice %+v", cur, n)
		}
		if _, err := os.Lstat(f.cachePath()); err == nil {
			t.Errorf("%q: a cache file was written", cur)
		}
	}
}

// U5 and U6
func TestU5U6FreshCache(t *testing.T) {
	f := newFixture(t, "0.1.8", nil)
	f.writeCache(f.cacheJSON(time.Hour, "0.1.9"))
	n, ok := f.check()
	if !ok || n.Latest != "0.1.9" {
		t.Fatalf("%+v %v", n, ok)
	}
	f.c.Current = "0.1.9" // the user upgraded
	if n, ok := f.check(); ok {
		t.Fatalf("notice after the upgrade: %+v", n)
	}
	f.c.Current = "v0.2.0"
	if _, ok := f.check(); ok {
		t.Fatal("notice for a newer build than the cache")
	}
}

// U7
func TestU7StaleCacheIsRefreshed(t *testing.T) {
	f := newFixture(t, "0.1.8", redirectTo(tagURL+"0.2.0"))
	f.writeCache(f.cacheJSON(25*time.Hour, "0.1.9"))
	n, ok := f.check()
	if !ok || n.Latest != "0.2.0" {
		t.Fatalf("%+v %v", n, ok)
	}
	cf := f.readCache()
	if cf.Latest != "0.2.0" || cf.Checked != f.e.Now().UTC().Format(time.RFC3339) {
		t.Fatalf("cache %+v", cf)
	}
	if fi, _ := os.Stat(f.cachePath()); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}

// U8
func TestU8Boundaries(t *testing.T) {
	for _, tc := range []struct {
		age   time.Duration
		fresh bool
	}{
		{23*time.Hour + 59*time.Minute + 59*time.Second, true},
		{24 * time.Hour, false},
		{-4 * time.Minute, true},
		{-5 * time.Minute, true},
		{-6 * time.Minute, false},
		{0, true},
	} {
		f := newFixture(t, "0.1.8", redirectTo(tagURL+"0.3.0"))
		f.writeCache(f.cacheJSON(tc.age, "0.2.0"))
		n, ok := f.check()
		wantHits, wantLatest := int32(1), "0.3.0"
		if tc.fresh {
			wantHits, wantLatest = 0, "0.2.0"
		}
		if !ok || n.Latest != wantLatest || f.hits.Load() != wantHits {
			t.Errorf("age %v: %+v %v, %d requests", tc.age, n, ok, f.hits.Load())
		}
	}
}

// U9
func TestU9NoCacheFile(t *testing.T) {
	f := newFixture(t, "0.1.8", redirectTo(tagURL+"0.1.9"))
	n, ok := f.check()
	if !ok || n.Latest != "0.1.9" || f.hits.Load() != 1 {
		t.Fatalf("%+v %v %d", n, ok, f.hits.Load())
	}
	fi, err := os.Stat(f.cachePath())
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("cache %v %v", fi, err)
	}
}

// U10
func TestU10BadCache(t *testing.T) {
	for name, content := range map[string]string{
		"garbage":        "garbage",
		"empty object":   "{}",
		"bad time":       `{"checked":"x"}`,
		"too big":        `{"checked":"2026-11-16T14:01:02Z","latest":"0.1.9","pad":"` + strings.Repeat("x", 5<<10) + `"}`,
		"escape":         `{"checked":"2026-11-16T14:01:02Z","latest":"9.9.9\u001b[2J"}`,
		"huge component": `{"checked":"2026-11-16T14:01:02Z","latest":"99999999.0.0"}`,
		"prerelease":     `{"checked":"2026-11-16T14:01:02Z","latest":"9.9.9-evil"}`,
		"array":          `[1,2]`,
		"wrong types":    `{"checked":5,"latest":6}`,
	} {
		f := newFixture(t, "0.1.8", redirectTo(tagURL+"0.2.0"))
		f.writeCache(content)
		n, ok := f.check()
		if !ok || n.Latest != "0.2.0" || f.hits.Load() != 1 {
			t.Errorf("%s: %+v %v %d requests", name, n, ok, f.hits.Load())
		}
	}
}

// U11: nothing a hostile server sends gets through.
func TestU11HostileAnswers(t *testing.T) {
	long := strings.Repeat("a", 60<<10)
	cases := map[string]http.HandlerFunc{
		"200 html":      func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<html>v9.9.9</html>") },
		"200 json":      func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"tag_name":"v9.9.9"}`) },
		"other host":    redirectTo("https://evil.example/tinhnd1902/gitid/releases/tag/v9.9.9"),
		"look-alike":    redirectTo("https://github.com.evil.example/tinhnd1902/gitid/releases/tag/v9.9.9"),
		"userinfo":      redirectTo("https://github.com@evil.example/tinhnd1902/gitid/releases/tag/v9.9.9"),
		"port":          redirectTo("https://github.com:8443/tinhnd1902/gitid/releases/tag/v9.9.9"),
		"http":          redirectTo("http://github.com/tinhnd1902/gitid/releases/tag/v9.9.9"),
		"other project": redirectTo("https://github.com/other/gitid/releases/tag/v9.9.9"),
		"other path":    redirectTo("https://github.com/tinhnd1902/gitid/releases/download/v9.9.9"),
		"prerelease":    redirectTo("https://github.com/tinhnd1902/gitid/releases/tag/v9.9.9-evil"),
		"escape":        redirectTo("https://github.com/tinhnd1902/gitid/releases/tag/v1.2.3%1B%5B2J"),
		"extra segment": redirectTo("https://github.com/tinhnd1902/gitid/releases/tag/v9.9.9/x"),
		"empty":         redirectTo(""),
		"long":          redirectTo(tagURL + long),
		"404":           func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) },
		"403":           func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) },
		"429":           func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429) },
		"500":           func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) },
		"300": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", tagURL+"9.9.9")
			w.WriteHeader(300)
		},
		"304": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", tagURL+"9.9.9")
			w.WriteHeader(304)
		},
	}
	for name, h := range cases {
		f := newFixture(t, "0.1.8", h)
		f.writeCache(f.cacheJSON(48*time.Hour, ""))
		start := time.Now()
		n, ok := f.check()
		if ok {
			t.Errorf("%s: notice %+v", name, n)
		}
		if d := time.Since(start); d > f.c.Timeout+time.Second {
			t.Errorf("%s: took %v", name, d)
		}
		if f.hits.Load() != 1 {
			t.Errorf("%s: %d requests", name, f.hits.Load())
		}
		cf := f.readCache()
		if cf.Checked != f.e.Now().UTC().Format(time.RFC3339) || cf.Latest != "" {
			t.Errorf("%s: cache %+v", name, cf)
		}
	}
}

// A relative Location is read against github.com and is accepted.
func TestU11RelativeLocation(t *testing.T) {
	f := newFixture(t, "0.1.8", redirectTo("/tinhnd1902/gitid/releases/tag/v0.2.0"))
	if n, ok := f.check(); !ok || n.Latest != "0.2.0" {
		t.Fatalf("%+v %v", n, ok)
	}
}

// Host names compare without case.
func TestU11HostCase(t *testing.T) {
	f := newFixture(t, "0.1.8", redirectTo("https://GitHub.com/tinhnd1902/gitid/releases/tag/v0.2.0"))
	if n, ok := f.check(); !ok || n.Latest != "0.2.0" {
		t.Fatalf("%+v %v", n, ok)
	}
}

// A failed attempt keeps the last valid answer.
func TestU11FailureKeepsLatest(t *testing.T) {
	f := newFixture(t, "0.1.8", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	f.writeCache(f.cacheJSON(48*time.Hour, "0.1.9"))
	n, ok := f.check()
	if !ok || n.Latest != "0.1.9" {
		t.Fatalf("%+v %v", n, ok)
	}
	if cf := f.readCache(); cf.Latest != "0.1.9" || cf.Checked != f.e.Now().UTC().Format(time.RFC3339) {
		t.Fatalf("cache %+v", cf)
	}
	// and it is trusted for the next day: no second request
	if _, ok := f.check(); !ok || f.hits.Load() != 1 {
		t.Fatalf("second check: %v, %d requests", ok, f.hits.Load())
	}
}

// A body of 50 MiB behind a redirect is not read.
func TestU11BodyNotRead(t *testing.T) {
	var written atomic.Int64
	f := newFixture(t, "0.1.8", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://evil.example/x")
		w.WriteHeader(http.StatusFound)
		chunk := bytes.Repeat([]byte("x"), 64<<10)
		for i := 0; i < 800; i++ {
			n, err := w.Write(chunk)
			written.Add(int64(n))
			if err != nil {
				return
			}
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
		}
	})
	if _, ok := f.check(); ok {
		t.Fatal("notice")
	}
	time.Sleep(200 * time.Millisecond)
	if written.Load() >= 50<<20 {
		t.Fatalf("the server wrote the whole body (%d bytes): it was read", written.Load())
	}
}

// U11b
func TestU11bTimeout(t *testing.T) {
	release := make(chan struct{})
	f := newFixture(t, "0.1.8", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	t.Cleanup(func() { close(release) })
	f.c.Timeout = 200 * time.Millisecond
	start := time.Now()
	if _, ok := f.check(); ok {
		t.Fatal("notice")
	}
	if d := time.Since(start); d > 1200*time.Millisecond {
		t.Fatalf("took %v", d)
	}
	if cf := f.readCache(); cf.Checked != f.e.Now().UTC().Format(time.RFC3339) {
		t.Fatalf("not stamped: %+v", cf)
	}
}

// U11c: refused connection and a TLS failure.
func TestU11cConnectionErrors(t *testing.T) {
	f := newFixture(t, "0.1.8", redirectTo(tagURL+"0.2.0"))
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	rt := testutil.RedirectTransport(srv)
	srv.Close() // refused from now on
	f.e.HTTP = rt
	f.sync()
	if _, ok := f.check(); ok {
		t.Fatal("notice after a refused connection")
	}
	plain := httptest.NewServer(http.NotFoundHandler()) // speaks no TLS
	defer plain.Close()
	f2 := newFixture(t, "0.1.8", nil)
	f2.e.HTTP = redirectPlain{plain.Listener.Addr().String()}
	f2.sync()
	if _, ok := f2.check(); ok {
		t.Fatal("notice after a TLS failure")
	}
}

// redirectPlain sends every request to addr over https although the server
// speaks plain http.
type redirectPlain struct{ addr string }

func (r redirectPlain) RoundTrip(req *http.Request) (*http.Response, error) {
	out := req.Clone(req.Context())
	u := *req.URL
	u.Host = r.addr
	out.URL = &u
	return (&http.Transport{}).RoundTrip(out)
}

// U12: what the request carries.
func TestU12Request(t *testing.T) {
	type seen struct {
		method, host, path, query, auth, cookie, ua, accept string
		body                                                int
	}
	var got seen
	var mu sync.Mutex
	f := newFixture(t, "0.1.8", func(w http.ResponseWriter, r *http.Request) {
		b := new(bytes.Buffer)
		_, _ = b.ReadFrom(r.Body)
		mu.Lock()
		got = seen{r.Method, r.Host, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), r.Header.Get("Cookie"), r.Header.Get("User-Agent"), r.Header.Get("Accept"), b.Len()}
		mu.Unlock()
		w.Header().Set("Location", tagURL+"0.2.0")
		w.WriteHeader(http.StatusFound)
	})
	f.setVars(map[string]string{"GITHUB_TOKEN": "ghp_secret", "GH_TOKEN": "ghp_secret2", "GITID_TOKEN_ACME": "x", "HTTPS_PROXY": ""})
	if _, ok := f.check(); !ok {
		t.Fatal("no notice")
	}
	mu.Lock()
	defer mu.Unlock()
	want := seen{method: "GET", host: "github.com", path: "/tinhnd1902/gitid/releases/latest", ua: "gitid/0.1.8", accept: ""}
	if got != want {
		t.Fatalf("request %+v\nwant    %+v", got, want)
	}
	// the user agent carries the number, not "v"
	if reqs := f.rt.Requests(); len(reqs) != 1 || reqs[0] != "GET github.com/tinhnd1902/gitid/releases/latest" {
		t.Fatalf("requests %v", reqs)
	}
}

// U13: the redirect is not followed.
func TestU13NoRedirectFollowed(t *testing.T) {
	f := newFixture(t, "0.1.8", redirectTo(tagURL+"0.2.0"))
	f.check()
	if f.hits.Load() != 1 || len(f.rt.Hosts()) != 1 {
		t.Fatalf("%d requests, hosts %v", f.hits.Load(), f.rt.Hosts())
	}
}

// U14: the cache is best effort.
func TestU14CacheWriteFailures(t *testing.T) {
	t.Run("config dir missing", func(t *testing.T) {
		f := newFixture(t, "0.1.8", redirectTo(tagURL+"0.2.0"))
		if err := os.RemoveAll(f.e.Paths().ConfigDir); err != nil {
			t.Fatal(err)
		}
		if n, ok := f.check(); !ok || n.Latest != "0.2.0" {
			t.Fatalf("%+v %v", n, ok)
		}
		if _, err := os.Stat(f.e.Paths().ConfigDir); err == nil {
			t.Fatal("the check created the config folder")
		}
	})
	t.Run("config dir read only", func(t *testing.T) {
		f := newFixture(t, "0.1.8", redirectTo(tagURL+"0.2.0"))
		dir := f.e.Paths().ConfigDir
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		if n, ok := f.check(); !ok || n.Latest != "0.2.0" {
			t.Fatalf("%+v %v", n, ok)
		}
		ents, _ := os.ReadDir(dir)
		if len(ents) != 0 {
			t.Fatalf("left behind: %v", ents)
		}
	})
	t.Run("parent is a file", func(t *testing.T) {
		f := newFixture(t, "0.1.8", redirectTo(tagURL+"0.2.0"))
		dir := f.e.Paths().ConfigDir
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dir, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if n, ok := f.check(); !ok || n.Latest != "0.2.0" {
			t.Fatalf("%+v %v", n, ok)
		}
	})
}

// U10b: a cache path that is not a regular file stops the check.
func TestU10bSpecialCachePath(t *testing.T) {
	t.Run("directory", func(t *testing.T) {
		f := newFixture(t, "0.1.8", nil)
		if err := os.Mkdir(f.cachePath(), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.check(); ok {
			t.Fatal("notice")
		}
		if ents, _ := os.ReadDir(f.cachePath()); len(ents) != 0 {
			t.Fatalf("written into the directory: %v", ents)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		f := newFixture(t, "0.1.8", nil)
		target := filepath.Join(f.e.Home, "target.json")
		testutil.WriteFile(t, target, f.cacheJSON(time.Hour, "0.2.0"), 0o600)
		if err := os.Symlink(target, f.cachePath()); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.check(); ok {
			t.Fatal("notice through a symlink")
		}
		if testutil.ReadFile(t, target) != f.cacheJSON(time.Hour, "0.2.0") {
			t.Fatal("the target was written")
		}
	})
}

// U15
func TestU15Classify(t *testing.T) {
	home := "/home/u"
	for path, want := range map[string]Method{
		"/opt/homebrew/Caskroom/gitid/0.1.8/gitid":    MethodBrew,
		"/usr/local/Caskroom/gitid/0.1.8/gitid":       MethodBrew,
		"/opt/homebrew/Cellar/gitid/0.1.8/bin/gitid":  MethodBrew,
		"/home/u/.local/bin/gitid":                    MethodScript,
		"/usr/local/bin/gitid":                        MethodScript,
		"/home/u/.local/bin/sub/gitid":                MethodUnknown,
		"/home/other/.local/bin/gitid":                MethodUnknown,
		"/tmp/x/gitid":                                MethodUnknown,
		"/opt/homebrew/Caskroom/gitid-other/1/gitid":  MethodUnknown,
		"/opt/homebrew/Caskroom/notgitid/0.1.8/gitid": MethodUnknown,
		"": MethodUnknown,
	} {
		if got := classify(path, home); got != want {
			t.Errorf("%q: %v, want %v", path, got, want)
		}
	}
	if got := classify("/home/u/.local/bin/gitid", ""); got != MethodUnknown {
		t.Errorf("empty home: %v", got)
	}
}

func TestU15MethodThroughExecutable(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	cask := filepath.Join(root, "opt", "homebrew", "Caskroom", "gitid", "0.1.8")
	if err := os.MkdirAll(cask, 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(cask, "gitid")
	if err := os.WriteFile(real, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(bin, "gitid")
	if err := os.WriteFile(plain, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linkdir")
	if err := os.MkdirAll(link, 0o755); err != nil {
		t.Fatal(err)
	}
	viaLink := filepath.Join(link, "gitid")
	if err := os.Symlink(real, viaLink); err != nil {
		t.Fatal(err)
	}
	e := env.New(home, "")
	for name, tc := range map[string]struct {
		exe  func() (string, error)
		want Method
	}{
		"cask":      {func() (string, error) { return real, nil }, MethodBrew},
		"script":    {func() (string, error) { return plain, nil }, MethodScript},
		"symlink":   {func() (string, error) { return viaLink, nil }, MethodBrew},
		"elsewhere": {func() (string, error) { return filepath.Join(root, "x", "gitid"), nil }, MethodUnknown},
		"error":     {func() (string, error) { return "", errors.New("boom") }, MethodUnknown},
		"nil":       {nil, MethodUnknown},
	} {
		if got := (Checker{Env: e, Executable: tc.exe}).method(); got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}

func TestNoticeCarriesMethod(t *testing.T) {
	f := newFixture(t, "0.1.8", redirectTo(tagURL+"0.2.0"))
	f.c.Executable = func() (string, error) { return "/opt/homebrew/Caskroom/gitid/0.1.8/gitid", nil }
	if n, ok := f.check(); !ok || n.Method != MethodBrew || n.Latest != "0.2.0" {
		t.Fatalf("%+v %v", n, ok)
	}
}

// U16
func TestU16PanicIsSilent(t *testing.T) {
	f := newFixture(t, "0.1.8", redirectTo(tagURL+"0.2.0"))
	f.c.Executable = func() (string, error) { panic("boom") }
	if n, ok := f.check(); ok {
		t.Fatalf("notice %+v", n)
	}
}

// U17
func TestU17CancelledContext(t *testing.T) {
	f := newFixture(t, "0.1.8", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if _, ok := f.c.Check(ctx); ok {
		t.Fatal("notice")
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("slow")
	}
	if _, err := os.Lstat(f.cachePath()); err == nil {
		t.Fatal("stamped")
	}
}

// The program leaving during the request is not an attempt.
func TestCancelDuringRequestDoesNotStamp(t *testing.T) {
	started := make(chan struct{})
	f := newFixture(t, "0.1.8", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.c.Check(ctx); close(done) }()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Check did not return after cancel")
	}
	if _, err := os.Lstat(f.cachePath()); err == nil {
		t.Fatal("stamped")
	}
}

// U18
func TestU18Concurrent(t *testing.T) {
	f := newFixture(t, "0.1.8", redirectTo(tagURL+"0.2.0"))
	f.writeCache(f.cacheJSON(48*time.Hour, ""))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.check()
		}()
	}
	wg.Wait()
	cf := f.readCache()
	if _, ok := ParseRelease(cf.Latest); !ok {
		t.Fatalf("cache %+v", cf)
	}
}

// A tampered Env without a clock or a lookup still works.
func TestZeroEnvFields(t *testing.T) {
	e := env.Env{Home: t.TempDir()}
	c := Checker{Env: e, Current: "dev"}
	if _, ok := c.Check(context.Background()); ok {
		t.Fatal("notice")
	}
	if optedOut(e) {
		t.Fatal("opted out without a lookup")
	}
}

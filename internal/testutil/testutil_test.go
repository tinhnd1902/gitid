package testutil

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinhnd1902/gitid/internal/env"
)

func TestTempEnvAndGuard(t *testing.T) {
	e := TempEnv(t)
	if e.Home == "" || e.Now() != FixedTime {
		t.Fatal("bad env")
	}
	if proc, err := env.FromOS(); err != nil || proc.Home != e.Home {
		t.Fatal("process HOME not redirected")
	}
	Guard(t, e, e.Paths().GitConfig, e.Paths().ConfigFile, "")
}

func TestGuardFailsOutside(t *testing.T) {
	e := TempEnv(t)
	ft := &fakeT{}
	Guard(ft, e, "/etc/passwd")
	if !ft.failed {
		t.Fatal("guard should fail for outside path")
	}
}

type fakeT struct {
	testing.TB
	failed bool
}

func (f *fakeT) Helper()                           {}
func (f *fakeT) Errorf(format string, args ...any) { f.failed = true }

func TestSnapshot(t *testing.T) {
	e := TempEnv(t)
	WriteFile(t, filepath.Join(e.Home, "a/b.txt"), "x", 0o600)
	if err := os.Symlink("b.txt", filepath.Join(e.Home, "a/l")); err != nil {
		t.Fatal(err)
	}
	WriteFile(t, filepath.Join(e.Home, "a/secret"), "s", 0o600)
	if err := os.Chmod(filepath.Join(e.Home, "a/secret"), 0); err != nil {
		t.Fatal(err)
	}
	s1 := Snapshot(t, e.Home)
	s2 := Snapshot(t, e.Home)
	AssertSame(t, s1, s2)
	if ReadFile(t, filepath.Join(e.Home, "a/b.txt")) != "x" {
		t.Fatal("ReadFile")
	}
	WriteFile(t, filepath.Join(e.Home, "a/b.txt"), "y", 0o600)
	WriteFile(t, filepath.Join(e.Home, "new"), "n", 0o600)
	if err := os.Remove(filepath.Join(e.Home, "a/l")); err != nil {
		t.Fatal(err)
	}
	s3 := Snapshot(t, e.Home)
	d := s2.Diff(s3)
	if len(d) != 5 { // new, removed link, changed file and two dirs (mtime)
		t.Fatalf("diff = %v", d)
	}
	_ = os.Chmod(filepath.Join(e.Home, "a/secret"), 0o600)
}

// ENV-2: TempEnv hides the process environment from LookupEnv, MapEnv
// installs only the given variables.
func TestTempEnvLookupEnvIsEmpty(t *testing.T) {
	t.Setenv("PATH_FOR_GITID_TEST", "x")
	e := TempEnv(t)
	if v, ok := e.LookupEnv("PATH"); ok || v != "" {
		t.Fatalf("PATH visible: %q %v", v, ok)
	}
	if _, ok := e.LookupEnv("PATH_FOR_GITID_TEST"); ok {
		t.Fatal("process variable visible")
	}
	m := MapEnv(e, map[string]string{"A": "1"})
	if v, ok := m.LookupEnv("A"); !ok || v != "1" {
		t.Fatalf("MapEnv A = %q %v", v, ok)
	}
	if _, ok := m.LookupEnv("PATH"); ok {
		t.Fatal("MapEnv leaks PATH")
	}
}

// recorder is a testing.TB that records Errorf instead of failing, so the
// test of the failing transport itself passes.
type recorder struct {
	testing.TB
	errs []string
}

func (r *recorder) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

// ENV-3: a request through the HTTP transport of TempEnv fails the test.
func TestTempEnvHTTPFailsTheTest(t *testing.T) {
	rec := &recorder{TB: t}
	e := TempEnv(rec)
	if e.HTTP == nil {
		t.Fatal("TempEnv has no HTTP transport")
	}
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/user", nil)
	_, err := e.HTTP.RoundTrip(req)
	if !errors.Is(err, ErrNetworkInTest) {
		t.Fatalf("err = %v", err)
	}
	if len(rec.errs) != 1 || !strings.Contains(rec.errs[0], "network in test") {
		t.Fatalf("recorded %v", rec.errs)
	}
}

// RedirectTransport sends any host to the server, keeps the host the code
// asked for in r.Host and records it.
func TestRedirectTransport(t *testing.T) {
	var gotHost, gotPath string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost, gotPath = r.Host, r.URL.RequestURI()
	}))
	defer srv.Close()
	rt := RedirectTransport(srv)
	c := &http.Client{Transport: rt}
	resp, err := c.Get("https://api.github.com/user?x=1")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if gotHost != "api.github.com" || gotPath != "/user?x=1" {
		t.Fatalf("server saw %q %q", gotHost, gotPath)
	}
	if h := rt.Hosts(); len(h) != 1 || h[0] != "api.github.com" {
		t.Fatalf("hosts %v", h)
	}
	if r := rt.Requests(); len(r) != 1 || r[0] != "GET api.github.com/user?x=1" {
		t.Fatalf("requests %v", r)
	}
}

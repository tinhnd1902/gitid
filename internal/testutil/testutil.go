// Package testutil provides helpers for tests: a TempEnv rooted in
// t.TempDir(), a guard that fails when a path escapes it (or touches the real
// HOME), and tree snapshot helpers.
package testutil

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/tinhnd1902/gitid/internal/env"
)

// FixedTime is the default clock of TempEnv.
var FixedTime = time.Date(2026, 11, 16, 14, 1, 2, 123_000_000, time.UTC)

// TempEnv returns an Env whose Home is a fresh temp dir. The process env is
// also pointed at it (HOME, XDG_CONFIG_HOME unset, git config isolated) so an
// accidental read of the process environment cannot reach the real HOME.
func TempEnv(t testing.TB) env.Env {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if real := env.RealHome(); real != "" && filepath.Clean(real) == filepath.Clean(home) {
		t.Fatalf("temp home equals the real HOME %q", real)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	e := env.New(home, "")
	e.Now = func() time.Time { return FixedTime }
	e.GOOS = "darwin"
	e.LookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	// no variable of the real process environment is visible to the code
	// under test; MapEnv installs the ones a test needs
	e.LookupEnv = func(string) (string, bool) { return "", false }
	// and no request leaves the test process: a test that needs the network
	// layer installs RedirectTransport
	e.HTTP = failTransport{t}
	return e
}

// ErrNetworkInTest is what the transport of TempEnv returns.
var ErrNetworkInTest = errors.New("testutil: network request in a test")

// failTransport fails the test that sends a request through it.
type failTransport struct{ t testing.TB }

func (f failTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.t.Errorf("network in test: %s %s", r.Method, r.URL.Redacted())
	return nil, ErrNetworkInTest
}

// RedirectRT sends every request to one httptest server whatever host the
// request names, and remembers what it was asked for. The server sees the
// original host in r.Host, so a handler can route on it. The client side
// trusts the certificate of the server.
type RedirectRT struct {
	inner http.RoundTripper
	addr  string

	mu    sync.Mutex
	hosts []string
	reqs  []string
}

// RedirectTransport points the requests of a test at srv (an
// httptest.NewTLSServer).
func RedirectTransport(srv *httptest.Server) *RedirectRT {
	return &RedirectRT{inner: srv.Client().Transport, addr: srv.Listener.Addr().String()}
}

// RoundTrip implements http.RoundTripper.
func (r *RedirectRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.hosts = append(r.hosts, req.URL.Host)
	r.reqs = append(r.reqs, req.Method+" "+req.URL.Host+req.URL.RequestURI())
	r.mu.Unlock()
	out := req.Clone(req.Context())
	out.Host = req.URL.Host
	u := *req.URL
	u.Host = r.addr
	out.URL = &u
	return r.inner.RoundTrip(out)
}

// Hosts lists the host of every request, in order, as the code asked for it.
func (r *RedirectRT) Hosts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.hosts...)
}

// Requests lists "METHOD host/path?query" of every request.
func (r *RedirectRT) Requests() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.reqs...)
}

// MapEnv makes e.LookupEnv answer from vars only and returns the updated Env.
func MapEnv(e env.Env, vars map[string]string) env.Env {
	e.LookupEnv = func(k string) (string, bool) {
		v, ok := vars[k]
		return v, ok
	}
	return e
}

// Guard fails the test if any of paths is not inside e.Home or lies inside
// the real HOME.
func Guard(t testing.TB, e env.Env, paths ...string) {
	t.Helper()
	real := env.RealHome()
	for _, p := range paths {
		if p == "" {
			continue
		}
		if !env.IsUnder(p, e.Home) {
			if !env.IsUnder(p, e.XDGConfigHome) || e.XDGConfigHome == "" {
				t.Errorf("guard: path %q is outside the temp home %q", p, e.Home)
			}
		}
		if real != "" && env.IsUnder(p, real) && !env.IsUnder(real, e.Home) && !env.IsUnder(e.Home, real) {
			t.Errorf("guard: path %q is inside the real HOME", p)
		}
	}
}

// WriteFile writes a file (creating parent dirs 0755) for test setup.
func WriteFile(t testing.TB, path string, data string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

// ReadFile reads a file for assertions.
func ReadFile(t testing.TB, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Entry describes one node of a tree snapshot.
type Entry struct {
	Mode  fs.FileMode
	SHA   string
	Size  int64
	MTime time.Time
	Link  string
}

// Tree is a snapshot of a directory tree keyed by relative path.
type Tree map[string]Entry

// Snapshot walks root (without following symlinks) and records every node.
func Snapshot(t testing.TB, root string) Tree {
	t.Helper()
	tree := Tree{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		e := Entry{Mode: info.Mode(), Size: info.Size(), MTime: info.ModTime()}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			e.Link, _ = os.Readlink(p)
		case info.Mode().IsRegular():
			if b, err := os.ReadFile(p); err == nil {
				sum := sha256.Sum256(b)
				e.SHA = hex.EncodeToString(sum[:])
			} else {
				e.SHA = "unreadable"
			}
		}
		tree[rel] = e
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return tree
}

// Diff returns a sorted list of differences between two snapshots (empty if
// identical, mtimes included).
func (a Tree) Diff(b Tree) []string {
	var out []string
	for k, ea := range a {
		eb, ok := b[k]
		switch {
		case !ok:
			out = append(out, "removed: "+k)
		case ea != eb:
			out = append(out, "changed: "+k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			out = append(out, "added: "+k)
		}
	}
	sort.Strings(out)
	return out
}

// AssertSame fails when the two snapshots differ.
func AssertSame(t testing.TB, before, after Tree) {
	t.Helper()
	if d := before.Diff(after); len(d) > 0 {
		t.Fatalf("tree changed: %v", d)
	}
}

package online

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/platform"
	"github.com/tinhnd1902/gitid/internal/testutil"
)

// fixtureToken is the distinctive token every secrecy scan looks for.
const fixtureToken = "tok_SECRET_9f3c"

func secret() Secret { return NewSecret(fixtureToken) }

func ghAccount() Account {
	return Account{Ref: "acme/alice", Slug: "acme", ID: "alice", Platform: platform.GitHub, Host: "github.com", Username: "alice"}
}

func glAccount() Account {
	return Account{Ref: "globex/dave", Slug: "globex", ID: "dave", Platform: platform.GitLab, Host: "git.example.com", Username: "dave"}
}

// api is a TLS test server that checks the rules every request must follow
// (GET only, the token only in the Authorization header) and records what it
// saw.
type api struct {
	t   *testing.T
	srv *httptest.Server
	rt  *testutil.RedirectRT
	h   http.HandlerFunc

	mu   sync.Mutex
	seen []seenReq
}

type seenReq struct {
	Host, Path, Query, Auth string
	Header                  http.Header
}

func newAPI(t *testing.T, h http.HandlerFunc) *api {
	t.Helper()
	a := &api{t: t, h: h}
	a.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// O-11: only GET
		if r.Method != http.MethodGet {
			t.Errorf("method %s", r.Method)
		}
		// SEC-5: the token appears only in the Authorization header
		if strings.Contains(r.URL.String(), fixtureToken) {
			t.Errorf("token in the URL: %s", r.URL)
		}
		for k, vs := range r.Header {
			for _, v := range vs {
				if strings.Contains(v, fixtureToken) && k != "Authorization" {
					t.Errorf("token in header %s", k)
				}
			}
		}
		a.mu.Lock()
		a.seen = append(a.seen, seenReq{Host: r.Host, Path: r.URL.Path, Query: r.URL.RawQuery, Auth: r.Header.Get("Authorization"), Header: r.Header.Clone()})
		a.mu.Unlock()
		a.h(w, r)
	}))
	t.Cleanup(a.srv.Close)
	a.rt = testutil.RedirectTransport(a.srv)
	return a
}

func (a *api) requests() []seenReq {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]seenReq(nil), a.seen...)
}

// client builds a Client over the server; sleeps are recorded and instant.
func (a *api) client() (*Client, *[]time.Duration) {
	a.t.Helper()
	e := testutil.TempEnv(a.t)
	e.HTTP = a.rt
	c, err := NewClient(e)
	if err != nil {
		a.t.Fatal(err)
	}
	var slept []time.Duration
	c.Sleep = func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }
	return c, &slept
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func testEnv(t *testing.T, vars map[string]string) env.Env {
	return testutil.MapEnv(testutil.TempEnv(t), vars)
}

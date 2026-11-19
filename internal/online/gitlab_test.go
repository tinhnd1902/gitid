package online

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func glProjectJSON(full string, extra map[string]any) map[string]any {
	i := strings.LastIndexByte(full, '/')
	m := map[string]any{
		"path_with_namespace": full, "path": full[i+1:], "namespace": map[string]any{"full_path": full[:i]},
		"visibility": "private", "archived": false, "empty_repo": false, "last_activity_at": "2026-11-10T08:00:00.000Z",
		"forked_from_project": nil, "web_url": "https://evil.example/" + full,
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func glServer(t *testing.T, user string, pages [][]map[string]any) *api {
	return newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/user"):
			writeJSON(w, 200, map[string]any{"username": user})
		case strings.HasSuffix(r.URL.Path, "/projects"):
			page := 1
			fmt.Sscanf(r.URL.Query().Get("cursor"), "%d", &page)
			if page == 0 {
				page = 1
			}
			if page < len(pages) {
				q := r.URL.Query()
				q.Set("cursor", fmt.Sprint(page+1))
				w.Header().Set("Link", fmt.Sprintf(`<https://%s%s?%s>; rel="next"`, r.Host, r.URL.Path, q.Encode()))
			}
			writeJSON(w, 200, pages[page-1])
		default:
			http.NotFound(w, r)
		}
	})
}

// GL-1 / GL-2 / GL-4: root, query, clone URL.
func TestGL1To4(t *testing.T) {
	srv := glServer(t, "dave", [][]map[string]any{{glProjectJSON("g/sub/p", nil)}})
	c, _ := srv.client()
	_, repos, _, err := listAll(t, c, glAccount(), tokenOf(SourceGlab))
	if err != nil || len(repos) != 1 {
		t.Fatalf("%v", err)
	}
	reqs := srv.requests()
	if reqs[0].Host != "git.example.com" || reqs[0].Path != "/api/v4/user" || reqs[1].Path != "/api/v4/projects" {
		t.Errorf("requests %+v", reqs)
	}
	q, _ := url.ParseQuery(reqs[1].Query)
	for k, v := range map[string]string{"membership": "true", "per_page": "100", "pagination": "keyset", "order_by": "id", "sort": "asc"} {
		if q.Get(k) != v {
			t.Errorf("query %s=%q, want %q", k, q.Get(k), v)
		}
	}
	for _, r := range reqs {
		if r.Auth != "Bearer "+fixtureToken || r.Header.Get("Accept") != "application/json" {
			t.Errorf("headers %q %q", r.Auth, r.Header.Get("Accept"))
		}
	}
	r := repos[0]
	if r.CloneURL != "https://git.example.com/g/sub/p.git" || r.WebURL != "https://git.example.com/g/sub/p" {
		t.Errorf("urls %q %q", r.CloneURL, r.WebURL)
	}
}

// GL-3 / GL-10: mapping.
func TestGL3Mapping(t *testing.T) {
	pages := [][]map[string]any{{
		glProjectJSON("g/sub/p", map[string]any{"visibility": "internal"}),
		glProjectJSON("g/pub", map[string]any{"visibility": "public", "archived": true}),
		glProjectJSON("g/fork", map[string]any{"forked_from_project": map[string]any{"id": 1}, "empty_repo": true}),
		glProjectJSON("x/noslash", nil),
	}}
	pages[0][3]["path_with_namespace"] = "noslash"
	srv := glServer(t, "dave", pages)
	c, _ := srv.client()
	_, repos, _, err := listAll(t, c, glAccount(), tokenOf(SourceGlab))
	if err != nil || len(repos) != 3 {
		t.Fatalf("%v %d", err, len(repos))
	}
	p, pub, fork := repos[0], repos[1], repos[2]
	if p.Owner != "g/sub" || p.Name != "p" || p.FullPath != "g/sub/p" || !p.Private || p.Archived {
		t.Errorf("p %+v", p)
	}
	if pub.Private || !pub.Archived {
		t.Errorf("pub %+v", pub)
	}
	if !fork.Fork || !fork.Empty {
		t.Errorf("fork %+v", fork)
	}
	if !p.Updated.Equal(time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("updated %v", p.Updated)
	}
}

// GL-5: another user's token.
func TestGL5Identity(t *testing.T) {
	srv := glServer(t, "erin", [][]map[string]any{{}})
	c, _ := srv.client()
	_, _, _, err := listAll(t, c, glAccount(), tokenOf(SourceGlab))
	var oe *Error
	if !errors.As(err, &oe) || oe.Kind != KindWrongUser || oe.Login != "erin" {
		t.Fatalf("%v", err)
	}
	for _, r := range srv.requests() {
		if strings.HasSuffix(r.Path, "/projects") {
			t.Error("projects requested")
		}
	}
}

// GL-6 / GL-7 / GL-8: errors.
func TestGL6To8Errors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		hdr    map[string]string
		body   map[string]any
		want   Kind
	}{
		{"insufficient scope", 403, nil, map[string]any{"error": "insufficient_scope", "error_description": "The request requires higher privileges than provided by the access token.", "scope": "api read_api"}, KindScope},
		{"401", 401, nil, map[string]any{"message": "401 Unauthorized"}, KindAuth},
		{"429", 429, map[string]string{"RateLimit-Reset": "1795000000", "Retry-After": "300"}, map[string]any{"message": "Retry later"}, KindRateLimit},
	}
	for _, c := range cases {
		c := c
		srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
			for k, v := range c.hdr {
				w.Header().Set(k, v)
			}
			writeJSON(w, c.status, c.body)
		})
		cl, slept := srv.client()
		_, _, _, err := listAll(t, cl, glAccount(), tokenOf(SourceGlab))
		var oe *Error
		if !errors.As(err, &oe) || oe.Kind != c.want {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if c.want == KindRateLimit && (len(*slept) != 0 || !oe.Reset.Equal(cl.Now().Add(300*time.Second))) {
			t.Errorf("%s: slept %v reset %v", c.name, *slept, oe.Reset)
		}
	}
	// without Retry-After the epoch of RateLimit-Reset is the reset time
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("RateLimit-Reset", "1795000000")
		writeJSON(w, 429, map[string]any{"message": "x"})
	})
	cl, _ := srv.client()
	_, _, _, err := listAll(t, cl, glAccount(), tokenOf(SourceGlab))
	var oe *Error
	if !errors.As(err, &oe) || oe.Reset.Unix() != 1795000000 {
		t.Errorf("reset %+v", oe)
	}
}

// GL-9: the keyset pagination follows the Link header on the same host.
func TestGL9Pagination(t *testing.T) {
	pages := [][]map[string]any{
		{glProjectJSON("g/a", nil)}, {glProjectJSON("g/b", nil)}, {glProjectJSON("g/c", nil)},
	}
	srv := glServer(t, "dave", pages)
	c, _ := srv.client()
	_, repos, got, err := listAll(t, c, glAccount(), tokenOf(SourceGlab))
	if err != nil || len(repos) != 3 || len(got) != 3 {
		t.Fatalf("%v %d", err, len(repos))
	}
	for _, h := range srv.rt.Hosts() {
		if h != "git.example.com" {
			t.Errorf("request to %s", h)
		}
	}
	// a next link to another host is not followed
	srv = newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/user") {
			writeJSON(w, 200, map[string]any{"username": "dave"})
			return
		}
		w.Header().Set("Link", `<https://evil.example/api/v4/projects?cursor=2>; rel="next"`)
		writeJSON(w, 200, []map[string]any{glProjectJSON("g/a", nil)})
	})
	c, _ = srv.client()
	_, _, _, err = listAll(t, c, glAccount(), tokenOf(SourceGlab))
	if kindOf(err) != KindBadResponse {
		t.Errorf("%v", err)
	}
}

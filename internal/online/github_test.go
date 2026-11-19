package online

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// ghRepoJSON is one repository of the GitHub answer.
func ghRepoJSON(owner, name string, extra map[string]any) map[string]any {
	m := map[string]any{
		"name": name, "full_name": owner + "/" + name, "owner": map[string]any{"login": owner},
		"private": false, "archived": false, "fork": false, "size": 10,
		"pushed_at": "2026-11-15T10:00:00Z", "updated_at": "2026-11-01T10:00:00Z",
		"html_url": "https://evil.example/" + owner + "/" + name, "clone_url": "https://evil.example/" + owner + "/" + name + ".git",
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

// ghServer serves /user and the pages of /user/repos under any API root.
func ghServer(t *testing.T, login string, pages [][]map[string]any, hdr func(h http.Header, page int)) *api {
	t.Helper()
	return newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/user"):
			writeJSON(w, 200, map[string]any{"login": login})
		case strings.HasSuffix(r.URL.Path, "/user/repos"):
			page := 1
			fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
			if hdr != nil {
				hdr(w.Header(), page)
			}
			if page < len(pages) {
				q := r.URL.Query()
				q.Set("page", fmt.Sprint(page+1))
				w.Header().Set("Link", fmt.Sprintf(`<https://%s%s?%s>; rel="next", <https://%s%s?page=%d>; rel="last"`, r.Host, r.URL.Path, q.Encode(), r.Host, r.URL.Path, len(pages)))
			}
			writeJSON(w, 200, pages[page-1])
		default:
			http.NotFound(w, r)
		}
	})
}

func listAll(t *testing.T, c *Client, a Account, tok Token) (Result, []Repo, []Page, error) {
	t.Helper()
	var repos []Repo
	var pages []Page
	res, err := c.List(context.Background(), a, tok, func(p Page) { pages = append(pages, p); repos = append(repos, p.Repos...) })
	return res, repos, pages, err
}

func tokenOf(src Source) Token { return Token{Secret: secret(), Source: src} }

// GH-1: the API root.
func TestGH1BaseURL(t *testing.T) {
	if got := githubBase("github.com"); got != "https://api.github.com" {
		t.Error(got)
	}
	if got := githubBase("GitHub.com"); got != "https://api.github.com" {
		t.Error(got)
	}
	if got := githubBase("ghe.example.com"); got != "https://ghe.example.com/api/v3" {
		t.Error(got)
	}
	srv := ghServer(t, "alice", [][]map[string]any{{}}, nil)
	c, _ := srv.client()
	a := ghAccount()
	if _, _, _, err := listAll(t, c, a, tokenOf(SourceEnv)); err != nil {
		t.Fatal(err)
	}
	a.Host = "ghe.example.com"
	if _, _, _, err := listAll(t, c, a, tokenOf(SourceEnv)); err != nil {
		t.Fatal(err)
	}
	got := srv.rt.Requests()
	want := []string{
		"GET api.github.com/user",
		"GET api.github.com/user/repos?affiliation=owner%2Ccollaborator%2Corganization_member&per_page=100&sort=pushed",
		"GET ghe.example.com/api/v3/user",
		"GET ghe.example.com/api/v3/user/repos?affiliation=owner%2Ccollaborator%2Corganization_member&per_page=100&sort=pushed",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// GH-2 / GH-3: the identity check.
func TestGH2And3Identity(t *testing.T) {
	srv := ghServer(t, "Alice", [][]map[string]any{{ghRepoJSON("acme", "api", nil)}}, nil)
	c, _ := srv.client()
	res, repos, _, err := listAll(t, c, ghAccount(), tokenOf(SourceGH))
	if err != nil || res.Login != "Alice" || len(repos) != 1 {
		t.Fatalf("%+v %v", res, err)
	}

	srv = ghServer(t, "bob", [][]map[string]any{{ghRepoJSON("acme", "api", nil)}}, nil)
	c, _ = srv.client()
	res, repos, _, err = listAll(t, c, ghAccount(), tokenOf(SourceGH))
	var oe *Error
	if !errors.As(err, &oe) || oe.Kind != KindWrongUser || oe.Login != "bob" || res.Login != "bob" || len(repos) != 0 {
		t.Fatalf("%+v %v", oe, err)
	}
	for _, r := range srv.requests() {
		if strings.HasSuffix(r.Path, "/user/repos") {
			t.Error("/user/repos requested for another user's token")
		}
	}
}

// GH-4 / O-10 / O-40: the list query and the headers.
func TestGH4QueryAndHeaders(t *testing.T) {
	srv := ghServer(t, "alice", [][]map[string]any{{}}, nil)
	c, _ := srv.client()
	c.Version = "1.2.3"
	if _, _, _, err := listAll(t, c, ghAccount(), tokenOf(SourceGH)); err != nil {
		t.Fatal(err)
	}
	reqs := srv.requests()
	if len(reqs) != 2 {
		t.Fatalf("%d requests", len(reqs))
	}
	q, _ := url.ParseQuery(reqs[1].Query)
	if q.Get("affiliation") != "owner,collaborator,organization_member" || q.Get("per_page") != "100" || q.Get("sort") != "pushed" {
		t.Errorf("query %v", q)
	}
	for _, r := range reqs {
		if r.Auth != "Bearer "+fixtureToken {
			t.Errorf("Authorization %q", r.Auth)
		}
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("Accept %q", r.Header.Get("Accept"))
		}
		if r.Header.Get("User-Agent") != "gitid/1.2.3" {
			t.Errorf("User-Agent %q", r.Header.Get("User-Agent"))
		}
		if r.Host != "api.github.com" {
			t.Errorf("host %q", r.Host)
		}
	}
}

// GH-5 / GH-6 / O-5: the field mapping, the clone URL from the config, the
// display strings sanitized.
func TestGH5And6Mapping(t *testing.T) {
	pages := [][]map[string]any{{
		ghRepoJSON("acme-org", "api", map[string]any{"private": true, "fork": true}),
		ghRepoJSON("acme-org", "old", map[string]any{"archived": true, "size": 0, "pushed_at": nil}),
		ghRepoJSON("alice", "evil\x1b]0;pwn\x07x", nil),
		ghRepoJSON("alice", "bad name?x=1", nil),
		ghRepoJSON("", "noowner", nil),
	}}
	srv := ghServer(t, "alice", pages, nil)
	c, _ := srv.client()
	_, repos, _, err := listAll(t, c, ghAccount(), tokenOf(SourceGH))
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 4 {
		t.Fatalf("%d repos: %+v", len(repos), repos)
	}
	api, old := repos[0], repos[1]
	if !api.Private || !api.Fork || api.Archived || api.Empty || api.Owner != "acme-org" || api.Name != "api" || api.FullPath != "acme-org/api" {
		t.Errorf("api %+v", api)
	}
	if !api.Updated.Equal(time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("updated %v", api.Updated)
	}
	if api.CloneURL != "https://github.com/acme-org/api.git" || api.WebURL != "https://github.com/acme-org/api" {
		t.Errorf("urls %q %q", api.CloneURL, api.WebURL)
	}
	// size 0 is empty; pushed_at null falls back to updated_at
	if !old.Empty || !old.Archived || !old.Updated.Equal(time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("old %+v", old)
	}
	// O-5: the name reaches the caller sanitized, and cannot be cloned
	if strings.ContainsAny(repos[2].Name, "\x1b\x07") || repos[2].Name != "evil]0;pwnx" && repos[2].Name != "evilx" {
		t.Errorf("name %q", repos[2].Name)
	}
	if repos[2].CloneURL != "" {
		t.Errorf("an odd name got a clone URL %q", repos[2].CloneURL)
	}
	if repos[3].CloneURL != "" || repos[3].WebURL != "" {
		t.Errorf("a name with a space got URLs: %+v", repos[3])
	}
}

// GH-7 / GH-8: the scope warning.
func TestGH7And8Scopes(t *testing.T) {
	cases := []struct {
		hdr  *string
		warn bool
	}{
		{ptr("read:user"), true},
		{ptr("repo, read:org"), false},
		{ptr(""), true},
		{nil, false}, // a fine-grained token sends no header
	}
	for _, c := range cases {
		c := c
		srv := ghServer(t, "alice", [][]map[string]any{{ghRepoJSON("a", "b", nil)}}, func(h http.Header, _ int) {
			if c.hdr != nil {
				h["X-Oauth-Scopes"] = []string{*c.hdr}
			}
		})
		cl, _ := srv.client()
		res, _, pages, err := listAll(t, cl, ghAccount(), tokenOf(SourceGH))
		if err != nil {
			t.Fatal(err)
		}
		got := len(res.Warnings) == 1 && res.Warnings[0] == WarnNoRepoScope && len(pages[0].Warnings) == 1
		if got != c.warn {
			t.Errorf("scopes %v: warnings %v", c.hdr, res.Warnings)
		}
	}
}

func ptr(s string) *string { return &s }

// GH-9: 403 with X-GitHub-SSO: required.
func TestGH9SSORequired(t *testing.T) {
	for _, c := range []struct{ hdr, url string }{
		{"required; url=https://github.com/orgs/x/sso?authorization_request=abc", "https://github.com/orgs/x/sso?authorization_request=abc"},
		{"required; url=http://github.com/orgs/x/sso", ""},          // not https
		{"required; url=https://user:pw@github.com/orgs/x/sso", ""}, // credentials
		{"required", ""},
	} {
		c := c
		srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/user") {
				writeJSON(w, 200, map[string]any{"login": "alice"})
				return
			}
			w.Header().Set("X-GitHub-SSO", c.hdr)
			writeJSON(w, 403, map[string]any{"message": "Resource protected by organization SAML enforcement"})
		})
		cl, _ := srv.client()
		_, _, _, err := listAll(t, cl, ghAccount(), tokenOf(SourceGH))
		var oe *Error
		if !errors.As(err, &oe) || oe.Kind != KindSSO || oe.SSOURL != c.url || oe.Status != 403 {
			t.Errorf("%q: %+v", c.hdr, oe)
		}
	}
}

// GH-10: 200 with partial results.
func TestGH10SSOPartial(t *testing.T) {
	srv := ghServer(t, "alice", [][]map[string]any{{ghRepoJSON("a", "b", nil)}}, func(h http.Header, _ int) {
		h.Set("X-GitHub-SSO", "partial-results; organizations=1,2")
	})
	cl, _ := srv.client()
	res, repos, _, err := listAll(t, cl, ghAccount(), tokenOf(SourceGH))
	if err != nil || len(repos) != 1 || len(res.Warnings) != 1 || res.Warnings[0] != WarnSSOPartial {
		t.Fatalf("%+v %v", res, err)
	}
}

// GH-11: 401 with a one-time code requirement.
func TestGH11OTP(t *testing.T) {
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-GitHub-OTP", "required; sms")
		writeJSON(w, 401, map[string]any{"message": "Must specify two-factor authentication OTP code."})
	})
	cl, _ := srv.client()
	_, _, _, err := listAll(t, cl, ghAccount(), tokenOf(SourceGit))
	var oe *Error
	if !errors.As(err, &oe) || oe.Kind != KindOTP {
		t.Fatalf("%v", err)
	}
}

// GH-13: a secondary rate limit.
func TestGH13SecondaryRateLimit(t *testing.T) {
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, 403, map[string]any{"message": "You have exceeded a secondary rate limit."})
	})
	cl, slept := srv.client()
	_, _, _, err := listAll(t, cl, ghAccount(), tokenOf(SourceGH))
	var oe *Error
	if !errors.As(err, &oe) || oe.Kind != KindRateLimit || len(*slept) != 0 {
		t.Fatalf("%v slept %v", err, *slept)
	}
	if want := cl.Now().Add(60 * time.Second); !oe.Reset.Equal(want) {
		t.Errorf("reset %v, want %v", oe.Reset, want)
	}
}

// GH-14: 1000 repositories over 10 pages, streamed in order.
func TestGH14ThousandRepos(t *testing.T) {
	var pages [][]map[string]any
	for p := 0; p < 10; p++ {
		var pg []map[string]any
		for i := 0; i < 100; i++ {
			pg = append(pg, ghRepoJSON("acme", fmt.Sprintf("r%d-%d", p, i), nil))
		}
		pages = append(pages, pg)
	}
	srv := ghServer(t, "alice", pages, nil)
	cl, _ := srv.client()
	res, repos, got, err := listAll(t, cl, ghAccount(), tokenOf(SourceGH))
	if err != nil || len(repos) != 1000 || res.Truncated || res.Count != 1000 || res.Pages != 10 || len(got) != 10 {
		t.Fatalf("%+v %v %d", res, err, len(repos))
	}
	for i, p := range got {
		if p.N != i+1 {
			t.Errorf("page %d has N=%d", i, p.N)
		}
	}
}

// GH-15: no repositories.
func TestGH15Empty(t *testing.T) {
	srv := ghServer(t, "alice", [][]map[string]any{{}}, nil)
	cl, _ := srv.client()
	res, repos, _, err := listAll(t, cl, ghAccount(), tokenOf(SourceGH))
	if err != nil || len(repos) != 0 || res.Count != 0 {
		t.Fatalf("%+v %v", res, err)
	}
}

// GH-16: a body cut short.
func TestGH16InvalidJSON(t *testing.T) {
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/user") {
			writeJSON(w, 200, map[string]any{"login": "alice"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name": "a", "owner": {"log`))
	})
	cl, _ := srv.client()
	_, _, _, err := listAll(t, cl, ghAccount(), tokenOf(SourceGH))
	var oe *Error
	if !errors.As(err, &oe) || oe.Kind != KindBadResponse {
		t.Fatalf("%v", err)
	}
}

// 4xx/5xx kinds
func TestStatusKinds(t *testing.T) {
	cases := []struct {
		status int
		hdr    map[string]string
		body   map[string]any
		want   Kind
	}{
		{401, nil, map[string]any{"message": "Bad credentials " + fixtureToken}, KindAuth},
		{403, nil, map[string]any{"message": "Resource not accessible by personal access token"}, KindScope},
		{429, nil, map[string]any{"message": "slow down"}, KindRateLimit},
		{500, nil, map[string]any{"message": "boom"}, KindServer},
		{502, nil, nil, KindServer},
		{404, nil, map[string]any{"message": "Not Found"}, KindBadResponse},
		{422, nil, map[string]any{"message": "Validation Failed"}, KindBadResponse},
		{403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1795000000"}, map[string]any{"message": "API rate limit exceeded"}, KindRateLimit},
	}
	for _, c := range cases {
		c := c
		srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
			for k, v := range c.hdr {
				w.Header().Set(k, v)
			}
			if c.body == nil {
				w.WriteHeader(c.status)
				return
			}
			writeJSON(w, c.status, c.body)
		})
		cl, _ := srv.client()
		_, _, _, err := listAll(t, cl, ghAccount(), tokenOf(SourceGH))
		var oe *Error
		if !errors.As(err, &oe) || oe.Kind != c.want || oe.Status != c.status {
			t.Errorf("%d: %+v", c.status, oe)
			continue
		}
		if strings.Contains(err.Error(), fixtureToken) {
			t.Errorf("%d: the error holds the token: %v", c.status, err)
		}
	}
}

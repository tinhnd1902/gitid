package online

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/tinhnd1902/gitid/internal/platform"
)

func gtAccount() Account {
	return Account{Ref: "gt/sam", Slug: "gt", ID: "sam", Platform: platform.Gitea, Host: "gitea.example.com", Username: "sam", Email: "sam@gt.example"}
}

func gtRepoJSON(owner, name string, extra map[string]any) map[string]any {
	m := map[string]any{
		"name": name, "full_name": owner + "/" + name, "owner": map[string]any{"login": owner},
		"private": false, "archived": false, "fork": false, "empty": false,
		"updated_at": "2026-11-15T10:00:00Z",
		"html_url":   "https://evil.example/" + owner + "/" + name, "clone_url": "https://evil.example/" + owner + "/" + name + ".git",
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

// gtServer serves /user, /user/orgs, /user/repos and /orgs/{o}/repos.
// pages are the pages of /user/repos; orgs maps an organization to its repos.
type gtOpts struct {
	login string
	pages [][]map[string]any
	orgs  map[string][]map[string]any
	// noLink: never send a Link header
	noLink bool
	// orgStatus: the status of an organization's repos (default 200)
	orgStatus map[string]int
}

func gtServer(t *testing.T, o gtOpts) *api {
	t.Helper()
	if o.login == "" {
		o.login = "sam"
	}
	return newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/api/v1")
		switch {
		case p == "/user":
			writeJSON(w, 200, map[string]any{"login": o.login})
		case p == "/user/orgs":
			var orgs []map[string]any
			for name := range o.orgs {
				orgs = append(orgs, map[string]any{"username": name})
			}
			writeJSON(w, 200, orgs)
		case p == "/user/repos":
			page := 1
			fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
			if page < 1 || page > len(o.pages) {
				writeJSON(w, 200, []any{})
				return
			}
			if page < len(o.pages) && !o.noLink {
				w.Header().Set("Link", fmt.Sprintf(`<https://%s%s?limit=50&page=%d>; rel="next"`, r.Host, r.URL.Path, page+1))
			} else if !o.noLink {
				w.Header().Set("Link", fmt.Sprintf(`<https://%s%s?limit=50&page=1>; rel="first"`, r.Host, r.URL.Path))
			}
			writeJSON(w, 200, o.pages[page-1])
		case strings.HasPrefix(p, "/orgs/") && strings.HasSuffix(p, "/repos"):
			org := strings.TrimSuffix(strings.TrimPrefix(p, "/orgs/"), "/repos")
			if st := o.orgStatus[org]; st != 0 {
				writeJSON(w, st, map[string]any{"message": "no access"})
				return
			}
			writeJSON(w, 200, o.orgs[org])
		default:
			http.NotFound(w, r)
		}
	})
}

// GT-1: the URLs.
func TestGT1URLs(t *testing.T) {
	srv := gtServer(t, gtOpts{pages: [][]map[string]any{{gtRepoJSON("sam", "dots", nil)}}, orgs: map[string][]map[string]any{"acme-org": {gtRepoJSON("acme-org", "api", nil)}}})
	c, _ := srv.client()
	res, repos, _, err := listAll(t, c, gtAccount(), tokenOf(SourceEnv))
	if err != nil || res.Login != "sam" || len(repos) != 2 {
		t.Fatalf("%+v %v %d", res, err, len(repos))
	}
	want := []string{
		"GET gitea.example.com/api/v1/user",
		"GET gitea.example.com/api/v1/user/repos?limit=50&page=1",
		"GET gitea.example.com/api/v1/user/orgs?limit=50&page=1",
		"GET gitea.example.com/api/v1/orgs/acme-org/repos?limit=50&page=1",
	}
	if got := srv.rt.Requests(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// GT-2: the Authorization header by source.
func TestGT2Auth(t *testing.T) {
	for src, want := range map[Source]string{
		SourceEnv:      "token " + fixtureToken,
		SourceKeychain: "token " + fixtureToken,
		SourceGit:      "Basic " + base64.StdEncoding.EncodeToString([]byte("sam:"+fixtureToken)),
	} {
		srv := gtServer(t, gtOpts{pages: [][]map[string]any{{}}})
		c, _ := srv.client()
		if _, _, _, err := listAll(t, c, gtAccount(), tokenOf(src)); err != nil {
			t.Fatal(err)
		}
		for _, r := range srv.requests() {
			if r.Auth != want {
				t.Errorf("%s: %s sent %q, want %q", src, r.Path, r.Auth, want)
			}
		}
	}
}

// GT-3: Link next is followed; no Link and a short page stops; no Link and a
// full page asks for the next page by number.
func TestGT3Pagination(t *testing.T) {
	srv := gtServer(t, gtOpts{pages: [][]map[string]any{{gtRepoJSON("sam", "a", nil)}, {gtRepoJSON("sam", "b", nil)}, {gtRepoJSON("sam", "c", nil)}}})
	c, _ := srv.client()
	_, repos, pages, err := listAll(t, c, gtAccount(), tokenOf(SourceEnv))
	if err != nil || len(repos) != 3 || len(pages) != 3 {
		t.Fatalf("link: %v %d %d", err, len(repos), len(pages))
	}
	// a short page without any Link header: one request
	srv = gtServer(t, gtOpts{noLink: true, pages: [][]map[string]any{{gtRepoJSON("sam", "a", nil)}}})
	c, _ = srv.client()
	_, repos, _, err = listAll(t, c, gtAccount(), tokenOf(SourceEnv))
	if err != nil || len(repos) != 1 {
		t.Fatal(err)
	}
	n := 0
	for _, r := range srv.rt.Requests() {
		if strings.Contains(r, "/user/repos") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d page requests for a short page", n)
	}
	// a full page without a Link header: page 2 is requested
	var full []map[string]any
	for i := 0; i < giteaLimit; i++ {
		full = append(full, gtRepoJSON("sam", fmt.Sprintf("r%d", i), nil))
	}
	srv = gtServer(t, gtOpts{noLink: true, pages: [][]map[string]any{full, {gtRepoJSON("sam", "last", nil)}}})
	c, _ = srv.client()
	_, repos, _, err = listAll(t, c, gtAccount(), tokenOf(SourceEnv))
	if err != nil || len(repos) != giteaLimit+1 {
		t.Fatalf("%v %d", err, len(repos))
	}
}

// GT-4: 2FA.
func TestGT4OTP(t *testing.T) {
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Gitea-OTP", "required;application")
		writeJSON(w, 401, map[string]any{"message": "otp"})
	})
	c, _ := srv.client()
	_, _, _, err := listAll(t, c, gtAccount(), tokenOf(SourceGit))
	if kindOf(err) != KindOTP {
		t.Fatalf("%v", err)
	}
	// a plain 401
	srv = newAPI(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 401, map[string]any{"message": "bad"}) })
	c, _ = srv.client()
	_, _, _, err = listAll(t, c, gtAccount(), tokenOf(SourceEnv))
	if kindOf(err) != KindAuth {
		t.Fatalf("%v", err)
	}
}

// GT-5 / GT-8: the mapping and the clone URL (built from the account's host).
func TestGT5And8Mapping(t *testing.T) {
	srv := gtServer(t, gtOpts{pages: [][]map[string]any{{
		gtRepoJSON("sam", "full", map[string]any{"private": true, "archived": true, "fork": true, "empty": true}),
		gtRepoJSON("sam", "plain", nil),
	}}})
	c, _ := srv.client()
	_, repos, _, err := listAll(t, c, gtAccount(), tokenOf(SourceEnv))
	if err != nil || len(repos) != 2 {
		t.Fatal(err)
	}
	f := repos[0]
	if !f.Private || !f.Archived || !f.Fork || !f.Empty || f.Updated.IsZero() || f.FullPath != "sam/full" {
		t.Errorf("%+v", f)
	}
	if repos[1].Private || repos[1].Archived || repos[1].Fork || repos[1].Empty {
		t.Errorf("%+v", repos[1])
	}
	for _, r := range repos {
		if r.CloneURL != "https://gitea.example.com/sam/"+r.Name+".git" || r.WebURL != "https://gitea.example.com/sam/"+r.Name {
			t.Errorf("urls %q %q (the API sent evil.example)", r.CloneURL, r.WebURL)
		}
	}
}

// GT-6: no host; and a host that would need plain http.
func TestGT6NoHost(t *testing.T) {
	srv := gtServer(t, gtOpts{})
	c, _ := srv.client()
	a := gtAccount()
	a.Host = ""
	_, _, _, err := listAll(t, c, a, tokenOf(SourceEnv))
	if kindOf(err) != KindUnsupported {
		t.Fatalf("%v", err)
	}
	a.Host = "http://gitea.example.com"
	_, _, _, err = listAll(t, c, a, tokenOf(SourceEnv))
	if kindOf(err) != KindInsecureHost {
		t.Fatalf("%v", err)
	}
	if len(srv.requests()) != 0 {
		t.Error("a request was made")
	}
}

// GT-7: another user's token lists nothing.
func TestGT7WrongUser(t *testing.T) {
	srv := gtServer(t, gtOpts{login: "mallory", pages: [][]map[string]any{{gtRepoJSON("mallory", "x", nil)}}})
	c, _ := srv.client()
	res, repos, _, err := listAll(t, c, gtAccount(), tokenOf(SourceEnv))
	var oe *Error
	if !errors.As(err, &oe) || oe.Kind != KindWrongUser || oe.Login != "mallory" || res.Login != "mallory" || len(repos) != 0 {
		t.Fatalf("%v", err)
	}
	if len(srv.requests()) != 1 {
		t.Errorf("%d requests", len(srv.requests()))
	}
}

// One organization that answers 403 is a warning; the others are listed and
// a repository seen twice (own and organization) once. Hostile organization
// names are never put in a URL.
func TestGTOrgs(t *testing.T) {
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/api/v1")
		switch {
		case p == "/user":
			writeJSON(w, 200, map[string]any{"login": "sam"})
		case p == "/user/repos":
			writeJSON(w, 200, []map[string]any{gtRepoJSON("sam", "own", nil), gtRepoJSON("good", "shared", nil)})
		case p == "/user/orgs":
			writeJSON(w, 200, []map[string]any{{"username": "good"}, {"username": "locked"}, {"username": "../x"}, {"username": "a/b"}, {"username": ".git"}, {"username": "ev\x1b[2Jil"}})
		case p == "/orgs/good/repos":
			writeJSON(w, 200, []map[string]any{gtRepoJSON("good", "shared", nil), gtRepoJSON("good", "other", nil)})
		case p == "/orgs/locked/repos":
			writeJSON(w, 403, map[string]any{"message": "forbidden"})
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	})
	c, _ := srv.client()
	res, repos, _, err := listAll(t, c, gtAccount(), tokenOf(SourceEnv))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range repos {
		names = append(names, r.FullPath)
	}
	if strings.Join(names, " ") != "sam/own good/shared good/other" {
		t.Errorf("repos %v", names)
	}
	if len(res.Warnings) != 1 || res.Warnings[0] != "org_failed:locked" {
		t.Errorf("warnings %v", res.Warnings)
	}
}

// Gitea: the organizations cannot be listed -> a warning, the own repos stay.
func TestGTOrgsListFails(t *testing.T) {
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/api/v1")
		switch p {
		case "/user":
			writeJSON(w, 200, map[string]any{"login": "sam"})
		case "/user/repos":
			writeJSON(w, 200, []map[string]any{gtRepoJSON("sam", "own", nil)})
		default:
			writeJSON(w, 403, map[string]any{"message": "scope"})
		}
	})
	c, _ := srv.client()
	res, repos, _, err := listAll(t, c, gtAccount(), tokenOf(SourceEnv))
	if err != nil || len(repos) != 1 || len(res.Warnings) != 1 || res.Warnings[0] != WarnOrgsList {
		t.Fatalf("%v %v %d", err, res.Warnings, len(repos))
	}
}

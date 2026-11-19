package online

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/tinhnd1902/gitid/internal/platform"
)

func bbAccount() Account {
	return Account{Ref: "bb/joe", Slug: "bb", ID: "joe", Platform: platform.Bitbucket, Host: "bitbucket.org", Username: "joe", Email: "joe@bb.example"}
}

func bbRepoJSON(full string, extra map[string]any) map[string]any {
	m := map[string]any{
		"full_name": full, "is_private": false, "updated_on": "2026-11-15T10:00:00.123456+00:00",
		"size": 100, "mainbranch": map[string]any{"name": "main"},
		"links": map[string]any{"html": map[string]any{"href": "https://evil.example/" + full}},
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

type bbOpts struct {
	nick       string
	workspaces []string
	repos      map[string][][]map[string]any // workspace -> pages
	wsStatus   map[string]int
	// accept decides which Authorization header /user accepts (default: any)
	accept func(auth string) bool
}

func bbServer(t *testing.T, o bbOpts) *api {
	t.Helper()
	if o.nick == "" {
		o.nick = "joe"
	}
	return newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if o.accept != nil && !o.accept(r.Header.Get("Authorization")) {
			writeJSON(w, 401, map[string]any{"type": "error", "error": map[string]any{"message": "bad credentials"}})
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/2.0")
		switch {
		case p == "/user":
			writeJSON(w, 200, map[string]any{"nickname": o.nick, "account_id": "123"})
		case p == "/user/workspaces":
			var vs []map[string]any
			for _, ws := range o.workspaces {
				vs = append(vs, map[string]any{"workspace": map[string]any{"slug": ws}})
			}
			writeJSON(w, 200, map[string]any{"pagelen": 100, "values": vs})
		case strings.HasPrefix(p, "/repositories/"):
			ws := strings.TrimPrefix(p, "/repositories/")
			if st := o.wsStatus[ws]; st != 0 {
				writeJSON(w, st, map[string]any{"type": "error", "error": map[string]any{"message": "no"}})
				return
			}
			pages := o.repos[ws]
			page := 1
			fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
			if page < 1 || page > len(pages) {
				writeJSON(w, 200, map[string]any{"values": []any{}})
				return
			}
			body := map[string]any{"pagelen": 100, "values": pages[page-1]}
			if page < len(pages) {
				body["next"] = fmt.Sprintf("https://%s%s?role=member&pagelen=100&page=%d", r.Host, r.URL.Path, page+1)
			}
			writeJSON(w, 200, body)
		default:
			http.NotFound(w, r)
		}
	})
}

// BB-1: the URLs. The old /repositories?role=member is not used; the repos of
// each workspace of the user are listed.
func TestBB1URLs(t *testing.T) {
	srv := bbServer(t, bbOpts{workspaces: []string{"ws1", "ws2"}, repos: map[string][][]map[string]any{
		"ws1": {{bbRepoJSON("ws1/api", nil)}}, "ws2": {{bbRepoJSON("ws2/web", nil)}},
	}})
	c, _ := srv.client()
	res, repos, _, err := listAll(t, c, bbAccount(), tokenOf(SourceEnv))
	if err != nil || len(repos) != 2 || res.Login != "joe" {
		t.Fatalf("%+v %v", res, err)
	}
	want := []string{
		"GET api.bitbucket.org/2.0/user",
		"GET api.bitbucket.org/2.0/user/workspaces?pagelen=100",
		"GET api.bitbucket.org/2.0/repositories/ws1?role=member&pagelen=100",
		"GET api.bitbucket.org/2.0/repositories/ws2?role=member&pagelen=100",
	}
	if got := srv.rt.Requests(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, h := range srv.rt.Hosts() {
		if h != "api.bitbucket.org" {
			t.Errorf("a request went to %s", h)
		}
	}
}

// BB-2: the JSON next is followed, on the API host only.
func TestBB2Pagination(t *testing.T) {
	srv := bbServer(t, bbOpts{workspaces: []string{"ws"}, repos: map[string][][]map[string]any{
		"ws": {{bbRepoJSON("ws/a", nil)}, {bbRepoJSON("ws/b", nil)}, {bbRepoJSON("ws/c", nil)}},
	}})
	c, _ := srv.client()
	_, repos, pages, err := listAll(t, c, bbAccount(), tokenOf(SourceEnv))
	if err != nil || len(repos) != 3 || len(pages) != 3 || pages[2].N != 3 {
		t.Fatalf("%v %d", err, len(repos))
	}
}

// BB-3: with an email: Basic(email), then one retry with Bearer; then Auth.
// Exactly two requests.
func TestBB3Auth(t *testing.T) {
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("joe@bb.example:"+fixtureToken))
	bearer := "Bearer " + fixtureToken

	srv := bbServer(t, bbOpts{workspaces: []string{"ws"}, repos: map[string][][]map[string]any{"ws": {{}}}, accept: func(a string) bool { return a == bearer }})
	c, _ := srv.client()
	if _, _, _, err := listAll(t, c, bbAccount(), tokenOf(SourceEnv)); err != nil {
		t.Fatal(err)
	}
	var auths []string
	for _, r := range srv.requests() {
		auths = append(auths, r.Auth)
	}
	if auths[0] != basic || auths[1] != bearer {
		t.Errorf("first two: %q", auths[:2])
	}
	for _, a := range auths[2:] {
		if a != bearer {
			t.Errorf("a later request used %q", a)
		}
	}

	srv = bbServer(t, bbOpts{accept: func(string) bool { return false }})
	c, _ = srv.client()
	_, _, _, err := listAll(t, c, bbAccount(), tokenOf(SourceEnv))
	if kindOf(err) != KindAuth || len(srv.requests()) != 2 {
		t.Fatalf("%v, %d requests", err, len(srv.requests()))
	}

	// no email: Bearer, then Basic(username)
	a := bbAccount()
	a.Email = ""
	srv = bbServer(t, bbOpts{accept: func(string) bool { return false }})
	c, _ = srv.client()
	_, _, _, err = listAll(t, c, a, tokenOf(SourceEnv))
	rs := srv.requests()
	if kindOf(err) != KindAuth || len(rs) != 2 || rs[0].Auth != bearer ||
		rs[1].Auth != "Basic "+base64.StdEncoding.EncodeToString([]byte("joe:"+fixtureToken)) {
		t.Fatalf("%v %v", err, rs)
	}
}

// BB-4: the mapping. There is no archived flag.
func TestBB4Mapping(t *testing.T) {
	srv := bbServer(t, bbOpts{workspaces: []string{"ws"}, repos: map[string][][]map[string]any{"ws": {{
		bbRepoJSON("ws/priv", map[string]any{"is_private": true}),
		bbRepoJSON("ws/forked", map[string]any{"parent": map[string]any{"full_name": "x/y"}}),
		bbRepoJSON("ws/empty", map[string]any{"size": 0, "mainbranch": nil}),
	}}}})
	c, _ := srv.client()
	_, repos, _, err := listAll(t, c, bbAccount(), tokenOf(SourceEnv))
	if err != nil || len(repos) != 3 {
		t.Fatal(err)
	}
	if !repos[0].Private || repos[0].Archived || repos[0].Fork || repos[0].Empty || repos[0].Updated.IsZero() {
		t.Errorf("%+v", repos[0])
	}
	if !repos[1].Fork || !repos[2].Empty {
		t.Errorf("%+v %+v", repos[1], repos[2])
	}
}

// BB-5: the clone URL comes from the account's host, not from the answer.
func TestBB5CloneURL(t *testing.T) {
	srv := bbServer(t, bbOpts{workspaces: []string{"ws"}, repos: map[string][][]map[string]any{"ws": {{bbRepoJSON("ws/api", nil)}}}})
	c, _ := srv.client()
	_, repos, _, err := listAll(t, c, bbAccount(), tokenOf(SourceEnv))
	if err != nil || repos[0].CloneURL != "https://bitbucket.org/ws/api.git" || repos[0].WebURL != "https://bitbucket.org/ws/api" {
		t.Fatalf("%v %+v", err, repos)
	}
}

// BB-6: a Bitbucket account on another host (Data Center) is not listed.
func TestBB6NonCloudHost(t *testing.T) {
	srv := bbServer(t, bbOpts{})
	c, _ := srv.client()
	a := bbAccount()
	a.Host = "bitbucket.corp.example"
	_, _, _, err := listAll(t, c, a, tokenOf(SourceEnv))
	if kindOf(err) != KindUnsupported || len(srv.requests()) != 0 {
		t.Fatalf("%v", err)
	}
}

// BB-7: the identity check.
func TestBB7WrongUser(t *testing.T) {
	srv := bbServer(t, bbOpts{nick: "mallory", workspaces: []string{"ws"}, repos: map[string][][]map[string]any{"ws": {{bbRepoJSON("ws/x", nil)}}}})
	c, _ := srv.client()
	res, repos, _, err := listAll(t, c, bbAccount(), tokenOf(SourceEnv))
	if kindOf(err) != KindWrongUser || res.Login != "mallory" || len(repos) != 0 || len(srv.requests()) != 1 {
		t.Fatalf("%v", err)
	}
	// a case difference is the same user
	srv = bbServer(t, bbOpts{nick: "JOE", workspaces: nil})
	c, _ = srv.client()
	if _, _, _, err := listAll(t, c, bbAccount(), tokenOf(SourceEnv)); err != nil {
		t.Fatal(err)
	}
}

// BB-8: 429.
func TestBB8RateLimit(t *testing.T) {
	srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3600")
		writeJSON(w, 429, map[string]any{"error": map[string]any{"message": "slow down"}})
	})
	c, slept := srv.client()
	_, _, _, err := listAll(t, c, bbAccount(), tokenOf(SourceEnv))
	var oe *Error
	if kindOf(err) != KindRateLimit || len(*slept) != 0 {
		t.Fatalf("%v %v", err, *slept)
	}
	_ = oe
}

// A workspace that answers 403 is a warning; the others are listed. When
// every workspace fails, the error is returned.
func TestBBWorkspaceFails(t *testing.T) {
	srv := bbServer(t, bbOpts{workspaces: []string{"ok", "locked"}, wsStatus: map[string]int{"locked": 403},
		repos: map[string][][]map[string]any{"ok": {{bbRepoJSON("ok/a", nil)}}}})
	c, _ := srv.client()
	res, repos, _, err := listAll(t, c, bbAccount(), tokenOf(SourceEnv))
	if err != nil || len(repos) != 1 || len(res.Warnings) != 1 || res.Warnings[0] != "ws_failed:locked" {
		t.Fatalf("%v %v", err, res.Warnings)
	}
	srv = bbServer(t, bbOpts{workspaces: []string{"locked"}, wsStatus: map[string]int{"locked": 403}})
	c, _ = srv.client()
	if _, _, _, err := listAll(t, c, bbAccount(), tokenOf(SourceEnv)); kindOf(err) != KindScope {
		t.Fatalf("%v", err)
	}
}

// Workspace slugs from the answer are checked before they go in a URL.
func TestBBHostileWorkspaces(t *testing.T) {
	srv := bbServer(t, bbOpts{workspaces: []string{"../x", "a/b", "..", ".git", "ev\x1b[2Jil", "good"},
		repos: map[string][][]map[string]any{"good": {{bbRepoJSON("good/a", nil)}}}})
	c, _ := srv.client()
	_, repos, _, err := listAll(t, c, bbAccount(), tokenOf(SourceEnv))
	if err != nil || len(repos) != 1 {
		t.Fatalf("%v", err)
	}
	for _, r := range srv.rt.Requests() {
		if strings.Contains(r, "..") || strings.Contains(r, "%2F") || strings.Contains(r, "\x1b") || strings.Contains(r, ".git") {
			t.Errorf("hostile request %s", r)
		}
	}
}

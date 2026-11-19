package online

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/tinhnd1902/gitid/internal/platform"
	"github.com/tinhnd1902/gitid/internal/remote"
)

func azAccount(orgs ...string) Account {
	return Account{Ref: "az/dev", Slug: "az", ID: "dev", Platform: platform.AzureDevOps, Host: "dev.azure.com", Username: "dev", Orgs: orgs}
}

func azRepoJSON(project, name string, extra map[string]any) map[string]any {
	m := map[string]any{
		"id": "5febef5a-833d-4e14-b9c0-14cb638f91e6", "name": name, "size": 1234,
		"project":   map[string]any{"name": project},
		"remoteUrl": "https://evil.example/" + name, "webUrl": "https://evil.example/" + name,
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

// azServer serves the repository lists per organization (orgs), the profile
// and the accounts of the user (member).
func azServer(t *testing.T, orgs map[string][]map[string]any, member []string) *api {
	t.Helper()
	return newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "app.vssps.visualstudio.com" && r.URL.Path == "/_apis/profile/profiles/me":
			writeJSON(w, 200, map[string]any{"id": "d6245f20-2af8-44f4-9451-8107cb2767db"})
		case r.Host == "app.vssps.visualstudio.com" && r.URL.Path == "/_apis/accounts":
			var vs []map[string]any
			for _, m := range member {
				vs = append(vs, map[string]any{"accountName": m})
			}
			writeJSON(w, 200, map[string]any{"count": len(vs), "value": vs})
		case r.Host == "dev.azure.com" && strings.HasSuffix(r.URL.Path, "/_apis/git/repositories"):
			org := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")[0]
			repos, ok := orgs[org]
			if !ok {
				writeJSON(w, 404, map[string]any{"message": "no such organization"})
				return
			}
			writeJSON(w, 200, map[string]any{"count": len(repos), "value": repos})
		default:
			http.NotFound(w, r)
		}
	})
}

// AZ-1 / AZ-10: one GET per configured organization, no identity call.
func TestAZ1OrgsAndNoIdentity(t *testing.T) {
	srv := azServer(t, map[string][]map[string]any{
		"acme":  {azRepoJSON("proj", "repo", nil)},
		"other": {azRepoJSON("p2", "r2", nil)},
	}, nil)
	c, _ := srv.client()
	res, repos, pages, err := listAll(t, c, azAccount("acme", "other"), tokenOf(SourceEnv))
	if err != nil || len(repos) != 2 || len(pages) != 2 || res.Login != "" {
		t.Fatalf("%+v %v", res, err)
	}
	want := []string{
		"GET dev.azure.com/acme/_apis/git/repositories?api-version=7.1",
		"GET dev.azure.com/other/_apis/git/repositories?api-version=7.1",
	}
	if got := srv.rt.Requests(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s", strings.Join(got, "\n"))
	}
}

// AZ-2 / AZ-3: a PAT is Basic with an empty user, a JWT is Bearer.
func TestAZ2And3Auth(t *testing.T) {
	srv := azServer(t, map[string][]map[string]any{"acme": {}}, nil)
	c, _ := srv.client()
	if _, _, _, err := listAll(t, c, azAccount("acme"), tokenOf(SourceEnv)); err != nil {
		t.Fatal(err)
	}
	if got, want := srv.requests()[0].Auth, "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+fixtureToken)); got != want {
		t.Errorf("PAT: %q want %q", got, want)
	}

	// a Microsoft Entra token; the secrecy scan of newAPI looks for
	// fixtureToken, so this test uses its own token
	jwt := "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiIxIn0.c2ln"
	srv = azServer(t, map[string][]map[string]any{"acme": {}}, nil)
	c, _ = srv.client()
	if _, _, _, err := listAll(t, c, azAccount("acme"), Token{Secret: NewSecret(jwt), Source: SourceEnv}); err != nil {
		t.Fatal(err)
	}
	if got := srv.requests()[0].Auth; got != "Bearer "+jwt {
		t.Errorf("JWT: %q", got)
	}
	// two dots but not eyJ, and eyJ without three parts, are PATs
	for _, tok := range []string{"abc.def.ghi", "eyJonly.twoparts"} {
		srv = azServer(t, map[string][]map[string]any{"acme": {}}, nil)
		c, _ = srv.client()
		if _, _, _, err := listAll(t, c, azAccount("acme"), Token{Secret: NewSecret(tok), Source: SourceEnv}); err != nil {
			t.Fatal(err)
		}
		if got := srv.requests()[0].Auth; !strings.HasPrefix(got, "Basic ") {
			t.Errorf("%s: %q", tok, got)
		}
	}
}

// AZ-4: a rejected PAT gets the sign-in page (203, or a redirect to the login
// host, or a 200 HTML page).
func TestAZ4SignInPage(t *testing.T) {
	page := "<html>Sign in " + fixtureToken + "</html>"
	for name, h := range map[string]http.HandlerFunc{
		"203": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(203)
			_, _ = io.WriteString(w, page)
		},
		"redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://login.microsoftonline.com/x?t="+fixtureToken, http.StatusFound)
		},
		"200 html": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, page)
		},
	} {
		srv := newAPI(t, h)
		c, _ := srv.client()
		_, repos, _, err := listAll(t, c, azAccount("acme"), tokenOf(SourceEnv))
		// every organization failed: the error is returned
		var oe *Error
		if kindOf(err) != KindBadResponse || len(repos) != 0 {
			t.Errorf("%s: %v", name, err)
			continue
		}
		oe = err.(*Error)
		if oe.Detail != AzureSignInHint {
			t.Errorf("%s: detail %q", name, oe.Detail)
		}
		noTokenIn(t, name, err)
	}
}

// AZ-5: the mapping.
func TestAZ5Mapping(t *testing.T) {
	srv := azServer(t, map[string][]map[string]any{"acme": {
		azRepoJSON("proj", "ok", nil),
		azRepoJSON("proj", "off", map[string]any{"isDisabled": true}),
		azRepoJSON("proj", "empty", map[string]any{"size": 0}),
		azRepoJSON("proj", "forked", map[string]any{"isFork": true}),
	}}, nil)
	c, _ := srv.client()
	_, repos, _, err := listAll(t, c, azAccount("acme"), tokenOf(SourceEnv))
	if err != nil || len(repos) != 4 {
		t.Fatal(err)
	}
	r := repos[0]
	if r.Owner != "acme/proj" || r.Name != "ok" || r.FullPath != "acme/proj/ok" || !r.Updated.IsZero() || r.Archived || r.Empty || r.Private {
		t.Errorf("%+v", r)
	}
	if !repos[1].Archived || !repos[2].Empty || !repos[3].Fork {
		t.Errorf("flags: %+v", repos[1:])
	}
}

// AZ-6 / AZ-9: the clone URL is built from the organization, project and
// repository; a name with a space is percent-encoded and still parses.
func TestAZ6And9CloneURL(t *testing.T) {
	srv := azServer(t, map[string][]map[string]any{"acme": {
		azRepoJSON("proj", "repo", nil),
		azRepoJSON("My Proj", "my repo", nil),
		azRepoJSON("proj", "ünï", nil),
	}}, nil)
	c, _ := srv.client()
	_, repos, _, err := listAll(t, c, azAccount("acme"), tokenOf(SourceEnv))
	if err != nil || len(repos) != 3 {
		t.Fatal(err)
	}
	if repos[0].CloneURL != "https://dev.azure.com/acme/proj/_git/repo" || repos[0].WebURL != repos[0].CloneURL {
		t.Errorf("%q", repos[0].CloneURL)
	}
	sp := repos[1]
	if sp.Name != "my repo" || sp.Owner != "acme/My Proj" {
		t.Errorf("the name is shown decoded: %+v", sp)
	}
	if sp.CloneURL != "https://dev.azure.com/acme/My%20Proj/_git/my%20repo" {
		t.Errorf("%q", sp.CloneURL)
	}
	for _, r := range repos {
		rem, err := remote.Parse(r.CloneURL)
		if err != nil || !rem.Azure || rem.AzOrg != "acme" || rem.Repo == "" {
			t.Errorf("remote.Parse(%q) = %+v, %v", r.CloneURL, rem, err)
		}
		if _, err := url.Parse(r.CloneURL); err != nil {
			t.Error(err)
		}
	}
}

// AZ-7: without orgs, the profile and the accounts are asked for.
func TestAZ7DiscoverOrgs(t *testing.T) {
	srv := azServer(t, map[string][]map[string]any{
		"acme": {azRepoJSON("p", "r", nil)}, "beta": {azRepoJSON("q", "s", nil)},
	}, []string{"acme", "beta", "../evil", "ev\x1b[2Jil"})
	c, _ := srv.client()
	_, repos, _, err := listAll(t, c, azAccount(), tokenOf(SourceEnv))
	if err != nil || len(repos) != 2 {
		t.Fatalf("%v", err)
	}
	want := []string{
		"GET app.vssps.visualstudio.com/_apis/profile/profiles/me?api-version=7.1",
		"GET app.vssps.visualstudio.com/_apis/accounts?api-version=7.1&memberId=d6245f20-2af8-44f4-9451-8107cb2767db",
		"GET dev.azure.com/acme/_apis/git/repositories?api-version=7.1",
		"GET dev.azure.com/beta/_apis/git/repositories?api-version=7.1",
	}
	got := srv.rt.Requests()
	if len(got) != 4 || got[0] != want[0] || got[2] != want[2] || got[3] != want[3] || !strings.HasPrefix(got[1], "GET app.vssps.visualstudio.com/_apis/accounts?") ||
		!strings.Contains(got[1], "memberId=d6245f20-2af8-44f4-9451-8107cb2767db") {
		t.Errorf("requests:\n%s", strings.Join(got, "\n"))
	}
	// no organization at all: a warning, no error
	srv = azServer(t, nil, nil)
	c, _ = srv.client()
	res, repos, _, err := listAll(t, c, azAccount(), tokenOf(SourceEnv))
	if err != nil || len(repos) != 0 || len(res.Warnings) != 1 || res.Warnings[0] != WarnNoOrgs {
		t.Fatalf("%v %v", err, res.Warnings)
	}
	// a PAT that the profile service refuses (it may need an Entra token)
	srv = newAPI(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 401, map[string]any{"message": "no"}) })
	c, _ = srv.client()
	if _, _, _, err := listAll(t, c, azAccount(), tokenOf(SourceEnv)); kindOf(err) != KindAuth {
		t.Fatalf("%v", err)
	}
}

// AZ-8: one organization fails, the other is listed with a warning.
func TestAZ8PartialFailure(t *testing.T) {
	srv := azServer(t, map[string][]map[string]any{"good": {azRepoJSON("p", "r", nil)}}, nil)
	c, _ := srv.client()
	res, repos, pages, err := listAll(t, c, azAccount("missing", "good"), tokenOf(SourceEnv))
	if err != nil || len(repos) != 1 || len(res.Warnings) != 1 || res.Warnings[0] != "org_failed:missing" {
		t.Fatalf("%v %v", err, res.Warnings)
	}
	if len(pages) != 2 || pages[0].Warnings[0] != "org_failed:missing" {
		t.Errorf("%+v", pages)
	}
	// all fail: the error
	if _, _, _, err := listAll(t, c, azAccount("missing"), tokenOf(SourceEnv)); kindOf(err) != KindBadResponse {
		t.Fatalf("%v", err)
	}
}

// Another host than dev.azure.com is not listed (Azure DevOps Server).
func TestAZNonCloudHost(t *testing.T) {
	srv := azServer(t, nil, nil)
	c, _ := srv.client()
	a := azAccount("acme")
	a.Host = "tfs.corp.example"
	if _, _, _, err := listAll(t, c, a, tokenOf(SourceEnv)); kindOf(err) != KindUnsupported || len(srv.requests()) != 0 {
		t.Fatalf("%v", err)
	}
}

package online

// Adversarial tests for Gitea, Bitbucket Cloud and Azure DevOps (C1): the same
// hostile platform as in adversarial_test.go, run against each provider. The
// fixture token must appear in no error text, no repository and no result;
// nothing is requested from a host other than the provider's; no clone URL is
// built from a hostile name.

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

type advProv struct {
	name  string
	acct  Account
	hosts []string // the hosts a request may go to
	// serve answers every request of a normal listing; repos are (owner, name)
	// pairs. For Azure the owner is the project (the organization is acme).
	serve func(repos [][2]string) http.HandlerFunc
}

func (p advProv) allowed(h string) bool {
	for _, x := range p.hosts {
		if x == h {
			return true
		}
	}
	return false
}

var advProviders = []advProv{
	{
		name: "gitea", acct: gtAccount(), hosts: []string{"gitea.example.com"},
		serve: func(repos [][2]string) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				switch strings.TrimPrefix(r.URL.Path, "/api/v1") {
				case "/user":
					writeJSON(w, 200, map[string]any{"login": "sam"})
				case "/user/orgs":
					writeJSON(w, 200, []any{})
				default:
					var rs []map[string]any
					for _, p := range repos {
						rs = append(rs, gtRepoJSON(p[0], p[1], nil))
					}
					writeJSON(w, 200, rs)
				}
			}
		},
	},
	{
		name: "bitbucket", acct: bbAccount(), hosts: []string{"api.bitbucket.org"},
		serve: func(repos [][2]string) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				switch p := strings.TrimPrefix(r.URL.Path, "/2.0"); {
				case p == "/user":
					writeJSON(w, 200, map[string]any{"nickname": "joe"})
				case p == "/user/workspaces":
					writeJSON(w, 200, map[string]any{"values": []map[string]any{{"workspace": map[string]any{"slug": "ws"}}}})
				default:
					var rs []map[string]any
					for _, p := range repos {
						rs = append(rs, bbRepoJSON(p[0]+"/"+p[1], nil))
					}
					writeJSON(w, 200, map[string]any{"values": rs})
				}
			}
		},
	},
	{
		name: "azure", acct: azAccount("acme"), hosts: []string{"dev.azure.com"},
		serve: func(repos [][2]string) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				var rs []map[string]any
				for _, p := range repos {
					rs = append(rs, azRepoJSON(p[0], p[1], nil))
				}
				writeJSON(w, 200, map[string]any{"value": rs})
			}
		},
	},
}

func (p advProv) check(t *testing.T, srv *api) {
	t.Helper()
	for _, h := range srv.rt.Hosts() {
		if !p.allowed(h) {
			t.Errorf("%s: a request went to %q", p.name, h)
		}
	}
}

func (p advProv) list(t *testing.T, srv *api) (Result, []Repo, error) {
	t.Helper()
	c, _ := srv.client()
	res, repos, _, err := listAll(t, c, p.acct, tokenOf(SourceEnv))
	return res, repos, err
}

// The server echoes the token in its error body and in every header, in every
// failure status.
func TestAdvProvidersServerEchoesToken(t *testing.T) {
	for _, p := range advProviders {
		for _, st := range []int{400, 401, 403, 404, 422, 429, 500, 502, 503} {
			p, st := p, st
			t.Run(fmt.Sprintf("%s/%d", p.name, st), func(t *testing.T) {
				srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
					h := w.Header()
					h.Set("X-Echo", fixtureToken)
					h.Set("Location", "http://evil.example/login?access_token="+fixtureToken)
					h.Set("WWW-Authenticate", `Basic realm="`+fixtureToken+`"`)
					h.Set("Link", "<https://evil.example/?t="+fixtureToken+`>; rel="next"`)
					h.Set("Retry-After", "7200")
					writeJSON(w, st, map[string]any{
						"message": "bad " + fixtureToken + " here", "error": map[string]any{"message": fixtureToken},
						"error_description": "token " + fixtureToken,
					})
				})
				res, repos, err := p.list(t, srv)
				if err == nil {
					t.Fatal("want an error")
				}
				noTokenIn(t, p.name, err)
				noTokenIn(t, p.name, res)
				noTokenIn(t, p.name, repos)
				p.check(t, srv)
			})
		}
	}
}

// The token echoed where the client takes text from a successful answer.
func TestAdvProvidersServerEchoesTokenInData(t *testing.T) {
	for _, p := range advProviders {
		p := p
		t.Run(p.name, func(t *testing.T) {
			srv := newAPI(t, p.serve([][2]string{{fixtureToken, "r"}, {"ws", fixtureToken}, {"acme", "ok"}}))
			res, repos, err := p.list(t, srv)
			if err != nil {
				t.Fatal(err)
			}
			noTokenIn(t, p.name, repos)
			noTokenIn(t, p.name, res)
			for _, r := range repos {
				if strings.Contains(r.FullPath, "***") && r.CloneURL != "" {
					t.Errorf("a redacted name got a clone URL: %+v", r)
				}
			}
		})
	}
	t.Run("wrong user login", func(t *testing.T) {
		for _, p := range advProviders[:2] {
			srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, 200, map[string]any{"login": fixtureToken, "nickname": fixtureToken, "username": fixtureToken})
			})
			res, _, err := p.list(t, srv)
			if kindOf(err) != KindWrongUser {
				t.Fatalf("%s: %v", p.name, err)
			}
			noTokenIn(t, p.name, err)
			noTokenIn(t, p.name, res)
		}
	})
}

// A redirect off the host, to http:// or to a login page is refused and not
// followed; nothing is sent there.
func TestAdvProvidersRedirect(t *testing.T) {
	for _, p := range advProviders {
		for _, loc := range []string{
			"http://" + p.hosts[0] + "/user?t=" + fixtureToken,
			"http://evil.example/x",
			"https://evil.example/x",
			"//evil.example/x",
			"ftp://" + p.hosts[0] + "/x",
		} {
			p, loc := p, loc
			t.Run(p.name+" "+loc, func(t *testing.T) {
				srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, loc, http.StatusFound) })
				_, _, err := p.list(t, srv)
				if kindOf(err) != KindBadResponse {
					t.Fatalf("%v", err)
				}
				noTokenIn(t, loc, err)
				if n := len(srv.requests()); n != 1 {
					t.Errorf("%d requests (a redirect was followed)", n)
				}
				p.check(t, srv)
			})
		}
	}
}

// A "next" link to another host, plain http, a look-alike or with credentials
// is never requested; the first page is kept. Gitea sends a Link header,
// Bitbucket a "next" member of the body.
func TestAdvProvidersNextOtherHost(t *testing.T) {
	for _, p := range advProviders[:2] {
		host := p.hosts[0]
		for _, next := range []string{
			"https://evil.example/page2",
			"https://" + host + ".evil.example/page2",
			"http://" + host + "/page2",
			"https://user:pw@" + host + "/page2",
			"https://" + host + ":8443/page2",
			"//evil.example/page2",
			"https://evil.example/?t=" + fixtureToken,
		} {
			p, next := p, next
			t.Run(p.name+" "+next, func(t *testing.T) {
				inner := p.serve([][2]string{{"ws", "first"}})
				srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
					isRepos := strings.Contains(r.URL.Path, "/repos") || strings.HasPrefix(r.URL.Path, "/2.0/repositories/")
					if !isRepos {
						inner(w, r)
						return
					}
					if p.name == "gitea" {
						w.Header().Set("Link", "<"+next+`>; rel="next"`)
						inner(w, r)
						return
					}
					rec := &bodyRecorder{header: http.Header{}}
					inner(rec, r)
					body := strings.TrimSuffix(strings.TrimSpace(rec.buf.String()), "}") + `,"next":"` + next + `"}`
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, body)
				})
				res, repos, err := p.list(t, srv)
				if kindOf(err) != KindBadResponse {
					t.Fatalf("%v", err)
				}
				noTokenIn(t, next, err)
				if len(repos) != 1 || res.Count != 1 {
					t.Errorf("the first page was lost: %d repos", len(repos))
				}
				p.check(t, srv)
			})
		}
	}
}

// bodyRecorder captures the body a handler writes.
type bodyRecorder struct {
	header http.Header
	buf    bytes.Buffer
}

func (b *bodyRecorder) Header() http.Header         { return b.header }
func (b *bodyRecorder) Write(p []byte) (int, error) { return b.buf.Write(p) }
func (b *bodyRecorder) WriteHeader(int)             {}

// A body over the cap is refused, whatever the provider.
func TestAdvProvidersBodyTooLarge(t *testing.T) {
	for _, p := range advProviders {
		p := p
		t.Run(p.name, func(t *testing.T) {
			inner := p.serve(nil)
			srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
				isList := strings.Contains(r.URL.Path, "/repos") || strings.Contains(r.URL.Path, "/repositories")
				if !isList {
					inner(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"value":[{"name":"`))
				chunk := bytes.Repeat([]byte("a"), 64<<10)
				for n := 0; n < MaxBodyBytes+(1<<20); n += len(chunk) {
					if _, err := w.Write(chunk); err != nil {
						return
					}
				}
			})
			_, repos, err := p.list(t, srv)
			if err == nil || len(repos) != 0 {
				t.Fatalf("%v %d", err, len(repos))
			}
			noTokenIn(t, p.name, err)
		})
	}
}

// An HTML page with status 200 (captive portal, sign-in page) is no answer.
func TestAdvProvidersHTML200(t *testing.T) {
	page := "<html><body><form action=/login>Sign in \x1b[31m " + fixtureToken + "</form></body></html>"
	for _, p := range advProviders {
		for _, ct := range []string{"text/html; charset=utf-8", "text/plain", "", "application/octet-stream"} {
			p, ct := p, ct
			t.Run(p.name+" "+ct, func(t *testing.T) {
				srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
					if ct != "" {
						w.Header().Set("Content-Type", ct)
					} else {
						w.Header()["Content-Type"] = nil
					}
					_, _ = io.WriteString(w, page)
				})
				_, repos, err := p.list(t, srv)
				var oe *Error
				if kindOf(err) != KindBadResponse || len(repos) != 0 {
					t.Fatalf("%v", err)
				}
				oe = err.(*Error)
				if strings.Contains(oe.Detail, "Sign in") || strings.Contains(oe.Detail, "<") {
					t.Errorf("the page is in the message: %q", oe.Detail)
				}
				noTokenIn(t, p.name, err)
			})
		}
	}
}

// A listing that never ends stops at the page cap.
func TestAdvProvidersRunawayPages(t *testing.T) {
	for _, p := range advProviders[:2] {
		p := p
		t.Run(p.name, func(t *testing.T) {
			var hits atomic.Int64
			inner := p.serve([][2]string{{"ws", "r"}})
			srv := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
				isRepos := strings.Contains(r.URL.Path, "/repos") || strings.HasPrefix(r.URL.Path, "/2.0/repositories/")
				if !isRepos {
					inner(w, r)
					return
				}
				n := hits.Add(1)
				next := fmt.Sprintf("https://%s%s?page=%d", r.Host, r.URL.Path, n+1)
				if p.name == "gitea" {
					w.Header().Set("Link", "<"+next+`>; rel="next"`)
					inner(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"values":[{"full_name":"ws/r%d","is_private":false}],"next":%q}`, n, next)
			})
			c, _ := srv.client()
			c.MaxPages = 3
			res, repos, _, err := listAll(t, c, p.acct, tokenOf(SourceEnv))
			if err != nil || !res.Truncated || hits.Load() != 3 {
				t.Fatalf("%v truncated=%v hits=%d repos=%d", err, res.Truncated, hits.Load(), len(repos))
			}
		})
	}
}

// Names with ../, absolute paths, escape sequences and bidi overrides never
// get a clone URL, and nothing they hold reaches a terminal.
func TestAdvProvidersHostileNames(t *testing.T) {
	hostile := [][2]string{
		{"acme", "../../etc/passwd"}, {"..", "x"}, {"acme", ".."}, {"/etc", "passwd"}, {"acme", "/abs/path"},
		{"acme", `..\win`}, {"acme", "a/b"},
		{"acme", "evil\x1b]8;;https://evil.example\x07link\x1b]8;;\x07"}, {"acme", "evil\x1b[2Jname"},
		{"acme", "re‮txt.exe"}, {"acme⁦", "iso"}, {"acme", "zero​width"}, {"acme", "tab\tname"},
		{"acme", "nl\nname"}, {"acme", "nul\x00name"}, {"acme", "c1\u009bname"}, {"acme", ".git"}, {"acme", "."}, {".git", "x"},
	}
	for _, p := range advProviders {
		p := p
		t.Run(p.name, func(t *testing.T) {
			owner := "ws"
			if p.name == "gitea" {
				owner = "sam"
			}
			repos := append([][2]string(nil), hostile...)
			repos = append(repos, [2]string{owner, "good"})
			srv := newAPI(t, p.serve(repos))
			_, got, err := p.list(t, srv)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range got {
				for _, s := range []string{r.Owner, r.Name, r.FullPath, r.CloneURL, r.WebURL} {
					assertClean(t, r.FullPath, s)
				}
				if r.Name == "good" {
					if r.CloneURL == "" {
						t.Errorf("the good repo got no URL: %+v", r)
					}
					continue
				}
				if r.CloneURL != "" || r.WebURL != "" {
					t.Errorf("a hostile name got a URL: %+v", r)
				}
			}
			for _, r := range got {
				if strings.Contains(r.CloneURL, "..") || strings.Contains(r.CloneURL, "%2e") {
					t.Errorf("traversal in %q", r.CloneURL)
				}
			}
		})
	}
}

// Azure: a project or repository name with a slash or a dot segment is
// refused (the path is the organization/project/repository).
func TestAdvAzureSegments(t *testing.T) {
	for _, bad := range []string{"a/b", "..", ".", ".git", "-x", " lead", "trail ", `a\b`, "a\x1bb"} {
		if c, w := azureURLs("dev.azure.com", "acme", bad, "r"); c != "" || w != "" {
			t.Errorf("project %q: %q", bad, c)
		}
		if c, _ := azureURLs("dev.azure.com", "acme", "p", bad); c != "" {
			t.Errorf("repo %q: %q", bad, c)
		}
	}
	if c, _ := azureURLs("dev.azure.com", "acme", "p q", "r%20x"); c != "https://dev.azure.com/acme/p%20q/_git/r%2520x" {
		t.Errorf("%q", c)
	}
}

// More organizations than the cap are not all requested.
func TestAdvAzureOrgCap(t *testing.T) {
	var many []string
	for i := 0; i < maxOrgs+50; i++ {
		many = append(many, fmt.Sprintf("org%d", i))
	}
	srv := azServer(t, map[string][]map[string]any{}, nil)
	c, _ := srv.client()
	_, _, _, _ = listAll(t, c, azAccount(many...), tokenOf(SourceEnv))
	if n := len(srv.requests()); n != maxOrgs {
		t.Errorf("%d requests, want %d", n, maxOrgs)
	}
}

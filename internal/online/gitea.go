package online

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Gitea REST API (https://docs.gitea.com/api/1.22/ and the swagger file of
// gitea.com): GET /user, GET /user/repos, GET /user/orgs and
// GET /orgs/{org}/repos, each with page and limit. The token goes in
// "Authorization: token <t>"; a credential that came from git's helper is a
// password and goes in Basic auth with the account's username.

const (
	giteaAccept = "application/json"
	giteaLimit  = 50  // the default maximum of a page (MAX_RESPONSE_ITEMS)
	maxOrgs     = 100 // organizations (Gitea) / workspaces (Bitbucket) per account
)

// giteaBase is the REST root of a Gitea host.
func giteaBase(host string) string { return "https://" + host + "/api/v1" }

type gtUser struct {
	Login    string `json:"login"`
	Username string `json:"username"`
}

type gtRepo struct {
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Owner    struct {
		Login    string `json:"login"`
		Username string `json:"username"`
	} `json:"owner"`
	Private   bool    `json:"private"`
	Archived  bool    `json:"archived"`
	Fork      bool    `json:"fork"`
	Empty     bool    `json:"empty"`
	UpdatedAt *string `json:"updated_at"`
}

type gtOrg struct {
	Username string `json:"username"`
	Name     string `json:"name"`
}

// orgNameRe is the form of an organization or workspace name taken from an
// answer before it is put in a URL path.
var orgNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func orgNameOK(s string) bool { return orgNameRe.MatchString(s) && plainSegment(s) }

func giteaAuth(t Token, a Account) auth {
	if t.Source == SourceGit {
		return auth{scheme: "Basic", user: a.Username, tok: t.Secret}
	}
	return auth{scheme: "token", tok: t.Secret}
}

func (c *Client) listGitea(ctx context.Context, a Account, t Token, onPage func(Page)) (Result, *Error) {
	var res Result
	if e := hostOK(a.Host); e != nil {
		return res, e
	}
	base := giteaBase(a.Host)
	au := giteaAuth(t, a)

	rep, e := c.get(ctx, base+"/user", giteaAccept, au, 0)
	if e != nil {
		return res, e
	}
	var u gtUser
	if e := decodeJSON(rep.body, &u, t.Secret); e != nil {
		return res, e
	}
	login := u.Login
	if login == "" {
		login = u.Username
	}
	res.Login = safeText(login, t.Secret)
	if !strings.EqualFold(login, a.Username) {
		e := newError(KindWrongUser, 0, "", t.Secret)
		e.Login = res.Login
		return res, e
	}

	seen := map[string]bool{}
	pageNo := 0
	// walk lists one /repos endpoint. A server that sends no Link header at
	// all and a full page is asked for the next page by number.
	walk := func(path string) (bool, *Error) {
		first := base + path + "?limit=" + strconv.Itoa(giteaLimit) + "&page=1"
		last := 0
		return c.pagerNext(ctx, first, giteaAccept, au,
			func(n int, r reply) string {
				if len(r.header.Values("Link")) > 0 || last < giteaLimit {
					return ""
				}
				return base + path + "?limit=" + strconv.Itoa(giteaLimit) + "&page=" + strconv.Itoa(n+1)
			},
			func(_ int, r reply) *Error {
				var raw []gtRepo
				if e := decodeJSON(r.body, &raw, t.Secret); e != nil {
					return e
				}
				last = len(raw)
				pageNo++
				pg := Page{N: pageNo}
				for _, g := range raw {
					owner := g.Owner.Login
					if owner == "" {
						owner = g.Owner.Username
					}
					so, sn := safeText(owner, t.Secret), safeText(g.Name, t.Secret)
					if so == "" || sn == "" {
						continue
					}
					key := strings.ToLower(so + "/" + sn)
					if seen[key] {
						continue
					}
					seen[key] = true
					rp := Repo{
						Owner: so, Name: sn, FullPath: so + "/" + sn,
						Private: g.Private, Archived: g.Archived, Fork: g.Fork, Empty: g.Empty,
						Updated: parseTime(g.UpdatedAt),
					}
					if so == owner && sn == g.Name && !strings.Contains(so+sn, "/") {
						rp.CloneURL, rp.WebURL = urls(a.Host, so, sn)
					}
					pg.Repos = append(pg.Repos, rp)
				}
				res.Pages, res.Count = pageNo, res.Count+len(pg.Repos)
				onPage(pg)
				return nil
			})
	}

	trunc, e := walk("/user/repos")
	res.Truncated = trunc
	if e != nil {
		return res, e
	}

	// /user/repos lists what the user owns; the repositories of the
	// organizations are listed one organization at a time
	var orgs []string
	orgTrunc, e := c.pagerNext(ctx, base+"/user/orgs?limit="+strconv.Itoa(giteaLimit)+"&page=1", giteaAccept, au, nil,
		func(_ int, r reply) *Error {
			var raw []gtOrg
			if e := decodeJSON(r.body, &raw, t.Secret); e != nil {
				return e
			}
			for _, o := range raw {
				name := o.Username
				if name == "" {
					name = o.Name
				}
				if orgNameOK(name) && len(orgs) < maxOrgs {
					orgs = append(orgs, name)
				}
			}
			return nil
		})
	if e != nil {
		if isFatal(e) {
			return res, e
		}
		res.Warnings = addWarning(res.Warnings, WarnOrgsList)
		onPage(Page{N: pageNo, Warnings: []string{WarnOrgsList}})
	}
	res.Truncated = res.Truncated || orgTrunc || len(orgs) >= maxOrgs
	for _, org := range orgs {
		tr, e := walk("/orgs/" + url.PathEscape(org) + "/repos")
		res.Truncated = res.Truncated || tr
		if e != nil {
			if isFatal(e) {
				return res, e
			}
			w := warnFor(WarnOrgFailed, org, t.Secret)
			res.Warnings = addWarning(res.Warnings, w)
			onPage(Page{N: pageNo, Warnings: []string{w}})
		}
	}
	return res, nil
}

// isFatal reports whether a failure in the middle of a listing ends it: the
// user cancelled, the quota is gone, or the connection is down. Other
// failures (one organization that answers 403) become a warning.
func isFatal(e *Error) bool {
	switch e.Kind {
	case KindCancelled, KindRateLimit, KindTimeout, KindNetwork, KindDNS, KindTLS, KindProxy, KindAuth, KindOTP:
		return true
	}
	return false
}

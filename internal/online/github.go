package online

import (
	"context"
	"net/url"
	"strings"
)

const ghAccept = "application/vnd.github+json"

// githubBase: api.github.com for github.com, else the GitHub Enterprise
// Server root https://HOST/api/v3.
func githubBase(host string) string {
	if strings.EqualFold(host, "github.com") {
		return "https://api.github.com"
	}
	return "https://" + host + "/api/v3"
}

type ghUser struct {
	Login string `json:"login"`
}

type ghRepo struct {
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Owner    struct {
		Login string `json:"login"`
	} `json:"owner"`
	Private   bool    `json:"private"`
	Archived  bool    `json:"archived"`
	Fork      bool    `json:"fork"`
	Size      int64   `json:"size"`
	PushedAt  *string `json:"pushed_at"`
	UpdatedAt *string `json:"updated_at"`
}

func (c *Client) listGitHub(ctx context.Context, a Account, t Token, onPage func(Page)) (Result, *Error) {
	var res Result
	if e := hostOK(a.Host); e != nil {
		return res, e
	}
	base := githubBase(a.Host)
	au := auth{scheme: "Bearer", tok: t.Secret}

	// who does the token belong to? Nothing is listed for another user.
	rep, e := c.get(ctx, base+"/user", ghAccept, au, 0)
	if e != nil {
		return res, e
	}
	var u ghUser
	if e := decodeJSON(rep.body, &u, t.Secret); e != nil {
		return res, e
	}
	res.Login = safeText(u.Login, t.Secret)
	if !strings.EqualFold(u.Login, a.Username) {
		e := newError(KindWrongUser, 0, "", t.Secret)
		e.Login = res.Login
		return res, e
	}

	q := url.Values{}
	q.Set("affiliation", "owner,collaborator,organization_member")
	q.Set("sort", "pushed")
	q.Set("per_page", "100")
	first := base + "/user/repos?" + q.Encode()
	trunc, e := c.pager(ctx, first, ghAccept, au, func(n int, r reply) *Error {
		var raw []ghRepo
		if e := decodeJSON(r.body, &raw, t.Secret); e != nil {
			return e
		}
		pg := Page{N: n}
		if n == 1 {
			// a classic token or OAuth app token lists its scopes; a
			// fine-grained token sends no such header
			if vals, ok := r.header["X-Oauth-Scopes"]; ok && !hasScope(strings.Join(vals, ","), "repo") {
				pg.Warnings = addWarning(pg.Warnings, WarnNoRepoScope)
			}
		}
		if strings.HasPrefix(r.header.Get("X-GitHub-SSO"), "partial-results") {
			pg.Warnings = addWarning(pg.Warnings, WarnSSOPartial)
		}
		for _, g := range raw {
			owner, name := safeText(g.Owner.Login, t.Secret), safeText(g.Name, t.Secret)
			if owner == "" || name == "" {
				continue
			}
			updated := parseTime(g.PushedAt)
			if updated.IsZero() {
				updated = parseTime(g.UpdatedAt)
			}
			rp := Repo{
				Owner: owner, Name: name, FullPath: owner + "/" + name,
				Private: g.Private, Archived: g.Archived, Fork: g.Fork, Empty: g.Size == 0,
				Updated: updated,
			}
			// a name that needed cleaning is not the real one; a GitHub owner
			// or name never holds a slash
			if owner == g.Owner.Login && name == g.Name && !strings.Contains(owner+name, "/") {
				rp.CloneURL, rp.WebURL = urls(a.Host, owner, name)
			}
			pg.Repos = append(pg.Repos, rp)
		}
		res.Pages, res.Count = n, res.Count+len(pg.Repos)
		for _, w := range pg.Warnings {
			res.Warnings = addWarning(res.Warnings, w)
		}
		onPage(pg)
		return nil
	})
	res.Truncated = trunc
	return res, e
}

// hasScope reports whether the comma separated X-OAuth-Scopes lists want.
func hasScope(header, want string) bool {
	for _, s := range strings.Split(header, ",") {
		if strings.TrimSpace(s) == want {
			return true
		}
	}
	return false
}

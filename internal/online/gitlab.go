package online

import (
	"context"
	"net/url"
	"strings"
)

const glAccept = "application/json"

// gitlabBase is the REST root of a GitLab host (a relative URL root is not
// supported).
func gitlabBase(host string) string { return "https://" + host + "/api/v4" }

type glUser struct {
	Username string `json:"username"`
}

type glProject struct {
	PathWithNamespace string `json:"path_with_namespace"`
	Path              string `json:"path"`
	Namespace         struct {
		FullPath string `json:"full_path"`
	} `json:"namespace"`
	Visibility     string         `json:"visibility"`
	Archived       bool           `json:"archived"`
	ForkedFrom     map[string]any `json:"forked_from_project"`
	EmptyRepo      bool           `json:"empty_repo"`
	LastActivityAt *string        `json:"last_activity_at"`
}

func (c *Client) listGitLab(ctx context.Context, a Account, t Token, onPage func(Page)) (Result, *Error) {
	var res Result
	if e := hostOK(a.Host); e != nil {
		return res, e
	}
	base := gitlabBase(a.Host)
	au := auth{scheme: "Bearer", tok: t.Secret}

	rep, e := c.get(ctx, base+"/user", glAccept, au, 0)
	if e != nil {
		return res, e
	}
	var u glUser
	if e := decodeJSON(rep.body, &u, t.Secret); e != nil {
		return res, e
	}
	res.Login = safeText(u.Username, t.Secret)
	if !strings.EqualFold(u.Username, a.Username) {
		e := newError(KindWrongUser, 0, "", t.Secret)
		e.Login = res.Login
		return res, e
	}

	q := url.Values{}
	q.Set("membership", "true")
	q.Set("per_page", "100")
	q.Set("pagination", "keyset")
	q.Set("order_by", "id")
	q.Set("sort", "asc")
	first := base + "/projects?" + q.Encode()
	trunc, e := c.pager(ctx, first, glAccept, au, func(n int, r reply) *Error {
		var raw []glProject
		if e := decodeJSON(r.body, &raw, t.Secret); e != nil {
			return e
		}
		pg := Page{N: n}
		for _, g := range raw {
			full := safeText(g.PathWithNamespace, t.Secret)
			i := strings.LastIndexByte(full, '/')
			if i <= 0 || i == len(full)-1 {
				continue
			}
			owner, name := full[:i], full[i+1:]
			rp := Repo{
				Owner: owner, Name: name, FullPath: full,
				Private: g.Visibility != "public", Archived: g.Archived,
				Fork: g.ForkedFrom != nil, Empty: g.EmptyRepo,
				Updated: parseTime(g.LastActivityAt),
			}
			if full == g.PathWithNamespace {
				rp.CloneURL, rp.WebURL = urls(a.Host, owner, name)
			}
			pg.Repos = append(pg.Repos, rp)
		}
		res.Pages, res.Count = n, res.Count+len(pg.Repos)
		onPage(pg)
		return nil
	})
	res.Truncated = trunc
	return res, e
}

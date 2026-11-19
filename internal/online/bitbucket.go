package online

import (
	"context"
	"net/url"
	"strings"
)

// Bitbucket Cloud REST API 2.0 (https://developer.atlassian.com/cloud/bitbucket/rest/,
// swagger.v3.json): GET /2.0/user, GET /2.0/user/workspaces and
// GET /2.0/repositories/{workspace}?role=member&pagelen=100. Pages are linked
// by the "next" member of the JSON body. App passwords stopped working on
// 2026-06-09; an API token is sent as Basic auth with the Atlassian account
// email, or as a Bearer token
// (https://support.atlassian.com/bitbucket-cloud/docs/using-api-tokens/).

const (
	bbAccept = "application/json"
	bbHost   = "bitbucket.org"
	bbAPI    = "https://api.bitbucket.org/2.0"
)

type bbUser struct {
	Username string `json:"username"` // removed from the API for privacy; kept if a server sends it
	Nickname string `json:"nickname"`
}

type bbPage struct {
	Next   string `json:"next"`
	Values []struct {
		Workspace struct {
			Slug string `json:"slug"`
		} `json:"workspace"`
		FullName  string         `json:"full_name"`
		IsPrivate bool           `json:"is_private"`
		UpdatedOn *string        `json:"updated_on"`
		Size      int64          `json:"size"`
		Parent    map[string]any `json:"parent"`
		Main      map[string]any `json:"mainbranch"`
	} `json:"values"`
}

// bbAttempts is the order in which the credentials are tried against /user:
// with an email, Basic(email) then Bearer; without, Bearer then
// Basic(username). The first one the API accepts is used for the rest.
func bbAttempts(a Account, t Token) []auth {
	if a.Email != "" {
		return []auth{{scheme: "Basic", user: a.Email, tok: t.Secret}, {scheme: "Bearer", tok: t.Secret}}
	}
	return []auth{{scheme: "Bearer", tok: t.Secret}, {scheme: "Basic", user: a.Username, tok: t.Secret}}
}

func (c *Client) listBitbucket(ctx context.Context, a Account, t Token, onPage func(Page)) (Result, *Error) {
	var res Result
	if e := hostOK(a.Host); e != nil {
		return res, e
	}
	if !strings.EqualFold(a.Host, bbHost) {
		return res, &Error{Kind: KindUnsupported, Detail: Sanitize(a.Host)}
	}

	var (
		au   auth
		user bbUser
		e    *Error
	)
	cands := bbAttempts(a, t)
	for i, cand := range cands {
		var rep reply
		rep, e = c.get(ctx, bbAPI+"/user", bbAccept, cand, 0)
		if e == nil {
			if e = decodeJSON(rep.body, &user, t.Secret); e != nil {
				return res, e
			}
			au = cand
			break
		}
		if e.Kind != KindAuth || i == len(cands)-1 {
			return res, e
		}
	}
	login := user.Nickname
	if user.Username != "" {
		login = user.Username
	}
	res.Login = safeText(login, t.Secret)
	if !strings.EqualFold(user.Nickname, a.Username) && !strings.EqualFold(user.Username, a.Username) {
		e := newError(KindWrongUser, 0, "", t.Secret)
		e.Login = res.Login
		return res, e
	}

	var next string
	var spaces []string
	spTrunc, e := c.pagerNext(ctx, bbAPI+"/user/workspaces?pagelen=100", bbAccept, au,
		func(int, reply) string { return next },
		func(_ int, r reply) *Error {
			var pg bbPage
			if e := decodeJSON(r.body, &pg, t.Secret); e != nil {
				return e
			}
			next = pg.Next
			for _, v := range pg.Values {
				if orgNameOK(v.Workspace.Slug) && len(spaces) < maxOrgs {
					spaces = append(spaces, v.Workspace.Slug)
				}
			}
			return nil
		})
	if e != nil {
		return res, e
	}
	res.Truncated = spTrunc || len(spaces) >= maxOrgs

	seen := map[string]bool{}
	pageNo := 0
	var firstErr *Error
	okCount := 0
	for _, ws := range spaces {
		next = ""
		first := bbAPI + "/repositories/" + url.PathEscape(ws) + "?role=member&pagelen=100"
		tr, e := c.pagerNext(ctx, first, bbAccept, au,
			func(int, reply) string { return next },
			func(_ int, r reply) *Error {
				var pg bbPage
				if e := decodeJSON(r.body, &pg, t.Secret); e != nil {
					return e
				}
				next = pg.Next
				pageNo++
				out := Page{N: pageNo}
				for _, v := range pg.Values {
					full := safeText(v.FullName, t.Secret)
					i := strings.IndexByte(full, '/')
					if i <= 0 || i == len(full)-1 || strings.Contains(full[i+1:], "/") {
						continue
					}
					owner, name := full[:i], full[i+1:]
					key := strings.ToLower(full)
					if seen[key] {
						continue
					}
					seen[key] = true
					rp := Repo{
						Owner: owner, Name: name, FullPath: full,
						Private: v.IsPrivate, Fork: v.Parent != nil,
						Empty:   v.Main == nil && v.Size == 0,
						Updated: parseTime(v.UpdatedOn),
					}
					if full == v.FullName {
						rp.CloneURL, rp.WebURL = urls(a.Host, owner, name)
					}
					out.Repos = append(out.Repos, rp)
				}
				res.Pages, res.Count = pageNo, res.Count+len(out.Repos)
				onPage(out)
				return nil
			})
		res.Truncated = res.Truncated || tr
		if e != nil {
			if isFatal(e) && e.Kind != KindAuth {
				return res, e
			}
			if firstErr == nil {
				firstErr = e
			}
			w := warnFor(WarnWsFailed, ws, t.Secret)
			res.Warnings = addWarning(res.Warnings, w)
			onPage(Page{N: pageNo, Warnings: []string{w}})
			continue
		}
		okCount++
	}
	if okCount == 0 && firstErr != nil {
		return res, firstErr
	}
	return res, nil
}

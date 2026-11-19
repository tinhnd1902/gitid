package online

import (
	"context"
	"net/url"
	"regexp"
	"strings"
)

// Azure DevOps Services REST API 7.1:
//   - Repositories - List, one call per organization (all its projects):
//     GET https://dev.azure.com/{org}/_apis/git/repositories?api-version=7.1
//     https://learn.microsoft.com/en-us/rest/api/azure/devops/git/repositories/list?view=azure-devops-rest-7.1
//   - when the account names no organization: Profiles - Get (id "me") and
//     Accounts - List (memberId) on app.vssps.visualstudio.com. Microsoft
//     documents that these two may require a Microsoft Entra token and not
//     a PAT, so a failure there is reported with a hint to set orgs.
//   - a PAT goes in Basic auth with an empty username; a Microsoft Entra
//     access token (a JWT) goes in Bearer.
//     https://learn.microsoft.com/en-us/azure/devops/organizations/accounts/use-personal-access-tokens-to-authenticate
//
// There is no "who am I" check (the PAT is bound to an organization, not to
// a user name) and no update time in the answer.

const (
	azAccept     = "application/json"
	azHost       = "dev.azure.com"
	azVssps      = "https://app.vssps.visualstudio.com/_apis"
	azAPIVersion = "7.1"
	// AzureSignInHint is the Detail of an Azure answer that is the sign-in
	// page: Azure sends one (HTTP 203, or a redirect) for a rejected PAT.
	AzureSignInHint = "token rejected (Azure returns a sign-in page)"
)

var (
	azOrgRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	azUUIDRe = regexp.MustCompile(`^[0-9A-Fa-f-]{8,64}$`)
)

type azRepo struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	IsDisabled bool   `json:"isDisabled"`
	IsFork     bool   `json:"isFork"`
	Project    struct {
		Name string `json:"name"`
	} `json:"project"`
}

// azureAuth: a JWT is a Microsoft Entra token (Bearer), anything else a PAT.
func azureAuth(t Token) auth {
	if t.Secret.looksLikeJWT() {
		return auth{scheme: "Bearer", tok: t.Secret}
	}
	return auth{scheme: "Basic", tok: t.Secret}
}

// azureURLs builds the clone and web URL (they are the same form) of
// org/project/repo. Each segment is percent-encoded; a segment that is not
// plain gives no URL.
func azureURLs(host, org, project, repo string) (clone, web string) {
	for _, s := range []string{org, project, repo} {
		if !plainSegment(s) {
			return "", ""
		}
	}
	u := "https://" + host + "/" + url.PathEscape(org) + "/" + url.PathEscape(project) + "/_git/" + url.PathEscape(repo)
	return u, u
}

// azureHint turns the sign-in page Azure sends for a rejected token into one
// clear failure.
func azureHint(e *Error) *Error {
	if e == nil || e.Kind != KindBadResponse {
		return e
	}
	if e.Status == 203 || strings.Contains(e.Detail, "redirect refused") || strings.HasPrefix(e.Detail, "the answer is not JSON") {
		e.Detail = AzureSignInHint
	}
	return e
}

func (c *Client) listAzure(ctx context.Context, a Account, t Token, onPage func(Page)) (Result, *Error) {
	var res Result
	if e := hostOK(a.Host); e != nil {
		return res, e
	}
	if !strings.EqualFold(a.Host, azHost) {
		return res, &Error{Kind: KindUnsupported, Detail: Sanitize(a.Host)}
	}
	au := azureAuth(t)
	pageNo := 0

	var orgs []string
	for _, o := range a.Orgs {
		if azOrgRe.MatchString(o) && len(orgs) < maxOrgs {
			orgs = append(orgs, o)
		}
	}
	res.Truncated = len(a.Orgs) > maxOrgs
	if len(orgs) == 0 {
		found, e := c.azureOrgs(ctx, au, t)
		if e != nil {
			return res, azureHint(e)
		}
		orgs = found
		if len(orgs) == 0 {
			res.Warnings = addWarning(res.Warnings, WarnNoOrgs)
			onPage(Page{N: 1, Warnings: []string{WarnNoOrgs}})
			return res, nil
		}
	}

	var firstErr *Error
	ok := 0
	for _, org := range orgs {
		pageNo++
		rep, e := c.get(ctx, azRepos(org), azAccept, au, pageNo)
		var raw struct {
			Value []azRepo `json:"value"`
		}
		if e == nil {
			e = decodeJSON(rep.body, &raw, t.Secret)
		}
		if e != nil {
			e = azureHint(e)
			if e.Kind == KindCancelled || e.Kind == KindRateLimit {
				return res, e
			}
			if firstErr == nil {
				firstErr = e
			}
			w := warnFor(WarnOrgFailed, org, t.Secret)
			res.Warnings = addWarning(res.Warnings, w)
			onPage(Page{N: pageNo, Warnings: []string{w}})
			continue
		}
		ok++
		pg := Page{N: pageNo}
		for _, g := range raw.Value {
			proj, name := safeText(g.Project.Name, t.Secret), safeText(g.Name, t.Secret)
			if proj == "" || name == "" {
				continue
			}
			rp := Repo{
				Owner: org + "/" + proj, Name: name, FullPath: org + "/" + proj + "/" + name,
				Archived: g.IsDisabled, Fork: g.IsFork, Empty: g.Size == 0,
			}
			if proj == g.Project.Name && name == g.Name {
				rp.CloneURL, rp.WebURL = azureURLs(a.Host, org, proj, name)
			}
			pg.Repos = append(pg.Repos, rp)
		}
		res.Pages, res.Count = pageNo, res.Count+len(pg.Repos)
		onPage(pg)
	}
	if ok == 0 && firstErr != nil {
		return res, firstErr
	}
	return res, nil
}

func azRepos(org string) string {
	return "https://" + azHost + "/" + url.PathEscape(org) + "/_apis/git/repositories?api-version=" + azAPIVersion
}

// azureOrgs finds the organizations of the token's user: the profile id, then
// the accounts the user is a member of.
func (c *Client) azureOrgs(ctx context.Context, au auth, t Token) ([]string, *Error) {
	rep, e := c.get(ctx, azVssps+"/profile/profiles/me?api-version="+azAPIVersion, azAccept, au, 0)
	if e != nil {
		return nil, e
	}
	var prof struct {
		ID string `json:"id"`
	}
	if e := decodeJSON(rep.body, &prof, t.Secret); e != nil {
		return nil, e
	}
	if !azUUIDRe.MatchString(prof.ID) {
		return nil, newError(KindBadResponse, 200, "the profile has no id", t.Secret)
	}
	rep, e = c.get(ctx, azVssps+"/accounts?memberId="+url.QueryEscape(prof.ID)+"&api-version="+azAPIVersion, azAccept, au, 0)
	if e != nil {
		return nil, e
	}
	var accs struct {
		Value []struct {
			AccountName string `json:"accountName"`
		} `json:"value"`
	}
	if e := decodeJSON(rep.body, &accs, t.Secret); e != nil {
		return nil, e
	}
	var orgs []string
	for _, v := range accs.Value {
		if azOrgRe.MatchString(v.AccountName) && len(orgs) < maxOrgs {
			orgs = append(orgs, v.AccountName)
		}
	}
	return orgs, nil
}

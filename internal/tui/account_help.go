package tui

import (
	"strings"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/platform"
	"github.com/tinhnd1902/gitid/internal/remote"
)

// Sentinels used to render an example remote with the real renderer. They are
// replaced by readable placeholders afterwards.
const (
	sentOwner   = "GITIDOWNER"
	sentRepo    = "GITIDREPO"
	sentOrg     = "GITIDORG"
	sentProject = "GITIDPROJECT"
	sentHost    = "gitidhost"
)

// remoteExample is the remote an account gives to a repository, for example
// https://alice@github.com/<owner>/<repo>.git or
// git@github-acme-alice:<owner>/<repo>.git. It is produced by the same code
// that clone and fix-remote use, so it is exactly what gitid would write.
func remoteExample(ref config.AccountRef) string {
	a := ref.Account
	host := a.EffectiveHost()
	if host == "" {
		// Gitea without a host: render with a stand-in host.
		a.Host, host = sentHost, sentHost
	}
	sample := "https://" + host + "/" + sentOwner + "/" + sentRepo + ".git"
	if a.Platform == platform.AzureDevOps {
		sample = "https://" + host + "/" + sentOrg + "/" + sentProject + "/_git/" + sentRepo
	}
	r, err := remote.Parse(sample)
	if err != nil {
		return accountTarget(ref)
	}
	out, err := remote.Render(config.AccountRef{Company: ref.Company, Account: a}, r, remote.Options{})
	if err != nil {
		return accountTarget(ref)
	}
	return strings.NewReplacer(
		sentProject, "<project>", sentOwner, "<owner>", sentOrg, "<org>", sentRepo, "<repo>", sentHost, "<host>",
	).Replace(out)
}

// sameLogin reports whether two accounts sign in as the same user on the same
// host: same platform, same effective host and the same username (ignoring
// case).
func sameLogin(a, b config.Account) bool {
	return a.Platform == b.Platform &&
		a.EffectiveHost() == b.EffectiveHost() &&
		strings.EqualFold(strings.TrimSpace(a.Username), strings.TrimSpace(b.Username))
}

// dupInCompany finds another account of c (not the one with id exceptID) that
// uses the same login as a.
func dupInCompany(c config.Company, a config.Account, exceptID string) (config.Account, bool) {
	for _, o := range c.Accounts {
		if o.ID != exceptID && sameLogin(o, a) {
			return o, true
		}
	}
	return config.Account{}, false
}

// loginElsewhere lists the "company/id" of the accounts of the other
// companies (not slug) that use the same login as a.
func loginElsewhere(cfg config.Config, slug string, a config.Account) []string {
	var out []string
	for _, c := range cfg.Companies {
		if c.Slug == slug {
			continue
		}
		for _, o := range c.Accounts {
			if sameLogin(o, a) {
				out = append(out, c.Slug+"/"+o.ID)
			}
		}
	}
	return out
}

// looksLikeEmail reports whether u is probably an email typed where a login
// belongs. Only GitHub, GitLab and Gitea logins can never contain "@";
// Bitbucket and Azure DevOps sign in differently and are not checked.
func looksLikeEmail(p platform.Platform, u string) bool {
	switch p {
	case platform.GitHub, platform.GitLab, platform.Gitea:
		return strings.Contains(u, "@")
	}
	return false
}

// duplicateOf finds another account of c with the same login as the stored
// account a. It flags configurations created before the form refused such
// accounts.
func duplicateOf(c config.Company, a config.Account) (config.Account, bool) {
	return dupInCompany(c, a, a.ID)
}

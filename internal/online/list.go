package online

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/tinhnd1902/gitid/internal/platform"
)

// Repo is one repository an account can see. Every string came through
// Sanitize. CloneURL and WebURL are built from the host the account is
// configured with, never taken from the API answer; they are empty when the
// repository's name is not a plain path.
type Repo struct {
	Owner, Name, FullPath string
	Private, Archived     bool
	Fork, Empty           bool
	Updated               time.Time // zero when the platform does not say
	WebURL, CloneURL      string
}

// Page is what one page of the listing added.
type Page struct {
	N        int
	Repos    []Repo
	Warnings []string // i18n suffixes, see the Warn constants
}

// Result is the outcome of a listing.
type Result struct {
	Login     string // who the token belongs to
	Truncated bool   // the page cap was reached
	Warnings  []string
	Pages     int
	Count     int
}

// Warnings of a listing that worked but may be incomplete.
const (
	WarnNoRepoScope = "no_repo_scope" // the token has no repo scope: private repositories are hidden
	WarnSSOPartial  = "sso_partial"   // organizations are hidden until the token is authorized for SSO
)

// Supported reports whether the listing knows the platform.
func Supported(p platform.Platform) bool { return p == platform.GitHub || p == platform.GitLab }

// List fetches the repositories of a with t, page by page: onPage is called
// for each page in order, so a caller can show them as they arrive and keep
// what came before an error. Before listing, the API's own answer to "who am
// I" must match a.Username (case-insensitive), else nothing is listed.
func (c *Client) List(ctx context.Context, a Account, t Token, onPage func(Page)) (Result, error) {
	if onPage == nil {
		onPage = func(Page) {}
	}
	var (
		res Result
		e   *Error
	)
	switch a.Platform {
	case platform.GitHub:
		res, e = c.listGitHub(ctx, a, t, onPage)
	case platform.GitLab:
		res, e = c.listGitLab(ctx, a, t, onPage)
	default:
		e = newError(KindUnsupported, 0, string(a.Platform), t.Secret)
	}
	if e != nil {
		pinSSOURL(e, a)
		return res, e
	}
	return res, nil
}

// pinSSOURL keeps the SSO authorization link only when it is on the account's
// own host (github.com for GitHub cloud). A server that sends a link to
// another place gets the generic text instead: the link is shown to the user
// and must not be a way to lead them to a look-alike page.
func pinSSOURL(e *Error, a Account) {
	if e.SSOURL == "" {
		return
	}
	u, err := url.Parse(e.SSOURL)
	if err == nil && u.Scheme == "https" && u.User == nil && strings.EqualFold(u.Host, a.Host) {
		return
	}
	e.SSOURL = ""
}

// hostOK rejects a host that is not a plain host[:port]: a scheme (an http
// host would send the token in clear), a path, credentials or spaces.
func hostOK(host string) *Error {
	l := strings.ToLower(host)
	switch {
	case strings.HasPrefix(l, "http://"):
		return &Error{Kind: KindInsecureHost, Detail: Sanitize(host)}
	case host == "", strings.ContainsAny(host, "/@\\?# \t\n\r"), strings.Contains(host, "://"):
		return &Error{Kind: KindUnsupported, Detail: Sanitize(host)}
	}
	return nil
}

var safePathRe = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)

// urls builds the clone and web URLs of owner/name on host, or "" for both
// when the path holds anything but letters, digits, dot, dash, underscore
// and the separating slashes, or when a segment is "." or ".git".
func urls(host, owner, name string) (clone, web string) {
	full := owner + "/" + name
	if owner == "" || name == "" || !safePathRe.MatchString(full) || strings.Contains(full, "..") {
		return "", ""
	}
	// a segment named .git would be cloned into a folder git takes for the
	// git directory of the folder around it
	for _, seg := range strings.Split(full, "/") {
		if strings.EqualFold(seg, ".git") || seg == "." {
			return "", ""
		}
	}
	u := url.URL{Scheme: "https", Host: host, Path: "/" + full}
	return u.String() + ".git", u.String()
}

// addWarning appends w once.
func addWarning(list []string, w string) []string {
	for _, x := range list {
		if x == w {
			return list
		}
	}
	return append(list, w)
}

// Package update is the once-a-day check for a newer gitid release.
//
// The check is a single unauthenticated GET of the "latest release" page of
// the project. GitHub answers it with a redirect to .../releases/tag/vX.Y.Z;
// the check never follows that redirect and never reads a body, it only reads
// the Location header and accepts it when it has exactly the expected shape.
// Nothing the server sends reaches the terminal except the three numbers of a
// version that passed ParseRelease.
//
// Nothing here fails loudly: every problem means "no notice".
package update

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/online"
)

// Constants of the check.
const (
	// OptOutVar turns the check off when set to anything but "", "0" or "false".
	OptOutVar = "GITID_NO_UPDATE_CHECK"
	// LatestURL is the only address the check requests.
	LatestURL = "https://github.com/tinhnd1902/gitid/releases/latest"
	// Interval is how long an answer (or a failed attempt) is trusted.
	Interval = 24 * time.Hour
	// Timeout bounds the whole exchange.
	Timeout = 5 * time.Second

	latestHost    = "github.com"
	tagPathPrefix = "/tinhnd1902/gitid/releases/tag/"
	maxLocation   = 512 // a longer Location header is not a release
	// futureSlack is how far in the future the time of the last check may be
	// (a clock that was set back a little) before the cache is not trusted.
	futureSlack = 5 * time.Minute
)

// Method is how this gitid was installed, which decides the command shown.
type Method int

// Install methods.
const (
	MethodUnknown Method = iota
	MethodBrew
	MethodScript
)

// Notice says a newer release exists.
type Notice struct {
	// Latest is the newer version, "X.Y.Z" (digits and dots only).
	Latest string
	Method Method
}

// Checker runs the check. The zero value of the optional fields is safe.
type Checker struct {
	Env env.Env
	// Current is the running version ("0.1.8" or "v0.1.8"); anything that is
	// not a plain release (dev, a snapshot, a prerelease) disables the check.
	Current string
	// Executable returns the path of the running binary (os.Executable);
	// nil means the install method is unknown.
	Executable func() (string, error)
	// Timeout bounds the request (0: the package Timeout).
	Timeout time.Duration
}

var releaseRE = regexp.MustCompile(`^v?(0|[1-9][0-9]{0,4})\.(0|[1-9][0-9]{0,4})\.(0|[1-9][0-9]{0,4})$`)

// ParseRelease reads a plain release version, with or without a leading "v".
// Prereleases, build metadata, a fourth component, leading zeros, numbers over
// five digits, and anything with other characters are rejected.
func ParseRelease(s string) (v [3]int, ok bool) {
	m := releaseRE.FindStringSubmatch(s)
	if m == nil {
		return v, false
	}
	for i := range v {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return [3]int{}, false
		}
		v[i] = n
	}
	return v, true
}

func format(v [3]int) string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

// newer reports whether a is a later release than b, comparing each part as a
// number.
func newer(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// optedOut reports whether the user turned the check off.
func optedOut(e env.Env) bool {
	if e.LookupEnv == nil {
		return false
	}
	v, ok := e.LookupEnv(OptOutVar)
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false":
		return false
	}
	return true
}

// Check reports whether a newer release than Current exists. It never returns
// an error and never panics: every failure is "no notice". It makes at most
// one request and none while the cache is fresh, the check is turned off, or
// the running build is not a plain release.
func (c Checker) Check(ctx context.Context) (n Notice, ok bool) {
	defer func() {
		if recover() != nil {
			n, ok = Notice{}, false
		}
	}()
	if optedOut(c.Env) {
		return Notice{}, false
	}
	cur, isRelease := ParseRelease(c.Current)
	if !isRelease || ctx.Err() != nil {
		return Notice{}, false
	}
	path := c.Env.Paths().UpdateCheckFile
	cache, state := readCache(path, c.now())
	var latest [3]int
	switch state {
	case cacheBlocked:
		return Notice{}, false
	case cacheFresh:
		latest = cache.latest
	default:
		got, fetched := c.fetch(ctx, cur)
		if ctx.Err() != nil {
			return Notice{}, false // the program is leaving: not an attempt
		}
		latest = cache.latest // the last valid answer survives a failed attempt
		if fetched {
			latest = got
		}
		writeCache(c.Env, path, c.now(), latest)
	}
	if latest == ([3]int{}) || !newer(latest, cur) {
		return Notice{}, false
	}
	return Notice{Latest: format(latest), Method: c.method()}, true
}

func (c Checker) now() time.Time {
	if c.Env.Now != nil {
		return c.Env.Now()
	}
	return time.Now()
}

// fetch asks for the latest release. It returns the version the redirect
// names, or false for any other answer.
func (c Checker) fetch(ctx context.Context, cur [3]int) (latest [3]int, ok bool) {
	d := c.Timeout
	if d <= 0 {
		d = Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	rt, err := online.Transport(c.Env)
	if err != nil {
		return latest, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, LatestURL, nil)
	if err != nil {
		return latest, false
	}
	req.Header.Set("User-Agent", "gitid/"+format(cur))
	client := &http.Client{
		Transport: rt,
		// the redirect is the answer: it is never followed
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return latest, false
	}
	// the body is never read; closing it early drops the connection
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
	default:
		return latest, false
	}
	hdr := resp.Header.Get("Location")
	if hdr == "" || len(hdr) > maxLocation {
		return latest, false
	}
	ref, err := url.Parse(hdr)
	if err != nil {
		return latest, false
	}
	base, err := url.Parse(LatestURL)
	if err != nil {
		return latest, false
	}
	// a relative Location is read against the address that was asked for
	return tagOf(base.ResolveReference(ref))
}

// tagOf reads the version out of https://github.com/<project>/releases/tag/vX.Y.Z.
func tagOf(u *url.URL) ([3]int, bool) {
	if u.Scheme != "https" || !strings.EqualFold(u.Host, latestHost) || u.User != nil || u.Opaque != "" {
		return [3]int{}, false
	}
	tag, found := strings.CutPrefix(u.Path, tagPathPrefix)
	if !found {
		return [3]int{}, false
	}
	return ParseRelease(tag)
}

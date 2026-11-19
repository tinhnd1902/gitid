package online

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tinhnd1902/gitid/internal/env"
)

// Limits of the client.
const (
	DefaultMaxPages = 50      // pages per account
	MaxPagesLimit   = 500     // the most a caller may ask for
	MaxBodyBytes    = 8 << 20 // JSON body per page
	maxRedirects    = 5
	retryAfterMax   = 10 * time.Second // a longer wait is reported, not slept
	retryAfterCap   = 366 * 24 * 3600  // seconds; a Retry-After above this is read as this
	maxResetUnix    = 4_102_444_800    // 2100-01-01: a larger X-RateLimit-Reset is ignored
)

// Per request limits (NFR7): the whole exchange, including the body, is
// bounded by RequestTimeout.
const (
	dialTimeout           = 10 * time.Second
	tlsTimeout            = 10 * time.Second
	responseHeaderTimeout = 15 * time.Second
	RequestTimeout        = 30 * time.Second
)

// Client makes the GET requests of the listing. It never follows a redirect
// or a "next" link off the host of the first request, and it sends the token
// only in an Authorization header.
type Client struct {
	HTTP     *http.Client
	MaxPages int
	Version  string // for the User-Agent: gitid/<Version>
	Now      func() time.Time
	// Sleep waits for d or until ctx is done (tests replace it).
	Sleep func(ctx context.Context, d time.Duration) error
}

// NewClient builds the client for e. With e.HTTP set (tests) that transport
// is used as it is. Otherwise requests use a clone of the default transport:
// HTTPS_PROXY and NO_PROXY are honoured, TLS is 1.2 or newer with the system
// roots, plus the certificates of the file named by SSL_CERT_FILE. Go on
// macOS ignores that variable, so it is read here. Certificate verification
// is never turned off.
func NewClient(e env.Env) (*Client, error) {
	rt := e.HTTP
	if rt == nil {
		t, err := newTransport(e)
		if err != nil {
			return nil, err
		}
		rt = t
	}
	now := e.Now
	if now == nil {
		now = time.Now
	}
	return &Client{
		HTTP:     &http.Client{Transport: rt, Timeout: RequestTimeout, CheckRedirect: checkRedirect},
		MaxPages: DefaultMaxPages,
		Version:  "dev",
		Now:      now,
		Sleep:    sleepCtx,
	}, nil
}

func newTransport(e env.Env) (*http.Transport, error) {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = http.ProxyFromEnvironment
	t.DialContext = (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext
	t.TLSHandshakeTimeout = tlsTimeout
	t.ResponseHeaderTimeout = responseHeaderTimeout
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if e.LookupEnv != nil {
		if path, ok := e.LookupEnv("SSL_CERT_FILE"); ok && path != "" {
			pool, err := x509.SystemCertPool()
			if err != nil || pool == nil {
				pool = x509.NewCertPool()
			}
			pem, err := os.ReadFile(path)
			if err != nil {
				return nil, &Error{Kind: KindTLS, Detail: "SSL_CERT_FILE: " + Sanitize(errText(err))}
			}
			if !pool.AppendCertsFromPEM(pem) {
				return nil, &Error{Kind: KindTLS, Detail: "SSL_CERT_FILE: no certificate found"}
			}
			cfg.RootCAs = pool
		}
	}
	t.TLSClientConfig = cfg
	return t, nil
}

func errText(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error() // not the path
	}
	return err.Error()
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	switch {
	case len(via) >= maxRedirects:
		return &redirectError{"too many redirects"}
	case req.URL.Scheme != "https":
		return &redirectError{"not https"}
	case !strings.EqualFold(req.URL.Host, via[0].URL.Host):
		return &redirectError{"other host"}
	}
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// auth is the Authorization header of a request. scheme is "Bearer", "token"
// (Gitea) or "Basic"; for Basic the header holds base64(user ":" token).
type auth struct {
	scheme string
	user   string // Basic only; may be empty (Azure DevOps PAT)
	tok    Secret
}

func (a auth) apply(req *http.Request) {
	v := a.tok.reveal()
	if a.scheme == "Basic" {
		v = base64.StdEncoding.EncodeToString([]byte(a.user + ":" + v))
	}
	req.Header.Set("Authorization", a.scheme+" "+v)
}

// reply is a successful (200, JSON) answer.
type reply struct {
	header http.Header
	body   []byte
}

func (c *Client) userAgent() string {
	v := c.Version
	if v == "" {
		v = "dev"
	}
	return "gitid/" + v
}

func (c *Client) maxPages() int {
	switch {
	case c.MaxPages <= 0:
		return DefaultMaxPages
	case c.MaxPages > MaxPagesLimit:
		return MaxPagesLimit
	}
	return c.MaxPages
}

// get sends one GET request and returns the answer, or the classified
// failure. A rate limit that ends in 10 seconds or less is waited out once.
func (c *Client) get(ctx context.Context, rawURL, accept string, au auth, page int) (reply, *Error) {
	for attempt := 0; ; attempt++ {
		rep, wait, e := c.once(ctx, rawURL, accept, au)
		if e == nil {
			return rep, nil
		}
		e.Page = page
		if e.Kind != KindRateLimit || attempt > 0 || wait < 0 || wait > retryAfterMax {
			return reply{}, e
		}
		if err := c.Sleep(ctx, wait); err != nil {
			ce := classify(err, au.tok)
			ce.Page = page
			return reply{}, ce
		}
	}
}

// once is one request. wait is how long the platform asked to wait when the
// request was rate limited (-1: unknown).
func (c *Client) once(ctx context.Context, rawURL, accept string, au auth) (reply, time.Duration, *Error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return reply{}, -1, newError(KindBadResponse, 0, "bad request URL", au.tok)
	}
	req.Header.Set("User-Agent", c.userAgent())
	req.Header.Set("Accept", accept)
	au.apply(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return reply{}, -1, classify(err, au.tok)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodyBytes+1))
	if err != nil {
		return reply{}, -1, classify(err, au.tok)
	}
	if len(body) > MaxBodyBytes {
		return reply{}, -1, newError(KindBadResponse, resp.StatusCode, "answer too large", au.tok)
	}
	if resp.StatusCode == http.StatusOK {
		if !isJSON(resp.Header.Get("Content-Type")) {
			return reply{}, -1, newError(KindBadResponse, resp.StatusCode, "the answer is not JSON", au.tok)
		}
		return reply{header: resp.Header, body: body}, 0, nil
	}
	e, wait := c.failure(resp, body, au.tok)
	return reply{}, wait, e
}

func isJSON(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	return err == nil && (mt == "application/json" || strings.HasSuffix(mt, "+json"))
}

// apiMessage is the platform's own explanation: the "message" of GitHub and
// GitLab, or the "error_description" / "error" of an OAuth style answer.
func apiMessage(body []byte) (msg, code string) {
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return "", ""
	}
	str := func(k string) string { s, _ := m[k].(string); return s }
	code = str("error")
	for _, k := range []string{"message", "error_description", "error"} {
		if s := str(k); s != "" {
			return s, code
		}
	}
	return "", code
}

// failure classifies a non-200 answer.
func (c *Client) failure(resp *http.Response, body []byte, tok Secret) (*Error, time.Duration) {
	msg, code := apiMessage(body)
	st := resp.StatusCode
	h := resp.Header
	mk := func(k Kind) *Error { return newError(k, st, msg, tok) }
	switch {
	case st == http.StatusUnauthorized:
		if otpRequired(h) {
			return mk(KindOTP), -1
		}
		return mk(KindAuth), -1
	case st == http.StatusForbidden || st == http.StatusTooManyRequests:
		if sso := h.Get("X-GitHub-SSO"); st == http.StatusForbidden && strings.HasPrefix(sso, "required") {
			e := mk(KindSSO)
			e.SSOURL = ssoURL(sso)
			if RedactSecret(e.SSOURL, tok) != e.SSOURL { // a link that holds the token is not shown
				e.SSOURL = ""
			}
			return e, -1
		}
		if reset, wait, limited := c.rateInfo(h); st == http.StatusTooManyRequests || limited {
			e := mk(KindRateLimit)
			e.Reset = reset
			return e, wait
		}
		if code == "insufficient_scope" {
			return mk(KindScope), -1
		}
		if st == http.StatusForbidden {
			return mk(KindScope), -1
		}
	case st >= 500:
		return mk(KindServer), -1
	}
	return mk(KindBadResponse), -1
}

func otpRequired(h http.Header) bool {
	for _, k := range []string{"X-GitHub-OTP", "X-Gitea-OTP"} {
		if strings.HasPrefix(strings.ToLower(h.Get(k)), "required") {
			return true
		}
	}
	return false
}

var ssoURLRe = regexp.MustCompile(`url=([^\s;,]+)`)

// ssoURL takes the authorization URL out of "required; url=..." and keeps it
// only when it is an https URL without credentials.
func ssoURL(header string) string {
	m := ssoURLRe.FindStringSubmatch(header)
	if m == nil {
		return ""
	}
	u, err := url.Parse(m[1])
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return ""
	}
	return Sanitize(u.String())
}

// rateInfo reads the rate limit headers of a 403 or 429. reset is when the
// quota is back (zero when no header says) and wait the time to wait from
// now (-1 when unknown). limited is true when the answer itself says the
// quota is used up: a Retry-After, or a remaining count of 0.
func (c *Client) rateInfo(h http.Header) (reset time.Time, wait time.Duration, limited bool) {
	now := c.Now()
	wait = -1
	if ra := strings.TrimSpace(h.Get("Retry-After")); ra != "" {
		limited = true
		if n, err := strconv.Atoi(ra); err == nil && n >= 0 {
			// a huge value must not overflow time.Duration into a short wait
			d := time.Duration(min(n, retryAfterCap)) * time.Second
			return now.Add(d), d, true
		}
		if t, err := http.ParseTime(ra); err == nil {
			return t, maxDur(t.Sub(now), 0), true
		}
	}
	limited = limited || strings.TrimSpace(h.Get("X-RateLimit-Remaining")) == "0"
	for _, k := range []string{"X-RateLimit-Reset", "RateLimit-Reset"} {
		n, err := strconv.ParseInt(strings.TrimSpace(h.Get(k)), 10, 64)
		if err != nil || n < 0 {
			continue
		}
		t := now.Add(time.Duration(n) * time.Second)
		if n >= 1_000_000_000 { // Unix time (GitHub and GitLab)
			if n > maxResetUnix {
				continue // not a time: ignore the header
			}
			t = time.Unix(n, 0).UTC()
		}
		return t, maxDur(t.Sub(now), 0), limited
	}
	return time.Time{}, -1, limited
}

func maxDur(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

var linkNextRe = regexp.MustCompile(`<([^>]*)>\s*;[^,]*\brel="?next"?`)

// nextLink is the rel="next" URL of a Link header ("" when there is none).
func nextLink(h http.Header) string {
	for _, v := range h.Values("Link") {
		if m := linkNextRe.FindStringSubmatch(v); m != nil {
			return m[1]
		}
	}
	return ""
}

// pager walks the pages of a listing. handle gets each page's answer; the
// walk stops at the last page, at MaxPages (truncated) or on an error. A
// "next" link that is not https or not on the host of the first URL ends the
// walk with an error: the token is never sent anywhere else.
func (c *Client) pager(ctx context.Context, first, accept string, au auth, handle func(n int, r reply) *Error) (truncated bool, err *Error) {
	return c.pagerNext(ctx, first, accept, au, nil, handle)
}

// pagerNext is pager for platforms whose "next" page is not (only) in a Link
// header: when the answer has no rel="next" link, more is asked (after handle
// ran for the page) and a non-empty result is the next URL. The same rules
// apply to it: https, the host of the first URL, no credentials.
func (c *Client) pagerNext(ctx context.Context, first, accept string, au auth, more func(n int, r reply) string, handle func(n int, r reply) *Error) (truncated bool, err *Error) {
	fu, perr := url.Parse(first)
	if perr != nil {
		return false, newError(KindBadResponse, 0, "bad URL", au.tok)
	}
	next := first
	for n := 1; ; n++ {
		rep, e := c.get(ctx, next, accept, au, n)
		if e != nil {
			return false, e
		}
		if e := handle(n, rep); e != nil {
			e.Page = n
			return false, e
		}
		link := nextLink(rep.header)
		if link == "" && more != nil {
			link = more(n, rep)
		}
		if link == "" {
			return false, nil
		}
		if n >= c.maxPages() {
			return true, nil
		}
		lu, perr := fu.Parse(link) // a relative link resolves against the first URL
		if perr != nil || lu.Scheme != "https" || !strings.EqualFold(lu.Host, fu.Host) || lu.User != nil {
			e := newError(KindBadResponse, 0, "the next page is not on the same host", au.tok)
			e.Page = n
			return false, e
		}
		next = lu.String()
	}
}

// decodeJSON decodes a page; a broken body is a BadResponse.
func decodeJSON(body []byte, v any, tok Secret) *Error {
	if err := json.Unmarshal(body, v); err != nil {
		return newError(KindBadResponse, 200, "the answer is not valid JSON", tok)
	}
	return nil
}

// parseTime reads an RFC 3339 time; the zero time when absent or broken.
func parseTime(p *string) time.Time {
	if p == nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, *p)
	if err != nil {
		return time.Time{}
	}
	return t
}

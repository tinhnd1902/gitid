package online

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

// Kind classifies a failure of the Online listing.
type Kind string

// Kinds.
const (
	KindAuth         Kind = "auth"          // 401: the token is invalid or expired
	KindScope        Kind = "scope"         // 403: the token may not read repositories
	KindSSO          Kind = "sso"           // 403: SAML single sign-on has to authorize the token
	KindOTP          Kind = "otp"           // 401: the credential is a password that needs a one-time code
	KindRateLimit    Kind = "rate_limit"    // 429, or 403 with an exhausted quota
	KindNetwork      Kind = "network"       // connection refused or reset, no route
	KindDNS          Kind = "dns"           // the host name does not resolve
	KindTimeout      Kind = "timeout"       // the platform did not answer in time
	KindTLS          Kind = "tls"           // the certificate is not trusted
	KindProxy        Kind = "proxy"         // the proxy could not be reached
	KindServer       Kind = "server"        // 5xx
	KindBadResponse  Kind = "bad_response"  // not JSON, too big, a redirect or a link off the host
	KindWrongUser    Kind = "wrong_user"    // the token belongs to another user
	KindUnsupported  Kind = "unsupported"   // no listing for this platform or host
	KindInsecureHost Kind = "insecure_host" // the host would need plain http
	KindCancelled    Kind = "cancelled"
)

// Error is a failure of the Online listing. Nothing in it can hold a token:
// Detail is the API's own message, sanitized and redacted.
type Error struct {
	Kind   Kind
	Status int    // HTTP status, 0 when there was no answer
	Detail string // the platform's message, or the transport error, sanitized
	// Reset is when a rate limit ends (zero when unknown).
	Reset time.Time
	// SSOURL is where the user authorizes the token (https only; may be empty).
	SSOURL string
	// Login is the user the token belongs to (KindWrongUser).
	Login string
	// Page is the page that failed (1-based; 0: before the first page).
	Page int

	cause error
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("online: " + string(e.Kind))
	if e.Status != 0 {
		fmt.Fprintf(&b, " (HTTP %d)", e.Status)
	}
	if e.Detail != "" {
		b.WriteString(": " + e.Detail)
	}
	return b.String()
}

// Unwrap exposes the transport error (context.Canceled and the like) to
// errors.Is.
func (e *Error) Unwrap() error { return e.cause }

// newError builds an Error and makes sure no secret is in its text. The token
// is removed before the text is cleaned and cut (a cut in the middle of a
// token would leave a prefix that no longer matches) and once more after the
// cleaning (dropping an escape sequence inside a token joins its two halves).
// The cut comes last.
func newError(k Kind, status int, detail string, t Secret) *Error {
	return &Error{Kind: k, Status: status, Detail: safeText(detail, t)}
}

// redirectError is what CheckRedirect returns for a redirect it refuses.
type redirectError struct{ why string }

func (e *redirectError) Error() string { return "redirect refused: " + e.why }

// classify turns an error of http.Client.Do into an *Error.
func classify(err error, t Secret) *Error {
	var oe *Error
	if errors.As(err, &oe) {
		return oe
	}
	out := func(k Kind) *Error {
		e := newError(k, 0, causeText(err), t)
		e.cause = withoutURL(err)
		return e
	}
	var re *redirectError
	var dns *net.DNSError
	var ua x509.UnknownAuthorityError
	var he x509.HostnameError
	var ci x509.CertificateInvalidError
	var op *net.OpError
	var ne net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return out(KindCancelled)
	case errors.Is(err, context.DeadlineExceeded):
		return out(KindTimeout)
	case errors.As(err, &re):
		return out(KindBadResponse)
	case errors.As(err, &ua), errors.As(err, &he), errors.As(err, &ci):
		return out(KindTLS)
	case errors.As(err, &op) && op.Op == "proxyconnect":
		return out(KindProxy)
	case errors.As(err, &dns):
		if dns.IsTimeout {
			return out(KindTimeout)
		}
		return out(KindDNS)
	case errors.As(err, &ne) && ne.Timeout(), errors.Is(err, os.ErrDeadlineExceeded):
		return out(KindTimeout)
	}
	return out(KindNetwork)
}

// causeText is the text of a transport error without the URL (an *url.Error
// prints the whole request URL; the host is not secret but the query adds
// noise, and nothing about the request is needed in a message).
func causeText(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Op + ": " + ue.Err.Error()
	}
	return err.Error()
}

// withoutURL is err without the *url.Error wrapper, which holds the whole
// request URL (a redirect target chosen by the server, for one). The error
// underneath is kept for errors.Is and errors.As (context.Canceled and the
// like) and for anyone who unwraps and prints the chain.
func withoutURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return ue.Err
	}
	return err
}

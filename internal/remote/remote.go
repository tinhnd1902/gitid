// Package remote parses git remote URLs, rewrites them for a gitid account,
// builds the hasconfig patterns used in gitconfig includeIf entries and
// matches a remote to an account.
package remote

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/tinhnd1902/gitid/internal/platform"
)

// Errors.
var (
	// ErrInvalid wraps every Parse failure.
	ErrInvalid = errors.New("remote: invalid git remote URL")
	// ErrHostMismatch means the remote's host is not the account's host.
	ErrHostMismatch = errors.New("remote: remote host does not match the account host")
)

// Warning codes (i18n keys).
const (
	WarnHTTP       = "warn.http_insecure"
	WarnPassword   = "warn.password_in_url"
	WarnNoUsername = "warn.remote_no_username"
	WarnAmbiguous  = "warn.ambiguous_match"
)

// Kind is the syntax of the remote URL.
type Kind string

// Kinds.
const (
	KindHTTPS Kind = "https"
	KindHTTP  Kind = "http"
	KindSSH   Kind = "ssh"  // ssh://
	KindSCP   Kind = "scp"  // [user@]host:path
	azureLeaf      = "_git" // path marker for azure https
)

// Remote is a parsed git remote URL. The password of a URL is never kept.
type Remote struct {
	Kind         Kind
	User         string // percent-decoded
	Host         string // lowercase, no port
	Port         int    // 0 when absent
	Segs         []string
	Platform     platform.Platform // guessed from the host; "" if unknown
	HadGitSuffix bool
	HadPassword  bool
	Warnings     []string

	// Derived by interpret.
	Owner, Repo      string
	Azure            bool
	AzOrg, AzProject string
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

var hostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)

func azureHost(h string) bool {
	return h == "dev.azure.com" || h == "ssh.dev.azure.com" || h == "vs-ssh.visualstudio.com" || strings.HasSuffix(h, ".visualstudio.com")
}

func guessPlatform(h string) platform.Platform {
	switch {
	case h == "github.com":
		return platform.GitHub
	case h == "gitlab.com":
		return platform.GitLab
	case h == "bitbucket.org":
		return platform.Bitbucket
	case azureHost(h):
		return platform.AzureDevOps
	}
	return ""
}

// Parse parses a remote URL in https, http, ssh or scp-like form.
func Parse(input string) (Remote, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return Remote{}, invalid("empty")
	}
	if strings.HasPrefix(s, "-") {
		return Remote{}, invalid("must not start with '-'")
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == '\\' {
			return Remote{}, invalid("whitespace or control characters")
		}
	}
	if strings.ContainsAny(s, "?#'\"`$;&|<>*[]{}^") {
		return Remote{}, invalid("forbidden characters")
	}
	if strings.Contains(s, "::") && !strings.Contains(s, "://") {
		return Remote{}, invalid("transport helpers are not supported")
	}
	var r Remote
	var rawPath string
	if i := strings.Index(s, "://"); i >= 0 {
		scheme := strings.ToLower(s[:i])
		switch scheme {
		case "https":
			r.Kind = KindHTTPS
		case "http":
			r.Kind = KindHTTP
			r.Warnings = append(r.Warnings, WarnHTTP)
		case "ssh":
			r.Kind = KindSSH
		default:
			return Remote{}, invalid("unsupported scheme %q", scheme)
		}
		rest := s[i+3:]
		auth, path := rest, ""
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			auth, path = rest[:j], rest[j:]
		}
		hostport := auth
		if at := strings.LastIndexByte(auth, '@'); at >= 0 {
			ui := auth[:at]
			hostport = auth[at+1:]
			user, _, hasPass := strings.Cut(ui, ":")
			r.HadPassword = hasPass
			if u, err := url.PathUnescape(user); err == nil {
				r.User = u
			} else {
				return Remote{}, invalid("bad percent-encoding in user")
			}
		}
		host := hostport
		if k := strings.LastIndexByte(hostport, ':'); k >= 0 {
			p, err := strconv.Atoi(hostport[k+1:])
			if err != nil || p < 1 || p > 65535 {
				return Remote{}, invalid("bad port")
			}
			r.Port, host = p, hostport[:k]
		}
		r.Host = strings.ToLower(host)
		rawPath = path
	} else {
		// scp-like: [user@]host:path, colon before any slash.
		colon := strings.IndexByte(s, ':')
		slash := strings.IndexByte(s, '/')
		if colon < 0 || (slash >= 0 && slash < colon) || strings.HasPrefix(s, "~") || strings.HasPrefix(s, ".") {
			return Remote{}, invalid("not a remote URL")
		}
		r.Kind = KindSCP
		hostpart, path := s[:colon], s[colon+1:]
		if at := strings.LastIndexByte(hostpart, '@'); at >= 0 {
			r.User, hostpart = hostpart[:at], hostpart[at+1:]
		}
		r.Host = strings.ToLower(hostpart)
		rawPath = path
	}
	if r.Host == "" || !hostRe.MatchString(r.Host) || strings.Contains(r.Host, "..") {
		return Remote{}, invalid("bad host")
	}
	p := strings.Trim(rawPath, "/")
	if p == "" {
		return Remote{}, invalid("missing repository path")
	}
	r.Segs = strings.Split(p, "/")
	for _, seg := range r.Segs {
		if seg == "" || seg == "." || seg == ".." || strings.Contains(seg, ":") {
			return Remote{}, invalid("bad path segment")
		}
	}
	r.Platform = guessPlatform(r.Host)
	if r.HadPassword {
		r.Warnings = append(r.Warnings, WarnPassword)
	}
	azure := azureHost(r.Host) || r.hasGitMarker() ||
		(r.Platform == "" && (r.Kind == KindSSH || r.Kind == KindSCP) && len(r.Segs) == 4 && r.Segs[0] == "v3")
	if err := r.interpret(azure); err != nil {
		if azure && !azureHost(r.Host) {
			// tentative azure guess on an unknown host: fall back to generic
			if err2 := r.interpret(false); err2 == nil {
				return r, nil
			}
		}
		return Remote{}, err
	}
	return r, nil
}

func (r Remote) hasGitMarker() bool {
	for i, s := range r.Segs {
		if s == azureLeaf && i >= len(r.Segs)-3 && i < len(r.Segs)-1 {
			return true
		}
	}
	return false
}

// interpret (re)computes Owner/Repo for the generic or azure layout.
func (r *Remote) interpret(azure bool) error {
	r.Owner, r.Repo, r.Azure, r.AzOrg, r.AzProject = "", "", false, "", ""
	segs := r.Segs
	if !azure {
		if len(segs) < 2 {
			return invalid("expected owner/repo")
		}
		repo := segs[len(segs)-1]
		r.Owner = strings.Join(segs[:len(segs)-1], "/")
		r.Repo, r.HadGitSuffix = trimGit(repo)
		if r.Repo == "" {
			return invalid("empty repository name")
		}
		return nil
	}
	r.Azure = true
	switch r.Kind {
	case KindSSH, KindSCP:
		if len(segs) != 4 || segs[0] != "v3" {
			return invalid("azure ssh path must be v3/{org}/{project}/{repo}")
		}
		r.AzOrg, r.AzProject, r.Repo = segs[1], segs[2], segs[3]
	default:
		idx := -1
		for i, s := range segs {
			if s == azureLeaf {
				idx = i
			}
		}
		if idx < 0 || idx != len(segs)-2 {
			return invalid("azure https path must contain /_git/{repo}")
		}
		before := segs[:idx]
		if len(before) > 0 && before[0] == "DefaultCollection" && strings.HasSuffix(r.Host, ".visualstudio.com") {
			before = before[1:]
		}
		repo := segs[len(segs)-1]
		legacy := strings.HasSuffix(r.Host, ".visualstudio.com") && r.Host != "vs-ssh.visualstudio.com"
		switch {
		case legacy:
			r.AzOrg = strings.TrimSuffix(r.Host, ".visualstudio.com")
			switch len(before) {
			case 0:
				r.AzProject = repo
			case 1:
				r.AzProject = before[0]
			default:
				return invalid("bad azure legacy path")
			}
		default:
			switch len(before) {
			case 1:
				r.AzOrg, r.AzProject = before[0], repo
			case 2:
				r.AzOrg, r.AzProject = before[0], before[1]
			default:
				return invalid("azure https path must be {org}/{project}/_git/{repo}")
			}
		}
		r.Repo = repo
	}
	if r.AzOrg == "" || r.AzProject == "" || r.Repo == "" {
		return invalid("incomplete azure path")
	}
	r.Owner = r.AzOrg + "/" + r.AzProject
	r.HadGitSuffix = false
	return nil
}

func trimGit(s string) (string, bool) {
	if strings.HasSuffix(s, ".git") {
		return strings.TrimSuffix(s, ".git"), true
	}
	return s, false
}

// HostPort returns host[:port].
func (r Remote) HostPort() string {
	if r.Port > 0 {
		return r.Host + ":" + strconv.Itoa(r.Port)
	}
	return r.Host
}

// String renders the remote with any password masked as ***.
func (r Remote) String() string {
	path := strings.Join(r.Segs, "/")
	user := ""
	switch {
	case r.User != "" && r.HadPassword:
		user = url.User(r.User).String() + ":***@"
	case r.User != "":
		if r.Kind == KindSCP {
			user = r.User + "@"
		} else {
			user = url.User(r.User).String() + "@"
		}
	}
	switch r.Kind {
	case KindSCP:
		return user + r.Host + ":" + path
	default:
		return string(r.Kind) + "://" + user + r.HostPort() + "/" + path
	}
}

var credRe = regexp.MustCompile(`(://[^/@:\s]*):[^/@\s]*@`)

// Redact masks "user:password@" in any text that may contain URLs.
func Redact(s string) string { return credRe.ReplaceAllString(s, "$1:***@") }

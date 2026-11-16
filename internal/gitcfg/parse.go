package gitcfg

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/tinhnd1902/gitid/internal/block"
)

// IncludeIf is one [includeIf "<cond>"] entry with its path.
type IncludeIf struct {
	Condition string // e.g. gitdir:~/Code/x/
	Path      string
	Line      int // 1-based line of the section header
}

// Kind returns the condition keyword (gitdir, gitdir/i, hasconfig, onbranch).
func (i IncludeIf) Kind() string {
	k, _, _ := strings.Cut(i.Condition, ":")
	return k
}

// Target returns the condition after the keyword.
func (i IncludeIf) Target() string {
	_, v, _ := strings.Cut(i.Condition, ":")
	return v
}

var (
	includeIfRe = regexp.MustCompile(`(?i)^\[\s*includeIf\s+"((?:[^"\\]|\\.)*)"\s*\]\s*(?:[#;].*)?$`)
	sectionRe   = regexp.MustCompile(`^\[`)
	pathRe      = regexp.MustCompile(`(?i)^path\s*=\s*(.*)$`)
)

func unquote(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, `"`) {
		if end := strings.LastIndex(v, `"`); end > 0 {
			inner := v[1:end]
			inner = strings.ReplaceAll(inner, `\"`, `"`)
			return strings.ReplaceAll(inner, `\\`, `\`)
		}
	}
	for _, c := range []string{" #", " ;", "\t#", "\t;"} {
		if i := strings.Index(v, c); i >= 0 {
			v = v[:i]
		}
	}
	return strings.TrimSpace(v)
}

// ParseIncludeIfs lists the includeIf entries of a gitconfig text, ignoring
// everything inside the gitid managed block.
func ParseIncludeIfs(text string) []IncludeIf {
	if stripped, err := block.Remove(text, Markers); err == nil {
		text = stripped
	}
	var out []IncludeIf
	cur := -1
	for n, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if m := includeIfRe.FindStringSubmatch(line); m != nil {
			cond := strings.ReplaceAll(strings.ReplaceAll(m[1], `\"`, `"`), `\\`, `\`)
			out = append(out, IncludeIf{Condition: cond, Line: n + 1})
			cur = len(out) - 1
			continue
		}
		if sectionRe.MatchString(line) {
			cur = -1
			continue
		}
		if cur >= 0 {
			if m := pathRe.FindStringSubmatch(line); m != nil {
				out[cur].Path = unquote(m[1])
			}
		}
	}
	return out
}

// Version is a parsed git version.
type Version struct{ Major, Minor, Patch int }

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

// AtLeast reports v >= major.minor.
func (v Version) AtLeast(major, minor int) bool {
	return v.Major > major || (v.Major == major && v.Minor >= minor)
}

var versionRe = regexp.MustCompile(`git version (\d+)\.(\d+)(?:\.(\d+))?`)

// ParseVersion parses the output of `git version`.
func ParseVersion(out string) (Version, error) {
	m := versionRe.FindStringSubmatch(out)
	if m == nil {
		return Version{}, fmt.Errorf("gitcfg: cannot parse git version from %q", strings.TrimSpace(out))
	}
	atoi := func(s string) int { n, _ := strconv.Atoi(s); return n }
	return Version{atoi(m[1]), atoi(m[2]), atoi(m[3])}, nil
}

// MinGitVersion is the oldest git that supports hasconfig:remote.*.url.
var MinGitVersion = Version{2, 36, 0}

var (
	userSectionRe = regexp.MustCompile(`(?i)^\[\s*user\s*\]\s*(?:[#;].*)?$`)
	kvRe          = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9-]*)\s*=\s*(.*)$`)
)

// value decodes a config value: quoted strings with \" and \\ escapes, or a
// bare value up to an inline comment.
func value(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, `"`) {
		var b strings.Builder
		esc := false
		for _, r := range v[1:] {
			switch {
			case esc:
				b.WriteRune(r)
				esc = false
			case r == '\\':
				esc = true
			case r == '"':
				return b.String()
			default:
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	for i, r := range v {
		if (r == '#' || r == ';') && (i == 0 || v[i-1] == ' ' || v[i-1] == '\t') {
			return strings.TrimSpace(v[:i])
		}
	}
	return v
}

// ParseUser extracts user.name and user.email from a gitconfig text (the
// last value of each wins, as in git).
func ParseUser(text string) (name, email string) {
	in := false
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if strings.HasPrefix(line, "[") {
			in = userSectionRe.MatchString(line)
			continue
		}
		if !in {
			continue
		}
		if m := kvRe.FindStringSubmatch(line); m != nil {
			switch strings.ToLower(m[1]) {
			case "name":
				name = value(m[2])
			case "email":
				email = value(m[2])
			}
		}
	}
	return name, email
}

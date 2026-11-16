// Package importer proposes gitid companies from the user's existing git and
// ssh configuration. It only reads; it never writes and never opens keys.
package importer

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/fsx"
	"github.com/tinhnd1902/gitid/internal/gitcfg"
	"github.com/tinhnd1902/gitid/internal/sshcfg"
)

// Warning codes (i18n keys).
const (
	WarnTargetMissing = "warn.import.target_missing"
	WarnNoUser        = "warn.import.no_user"
	WarnSlugAdjusted  = "warn.import.slug_adjusted"
	WarnConfigured    = "warn.import.already_configured"
	WarnNoPath        = "warn.import.no_path"
)

// AllWarnings lists the warning keys (for the i18n test).
func AllWarnings() []string {
	return []string{WarnTargetMissing, WarnNoUser, WarnSlugAdjusted, WarnConfigured, WarnNoPath}
}

// Proposal is a company suggested by an existing includeIf gitdir entry.
type Proposal struct {
	Slug      string
	Name      string
	BaseDir   string // stored form: ~/.../ with trailing slash
	UserName  string
	UserEmail string
	Source    string // the gitconfig file the identity was read from (as written)
	Warnings  []string
	// Platform, Username and Auth are NOT guessed: the user must provide them.
}

// Result is everything Import found.
type Result struct {
	Proposals []Proposal
	Hosts     []sshcfg.HostEntry // info only
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugFor(dir string) (slug string, adjusted bool) {
	base := filepath.Base(strings.TrimRight(dir, "/"))
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(base), "-"), "-")
	if s == "" {
		s = "company"
	}
	if len(s) > 32 {
		s = strings.TrimRight(s[:32], "-")
	}
	return s, s != base
}

// Import scans ~/.gitconfig (outside the gitid block) and ~/.ssh/config.
func Import(e env.Env, cfg config.Config) (Result, error) {
	var res Result
	paths := e.Paths()
	gsnap, err := fsx.ReadSnapshot(paths.GitConfig)
	if err != nil {
		return res, err
	}
	used := map[string]bool{}
	baseDirs := map[string]bool{}
	for _, c := range cfg.Companies {
		used[c.Slug] = true
		baseDirs[config.NormalizeBaseDir(e, c.BaseDir)] = true
	}
	if gsnap.Exists {
		dir := filepath.Dir(gsnap.RealPath)
		for _, inc := range gitcfg.ParseIncludeIfs(string(gsnap.Data)) {
			if k := inc.Kind(); k != "gitdir" && k != "gitdir/i" {
				continue
			}
			target := inc.Target()
			if target == "" || !(strings.HasPrefix(target, "~/") || filepath.IsAbs(target)) {
				continue // relative gitdir patterns are not base directories
			}
			p := Proposal{BaseDir: config.NormalizeBaseDir(e, target), Source: inc.Path}
			var adj bool
			p.Slug, adj = slugFor(p.BaseDir)
			if adj {
				p.Warnings = append(p.Warnings, WarnSlugAdjusted)
			}
			if used[p.Slug] {
				base := strings.TrimRight(trimTo(p.Slug, 28), "-")
				for n := 2; used[p.Slug]; n++ {
					p.Slug = base + "-" + strconv.Itoa(n)
				}
			}
			used[p.Slug] = true
			p.Name = p.Slug
			if baseDirs[p.BaseDir] {
				p.Warnings = append(p.Warnings, WarnConfigured)
			}
			if inc.Path == "" {
				p.Warnings = append(p.Warnings, WarnNoPath)
			} else {
				tp := inc.Path
				if !strings.HasPrefix(tp, "~/") && !filepath.IsAbs(tp) {
					tp = filepath.Join(dir, tp)
				}
				ts, err := fsx.ReadSnapshot(e.Expand(tp))
				switch {
				case err != nil || !ts.Exists:
					p.Warnings = append(p.Warnings, WarnTargetMissing)
				default:
					p.UserName, p.UserEmail = gitcfg.ParseUser(string(ts.Data))
					if p.UserName == "" && p.UserEmail == "" {
						p.Warnings = append(p.Warnings, WarnNoUser)
					}
				}
			}
			res.Proposals = append(res.Proposals, p)
		}
	}
	ssnap, err := fsx.ReadSnapshot(paths.SSHConfig)
	if err != nil {
		return res, err
	}
	if ssnap.Exists {
		for _, h := range sshcfg.ParseHosts(string(ssnap.Data)) {
			if len(h.Patterns) == 1 && h.Patterns[0] == "*" {
				continue
			}
			res.Hosts = append(res.Hosts, h)
		}
	}
	return res, nil
}

func trimTo(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

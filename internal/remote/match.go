package remote

import (
	"path/filepath"
	"strings"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/env"
)

// Match reasons.
const (
	ReasonAlias    = "alias"
	ReasonUsername = "username"
	ReasonDefault  = "default"
)

// MatchResult is the outcome of Match.
type MatchResult struct {
	Account   *config.AccountRef
	Reason    string
	Ambiguous []config.AccountRef
	Warnings  []string
}

func inBaseDir(e env.Env, c config.Company, cwd string) bool {
	if cwd == "" || c.BaseDir == "" {
		return false
	}
	base := e.Expand(c.BaseDir)
	if env.IsUnder(cwd, base) {
		return true
	}
	// git reports real paths (/private/var on macOS): compare resolved forms too.
	rb, err1 := filepath.EvalSymlinks(base)
	rc, err2 := filepath.EvalSymlinks(cwd)
	if err1 != nil {
		rb = base
	}
	if err2 != nil {
		rc = cwd
	}
	return (err1 == nil || err2 == nil) && env.IsUnder(rc, rb)
}

func ownerMatches(owner, org string) bool {
	return owner == org || strings.HasPrefix(owner, org+"/")
}

// Match finds the account a remote belongs to, in the priority order of the
// plan: ssh alias, then host+username (narrowed by orgs and base dir), then
// the default account of the company whose base dir contains cwd.
func Match(cfg config.Config, e env.Env, r Remote, cwd string) MatchResult {
	res := MatchResult{Warnings: append([]string(nil), r.Warnings...)}
	if _, ref := Resolve(cfg, r); ref != nil {
		res.Account, res.Reason = ref, ReasonAlias
		return res
	}
	var hostCands []config.AccountRef
	for _, ref := range cfg.All() {
		if hostMatches(ref.Account, r) {
			hostCands = append(hostCands, ref)
		}
	}
	userIsAccount := r.User != "" && (r.Kind == KindHTTPS || r.Kind == KindHTTP) && !(r.Azure && r.User == r.AzOrg)
	if userIsAccount {
		var cands []config.AccountRef
		for _, ref := range hostCands {
			if ref.Account.Username != r.User {
				continue
			}
			if len(ref.Account.Orgs) > 0 {
				ok := false
				for _, o := range ref.Account.Orgs {
					if ownerMatches(r.Owner, o) {
						ok = true
					}
				}
				if !ok {
					continue
				}
			}
			cands = append(cands, ref)
		}
		if len(cands) > 1 {
			var narrowed []config.AccountRef
			for _, c := range cands {
				if len(c.Account.Orgs) > 0 {
					narrowed = append(narrowed, c)
				}
			}
			if len(narrowed) > 0 {
				cands = narrowed
			}
		}
		if len(cands) > 1 {
			var inDir []config.AccountRef
			for _, c := range cands {
				if inBaseDir(e, c.Company, cwd) {
					inDir = append(inDir, c)
				}
			}
			if len(inDir) > 0 {
				cands = inDir
			}
		}
		switch len(cands) {
		case 0:
		case 1:
			res.Account, res.Reason = &cands[0], ReasonUsername
		default:
			res.Ambiguous = cands
			res.Warnings = append(res.Warnings, WarnAmbiguous)
		}
		return res
	}
	// No usable username: fall back to the default account of the company
	// that owns cwd.
	for _, ref := range hostCands {
		if inBaseDir(e, ref.Company, cwd) && ref.Company.DefaultAccount == ref.Account.ID {
			ref := ref
			res.Account, res.Reason = &ref, ReasonDefault
			if r.Kind == KindHTTPS || r.Kind == KindHTTP {
				res.Warnings = append(res.Warnings, WarnNoUsername)
			}
			return res
		}
	}
	return res
}

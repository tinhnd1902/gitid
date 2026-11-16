package app

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/gitops"
	"github.com/tinhnd1902/gitid/internal/remote"
)

// Check warning codes (i18n keys); remote.Warn* codes are reused too.
const (
	WarnEmailMismatch  = "warn.check.email_mismatch"
	WarnLocalOverride  = "warn.check.local_override"
	WarnNoUserOutside  = "warn.check.no_username_outside"
	WarnPendingApply   = "warn.check.pending_apply"
	WarnNoAccount      = "warn.check.no_account"
	WarnRemoteUnparsed = "warn.check.remote_unparsable"
	WarnNoRemote       = "warn.check.no_remote"
	WarnNoEmail        = "warn.check.no_email"
)

// AllWarnings lists every warning key the app can emit (for the i18n test).
func AllWarnings() []string {
	return []string{WarnEmailMismatch, WarnLocalOverride, WarnNoUserOutside, WarnPendingApply, WarnNoAccount, WarnRemoteUnparsed, WarnNoRemote, WarnNoEmail,
		remote.WarnHTTP, remote.WarnPassword, remote.WarnNoUsername, remote.WarnAmbiguous}
}

// CheckResult is the outcome of checking a repository (stable JSON schema).
type CheckResult struct {
	Repo           string   `json:"repo"`
	Remote         string   `json:"remote"`
	EffectiveEmail string   `json:"effective_email"`
	EmailOrigin    string   `json:"email_origin"`
	Account        string   `json:"account"`
	Reason         string   `json:"reason"`
	ExpectedEmail  string   `json:"expected_email,omitempty"`
	Warnings       []string `json:"warnings"`
}

// OK reports whether the check found nothing to warn about.
func (r CheckResult) OK() bool { return len(r.Warnings) == 0 }

func pickRemote(rs []gitops.Remote, name string) (gitops.Remote, bool) {
	for _, r := range rs {
		if r.Name == name {
			return r, true
		}
	}
	return gitops.Remote{}, false
}

// Check inspects the repository containing dir: which account its remote
// maps to, which email git actually uses and why.
func (s *Service) Check(ctx context.Context, cfg config.Config, dir string) (CheckResult, error) {
	cfg = config.Normalize(cfg, s.Env)
	top, err := gitops.Toplevel(ctx, s.Run, dir)
	if err != nil {
		return CheckResult{}, err
	}
	res := CheckResult{Repo: top, Warnings: []string{}}
	add := func(w string) {
		for _, x := range res.Warnings {
			if x == w {
				return
			}
		}
		res.Warnings = append(res.Warnings, w)
	}
	remotes, err := gitops.Remotes(ctx, s.Run, top)
	if err != nil {
		return res, err
	}
	var rem remote.Remote
	haveRemote := false
	if r, ok := pickRemote(remotes, "origin"); ok {
		remotes = []gitops.Remote{r}
	}
	if len(remotes) > 0 {
		res.Remote = remote.Redact(remotes[0].URL)
		if parsed, perr := remote.Parse(remotes[0].URL); perr == nil {
			rem, haveRemote = parsed, true
			res.Remote = parsed.String()
		} else {
			add(WarnRemoteUnparsed)
		}
	} else {
		add(WarnNoRemote)
	}

	var expected *config.AccountRef
	ambiguous := false
	if haveRemote {
		m := remote.Match(cfg, s.Env, rem, top)
		for _, w := range m.Warnings {
			add(w)
		}
		ambiguous = len(m.Ambiguous) > 0
		if m.Account != nil {
			expected = m.Account
			res.Account, res.Reason = m.Account.String(), m.Reason
		}
	} else {
		// no usable remote: fall back to the company whose base dir holds the repo
		for _, c := range cfg.Companies {
			if def, ok := c.Default(); ok && baseDirContains(s.Env, c, top) {
				ref := config.AccountRef{Company: c, Account: def}
				expected = &ref
				res.Account, res.Reason = ref.String(), "default"
				break
			}
		}
	}
	if expected == nil && haveRemote && !ambiguous {
		add(WarnNoAccount)
		if (rem.Kind == remote.KindHTTPS || rem.Kind == remote.KindHTTP) && rem.User == "" && !anyBaseDir(s.Env, cfg, top) {
			add(WarnNoUserOutside)
		}
	}

	ev, err := gitops.ConfigGet(ctx, s.Run, top, "user.email")
	switch {
	case err == nil:
		res.EffectiveEmail, res.EmailOrigin = ev.Value, ev.Origin
		if ev.IsRepoLocal() {
			add(WarnLocalOverride)
		}
	case errors.Is(err, gitops.ErrNotSet):
		add(WarnNoEmail)
	default:
		return res, err
	}
	if expected != nil {
		res.ExpectedEmail = expected.Account.Email
		if res.EffectiveEmail != "" && res.EffectiveEmail != expected.Account.Email {
			add(WarnEmailMismatch)
		}
	}
	if p, perr := s.BuildPlan(cfg); perr == nil && !p.Empty() {
		add(WarnPendingApply)
	}
	return res, nil
}

func baseDirContains(e env.Env, c config.Company, dir string) bool {
	return anyBaseDir(e, config.Config{Companies: []config.Company{c}}, dir)
}

func anyBaseDir(e env.Env, cfg config.Config, dir string) bool {
	for _, c := range cfg.Companies {
		base := e.Expand(c.BaseDir)
		if env.IsUnder(dir, base) {
			return true
		}
		rb, err1 := filepath.EvalSymlinks(base)
		rd, err2 := filepath.EvalSymlinks(dir)
		if err1 == nil && err2 == nil && env.IsUnder(rd, rb) {
			return true
		}
	}
	return false
}

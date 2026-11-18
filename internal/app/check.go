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

// evalInput is what is known about a repository before it is classified:
// the data a git call (or a scan) collected, nothing else.
type evalInput struct {
	Top      string // repository root (used for the base dir rules)
	Remotes  []gitops.Remote
	Email    gitops.ConfigValue
	HasEmail bool
}

// evalOutput is the classification of one repository. Check and the local
// scan share it, so both always agree on what is wrong.
type evalOutput struct {
	Result     CheckResult // Warnings do not include WarnPendingApply
	Remote     remote.Remote
	HaveRemote bool
	RawURL     string // the URL of the remote that was picked (origin, else the first)
	NoRemote   bool
	Unparsed   bool
	Match      remote.MatchResult
	Expected   *config.AccountRef
	Ambiguous  bool
}

// evaluate classifies a repository: which account its remote maps to, what
// the effective email says about it, and the warnings that follow. It is pure
// (no I/O except the symlink resolution of the base dir rules).
func evaluate(e env.Env, cfg config.Config, in evalInput) evalOutput {
	out := evalOutput{Result: CheckResult{Repo: in.Top, Warnings: []string{}}}
	res := &out.Result
	add := func(w string) {
		for _, x := range res.Warnings {
			if x == w {
				return
			}
		}
		res.Warnings = append(res.Warnings, w)
	}
	remotes := in.Remotes
	if r, ok := pickRemote(remotes, "origin"); ok {
		remotes = []gitops.Remote{r}
	}
	if len(remotes) > 0 {
		out.RawURL = remotes[0].URL
		res.Remote = remote.Redact(remotes[0].URL)
		if parsed, perr := remote.Parse(remotes[0].URL); perr == nil {
			out.Remote, out.HaveRemote = parsed, true
			res.Remote = parsed.String()
		} else {
			out.Unparsed = true
			add(WarnRemoteUnparsed)
		}
	} else {
		out.NoRemote = true
		add(WarnNoRemote)
	}

	if out.HaveRemote {
		m := remote.Match(cfg, e, out.Remote, in.Top)
		out.Match = m
		for _, w := range m.Warnings {
			add(w)
		}
		out.Ambiguous = len(m.Ambiguous) > 0
		if m.Account != nil {
			out.Expected = m.Account
			res.Account, res.Reason = m.Account.String(), m.Reason
		}
	} else {
		// no usable remote: fall back to the company whose base dir holds the repo
		for _, c := range cfg.Companies {
			if def, ok := c.Default(); ok && baseDirContains(e, c, in.Top) {
				ref := config.AccountRef{Company: c, Account: def}
				out.Expected = &ref
				res.Account, res.Reason = ref.String(), "default"
				break
			}
		}
	}
	if out.Expected == nil && out.HaveRemote && !out.Ambiguous {
		add(WarnNoAccount)
		if (out.Remote.Kind == remote.KindHTTPS || out.Remote.Kind == remote.KindHTTP) && out.Remote.User == "" && !anyBaseDir(e, cfg, in.Top) {
			add(WarnNoUserOutside)
		}
	}

	if in.HasEmail {
		res.EffectiveEmail, res.EmailOrigin = in.Email.Value, in.Email.Origin
		if in.Email.IsRepoLocal() {
			add(WarnLocalOverride)
		}
	} else {
		add(WarnNoEmail)
	}
	if out.Expected != nil {
		res.ExpectedEmail = out.Expected.Account.Email
		if res.EffectiveEmail != "" && res.EffectiveEmail != out.Expected.Account.Email {
			add(WarnEmailMismatch)
		}
	}
	return out
}

// Check inspects the repository containing dir: which account its remote
// maps to, which email git actually uses and why.
func (s *Service) Check(ctx context.Context, cfg config.Config, dir string) (CheckResult, error) {
	cfg = config.Normalize(cfg, s.Env)
	top, err := gitops.Toplevel(ctx, s.Run, dir)
	if err != nil {
		return CheckResult{}, err
	}
	remotes, err := gitops.Remotes(ctx, s.Run, top)
	if err != nil {
		return CheckResult{Repo: top, Warnings: []string{}}, err
	}
	in := evalInput{Top: top, Remotes: remotes}
	ev, err := gitops.ConfigGet(ctx, s.Run, top, "user.email")
	switch {
	case err == nil:
		in.Email, in.HasEmail = ev, true
	case errors.Is(err, gitops.ErrNotSet):
	default:
		// the warnings gathered so far are still reported with the error
		partial := evaluate(s.Env, cfg, in).Result
		partial.ExpectedEmail = ""
		kept := []string{}
		for _, w := range partial.Warnings {
			if w != WarnNoEmail && w != WarnEmailMismatch {
				kept = append(kept, w)
			}
		}
		partial.Warnings = kept
		return partial, err
	}
	res := evaluate(s.Env, cfg, in).Result
	if p, perr := s.BuildPlan(cfg); perr == nil && !p.Empty() {
		res.Warnings = append(res.Warnings, WarnPendingApply)
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

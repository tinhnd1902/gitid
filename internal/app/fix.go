package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/gitops"
	"github.com/tinhnd1902/gitid/internal/remote"
)

// FixRequest asks to rewrite a remote URL for an account.
type FixRequest struct {
	Dir     string
	Remote  string // default "origin"
	Account string // "company/account"; empty = detect from the remote
	DryRun  bool
	Force   bool
}

// FixResult is the outcome of FixRemote.
type FixResult struct {
	Remote  string `json:"remote"`
	Old     string `json:"old"` // credentials masked
	New     string `json:"new"`
	Account string `json:"account"`
	Changed bool   `json:"changed"`
	DryRun  bool   `json:"dry_run"`
}

// PlanFix computes the rewritten URL without changing anything.
func (s *Service) PlanFix(ctx context.Context, cfg config.Config, req FixRequest) (FixResult, string, string, error) {
	cfg = config.Normalize(cfg, s.Env)
	name := req.Remote
	if name == "" {
		name = "origin"
	}
	top, err := gitops.Toplevel(ctx, s.Run, req.Dir)
	if err != nil {
		return FixResult{}, "", "", err
	}
	rs, err := gitops.Remotes(ctx, s.Run, top)
	if err != nil {
		return FixResult{}, "", "", err
	}
	cur, ok := pickRemote(rs, name)
	if !ok {
		return FixResult{}, "", "", fmt.Errorf("%w: %s", ErrNoRemote, name)
	}
	parsed, err := remote.Parse(cur.URL)
	if err != nil {
		return FixResult{}, "", "", err
	}
	var ref config.AccountRef
	if req.Account != "" {
		ref, err = cfg.ParseRef(req.Account)
		if err != nil {
			return FixResult{}, "", "", err
		}
	} else {
		m := remote.Match(cfg, s.Env, parsed, top)
		switch {
		case m.Account != nil:
			ref = *m.Account
		case len(m.Ambiguous) > 0:
			return FixResult{}, "", "", fmt.Errorf("%w: %s", ErrAmbiguous, joinRefs(m.Ambiguous))
		default:
			return FixResult{}, "", "", ErrNoAccount
		}
	}
	newURL, _, err := remote.Rewrite(cfg, ref, cur.URL, remote.Options{Force: req.Force})
	if err != nil {
		return FixResult{}, "", "", err
	}
	res := FixResult{Remote: name, Old: parsed.String(), New: newURL, Account: ref.String(), DryRun: req.DryRun}
	// compare against the original text without credentials
	res.Changed = newURL != strings.TrimSpace(cur.URL)
	return res, top, cur.URL, nil
}

func joinRefs(rs []config.AccountRef) string {
	var out []string
	for _, r := range rs {
		out = append(out, r.String())
	}
	return strings.Join(out, ", ")
}

// FixRemote rewrites the remote URL for the account. When the URL is already
// right (or DryRun is set) git is not asked to change anything.
func (s *Service) FixRemote(ctx context.Context, cfg config.Config, req FixRequest) (FixResult, error) {
	res, top, _, err := s.PlanFix(ctx, cfg, req)
	if err != nil {
		return res, err
	}
	if !res.Changed || req.DryRun {
		return res, nil
	}
	if err := gitops.SetURL(ctx, s.Run, top, res.Remote, res.New); err != nil {
		return res, err
	}
	return res, nil
}

// IsUserError reports whether err is a mistake by the user (as opposed to a
// failure of the environment).
func IsUserError(err error) bool {
	for _, target := range []error{ErrNoAccount, ErrAmbiguous, ErrNoRemote, config.ErrRefNotFound, config.ErrRefAmbiguous, remote.ErrInvalid, remote.ErrHostMismatch, gitops.ErrNotRepo} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

package app

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/gitops"
	"github.com/tinhnd1902/gitid/internal/remote"
)

// CloneRequest describes a clone.
type CloneRequest struct {
	URL     string
	Account string // "company/account"
	Dir     string // optional target; relative paths resolve against Cwd
	Cwd     string
	Force   bool
}

// ClonePlan is a resolved clone, ready to run or to show as a preview.
type ClonePlan struct {
	Account  config.AccountRef
	Original string // credentials masked
	URL      string // the rewritten URL that will be cloned
	Dir      string // absolute target
	Warnings []string
}

// PlanClone resolves the URL and target directory. It executes nothing and
// touches nothing.
func (s *Service) PlanClone(cfg config.Config, req CloneRequest) (ClonePlan, error) {
	cfg = config.Normalize(cfg, s.Env)
	ref, err := cfg.ParseRef(req.Account)
	if err != nil {
		return ClonePlan{}, err
	}
	newURL, parsed, err := remote.Rewrite(cfg, ref, req.URL, remote.Options{Force: req.Force})
	if err != nil {
		return ClonePlan{}, err
	}
	if err := gitops.ValidateURL(newURL); err != nil {
		return ClonePlan{}, err
	}
	p := ClonePlan{Account: ref, Original: parsed.String(), URL: newURL, Warnings: append([]string(nil), parsed.Warnings...)}
	switch {
	case req.Dir == "":
		if err := gitops.ValidateDirName(parsed.Repo); err != nil {
			return ClonePlan{}, err
		}
		p.Dir = filepath.Join(s.Env.Expand(ref.Company.BaseDir), parsed.Repo)
	default:
		d := s.Env.Expand(req.Dir)
		if !filepath.IsAbs(d) {
			if req.Cwd == "" || !filepath.IsAbs(req.Cwd) {
				return ClonePlan{}, gitops.ErrBadTarget
			}
			d = filepath.Join(req.Cwd, d)
		}
		if filepath.Base(d) == "" || filepath.Base(d) == "." || filepath.Base(d) == "/" {
			return ClonePlan{}, gitops.ErrBadTarget
		}
		if err := gitops.ValidateDirName(filepath.Base(d)); err != nil {
			return ClonePlan{}, err
		}
		p.Dir = d
	}
	if err := gitops.CheckTarget(p.Dir); err != nil {
		return ClonePlan{}, err
	}
	return p, nil
}

// CloneResult is the outcome of a finished clone.
type CloneResult struct {
	Plan           ClonePlan
	EffectiveEmail string
	EmailOK        bool
}

// Clone runs the clone in batch mode (no prompts) and then verifies which
// email git would use in the new repository. A *gitops.CloneError that
// satisfies errors.Is(err, gitops.ErrAuth) means the caller may offer
// CloneTerminal.
func (s *Service) Clone(ctx context.Context, p ClonePlan, onProgress func(gitops.Progress)) (CloneResult, error) {
	if err := gitops.Clone(ctx, s.Run, p.URL, p.Dir, onProgress); err != nil {
		return CloneResult{Plan: p}, err
	}
	return s.verifyClone(ctx, p), nil
}

// CloneCmd returns the interactive (terminal) clone command for the TUI to
// run through tea.ExecProcess; call VerifyClone afterwards.
func (s *Service) CloneCmd(p ClonePlan) (execx.Cmd, error) {
	return gitops.CloneCmd(p.URL, p.Dir, false)
}

// CloneTerminal runs the clone attached to the terminal (prompts allowed).
func (s *Service) CloneTerminal(ctx context.Context, p ClonePlan) (CloneResult, error) {
	cmd, err := s.CloneCmd(p)
	if err != nil {
		return CloneResult{Plan: p}, err
	}
	if err := s.Run.RunTTY(ctx, cmd); err != nil {
		return CloneResult{Plan: p}, err
	}
	return s.verifyClone(ctx, p), nil
}

// VerifyClone reads the effective email of a finished clone.
func (s *Service) VerifyClone(ctx context.Context, p ClonePlan) CloneResult {
	return s.verifyClone(ctx, p)
}

func (s *Service) verifyClone(ctx context.Context, p ClonePlan) CloneResult {
	res := CloneResult{Plan: p}
	if v, err := gitops.ConfigGet(ctx, s.Run, p.Dir, "user.email"); err == nil {
		res.EffectiveEmail = v.Value
		res.EmailOK = v.Value == p.Account.Account.Email
	} else if !errors.Is(err, gitops.ErrNotSet) {
		res.EffectiveEmail = ""
	}
	return res
}

// Candidates ranks the accounts for a pasted URL: accounts on the URL's host
// first (config order), then the rest.
func (s *Service) Candidates(cfg config.Config, url string) []config.AccountRef {
	cfg = config.Normalize(cfg, s.Env)
	all := cfg.All()
	parsed, err := remote.Parse(url)
	if err != nil {
		return all
	}
	resolved, _ := remote.Resolve(cfg, parsed)
	var hit, rest []config.AccountRef
	for _, r := range all {
		if remote.OnHost(r.Account, resolved) {
			hit = append(hit, r)
		} else {
			rest = append(rest, r)
		}
	}
	return append(hit, rest...)
}

package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/gitops"
	"github.com/tinhnd1902/gitid/internal/keys"
	"github.com/tinhnd1902/gitid/internal/online"
	"github.com/tinhnd1902/gitid/internal/remote"
)

// This file is the Online side of the repo browser: which accounts can be
// listed, where their tokens come from, the listing itself, and the clone
// request for a repository of the list. A token never leaves this file: the
// caller sees the source of a token ("gh", "env", ...) and nothing else.

// Limits on work running at the same time (NFR7).
const (
	MaxResolveAtOnce = 4 // accounts whose token source is being looked up
	MaxFetchAtOnce   = 3 // accounts whose repositories are being fetched
)

// Why an account has no listing.
const OnlineUnsupportedPlatform = "platform"

// ErrNoToken is returned by ListOnline for an account without a token.
var ErrNoToken = errors.New("app: no token for this account")

// ErrUnclonable is returned for a repository whose name cannot be turned
// into a clone URL gitid trusts.
var ErrUnclonable = errors.New("app: this repository name cannot be cloned")

// onlineState is the lazily built machinery of the Online listing.
type onlineState struct {
	once      sync.Once
	cache     *online.Cache
	resolveSm chan struct{}
	fetchSm   chan struct{}

	clientOnce sync.Once
	client     *online.Client
	clientErr  error
}

func (s *Service) onlineInit() *onlineState {
	o := &s.onl
	o.once.Do(func() {
		o.cache = online.NewCache()
		o.resolveSm = make(chan struct{}, MaxResolveAtOnce)
		o.fetchSm = make(chan struct{}, MaxFetchAtOnce)
	})
	return o
}

func (s *Service) onlineClient() (*online.Client, error) {
	o := s.onlineInit()
	o.clientOnce.Do(func() {
		o.client, o.clientErr = online.NewClient(s.Env)
		if o.client != nil && s.Version != "" {
			o.client.Version = s.Version
		}
	})
	return o.client, o.clientErr
}

// acquire takes a slot of sm or gives up when ctx ends.
func acquire(ctx context.Context, sm chan struct{}) error {
	select {
	case sm <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// OnlineAccount is a configured account as the Online tab sees it.
type OnlineAccount struct {
	Ref       config.AccountRef
	Account   online.Account
	Key       string // cache key: ref, platform, host and username
	EnvName   string // the variable that holds the token of the account
	Supported bool   // the listing knows the platform
	Reason    string // why not (OnlineUnsupportedPlatform)
}

func toOnline(ref config.AccountRef) online.Account {
	a := ref.Account
	return online.Account{
		Ref: ref.String(), Slug: ref.Company.Slug, ID: a.ID, Platform: a.Platform,
		Host: a.EffectiveHost(), Username: a.Username, Email: a.Email, Orgs: append([]string(nil), a.Orgs...),
	}
}

// OnlineAccounts lists every account of cfg in config order.
func (s *Service) OnlineAccounts(cfg config.Config) []OnlineAccount {
	cfg = config.Normalize(cfg, s.Env)
	var out []OnlineAccount
	for _, ref := range cfg.All() {
		oa := toOnline(ref)
		acc := OnlineAccount{Ref: ref, Account: oa, Key: oa.Key(), EnvName: oa.EnvName(), Supported: online.Supported(ref.Account.Platform)}
		if !acc.Supported {
			acc.Reason = OnlineUnsupportedPlatform
		}
		out = append(out, acc)
	}
	return out
}

func (s *Service) allOnline(cfg config.Config) []online.Account {
	var out []online.Account
	for _, ref := range cfg.All() {
		out = append(out, toOnline(ref))
	}
	return out
}

// TokenInfo says where the token of an account comes from. It never holds
// the token.
type TokenInfo struct {
	Source online.Source
	Note   string // why a source was skipped (online.Note*)
	// LooksLikePassword: git's credential helper returned something that is
	// not in the form of a token of the platform.
	LooksLikePassword bool
}

func (s *Service) tokenFor(ctx context.Context, cfg config.Config, ref config.AccountRef) (online.Token, error) {
	o := s.onlineInit()
	if err := acquire(ctx, o.resolveSm); err != nil {
		return online.Token{Source: online.SourceNone}, err
	}
	defer func() { <-o.resolveSm }()
	r := online.Resolver{Env: s.Env, Run: s.Run, UseGitCredential: true}
	return o.cache.Resolve(ctx, r, toOnline(ref), s.allOnline(cfg))
}

// ResolveSource finds the token of the account (at most MaxResolveAtOnce at
// the same time) and reports its source. The token stays in memory in the
// Service; ForgetToken drops it.
func (s *Service) ResolveSource(ctx context.Context, cfg config.Config, ref string) (TokenInfo, error) {
	cfg = config.Normalize(cfg, s.Env)
	r, err := cfg.ParseRef(ref)
	if err != nil {
		return TokenInfo{Source: online.SourceNone}, err
	}
	tok, err := s.tokenFor(ctx, cfg, r)
	if err != nil {
		return TokenInfo{Source: online.SourceNone}, err
	}
	return TokenInfo{Source: tok.Source, Note: tok.Note, LooksLikePassword: tok.LooksLikePassword(r.Account.Platform)}, nil
}

// ForgetToken drops the remembered token of an account, so the next
// ResolveSource looks the sources up again.
func (s *Service) ForgetToken(key string) { s.onlineInit().cache.Forget(key) }

// KeepTokens drops the remembered tokens of every account that is no longer
// in cfg, or whose platform, host or username changed.
func (s *Service) KeepTokens(cfg config.Config) {
	keep := map[string]bool{}
	for _, a := range s.OnlineAccounts(cfg) {
		keep[a.Key] = true
	}
	s.onlineInit().cache.Keep(keep)
}

// OnlineResult is the outcome of a listing.
type OnlineResult struct {
	Source            online.Source
	Login             string
	Truncated         bool
	Warnings          []string // online.Warn*
	Pages, Count      int
	LooksLikePassword bool
}

// ListOnline fetches the repositories of the account, page by page (at most
// MaxFetchAtOnce accounts at the same time). onPage is called from the
// calling goroutine as pages arrive. The error is an *online.Error (use
// errors.As), ErrNoToken, or the error of the context.
func (s *Service) ListOnline(ctx context.Context, cfg config.Config, ref string, onPage func(online.Page)) (OnlineResult, error) {
	cfg = config.Normalize(cfg, s.Env)
	r, err := cfg.ParseRef(ref)
	if err != nil {
		return OnlineResult{}, err
	}
	res := OnlineResult{Source: online.SourceNone}
	if !online.Supported(r.Account.Platform) {
		return res, &online.Error{Kind: online.KindUnsupported, Detail: string(r.Account.Platform)}
	}
	tok, err := s.tokenFor(ctx, cfg, r)
	if err != nil {
		return res, err
	}
	res.Source = tok.Source
	res.LooksLikePassword = tok.LooksLikePassword(r.Account.Platform)
	if !tok.Found() {
		return res, ErrNoToken
	}
	c, err := s.onlineClient()
	if err != nil {
		return res, err
	}
	o := s.onlineInit()
	if err := acquire(ctx, o.fetchSm); err != nil {
		return res, err
	}
	defer func() { <-o.fetchSm }()
	lr, err := c.List(ctx, toOnline(r), tok, onPage)
	res.Login, res.Truncated, res.Warnings, res.Pages, res.Count = lr.Login, lr.Truncated, lr.Warnings, lr.Pages, lr.Count
	return res, err
}

// OnlineRepoKey is the RepoKey of an online repository as the account would
// clone it ("" when the repository cannot be cloned).
func (s *Service) OnlineRepoKey(cfg config.Config, repo online.Repo) string {
	if repo.CloneURL == "" {
		return ""
	}
	rem, err := remote.Parse(repo.CloneURL)
	if err != nil {
		return ""
	}
	cfg = config.Normalize(cfg, s.Env)
	resolved, _ := remote.Resolve(cfg, rem)
	return RepoKey(resolved)
}

// OnlineCloneURL is the URL the account would clone the repository from
// (rendered for the account: https with its username, or its ssh alias), for
// copying. It touches no folder.
func (s *Service) OnlineCloneURL(cfg config.Config, ref string, repo online.Repo) (string, error) {
	cfg = config.Normalize(cfg, s.Env)
	r, err := cfg.ParseRef(ref)
	if err != nil {
		return "", err
	}
	if repo.CloneURL == "" {
		return "", ErrUnclonable
	}
	out, _, err := remote.Rewrite(cfg, r, repo.CloneURL, remote.Options{})
	return out, err
}

// OpenURL opens an https URL in the browser (macOS `open`).
func (s *Service) OpenURL(ctx context.Context, url string) error {
	return keys.OpenURL(ctx, s.Env, s.Run, url)
}

// Warnings of an OnlineClone (i18n keys; Arg is the %s of the message).
const (
	OnlineWarnDirFallback = "online.warn.dir_fallback"  // the folder was taken; another name is used
	OnlineWarnOtherAcc    = "online.warn.other_account" // git would use another account for this clone
	OnlineWarnNoAccount   = "online.warn.no_account"    // no account matches the clone URL
)

// OnlineWarning is a note about a clone.
type OnlineWarning struct {
	Key string
	Arg string
}

// OnlineClone is what the Online tab needs to open the clone flow for a
// repository, or to open the check screen when it is cloned already.
type OnlineClone struct {
	Request       CloneRequest // the URL (as configured, no username) and the account
	Plan          ClonePlan    // the preview: rendered URL and target folder
	Warnings      []OnlineWarning
	AlreadyCloned []LocalRepo // the local clones; when set, there is no request
	Key           string
}

// OnlineCloneRequest prepares the clone of a repository of the list for the
// account ref. The clone URL is built from the account's configured host and
// the repository path and rendered through PlanClone, like any pasted URL.
// When the repository is among the local clones, AlreadyCloned is returned
// and nothing else. When <base_dir>/<repo> exists (another repository), the
// target becomes <base_dir>/<owner>-<repo> with a warning; when that exists
// too, the error wraps gitops.ErrTargetExists. The API token plays no part:
// git clones with its own credentials.
func (s *Service) OnlineCloneRequest(cfg config.Config, ref string, repo online.Repo, idx LocalIndex) (OnlineClone, error) {
	cfg = config.Normalize(cfg, s.Env)
	r, err := cfg.ParseRef(ref)
	if err != nil {
		return OnlineClone{}, err
	}
	if repo.CloneURL == "" {
		return OnlineClone{}, ErrUnclonable
	}
	rem, err := remote.Parse(repo.CloneURL)
	if err != nil {
		return OnlineClone{}, fmt.Errorf("%w: %v", ErrUnclonable, err)
	}
	resolved, _ := remote.Resolve(cfg, rem)
	out := OnlineClone{Key: RepoKey(resolved)}
	if local := idx[out.Key]; out.Key != "" && len(local) > 0 {
		out.AlreadyCloned = append([]LocalRepo(nil), local...)
		return out, nil
	}
	out.Request = CloneRequest{URL: repo.CloneURL, Account: r.String()}
	plan, err := s.PlanClone(cfg, out.Request)
	if errors.Is(err, gitops.ErrTargetExists) {
		alt := filepath.Join(s.Env.Expand(r.Company.BaseDir), strings.ReplaceAll(repo.Owner, "/", "-")+"-"+repo.Name)
		alt2 := out.Request
		alt2.Dir = alt
		var err2 error
		if plan, err2 = s.PlanClone(cfg, alt2); err2 != nil {
			return out, err2
		}
		out.Request = alt2
		err = nil
		out.Warnings = append(out.Warnings, OnlineWarning{Key: OnlineWarnDirFallback, Arg: s.Env.Contract(alt)})
	}
	if err != nil {
		return out, err
	}
	out.Plan = plan
	// which account would git use for the URL that is going to be cloned?
	if cloned, perr := remote.Parse(plan.URL); perr == nil {
		m := remote.Match(cfg, s.Env, cloned, plan.Dir)
		switch {
		case m.Account == nil:
			out.Warnings = append(out.Warnings, OnlineWarning{Key: OnlineWarnNoAccount})
		case m.Account.String() != r.String():
			out.Warnings = append(out.Warnings, OnlineWarning{Key: OnlineWarnOtherAcc, Arg: m.Account.String()})
		}
	}
	return out, nil
}

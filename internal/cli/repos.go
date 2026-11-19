package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/online"
	"github.com/tinhnd1902/gitid/internal/platform"
	"github.com/tinhnd1902/gitid/internal/scan"
)

// textWidth is the width every line of `gitid repos` fits in.
const textWidth = 80

// ---- JSON schema (plan section 7) ----

type reposJSON struct {
	Local  *reposLocalJSON  `json:"local,omitempty"`
	Online *reposOnlineJSON `json:"online,omitempty"`
}

type reposLocalJSON struct {
	Companies []localCompanyJSON `json:"companies"`
}

type localCompanyJSON struct {
	Slug      string          `json:"slug"`
	BaseDir   string          `json:"base_dir"`
	State     string          `json:"state"`
	Truncated bool            `json:"truncated"`
	Denied    int             `json:"denied"`
	Repos     []localRepoJSON `json:"repos"`
}

type localRepoJSON struct {
	Path           string   `json:"path"`
	Kind           string   `json:"kind"`
	Remote         string   `json:"remote"`
	Key            string   `json:"key"`
	Account        string   `json:"account"`
	Reason         string   `json:"reason"`
	EffectiveEmail string   `json:"effective_email"`
	EmailOrigin    string   `json:"email_origin"`
	ExpectedEmail  string   `json:"expected_email"`
	Status         string   `json:"status"`
	Flags          []string `json:"flags"`
	BelongsTo      string   `json:"belongs_to"`
	Duplicates     []string `json:"duplicates"`
	FixTo          string   `json:"fix_to"`
}

type reposOnlineJSON struct {
	Accounts []onlineAccountJSON `json:"accounts"`
}

type onlineAccountJSON struct {
	Account     string           `json:"account"`
	Platform    string           `json:"platform"`
	Host        string           `json:"host"`
	TokenSource string           `json:"token_source"`
	Login       string           `json:"login"`
	State       string           `json:"state"`
	Error       string           `json:"error"`
	ErrorKind   string           `json:"error_kind"`
	Truncated   bool             `json:"truncated"`
	Warnings    []string         `json:"warnings"`
	Repos       []onlineRepoJSON `json:"repos"`
}

type onlineRepoJSON struct {
	FullName   string   `json:"full_name"`
	Owner      string   `json:"owner"`
	Name       string   `json:"name"`
	Private    bool     `json:"private"`
	Archived   bool     `json:"archived"`
	Fork       bool     `json:"fork"`
	Empty      bool     `json:"empty"`
	UpdatedAt  *string  `json:"updated_at"`
	WebURL     string   `json:"web_url"`
	CloneURL   string   `json:"clone_url"`
	LocalPaths []string `json:"local_paths"`
}

// States of an online account in the output.
const (
	stateOK          = "ok"
	stateNoToken     = "no_token"
	stateError       = "error"
	stateCancelled   = "cancelled"
	stateUnsupported = "unsupported"
)

// ---- the command ----

type reposOpts struct {
	company          string
	depth, maxPages  int
	problems, online bool
	local, asJSON    bool
	account          string
}

func (c *cli) reposCmd() *cobra.Command {
	var o reposOpts
	cmd := &cobra.Command{
		Use:   "repos",
		Short: i18n.T("cli.repos.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("depth") && o.depth < 1 {
				return c.failf("cli.repos.depth_invalid")
			}
			if cmd.Flags().Changed("max-pages") && (o.maxPages < 1 || o.maxPages > online.MaxPagesLimit) {
				return c.failf("cli.repos.pages_invalid")
			}
			return c.runRepos(cmd.Context(), o)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.company, "company", "", i18n.T("cli.flag.company"))
	f.IntVar(&o.depth, "depth", 0, i18n.T("cli.flag.depth"))
	f.BoolVar(&o.problems, "problems", false, i18n.T("cli.flag.problems"))
	f.BoolVar(&o.online, "online", false, i18n.T("cli.flag.online"))
	f.BoolVar(&o.local, "local", false, i18n.T("cli.flag.local"))
	f.StringVar(&o.account, "account", "", i18n.T("cli.flag.account"))
	f.IntVar(&o.maxPages, "max-pages", 0, i18n.T("cli.flag.max_pages"))
	f.BoolVar(&o.asJSON, "json", false, i18n.T("cli.flag.json"))
	return cmd
}

// failf is an error with a translated message that makes the command exit 1.
func (c *cli) failf(key string, args ...any) error {
	return &exitError{code: ExitError, err: errors.New(i18n.T(key, args...))}
}

func (c *cli) runRepos(ctx context.Context, o reposOpts) error {
	cfg, err := c.load()
	if err != nil {
		return err
	}
	scanCfg := cfg
	if o.company != "" {
		found := false
		for _, co := range cfg.Companies {
			if co.Slug == o.company {
				scanCfg = config.Config{Version: cfg.Version, Settings: cfg.Settings, Companies: []config.Company{co}}
				found = true
			}
		}
		if !found {
			return c.failf("cli.repos.company_none", o.company)
		}
	}
	wantLocal := !o.online || o.local
	if o.online && o.account != "" {
		if _, err := config.Normalize(cfg, c.d.Env).ParseRef(o.account); err != nil {
			return err
		}
	}
	if o.maxPages > 0 {
		c.svc.OnlineMaxPages = o.maxPages
	}

	// the local scan runs for the local listing and, with --online, for the
	// local_paths of each repository
	var raws []app.RawRepo
	states := map[string]app.CompanyDone{}
	c.svc.ScanLocal(ctx, scanCfg, app.ScanOptions{MaxDepth: o.depth}, func(ev app.LocalEvent) {
		if ev.Raw != nil {
			raws = append(raws, *ev.Raw)
		}
		if ev.Done != nil {
			states[ev.Company] = *ev.Done
		}
	})
	scans := app.ClassifyLocal(cfg, c.d.Env, raws, states)
	if o.company != "" {
		var keep []app.CompanyScan
		for _, s := range scans {
			if s.Slug == o.company {
				keep = append(keep, s)
			}
		}
		scans = keep
	}
	cancelled := ctx.Err() != nil

	var out reposJSON
	var onlineAccs []onlineView
	if wantLocal {
		out.Local = c.buildLocal(scans, o.problems)
	}
	if o.online && !cancelled {
		onlineAccs, cancelled = c.runOnline(ctx, cfg, o, app.IndexLocal(scans))
		out.Online = &reposOnlineJSON{Accounts: []onlineAccountJSON{}}
		for _, v := range onlineAccs {
			out.Online.Accounts = append(out.Online.Accounts, v.json)
		}
	}

	if o.asJSON {
		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		c.println(string(b))
	} else {
		if wantLocal {
			c.printLocal(scans, o.problems)
		}
		if o.online {
			c.printOnline(onlineAccs)
		}
	}
	if cancelled {
		fmt.Fprintln(c.err, i18n.T("cli.repos.cancelled"))
		return &exitError{code: ExitError}
	}

	problems := false
	if wantLocal {
		for _, s := range scans {
			if s.Problems() > 0 {
				problems = true
			}
		}
	}
	for _, v := range onlineAccs {
		if v.json.State == stateError {
			problems = true
		}
	}
	if problems {
		return problem()
	}
	return nil
}

// ---- local ----

func (c *cli) buildLocal(scans []app.CompanyScan, onlyProblems bool) *reposLocalJSON {
	out := &reposLocalJSON{Companies: []localCompanyJSON{}}
	for _, s := range scans {
		lc := localCompanyJSON{
			Slug: s.Slug, BaseDir: s.BaseDir, State: s.State, Truncated: s.Stats.Truncated,
			Denied: s.Stats.DeniedCount, Repos: []localRepoJSON{},
		}
		for _, r := range s.Repos {
			if onlyProblems && !r.Problem() {
				continue
			}
			lc.Repos = append(lc.Repos, localRepoJSON{
				Path: r.Path, Kind: string(r.Kind), Remote: r.Remote, Key: r.Key, Account: r.Account,
				Reason: r.Reason, EffectiveEmail: r.EffectiveEmail, EmailOrigin: r.EmailOrigin,
				ExpectedEmail: r.ExpectedEmail, Status: string(r.Status), Flags: nonNil(r.Flags),
				BelongsTo: r.BelongsTo, Duplicates: nonNil(r.Duplicates), FixTo: r.FixTo,
			})
		}
		out.Companies = append(out.Companies, lc)
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// fit cuts s to w columns (with an ellipsis) and pads it to w.
func fit(s string, w int) string {
	s = online.Sanitize(s)
	if ansi.StringWidth(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	return s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
}

// line writes s cut to the output width.
func (c *cli) line(s string) {
	if ansi.StringWidth(s) > textWidth {
		s = ansi.Truncate(s, textWidth, "…")
	}
	c.println(strings.TrimRight(s, " "))
}

// wrapped writes text under a prefix (first line) and an indent (the rest),
// broken into lines of at most 80 columns, so a hint is never cut.
func (c *cli) wrapped(first, rest, text string) {
	w := max(textWidth-max(ansi.StringWidth(first), ansi.StringWidth(rest)), 20)
	for i, l := range strings.Split(ansi.Wrap(online.Sanitize(text), w, ""), "\n") {
		if i == 0 {
			c.line(first + l)
		} else {
			c.line(rest + l)
		}
	}
}

func countText(n int, one, many string) string {
	if n == 1 {
		return i18n.T(one)
	}
	return i18n.T(many, n)
}

// repoNote is the short status text of a local repository.
func repoNote(r app.LocalRepo) string {
	var parts []string
	if r.Status != app.StatusOK && r.Status != app.StatusBare {
		parts = append(parts, i18n.T("tui.repos.status."+string(r.Status)))
	}
	for _, f := range r.Flags {
		switch f {
		case app.FlagWrongFolder:
			parts = append(parts, i18n.T("tui.repos.flag.wrong_folder", r.BelongsTo))
		case app.FlagDuplicate:
			parts = append(parts, i18n.T("tui.repos.flag.duplicate", len(r.Duplicates)+1))
		default:
			parts = append(parts, i18n.T("tui.repos.flag."+f))
		}
	}
	if r.Kind != scan.KindRepo {
		parts = append(parts, i18n.T("tui.repos.kind."+string(r.Kind)))
	}
	return strings.Join(parts, " · ")
}

func (c *cli) printLocal(scans []app.CompanyScan, onlyProblems bool) {
	for _, s := range scans {
		head := s.Slug + "  " + s.BaseDir
		if s.State == app.ScanOK {
			head += "  " + countText(len(s.Repos), "tui.repos.local.repos_one", "tui.repos.local.repos_many")
			if p := s.Problems(); p > 0 {
				head += " · " + countText(p, "tui.repos.local.problems_one", "tui.repos.local.problems_many")
			}
		}
		c.line(head)
		switch s.State {
		case app.ScanMissing:
			c.line("  " + i18n.T("tui.repos.local.missing", s.BaseDir))
		case app.ScanNotDir:
			c.line("  " + i18n.T("tui.repos.local.not_dir", s.BaseDir))
		case app.ScanCancelled:
			c.line("  " + i18n.T("tui.repos.local.company_cancelled"))
		}
		if s.Stats.Truncated {
			c.line("  " + i18n.T("tui.repos.local.truncated", fmt.Sprint(s.Stats.Dirs)))
		}
		if n := s.Stats.DeniedCount; n > 0 {
			list := strings.Join(s.Stats.Denied[:min(len(s.Stats.Denied), 3)], ", ")
			if n == 1 {
				c.line("  " + i18n.T("tui.repos.local.denied_one", list))
			} else {
				c.line("  " + i18n.T("tui.repos.local.denied_many", n, list))
			}
		}
		if s.State == app.ScanOK && len(s.Repos) == 0 {
			c.line("  " + i18n.T("tui.repos.local.empty", s.BaseDir))
		}
		for _, r := range s.Repos {
			if onlyProblems && !r.Problem() {
				continue
			}
			mark := "  "
			if r.Problem() {
				mark = "! "
			}
			rel := online.Sanitize(c.svc.RelPath(s.BaseDir, r.Path))
			acct := r.Account
			if acct == "" {
				acct = "-"
			}
			// 2 (mark) + path 34 + 1 + account 18 + 1 + note
			c.line(mark + fit(rel, 34) + " " + fit(acct, 18) + " " + repoNote(r))
		}
	}
}

// ---- online ----

type onlineView struct {
	json  onlineAccountJSON
	acc   app.OnlineAccount
	repos []online.Repo
	hint  string
	login string
	pages int
}

// runOnline lists every selected account, one after the other. It stops when
// ctx ends (cancelled is true) and keeps what it has.
func (c *cli) runOnline(ctx context.Context, cfg config.Config, o reposOpts, idx app.LocalIndex) (views []onlineView, cancelled bool) {
	accs := c.svc.OnlineAccounts(cfg)
	if o.account != "" {
		ref, _ := config.Normalize(cfg, c.d.Env).ParseRef(o.account) // checked before the scan
		var sel []app.OnlineAccount
		for _, a := range accs {
			if a.Ref.String() == ref.String() {
				sel = append(sel, a)
			}
		}
		accs = sel
	}
	for _, a := range accs {
		if o.company != "" && a.Ref.Company.Slug != o.company {
			continue
		}
		if ctx.Err() != nil {
			return views, true
		}
		v := c.listAccount(ctx, cfg, a, idx)
		views = append(views, v)
		if v.json.State == stateCancelled {
			return views, true
		}
	}
	return views, ctx.Err() != nil
}

func (c *cli) listAccount(ctx context.Context, cfg config.Config, a app.OnlineAccount, idx app.LocalIndex) onlineView {
	v := onlineView{acc: a, json: onlineAccountJSON{
		Account: a.Ref.String(), Platform: string(a.Account.Platform), Host: a.Account.Host,
		TokenSource: string(online.SourceNone), State: stateOK, Warnings: []string{}, Repos: []onlineRepoJSON{},
	}}
	if !a.Supported {
		v.json.State = stateUnsupported
		return v
	}
	res, err := c.svc.ListOnline(ctx, cfg, a.Ref.String(), func(p online.Page) {
		for _, r := range p.Repos {
			v.repos = append(v.repos, r)
		}
	})
	v.json.TokenSource = string(res.Source)
	v.json.Login = res.Login
	v.login = res.Login
	v.pages = res.Pages
	v.json.Truncated = res.Truncated
	v.json.Warnings = append(v.json.Warnings, res.Warnings...)
	switch {
	case err == nil:
	case errors.Is(err, app.ErrNoToken):
		v.json.State = stateNoToken
		info, _ := c.svc.ResolveSource(ctx, cfg, a.Ref.String())
		v.hint = c.noTokenText(a, info.Note)
		v.json.Error = v.hint
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) && ctx.Err() != nil:
		v.json.State = stateCancelled
	case offHost(a, err):
		// Bitbucket Data Center, Azure DevOps Server: nothing was requested
		v.json.State = stateUnsupported
	default:
		v.json.State = stateError
		msg, hint, kind := c.onlineFailure(a, res, err)
		v.json.Error, v.json.ErrorKind, v.hint = msg, kind, hint
	}
	for _, r := range v.repos {
		jr := onlineRepoJSON{
			FullName: r.FullPath, Owner: r.Owner, Name: r.Name, Private: r.Private, Archived: r.Archived,
			Fork: r.Fork, Empty: r.Empty, WebURL: r.WebURL, LocalPaths: []string{},
		}
		if !r.Updated.IsZero() {
			s := r.Updated.UTC().Format(time.RFC3339)
			jr.UpdatedAt = &s
		}
		if u, err := c.svc.OnlineCloneURL(cfg, a.Ref.String(), r); err == nil {
			jr.CloneURL = u
		}
		if key := c.svc.OnlineRepoKey(cfg, r); key != "" {
			for _, l := range idx[key] {
				jr.LocalPaths = append(jr.LocalPaths, l.Path)
			}
			sort.Strings(jr.LocalPaths)
		}
		v.json.Repos = append(v.json.Repos, jr)
	}
	return v
}

// offHost reports an account of Bitbucket or Azure DevOps on a host the
// listing does not serve (only bitbucket.org and dev.azure.com are).
func offHost(a app.OnlineAccount, err error) bool {
	var oe *online.Error
	if !errors.As(err, &oe) || oe.Kind != online.KindUnsupported {
		return false
	}
	p := a.Account.Platform
	return p == platform.Bitbucket || p == platform.AzureDevOps
}

func (c *cli) noTokenText(a app.OnlineAccount, note string) string {
	switch note {
	case online.NoteEnvAmbiguous:
		return i18n.T("tui.repos.online.env_ambiguous", a.EnvName)
	case online.NoteInvalidValue:
		return i18n.T("tui.repos.online.invalid_value")
	}
	switch a.Account.Platform {
	case platform.GitLab:
		return i18n.T("tui.repos.online.no_token.gitlab", a.EnvName)
	case platform.Gitea:
		return i18n.T("tui.repos.online.no_token.gitea", a.EnvName, a.Ref.String())
	case platform.Bitbucket:
		return i18n.T("tui.repos.online.no_token.bitbucket", a.EnvName, a.Ref.String())
	case platform.AzureDevOps:
		return i18n.T("tui.repos.online.no_token.azure", a.EnvName, a.Ref.String())
	}
	return i18n.T("tui.repos.online.no_token.github", a.EnvName)
}

// onlineFailure is the message, the hint and the error kind of a failed
// listing. Both texts are built from fixed strings, the kind and the
// platform's message (already sanitized and redacted by package online).
func (c *cli) onlineFailure(a app.OnlineAccount, res app.OnlineResult, err error) (msg, hint, kind string) {
	var oe *online.Error
	if !errors.As(err, &oe) {
		return i18n.T("tui.repos.online.err.other"), "", "other"
	}
	kind = string(oe.Kind)
	withDetail := func(m string) string {
		if oe.Detail != "" {
			return m + " (" + oe.Detail + ")"
		}
		return m
	}
	authHint := func() string {
		switch res.Source {
		case online.SourceEnv:
			return i18n.T("tui.repos.online.hint.auth.env", a.EnvName)
		case online.SourceKeychain:
			return i18n.T("tui.repos.online.hint.auth.keychain", online.KeychainAccount(a.Account))
		case online.SourceGH:
			return i18n.T("tui.repos.online.hint.auth.gh")
		case online.SourceGlab:
			return i18n.T("tui.repos.online.hint.auth.glab")
		case online.SourceGit:
			if res.LooksLikePassword {
				return i18n.T("tui.repos.online.hint.auth.git_password", a.EnvName)
			}
			return i18n.T("tui.repos.online.hint.auth.git", a.EnvName)
		}
		return ""
	}
	if a.Account.Platform == platform.AzureDevOps {
		switch {
		case oe.Kind == online.KindBadResponse && oe.Detail == online.AzureSignInHint:
			return i18n.T("tui.repos.online.err.auth"), i18n.T("tui.repos.online.hint.azure_signin"), kind
		case len(a.Account.Orgs) == 0 && (oe.Kind == online.KindAuth || oe.Kind == online.KindScope || oe.Kind == online.KindBadResponse):
			return withDetail(i18n.T("tui.repos.online.err.auth")), i18n.T("tui.repos.online.hint.azure_orgs"), kind
		}
	}
	switch oe.Kind {
	case online.KindAuth:
		return withDetail(i18n.T("tui.repos.online.err.auth")), authHint(), kind
	case online.KindOTP:
		return i18n.T("tui.repos.online.err.otp"), i18n.T("tui.repos.online.hint.otp", a.EnvName), kind
	case online.KindScope:
		k := "tui.repos.online.hint.scope.github"
		switch a.Account.Platform {
		case platform.GitLab:
			k = "tui.repos.online.hint.scope.gitlab"
		case platform.Gitea:
			k = "tui.repos.online.hint.scope.gitea"
		case platform.Bitbucket:
			k = "tui.repos.online.hint.scope.bitbucket"
		case platform.AzureDevOps:
			k = "tui.repos.online.hint.scope.azure"
		}
		return withDetail(i18n.T("tui.repos.online.err.scope")), i18n.T(k), kind
	case online.KindSSO:
		if oe.SSOURL != "" {
			return i18n.T("tui.repos.online.err.sso"), i18n.T("tui.repos.online.hint.sso", oe.SSOURL), kind
		}
		return i18n.T("tui.repos.online.err.sso"), i18n.T("tui.repos.online.hint.sso_nourl"), kind
	case online.KindRateLimit:
		if !oe.Reset.IsZero() {
			return i18n.T("tui.repos.online.err.rate_limit", oe.Reset.In(time.Local).Format("15:04")), "", kind
		}
		return i18n.T("tui.repos.online.err.rate_unknown"), "", kind
	case online.KindWrongUser:
		return i18n.T("tui.repos.online.err.wrong_user", oe.Login), i18n.T("tui.repos.online.hint.wrong_user", a.EnvName), kind
	case online.KindDNS:
		return i18n.T("tui.repos.online.err.dns"), "", kind
	case online.KindNetwork:
		return i18n.T("tui.repos.online.err.network"), "", kind
	case online.KindTimeout:
		return i18n.T("tui.repos.online.err.timeout"), "", kind
	case online.KindServer:
		return withDetail(i18n.T("tui.repos.online.err.server", oe.Status)), "", kind
	case online.KindTLS:
		return i18n.T("tui.repos.online.err.tls"), i18n.T("tui.repos.online.hint.tls"), kind
	case online.KindProxy:
		return i18n.T("tui.repos.online.err.proxy"), i18n.T("tui.repos.online.hint.proxy"), kind
	case online.KindBadResponse:
		return withDetail(i18n.T("tui.repos.online.err.bad_response")), i18n.T("tui.repos.online.hint.bad_response"), kind
	case online.KindInsecureHost, online.KindUnsupported:
		return i18n.T("tui.repos.online.err.insecure"), i18n.T("tui.repos.online.hint.bad_response"), kind
	case online.KindCancelled:
		return i18n.T("tui.repos.online.err.cancelled"), "", kind
	}
	return withDetail(i18n.T("tui.repos.online.err.other")), "", kind
}

// age is the time since t in one short unit (3d, 5h, 2mo).
func age(now, t time.Time) string {
	if t.IsZero() {
		return "–"
	}
	d := now.Sub(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", max(int(d/time.Minute), 0))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d/(30*24*time.Hour)))
	}
	return fmt.Sprintf("%dy", int(d/(365*24*time.Hour)))
}

func badges(r online.Repo) string {
	var b []string
	for _, f := range []struct {
		on  bool
		key string
	}{{r.Private, "private"}, {r.Archived, "archived"}, {r.Fork, "fork"}, {r.Empty, "empty"}} {
		if f.on {
			b = append(b, i18n.T("tui.repos.online.badge."+f.key))
		}
	}
	return strings.Join(b, " ")
}

func (c *cli) printOnline(views []onlineView) {
	now := c.d.Env.Now()
	for _, v := range views {
		c.line(v.json.Account + "  " + v.acc.Ref.Account.Platform.DisplayName() + "  " + v.json.Host)
		var meta []string
		switch v.json.State {
		case stateNoToken:
			meta = append(meta, i18n.T("cli.repos.token_none"))
		case stateUnsupported:
			meta = append(meta, i18n.T("cli.repos.unsupported", v.acc.Ref.Account.Platform.DisplayName()))
		default:
			meta = append(meta, i18n.T("cli.repos.token", v.json.TokenSource))
			if v.login != "" {
				meta = append(meta, i18n.T("cli.repos.login", v.login))
			}
			if v.json.State == stateOK {
				meta = append(meta, countText(len(v.repos), "tui.repos.local.repos_one", "tui.repos.local.repos_many"))
			}
		}
		c.line("  " + strings.Join(meta, " · "))
		switch v.json.State {
		case stateNoToken:
			c.wrapped("  ", "  ", v.hint)
		case stateError:
			c.wrapped("! ✗ ", "    ", v.json.Error)
			if v.hint != "" {
				c.wrapped("    ", "    ", v.hint)
			}
		case stateCancelled:
			c.line("! " + i18n.T("tui.repos.online.err.cancelled"))
		}
		for _, w := range v.json.Warnings {
			code, name := online.WarnParts(w)
			if name == "" {
				c.wrapped("  ! ", "    ", i18n.T("tui.repos.online.warn."+code))
			} else {
				c.wrapped("  ! ", "    ", i18n.T("tui.repos.online.warn."+code, name))
			}
		}
		if v.json.Truncated {
			c.line("  ! " + i18n.T("tui.repos.online.truncated", v.pages, fmt.Sprint(len(v.repos))))
		}
		for i, r := range v.repos {
			where := ""
			if p := v.json.Repos[i].LocalPaths; len(p) > 0 {
				where = "✓ " + c.d.Env.Contract(p[0])
			}
			// 2 (indent) + name 30 + 1 + badges 13 + 1 + age 4 + 1 + local
			c.line("  " + fit(r.FullPath, 30) + " " + fit(badges(r), 13) + " " + fit(age(now, r.Updated), 4) + " " + where)
		}
	}
}

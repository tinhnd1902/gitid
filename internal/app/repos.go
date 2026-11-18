package app

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/gitops"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/remote"
	"github.com/tinhnd1902/gitid/internal/scan"
)

// ScanWorkers is how many repositories are inspected at the same time (one
// git process each).
const ScanWorkers = 8

// RepoStatus is what the remote of a local repository says about the account
// it should use.
type RepoStatus string

// Statuses.
const (
	StatusOK             RepoStatus = "ok"
	StatusWrongRemote    RepoStatus = "wrong_remote"    // the account matches but the remote is not in its form
	StatusUnknownAccount RepoStatus = "unknown_account" // a known host, but no account for this username
	StatusForeignHost    RepoStatus = "foreign_host"    // a host without any account
	StatusNoRemote       RepoStatus = "no_remote"
	StatusUnparsable     RepoStatus = "unparsable"
	StatusAmbiguous      RepoStatus = "ambiguous"
	StatusBroken         RepoStatus = "broken"  // git cannot read the repository
	StatusDubious        RepoStatus = "dubious" // git refuses it (safe.directory)
	StatusBare           RepoStatus = "bare"    // a bare repository: no working tree, no email
)

// Flags raised next to a status.
const (
	FlagEmailMismatch = "email_mismatch"
	FlagLocalOverride = "local_override"
	FlagWrongFolder   = "wrong_folder"
	FlagDuplicate     = "duplicate"
	FlagViaSymlink    = "via_symlink"
)

// Scan states of a company folder.
const (
	ScanOK        = "ok"
	ScanMissing   = "missing"
	ScanNotDir    = "not_dir"
	ScanCancelled = "cancelled"
)

// i18n keys of LocalRepo.Detail.
const (
	DetailBroken  = "scan.detail.broken"
	DetailDubious = "scan.detail.dubious"
)

// RawRepo is the I/O result for one repository: what the filesystem walk and
// one git call found, with no interpretation. It is cached by the TUI, so a
// configuration change can be classified again without touching the disk.
type RawRepo struct {
	Company    string
	Path       string
	Kind       scan.Kind
	ViaSymlink bool
	GitDir     string
	Cfg        gitops.RepoConfig
	Err        error // git could not read the repository (gitops.ErrNotRepo, ErrDubious, ...)
}

// LocalRepo is a RawRepo classified against a configuration.
type LocalRepo struct {
	Company string
	Path    string // absolute
	Kind    scan.Kind
	// Remote is the picked remote (origin, else the first), credentials masked.
	Remote         string
	Key            string // RepoKey of Remote; "" without a usable remote
	Account        string // "company/account" the remote maps to ("" if none)
	Reason         string // alias | username | default
	EffectiveEmail string
	EmailOrigin    string
	ExpectedEmail  string
	FixTo          string // the remote URL `fix` would set; "" when there is nothing to fix
	Status         RepoStatus
	Flags          []string
	BelongsTo      string   // company slug of the account when Flags has wrong_folder
	Duplicates     []string // the other paths of the same remote when Flags has duplicate
	Detail         string   // localized extra text for broken / dubious
	Warnings       []string // the same codes `gitid check` reports
}

// HasFlag reports whether the repo carries the flag.
func (r LocalRepo) HasFlag(f string) bool {
	for _, x := range r.Flags {
		if x == f {
			return true
		}
	}
	return false
}

// Problem reports whether the repository needs attention: a status other than
// ok or bare, or any flag besides via_symlink.
func (r LocalRepo) Problem() bool {
	if r.Status != StatusOK && r.Status != StatusBare {
		return true
	}
	for _, f := range r.Flags {
		if f != FlagViaSymlink {
			return true
		}
	}
	return false
}

// Fixable reports whether `fix remote` has something to do.
func (r LocalRepo) Fixable() bool { return r.FixTo != "" }

// CompanyDone is how the scan of one company folder ended.
type CompanyDone struct {
	State string // ok | missing | not_dir | cancelled
	Stats scan.Stats
}

// CompanyScan is the classified result for one company, in config order.
type CompanyScan struct {
	Slug    string
	BaseDir string // as stored in the configuration ("~/Code/acme/")
	State   string
	Stats   scan.Stats
	Repos   []LocalRepo
}

// Problems counts the repositories that need attention.
func (c CompanyScan) Problems() int {
	n := 0
	for _, r := range c.Repos {
		if r.Problem() {
			n++
		}
	}
	return n
}

// ScanOptions bound a scan. The zero value takes the scan defaults.
type ScanOptions struct {
	MaxDepth int
	MaxDirs  int
}

// LocalEvent is reported while a scan runs. Exactly one of Raw and Done is
// set, or neither (a progress report with Dirs and Found).
type LocalEvent struct {
	Company string
	Dirs    int // folders visited so far in this company
	Found   int // repositories found so far in this company
	Raw     *RawRepo
	Done    *CompanyDone
}

// BaseDirs lists the expanded base directory of every company, in config
// order; it identifies "the same set of folders" for the TUI's cache.
func (s *Service) BaseDirs(cfg config.Config) []string {
	var out []string
	for _, c := range cfg.Companies {
		out = append(out, s.Env.Expand(c.BaseDir))
	}
	return out
}

// InspectRepo reads one repository with a single git call. dir need not be
// the root of a walk: the kind is detected on the spot.
func (s *Service) InspectRepo(ctx context.Context, company, dir string) RawRepo {
	f, ok := scan.Detect(dir)
	if !ok {
		f = scan.Found{Path: dir, Kind: scan.KindRepo}
	}
	return s.inspect(ctx, company, f)
}

func (s *Service) inspect(ctx context.Context, company string, f scan.Found) RawRepo {
	raw := RawRepo{Company: company, Path: f.Path, Kind: f.Kind, ViaSymlink: f.ViaSymlink, GitDir: f.GitDir}
	raw.Cfg, raw.Err = gitops.ReadRepoConfig(ctx, s.Run, f.Path)
	return raw
}

// ScanLocal walks the base directory of every company (config order) and
// inspects the repositories it finds with ScanWorkers workers, one git call
// each. Events go to emit, one at a time (emit need not be thread-safe, but it
// must not block for long). It returns when every company is done or ctx is
// cancelled; the companies not finished then end as ScanCancelled.
func (s *Service) ScanLocal(ctx context.Context, cfg config.Config, o ScanOptions, emit func(LocalEvent)) {
	cfg = config.Normalize(cfg, s.Env)
	var mu sync.Mutex
	send := func(ev LocalEvent) {
		mu.Lock()
		defer mu.Unlock()
		if emit != nil {
			emit(ev)
		}
	}
	own := s.newOwnership(cfg)
	for i, c := range cfg.Companies {
		if ctx.Err() != nil {
			send(LocalEvent{Company: c.Slug, Done: &CompanyDone{State: ScanCancelled}})
			continue
		}
		send(LocalEvent{Company: c.Slug, Done: s.scanCompany(ctx, c, i, own, o, send)})
	}
}

// ownership decides which company a folder belongs to when base directories
// overlap (nested, identical, or one a symlink to another): the company with
// the longest base directory that contains it, the first in config order on a
// tie. Paths are compared in their resolved form; no file system call is made
// per folder, because the walk never follows a symlink below its root.
type ownership struct {
	lex  []string // expanded base dir as given ("" when unset)
	real []string // the same with symlinks resolved
}

func (s *Service) newOwnership(cfg config.Config) ownership {
	o := ownership{lex: make([]string, len(cfg.Companies)), real: make([]string, len(cfg.Companies))}
	for i, c := range cfg.Companies {
		root := s.Env.Expand(c.BaseDir)
		if root == "" {
			continue
		}
		o.lex[i] = root
		o.real[i] = filepath.Clean(root)
		if r, err := filepath.EvalSymlinks(root); err == nil {
			o.real[i] = r
		}
	}
	return o
}

func within(root, p string) bool {
	return p == root || strings.HasPrefix(p, strings.TrimRight(root, string(filepath.Separator))+string(filepath.Separator))
}

// owns reports whether company i is the owner of path, a folder reached by
// walking the base directory of company i.
func (o ownership) owns(i int, path string) bool {
	rel, err := filepath.Rel(o.lex[i], path)
	if err != nil {
		return true
	}
	real := filepath.Join(o.real[i], rel)
	best := -1
	for j, r := range o.real {
		if r == "" || !within(r, real) {
			continue
		}
		if best < 0 || len(r) > len(o.real[best]) {
			best = j
		}
	}
	return best < 0 || best == i
}

func (s *Service) scanCompany(ctx context.Context, c config.Company, idx int, own ownership, o ScanOptions, send func(LocalEvent)) *CompanyDone {
	root := s.Env.Expand(c.BaseDir)
	if root == "" {
		return &CompanyDone{State: ScanMissing}
	}
	jobs := make(chan scan.Found, 2*ScanWorkers)
	var wg sync.WaitGroup
	for i := 0; i < ScanWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				raw := s.inspect(ctx, c.Slug, f)
				if ctx.Err() != nil {
					continue // a half-read repository is not a result
				}
				send(LocalEvent{Company: c.Slug, Raw: &raw})
			}
		}()
	}
	var foundN int
	opts := scan.Options{MaxDepth: o.MaxDepth, MaxDirs: o.MaxDirs,
		Skip: func(p string) bool { return !own.owns(idx, p) },
		Progress: func(st scan.Stats) {
			send(LocalEvent{Company: c.Slug, Dirs: st.Dirs, Found: foundN})
		}}
	st, err := scan.Walk(ctx, root, opts, func(f scan.Found) {
		if !own.owns(idx, f.Path) {
			return // a symlinked repository that belongs to a deeper base dir
		}
		foundN++
		select {
		case jobs <- f:
		case <-ctx.Done():
		}
	})
	close(jobs)
	wg.Wait()
	switch {
	case errors.Is(err, scan.ErrRootMissing):
		return &CompanyDone{State: ScanMissing}
	case errors.Is(err, scan.ErrRootNotDir):
		return &CompanyDone{State: ScanNotDir}
	case err != nil || ctx.Err() != nil:
		return &CompanyDone{State: ScanCancelled, Stats: st}
	}
	return &CompanyDone{State: ScanOK, Stats: st}
}

// RepoKey identifies a repository across URL forms: lower-case
// host/owner/repo without ".git". The remote must be Resolve()d (an ssh alias
// replaced by its real host). All the Azure DevOps forms (https, ssh, legacy
// visualstudio.com) give dev.azure.com/org/project/repo.
func RepoKey(r remote.Remote) string {
	if r.Repo == "" {
		return ""
	}
	if r.Azure {
		return strings.ToLower("dev.azure.com/" + r.AzOrg + "/" + r.AzProject + "/" + r.Repo)
	}
	return strings.ToLower(r.Host + "/" + r.Owner + "/" + r.Repo)
}

// LocalIndex maps a RepoKey to the local clones of that repository.
type LocalIndex map[string][]LocalRepo

// IndexLocal builds the LocalIndex of a classified scan.
func IndexLocal(scans []CompanyScan) LocalIndex {
	idx := LocalIndex{}
	for _, c := range scans {
		for _, r := range c.Repos {
			if r.Key != "" {
				idx[r.Key] = append(idx[r.Key], r)
			}
		}
	}
	return idx
}

// ClassifyLocal interprets raw scan results against cfg. It is pure: it runs
// no git and reads no configuration from disk, so calling it again with a
// changed cfg re-classifies the same data. states says how each company's
// scan ended (a company without an entry counts as ScanOK). Companies are in
// config order, their repositories sorted by path.
func ClassifyLocal(cfg config.Config, e env.Env, raws []RawRepo, states map[string]CompanyDone) []CompanyScan {
	cfg = config.Normalize(cfg, e)
	out := make([]CompanyScan, len(cfg.Companies))
	at := map[string]int{}
	for i, c := range cfg.Companies {
		st, ok := states[c.Slug]
		if !ok {
			st = CompanyDone{State: ScanOK}
		}
		out[i] = CompanyScan{Slug: c.Slug, BaseDir: c.BaseDir, State: st.State, Stats: st.Stats}
		at[c.Slug] = i
	}
	for _, raw := range raws {
		i, ok := at[raw.Company]
		if !ok {
			continue
		}
		out[i].Repos = append(out[i].Repos, classifyOne(cfg, e, raw))
	}
	for i := range out {
		sort.Slice(out[i].Repos, func(a, b int) bool { return out[i].Repos[a].Path < out[i].Repos[b].Path })
	}
	markDuplicates(out)
	return out
}

// classifyOne classifies one repository (flags that need the other
// repositories, duplicate, are set afterwards).
func classifyOne(cfg config.Config, e env.Env, raw RawRepo) LocalRepo {
	lr := LocalRepo{Company: raw.Company, Path: raw.Path, Kind: raw.Kind}
	if raw.ViaSymlink {
		lr.Flags = append(lr.Flags, FlagViaSymlink)
	}
	switch {
	case errors.Is(raw.Err, gitops.ErrDubious):
		lr.Status, lr.Detail = StatusDubious, i18n.T(DetailDubious)
		return lr
	case raw.Err != nil:
		lr.Status, lr.Detail = StatusBroken, i18n.T(DetailBroken)
		return lr
	}
	ev := evaluate(e, cfg, evalInput{Top: raw.Path, Remotes: raw.Cfg.Remotes, Email: raw.Cfg.Email, HasEmail: raw.Cfg.HasEmail})
	res := ev.Result
	lr.Remote, lr.Account, lr.Reason = res.Remote, res.Account, res.Reason
	lr.EffectiveEmail, lr.EmailOrigin, lr.ExpectedEmail = res.EffectiveEmail, res.EmailOrigin, res.ExpectedEmail
	lr.Warnings = res.Warnings
	var resolved remote.Remote
	if ev.HaveRemote {
		resolved, _ = remote.Resolve(cfg, ev.Remote)
		lr.Key = RepoKey(resolved)
	}
	if raw.Kind == scan.KindBare {
		// no working tree: there is no email to check
		lr.Status, lr.Warnings, lr.Account, lr.Reason = StatusBare, nil, "", ""
		lr.EffectiveEmail, lr.EmailOrigin, lr.ExpectedEmail = "", "", ""
		return lr
	}
	for _, w := range res.Warnings {
		switch w {
		case WarnEmailMismatch:
			lr.Flags = append(lr.Flags, FlagEmailMismatch)
		case WarnLocalOverride:
			lr.Flags = append(lr.Flags, FlagLocalOverride)
		}
	}
	switch {
	case ev.NoRemote:
		lr.Status = StatusNoRemote
	case ev.Unparsed:
		lr.Status = StatusUnparsable
	case ev.Ambiguous:
		lr.Status = StatusAmbiguous
	case ev.Expected == nil:
		lr.Status = StatusForeignHost
		for _, ref := range cfg.All() {
			if remote.OnHost(ref.Account, resolved) {
				lr.Status = StatusUnknownAccount
				break
			}
		}
	default:
		lr.Status = StatusOK
		newURL, changed, err := rewriteFor(cfg, *ev.Expected, ev.RawURL, remote.Options{})
		switch {
		case err != nil:
			lr.Status = StatusWrongRemote
		case changed:
			lr.Status, lr.FixTo = StatusWrongRemote, newURL
		}
		if ev.Expected.Company.Slug != raw.Company {
			lr.Flags = append(lr.Flags, FlagWrongFolder)
			lr.BelongsTo = ev.Expected.Company.Slug
		}
	}
	return lr
}

// markDuplicates flags the repositories that share a RepoKey, across all
// companies. Worktrees and bare repositories are left out: a worktree of a
// repository is not a second clone.
func markDuplicates(scans []CompanyScan) {
	type loc struct{ c, r int }
	groups := map[string][]loc{}
	for ci, c := range scans {
		for ri, r := range c.Repos {
			if r.Key == "" || r.Kind == scan.KindWorktree || r.Kind == scan.KindBare {
				continue
			}
			groups[r.Key] = append(groups[r.Key], loc{ci, ri})
		}
	}
	for _, g := range groups {
		if len(g) < 2 {
			continue
		}
		for _, l := range g {
			r := &scans[l.c].Repos[l.r]
			r.Flags = append(r.Flags, FlagDuplicate)
			for _, o := range g {
				if o != l {
					r.Duplicates = append(r.Duplicates, scans[o.c].Repos[o.r].Path)
				}
			}
			sort.Strings(r.Duplicates)
		}
	}
}

// RelPath is path relative to the base directory of its company, for display;
// a path outside the base directory is returned contracted with "~".
func (s *Service) RelPath(base, path string) string {
	b := s.Env.Expand(base)
	if rel, err := filepath.Rel(b, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return rel
	}
	if path == b {
		return "."
	}
	return s.Env.Contract(path)
}

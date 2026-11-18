package app_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/gitops"
	"github.com/tinhnd1902/gitid/internal/remote"
	"github.com/tinhnd1902/gitid/internal/scan"
	"github.com/tinhnd1902/gitid/internal/testutil/fixtures"
)

const (
	gitconfigOrigin = "file:/home/x/.gitconfig"
	localOrigin     = "file:/x/.git/config"
)

func nul(parts ...string) string { return strings.Join(parts, "\x00") + "\x00" }

// scanOut is the stdout of the scan's single git call.
func scanOut(url, email, emailOrigin string) string {
	var p []string
	if url != "" {
		p = append(p, "file:.git/config", "remote.origin.url\n"+url)
	}
	if email != "" {
		p = append(p, emailOrigin, "user.email\n"+email)
	}
	if len(p) == 0 {
		return ""
	}
	return nul(p...)
}

// repoCase is one fixture repository: where it lives and what git reports.
type repoCase struct {
	rel         string // path under ~/Code, e.g. "acme/api"
	kind        string // repo (default) | worktree | bare
	url         string
	email       string
	emailOrigin string
	exit        int
	stderr      string
	block       chan struct{} // the git call waits for this (or for ctx)
}

// build creates the folders on disk and scripts the fake for each case.
func build(t *testing.T, e env.Env, f *execx.Fake, cases []repoCase) map[string]string {
	t.Helper()
	paths := map[string]string{}
	for _, c := range cases {
		dir := filepath.Join(e.Home, "Code", c.rel)
		switch c.kind {
		case "bare":
			for _, d := range []string{"objects", "refs"} {
				if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		case "worktree":
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: /x/main/.git/worktrees/w\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		default:
			if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		origin := c.emailOrigin
		if origin == "" {
			origin = gitconfigOrigin
		}
		f.On("git -C "+dir+" config -z --show-origin --get-regexp", execx.Response{Stdout: scanOut(c.url, c.email, origin), ExitCode: c.exit, Stderr: c.stderr, Block: c.block})
		paths[c.rel] = dir
	}
	return paths
}

func scanAll(t *testing.T, s *app.Service, cfg config.Config) ([]app.RawRepo, map[string]app.CompanyDone) {
	t.Helper()
	var raws []app.RawRepo
	states := map[string]app.CompanyDone{}
	s.ScanLocal(bg, cfg, app.ScanOptions{}, func(ev app.LocalEvent) {
		if ev.Raw != nil {
			raws = append(raws, *ev.Raw)
		}
		if ev.Done != nil {
			states[ev.Company] = *ev.Done
		}
	})
	return raws, states
}

func findRepo(t *testing.T, scans []app.CompanyScan, path string) app.LocalRepo {
	t.Helper()
	for _, c := range scans {
		for _, r := range c.Repos {
			if r.Path == path {
				return r
			}
		}
	}
	t.Fatalf("no repo at %s", path)
	return app.LocalRepo{}
}

func sameStrings(a, b []string) bool {
	x, y := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	return strings.Join(x, ",") == strings.Join(y, ",")
}

// the fixture repositories of A-1 .. A-15
func statusCases() []repoCase {
	return []repoCase{
		{rel: "acme/api", url: "https://alice@github.com/acme/api.git", email: "alice@acme.com"},                     // A-1
		{rel: "acme/web", url: "https://bob2@github.com/acme/web.git", email: "bob@acme.com"},                        // A-2
		{rel: "acme/x", url: "https://github.com/acme/x.git", email: "alice@acme.com"},                               // A-3
		{rel: "acme/y", url: "git@github.com:acme/y.git", email: "alice@acme.com"},                                   // A-4
		{rel: "acme/z", url: "git@github-acme-carol:acme/z.git", email: "carol@acme.com"},                            // A-5
		{rel: "acme/bb", url: "https://alice@bitbucket.org/w/r.git", email: "alice@acme.com"},                        // A-6
		{rel: "acme/nor", email: "alice@acme.com"},                                                                   // A-7
		{rel: "acme/weird", url: "weird::url", email: "alice@acme.com"},                                              // A-8
		{rel: "beta/b9", url: "https://alice@github.com/acme/api9.git", email: "alice@beta.io"},                      // A-9
		{rel: "acme/ov", url: "https://alice@github.com/acme/ov.git", email: "other@x.io", emailOrigin: localOrigin}, // A-13
		{rel: "acme/noem", url: "https://alice@github.com/acme/noem.git"},                                            // A-14
		{rel: "acme/bare.git", kind: "bare", url: "https://alice@github.com/acme/bare.git"},                          // A-15
		{rel: "acme/broken", exit: 128, stderr: "fatal: not a git repository"},                                       // A-16
		{rel: "acme/dub", exit: 128, stderr: "fatal: detected dubious ownership in repository"},                      // A-17
	}
}

func TestA1ToA17Statuses(t *testing.T) {
	f := execx.NewFake()
	s, e := newSvc(t, f)
	cfg := fixtures.Basic()
	p := build(t, e, f, statusCases())
	raws, states := scanAll(t, s, cfg)
	if len(raws) != len(statusCases()) {
		t.Fatalf("%d raw results, want %d", len(raws), len(statusCases()))
	}
	scans := app.ClassifyLocal(cfg, e, raws, states)
	if len(scans) != 3 || scans[0].Slug != "acme" || scans[1].Slug != "globex" || scans[2].Slug != "beta" {
		t.Fatalf("company order: %+v", scans)
	}

	type want struct {
		status  app.RepoStatus
		account string
		reason  string
		flags   []string
		fixTo   string
	}
	cases := map[string]want{
		"acme/api":      {app.StatusOK, "acme/alice", "username", nil, ""},                                                    // A-1
		"acme/web":      {app.StatusUnknownAccount, "", "", nil, ""},                                                          // A-2
		"acme/x":        {app.StatusWrongRemote, "acme/alice", "default", nil, "https://alice@github.com/acme/x.git"},         // A-3
		"acme/y":        {app.StatusWrongRemote, "acme/alice", "default", nil, "https://alice@github.com/acme/y.git"},         // A-4
		"acme/z":        {app.StatusOK, "acme/carol", "alias", nil, ""},                                                       // A-5
		"acme/bb":       {app.StatusForeignHost, "", "", nil, ""},                                                             // A-6
		"acme/nor":      {app.StatusNoRemote, "acme/alice", "default", nil, ""},                                               // A-7
		"acme/weird":    {app.StatusUnparsable, "acme/alice", "default", nil, ""},                                             // A-8
		"beta/b9":       {app.StatusOK, "beta/alice", "username", nil, ""},                                                    // A-9
		"acme/ov":       {app.StatusOK, "acme/alice", "username", []string{app.FlagEmailMismatch, app.FlagLocalOverride}, ""}, // A-13
		"acme/noem":     {app.StatusOK, "acme/alice", "username", nil, ""},                                                    // A-14
		"acme/bare.git": {app.StatusBare, "", "", nil, ""},                                                                    // A-15
		"acme/broken":   {app.StatusBroken, "", "", nil, ""},                                                                  // A-16
		"acme/dub":      {app.StatusDubious, "", "", nil, ""},                                                                 // A-17
	}
	for rel, w := range cases {
		r := findRepo(t, scans, p[rel])
		if r.Status != w.status || r.Account != w.account || r.Reason != w.reason || r.FixTo != w.fixTo || !sameStrings(r.Flags, w.flags) {
			t.Errorf("%s: got status=%s account=%q reason=%q fixTo=%q flags=%v; want %+v", rel, r.Status, r.Account, r.Reason, r.FixTo, r.Flags, w)
		}
		if !filepath.IsAbs(r.Path) { // A-24
			t.Errorf("%s: path %q is not absolute", rel, r.Path)
		}
	}
	// A-3: the no-username warning is carried
	if r := findRepo(t, scans, p["acme/x"]); !contains(r.Warnings, remote.WarnNoUsername) {
		t.Errorf("A-3 warnings %v", r.Warnings)
	}
	// A-14: no email is a warning, the status follows the remote
	if r := findRepo(t, scans, p["acme/noem"]); !contains(r.Warnings, app.WarnNoEmail) || r.Problem() {
		t.Errorf("A-14 %+v", r)
	}
	// A-13: the effective email is the local one, with its origin
	if r := findRepo(t, scans, p["acme/ov"]); r.EffectiveEmail != "other@x.io" || r.EmailOrigin != localOrigin || r.ExpectedEmail != "alice@acme.com" {
		t.Errorf("A-13 %+v", r)
	}
	// A-15: a bare repository has no email checks and is not a problem
	if r := findRepo(t, scans, p["acme/bare.git"]); r.Kind != scan.KindBare || r.Problem() || r.EffectiveEmail != "" {
		t.Errorf("A-15 %+v", r)
	}
	// A-16 / A-17: localized details
	if r := findRepo(t, scans, p["acme/broken"]); r.Detail == "" || strings.HasPrefix(r.Detail, "scan.") {
		t.Errorf("A-16 detail %q", r.Detail)
	}
	if r := findRepo(t, scans, p["acme/dub"]); !strings.Contains(r.Detail, "safe.directory") {
		t.Errorf("A-17 detail %q", r.Detail)
	}
	// A-7 carries the company default, A-2 does not have an account
	if r := findRepo(t, scans, p["acme/nor"]); !contains(r.Warnings, app.WarnNoRemote) || !r.Problem() {
		t.Errorf("A-7 %+v", r)
	}
	for _, c := range scans {
		if c.State != app.ScanOK && c.Slug != "globex" {
			t.Errorf("%s state %s", c.Slug, c.State)
		}
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// A-18: a missing base dir does not stop the others.
func TestA18MissingBaseDir(t *testing.T) {
	f := execx.NewFake()
	s, e := newSvc(t, f)
	cfg := fixtures.Basic()
	build(t, e, f, []repoCase{{rel: "acme/api", url: "https://alice@github.com/acme/api.git", email: "alice@acme.com"}})
	raws, states := scanAll(t, s, cfg)
	scans := app.ClassifyLocal(cfg, e, raws, states)
	if scans[1].Slug != "globex" || scans[1].State != app.ScanMissing || len(scans[1].Repos) != 0 {
		t.Fatalf("globex: %+v", scans[1])
	}
	if scans[0].State != app.ScanOK || len(scans[0].Repos) != 1 || scans[2].State != app.ScanMissing {
		t.Fatalf("%+v", scans)
	}
	// a file in place of the folder
	if err := os.MkdirAll(filepath.Join(e.Home, "Code"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.Home, "Code", "globex"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, states = scanAll(t, s, cfg)
	if states["globex"].State != app.ScanNotDir {
		t.Fatalf("%+v", states["globex"])
	}
}

// A-10: a remote that belongs to another company's account.
func TestA10WrongFolder(t *testing.T) {
	f := execx.NewFake()
	s, e := newSvc(t, f)
	cfg := fixtures.Basic()
	p := build(t, e, f, []repoCase{
		{rel: "beta/api", url: "https://bob@github.com/acme/api.git", email: "bob@acme.com"},
	})
	raws, states := scanAll(t, s, cfg)
	r := findRepo(t, app.ClassifyLocal(cfg, e, raws, states), p["beta/api"])
	if !r.HasFlag(app.FlagWrongFolder) || r.BelongsTo != "acme" || r.Account != "acme/bob" || r.Status != app.StatusOK || !r.Problem() {
		t.Fatalf("%+v", r)
	}
	// the same with alice, once acme/alice is the only account for the acme org
	cfg2 := fixtures.Basic()
	cfg2.Companies[0].Accounts[0].Orgs = []string{"acme"}
	p = build(t, e, f, []repoCase{{rel: "beta/api2", url: "https://alice@github.com/acme/api2.git", email: "alice@acme.com"}})
	raws, states = scanAll(t, s, cfg2)
	r = findRepo(t, app.ClassifyLocal(cfg2, e, raws, states), p["beta/api2"])
	if !r.HasFlag(app.FlagWrongFolder) || r.BelongsTo != "acme" {
		t.Fatalf("%+v", r)
	}
}

// A-11 and A-12: duplicates across companies, a worktree is not one.
func TestA11A12Duplicates(t *testing.T) {
	f := execx.NewFake()
	s, e := newSvc(t, f)
	cfg := fixtures.Basic()
	u := "https://alice@github.com/acme/shared.git"
	p := build(t, e, f, []repoCase{
		{rel: "acme/api", url: u, email: "alice@acme.com"},
		{rel: "beta/api-copy", url: u, email: "alice@beta.io"},
		{rel: "acme/main", url: "https://alice@github.com/acme/wt.git", email: "alice@acme.com"},
		{rel: "acme/wt", kind: "worktree", url: "https://alice@github.com/acme/wt.git", email: "alice@acme.com"},
	})
	raws, states := scanAll(t, s, cfg)
	scans := app.ClassifyLocal(cfg, e, raws, states)
	a, b := findRepo(t, scans, p["acme/api"]), findRepo(t, scans, p["beta/api-copy"])
	if !a.HasFlag(app.FlagDuplicate) || !b.HasFlag(app.FlagDuplicate) {
		t.Fatalf("not flagged: %+v %+v", a, b)
	}
	if len(a.Duplicates) != 1 || a.Duplicates[0] != p["beta/api-copy"] || len(b.Duplicates) != 1 || b.Duplicates[0] != p["acme/api"] {
		t.Fatalf("duplicates: %v %v", a.Duplicates, b.Duplicates)
	}
	m, w := findRepo(t, scans, p["acme/main"]), findRepo(t, scans, p["acme/wt"])
	if m.HasFlag(app.FlagDuplicate) || w.HasFlag(app.FlagDuplicate) || w.Kind != scan.KindWorktree {
		t.Fatalf("worktree flagged: %+v %+v", m, w)
	}
}

// A-19: cancel mid-scan.
func TestA19Cancel(t *testing.T) {
	f := execx.NewFake()
	s, e := newSvc(t, f)
	cfg := fixtures.Basic()
	var cases []repoCase
	block := make(chan struct{}) // never closed: only the context ends the calls
	for i := 0; i < 12; i++ {
		cases = append(cases, repoCase{rel: fmt.Sprintf("acme/r%02d", i), url: "https://alice@github.com/acme/r.git", email: "alice@acme.com", block: block})
	}
	build(t, e, f, cases)
	before := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(bg)
	done := make(chan map[string]app.CompanyDone, 1)
	go func() {
		states := map[string]app.CompanyDone{}
		s.ScanLocal(ctx, cfg, app.ScanOptions{}, func(ev app.LocalEvent) {
			if ev.Done != nil {
				states[ev.Company] = *ev.Done
			}
		})
		done <- states
	}()
	deadline := time.Now().Add(5 * time.Second)
	for f.Count("git -C") == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	var states map[string]app.CompanyDone
	select {
	case states = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ScanLocal did not return after cancel")
	}
	for _, slug := range []string{"acme", "globex", "beta"} {
		if states[slug].State != app.ScanCancelled {
			t.Errorf("%s: %+v", slug, states[slug])
		}
	}
	deadline = time.Now().Add(time.Second)
	for runtime.NumGoroutine() > before+2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before+2 {
		t.Fatalf("goroutines: %d before, %d after", before, n)
	}
}

// A-20: the scan and `check` agree on the same data.
func TestA20ConsistentWithCheck(t *testing.T) {
	cfg := fixtures.Basic()
	type rel struct {
		flag, warn string
	}
	for _, c := range statusCases() {
		if c.kind == "bare" || c.exit != 0 {
			continue
		}
		t.Run(c.rel, func(t *testing.T) {
			f := execx.NewFake()
			s, e := newSvc(t, f)
			p := build(t, e, f, []repoCase{c})
			dir := p[c.rel]
			// the fake answers the calls `check` makes for the same repository
			f.On("git -C "+dir+" rev-parse --show-toplevel", execx.Response{Stdout: dir + "\n"})
			rems := ""
			if c.url != "" {
				rems = "remote.origin.url " + c.url + "\n"
			}
			if rems != "" {
				f.On("git -C "+dir+" config --get-regexp", execx.Response{Stdout: rems})
			} else {
				f.On("git -C "+dir+" config --get-regexp", execx.Response{ExitCode: 1})
			}
			if c.email != "" {
				o := c.emailOrigin
				if o == "" {
					o = gitconfigOrigin
				}
				f.On("git -C "+dir+" config --show-origin --get user.email", execx.Response{Stdout: o + "\t" + c.email + "\n"})
			} else {
				f.On("git -C "+dir+" config --show-origin --get user.email", execx.Response{ExitCode: 1})
			}
			chk, err := s.Check(bg, cfg, dir)
			if err != nil {
				t.Fatal(err)
			}
			raws, states := scanAll(t, s, cfg)
			r := findRepo(t, app.ClassifyLocal(cfg, e, raws, states), dir)
			// check adds pending-apply, which has no local meaning
			var chkW []string
			for _, w := range chk.Warnings {
				if w != app.WarnPendingApply {
					chkW = append(chkW, w)
				}
			}
			if !sameStrings(chkW, r.Warnings) {
				t.Errorf("warnings: check %v, scan %v", chkW, r.Warnings)
			}
			if chk.Account != r.Account || chk.Reason != r.Reason || chk.EffectiveEmail != r.EffectiveEmail || chk.EmailOrigin != r.EmailOrigin || chk.ExpectedEmail != r.ExpectedEmail {
				t.Errorf("check %+v, scan %+v", chk, r)
			}
			// the documented mapping between warnings and status / flags
			has := func(w string) bool { return contains(chk.Warnings, w) }
			if has(app.WarnEmailMismatch) != r.HasFlag(app.FlagEmailMismatch) || has(app.WarnLocalOverride) != r.HasFlag(app.FlagLocalOverride) {
				t.Errorf("flags %v vs warnings %v", r.Flags, chk.Warnings)
			}
			if has(app.WarnNoRemote) != (r.Status == app.StatusNoRemote) || has(app.WarnRemoteUnparsed) != (r.Status == app.StatusUnparsable) {
				t.Errorf("status %s vs warnings %v", r.Status, chk.Warnings)
			}
			if has(app.WarnNoAccount) != (r.Status == app.StatusUnknownAccount || r.Status == app.StatusForeignHost) {
				t.Errorf("status %s vs warnings %v", r.Status, chk.Warnings)
			}
			// fix agrees with the status
			res, _, _, ferr := s.PlanFix(bg, cfg, app.FixRequest{Dir: dir})
			if r.Status == app.StatusWrongRemote && (ferr != nil || !res.Changed || res.New != r.FixTo) {
				t.Errorf("fix %+v %v, scan fixTo %q", res, ferr, r.FixTo)
			}
			if r.Status == app.StatusOK && ferr == nil && res.Changed {
				t.Errorf("status ok but fix would change %+v", res)
			}
		})
	}
}

// A-21: one key for every URL form.
func TestA21RepoKey(t *testing.T) {
	cfg := fixtures.Full()
	key := func(u string) string {
		r, err := remote.Parse(u)
		if err != nil {
			t.Fatalf("%s: %v", u, err)
		}
		r, _ = remote.Resolve(cfg, r)
		return app.RepoKey(r)
	}
	for _, u := range []string{"https://Alice@GitHub.com/Acme/API.git", "git@github-acme-carol:acme/api", "ssh://git@github.com/acme/api", "https://github.com/acme/api"} {
		if got := key(u); got != "github.com/acme/api" {
			t.Errorf("%s: %q", u, got)
		}
	}
	az := []string{
		"https://dev.azure.com/org/proj/_git/repo",
		"https://dev@dev.azure.com/org/proj/_git/repo",
		"git@ssh.dev.azure.com:v3/org/proj/repo",
		"https://org.visualstudio.com/proj/_git/repo",
	}
	for _, u := range az {
		if got := key(u); got != "dev.azure.com/org/proj/repo" {
			t.Errorf("%s: %q", u, got)
		}
	}
	if app.RepoKey(remote.Remote{}) != "" {
		t.Error("empty remote has a key")
	}
}

// A-22: at most 8 repositories are inspected at once.
func TestA22WorkerPool(t *testing.T) {
	f := execx.NewFake()
	s, e := newSvc(t, f)
	cfg := fixtures.Basic()
	var cases []repoCase
	for i := 0; i < 50; i++ {
		cases = append(cases, repoCase{rel: fmt.Sprintf("acme/r%02d", i), url: "https://alice@github.com/acme/r.git", email: "alice@acme.com"})
	}
	build(t, e, f, cases)
	var cur, peak atomic.Int32
	f.On("git -C", execx.Response{Do: func(execx.Cmd) error {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(3 * time.Millisecond)
		cur.Add(-1)
		return nil
	}})
	// the generic rule loses to the per-repo rules (longer prefix), so script
	// the same Do on each one
	for _, c := range cases {
		dir := filepath.Join(e.Home, "Code", c.rel)
		f.On("git -C "+dir+" config -z --show-origin --get-regexp", execx.Response{Stdout: scanOut(c.url, c.email, gitconfigOrigin), Do: func(execx.Cmd) error {
			n := cur.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(3 * time.Millisecond)
			cur.Add(-1)
			return nil
		}})
	}
	raws, _ := scanAll(t, s, cfg)
	if len(raws) != 50 {
		t.Fatalf("%d results", len(raws))
	}
	if p := peak.Load(); p > app.ScanWorkers || p < 2 {
		t.Fatalf("peak concurrency %d (limit %d)", p, app.ScanWorkers)
	}
	// one git call per repository
	if n := f.Count("git "); n != 50 {
		t.Fatalf("%d git calls, want 50", n)
	}
}

// A-23: ClassifyLocal is pure.
func TestA23ClassifyIsPure(t *testing.T) {
	f := execx.NewFake()
	s, e := newSvc(t, f)
	cfg := fixtures.Basic()
	p := build(t, e, f, []repoCase{{rel: "acme/web", url: "https://bob@github.com/acme/web.git", email: "bob@acme.com"}})
	raws, states := scanAll(t, s, cfg)
	calls := len(f.Calls())
	first := findRepo(t, app.ClassifyLocal(cfg, e, raws, states), p["acme/web"])
	if first.Status != app.StatusOK || first.Account != "acme/bob" {
		t.Fatalf("%+v", first)
	}
	cfg2 := fixtures.Basic()
	cfg2.Companies[0].Accounts[1].Username = "bobby"
	second := findRepo(t, app.ClassifyLocal(cfg2, e, raws, states), p["acme/web"])
	if second.Status != app.StatusUnknownAccount || second.Account != "" {
		t.Fatalf("%+v", second)
	}
	if len(f.Calls()) != calls {
		t.Fatal("ClassifyLocal ran a command")
	}
	// the raw data was not modified
	if raws[0].Cfg.Remotes[0].URL != "https://bob@github.com/acme/web.git" {
		t.Fatal("raw modified")
	}
}

func TestInspectRepoAndIndex(t *testing.T) {
	f := execx.NewFake()
	s, e := newSvc(t, f)
	cfg := fixtures.Basic()
	p := build(t, e, f, []repoCase{
		{rel: "acme/api", url: "https://alice@github.com/acme/api.git", email: "alice@acme.com"},
		{rel: "beta/api", url: "https://alice@github.com/acme/api.git", email: "alice@beta.io"},
	})
	raw := s.InspectRepo(bg, "acme", p["acme/api"])
	if raw.Err != nil || raw.Kind != scan.KindRepo || len(raw.Cfg.Remotes) != 1 || raw.Company != "acme" {
		t.Fatalf("%+v", raw)
	}
	// a folder that is gone is broken, not a panic
	missing := s.InspectRepo(bg, "acme", filepath.Join(e.Home, "Code", "acme", "gone"))
	if missing.Err == nil {
		t.Fatal("missing repo has no error")
	}
	raws, states := scanAll(t, s, cfg)
	idx := app.IndexLocal(app.ClassifyLocal(cfg, e, raws, states))
	if got := idx["github.com/acme/api"]; len(got) != 2 {
		t.Fatalf("%+v", idx)
	}
	if bd := s.BaseDirs(cfg); len(bd) != 3 || bd[0] != filepath.Join(e.Home, "Code", "acme") {
		t.Fatalf("%v", bd)
	}
	if got := s.RelPath("~/Code/acme/", p["acme/api"]); got != "api" {
		t.Fatalf("rel %q", got)
	}
	if got := s.RelPath("~/Code/acme/", p["beta/api"]); got != "~/Code/beta/api" {
		t.Fatalf("outside %q", got)
	}
}

// the dir cap reaches the walk (used by the TUI test hook)
func TestScanOptionsCap(t *testing.T) {
	f := execx.NewFake()
	s, e := newSvc(t, f)
	cfg := fixtures.Basic()
	var cases []repoCase
	for i := 0; i < 10; i++ {
		cases = append(cases, repoCase{rel: fmt.Sprintf("acme/r%02d", i), url: "https://alice@github.com/acme/r.git", email: "alice@acme.com"})
	}
	build(t, e, f, cases)
	var mu sync.Mutex
	states := map[string]app.CompanyDone{}
	s.ScanLocal(bg, cfg, app.ScanOptions{MaxDirs: 5}, func(ev app.LocalEvent) {
		mu.Lock()
		defer mu.Unlock()
		if ev.Done != nil {
			states[ev.Company] = *ev.Done
		}
	})
	if !states["acme"].Stats.Truncated || states["acme"].Stats.Dirs > 5 {
		t.Fatalf("%+v", states["acme"])
	}
}

// overlapCfg is acme and beta with the given base dirs (relative to ~/Code).
func overlapCfg(bases ...string) config.Config {
	cfg := config.Default()
	for i, b := range bases {
		c := fixtures.Acme()
		c.Slug = fmt.Sprintf("co%d", i)
		c.BaseDir = "~/Code/" + b + "/"
		cfg.Companies = append(cfg.Companies, c)
	}
	return cfg
}

// scanPaths returns, per company, the repo paths that were reported, and
// fails when a path is reported twice anywhere.
func scanPaths(t *testing.T, s *app.Service, cfg config.Config) map[string][]string {
	t.Helper()
	raws, _ := scanAll(t, s, cfg)
	seen := map[string]bool{}
	out := map[string][]string{}
	for _, r := range raws {
		if seen[r.Path] {
			t.Fatalf("%s listed twice", r.Path)
		}
		seen[r.Path] = true
		out[r.Company] = append(out[r.Company], r.Path)
	}
	return out
}

func TestScanNestedBaseDirs(t *testing.T) {
	f := execx.NewFake()
	s, e := newSvc(t, f)
	u := "https://alice@github.com/acme/a.git"
	p := build(t, e, f, []repoCase{
		{rel: "acme/top", url: u, email: "alice@acme.com"},
		{rel: "acme/sub/deep", url: "https://alice@github.com/acme/b.git", email: "alice@acme.com"},
	})
	for _, order := range [][]string{{"acme", "acme/sub"}, {"acme/sub", "acme"}} {
		cfg := overlapCfg(order...)
		got := scanPaths(t, s, cfg)
		outer, inner := "co0", "co1"
		if order[0] == "acme/sub" {
			outer, inner = "co1", "co0"
		}
		if !sameStrings(got[outer], []string{p["acme/top"]}) || !sameStrings(got[inner], []string{p["acme/sub/deep"]}) {
			t.Fatalf("%v: %v", order, got)
		}
		raws, states := scanAll(t, s, cfg)
		scans := app.ClassifyLocal(cfg, e, raws, states)
		for _, c := range scans {
			for _, r := range c.Repos {
				if r.HasFlag(app.FlagDuplicate) {
					t.Fatalf("flagged duplicate: %+v", r)
				}
			}
		}
		// the nested folder is not walked twice: 2 + 2 folders (6 if it were)
		if d := states[outer].Stats.Dirs + states[inner].Stats.Dirs; d != 4 {
			t.Fatalf("dirs %d: %+v", d, states)
		}
	}
}

func TestScanIdenticalBaseDirs(t *testing.T) {
	f := execx.NewFake()
	s, e := newSvc(t, f)
	p := build(t, e, f, []repoCase{{rel: "acme/api", url: "https://alice@github.com/acme/a.git", email: "alice@acme.com"}})
	cfg := overlapCfg("acme", "acme")
	got := scanPaths(t, s, cfg)
	if !sameStrings(got["co0"], []string{p["acme/api"]}) || len(got["co1"]) != 0 {
		t.Fatalf("%v", got)
	}
	raws, states := scanAll(t, s, cfg)
	if states["co1"].State != app.ScanOK {
		t.Fatalf("%+v", states["co1"])
	}
	for _, c := range app.ClassifyLocal(cfg, e, raws, states) {
		for _, r := range c.Repos {
			if r.HasFlag(app.FlagDuplicate) {
				t.Fatalf("flagged duplicate: %+v", r)
			}
		}
	}
}

func TestScanSymlinkedBaseDir(t *testing.T) {
	f := execx.NewFake()
	s, e := newSvc(t, f)
	p := build(t, e, f, []repoCase{{rel: "acme/api", url: "https://alice@github.com/acme/a.git", email: "alice@acme.com"}})
	link := filepath.Join(e.Home, "Code", "link")
	if err := os.Symlink(filepath.Join(e.Home, "Code", "acme"), link); err != nil {
		t.Skip("symlinks unavailable: ", err)
	}
	// the link comes second: the real folder owns the repository
	got := scanPaths(t, s, overlapCfg("acme", "link"))
	if !sameStrings(got["co0"], []string{p["acme/api"]}) || len(got["co1"]) != 0 {
		t.Fatalf("link second: %v", got)
	}
	// the link comes first: it wins the tie and the repository is listed once
	linked := filepath.Join(link, "api")
	f.On("git -C "+linked+" config -z --show-origin --get-regexp", execx.Response{Stdout: scanOut("https://alice@github.com/acme/a.git", "alice@acme.com", gitconfigOrigin)})
	got = scanPaths(t, s, overlapCfg("link", "acme"))
	if len(got["co0"])+len(got["co1"]) != 1 {
		t.Fatalf("link first: %v", got)
	}
}

var _ = errors.Is
var _ = gitops.ErrNotRepo

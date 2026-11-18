// Package doctor runs diagnostic checks on the gitid setup. Every check is
// read-only; the optional ssh connectivity test only runs when asked.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/tinhnd1902/gitid/internal/block"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/fsx"
	"github.com/tinhnd1902/gitid/internal/gitcfg"
	"github.com/tinhnd1902/gitid/internal/gitops"
	"github.com/tinhnd1902/gitid/internal/keymap"
	"github.com/tinhnd1902/gitid/internal/keys"
	"github.com/tinhnd1902/gitid/internal/plan"
	"github.com/tinhnd1902/gitid/internal/sshcfg"
)

// Status of a check result.
type Status string

// Statuses.
const (
	OK   Status = "ok"
	Warn Status = "warn"
	Fail Status = "fail"
	Skip Status = "skip"
	Info Status = "info"
)

// Message and hint keys (also i18n keys).
const (
	MsgGitOK            = "doctor.git.ok"
	MsgGitOld           = "doctor.git.old"
	MsgGitUnknown       = "doctor.git.unknown"
	MsgConfigOK         = "doctor.config.ok"
	MsgConfigInvalid    = "doctor.config.invalid"
	MsgKBOK             = "doctor.keybindings.ok"
	MsgKBCustom         = "doctor.keybindings.custom"
	MsgKBWarn           = "doctor.keybindings.warn"
	MsgCredSkip         = "doctor.cred.skip"
	MsgCredGCM          = "doctor.cred.gcm"
	MsgCredKeychain     = "doctor.cred.keychain_only"
	MsgCredNone         = "doctor.cred.none"
	MsgSSHIncludeSkip   = "doctor.ssh_include.skip"
	MsgSSHIncludeOK     = "doctor.ssh_include.ok"
	MsgSSHIncludeBad    = "doctor.ssh_include.missing"
	MsgSSHIncludeScoped = "doctor.ssh_include.scoped"
	MsgSSHConfSkip      = "doctor.ssh_conf.skip"
	MsgSSHConfOK        = "doctor.ssh_conf.ok"
	MsgSSHConfStale     = "doctor.ssh_conf.stale"
	MsgKeyOK            = "doctor.key.ok"
	MsgKeyMissing       = "doctor.key.missing"
	MsgKeyMode          = "doctor.key.mode"
	MsgKeyNoPub         = "doctor.key.nopub"
	MsgPlanOK           = "doctor.plan.ok"
	MsgPlanPending      = "doctor.plan.pending"
	MsgBaseDirOK        = "doctor.basedir.ok"
	MsgBaseDirMissing   = "doctor.basedir.missing"
	MsgAliasOK          = "doctor.alias.ok"
	MsgAliasClash       = "doctor.alias.collision"
	MsgAmbiguousOK      = "doctor.ambiguous.ok"
	MsgAmbiguousFound   = "doctor.ambiguous.found"
	MsgBlockLastOK      = "doctor.block_last.ok"
	MsgBlockLastInfo    = "doctor.block_last.info"
	MsgSSHTestOK        = "doctor.sshtest.ok"
	MsgSSHTestFail      = "doctor.sshtest.fail"

	HintApply      = "doctor.hint.apply"
	HintInstallGCM = "doctor.hint.install_gcm"
	HintSetOrgs    = "doctor.hint.set_orgs"
	HintKeyGen     = "doctor.hint.keygen"
	HintChmod      = "doctor.hint.chmod"
	HintMkdir      = "doctor.hint.mkdir"
	HintUpgradeGit = "doctor.hint.upgrade_git"
	HintFixConfig  = "doctor.hint.fix_config"
	HintRename     = "doctor.hint.rename_alias"
)

// AllKeys lists every message and hint key (used by the i18n test).
func AllKeys() []string {
	return []string{
		MsgGitOK, MsgGitOld, MsgGitUnknown, MsgConfigOK, MsgConfigInvalid, MsgKBOK, MsgKBCustom, MsgKBWarn, MsgCredSkip, MsgCredGCM, MsgCredKeychain, MsgCredNone,
		MsgSSHIncludeSkip, MsgSSHIncludeOK, MsgSSHIncludeBad, MsgSSHIncludeScoped, MsgSSHConfSkip, MsgSSHConfOK, MsgSSHConfStale,
		MsgKeyOK, MsgKeyMissing, MsgKeyMode, MsgKeyNoPub, MsgPlanOK, MsgPlanPending, MsgBaseDirOK, MsgBaseDirMissing, MsgAliasOK, MsgAliasClash,
		MsgAmbiguousOK, MsgAmbiguousFound, MsgBlockLastOK, MsgBlockLastInfo, MsgSSHTestOK, MsgSSHTestFail,
		HintApply, HintInstallGCM, HintSetOrgs, HintKeyGen, HintChmod, HintMkdir, HintUpgradeGit, HintFixConfig, HintRename,
	}
}

// Result is the outcome of one check.
type Result struct {
	ID      string   `json:"id"`
	Status  Status   `json:"status"`
	Msg     string   `json:"message_key"`
	Args    []string `json:"args,omitempty"`
	Detail  string   `json:"detail,omitempty"`
	HintKey string   `json:"hint_key,omitempty"`
}

// Deps are the inputs of the checks.
type Deps struct {
	E   env.Env
	R   execx.Runner
	Cfg config.Config
}

// Check is one named diagnostic. It may return several results.
type Check struct {
	ID string
	Fn func(ctx context.Context, d Deps) []Result
}

// Options select the optional checks.
type Options struct {
	// SSHTest runs `ssh -T` against every ssh alias (network access).
	SSHTest bool
}

// Checks returns the checks in display order.
func Checks(opts Options) []Check {
	cs := []Check{
		{"git_version", checkGit},
		{"config", checkConfig},
		{"keybindings", checkKeyBindings},
		{"credential_helper", checkCred},
		{"ssh_include", checkSSHInclude},
		{"ssh_conf", checkSSHConf},
		{"keys", checkKeys},
		{"plan", checkPlan},
		{"base_dirs", checkBaseDirs},
		{"alias", checkAlias},
		{"ambiguous", checkAmbiguous},
		{"block_last", checkBlockLast},
	}
	if opts.SSHTest {
		cs = append(cs, Check{"ssh_test", checkSSHTest})
	}
	return cs
}

// RunChecks runs checks concurrently. onResult (may be nil) is called, from
// the checking goroutine, as each result becomes available; the returned
// slice is in check order. Cancelling ctx makes the checks return.
func RunChecks(ctx context.Context, d Deps, checks []Check, onResult func(Result)) []Result {
	out := make([][]Result, len(checks))
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i, c := range checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rs := c.Fn(ctx, d)
			out[i] = rs
			if onResult != nil {
				mu.Lock()
				for _, r := range rs {
					onResult(r)
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	var all []Result
	for _, rs := range out {
		all = append(all, rs...)
	}
	return all
}

// Run runs the standard checks.
func Run(ctx context.Context, d Deps, opts Options) []Result {
	return RunChecks(ctx, d, Checks(opts), nil)
}

// Worst returns the most severe status among results (Fail > Warn > Info/OK).
func Worst(rs []Result) Status {
	w := OK
	for _, r := range rs {
		switch r.Status {
		case Fail:
			return Fail
		case Warn:
			w = Warn
		}
	}
	return w
}

func res(id string, st Status, msg string, args ...string) Result {
	return Result{ID: id, Status: st, Msg: msg, Args: args}
}

func (r Result) hint(k string) Result { r.HintKey = k; return r }
func (r Result) detail(s string) Result {
	r.Detail = s
	return r
}

func checkGit(ctx context.Context, d Deps) []Result {
	v, err := gitops.Version(ctx, d.R)
	if err != nil {
		return []Result{res("git_version", Fail, MsgGitUnknown).detail(err.Error()).hint(HintUpgradeGit)}
	}
	if !v.AtLeast(gitcfg.MinGitVersion.Major, gitcfg.MinGitVersion.Minor) {
		return []Result{res("git_version", Fail, MsgGitOld, v.String(), gitcfg.MinGitVersion.String()).hint(HintUpgradeGit)}
	}
	return []Result{res("git_version", OK, MsgGitOK, v.String())}
}

func checkConfig(ctx context.Context, d Deps) []Result {
	hosts, _ := plan.UserHostPatterns(d.E)
	errs := config.Validate(d.Cfg, d.E, config.ValidateOptions{ExistingHosts: hosts})
	if len(errs) == 0 {
		return []Result{res("config", OK, MsgConfigOK)}
	}
	var parts []string
	for _, e := range errs {
		parts = append(parts, e.Error())
	}
	return []Result{res("config", Fail, MsgConfigInvalid, fmt.Sprint(len(errs))).detail(strings.Join(parts, "; ")).hint(HintFixConfig)}
}

// checkKeyBindings reports the problems of the optional keys.yaml. It only
// reads the file, and never fails: a bad entry just keeps its default key.
func checkKeyBindings(ctx context.Context, d Deps) []Result {
	m, issues := keymap.Load(d.E)
	if len(issues) == 0 {
		if n := m.CustomCount(); n > 0 {
			return []Result{res("keybindings", OK, MsgKBCustom, fmt.Sprint(n))}
		}
		return []Result{res("keybindings", OK, MsgKBOK)}
	}
	out := make([]Result, 0, len(issues))
	for _, is := range issues {
		out = append(out, res("keybindings", Warn, MsgKBWarn).detail(is.Message()))
	}
	return out
}

func hasHTTPS(cfg config.Config) bool {
	for _, r := range cfg.All() {
		if r.Account.Auth == config.AuthHTTPS {
			return true
		}
	}
	return false
}

func hasSSH(cfg config.Config) bool {
	for _, r := range cfg.All() {
		if r.Account.Auth == config.AuthSSH {
			return true
		}
	}
	return false
}

func checkCred(ctx context.Context, d Deps) []Result {
	if !hasHTTPS(d.Cfg) {
		return []Result{res("credential_helper", Skip, MsgCredSkip)}
	}
	if _, err := d.R.Run(ctx, execx.Cmd{Name: "git", Args: []string{"credential-manager", "--version"}}); err == nil {
		return []Result{res("credential_helper", OK, MsgCredGCM)}
	}
	helpers, _ := gitops.ConfigGetAll(ctx, d.R, "credential.helper")
	for _, h := range helpers {
		if strings.Contains(h, "osxkeychain") {
			return []Result{res("credential_helper", Warn, MsgCredKeychain).hint(HintInstallGCM)}
		}
	}
	return []Result{res("credential_helper", Warn, MsgCredNone).hint(HintInstallGCM)}
}

func readText(path string) (string, bool, error) {
	s, err := fsx.ReadSnapshot(path)
	return string(s.Data), s.Exists, err
}

func checkSSHInclude(ctx context.Context, d Deps) []Result {
	if !hasSSH(d.Cfg) {
		return []Result{res("ssh_include", Skip, MsgSSHIncludeSkip)}
	}
	text, _, err := readText(d.E.Paths().SSHConfig)
	if err != nil {
		return []Result{res("ssh_include", Fail, MsgSSHIncludeBad).detail(err.Error()).hint(HintApply)}
	}
	r, err := sshcfg.EnsureInclude(text)
	if err != nil {
		return []Result{res("ssh_include", Fail, MsgSSHIncludeBad).detail(err.Error()).hint(HintFixConfig)}
	}
	if r.Text != text {
		if len(r.Warnings) > 0 {
			return []Result{res("ssh_include", Fail, MsgSSHIncludeScoped).hint(HintApply)}
		}
		return []Result{res("ssh_include", Fail, MsgSSHIncludeBad).hint(HintApply)}
	}
	return []Result{res("ssh_include", OK, MsgSSHIncludeOK)}
}

func checkSSHConf(ctx context.Context, d Deps) []Result {
	if !hasSSH(d.Cfg) {
		return []Result{res("ssh_conf", Skip, MsgSSHConfSkip)}
	}
	want, err := sshcfg.RenderGitidConf(d.Cfg)
	if err != nil {
		return []Result{res("ssh_conf", Fail, MsgSSHConfStale).detail(err.Error()).hint(HintFixConfig)}
	}
	got, _, err := readText(d.E.Paths().SSHGitidConf)
	if err != nil || got != want {
		return []Result{res("ssh_conf", Fail, MsgSSHConfStale).hint(HintApply)}
	}
	return []Result{res("ssh_conf", OK, MsgSSHConfOK)}
}

func checkKeys(ctx context.Context, d Deps) []Result {
	var out []Result
	for _, ref := range d.Cfg.All() {
		a := ref.Account
		if a.Auth != config.AuthSSH {
			continue
		}
		id := "key:" + ref.String()
		in := keys.Inspect(d.E, ref.Company, a)
		switch {
		case !in.Exists:
			out = append(out, res(id, Fail, MsgKeyMissing, ref.String(), d.E.Contract(in.Path)).hint(HintKeyGen))
		case in.Mode&0o077 != 0:
			out = append(out, res(id, Warn, MsgKeyMode, ref.String(), fmt.Sprintf("%04o", in.Mode)).hint(HintChmod))
		case !in.PubExists:
			out = append(out, res(id, Warn, MsgKeyNoPub, ref.String()))
		default:
			out = append(out, res(id, OK, MsgKeyOK, ref.String()))
		}
	}
	return out
}

func checkPlan(ctx context.Context, d Deps) []Result {
	p, err := plan.Build(d.E, d.Cfg)
	if err != nil {
		return []Result{res("plan", Fail, MsgPlanPending, "?").detail(err.Error()).hint(HintFixConfig)}
	}
	if p.Empty() {
		return []Result{res("plan", OK, MsgPlanOK)}
	}
	return []Result{res("plan", Warn, MsgPlanPending, fmt.Sprint(len(p.Changes))).hint(HintApply)}
}

func checkBaseDirs(ctx context.Context, d Deps) []Result {
	var out []Result
	for _, c := range d.Cfg.Companies {
		id := "basedir:" + c.Slug
		if fi, err := os.Stat(d.E.Expand(c.BaseDir)); err != nil || !fi.IsDir() {
			out = append(out, res(id, Warn, MsgBaseDirMissing, c.Slug, c.BaseDir).hint(HintMkdir))
		} else {
			out = append(out, res(id, OK, MsgBaseDirOK, c.Slug))
		}
	}
	return out
}

func checkAlias(ctx context.Context, d Deps) []Result {
	hosts, err := plan.UserHostPatterns(d.E)
	if err != nil {
		return []Result{res("alias", Warn, MsgAliasOK).detail(err.Error())}
	}
	set := map[string]bool{}
	for _, h := range hosts {
		set[h] = true
	}
	var out []Result
	for _, al := range sshcfg.Aliases(d.Cfg) {
		if set[al] {
			out = append(out, res("alias:"+al, Fail, MsgAliasClash, al).hint(HintRename))
		}
	}
	if len(out) == 0 {
		return []Result{res("alias", OK, MsgAliasOK)}
	}
	return out
}

func checkAmbiguous(ctx context.Context, d Deps) []Result {
	_, amb := gitcfg.GlobalPatterns(d.Cfg)
	if len(amb) == 0 {
		return []Result{res("ambiguous", OK, MsgAmbiguousOK)}
	}
	var out []Result
	for _, a := range amb {
		out = append(out, res("ambiguous:"+a.Pattern, Warn, MsgAmbiguousFound, a.Pattern, strings.Join(a.Accounts, ", ")).hint(HintSetOrgs))
	}
	return out
}

func checkBlockLast(ctx context.Context, d Deps) []Result {
	text, exists, err := readText(d.E.Paths().GitConfig)
	if err != nil || !exists {
		return []Result{res("block_last", OK, MsgBlockLastOK)}
	}
	loc, found, err := block.Find(text, gitcfg.Markers)
	if err != nil || !found {
		return []Result{res("block_last", OK, MsgBlockLastOK)}
	}
	if strings.TrimSpace(text[loc.End:]) != "" {
		return []Result{res("block_last", Info, MsgBlockLastInfo)}
	}
	return []Result{res("block_last", OK, MsgBlockLastOK)}
}

// SSHTestArgs are the arguments of the connectivity test for an alias.
func SSHTestArgs(alias string) []string {
	return []string{"-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=8", "git@" + alias}
}

func checkSSHTest(ctx context.Context, d Deps) []Result {
	var out []Result
	for _, al := range sshcfg.Aliases(d.Cfg) {
		out = append(out, SSHTest(ctx, d, al))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// SSHTest runs `ssh -T` for one alias. GitHub answers with exit code 1 and a
// "successfully authenticated" banner, which counts as success.
func SSHTest(ctx context.Context, d Deps, alias string) Result {
	id := "ssh_test:" + alias
	cctx, cancel := context.WithTimeout(ctx, gitops.Timeout)
	defer cancel()
	r, err := d.R.Run(cctx, execx.Cmd{Name: "ssh", Args: SSHTestArgs(alias)})
	out := r.Stdout + r.Stderr
	if err == nil || strings.Contains(out, "successfully authenticated") || strings.Contains(out, "Welcome to GitLab") {
		return res(id, OK, MsgSSHTestOK, alias)
	}
	detail := strings.TrimSpace(out)
	if detail == "" {
		detail = err.Error()
	}
	if errors.Is(err, fs.ErrNotExist) {
		detail = "ssh not found"
	}
	return res(id, Fail, MsgSSHTestFail, alias).detail(detail)
}

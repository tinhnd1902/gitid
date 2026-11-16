package app

import (
	"context"
	"os/exec"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/doctor"
	"github.com/tinhnd1902/gitid/internal/fsx"
	"github.com/tinhnd1902/gitid/internal/gitops"
	"github.com/tinhnd1902/gitid/internal/keys"
	"github.com/tinhnd1902/gitid/internal/sshcfg"
)

// This file holds the small adapters the TUI needs so that it never touches
// execx, fsx or the lower-level packages directly.

// ErrLocked is returned when another gitid process holds the apply lock.
var ErrLocked = fsx.ErrLocked

// ValidateField validates one form value; it returns "" or an i18n error key.
func (s *Service) ValidateField(field, value string) string {
	return config.ValidateField(s.Env, field, value)
}

// DefaultBaseDir is the stored form of the default base directory of slug.
func (s *Service) DefaultBaseDir(cfg config.Config, slug string) string {
	return config.DefaultBaseDir(s.Env, cfg.Settings.CodeRoot, slug)
}

// DoctorChecks lists the diagnostics in display order so that a UI can run
// and show them one by one.
func (s *Service) DoctorChecks(opts doctor.Options) []doctor.Check { return doctor.Checks(opts) }

// RunDoctorCheck runs one diagnostic.
func (s *Service) RunDoctorCheck(ctx context.Context, cfg config.Config, c doctor.Check) []doctor.Result {
	return c.Fn(ctx, doctor.Deps{E: s.Env, R: s.Run, Cfg: config.Normalize(cfg, s.Env)})
}

// SSHAliases lists the ssh aliases of cfg (sorted).
func (s *Service) SSHAliases(cfg config.Config) []string {
	return sshcfg.Aliases(config.Normalize(cfg, s.Env))
}

// SSHTest runs the opt-in `ssh -T` connectivity test for one alias.
func (s *Service) SSHTest(ctx context.Context, cfg config.Config, alias string) doctor.Result {
	return doctor.SSHTest(ctx, doctor.Deps{E: s.Env, R: s.Run, Cfg: config.Normalize(cfg, s.Env)}, alias)
}

// KeyInfo reports (with Lstat only) the key files of an account.
func (s *Service) KeyInfo(ref config.AccountRef) keys.Info {
	return keys.Inspect(s.Env, ref.Company, ref.Account)
}

// ExistingKey returns the public key of an account that already has a key
// pair on disk. Only the .pub file is read.
func (s *Service) ExistingKey(ref config.AccountRef) (keys.Pub, error) {
	return keys.Finish(keys.PrivatePath(s.Env, ref.Company, ref.Account))
}

// KeygenExec returns the interactive ssh-keygen process (to run through
// tea.ExecProcess) and the private key path. The pre-checks run first.
func (s *Service) KeygenExec(ref config.AccountRef, noPass bool) (*exec.Cmd, string, error) {
	cmd, path, err := s.PrepareKey(ref, noPass)
	if err != nil {
		return nil, path, err
	}
	return s.Run.Interactive(cmd), path, nil
}

// CloneExec returns the interactive git clone process for the terminal-mode
// fallback (to run through tea.ExecProcess).
func (s *Service) CloneExec(p ClonePlan) (*exec.Cmd, error) {
	cmd, err := s.CloneCmd(p)
	if err != nil {
		return nil, err
	}
	return s.Run.Interactive(cmd), nil
}

// CloneProgress is one progress report of a running clone.
type CloneProgress = gitops.Progress

// ErrAuth is returned (wrapped) by a clone that failed authentication; the
// caller may then offer the terminal mode.
var ErrAuth = gitops.ErrAuth

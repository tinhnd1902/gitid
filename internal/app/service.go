// Package app is the orchestration layer used by both the CLI and the TUI.
// It is the only package that combines config, plan, git, ssh and keys.
package app

import (
	"context"
	"errors"
	"strings"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/doctor"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/importer"
	"github.com/tinhnd1902/gitid/internal/plan"
)

// Service is the facade over every gitid operation.
type Service struct {
	Env env.Env
	Run execx.Runner
	// Version is gitid's version, sent in the User-Agent of the Online
	// listing ("gitid/<version>").
	Version string
	// OnlineMaxPages caps the pages fetched per account (0: the default).
	// Set it before the first listing.
	OnlineMaxPages int
	// Executable returns the path of the running binary (os.Executable). The
	// update notice uses it to name the command that upgrades this install;
	// nil means the install method is unknown.
	Executable func() (string, error)

	// onl is the state of the Online tab: the token cache and the HTTP client
	// (see online.go). It is created on first use.
	onl onlineState
}

// New creates a Service.
func New(e env.Env, r execx.Runner) *Service { return &Service{Env: e, Run: r} }

// ValidationError carries every field error of an invalid config.
type ValidationError struct{ Errors []config.FieldError }

func (e *ValidationError) Error() string {
	var parts []string
	for _, fe := range e.Errors {
		parts = append(parts, fe.Error())
	}
	return "invalid configuration: " + strings.Join(parts, "; ")
}

// Load reads and normalizes config.yaml (defaults when missing).
func (s *Service) Load() (config.Config, error) {
	cfg, err := config.Load(s.Env)
	if err != nil {
		return config.Config{}, err
	}
	return config.Normalize(cfg, s.Env), nil
}

// Validate validates cfg against the user's existing ssh Host names.
func (s *Service) Validate(cfg config.Config) []config.FieldError {
	hosts, _ := plan.UserHostPatterns(s.Env)
	return config.Validate(config.Normalize(cfg, s.Env), s.Env, config.ValidateOptions{ExistingHosts: hosts})
}

// Commit normalizes, validates and saves cfg. It returns the normalized
// config and the backup directory of the previous config.yaml ("" if none).
func (s *Service) Commit(cfg config.Config) (config.Config, string, error) {
	cfg = config.Normalize(cfg, s.Env)
	if errs := s.Validate(cfg); len(errs) > 0 {
		return cfg, "", &ValidationError{Errors: errs}
	}
	dir, err := config.Save(s.Env, cfg)
	return cfg, dir, err
}

// BuildPlan computes pending file changes for cfg (read-only).
func (s *Service) BuildPlan(cfg config.Config) (plan.Plan, error) {
	return plan.Build(s.Env, config.Normalize(cfg, s.Env))
}

// Apply executes a plan.
func (s *Service) Apply(ctx context.Context, p plan.Plan) (plan.Result, error) {
	return plan.Apply(ctx, s.Env, p)
}

// Doctor runs the diagnostics.
func (s *Service) Doctor(ctx context.Context, cfg config.Config, opts doctor.Options) []doctor.Result {
	return doctor.Run(ctx, doctor.Deps{E: s.Env, R: s.Run, Cfg: config.Normalize(cfg, s.Env)}, opts)
}

// Import lists proposals from the user's existing configuration (read-only).
func (s *Service) Import(cfg config.Config) (importer.Result, error) {
	return importer.Import(s.Env, cfg)
}

// Errors.
var (
	ErrNotSSH      = errors.New("app: the account does not use ssh")
	ErrNoAccount   = errors.New("app: no account matches this repository")
	ErrAmbiguous   = errors.New("app: several accounts match, pass --account")
	ErrNoRemote    = errors.New("app: remote not found")
	ErrImmutableID = errors.New("app: slug and id cannot be changed")
)

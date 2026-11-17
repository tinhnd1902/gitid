package tui

import (
	"errors"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/gitops"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/keys"
	"github.com/tinhnd1902/gitid/internal/plan"
	"github.com/tinhnd1902/gitid/internal/remote"
)

// errText turns an error into a translated, credential-free message.
func errText(err error) string {
	if err == nil {
		return ""
	}
	var ve *app.ValidationError
	if errors.As(err, &ve) {
		msg := i18n.T("err.invalid_config")
		for _, fe := range ve.Errors {
			msg += "\n  - " + fe.Field + ": " + i18n.T(fe.Code)
		}
		return msg
	}
	var se *plan.StaleError
	if errors.As(err, &se) {
		return i18n.T("err.stale", se.Path)
	}
	var pe *plan.PartialError
	if errors.As(err, &pe) {
		return i18n.T("err.partial", len(pe.Written), remote.Redact(pe.Err.Error()), pe.BackupDir)
	}
	table := []struct {
		target error
		key    string
	}{
		{remote.ErrHostMismatch, "err.remote.host_mismatch"},
		{remote.ErrInvalid, "err.remote.invalid"},
		{config.ErrRefAmbiguous, "err.ref.ambiguous"},
		{config.ErrRefNotFound, "err.ref.not_found"},
		{config.ErrUnsupportedVersion, "err.config.version"},
		{gitops.ErrNotRepo, "err.not_repo"},
		{app.ErrNoAccount, "err.no_account"},
		{app.ErrAmbiguous, "err.ambiguous_accounts"},
		{app.ErrNoRemote, "err.no_remote"},
		{app.ErrNotSSH, "err.not_ssh"},
		{keys.ErrKeyExists, "err.key_exists"},
		{keys.ErrNoKeygen, "err.no_keygen"},
		{keys.ErrRefuseDelete, "err.refuse_delete"},
		{gitops.ErrTargetExists, "err.target_exists"},
		{gitops.ErrBadTarget, "err.bad_target"},
		{gitops.ErrBadURL, "err.bad_url"},
		{gitops.ErrAuth, "err.auth"},
		{gitops.ErrNotFound, "err.repo_not_found"},
		{plan.ErrGitConfigLocked, "err.gitconfig_locked"},
		{app.ErrLocked, "err.locked"},
		{app.ErrConfigChanged, "tui.err.config_changed"},
	}
	for _, t := range table {
		if errors.Is(err, t.target) {
			return i18n.T(t.key)
		}
	}
	return remote.Redact(err.Error())
}

package app

import (
	"context"

	"github.com/tinhnd1902/gitid/internal/update"
)

// UpdateNotice says a newer gitid release exists (see package update).
type UpdateNotice = update.Notice

// How this gitid was installed, which decides the upgrade command shown.
const (
	UpdateViaUnknown = update.MethodUnknown
	UpdateViaBrew    = update.MethodBrew
	UpdateViaScript  = update.MethodScript
)

// CheckUpdate asks whether a newer release exists. It is silent on every
// failure, is skipped for builds that are not a plain release and when
// GITID_NO_UPDATE_CHECK is set, and makes at most one request a day. Only the
// TUI calls it, once at start.
func (s *Service) CheckUpdate(ctx context.Context) (UpdateNotice, bool) {
	return update.Checker{Env: s.Env, Current: s.Version, Executable: s.Executable}.Check(ctx)
}

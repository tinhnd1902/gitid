package tui

import (
	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/doctor"
	"github.com/tinhnd1902/gitid/internal/importer"
	"github.com/tinhnd1902/gitid/internal/keys"
	"github.com/tinhnd1902/gitid/internal/plan"
)

// Navigation messages, handled by the root model.
type (
	pushMsg    struct{ s Screen }
	replaceMsg struct{ s Screen }
	popMsg     struct{}
	popRootMsg struct{}
)

// Level of a status bar message.
type level int

const (
	levelInfo level = iota
	levelOK
	levelWarn
	levelErr
)

// Global state messages, handled by the root model (and broadcast).
type (
	statusMsg struct {
		level level
		text  string
	}
	statusExpireMsg struct{ seq int }
	cfgLoadedMsg    struct {
		cfg config.Config
		err error
	}
	// cfgChangedMsg is emitted after the configuration was saved.
	cfgChangedMsg struct{ cfg config.Config }
	// refreshPlanMsg asks the root to recompute the pending-changes badge.
	refreshPlanMsg struct{}
	planBuiltMsg   struct {
		seq  int
		plan plan.Plan
		err  error
	}
)

// Results of asynchronous work.
type (
	// confirm screen
	confirmPlanMsg struct {
		id   int
		plan plan.Plan
		err  error
	}
	applyDoneMsg struct {
		id  int
		res plan.Result
		err error
	}
	// forms and deletes
	savedMsg struct {
		id     int
		cfg    config.Config
		backup string
		err    error
		after  func(cfg config.Config) Screen // optional next screen
	}
	deletedMsg struct {
		id  int
		res app.DeleteResult
		err error
	}
	// keys
	keyExecDoneMsg struct {
		id  int
		err error
	}
	keyDoneMsg struct {
		id  int
		pub keys.Pub
		cfg config.Config
		err error
	}
	keyLoadedMsg struct {
		id  int
		pub keys.Pub
		err error
	}
	copyDoneMsg struct {
		id  int
		err error
	}
	openDoneMsg struct {
		id  int
		err error
	}
	// clone
	cloneProgressMsg struct {
		id    int
		Phase string
		Pct   int
	}
	cloneDoneMsg struct {
		id  int
		Res app.CloneResult
		Err error
	}
	// check and fix
	checkDoneMsg struct {
		id  int
		res app.CheckResult
		err error
	}
	fixPlanMsg struct {
		id  int
		res app.FixResult
		err error
	}
	fixDoneMsg struct {
		id  int
		res app.FixResult
		err error
	}
	// doctor
	doctorResultMsg struct {
		id      int
		check   string
		results []doctor.Result
	}
	doctorSSHMsg struct {
		id     int
		alias  string
		result doctor.Result
	}
	// import
	importLoadedMsg struct {
		id  int
		res importer.Result
		err error
	}
)

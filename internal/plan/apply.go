package plan

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/fsx"
)

// Errors.
var (
	// ErrStale means a target changed on disk after the plan was built.
	ErrStale = errors.New("plan: a target file changed since the plan was built")
	// ErrGitConfigLocked means ~/.gitconfig.lock exists (git is writing it).
	ErrGitConfigLocked = errors.New("plan: ~/.gitconfig.lock exists")
	// ErrLocked means another gitid process is applying.
	ErrLocked = fsx.ErrLocked
)

// KeepBackups is how many backup directories survive pruning.
const KeepBackups = 30

// StaleError names the file that changed.
type StaleError struct{ Path string }

func (e *StaleError) Error() string { return "plan: file changed since the plan was built: " + e.Path }
func (e *StaleError) Unwrap() error { return ErrStale }

// PartialError reports a failure while writing: which files were already
// written and where the backup is.
type PartialError struct {
	Err       error
	Written   []string
	BackupDir string
}

func (e *PartialError) Error() string {
	return fmt.Sprintf("plan: apply failed after writing %d file(s): %v (backup: %s)", len(e.Written), e.Err, e.BackupDir)
}
func (e *PartialError) Unwrap() error { return e.Err }

// Result describes a finished apply.
type Result struct {
	BackupDir string
	Written   []string
	Deleted   []string
	Warnings  []string
}

// Test hooks.
var (
	writeFn  = fsx.WriteAtomic
	removeFn = os.Remove
)

// Apply executes the plan: lock, refuse if ~/.gitconfig.lock exists, verify
// every target still has the SHA captured at plan time, back everything up,
// then write atomically. An empty plan does nothing (no lock, no backup).
func Apply(ctx context.Context, e env.Env, p Plan) (Result, error) {
	var res Result
	if p.Empty() {
		return res, nil
	}
	paths := e.Paths()
	lock, err := fsx.AcquireLock(paths.LockFile)
	if err != nil {
		return res, err
	}
	defer lock.Release()

	gc, err := fsx.ResolvePath(paths.GitConfig)
	if err != nil {
		return res, err
	}
	if _, err := os.Lstat(gc + ".lock"); err == nil {
		return res, ErrGitConfigLocked
	}

	items := make([]fsx.BackupItem, 0, len(p.Changes))
	for _, c := range p.Changes {
		snap, err := fsx.ReadSnapshot(c.Path)
		if err != nil {
			return res, err
		}
		if snap.Exists != c.Existed || snap.SHA != c.OldSHA {
			return res, &StaleError{Path: c.Display}
		}
		items = append(items, fsx.BackupItem{Snap: snap, Op: c.Op})
	}
	if err := ctx.Err(); err != nil {
		return res, err
	}

	dir, err := fsx.Backup(paths.BackupsDir, e.Now(), items)
	res.BackupDir = dir
	if err != nil {
		return res, fmt.Errorf("backup failed, nothing was written: %w", err)
	}
	for _, d := range p.Dirs {
		if err := fsx.MkdirAll(d.Path, d.Mode); err != nil {
			return res, &PartialError{Err: err, BackupDir: dir}
		}
	}
	for _, c := range p.Changes {
		if err := ctx.Err(); err != nil {
			return res, &PartialError{Err: err, Written: res.Written, BackupDir: dir}
		}
		var werr error
		if c.Op == fsx.OpDelete {
			werr = removeFn(c.Path)
		} else {
			werr = writeFn(c.Path, c.New, c.Mode)
		}
		if werr != nil {
			return res, &PartialError{Err: werr, Written: res.Written, BackupDir: dir}
		}
		if c.Op == fsx.OpDelete {
			res.Deleted = append(res.Deleted, c.Path)
		} else {
			res.Written = append(res.Written, c.Path)
		}
	}
	if err := fsx.Prune(paths.BackupsDir, KeepBackups); err != nil {
		res.Warnings = append(res.Warnings, "prune backups: "+err.Error())
	}
	return res, nil
}

// Summary lists the changes as "op path" lines.
func (p Plan) Summary() string {
	var b strings.Builder
	for _, c := range p.Changes {
		b.WriteString(string(c.Op) + " " + c.Display + "\n")
	}
	return b.String()
}

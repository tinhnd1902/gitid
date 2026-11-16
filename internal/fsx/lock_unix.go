//go:build unix

package fsx

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// ErrLocked means another process (or handle) holds the gitid lock.
var ErrLocked = errors.New("fsx: lock is held by another gitid process")

// Lock is an acquired exclusive lock file.
type Lock struct{ f *os.File }

// AcquireLock takes a non-blocking exclusive flock on path (created 0600,
// parent 0700 if missing).
func AcquireLock(path string) (*Lock, error) {
	if err := MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, err
	}
	return &Lock{f: f}, nil
}

// Release unlocks and closes the lock file.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	err := l.f.Close()
	l.f = nil
	return err
}

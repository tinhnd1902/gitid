//go:build !unix

package fsx

import "errors"

// ErrLocked means another process holds the gitid lock.
var ErrLocked = errors.New("fsx: lock is held by another gitid process")

// Lock is a no-op lock on platforms without flock.
type Lock struct{}

// AcquireLock is a no-op on this platform.
func AcquireLock(path string) (*Lock, error) { return &Lock{}, nil }

// Release is a no-op.
func (l *Lock) Release() error { return nil }

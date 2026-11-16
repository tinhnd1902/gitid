//go:build unix

package fsx

import "syscall"

func syscallUmask(m int) int { return syscall.Umask(m) }

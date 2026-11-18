//go:build !unix

package keymap_test

import "errors"

func mkfifo(string) error { return errors.New("no fifo") }

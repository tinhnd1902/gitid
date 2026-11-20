//go:build unix

package update

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

// U10b: a FIFO at the cache path must not hang the check or be opened.
func TestU10bFIFO(t *testing.T) {
	f := newFixture(t, "0.1.8", nil)
	if err := syscall.Mkfifo(f.cachePath(), 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan bool, 1)
	go func() {
		_, ok := f.c.Check(context.Background())
		done <- ok
	}()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("notice")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the check hung on a FIFO")
	}
	if fi, err := os.Lstat(f.cachePath()); err != nil || fi.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("the FIFO was replaced: %v %v", fi, err)
	}
}

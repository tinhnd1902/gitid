package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tinhnd1902/gitid/internal/env"
)

func TestTempEnvAndGuard(t *testing.T) {
	e := TempEnv(t)
	if e.Home == "" || e.Now() != FixedTime {
		t.Fatal("bad env")
	}
	if proc, err := env.FromOS(); err != nil || proc.Home != e.Home {
		t.Fatal("process HOME not redirected")
	}
	Guard(t, e, e.Paths().GitConfig, e.Paths().ConfigFile, "")
}

func TestGuardFailsOutside(t *testing.T) {
	e := TempEnv(t)
	ft := &fakeT{}
	Guard(ft, e, "/etc/passwd")
	if !ft.failed {
		t.Fatal("guard should fail for outside path")
	}
}

type fakeT struct {
	testing.TB
	failed bool
}

func (f *fakeT) Helper()                           {}
func (f *fakeT) Errorf(format string, args ...any) { f.failed = true }

func TestSnapshot(t *testing.T) {
	e := TempEnv(t)
	WriteFile(t, filepath.Join(e.Home, "a/b.txt"), "x", 0o600)
	if err := os.Symlink("b.txt", filepath.Join(e.Home, "a/l")); err != nil {
		t.Fatal(err)
	}
	WriteFile(t, filepath.Join(e.Home, "a/secret"), "s", 0o600)
	if err := os.Chmod(filepath.Join(e.Home, "a/secret"), 0); err != nil {
		t.Fatal(err)
	}
	s1 := Snapshot(t, e.Home)
	s2 := Snapshot(t, e.Home)
	AssertSame(t, s1, s2)
	if ReadFile(t, filepath.Join(e.Home, "a/b.txt")) != "x" {
		t.Fatal("ReadFile")
	}
	WriteFile(t, filepath.Join(e.Home, "a/b.txt"), "y", 0o600)
	WriteFile(t, filepath.Join(e.Home, "new"), "n", 0o600)
	if err := os.Remove(filepath.Join(e.Home, "a/l")); err != nil {
		t.Fatal(err)
	}
	s3 := Snapshot(t, e.Home)
	d := s2.Diff(s3)
	if len(d) != 5 { // new, removed link, changed file and two dirs (mtime)
		t.Fatalf("diff = %v", d)
	}
	_ = os.Chmod(filepath.Join(e.Home, "a/secret"), 0o600)
}

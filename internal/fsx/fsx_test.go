package fsx

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, p, s string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
}

func real(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mode(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func TestReadSnapshot(t *testing.T) {
	d := real(t)
	p := filepath.Join(d, "f")
	s, err := ReadSnapshot(p)
	if err != nil || s.Exists || s.SHA != "" || s.RealPath != p {
		t.Fatalf("missing: %+v %v", s, err)
	}
	write(t, p, "hello", 0o640)
	s, err = ReadSnapshot(p)
	if err != nil || !s.Exists || string(s.Data) != "hello" || s.Mode != 0o640 || s.SHA != SHA256Hex([]byte("hello")) {
		t.Fatalf("%+v %v", s, err)
	}
	if _, err := ReadSnapshot(d); err == nil {
		t.Fatal("dir should error")
	}
}

func TestSnapshotSymlink(t *testing.T) {
	d := real(t)
	write(t, filepath.Join(d, "dot/gitconfig"), "x", 0o644)
	if err := os.Symlink("dot/gitconfig", filepath.Join(d, "link")); err != nil {
		t.Fatal(err)
	}
	s, err := ReadSnapshot(filepath.Join(d, "link"))
	if err != nil || !s.Exists || s.RealPath != filepath.Join(d, "dot/gitconfig") {
		t.Fatalf("%+v %v", s, err)
	}
	// dangling symlink
	if err := os.Symlink("dot/missing", filepath.Join(d, "dangling")); err != nil {
		t.Fatal(err)
	}
	s, err = ReadSnapshot(filepath.Join(d, "dangling"))
	if err != nil || s.Exists || s.RealPath != filepath.Join(d, "dot/missing") {
		t.Fatalf("%+v %v", s, err)
	}
	// loop
	_ = os.Symlink("l2", filepath.Join(d, "l1"))
	_ = os.Symlink("l1", filepath.Join(d, "l2"))
	if _, err := ResolvePath(filepath.Join(d, "l1")); err == nil {
		t.Fatal("loop should error")
	}
	// missing parent
	if p, err := ResolvePath(filepath.Join(d, "nodir/x")); err != nil || p != filepath.Join(d, "nodir/x") {
		t.Fatalf("%q %v", p, err)
	}
	// relative path without dir
	if p, err := ResolvePath("zzz-nonexistent"); err != nil || p == "" {
		t.Fatalf("%q %v", p, err)
	}
}

func TestWriteAtomicNewAndModes(t *testing.T) { // F1, F2
	d := real(t)
	p := filepath.Join(d, "new")
	if err := WriteAtomic(p, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "abc" || mode(t, p) != 0o600 {
		t.Fatal("new file wrong")
	}
	for _, m := range []os.FileMode{0o644, 0o600} {
		q := filepath.Join(d, "ex"+m.String())
		write(t, q, "old", m)
		if err := WriteAtomic(q, []byte("new"), 0o600); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(q); string(b) != "new" || mode(t, q) != m {
			t.Fatalf("mode not preserved: %v", mode(t, q))
		}
	}
	if err := WriteAtomic(d, []byte("x"), 0o600); err == nil {
		t.Fatal("dir target should fail")
	}
}

func TestWriteAtomicSymlink(t *testing.T) { // F3
	d := real(t)
	target := filepath.Join(d, "dotfiles/gitconfig")
	write(t, target, "old", 0o644)
	link := filepath.Join(d, "gitconfig")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(link, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink replaced")
	}
	if b, _ := os.ReadFile(target); string(b) != "new" {
		t.Fatal("target not updated")
	}
	if m, _ := filepath.Glob(filepath.Join(d, ".gitid-*")); len(m) != 0 {
		t.Fatalf("temp in link dir: %v", m)
	}
}

func TestWriteAtomicFailures(t *testing.T) { // F4
	d := real(t)
	p := filepath.Join(d, "f")
	write(t, p, "orig", 0o644)
	renameFn = func(a, b string) error { return errors.New("injected") }
	err := WriteAtomic(p, []byte("new"), 0o644)
	renameFn = os.Rename
	if err == nil {
		t.Fatal("want rename error")
	}
	if b, _ := os.ReadFile(p); string(b) != "orig" {
		t.Fatal("original changed")
	}
	if m, _ := filepath.Glob(filepath.Join(d, ".gitid-*")); len(m) != 0 {
		t.Fatalf("temp left: %v", m)
	}
	// read-only dir
	if os.Getuid() != 0 {
		ro := filepath.Join(d, "ro")
		write(t, filepath.Join(ro, "f"), "orig", 0o644)
		_ = os.Chmod(ro, 0o500)
		defer os.Chmod(ro, 0o700)
		if err := WriteAtomic(filepath.Join(ro, "f"), []byte("n"), 0o644); err == nil {
			t.Fatal("want permission error")
		}
		if b, _ := os.ReadFile(filepath.Join(ro, "f")); string(b) != "orig" {
			t.Fatal("original changed")
		}
	}
}

func TestBackup(t *testing.T) { // F5
	d := real(t)
	p := filepath.Join(d, "f")
	write(t, p, "old bytes", 0o644)
	snap, _ := ReadSnapshot(p)
	missing, _ := ReadSnapshot(filepath.Join(d, "nope"))
	now := time.Date(2026, 11, 16, 14, 1, 2, 123_000_000, time.UTC)
	bdir := filepath.Join(d, "backups")
	dir, err := Backup(bdir, now, []BackupItem{{snap, OpUpdate}, {missing, OpCreate}})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dir) != "20261116-140102.123" || mode(t, dir) != 0o700 {
		t.Fatalf("dir %s %v", dir, mode(t, dir))
	}
	var m Manifest
	b, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err := json.Unmarshal(b, &m); err != nil || len(m.Entries) != 2 {
		t.Fatalf("manifest: %v %s", err, b)
	}
	e := m.Entries[0]
	if e.Path != p || e.Mode != "0644" || e.SHA256 != snap.SHA || e.Op != OpUpdate || !e.Existed {
		t.Fatalf("entry %+v", e)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, e.BackupFile)); string(got) != "old bytes" || mode(t, filepath.Join(dir, e.BackupFile)) != 0o600 {
		t.Fatal("backup bytes/mode wrong")
	}
	if m.Entries[1].Existed || m.Entries[1].BackupFile != "" {
		t.Fatalf("create entry %+v", m.Entries[1])
	}
	if mode(t, filepath.Join(dir, "manifest.json")) != 0o600 {
		t.Fatal("manifest mode")
	}
	// collision in same ms
	dir2, err := Backup(bdir, now, nil)
	if err != nil || filepath.Base(dir2) != "20261116-140102.123-1" {
		t.Fatalf("%s %v", dir2, err)
	}
	dir3, _ := Backup(bdir, now, nil)
	if filepath.Base(dir3) != "20261116-140102.123-2" {
		t.Fatal(dir3)
	}
}

func TestPrune(t *testing.T) { // F6
	d := real(t)
	bdir := filepath.Join(d, "backups")
	for i := 0; i < 35; i++ {
		name := time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC).Format(BackupDirTime)
		if err := os.MkdirAll(filepath.Join(bdir, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(bdir, "notes.txt"), "keep", 0o600)
	if err := os.MkdirAll(filepath.Join(bdir, "my-own-dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Prune(bdir, 30); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(bdir)
	n := 0
	for _, e := range ents {
		if backupNameRe.MatchString(e.Name()) {
			n++
		}
	}
	if n != 30 || len(ents) != 32 {
		t.Fatalf("kept %d backups, %d entries", n, len(ents))
	}
	oldest := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(BackupDirTime)
	if _, err := os.Stat(filepath.Join(bdir, oldest)); err == nil {
		t.Fatal("oldest should be pruned")
	}
	if err := Prune(filepath.Join(d, "missing"), 30); err != nil {
		t.Fatal(err)
	}
}

func TestLock(t *testing.T) { // F7
	d := real(t)
	p := filepath.Join(d, "cfg/.lock")
	l1, err := AcquireLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLock(p); !errors.Is(err, ErrLocked) {
		t.Fatalf("second lock err=%v", err)
	}
	if err := l1.Release(); err != nil {
		t.Fatal(err)
	}
	l2, err := AcquireLock(p)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	_ = l2.Release()
	_ = l2.Release()
	var nilLock *Lock
	if nilLock.Release() != nil {
		t.Fatal("nil release")
	}
	if mode(t, p) != 0o600 || mode(t, filepath.Dir(p)) != 0o700 {
		t.Fatal("lock modes")
	}
}

func TestMkdirAll(t *testing.T) { // F8
	d := real(t)
	ssh := filepath.Join(d, ".ssh")
	if err := os.Mkdir(ssh, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := MkdirAll(filepath.Join(ssh, "config.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	if mode(t, ssh) != 0o755 || mode(t, filepath.Join(ssh, "config.d")) != 0o700 {
		t.Fatalf("modes %v %v", mode(t, ssh), mode(t, filepath.Join(ssh, "config.d")))
	}
	fresh := filepath.Join(d, "h2/.ssh/gitid")
	if err := MkdirAll(fresh, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(d, "h2"), filepath.Join(d, "h2/.ssh"), fresh} {
		if mode(t, p) != 0o700 {
			t.Fatalf("%s mode %v", p, mode(t, p))
		}
	}
	write(t, filepath.Join(d, "afile"), "x", 0o600)
	if err := MkdirAll(filepath.Join(d, "afile/sub"), 0o700); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("err=%v", err)
	}
	if err := MkdirAll(ssh, 0o700); err != nil || mode(t, ssh) != 0o755 {
		t.Fatal("existing dir must not be chmod'ed")
	}
}

func TestErrorPaths(t *testing.T) {
	d := real(t)
	write(t, filepath.Join(d, "afile"), "x", 0o600)
	now := time.Now()
	if _, err := Backup(filepath.Join(d, "afile/backups"), now, nil); err == nil {
		t.Fatal("backup under a file should fail")
	}
	if _, err := AcquireLock(filepath.Join(d, "afile/sub/.lock")); err == nil {
		t.Fatal("lock under a file should fail")
	}
	if err := WriteAtomic(filepath.Join(d, "nodir/f"), []byte("x"), 0o600); err == nil {
		t.Fatal("missing parent should fail")
	}
	if err := Prune(filepath.Join(d, "afile"), 1); err == nil {
		t.Fatal("prune on a file should fail")
	}
	if _, err := ReadSnapshot(filepath.Join(d, "afile/x")); err == nil {
		t.Fatal("ENOTDIR should surface")
	}
	if os.Getuid() != 0 {
		write(t, filepath.Join(d, "secret"), "x", 0o000)
		if _, err := ReadSnapshot(filepath.Join(d, "secret")); err == nil {
			t.Fatal("unreadable file should error")
		}
		bd := filepath.Join(d, "bk")
		_ = os.MkdirAll(bd, 0o700)
		_ = os.Chmod(bd, 0o500)
		if _, err := Backup(bd, now, nil); err == nil {
			t.Fatal("backup into read-only dir should fail")
		}
		_ = os.Chmod(bd, 0o700)
		// backup item whose file cannot be written (dir exists but items fine) - read-only backup dir after creation is not testable; skip
	}
}

func TestBackupDirChmodNeverLoosens(t *testing.T) {
	d := real(t)
	old := syscallUmask(0)
	defer syscallUmask(old)
	dir, err := Backup(filepath.Join(d, "bk"), time.Now(), nil)
	if err != nil || mode(t, dir) != 0o700 {
		t.Fatalf("%v %v", err, mode(t, dir))
	}
}

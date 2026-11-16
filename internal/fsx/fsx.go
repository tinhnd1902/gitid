// Package fsx contains the safe filesystem primitives gitid uses: snapshots
// with SHA-256, atomic writes that preserve mode and symlinks, timestamped
// backups with a manifest, directory creation with explicit modes and a
// process-wide lock file.
package fsx

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Snapshot is the state of a file at one moment.
type Snapshot struct {
	Path     string      // path as given
	RealPath string      // path after resolving symlinks (target for writes)
	Exists   bool        // false when the file (or symlink target) is missing
	Data     []byte      // nil when !Exists
	Mode     fs.FileMode // permission bits only; 0 when !Exists
	SHA      string      // hex sha256 of Data; "" when !Exists
}

// SHA256Hex returns the hex sha256 of data.
func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ResolvePath resolves symlinks in path. For a missing file it resolves the
// parent directory (and follows a dangling symlink one hop at a time) so
// that writes land on the symlink target.
func ResolvePath(path string) (string, error) {
	p := filepath.Clean(path)
	for i := 0; i < 16; i++ {
		fi, err := os.Lstat(p)
		switch {
		case err == nil && fi.Mode()&os.ModeSymlink != 0:
			t, err := os.Readlink(p)
			if err != nil {
				return "", err
			}
			if !filepath.IsAbs(t) {
				t = filepath.Join(filepath.Dir(p), t)
			}
			p = filepath.Clean(t)
			continue
		case err == nil:
			return resolveDir(p)
		case errors.Is(err, fs.ErrNotExist):
			return resolveDir(p)
		default:
			return "", err
		}
	}
	return "", fmt.Errorf("fsx: too many levels of symlinks: %s", path)
}

func resolveDir(p string) (string, error) {
	dir, base := filepath.Split(p)
	if dir == "" {
		return p, nil
	}
	rd, err := filepath.EvalSymlinks(filepath.Clean(dir))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return p, nil
		}
		return "", err
	}
	return filepath.Join(rd, base), nil
}

// ReadSnapshot reads path (following symlinks). A missing file is not an
// error: Exists is false.
func ReadSnapshot(path string) (Snapshot, error) {
	s := Snapshot{Path: path}
	real, err := ResolvePath(path)
	if err != nil {
		return s, err
	}
	s.RealPath = real
	fi, err := os.Stat(real)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return s, nil
		}
		return s, err
	}
	if !fi.Mode().IsRegular() {
		return s, fmt.Errorf("fsx: %s is not a regular file", path)
	}
	data, err := os.ReadFile(real)
	if err != nil {
		return s, err
	}
	s.Exists, s.Data, s.Mode, s.SHA = true, data, fi.Mode().Perm(), SHA256Hex(data)
	return s, nil
}

// renameFn is replaced in tests to inject failures.
var renameFn = os.Rename

// WriteAtomic writes data to path atomically: temp file in the same
// directory, fsync, chmod, rename, fsync of the directory. If path is a
// symlink the target is written and the symlink kept. An existing file keeps
// its mode; a new file gets mode. The parent directory must exist.
func WriteAtomic(path string, data []byte, mode fs.FileMode) error {
	real, err := ResolvePath(path)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(real); err == nil {
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("fsx: %s is not a regular file", path)
		}
		mode = fi.Mode().Perm()
	}
	dir := filepath.Dir(real)
	tmp, err := os.CreateTemp(dir, ".gitid-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := renameFn(tmpName, real); err != nil {
		return err
	}
	ok = true
	return syncDir(dir)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	// Some filesystems do not support fsync on directories; ignore EINVAL-like errors.
	_ = d.Sync()
	return nil
}

// MkdirAll creates path and any missing parents with exactly mode (chmod'ed
// after creation to defeat the umask). Existing directories are never
// chmod'ed.
func MkdirAll(path string, mode fs.FileMode) error {
	path = filepath.Clean(path)
	var missing []string
	for p := path; ; p = filepath.Dir(p) {
		fi, err := os.Stat(p)
		if err == nil {
			if !fi.IsDir() {
				return fmt.Errorf("fsx: %s exists and is not a directory", p)
			}
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		missing = append(missing, p)
		if filepath.Dir(p) == p {
			break
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], mode); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		if err := os.Chmod(missing[i], mode); err != nil {
			return err
		}
	}
	return nil
}

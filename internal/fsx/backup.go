package fsx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// Op is the kind of change recorded in a backup manifest.
type Op string

// Operations.
const (
	OpCreate Op = "create"
	OpUpdate Op = "update"
	OpDelete Op = "delete"
)

// BackupItem names one file about to be changed.
type BackupItem struct {
	Snap Snapshot
	Op   Op
}

// ManifestEntry is one record of manifest.json.
type ManifestEntry struct {
	Path       string `json:"path"`
	RealPath   string `json:"real_path"`
	Op         Op     `json:"op"`
	Existed    bool   `json:"existed"`
	Mode       string `json:"mode,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	BackupFile string `json:"backup_file,omitempty"`
}

// Manifest is the content of manifest.json.
type Manifest struct {
	Created string          `json:"created"`
	Entries []ManifestEntry `json:"entries"`
}

// BackupDirTime is the time layout of backup directory names.
const BackupDirTime = "20060102-150405.000"

var backupNameRe = regexp.MustCompile(`^\d{8}-\d{6}\.\d{3}(-\d+)?$`)

// Backup stores the old bytes of every existing item under
// <backupsDir>/<timestamp>/ (dir 0700, files 0600) with a manifest.json and
// returns the directory. Bytes come from the snapshots (no re-read).
func Backup(backupsDir string, now time.Time, items []BackupItem) (string, error) {
	if err := MkdirAll(backupsDir, 0o700); err != nil {
		return "", err
	}
	base := now.UTC().Format(BackupDirTime)
	dir := filepath.Join(backupsDir, base)
	for i := 1; ; i++ {
		err := os.Mkdir(dir, 0o700)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
		dir = filepath.Join(backupsDir, fmt.Sprintf("%s-%d", base, i))
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	m := Manifest{Created: now.UTC().Format(time.RFC3339Nano)}
	for i, it := range items {
		e := ManifestEntry{Path: it.Snap.Path, RealPath: it.Snap.RealPath, Op: it.Op, Existed: it.Snap.Exists}
		if it.Snap.Exists {
			name := fmt.Sprintf("%03d-%s", i, filepath.Base(it.Snap.RealPath))
			if err := os.WriteFile(filepath.Join(dir, name), it.Snap.Data, 0o600); err != nil {
				return dir, err
			}
			if err := os.Chmod(filepath.Join(dir, name), 0o600); err != nil {
				return dir, err
			}
			e.BackupFile, e.Mode, e.SHA256 = name, fmt.Sprintf("%04o", it.Snap.Mode), it.Snap.SHA
		}
		m.Entries = append(m.Entries, e)
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return dir, err
	}
	mp := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(mp, append(b, '\n'), 0o600); err != nil {
		return dir, err
	}
	if err := os.Chmod(mp, 0o600); err != nil {
		return dir, err
	}
	return dir, nil
}

// Prune keeps the newest keep backup directories (by name) and removes the
// rest. Entries whose names do not look like gitid backups are never touched.
func Prune(backupsDir string, keep int) error {
	ents, err := os.ReadDir(backupsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() && backupNameRe.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) <= keep {
		return nil
	}
	for _, n := range names[:len(names)-keep] {
		if err := os.RemoveAll(filepath.Join(backupsDir, n)); err != nil {
			return err
		}
	}
	return nil
}

package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/fsx"
)

// ErrUnsupportedVersion is returned for a config file of another schema
// version; the file is never overwritten in that case.
var ErrUnsupportedVersion = errors.New("config: unsupported config version")

const header = "# gitid configuration. Written by gitid; comments are not preserved on save.\n"

// Load reads config.yaml. A missing file yields Default() and creates
// nothing.
func Load(e env.Env) (Config, error) {
	p := e.Paths().ConfigFile
	snap, err := fsx.ReadSnapshot(p)
	if err != nil {
		return Config{}, err
	}
	if !snap.Exists {
		return Default(), nil
	}
	return Parse(snap.Data)
}

// Parse decodes config.yaml content.
func Parse(data []byte) (Config, error) {
	var probe struct {
		Version int `yaml:"version"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if probe.Version != CurrentVersion {
		return Config{}, fmt.Errorf("%w: %d (supported: %d)", ErrUnsupportedVersion, probe.Version, CurrentVersion)
	}
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if cfg.Settings.Language == "" {
		cfg.Settings.Language = "vi"
	}
	if cfg.Settings.CodeRoot == "" {
		cfg.Settings.CodeRoot = "~/Code"
	}
	return cfg, nil
}

// Marshal encodes the config as it is stored on disk.
func Marshal(cfg Config) ([]byte, error) {
	if cfg.Version == 0 {
		cfg.Version = CurrentVersion
	}
	var buf bytes.Buffer
	buf.WriteString(header)
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cfg); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Save writes config.yaml atomically with mode 0600. If a different file
// already exists it is backed up first; the backup directory is returned
// ("" when nothing was backed up or nothing changed).
func Save(e env.Env, cfg Config) (backupDir string, err error) {
	data, err := Marshal(cfg)
	if err != nil {
		return "", err
	}
	p := e.Paths()
	snap, err := fsx.ReadSnapshot(p.ConfigFile)
	if err != nil {
		return "", err
	}
	if snap.Exists && bytes.Equal(snap.Data, data) {
		return "", nil
	}
	if snap.Exists {
		// Refuse to overwrite a file of another schema version.
		if _, perr := Parse(snap.Data); errors.Is(perr, ErrUnsupportedVersion) {
			return "", perr
		}
		backupDir, err = fsx.Backup(p.BackupsDir, e.Now(), []fsx.BackupItem{{Snap: snap, Op: fsx.OpUpdate}})
		if err != nil {
			return "", err
		}
	}
	if err := fsx.MkdirAll(p.ConfigDir, 0o700); err != nil {
		return backupDir, err
	}
	if err := fsx.WriteAtomic(p.ConfigFile, data, fs.FileMode(0o600)); err != nil {
		return backupDir, err
	}
	// WriteAtomic preserves an existing mode; tighten a file created loosely.
	if snap.Exists && snap.Mode != 0o600 {
		if err := os.Chmod(snap.RealPath, 0o600); err != nil {
			return backupDir, err
		}
	}
	return backupDir, nil
}

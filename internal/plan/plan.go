// Package plan computes the set of file changes needed to bring the user's
// git and ssh configuration in line with gitid's config, shows them as a
// diff and applies them safely (lock, stale check, backup, atomic writes).
package plan

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tinhnd1902/gitid/internal/block"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/fsx"
	"github.com/tinhnd1902/gitid/internal/gitcfg"
	"github.com/tinhnd1902/gitid/internal/sshcfg"
)

// WarnGitidConfForeign is the warning key for a gitid.conf that gitid did
// not create (it is left untouched).
const WarnGitidConfForeign = "warn.gitid_conf_foreign"

// Kind classifies a change for display.
type Kind string

// Kinds.
const (
	KindGitConfig Kind = "gitconfig" // ~/.gitconfig managed block
	KindSSHConfig Kind = "sshconfig" // ~/.ssh/config managed block
	KindSSHConf   Kind = "gitid.conf"
	KindGenerated Kind = "generated" // files under <configdir>/git
)

// FileChange is one planned change.
type FileChange struct {
	Path     string // path as gitid knows it (may be a symlink)
	RealPath string // where the bytes land
	Display  string // ~-contracted path for diffs
	Kind     Kind
	Op       fsx.Op
	Old, New []byte
	OldSHA   string // "" when the file did not exist
	Existed  bool
	OldMode  fs.FileMode
	Mode     fs.FileMode // mode used when creating
	prio     int
}

// Dir is a directory that Apply must create.
type Dir struct {
	Path string
	Mode fs.FileMode
}

// Plan is the full set of pending changes.
type Plan struct {
	Changes  []FileChange
	Dirs     []Dir
	Warnings []string
}

// Empty reports whether nothing needs to change.
func (p Plan) Empty() bool { return len(p.Changes) == 0 }

// Paths lists the paths of all changes.
func (p Plan) Paths() []string {
	var out []string
	for _, c := range p.Changes {
		out = append(out, c.Path)
	}
	return out
}

type builder struct {
	e    env.Env
	plan Plan
	dirs map[string]bool
}

// dirMode is 0700 for directories gitid owns or that hold secrets, 0755 for
// anything else it has to create on the way (for example ~/.config).
func (b *builder) dirMode(path string) fs.FileMode {
	p := b.e.Paths()
	if env.IsUnder(path, p.ConfigDir) || env.IsUnder(path, p.SSHDir) {
		return 0o700
	}
	return 0o755
}

func (b *builder) needDir(path string) {
	mode := b.dirMode(path)
	// Walk up to the first existing directory; every missing one is created.
	var missing []string
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			break
		}
		missing = append(missing, p)
		if filepath.Dir(p) == p {
			break
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if !b.dirs[missing[i]] {
			b.dirs[missing[i]] = true
			b.plan.Dirs = append(b.plan.Dirs, Dir{Path: missing[i], Mode: mode})
		}
	}
}

// set records the desired content of a file (nil want = delete).
func (b *builder) set(kind Kind, path string, want []byte, wantExists bool, createMode fs.FileMode, prio int) error {
	snap, err := fsx.ReadSnapshot(path)
	if err != nil {
		return err
	}
	ch := FileChange{
		Path: path, RealPath: snap.RealPath, Display: b.e.Contract(path), Kind: kind,
		Old: snap.Data, OldSHA: snap.SHA, Existed: snap.Exists, OldMode: snap.Mode, Mode: createMode, prio: prio,
	}
	switch {
	case !wantExists && !snap.Exists:
		return nil
	case !wantExists:
		ch.Op = fsx.OpDelete
	case !snap.Exists:
		ch.Op, ch.New = fsx.OpCreate, want
		b.needDir(filepath.Dir(snap.RealPath))
	case string(snap.Data) == string(want):
		return nil
	default:
		ch.Op, ch.New = fsx.OpUpdate, want
	}
	b.plan.Changes = append(b.plan.Changes, ch)
	return nil
}

// Build computes the plan. It only reads: nothing is created or modified.
func Build(e env.Env, cfg config.Config) (Plan, error) {
	b := &builder{e: e, dirs: map[string]bool{}}
	paths := e.Paths()
	desired := map[string]bool{}

	// 1. generated git files
	for _, c := range cfg.Companies {
		if len(c.Accounts) == 0 {
			continue
		}
		cf, err := gitcfg.RenderCompanyFile(e, c)
		if err != nil {
			return Plan{}, fmt.Errorf("company %s: %w", c.Slug, err)
		}
		p := gitcfg.CompanyFile(e, c)
		desired[p] = true
		if err := b.set(KindGenerated, p, []byte(cf), true, 0o600, 0); err != nil {
			return Plan{}, err
		}
		for _, a := range c.Accounts {
			af, err := gitcfg.RenderAccountFile(a)
			if err != nil {
				return Plan{}, fmt.Errorf("account %s/%s: %w", c.Slug, a.ID, err)
			}
			p := gitcfg.AccountFile(e, c, a)
			desired[p] = true
			if err := b.set(KindGenerated, p, []byte(af), true, 0o600, 0); err != nil {
				return Plan{}, err
			}
		}
	}

	// 2. ssh
	conf, err := sshcfg.RenderGitidConf(cfg)
	if err != nil {
		return Plan{}, err
	}
	if conf != "" {
		if err := b.set(KindSSHConf, paths.SSHGitidConf, []byte(conf), true, 0o600, 0); err != nil {
			return Plan{}, err
		}
		b.needDir(paths.SSHConfigD)
	}
	sshSnap, err := fsx.ReadSnapshot(paths.SSHConfig)
	if err != nil {
		return Plan{}, err
	}
	if conf != "" {
		res, err := sshcfg.EnsureInclude(string(sshSnap.Data))
		if err != nil {
			return Plan{}, fmt.Errorf("%s: %w", e.Contract(paths.SSHConfig), err)
		}
		b.plan.Warnings = append(b.plan.Warnings, res.Warnings...)
		if res.Text != string(sshSnap.Data) || !sshSnap.Exists {
			if err := b.set(KindSSHConfig, paths.SSHConfig, []byte(res.Text), true, 0o600, 1); err != nil {
				return Plan{}, err
			}
		}
	} else if sshSnap.Exists {
		out, err := sshcfg.RemoveInclude(string(sshSnap.Data))
		if err != nil {
			return Plan{}, fmt.Errorf("%s: %w", e.Contract(paths.SSHConfig), err)
		}
		if out != string(sshSnap.Data) {
			if err := b.set(KindSSHConfig, paths.SSHConfig, []byte(out), true, 0o600, 1); err != nil {
				return Plan{}, err
			}
		}
	}
	if conf == "" {
		// gitid.conf is wholly ours: remove it when no ssh accounts remain.
		gs, err := fsx.ReadSnapshot(paths.SSHGitidConf)
		if err != nil {
			return Plan{}, err
		}
		if gs.Exists {
			if gitcfg.IsGenerated(string(gs.Data)) {
				if err := b.set(KindSSHConf, paths.SSHGitidConf, nil, false, 0, 3); err != nil {
					return Plan{}, err
				}
			} else {
				b.plan.Warnings = append(b.plan.Warnings, WarnGitidConfForeign)
			}
		}
	}

	// 3. ~/.gitconfig
	body, err := gitcfg.RenderManagedBody(e, cfg)
	if err != nil {
		return Plan{}, err
	}
	gSnap, err := fsx.ReadSnapshot(paths.GitConfig)
	if err != nil {
		return Plan{}, err
	}
	newG, err := block.Upsert(string(gSnap.Data), body, gitcfg.Markers, block.AtEnd)
	if err != nil {
		return Plan{}, fmt.Errorf("%s: %w", e.Contract(paths.GitConfig), err)
	}
	if newG != string(gSnap.Data) || (!gSnap.Exists && body != "") {
		if err := b.set(KindGitConfig, paths.GitConfig, []byte(newG), true, 0o644, 2); err != nil {
			return Plan{}, err
		}
	}

	// 4. orphans in <configdir>/git (generated files only)
	ents, err := os.ReadDir(paths.GitDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Plan{}, err
	}
	for _, en := range ents {
		if en.IsDir() || !strings.HasSuffix(en.Name(), ".gitconfig") {
			continue
		}
		p := filepath.Join(paths.GitDir, en.Name())
		if desired[p] {
			continue
		}
		snap, err := fsx.ReadSnapshot(p)
		if err != nil {
			return Plan{}, err
		}
		if snap.Exists && gitcfg.IsGenerated(string(snap.Data)) {
			if err := b.set(KindGenerated, p, nil, false, 0, 3); err != nil {
				return Plan{}, err
			}
		}
	}

	sort.SliceStable(b.plan.Changes, func(i, j int) bool {
		ci, cj := b.plan.Changes[i], b.plan.Changes[j]
		if ci.prio != cj.prio {
			return ci.prio < cj.prio
		}
		return ci.Path < cj.Path
	})
	sort.Slice(b.plan.Dirs, func(i, j int) bool { return len(b.plan.Dirs[i].Path) < len(b.plan.Dirs[j].Path) })
	return b.plan, nil
}

// UserHostPatterns returns the Host patterns in the user's own
// ~/.ssh/config (the gitid block excluded), for alias collision checks.
func UserHostPatterns(e env.Env) ([]string, error) {
	snap, err := fsx.ReadSnapshot(e.Paths().SSHConfig)
	if err != nil {
		return nil, err
	}
	if !snap.Exists {
		return nil, nil
	}
	return sshcfg.HostPatterns(sshcfg.ParseHosts(string(snap.Data))), nil
}

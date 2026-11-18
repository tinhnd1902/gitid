// Package scan finds git repositories under a folder. It is a pure
// filesystem walk: no git process, no configuration, no network.
//
// The walk is bounded (depth and number of folders), never descends into a
// repository, never descends through a symlink (so a loop is impossible), and
// skips hidden folders, node_modules and vendor. Folders it cannot read are
// collected, not fatal.
package scan

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Kind is what a found folder is.
type Kind string

// Kinds.
const (
	KindRepo      Kind = "repo"
	KindWorktree  Kind = "worktree"
	KindSubmodule Kind = "submodule"
	KindBare      Kind = "bare"
)

// Found is one repository.
type Found struct {
	Path       string // under the root that was given, not its resolved form
	Kind       Kind
	ViaSymlink bool   // Path is a symlink whose target is a repository
	GitDir     string // the "gitdir:" value of a .git file ("" otherwise)
}

// Defaults of Options.
const (
	DefaultMaxDepth = 4
	DefaultMaxDirs  = 20000
	// maxDeniedKept is how many unreadable folders are listed (the count is
	// always exact).
	maxDeniedKept = 20
	// progressEvery is how many folders pass between two Progress calls.
	progressEvery = 64
	// maxGitFile is how much of a .git file is read.
	maxGitFile = 4096
)

// DefaultSkipNames are the folder names the walk never enters.
var DefaultSkipNames = []string{"node_modules", "vendor"}

// Options bound the walk. A zero field takes its default.
type Options struct {
	MaxDepth  int      // repos deeper than this below the root are not found
	MaxDirs   int      // folders visited at most; the result is then Truncated
	SkipNames []string // nil: DefaultSkipNames
	// Progress, when set, is called every few dozen folders with the counts
	// so far (from the walking goroutine; keep it cheap).
	Progress func(Stats)
	// Skip, when set, is asked about every folder before it is visited
	// (the root included); a folder it rejects is neither counted nor
	// entered.
	Skip func(path string) bool
}

// Stats are the counts of a walk.
type Stats struct {
	Dirs        int
	Repos       int
	Denied      []string // the first folders that could not be read
	DeniedCount int
	Truncated   bool // MaxDirs was reached
}

// Errors.
var (
	ErrRootMissing = errors.New("scan: the folder does not exist")
	ErrRootNotDir  = errors.New("scan: the path is not a folder")
)

func (o Options) withDefaults() Options {
	if o.MaxDepth <= 0 {
		o.MaxDepth = DefaultMaxDepth
	}
	if o.MaxDirs <= 0 {
		o.MaxDirs = DefaultMaxDirs
	}
	if o.SkipNames == nil {
		o.SkipNames = DefaultSkipNames
	}
	return o
}

type walker struct {
	ctx  context.Context
	o    Options
	skip map[string]bool
	emit func(Found)
	st   Stats
	stop error // set when the walk must end (cancel); Truncated stops without error
}

// Walk finds the repositories under root and calls emit for each, in
// directory order, from the calling goroutine. It returns the counts and
// ErrRootMissing / ErrRootNotDir / ctx.Err() when the walk could not run or
// was cancelled. A root that is a repository is itself the only result.
func Walk(ctx context.Context, root string, o Options, emit func(Found)) (Stats, error) {
	o = o.withDefaults()
	w := &walker{ctx: ctx, o: o, skip: map[string]bool{}, emit: emit}
	for _, n := range o.SkipNames {
		w.skip[n] = true
	}
	// Stat follows a symlinked root: the root may be a link to the real
	// folder, which is the only symlink the walk ever follows.
	fi, err := os.Stat(root)
	switch {
	case err == nil:
	case errors.Is(err, fs.ErrNotExist):
		return Stats{}, ErrRootMissing
	case errors.Is(err, fs.ErrPermission):
		w.deny(root)
		return w.st, nil
	default:
		return Stats{}, ErrRootMissing
	}
	if !fi.IsDir() {
		return Stats{}, ErrRootNotDir
	}
	w.dir(root, 0)
	if w.stop != nil {
		return w.st, w.stop
	}
	return w.st, nil
}

func (w *walker) deny(path string) {
	w.st.DeniedCount++
	if len(w.st.Denied) < maxDeniedKept {
		w.st.Denied = append(w.st.Denied, path)
	}
}

func (w *walker) cancelled() bool {
	if w.stop != nil {
		return true
	}
	if err := w.ctx.Err(); err != nil {
		w.stop = err
		return true
	}
	return false
}

func (w *walker) found(f Found) {
	if w.cancelled() {
		return
	}
	w.st.Repos++
	if w.emit != nil {
		w.emit(f)
	}
}

// dir visits one folder: it is either a repository (reported, not entered) or
// a plain folder whose sub-folders are visited.
func (w *walker) dir(path string, depth int) {
	if w.cancelled() || w.st.Truncated {
		return
	}
	if w.o.Skip != nil && w.o.Skip(path) {
		return
	}
	if w.st.Dirs >= w.o.MaxDirs {
		w.st.Truncated = true
		return
	}
	w.st.Dirs++
	if w.o.Progress != nil && w.st.Dirs%progressEvery == 0 {
		w.o.Progress(w.st)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			w.deny(path)
		}
		return
	}
	if f, ok := classify(path, entries); ok {
		w.found(f)
		return
	}
	if depth >= w.o.MaxDepth {
		return
	}
	for _, e := range entries {
		if w.cancelled() || w.st.Truncated {
			return
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") || w.skip[name] {
			continue
		}
		child := filepath.Join(path, name)
		switch {
		case e.Type()&fs.ModeSymlink != 0:
			// never enter a link; a link to a repository is listed
			if fi, err := os.Stat(child); err == nil && fi.IsDir() {
				if sub, err := os.ReadDir(child); err == nil {
					if f, ok := classify(child, sub); ok {
						f.ViaSymlink = true
						w.found(f)
					}
				}
			}
		case e.IsDir():
			w.dir(child, depth+1)
		}
	}
}

// classify says whether a folder with these entries is a repository: it holds
// a .git folder or a .git file, or it is a bare repository (HEAD, objects/ and
// refs/).
func classify(path string, entries []fs.DirEntry) (Found, bool) {
	var head, objects, refs bool
	for _, e := range entries {
		switch e.Name() {
		case ".git":
			if e.IsDir() {
				return Found{Path: path, Kind: KindRepo}, true
			}
			if e.Type()&fs.ModeSymlink != 0 {
				if fi, err := os.Stat(filepath.Join(path, ".git")); err == nil && fi.IsDir() {
					return Found{Path: path, Kind: KindRepo}, true
				}
			}
			return gitFile(path), true
		case "HEAD":
			head = !e.IsDir()
		case "objects":
			objects = e.IsDir()
		case "refs":
			refs = e.IsDir()
		}
	}
	if head && objects && refs {
		return Found{Path: path, Kind: KindBare}, true
	}
	return Found{}, false
}

// gitFile reads the .git file of a worktree or a submodule. A file that
// cannot be read or parsed is reported as a plain repository: git itself will
// say what is wrong with it.
func gitFile(path string) Found {
	f := Found{Path: path, Kind: KindRepo}
	fh, err := os.Open(filepath.Join(path, ".git"))
	if err != nil {
		return f
	}
	defer fh.Close()
	b, err := io.ReadAll(io.LimitReader(fh, maxGitFile))
	if err != nil {
		return f
	}
	line, _, _ := strings.Cut(string(b), "\n")
	dir, ok := strings.CutPrefix(strings.TrimSpace(line), "gitdir:")
	if !ok {
		return f
	}
	f.GitDir = strings.TrimSpace(dir)
	slashed := filepath.ToSlash(f.GitDir)
	switch {
	case strings.Contains(slashed, "/worktrees/"):
		f.Kind = KindWorktree
	case strings.Contains(slashed, "/modules/"):
		f.Kind = KindSubmodule
	}
	return f
}

// Detect reports whether dir itself is a repository (a folder holding .git,
// or a bare repository), without looking below it.
func Detect(dir string) (Found, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Found{}, false
	}
	return classify(dir, entries)
}

package scan_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/tinhnd1902/gitid/internal/scan"
)

func mkdir(t testing.TB, p ...string) string {
	t.Helper()
	d := filepath.Join(p...)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	return d
}

func write(t testing.TB, path, content string) {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mkRepo creates a repository folder of the given kind at dir.
func mkRepo(t testing.TB, dir, kind string) {
	t.Helper()
	switch kind {
	case "repo":
		mkdir(t, dir, ".git")
	case "worktree":
		mkdir(t, dir)
		write(t, filepath.Join(dir, ".git"), "gitdir: /x/main/.git/worktrees/w\n")
	case "submodule":
		mkdir(t, dir)
		write(t, filepath.Join(dir, ".git"), "gitdir: ../.git/modules/m\n")
	case "bare":
		mkdir(t, dir, "objects")
		mkdir(t, dir, "refs")
		write(t, filepath.Join(dir, "HEAD"), "ref: refs/heads/main\n")
	default:
		t.Fatalf("kind %q", kind)
	}
}

func walk(t *testing.T, root string, o scan.Options) ([]scan.Found, scan.Stats, error) {
	t.Helper()
	var got []scan.Found
	st, err := scan.Walk(context.Background(), root, o, func(f scan.Found) { got = append(got, f) })
	sort.Slice(got, func(i, j int) bool { return got[i].Path < got[j].Path })
	return got, st, err
}

func rels(root string, fs []scan.Found) []string {
	var out []string
	for _, f := range fs {
		r, _ := filepath.Rel(root, f.Path)
		out = append(out, r)
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func tmp(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestS1TwoRepos(t *testing.T) {
	root := tmp(t)
	mkRepo(t, filepath.Join(root, "a"), "repo")
	mkRepo(t, filepath.Join(root, "b"), "repo")
	got, st, err := walk(t, root, scan.Options{})
	if err != nil || !eq(rels(root, got), []string{"a", "b"}) || st.Repos != 2 {
		t.Fatalf("%v %+v %v", rels(root, got), st, err)
	}
	for _, f := range got {
		if f.Kind != scan.KindRepo || f.ViaSymlink {
			t.Errorf("%+v", f)
		}
	}
}

func TestS2Depth2(t *testing.T) {
	root := tmp(t)
	mkRepo(t, filepath.Join(root, "org", "x"), "repo")
	got, _, _ := walk(t, root, scan.Options{})
	if !eq(rels(root, got), []string{"org/x"}) {
		t.Fatalf("%v", rels(root, got))
	}
}

func TestS3DepthBoundary(t *testing.T) {
	root := tmp(t)
	mkRepo(t, filepath.Join(root, "a", "b", "c", "d"), "repo")      // depth 4
	mkRepo(t, filepath.Join(root, "a", "b", "c", "e", "f"), "repo") // depth 5
	got, _, _ := walk(t, root, scan.Options{MaxDepth: 4})
	if !eq(rels(root, got), []string{"a/b/c/d"}) {
		t.Fatalf("%v", rels(root, got))
	}
	got, _, _ = walk(t, root, scan.Options{MaxDepth: 1})
	if len(got) != 0 {
		t.Fatalf("depth 1: %v", rels(root, got))
	}
}

func TestS4RootIsRepo(t *testing.T) {
	root := tmp(t)
	mkRepo(t, root, "repo")
	mkRepo(t, filepath.Join(root, "inner"), "repo")
	got, st, _ := walk(t, root, scan.Options{})
	if len(got) != 1 || got[0].Path != root || st.Dirs != 1 {
		t.Fatalf("%+v %+v", got, st)
	}
}

func TestS5NoDescentIntoRepo(t *testing.T) {
	root := tmp(t)
	mkRepo(t, filepath.Join(root, "repo"), "repo")
	mkRepo(t, filepath.Join(root, "repo", "sub"), "repo")
	mkdir(t, root, "repo", ".git", "x")
	got, _, _ := walk(t, root, scan.Options{})
	if !eq(rels(root, got), []string{"repo"}) {
		t.Fatalf("%v", rels(root, got))
	}
}

func TestS6Worktree(t *testing.T) {
	root := tmp(t)
	mkRepo(t, filepath.Join(root, "w"), "worktree")
	got, _, _ := walk(t, root, scan.Options{})
	if len(got) != 1 || got[0].Kind != scan.KindWorktree || got[0].GitDir != "/x/main/.git/worktrees/w" {
		t.Fatalf("%+v", got)
	}
}

func TestS7Submodule(t *testing.T) {
	root := tmp(t)
	mkRepo(t, filepath.Join(root, "m"), "submodule")
	got, _, _ := walk(t, root, scan.Options{})
	if len(got) != 1 || got[0].Kind != scan.KindSubmodule || got[0].GitDir != "../.git/modules/m" {
		t.Fatalf("%+v", got)
	}
}

func TestS8Bare(t *testing.T) {
	root := tmp(t)
	mkRepo(t, filepath.Join(root, "proj.git"), "bare")
	mkdir(t, root, "proj.git", "objects", "pack", "deep", ".git")
	got, st, _ := walk(t, root, scan.Options{})
	if len(got) != 1 || got[0].Kind != scan.KindBare || st.Dirs != 2 {
		t.Fatalf("%+v %+v", got, st)
	}
}

func TestS9HeadOnlyIsNotARepo(t *testing.T) {
	root := tmp(t)
	write(t, filepath.Join(root, "p", "HEAD"), "ref: refs/heads/main\n")
	mkdir(t, root, "q", "objects")
	got, _, _ := walk(t, root, scan.Options{})
	if len(got) != 0 {
		t.Fatalf("%v", rels(root, got))
	}
}

func TestS10SkipRules(t *testing.T) {
	root := tmp(t)
	mkRepo(t, filepath.Join(root, "node_modules", "p"), "repo")
	mkRepo(t, filepath.Join(root, "vendor", "q"), "repo")
	mkRepo(t, filepath.Join(root, ".cache", "r"), "repo")
	mkRepo(t, filepath.Join(root, "ok", ".hidden", "s"), "repo")
	mkRepo(t, filepath.Join(root, "ok", "real"), "repo")
	got, _, _ := walk(t, root, scan.Options{})
	if !eq(rels(root, got), []string{"ok/real"}) {
		t.Fatalf("%v", rels(root, got))
	}
	// SkipNames replaces the defaults
	got, _, _ = walk(t, root, scan.Options{SkipNames: []string{"ok"}})
	if !eq(rels(root, got), []string{"node_modules/p", "vendor/q"}) {
		t.Fatalf("custom skip: %v", rels(root, got))
	}
}

func TestS11HiddenRootIsScanned(t *testing.T) {
	root := filepath.Join(tmp(t), ".work", "acme")
	mkRepo(t, filepath.Join(root, "api"), "repo")
	got, _, err := walk(t, root, scan.Options{})
	if err != nil || !eq(rels(root, got), []string{"api"}) {
		t.Fatalf("%v %v", rels(root, got), err)
	}
}

func TestS12SymlinkedRepo(t *testing.T) {
	base := tmp(t)
	root := mkdir(t, base, "root")
	target := filepath.Join(base, "elsewhere", "repo")
	mkRepo(t, target, "repo")
	mkRepo(t, filepath.Join(target, "inner"), "repo")
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Skip("symlinks:", err)
	}
	got, st, _ := walk(t, root, scan.Options{})
	if len(got) != 1 || !got[0].ViaSymlink || got[0].Path != filepath.Join(root, "link") || st.Repos != 1 {
		t.Fatalf("%+v %+v", got, st)
	}
}

func TestS13SymlinkLoop(t *testing.T) {
	root := tmp(t)
	mkdir(t, root, "a")
	mkRepo(t, filepath.Join(root, "b", "r"), "repo")
	if err := os.Symlink(root, filepath.Join(root, "a", "loop")); err != nil {
		t.Skip("symlinks:", err)
	}
	if err := os.Symlink("..", filepath.Join(root, "b", "up")); err != nil {
		t.Skip("symlinks:", err)
	}
	got, st, err := walk(t, root, scan.Options{})
	if err != nil || !eq(rels(root, got), []string{"b/r"}) {
		t.Fatalf("%v %v", rels(root, got), err)
	}
	if st.Dirs > 6 {
		t.Fatalf("Dirs = %d", st.Dirs)
	}
}

func TestS14NoDescentThroughLinks(t *testing.T) {
	base := tmp(t)
	root := mkdir(t, base, "root")
	plain := filepath.Join(base, "plain")
	mkRepo(t, filepath.Join(plain, "one"), "repo")
	mkRepo(t, filepath.Join(plain, "two"), "repo")
	if err := os.Symlink(plain, filepath.Join(root, "link")); err != nil {
		t.Skip("symlinks:", err)
	}
	got, _, _ := walk(t, root, scan.Options{})
	if len(got) != 0 {
		t.Fatalf("%v", rels(root, got))
	}
}

func TestS15SymlinkedRoot(t *testing.T) {
	base := tmp(t)
	real := mkdir(t, base, "real")
	mkRepo(t, filepath.Join(real, "api"), "repo")
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks:", err)
	}
	got, _, err := walk(t, link, scan.Options{})
	if err != nil || len(got) != 1 || got[0].Path != filepath.Join(link, "api") || got[0].ViaSymlink {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestS16PermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	root := tmp(t)
	mkRepo(t, filepath.Join(root, "ok"), "repo")
	locked := mkdir(t, root, "locked")
	mkRepo(t, filepath.Join(locked, "hidden"), "repo")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	got, st, err := walk(t, root, scan.Options{})
	if err != nil || !eq(rels(root, got), []string{"ok"}) {
		t.Fatalf("%v %v", rels(root, got), err)
	}
	if st.DeniedCount != 1 || len(st.Denied) != 1 || st.Denied[0] != locked {
		t.Fatalf("%+v", st)
	}
}

func TestS17RootMissing(t *testing.T) {
	var emits int
	_, err := scan.Walk(context.Background(), filepath.Join(tmp(t), "nope"), scan.Options{}, func(scan.Found) { emits++ })
	if !errors.Is(err, scan.ErrRootMissing) || emits != 0 {
		t.Fatalf("%v %d", err, emits)
	}
}

func TestS18RootNotDir(t *testing.T) {
	f := filepath.Join(tmp(t), "file")
	write(t, f, "x")
	_, err := scan.Walk(context.Background(), f, scan.Options{}, nil)
	if !errors.Is(err, scan.ErrRootNotDir) {
		t.Fatalf("%v", err)
	}
	// a path below a file is just missing
	_, err = scan.Walk(context.Background(), filepath.Join(f, "x"), scan.Options{}, nil)
	if !errors.Is(err, scan.ErrRootMissing) {
		t.Fatalf("below a file: %v", err)
	}
}

func TestS19DirCap(t *testing.T) {
	root := tmp(t)
	for i := 0; i < 30; i++ {
		mkRepo(t, filepath.Join(root, fmt.Sprintf("d%02d", i)), "repo")
	}
	got, st, err := walk(t, root, scan.Options{MaxDirs: 10})
	if err != nil || !st.Truncated || st.Dirs > 10 {
		t.Fatalf("%+v %v", st, err)
	}
	if len(got) != st.Repos || len(got) > 9 {
		t.Fatalf("emits %d, repos %d", len(got), st.Repos)
	}
	// without the cap nothing is truncated
	_, st, _ = walk(t, root, scan.Options{})
	if st.Truncated || st.Repos != 30 {
		t.Fatalf("%+v", st)
	}
}

func TestS20Cancel(t *testing.T) {
	root := tmp(t)
	for i := 0; i < 20; i++ {
		mkRepo(t, filepath.Join(root, fmt.Sprintf("d%02d", i)), "repo")
	}
	ctx, cancel := context.WithCancel(context.Background())
	var emits int
	st, err := scan.Walk(ctx, root, scan.Options{}, func(scan.Found) {
		emits++
		cancel()
	})
	if !errors.Is(err, context.Canceled) || emits != 1 || st.Repos != 1 {
		t.Fatalf("emits=%d err=%v %+v", emits, err, st)
	}
	// a context that is already cancelled emits nothing
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	emits = 0
	if _, err := scan.Walk(ctx2, root, scan.Options{}, func(scan.Found) { emits++ }); !errors.Is(err, context.Canceled) || emits != 0 {
		t.Fatalf("pre-cancelled: %d %v", emits, err)
	}
}

func TestProgress(t *testing.T) {
	root := tmp(t)
	for i := 0; i < 200; i++ {
		mkdir(t, root, fmt.Sprintf("d%03d", i))
	}
	var calls int
	var last scan.Stats
	_, err := scan.Walk(context.Background(), root, scan.Options{Progress: func(s scan.Stats) { calls++; last = s }}, nil)
	if err != nil || calls < 2 || last.Dirs < 64 {
		t.Fatalf("calls=%d last=%+v err=%v", calls, last, err)
	}
}

func TestDeniedListIsCapped(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	root := tmp(t)
	var locked []string
	for i := 0; i < 25; i++ {
		d := mkdir(t, root, fmt.Sprintf("l%02d", i))
		if err := os.Chmod(d, 0); err != nil {
			t.Fatal(err)
		}
		locked = append(locked, d)
	}
	t.Cleanup(func() {
		for _, d := range locked {
			_ = os.Chmod(d, 0o755)
		}
	})
	_, st, _ := walk(t, root, scan.Options{})
	if st.DeniedCount != 25 || len(st.Denied) != 20 {
		t.Fatalf("%d %d", st.DeniedCount, len(st.Denied))
	}
}

func TestGitFileVariants(t *testing.T) {
	root := tmp(t)
	write(t, filepath.Join(root, "garbage", ".git"), "not a gitfile")
	write(t, filepath.Join(root, "sep", ".git"), "gitdir: /somewhere/else.git\n")
	got, _, _ := walk(t, root, scan.Options{})
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	for _, f := range got {
		if f.Kind != scan.KindRepo {
			t.Errorf("%+v", f)
		}
	}
}

// BenchmarkWalk10k walks 10 000 folders holding 200 repositories (target:
// under a second per operation).
func BenchmarkWalk10k(b *testing.B) {
	root, err := filepath.EvalSymlinks(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		for j := 0; j < 100; j++ {
			d := filepath.Join(root, fmt.Sprintf("g%03d", i), fmt.Sprintf("p%03d", j))
			if i < 2 {
				mkRepo(b, d, "repo")
			} else {
				mkdir(b, d)
			}
		}
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		st, err := scan.Walk(context.Background(), root, scan.Options{MaxDirs: 20000}, nil)
		if err != nil || st.Repos != 200 {
			b.Fatalf("%+v %v", st, err)
		}
	}
}

package installsh

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

const (
	marker     = "# added by gitid installer"
	defaultVer = "0.1.0"
)

var platforms = []string{"darwin_amd64", "darwin_arm64", "linux_amd64", "linux_arm64"}

// fixture is one isolated world: temp HOME, TMPDIR, INSTALL_DIR, a system bin
// directory that does not exist unless a test creates it, a fake uname, and a
// local server that imitates the GitHub release URLs.
type fixture struct {
	t    *testing.T
	root string
	home string
	tmp  string
	bin  string // default INSTALL_DIR (exists, empty)
	sys  string // GITID_SYSTEM_BIN_DIR (does NOT exist by default)
	fake string // directory holding the fake uname

	srv *httptest.Server

	mu     sync.Mutex
	log    []string
	files  map[string][]byte
	latest string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is for macOS and Linux")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh available")
	}
	root := t.TempDir()
	f := &fixture{
		t:      t,
		root:   root,
		home:   filepath.Join(root, "home"),
		tmp:    filepath.Join(root, "tmp"),
		bin:    filepath.Join(root, "bin"),
		sys:    filepath.Join(root, "sys"),
		fake:   filepath.Join(root, "fake"),
		files:  map[string][]byte{},
		latest: "/releases/tag/v" + defaultVer,
	}
	for _, d := range []string{f.home, f.tmp, f.bin, f.fake} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.srv = httptest.NewServer(f)
	t.Cleanup(f.srv.Close)
	f.publish(defaultVer)
	return f
}

func (f *fixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.log = append(f.log, r.Method+" "+r.URL.Path)
	data, ok := f.files[r.URL.Path]
	latest := f.latest
	f.mu.Unlock()

	switch {
	case r.URL.Path == "/releases/latest":
		if latest == "" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, latest, http.StatusFound)
	case r.URL.Path == "/releases" || strings.HasPrefix(r.URL.Path, "/releases/tag/"):
		w.WriteHeader(http.StatusOK)
	case ok:
		_, _ = w.Write(data)
	default:
		http.NotFound(w, r)
	}
}

func (f *fixture) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.log...)
}

func (f *fixture) requested(substr string) bool {
	for _, l := range f.requests() {
		if strings.Contains(l, substr) {
			return true
		}
	}
	return false
}

func (f *fixture) setFile(path string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if data == nil {
		delete(f.files, path)
		return
	}
	f.files[path] = data
}

func archiveName(ver, platform string) string {
	return fmt.Sprintf("gitid_%s_%s.tar.gz", ver, platform)
}

func downloadPath(ver, name string) string {
	return "/releases/download/v" + ver + "/" + name
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func fakeGitid(ver string) string {
	return "#!/bin/sh\necho \"gitid " + ver + "\"\n"
}

func tarGz(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		body := entries[n]
		mode := int64(0o644)
		if n == "gitid" {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: mode, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// publish serves a complete, correct release for ver: four archives and checksums.txt.
func (f *fixture) publish(ver string) {
	var lines []string
	for _, p := range platforms {
		a := tarGz(f.t, map[string]string{
			"gitid":     fakeGitid(ver),
			"LICENSE":   "MIT\n",
			"README.md": "readme\n",
		})
		name := archiveName(ver, p)
		f.setFile(downloadPath(ver, name), a)
		lines = append(lines, sum(a)+"  "+name)
	}
	sort.Strings(lines)
	f.setFile(downloadPath(ver, "checksums.txt"), []byte(strings.Join(lines, "\n")+"\n"))
}

// opts tunes one run of install.sh.
type opts struct {
	uname      string            // fake `uname -s`, default Linux
	arch       string            // fake `uname -m`, default x86_64
	shell      string            // $SHELL, default /bin/zsh
	noShell    bool              // SHELL is empty
	noInstDir  bool              // do not set INSTALL_DIR
	path       string            // replaces the whole PATH when set
	pathAppend string            // extra PATH entry
	env        map[string]string // extra or overriding variables
	interp     []string          // interpreter command, default {"sh"}
	keepTmp    bool              // do not assert that TMPDIR is empty afterwards
}

type result struct {
	stdout string
	stderr string
	code   int
}

func scriptPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the test file")
	}
	p, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", "..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (f *fixture) writeUname(osName, arch string) {
	f.t.Helper()
	script := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\n  -m) echo '%s' ;;\n  *) echo '%s' ;;\nesac\n", arch, osName)
	if err := os.WriteFile(filepath.Join(f.fake, "uname"), []byte(script), 0o755); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) environ(o opts) []string {
	pathVal := f.fake + ":/usr/bin:/bin:/usr/sbin:/sbin"
	if o.path != "" {
		pathVal = o.path
	}
	if o.pathAppend != "" {
		pathVal += ":" + o.pathAppend
	}
	m := map[string]string{
		"HOME":                 f.home,
		"TMPDIR":               f.tmp,
		"PATH":                 pathVal,
		"LC_ALL":               "C",
		"GITID_BASE_URL":       f.srv.URL + "/releases",
		"GITID_SYSTEM_BIN_DIR": f.sys,
	}
	if !o.noInstDir {
		m["INSTALL_DIR"] = f.bin
	}
	// noShell means "SHELL has no value". It is set to the empty string rather than
	// removed, because bash (macOS /bin/sh) fills in an unset SHELL from the
	// password database, which would make the test depend on the machine.
	sh := o.shell
	if sh == "" {
		sh = "/bin/zsh"
	}
	if o.noShell {
		sh = ""
	}
	m["SHELL"] = sh
	for k, v := range o.env {
		m[k] = v
	}
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

func (f *fixture) run(o opts) result {
	f.t.Helper()
	uname, arch := o.uname, o.arch
	if uname == "" {
		uname = "Linux"
	}
	if arch == "" {
		arch = "x86_64"
	}
	f.writeUname(uname, arch)

	interp := o.interp
	if len(interp) == 0 {
		interp = []string{"sh"}
	}
	bin, err := exec.LookPath(interp[0])
	if err != nil {
		f.t.Skipf("%s not available", interp[0])
	}
	args := append(append([]string(nil), interp[1:]...), scriptPath(f.t))
	cmd := exec.Command(bin, args...)
	cmd.Dir = f.root
	cmd.Env = f.environ(o)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	code := 0
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			f.t.Fatalf("running install.sh: %v", err)
		}
		code = ee.ExitCode()
	}
	r := result{stdout: so.String(), stderr: se.String(), code: code}
	if !o.keepTmp {
		if left := listTree(f.t, f.tmp); len(left) != 0 {
			f.t.Errorf("temporary files left behind in TMPDIR: %v", left)
		}
	}
	return r
}

func (f *fixture) mustRun(o opts) result {
	f.t.Helper()
	r := f.run(o)
	if r.code != 0 {
		f.t.Fatalf("install.sh failed (exit %d)\nstdout:\n%s\nstderr:\n%s", r.code, r.stdout, r.stderr)
	}
	return r
}

func (f *fixture) mustFail(o opts) result {
	f.t.Helper()
	r := f.run(o)
	if r.code == 0 {
		f.t.Fatalf("install.sh succeeded but should have failed\nstdout:\n%s\nstderr:\n%s", r.stdout, r.stderr)
	}
	return r
}

func listTree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		out = append(out, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func contains(t *testing.T, what, s, want string) {
	t.Helper()
	if !strings.Contains(s, want) {
		t.Errorf("%s does not contain %q:\n%s", what, want, s)
	}
}

func rcLine(dir string) string {
	return `export PATH="` + dir + `:$PATH"  ` + marker
}

// ---------------------------------------------------------------- positive

func TestT01LatestResolution(t *testing.T) {
	f := newFixture(t)
	r := f.mustRun(opts{})
	gitid := filepath.Join(f.bin, "gitid")
	info, err := os.Stat(gitid)
	if err != nil {
		t.Fatalf("gitid not installed: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755", info.Mode().Perm())
	}
	contains(t, "stdout", r.stdout, "gitid 0.1.0")
	if !f.requested("GET /releases/download/v0.1.0/gitid_0.1.0_linux_amd64.tar.gz") {
		t.Errorf("archive not requested: %v", f.requests())
	}
}

func TestT02ExplicitVersion(t *testing.T) {
	for _, v := range []string{"0.1.0", "v0.1.0"} {
		t.Run(v, func(t *testing.T) {
			f := newFixture(t)
			f.mustRun(opts{env: map[string]string{"VERSION": v}})
			if !f.requested("GET /releases/download/v0.1.0/gitid_0.1.0_linux_amd64.tar.gz") {
				t.Errorf("archive not requested: %v", f.requests())
			}
			if f.requested("/releases/latest") {
				t.Errorf("/latest must not be requested when VERSION is set: %v", f.requests())
			}
		})
	}
}

func TestT03DefaultDirFallback(t *testing.T) {
	f := newFixture(t)
	f.mustRun(opts{noInstDir: true})
	if !exists(filepath.Join(f.home, ".local", "bin", "gitid")) {
		t.Errorf("expected HOME/.local/bin/gitid; tree: %v", listTree(t, f.home))
	}
}

func TestT04DefaultDirSystem(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(f.sys, 0o755); err != nil {
		t.Fatal(err)
	}
	f.mustRun(opts{noInstDir: true})
	if !exists(filepath.Join(f.sys, "gitid")) {
		t.Errorf("expected gitid in the system dir; tree: %v", listTree(t, f.sys))
	}
	if exists(filepath.Join(f.home, ".local")) {
		t.Error("HOME/.local must not be created when the system dir is used")
	}
}

func TestT05PlatformMatrix(t *testing.T) {
	cases := []struct{ uname, arch, platform string }{
		{"Darwin", "x86_64", "darwin_amd64"},
		{"Darwin", "arm64", "darwin_arm64"},
		{"Linux", "x86_64", "linux_amd64"},
		{"Linux", "aarch64", "linux_arm64"},
		{"Linux", "amd64", "linux_amd64"},
	}
	for _, c := range cases {
		t.Run(c.uname+"/"+c.arch, func(t *testing.T) {
			f := newFixture(t)
			f.mustRun(opts{uname: c.uname, arch: c.arch})
			want := "GET /releases/download/v0.1.0/" + archiveName(defaultVer, c.platform)
			if !f.requested(want) {
				t.Errorf("want request %q, got %v", want, f.requests())
			}
		})
	}
}

func TestT06Upgrade(t *testing.T) {
	f := newFixture(t)
	old := filepath.Join(f.bin, "gitid")
	if err := os.WriteFile(old, []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.mustRun(opts{})
	if got := readFile(t, old); !strings.Contains(got, "gitid 0.1.0") {
		t.Errorf("binary not replaced: %q", got)
	}
	for _, p := range listTree(t, f.bin) {
		if strings.HasPrefix(p, ".gitid.tmp") {
			t.Errorf("temporary file left behind: %s", p)
		}
	}
}

// ---------------------------------------------------------------- PATH / rc

func homeBin(f *fixture) string {
	return filepath.Join(f.home, ".local", "bin")
}

func TestT10T11ZshRcAndIdempotency(t *testing.T) {
	f := newFixture(t)
	o := opts{shell: "/bin/zsh", env: map[string]string{"INSTALL_DIR": homeBin(f)}}
	r := f.mustRun(o)
	rc := filepath.Join(f.home, ".zshrc")
	got := readFile(t, rc)
	if n := strings.Count(got, marker); n != 1 {
		t.Fatalf("marker count = %d, want 1:\n%s", n, got)
	}
	if want := `export PATH="$HOME/.local/bin:$PATH"  ` + marker + "\n"; got != want {
		t.Errorf("rc = %q, want %q", got, want)
	}
	contains(t, "stdout", r.stdout, rc)
	contains(t, "stdout", r.stdout, "Open a new terminal")

	r2 := f.mustRun(o)
	if got2 := readFile(t, rc); got2 != got {
		t.Errorf("second run changed the rc file:\n%s", got2)
	}
	contains(t, "second stdout", r2.stdout, "already configured")
}

func TestT12Zdotdir(t *testing.T) {
	f := newFixture(t)
	zdot := filepath.Join(f.root, "zdot")
	if err := os.MkdirAll(zdot, 0o755); err != nil {
		t.Fatal(err)
	}
	f.mustRun(opts{shell: "/bin/zsh", env: map[string]string{"ZDOTDIR": zdot, "INSTALL_DIR": homeBin(f)}})
	if !strings.Contains(readFile(t, filepath.Join(zdot, ".zshrc")), marker) {
		t.Error("ZDOTDIR/.zshrc not written")
	}
	if exists(filepath.Join(f.home, ".zshrc")) {
		t.Error("HOME/.zshrc must not be written when ZDOTDIR is set")
	}
}

func TestT13BashRc(t *testing.T) {
	cases := []struct{ uname, file string }{
		{"Linux", ".bashrc"},
		{"Darwin", ".bash_profile"},
	}
	for _, c := range cases {
		t.Run(c.uname, func(t *testing.T) {
			f := newFixture(t)
			f.mustRun(opts{uname: c.uname, shell: "/bin/bash", env: map[string]string{"INSTALL_DIR": homeBin(f)}})
			if !strings.Contains(readFile(t, filepath.Join(f.home, c.file)), marker) {
				t.Errorf("%s not written; home tree: %v", c.file, listTree(t, f.home))
			}
		})
	}
}

func TestT14Fish(t *testing.T) {
	f := newFixture(t)
	o := opts{shell: "/usr/bin/fish", env: map[string]string{"INSTALL_DIR": homeBin(f)}}
	f.mustRun(o)
	rc := filepath.Join(f.home, ".config", "fish", "conf.d", "gitid.fish")
	got := readFile(t, rc)
	contains(t, "gitid.fish", got, "fish_add_path")
	contains(t, "gitid.fish", got, "$HOME/.local/bin")
	f.mustRun(o)
	if again := readFile(t, rc); again != got {
		t.Errorf("second run changed the fish file:\n%s", again)
	}
}

func TestT15ProfileFallback(t *testing.T) {
	cases := []struct {
		name string
		o    opts
	}{
		{"unset", opts{noShell: true}},
		{"ksh", opts{shell: "/bin/ksh"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			c.o.env = map[string]string{"INSTALL_DIR": homeBin(f)}
			f.mustRun(c.o)
			if !strings.Contains(readFile(t, filepath.Join(f.home, ".profile")), marker) {
				t.Errorf(".profile not written; tree: %v", listTree(t, f.home))
			}
		})
	}
}

func TestT16NoModifyPath(t *testing.T) {
	f := newFixture(t)
	r := f.mustRun(opts{env: map[string]string{"NO_MODIFY_PATH": "1"}})
	if tree := listTree(t, f.home); len(tree) != 0 {
		t.Errorf("HOME must stay untouched, found %v", tree)
	}
	contains(t, "stdout", r.stdout, `export PATH="`+f.bin+`:$PATH"`)
	if strings.Contains(r.stdout, "Open a new terminal") {
		t.Errorf("must not claim to have edited a file:\n%s", r.stdout)
	}
}

func TestT17DirAlreadyOnPath(t *testing.T) {
	f := newFixture(t)
	r := f.mustRun(opts{pathAppend: f.bin})
	if tree := listTree(t, f.home); len(tree) != 0 {
		t.Errorf("HOME must stay untouched, found %v", tree)
	}
	if strings.Contains(r.stdout, "Open a new terminal") {
		t.Errorf("unexpected PATH message:\n%s", r.stdout)
	}
}

func TestT18T19ExistingRcPreserved(t *testing.T) {
	f := newFixture(t)
	rc := filepath.Join(f.home, ".zshrc")
	orig := "alias ll='ls -l'\nexport EDITOR=vi"
	if err := os.WriteFile(rc, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	f.mustRun(opts{env: map[string]string{"INSTALL_DIR": homeBin(f)}})
	want := orig + "\n" + rcLine("$HOME/.local/bin") + "\n"
	if got := readFile(t, rc); got != want {
		t.Errorf("rc = %q, want %q", got, want)
	}

	// With a trailing newline already present nothing extra is inserted.
	f2 := newFixture(t)
	rc2 := filepath.Join(f2.home, ".zshrc")
	if err := os.WriteFile(rc2, []byte(orig+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f2.mustRun(opts{env: map[string]string{"INSTALL_DIR": homeBin(f2)}})
	if got, want := readFile(t, rc2), orig+"\n"+rcLine("$HOME/.local/bin")+"\n"; got != want {
		t.Errorf("rc = %q, want %q", got, want)
	}
}

// ------------------------------------------------------------------ failure

func TestT20ChecksumMismatch(t *testing.T) {
	f := newFixture(t)
	var lines []string
	for _, p := range platforms {
		lines = append(lines, strings.Repeat("0", 64)+"  "+archiveName(defaultVer, p))
	}
	f.setFile(downloadPath(defaultVer, "checksums.txt"), []byte(strings.Join(lines, "\n")+"\n"))
	r := f.mustFail(opts{})
	contains(t, "stderr", r.stderr, "checksum")
	if tree := listTree(t, f.bin); len(tree) != 0 {
		t.Errorf("nothing may be installed, found %v", tree)
	}
}

func TestT21MissingChecksumEntry(t *testing.T) {
	f := newFixture(t)
	f.setFile(downloadPath(defaultVer, "checksums.txt"), []byte(strings.Repeat("a", 64)+"  other.tar.gz\n"))
	r := f.mustFail(opts{})
	contains(t, "stderr", r.stderr, "no checksum entry")
	if tree := listTree(t, f.bin); len(tree) != 0 {
		t.Errorf("nothing may be installed, found %v", tree)
	}
}

func TestT22ChecksumsNotFound(t *testing.T) {
	f := newFixture(t)
	f.setFile(downloadPath(defaultVer, "checksums.txt"), nil)
	r := f.mustFail(opts{})
	contains(t, "stderr", r.stderr, "checksums.txt")
	if tree := listTree(t, f.bin); len(tree) != 0 {
		t.Errorf("nothing may be installed, found %v", tree)
	}
}

func TestT23ArchiveNotFound(t *testing.T) {
	f := newFixture(t)
	r := f.mustFail(opts{env: map[string]string{"VERSION": "9.9.9"}})
	contains(t, "stderr", r.stderr, "9.9.9")
	contains(t, "stderr", r.stderr, "gitid_9.9.9_linux_amd64.tar.gz")
	if tree := listTree(t, f.bin); len(tree) != 0 {
		t.Errorf("nothing may be installed, found %v", tree)
	}
}

func TestT24UnsupportedArch(t *testing.T) {
	for _, arch := range []string{"i686", "armv7l"} {
		t.Run(arch, func(t *testing.T) {
			f := newFixture(t)
			r := f.mustFail(opts{arch: arch})
			contains(t, "stderr", r.stderr, "unsupported architecture")
			if f.requested("/download/") {
				t.Errorf("no archive may be requested: %v", f.requests())
			}
		})
	}
}

func TestT25UnsupportedOS(t *testing.T) {
	for _, name := range []string{"FreeBSD", "MINGW64_NT-10.0"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			r := f.mustFail(opts{uname: name})
			contains(t, "stderr", r.stderr, "unsupported operating system")
			if f.requested("/download/") {
				t.Errorf("no archive may be requested: %v", f.requests())
			}
		})
	}
}

func TestT26LatestNotATag(t *testing.T) {
	f := newFixture(t)
	f.mu.Lock()
	f.latest = "/releases"
	f.mu.Unlock()
	r := f.mustFail(opts{})
	contains(t, "stderr", r.stderr, "VERSION")
	if f.requested("/download/") {
		t.Errorf("no archive may be requested: %v", f.requests())
	}
}

func TestT27ArchiveWithoutBinary(t *testing.T) {
	f := newFixture(t)
	a := tarGz(t, map[string]string{"README.md": "x\n"})
	name := archiveName(defaultVer, "linux_amd64")
	f.setFile(downloadPath(defaultVer, name), a)
	f.setFile(downloadPath(defaultVer, "checksums.txt"), []byte(sum(a)+"  "+name+"\n"))
	r := f.mustFail(opts{})
	contains(t, "stderr", r.stderr, "gitid binary")
	if tree := listTree(t, f.bin); len(tree) != 0 {
		t.Errorf("nothing may be installed, found %v", tree)
	}
}

func TestT28CorruptArchive(t *testing.T) {
	f := newFixture(t)
	a := []byte("this is not a gzip archive at all, just bytes")
	name := archiveName(defaultVer, "linux_amd64")
	f.setFile(downloadPath(defaultVer, name), a)
	f.setFile(downloadPath(defaultVer, "checksums.txt"), []byte(sum(a)+"  "+name+"\n"))
	r := f.mustFail(opts{})
	contains(t, "stderr", r.stderr, "extract")
	if tree := listTree(t, f.bin); len(tree) != 0 {
		t.Errorf("nothing may be installed, found %v", tree)
	}
}

func TestT29UnwritableInstallDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	f := newFixture(t)
	ro := filepath.Join(f.root, "ro")
	if err := os.MkdirAll(ro, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	r := f.mustFail(opts{env: map[string]string{"INSTALL_DIR": ro}})
	contains(t, "stderr", r.stderr, "INSTALL_DIR")
	contains(t, "stderr", r.stderr, "permission")
	if tree := listTree(t, ro); len(tree) != 0 {
		t.Errorf("nothing may be installed, found %v", tree)
	}
}

// TestT32NoDirectoryLeftOnFailure checks that a failed install does not leave
// the directories it would have created (INSTALL_DIR levels or ~/.local/bin).
func TestT32NoDirectoryLeftOnFailure(t *testing.T) {
	bad := func(f *fixture) {
		f.setFile(downloadPath(defaultVer, "checksums.txt"),
			[]byte(strings.Repeat("0", 64)+"  "+archiveName(defaultVer, "linux_amd64")+"\n"))
	}
	t.Run("nested INSTALL_DIR, checksum mismatch", func(t *testing.T) {
		f := newFixture(t)
		bad(f)
		top := filepath.Join(f.root, "new")
		f.mustFail(opts{env: map[string]string{"INSTALL_DIR": filepath.Join(top, "sub", "bin")}})
		if exists(top) {
			t.Errorf("INSTALL_DIR levels were left behind: %v", listTree(t, top))
		}
	})
	t.Run("default dir, archive 404", func(t *testing.T) {
		f := newFixture(t)
		f.mustFail(opts{noInstDir: true, env: map[string]string{"VERSION": "9.9.9"}})
		if tree := listTree(t, f.home); len(tree) != 0 {
			t.Errorf("HOME must stay untouched, found %v", tree)
		}
	})
	t.Run("corrupt archive", func(t *testing.T) {
		f := newFixture(t)
		a := []byte("not gzip")
		name := archiveName(defaultVer, "linux_amd64")
		f.setFile(downloadPath(defaultVer, name), a)
		f.setFile(downloadPath(defaultVer, "checksums.txt"), []byte(sum(a)+"  "+name+"\n"))
		dir := filepath.Join(f.root, "never")
		f.mustFail(opts{env: map[string]string{"INSTALL_DIR": dir}})
		if exists(dir) {
			t.Error("INSTALL_DIR must not be created when the install fails")
		}
	})
	t.Run("success still creates nested INSTALL_DIR", func(t *testing.T) {
		f := newFixture(t)
		dir := filepath.Join(f.root, "a", "b", "bin")
		f.mustRun(opts{env: map[string]string{"INSTALL_DIR": dir}})
		if !exists(filepath.Join(dir, "gitid")) {
			t.Error("gitid not installed into the nested directory")
		}
	})
}

// A not-yet-existing INSTALL_DIR under an unwritable parent fails early and creates nothing.
func TestT32bUnwritableParent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	f := newFixture(t)
	ro := filepath.Join(f.root, "ro")
	if err := os.MkdirAll(ro, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	r := f.mustFail(opts{env: map[string]string{"INSTALL_DIR": filepath.Join(ro, "x", "bin")}})
	contains(t, "stderr", r.stderr, "INSTALL_DIR")
	if tree := listTree(t, ro); len(tree) != 0 {
		t.Errorf("nothing may be created, found %v", tree)
	}
}

func TestT30NoHashTool(t *testing.T) {
	tools := t.TempDir()
	required := []string{"curl", "tar", "gzip", "mktemp", "awk", "tr", "cp", "chmod", "mv", "rm", "mkdir", "dirname", "grep", "tail"}
	optional := []string{"sysctl", "xattr"}
	for _, n := range append(append([]string(nil), required...), optional...) {
		p, err := exec.LookPath(n)
		if err != nil {
			if contains2(required, n) {
				t.Skipf("cannot build a restricted PATH: %s not found", n)
			}
			continue
		}
		if err := os.Symlink(p, filepath.Join(tools, n)); err != nil {
			t.Skipf("cannot create symlinks: %v", err)
		}
	}
	f := newFixture(t)
	r := f.mustFail(opts{path: f.fake + ":" + tools})
	contains(t, "stderr", r.stderr, "sha256sum or shasum")
	if tree := listTree(t, f.bin); len(tree) != 0 {
		t.Errorf("never install without verification, found %v", tree)
	}
}

func contains2(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ----------------------------------------------------------------- boundary

func TestT40Prerelease(t *testing.T) {
	f := newFixture(t)
	f.publish("0.2.0-rc.1")
	f.mustRun(opts{env: map[string]string{"VERSION": "0.2.0-rc.1"}})
	want := "GET /releases/download/v0.2.0-rc.1/gitid_0.2.0-rc.1_linux_amd64.tar.gz"
	if !f.requested(want) {
		t.Errorf("want request %q, got %v", want, f.requests())
	}
}

func TestT41InstallDirWithSpace(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.root, "my bin")
	f.mustRun(opts{shell: "/bin/zsh", env: map[string]string{"INSTALL_DIR": dir}})
	rc := filepath.Join(f.home, ".zshrc")
	got := readFile(t, rc)
	if want := rcLine(dir) + "\n"; got != want {
		t.Fatalf("rc = %q, want %q", got, want)
	}
	cmd := exec.Command("sh", "-c", `. "$1" && gitid version`, "sh", rc)
	cmd.Env = []string{"HOME=" + f.home, "PATH=/usr/bin:/bin"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sourcing the rc file did not put gitid on PATH: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "gitid 0.1.0" {
		t.Errorf("unexpected output %q", out)
	}
}

func TestT42HomePrefixCollision(t *testing.T) {
	f := newFixture(t)
	dir := f.home + "2/bin"
	f.mustRun(opts{shell: "/bin/zsh", env: map[string]string{"INSTALL_DIR": dir}})
	got := readFile(t, filepath.Join(f.home, ".zshrc"))
	if want := rcLine(dir) + "\n"; got != want {
		t.Errorf("rc = %q, want %q", got, want)
	}
	if strings.Contains(got, "$HOME") {
		t.Errorf("a directory outside HOME must not be rewritten relative to HOME: %q", got)
	}
}

func TestT43Interpreters(t *testing.T) {
	interps := map[string][]string{
		"sh":         {"sh"},
		"dash":       {"dash"},
		"bash-posix": {"bash", "--posix"},
	}
	for name, interp := range interps {
		t.Run(name, func(t *testing.T) {
			// T01
			f := newFixture(t)
			r := f.mustRun(opts{interp: interp})
			contains(t, "stdout", r.stdout, "gitid 0.1.0")

			// T10
			f = newFixture(t)
			f.mustRun(opts{interp: interp, env: map[string]string{"INSTALL_DIR": homeBin(f)}})
			if got := readFile(t, filepath.Join(f.home, ".zshrc")); got != rcLine("$HOME/.local/bin")+"\n" {
				t.Errorf("rc = %q", got)
			}

			// T20
			f = newFixture(t)
			f.setFile(downloadPath(defaultVer, "checksums.txt"),
				[]byte(strings.Repeat("0", 64)+"  "+archiveName(defaultVer, "linux_amd64")+"\n"))
			r = f.mustFail(opts{interp: interp})
			contains(t, "stderr", r.stderr, "checksum")
		})
	}
}

// ------------------------------------------------------------------- static

// TestT55StaticChecks inspects install.sh for properties that must always hold.
func TestT55StaticChecks(t *testing.T) {
	raw, err := os.ReadFile(scriptPath(t))
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	if !strings.HasPrefix(src, "#!/bin/sh\n") {
		t.Error("first line must be #!/bin/sh")
	}
	var code []string
	for _, l := range strings.Split(src, "\n") {
		trim := strings.TrimSpace(l)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		code = append(code, trim)
	}
	if len(code) == 0 || code[len(code)-1] != `main "$@"` {
		t.Errorf(`last executable line must be main "$@", got %q`, code[len(code)-1])
	}
	body := strings.Join(code, "\n")
	for _, bad := range []string{"sudo", "jq ", "api.github.com", "[[", "set -x"} {
		if strings.Contains(body, bad) {
			t.Errorf("install.sh must not contain %q", bad)
		}
	}
	if !regexp.MustCompile(`(?m)^set -eu$`).MatchString(body) {
		t.Error("set -eu missing")
	}
	if regexp.MustCompile(`(^|[;&|(]\s*)(source|local|declare|typeset)\s`).MatchString(body) {
		t.Error("bash-only builtins (source, local, declare, typeset) are not allowed")
	}
	// Top-level code must be only assignments, function definitions and the final call.
	top := regexp.MustCompile(`(?m)^[a-z_]+\(\) \{$`)
	if len(top.FindAllString(body, -1)) < 5 {
		t.Error("expected the script body to live in functions")
	}
}

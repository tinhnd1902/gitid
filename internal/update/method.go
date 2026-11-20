package update

import (
	"path/filepath"
	"strings"
)

// method detects how gitid was installed from the path of the running binary
// (symlinks resolved): Homebrew keeps it under a Caskroom or Cellar folder,
// install.sh puts it in ~/.local/bin or /usr/local/bin.
func (c Checker) method() Method {
	if c.Executable == nil {
		return MethodUnknown
	}
	p, err := c.Executable()
	if err != nil || p == "" {
		return MethodUnknown
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	return classify(p, c.Env.Home)
}

// classify maps the path of the binary to an install method.
func classify(path, home string) Method {
	path = filepath.ToSlash(filepath.Clean(path))
	if strings.Contains(path, "/Caskroom/gitid/") || strings.Contains(path, "/Cellar/gitid/") {
		return MethodBrew
	}
	dir := filepath.ToSlash(filepath.Dir(path))
	if dir == "/usr/local/bin" || (home != "" && dir == filepath.ToSlash(filepath.Join(home, ".local", "bin"))) {
		return MethodScript
	}
	return MethodUnknown
}

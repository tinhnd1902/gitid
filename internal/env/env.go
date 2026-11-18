// Package env holds the injectable environment (home directory, XDG dir,
// clock, OS name, PATH lookup) so that no other package reads the process
// environment directly.
package env

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Env is the injectable environment used everywhere in gitid.
type Env struct {
	Home          string
	XDGConfigHome string
	Now           func() time.Time
	GOOS          string
	LookPath      func(file string) (string, error)
	// LookupEnv reads one variable of the process environment. Code that
	// needs a variable other than HOME / XDG_CONFIG_HOME goes through it, so
	// tests can inject a map instead of the real environment.
	LookupEnv func(key string) (string, bool)
}

// New returns an Env with sane defaults for the optional fields.
func New(home, xdg string) Env {
	return Env{Home: home, XDGConfigHome: xdg}.withDefaults()
}

func (e Env) withDefaults() Env {
	if e.Now == nil {
		e.Now = time.Now
	}
	if e.GOOS == "" {
		e.GOOS = runtime.GOOS
	}
	if e.LookPath == nil {
		e.LookPath = exec.LookPath
	}
	if e.LookupEnv == nil {
		e.LookupEnv = os.LookupEnv
	}
	return e
}

// Paths holds every path gitid reads or writes. All are absolute.
type Paths struct {
	ConfigDir       string // $XDG_CONFIG_HOME/gitid
	ConfigFile      string // ConfigDir/config.yaml
	KeyBindingsFile string // ConfigDir/keys.yaml (optional; gitid only reads it)
	GitDir          string // ConfigDir/git (generated gitconfig files)
	BackupsDir      string // ConfigDir/backups
	LockFile        string // ConfigDir/.lock
	GitConfig       string // ~/.gitconfig
	SSHDir          string // ~/.ssh
	SSHConfig       string // ~/.ssh/config
	SSHConfigD      string // ~/.ssh/config.d
	SSHGitidConf    string // ~/.ssh/config.d/gitid.conf
	KeysDir         string // ~/.ssh/gitid
}

// xdgBase returns the XDG config base: XDGConfigHome when absolute, else
// $HOME/.config.
func (e Env) xdgBase() string {
	if e.XDGConfigHome != "" && filepath.IsAbs(e.XDGConfigHome) {
		return filepath.Clean(e.XDGConfigHome)
	}
	return filepath.Join(e.Home, ".config")
}

// Paths computes every well-known path from the injected environment.
func (e Env) Paths() Paths {
	cfg := filepath.Join(e.xdgBase(), "gitid")
	ssh := filepath.Join(e.Home, ".ssh")
	return Paths{
		ConfigDir:       cfg,
		ConfigFile:      filepath.Join(cfg, "config.yaml"),
		KeyBindingsFile: filepath.Join(cfg, "keys.yaml"),
		GitDir:          filepath.Join(cfg, "git"),
		BackupsDir:      filepath.Join(cfg, "backups"),
		LockFile:        filepath.Join(cfg, ".lock"),
		GitConfig:       filepath.Join(e.Home, ".gitconfig"),
		SSHDir:          ssh,
		SSHConfig:       filepath.Join(ssh, "config"),
		SSHConfigD:      filepath.Join(ssh, "config.d"),
		SSHGitidConf:    filepath.Join(ssh, "config.d", "gitid.conf"),
		KeysDir:         filepath.Join(ssh, "gitid"),
	}
}

// Expand turns "~" and "~/x" into absolute paths under Home. Other inputs
// are returned cleaned (relative paths stay relative).
func (e Env) Expand(p string) string {
	switch {
	case p == "~":
		return e.Home
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(e.Home, p[2:])
	case p == "":
		return ""
	}
	return filepath.Clean(p)
}

// Contract turns an absolute path under Home into "~/x" form (with a
// trailing slash preserved). Paths outside Home are returned unchanged.
func (e Env) Contract(abs string) string {
	if e.Home == "" {
		return abs
	}
	trail := strings.HasSuffix(abs, "/") && abs != "/"
	c := filepath.Clean(abs)
	var out string
	switch {
	case c == e.Home:
		out = "~"
	case strings.HasPrefix(c, e.Home+"/"):
		out = "~/" + c[len(e.Home)+1:]
	default:
		return abs
	}
	if trail && out != "~" {
		out += "/"
	}
	return out
}

// IsUnder reports whether path is equal to or inside dir (both cleaned).
func IsUnder(path, dir string) bool {
	p, d := filepath.Clean(path), filepath.Clean(dir)
	return p == d || strings.HasPrefix(p, d+string(filepath.Separator))
}

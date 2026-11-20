// Command gitid manages many Git identities (companies and accounts).
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/x/term"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/cli"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/tui"
)

// version is set with -ldflags "-X main.version=...".
var version = "dev"

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func main() {
	e, err := env.FromOS()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gitid:", err)
		os.Exit(cli.ExitError)
	}
	code := cli.Execute(cli.Deps{
		Env:     e,
		Runner:  execx.OSRunner{},
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		IsTTY:   func() bool { return isTerminal(os.Stdin) && isTerminal(os.Stdout) },
		Cwd:     os.Getwd,
		Version: version,
		// the TUI names the command that upgrades this install
		Executable: os.Executable,
		ReadSecret: func() (string, error) {
			b, err := term.ReadPassword(os.Stdin.Fd())
			return string(b), err
		},
		StartTUI: func(svc *app.Service) error {
			if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
				return errors.New(i18n.T("cli.tui.needs_tty"))
			}
			cwd, _ := os.Getwd()
			return tui.Run(svc, tui.Options{Cwd: cwd, Version: version, CheckUpdates: true})
		},
		Lang: os.Getenv("GITID_LANG"),
	}, os.Args[1:])
	os.Exit(code)
}

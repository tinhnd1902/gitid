// Package cli implements the gitid command line on top of internal/app.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/env"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/i18n"
)

// Exit codes.
const (
	ExitOK      = 0
	ExitError   = 1
	ExitProblem = 2 // check/doctor found problems
)

// Deps are the injectable dependencies of the CLI.
type Deps struct {
	Env     env.Env
	Runner  execx.Runner
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	IsTTY   func() bool // stdin and stdout are terminals
	Cwd     func() (string, error)
	Version string
	// StartTUI is called for `gitid` without arguments. When nil a short
	// notice is printed.
	StartTUI func(svc *app.Service) error
	// Lang overrides the configured language (GITID_LANG).
	Lang string
}

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return fmt.Sprintf("exit %d", e.code)
}
func (e *exitError) Unwrap() error { return e.err }

// problem makes a command exit with ExitProblem without printing an error.
func problem() error { return &exitError{code: ExitProblem} }

type cli struct {
	d   Deps
	svc *app.Service
	out io.Writer
	err io.Writer
}

func (c *cli) printf(format string, args ...any) { fmt.Fprintf(c.out, format, args...) }
func (c *cli) println(args ...any)               { fmt.Fprintln(c.out, args...) }
func (c *cli) print(s string)                    { fmt.Fprint(c.out, s) }

func (c *cli) isTTY() bool { return c.d.IsTTY != nil && c.d.IsTTY() }

func (c *cli) cwd() (string, error) {
	if c.d.Cwd != nil {
		return c.d.Cwd()
	}
	return os.Getwd()
}

// confirm asks a yes/no question on a TTY. The default answer is no.
func (c *cli) confirm(prompt string) bool {
	fmt.Fprintf(c.out, "%s ", prompt)
	line, _ := bufio.NewReader(c.d.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "c", "co", "có":
		return true
	}
	return false
}

// Execute runs the CLI and returns the process exit code.
func Execute(d Deps, args []string) int {
	if d.Stdout == nil {
		d.Stdout = io.Discard
	}
	if d.Stderr == nil {
		d.Stderr = io.Discard
	}
	if d.Stdin == nil {
		d.Stdin = strings.NewReader("")
	}
	if d.Runner == nil {
		d.Runner = execx.OSRunner{}
	}
	svc := app.New(d.Env, d.Runner)
	svc.Version = d.Version
	c := &cli{d: d, svc: svc, out: d.Stdout, err: d.Stderr}
	c.setLanguage()
	root := c.root()
	root.SetArgs(args)
	root.SetOut(d.Stdout)
	root.SetErr(d.Stderr)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err := root.ExecuteContext(ctx)
	if err == nil {
		return ExitOK
	}
	var ee *exitError
	if errors.As(err, &ee) {
		if ee.err != nil {
			fmt.Fprintln(c.err, i18n.T("cli.error.prefix", c.errorMessage(ee.err)))
		}
		return ee.code
	}
	fmt.Fprintln(c.err, i18n.T("cli.error.prefix", c.errorMessage(err)))
	return ExitError
}

func (c *cli) setLanguage() {
	lang := c.d.Lang
	if lang == "" {
		if cfg, err := config.Load(c.d.Env); err == nil {
			lang = cfg.Settings.Language
		}
	}
	i18n.SetLang(lang)
}

func (c *cli) load() (config.Config, error) { return c.svc.Load() }

func (c *cli) root() *cobra.Command {
	root := &cobra.Command{
		Use:           "gitid",
		Short:         i18n.T("cli.root.short"),
		Long:          i18n.T("cli.root.long"),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if c.d.StartTUI == nil {
				c.println(i18n.T("cli.tui.soon"))
				return nil
			}
			return c.d.StartTUI(c.svc)
		},
	}
	root.AddCommand(c.listCmd(), c.checkCmd(), c.cloneCmd(), c.applyCmd(), c.doctorCmd(), c.fixRemoteCmd(), c.keyCmd(), c.keysCmd(), c.importCmd(), c.versionCmd())
	return root
}

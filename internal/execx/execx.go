// Package execx abstracts process execution. All callers go through Runner;
// unit tests use Fake, production uses OSRunner.
package execx

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Cmd describes a process to run.
type Cmd struct {
	Name  string
	Args  []string
	Dir   string
	Env   []string // extra KEY=VALUE entries appended to the process environment
	Stdin string
}

// String renders the command for logs/tests (never includes Stdin).
func (c Cmd) String() string {
	return strings.TrimSpace(c.Name + " " + strings.Join(c.Args, " "))
}

// Result is the captured outcome of a finished process.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int // -1 when the process could not be started
}

// ExitError is returned (wrapped) when a process exits non-zero.
type ExitError struct {
	Cmd  string
	Code int
}

func (e *ExitError) Error() string { return fmt.Sprintf("%s: exit status %d", e.Cmd, e.Code) }

// Runner runs external commands.
type Runner interface {
	// Run runs the command to completion, capturing stdout and stderr.
	// A non-zero exit returns the Result together with an *ExitError.
	Run(ctx context.Context, c Cmd) (Result, error)
	// Stream is like Run but calls onLine for every stderr line (lines are
	// split on '\n' and '\r', which is how git reports progress).
	Stream(ctx context.Context, c Cmd, onLine func(line string)) (Result, error)
	// Interactive returns a command the caller attaches to a terminal
	// (tea.ExecProcess or os.Stdin/Stdout/Stderr).
	Interactive(c Cmd) *exec.Cmd
	// RunTTY runs the command attached to the process stdio.
	RunTTY(ctx context.Context, c Cmd) error
}

// OSRunner runs real processes.
type OSRunner struct{}

func build(ctx context.Context, c Cmd) *exec.Cmd {
	var cmd *exec.Cmd
	if ctx != nil {
		cmd = exec.CommandContext(ctx, c.Name, c.Args...)
		cmd.WaitDelay = 2 * time.Second
	} else {
		cmd = exec.Command(c.Name, c.Args...)
	}
	cmd.Dir = c.Dir
	cmd.Env = childEnv(append(os.Environ(), c.Env...))
	if c.Stdin != "" {
		cmd.Stdin = strings.NewReader(c.Stdin)
	}
	return cmd
}

// tokenEnvPrefix starts the names of the variables that hold an account's
// Online token (GITID_TOKEN_<COMPANY>__<ACCOUNT>). gitid reads them itself and
// never hands them to a program it starts: git, gh, glab, security, open and
// pbcopy do not need them, and a helper that logged its environment would
// otherwise leak the tokens of every account.
const tokenEnvPrefix = "GITID_TOKEN_"

// childEnv returns env without any GITID_TOKEN_* variable.
func childEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(strings.ToUpper(kv), tokenEnvPrefix) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func finish(c Cmd, cmd *exec.Cmd, err error, so, se *bytes.Buffer) (Result, error) {
	res := Result{Stdout: so.String(), Stderr: se.String()}
	if err == nil {
		return res, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.ExitCode = ee.ExitCode()
		return res, fmt.Errorf("%w", &ExitError{Cmd: c.String(), Code: res.ExitCode})
	}
	res.ExitCode = -1
	return res, err
}

// Run implements Runner.
func (OSRunner) Run(ctx context.Context, c Cmd) (Result, error) {
	cmd := build(ctx, c)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	return finish(c, cmd, err, &so, &se)
}

// splitLines splits on \n or \r, dropping empty tokens.
func splitLines(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// Stream implements Runner.
func (OSRunner) Stream(ctx context.Context, c Cmd, onLine func(string)) (Result, error) {
	cmd := build(ctx, c)
	var so, se bytes.Buffer
	cmd.Stdout = &so
	pr, pw := io.Pipe()
	cmd.Stderr = io.MultiWriter(&se, pw)
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		sc.Split(splitLines)
		for sc.Scan() {
			if line := sc.Text(); line != "" && onLine != nil {
				onLine(line)
			}
		}
		_, _ = io.Copy(io.Discard, pr)
	}()
	err := cmd.Run()
	_ = pw.Close()
	<-done
	return finish(c, cmd, err, &so, &se)
}

// Interactive implements Runner.
func (OSRunner) Interactive(c Cmd) *exec.Cmd {
	cmd := build(nil, c)
	return cmd
}

// RunTTY implements Runner.
func (OSRunner) RunTTY(ctx context.Context, c Cmd) error {
	cmd := build(ctx, c)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

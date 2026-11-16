package execx

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// ErrUnscripted is returned by Fake for a command no rule matches.
var ErrUnscripted = errors.New("execx.Fake: unscripted call")

// Response scripts the outcome of a Fake call.
type Response struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error // returned as-is (process "failed to start")
	// Do runs before the response is returned, to simulate side effects
	// (for example ssh-keygen creating key files).
	Do func(Cmd) error
	// Block, when non-nil, makes the call wait until closed or ctx is done.
	Block chan struct{}
}

type rule struct {
	prefix string
	resp   Response
}

// Fake is a scripted Runner that records every call.
type Fake struct {
	mu    sync.Mutex
	rules []rule
	calls []Cmd
}

// NewFake returns an empty Fake: every call fails with ErrUnscripted until
// rules are added with On.
func NewFake() *Fake { return &Fake{} }

// On scripts a response for commands whose "name arg arg..." string starts
// with prefix. The longest matching prefix wins; later rules win ties.
func (f *Fake) On(prefix string, r Response) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rules = append(f.rules, rule{prefix: prefix, resp: r})
	return f
}

func (f *Fake) lookup(c Cmd) (Response, bool) {
	s := c.String()
	best := -1
	var resp Response
	for _, r := range f.rules {
		if strings.HasPrefix(s, r.prefix) && len(r.prefix) >= best {
			best = len(r.prefix)
			resp = r.resp
		}
	}
	return resp, best >= 0
}

func (f *Fake) record(c Cmd) (Response, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
	return f.lookup(c)
}

// Calls returns a copy of the recorded calls.
func (f *Fake) Calls() []Cmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Cmd(nil), f.calls...)
}

// CallStrings returns the recorded calls as "name args..." strings.
func (f *Fake) CallStrings() []string {
	var out []string
	for _, c := range f.Calls() {
		out = append(out, c.String())
	}
	return out
}

// Count returns how many recorded calls start with prefix.
func (f *Fake) Count(prefix string) int {
	n := 0
	for _, s := range f.CallStrings() {
		if strings.HasPrefix(s, prefix) {
			n++
		}
	}
	return n
}

func (f *Fake) exec(ctx context.Context, c Cmd) (Result, error) {
	resp, ok := f.record(c)
	if !ok {
		return Result{ExitCode: -1}, fmt.Errorf("%w: %s", ErrUnscripted, c.String())
	}
	if resp.Block != nil {
		select {
		case <-resp.Block:
		case <-ctx.Done():
			return Result{ExitCode: -1}, ctx.Err()
		}
	}
	if ctx != nil && ctx.Err() != nil {
		return Result{ExitCode: -1}, ctx.Err()
	}
	if resp.Do != nil {
		if err := resp.Do(c); err != nil {
			return Result{ExitCode: -1}, err
		}
	}
	if resp.Err != nil {
		return Result{ExitCode: -1}, resp.Err
	}
	res := Result{Stdout: resp.Stdout, Stderr: resp.Stderr, ExitCode: resp.ExitCode}
	if resp.ExitCode != 0 {
		return res, &ExitError{Cmd: c.String(), Code: resp.ExitCode}
	}
	return res, nil
}

// Run implements Runner.
func (f *Fake) Run(ctx context.Context, c Cmd) (Result, error) { return f.exec(ctx, c) }

// Stream implements Runner; scripted Stderr is delivered line by line.
func (f *Fake) Stream(ctx context.Context, c Cmd, onLine func(string)) (Result, error) {
	res, err := f.exec(ctx, c)
	if onLine != nil {
		for _, l := range strings.FieldsFunc(res.Stderr, func(r rune) bool { return r == '\n' || r == '\r' }) {
			onLine(l)
		}
	}
	return res, err
}

// Interactive implements Runner. The scripted Do hook runs immediately (so
// tests can simulate side effects) and a harmless command is returned.
func (f *Fake) Interactive(c Cmd) *exec.Cmd {
	resp, ok := f.record(c)
	if ok && resp.Do != nil {
		_ = resp.Do(c)
	}
	return exec.Command("true")
}

// RunTTY implements Runner.
func (f *Fake) RunTTY(ctx context.Context, c Cmd) error {
	_, err := f.exec(ctx, c)
	return err
}

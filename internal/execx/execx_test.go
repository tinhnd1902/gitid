package execx

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOSRunnerRun(t *testing.T) {
	var r Runner = OSRunner{}
	res, err := r.Run(context.Background(), Cmd{Name: "sh", Args: []string{"-c", "echo out; echo err >&2"}})
	if err != nil || res.Stdout != "out\n" || res.Stderr != "err\n" || res.ExitCode != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	res, err = r.Run(context.Background(), Cmd{Name: "sh", Args: []string{"-c", "exit 3"}})
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != 3 || res.ExitCode != 3 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if !strings.Contains(ee.Error(), "exit status 3") {
		t.Fatal(ee.Error())
	}
	res, err = r.Run(context.Background(), Cmd{Name: "definitely-not-a-binary-xyz"})
	if err == nil || res.ExitCode != -1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestOSRunnerEnvDirStdin(t *testing.T) {
	dir := t.TempDir()
	res, err := OSRunner{}.Run(context.Background(), Cmd{Name: "sh", Args: []string{"-c", "echo $FOO; pwd; cat"}, Dir: dir, Env: []string{"FOO=bar"}, Stdin: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Stdout, "bar\n") || !strings.HasSuffix(res.Stdout, "hello") {
		t.Fatalf("stdout %q", res.Stdout)
	}
}

func TestOSRunnerStream(t *testing.T) {
	var lines []string
	res, err := OSRunner{}.Stream(context.Background(), Cmd{Name: "sh", Args: []string{"-c", `printf 'a 1%%\ra 2%%\rdone\n' >&2; echo out`}}, func(l string) { lines = append(lines, l) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(lines, "|") != "a 1%|a 2%|done" || res.Stdout != "out\n" {
		t.Fatalf("lines=%v res=%+v", lines, res)
	}
	_, err = OSRunner{}.Stream(context.Background(), Cmd{Name: "sh", Args: []string{"-c", "echo x >&2; exit 2"}}, nil)
	if err == nil {
		t.Fatal("want error")
	}
}

func TestOSRunnerContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := OSRunner{}.Run(ctx, Cmd{Name: "sleep", Args: []string{"5"}})
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("expected quick cancel, err=%v", err)
	}
}

func TestInteractiveAndTTY(t *testing.T) {
	cmd := OSRunner{}.Interactive(Cmd{Name: "true", Env: []string{"A=b"}})
	if cmd.Path == "" || cmd.Env == nil {
		t.Fatal("bad interactive cmd")
	}
	if err := (OSRunner{}).RunTTY(context.Background(), Cmd{Name: "true"}); err != nil {
		t.Fatal(err)
	}
}

func TestFakeRulesAndRecording(t *testing.T) {
	f := NewFake().
		On("git", Response{Stdout: "generic"}).
		On("git config --get", Response{Stdout: "specific"}).
		On("fail", Response{ExitCode: 2, Stderr: "boom\rline2\n"}).
		On("err", Response{Err: errors.New("nostart")}).
		On("do", Response{Do: func(Cmd) error { return errors.New("do failed") }})
	r, _ := f.Run(context.Background(), Cmd{Name: "git", Args: []string{"config", "--get", "x"}})
	if r.Stdout != "specific" {
		t.Fatalf("longest prefix: %q", r.Stdout)
	}
	r, _ = f.Run(context.Background(), Cmd{Name: "git", Args: []string{"status"}})
	if r.Stdout != "generic" {
		t.Fatalf("%q", r.Stdout)
	}
	if _, err := f.Run(context.Background(), Cmd{Name: "nope"}); !errors.Is(err, ErrUnscripted) {
		t.Fatalf("err=%v", err)
	}
	var lines []string
	res, err := f.Stream(context.Background(), Cmd{Name: "fail"}, func(l string) { lines = append(lines, l) })
	var ee *ExitError
	if !errors.As(err, &ee) || res.ExitCode != 2 || len(lines) != 2 {
		t.Fatalf("res=%+v err=%v lines=%v", res, err, lines)
	}
	if _, err := f.Run(context.Background(), Cmd{Name: "err"}); err == nil {
		t.Fatal("want err")
	}
	if _, err := f.Run(context.Background(), Cmd{Name: "do"}); err == nil {
		t.Fatal("want do err")
	}
	if f.Count("git") != 2 || len(f.Calls()) != 6 || len(f.CallStrings()) != 6 {
		t.Fatalf("calls: %v", f.CallStrings())
	}
}

func TestFakeBlockAndCancel(t *testing.T) {
	block := make(chan struct{})
	f := NewFake().On("slow", Response{Block: block, Stdout: "ok"})
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { _, err := f.Run(ctx, Cmd{Name: "slow"}); errc <- err }()
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	close(block)
	if r, err := f.Run(context.Background(), Cmd{Name: "slow"}); err != nil || r.Stdout != "ok" {
		t.Fatalf("%v %v", r, err)
	}
	cctx, c2 := context.WithCancel(context.Background())
	c2()
	if _, err := f.Run(cctx, Cmd{Name: "slow"}); err == nil {
		t.Fatal("cancelled ctx should fail")
	}
}

func TestFakeInteractive(t *testing.T) {
	did := false
	f := NewFake().On("ssh-keygen", Response{Do: func(Cmd) error { did = true; return nil }})
	cmd := f.Interactive(Cmd{Name: "ssh-keygen", Args: []string{"-t", "ed25519"}})
	if cmd == nil || !did {
		t.Fatal("interactive hook not run")
	}
	if err := f.RunTTY(context.Background(), Cmd{Name: "ssh-keygen"}); err != nil {
		t.Fatal(err)
	}
	if f.Count("ssh-keygen") != 2 {
		t.Fatal("not recorded")
	}
	if (Cmd{Name: "a", Args: []string{"b"}, Stdin: "secret"}).String() != "a b" {
		t.Fatal("String must not include stdin")
	}
}

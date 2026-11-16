package app_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/doctor"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/keys"
	"github.com/tinhnd1902/gitid/internal/testutil"
	"github.com/tinhnd1902/gitid/internal/testutil/fixtures"
)

func TestTUIAdapters(t *testing.T) {
	e := testutil.TempEnv(t)
	f := execx.NewFake()
	s := app.New(e, f)
	cfg := fixtures.Basic()

	if s.ValidateField(config.FieldSlug, "Bad Slug") != config.ErrSlugInvalid || s.ValidateField(config.FieldSlug, "ok-slug") != "" {
		t.Fatal("ValidateField")
	}
	if got := s.DefaultBaseDir(cfg, "zed"); got != "~/Code/zed/" {
		t.Fatalf("DefaultBaseDir %q", got)
	}
	if len(s.DoctorChecks(doctor.Options{})) == 0 || len(s.DoctorChecks(doctor.Options{SSHTest: true})) != len(s.DoctorChecks(doctor.Options{}))+1 {
		t.Fatal("DoctorChecks")
	}
	al := s.SSHAliases(cfg)
	if len(al) == 0 {
		t.Fatal("SSHAliases")
	}

	f.On("git version", execx.Response{Stdout: "git version 2.56.0\n"})
	var got []doctor.Result
	for _, c := range s.DoctorChecks(doctor.Options{}) {
		if c.ID == "git_version" {
			got = s.RunDoctorCheck(context.Background(), cfg, c)
		}
	}
	if len(got) != 1 || got[0].Status != doctor.OK {
		t.Fatalf("RunDoctorCheck %+v", got)
	}

	f.On("ssh -T", execx.Response{ExitCode: 1, Stderr: "Hi! You've successfully authenticated\n"})
	if r := s.SSHTest(context.Background(), cfg, al[0]); r.Status != doctor.OK {
		t.Fatalf("SSHTest %+v", r)
	}
}

func TestKeyAdaptersAndExec(t *testing.T) {
	e := testutil.TempEnv(t)
	f := execx.NewFake()
	s := app.New(e, f)
	cfg := fixtures.Basic()
	ref, err := cfg.ParseRef("acme/carol")
	if err != nil {
		t.Fatal(err)
	}
	if in := s.KeyInfo(ref); in.Exists {
		t.Fatal("no key yet")
	}
	if _, err := s.ExistingKey(ref); !errors.Is(err, keys.ErrKeyMissing) {
		t.Fatalf("ExistingKey: %v", err)
	}
	cmd, path, err := s.KeygenExec(ref, false)
	if err != nil || cmd == nil || path == "" {
		t.Fatalf("KeygenExec %v %q %v", cmd, path, err)
	}
	// create the key files as ssh-keygen would, then read the pub key
	testutil.WriteFile(t, path, "PRIVATE", 0o000)
	testutil.WriteFile(t, path+".pub", "ssh-ed25519 AAAA test\n", 0o644)
	if in := s.KeyInfo(ref); !in.Exists || !in.PubExists {
		t.Fatalf("KeyInfo %+v", in)
	}
	if _, _, err := s.KeygenExec(ref, false); !errors.Is(err, keys.ErrKeyExists) {
		t.Fatalf("second KeygenExec: %v", err)
	}
	if os.Getuid() != 0 {
		pub, err := s.ExistingKey(ref)
		if err != nil || pub.Content != "ssh-ed25519 AAAA test" || filepath.Base(pub.PubPath) == "" {
			t.Fatalf("ExistingKey %+v %v", pub, err)
		}
	}
	httpsRef, _ := cfg.ParseRef("acme/alice")
	if _, _, err := s.KeygenExec(httpsRef, false); !errors.Is(err, app.ErrNotSSH) {
		t.Fatalf("https account: %v", err)
	}
}

func TestCloneExec(t *testing.T) {
	e := testutil.TempEnv(t)
	s := app.New(e, execx.NewFake())
	cfg := fixtures.Basic()
	p, err := s.PlanClone(cfg, app.CloneRequest{URL: "https://github.com/org/repo.git", Account: "acme/alice"})
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := s.CloneExec(p)
	if err != nil || cmd == nil {
		t.Fatalf("CloneExec %v %v", cmd, err)
	}
}

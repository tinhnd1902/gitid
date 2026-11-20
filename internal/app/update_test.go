package app_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/testutil"
)

func releaseServer(t *testing.T, loc string) *app.Service {
	t.Helper()
	svc, _, _ := onlineSvc(t, nil, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", loc)
		w.WriteHeader(http.StatusFound)
	})
	return svc
}

// A1: the service asks, and names the install method.
func TestA1CheckUpdate(t *testing.T) {
	svc := releaseServer(t, "https://github.com/tinhnd1902/gitid/releases/tag/v0.2.0")
	svc.Version = "0.1.0"
	svc.Executable = func() (string, error) { return "/opt/homebrew/Caskroom/gitid/0.1.0/gitid", nil }
	n, ok := svc.CheckUpdate(context.Background())
	if !ok || n.Latest != "0.2.0" || n.Method != app.UpdateViaBrew {
		t.Fatalf("%+v %v", n, ok)
	}
}

// A1b: no Executable is safe.
func TestA1bNoExecutable(t *testing.T) {
	svc := releaseServer(t, "https://github.com/tinhnd1902/gitid/releases/tag/v0.2.0")
	svc.Version = "0.1.0"
	n, ok := svc.CheckUpdate(context.Background())
	if !ok || n.Method != app.UpdateViaUnknown {
		t.Fatalf("%+v %v", n, ok)
	}
}

// A2: a build without a version never asks (the failing transport of TempEnv
// would fail the test).
func TestA2NoVersionNoRequest(t *testing.T) {
	svc := app.New(testutil.TempEnv(t), execx.NewFake())
	if n, ok := svc.CheckUpdate(context.Background()); ok {
		t.Fatalf("notice %+v", n)
	}
	svc.Version = "dev"
	if _, ok := svc.CheckUpdate(context.Background()); ok {
		t.Fatal("notice for dev")
	}
}

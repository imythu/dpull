package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imythu/dpull/internal/cache"
	"github.com/imythu/dpull/internal/crane"
	"github.com/imythu/dpull/internal/docker"
	"github.com/imythu/dpull/internal/logger"
	"github.com/imythu/dpull/internal/runner"
)

type selectiveRunner struct {
	commands []runner.Command
	failName string
	local    bool
}

const fixtureImageID = "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"

type fakeCraneInstaller struct {
	path  string
	proxy string
}

func (f *fakeCraneInstaller) Ensure(_ context.Context, proxyURL string) (string, error) {
	f.proxy = proxyURL
	return f.path, nil
}

func (r *selectiveRunner) Run(_ context.Context, command runner.Command) error {
	r.commands = append(r.commands, command)
	if (command.Name == "crane" || command.Name == "/managed/bin/crane") && command.Args[0] == "config" {
		if r.failName == "crane" {
			return errors.New("fixture failure")
		}
		_, _ = fmt.Fprintln(command.Stdout, "{}")
		return nil
	}
	if command.Name == "docker" && len(command.Args) > 1 && command.Args[0] == "image" {
		if r.local {
			_, _ = fmt.Fprintln(command.Stdout, fixtureImageID)
			return nil
		}
		return errors.New("image not found")
	}
	if command.Name == r.failName {
		return errors.New("fixture failure")
	}
	if len(command.Args) > 0 && command.Args[0] == "pull" {
		return os.WriteFile(command.Args[2], []byte("archive"), 0o600)
	}
	return nil
}

func TestRunEnsuresCraneWithResolvedProxy(t *testing.T) {
	t.Parallel()
	recorder := &selectiveRunner{}
	installer := &fakeCraneInstaller{path: "/managed/bin/crane"}
	application := testApplication(t, recorder)
	application.CraneInstaller = installer
	proxyURL := "socks5://127.0.0.1:1080"
	if err := application.Run(context.Background(), Options{Images: []string{"nginx"}, Proxy: proxyURL}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if installer.proxy != proxyURL {
		t.Fatalf("installer proxy = %q, want %q", installer.proxy, proxyURL)
	}
	if recorder.commands[0].Name != installer.path || recorder.commands[2].Name != installer.path {
		t.Fatalf("crane commands do not use managed binary: %#v", recorder.commands)
	}
}

func TestRunDoesNotStartComposeAfterImageFailure(t *testing.T) {
	t.Parallel()
	recorder := &selectiveRunner{failName: "crane"}
	application := testApplication(t, recorder)
	err := application.Run(context.Background(), Options{Images: []string{"nginx"}, Up: true})
	if err == nil {
		t.Fatal("Run() error = nil, want failure")
	}
	if len(recorder.commands) != 1 || recorder.commands[0].Name != "crane" {
		t.Fatalf("commands = %#v", recorder.commands)
	}
}

func TestRunLoadsAndRemovesArchive(t *testing.T) {
	t.Parallel()
	recorder := &selectiveRunner{}
	application := testApplication(t, recorder)
	if err := application.Run(context.Background(), Options{Images: []string{"nginx"}}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(recorder.commands) != 4 {
		t.Fatalf("commands count = %d, want 4", len(recorder.commands))
	}
	entries, err := os.ReadDir(application.Cache.Root)
	if err != nil {
		t.Fatalf("read cache root: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("cache entries = %v, want empty", entries)
	}
}

func TestRunSkipsImageAlreadyInDocker(t *testing.T) {
	t.Parallel()
	recorder := &selectiveRunner{local: true}
	application := testApplication(t, recorder)
	if err := application.Run(context.Background(), Options{Images: []string{"registry.example/api:v1"}}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(recorder.commands) != 2 {
		t.Fatalf("commands = %#v, want crane config and docker inspect", recorder.commands)
	}
}

func testApplication(t *testing.T, commandRunner runner.CommandRunner) *Application {
	t.Helper()
	manager := cache.New(filepath.Join(t.TempDir(), ".crane"))
	manager.Now = func() time.Time { return time.Unix(0, 42) }
	return &Application{
		Cache: manager, Crane: crane.Client{Runner: commandRunner},
		Docker: docker.Client{Runner: commandRunner}, Log: logger.New(&bytes.Buffer{}),
		Now: func() time.Time { return time.Unix(100, 0) },
	}
}

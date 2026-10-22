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
	noDaemon bool
}

const fixtureImageID = "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"

type fakeRegistry struct {
	calls   []string
	fail    bool
	options crane.Options
}

func (f *fakeRegistry) ImageID(_ context.Context, image string, options crane.Options) (string, error) {
	f.calls = append(f.calls, "config "+image)
	f.options = options
	if f.fail {
		return "", errors.New("fixture failure")
	}
	return fixtureImageID, nil
}

func (f *fakeRegistry) Pull(_ context.Context, image, target string, _ crane.Options) error {
	f.calls = append(f.calls, "pull "+image)
	return os.WriteFile(target, []byte("archive"), 0o600)
}

func (r *selectiveRunner) Run(_ context.Context, command runner.Command) error {
	r.commands = append(r.commands, command)
	if command.Name == "docker" && len(command.Args) > 0 && command.Args[0] == "version" {
		if r.noDaemon {
			return errors.New("daemon unavailable")
		}
		_, _ = fmt.Fprintln(command.Stdout, "linux/arm64")
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
	return nil
}

func TestRunPassesProxyAndDockerPlatformToRegistry(t *testing.T) {
	t.Parallel()
	registry := &fakeRegistry{}
	application := testApplication(t, &selectiveRunner{}, registry)
	proxyURL := "socks5://127.0.0.1:1080"
	if err := application.Run(context.Background(), Options{Images: []string{"nginx"}, Proxy: proxyURL}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := crane.Options{Proxy: proxyURL, Platform: "linux/arm64"}
	if registry.options != want {
		t.Fatalf("registry options = %+v, want %+v", registry.options, want)
	}
}

func TestRunPrefersExplicitPlatform(t *testing.T) {
	t.Parallel()
	recorder := &selectiveRunner{}
	registry := &fakeRegistry{}
	application := testApplication(t, recorder, registry)
	if err := application.Run(context.Background(), Options{Images: []string{"nginx"}, Platform: "linux/arm/v7"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if registry.options.Platform != "linux/arm/v7" {
		t.Fatalf("platform = %q, want linux/arm/v7", registry.options.Platform)
	}
	for _, command := range recorder.commands {
		if command.Args[0] == "version" {
			t.Fatalf("queried Docker platform despite explicit --platform")
		}
	}
}

func TestRunFallsBackToHostPlatform(t *testing.T) {
	t.Parallel()
	registry := &fakeRegistry{}
	application := testApplication(t, &selectiveRunner{noDaemon: true}, registry)
	_ = application.Run(context.Background(), Options{Images: []string{"nginx"}})
	if registry.options.Platform != crane.DefaultPlatform() {
		t.Fatalf("platform = %q, want %q", registry.options.Platform, crane.DefaultPlatform())
	}
}

func TestRunDoesNotStartComposeAfterImageFailure(t *testing.T) {
	t.Parallel()
	recorder := &selectiveRunner{}
	application := testApplication(t, recorder, &fakeRegistry{fail: true})
	err := application.Run(context.Background(), Options{Images: []string{"nginx"}, Up: true})
	if err == nil {
		t.Fatal("Run() error = nil, want failure")
	}
	if len(recorder.commands) != 1 {
		t.Fatalf("commands = %#v, want docker version only", recorder.commands)
	}
}

func TestRunLoadsAndRemovesArchive(t *testing.T) {
	t.Parallel()
	recorder := &selectiveRunner{}
	registry := &fakeRegistry{}
	application := testApplication(t, recorder, registry)
	if err := application.Run(context.Background(), Options{Images: []string{"nginx"}}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(recorder.commands) != 3 || len(registry.calls) != 2 {
		t.Fatalf("commands = %#v, registry calls = %v", recorder.commands, registry.calls)
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
	registry := &fakeRegistry{}
	application := testApplication(t, recorder, registry)
	if err := application.Run(context.Background(), Options{Images: []string{"registry.example/api:v1"}}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(recorder.commands) != 2 || len(registry.calls) != 1 {
		t.Fatalf("commands = %#v, registry calls = %v, want config and docker inspect only", recorder.commands, registry.calls)
	}
}

func testApplication(t *testing.T, commandRunner runner.CommandRunner, registry Registry) *Application {
	t.Helper()
	manager := cache.New(filepath.Join(t.TempDir(), ".crane"))
	manager.Now = func() time.Time { return time.Unix(0, 42) }
	return &Application{
		Cache: manager, Crane: registry,
		Docker: docker.Client{Runner: commandRunner}, Log: logger.New(&bytes.Buffer{}),
		Now: func() time.Time { return time.Unix(100, 0) },
	}
}

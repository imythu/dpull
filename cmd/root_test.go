package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/imythu/dpull/internal/app"
	"github.com/imythu/dpull/internal/cache"
	"github.com/imythu/dpull/internal/crane"
	"github.com/imythu/dpull/internal/docker"
	"github.com/imythu/dpull/internal/logger"
	"github.com/imythu/dpull/internal/proxy"
	"github.com/imythu/dpull/internal/runner"
)

type fakeRunner struct {
	commands []runner.Command
}

func (f *fakeRunner) Run(_ context.Context, command runner.Command) error {
	f.commands = append(f.commands, command)
	if command.Name == "docker" && len(command.Args) > 1 && command.Args[0] == "image" {
		return errors.New("image not found")
	}
	return nil
}

type fakeRegistry struct {
	pulls [][]string
}

func (f *fakeRegistry) ImageID(context.Context, string, crane.Options) (string, error) {
	return "sha256:remote", nil
}

func (f *fakeRegistry) Pull(_ context.Context, image, target string, options crane.Options) error {
	f.pulls = append(f.pulls, []string{image, target, options.Proxy, options.Platform})
	return os.WriteFile(target, []byte("archive"), 0o600)
}

func TestRootCommandParsesArgumentsAndFlags(t *testing.T) {
	t.Parallel()
	temp := t.TempDir()
	recorder := &fakeRunner{}
	registry := &fakeRegistry{}
	manager := cache.New(filepath.Join(temp, ".crane"))
	manager.Now = func() time.Time { return time.Unix(0, 99) }
	application := &app.Application{
		Cache: manager, Crane: registry,
		Docker: docker.Client{Runner: recorder}, Log: logger.New(&bytes.Buffer{}),
		Now: func() time.Time { return time.Unix(10, 0) },
	}
	command := NewRootCommand(application)
	command.SetArgs([]string{"--proxy", "socks5://localhost:7890", "--platform", "linux/arm64", "--keep", "nginx:latest"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if len(recorder.commands) != 2 {
		t.Fatalf("commands count = %d, want 2", len(recorder.commands))
	}
	wantPull := [][]string{{"nginx:latest", filepath.Join(manager.Root, "99", "nginx_latest.tar"), "socks5://localhost:7890", "linux/arm64"}}
	if !reflect.DeepEqual(registry.pulls, wantPull) {
		t.Fatalf("pulls = %v, want %v", registry.pulls, wantPull)
	}
	if _, err := os.Stat(wantPull[0][1]); err != nil {
		t.Fatalf("--keep did not preserve tar: %v", err)
	}
}

func TestCleanCommandRemovesAllCacheEntries(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), ".crane")
	if err := os.MkdirAll(filepath.Join(root, "unfinished"), 0o755); err != nil {
		t.Fatalf("create cache fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "unfinished", "partial.tar"), []byte("partial"), 0o600); err != nil {
		t.Fatalf("write cache fixture: %v", err)
	}
	application := &app.Application{Cache: cache.New(root)}
	command := NewRootCommand(application)
	command.SetOut(&bytes.Buffer{})
	command.SetArgs([]string{"clean"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read cache root: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("cache entries remain: %v", entries)
	}
}

func TestCompletionCommandGeneratesBashScript(t *testing.T) {
	t.Parallel()
	command := NewRootCommand(nil)
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"completion", "bash"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if !bytes.Contains(output.Bytes(), []byte("__start_dpull")) {
		t.Fatalf("completion output does not contain dpull entry point")
	}
}

func TestProxySetCommand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	resolver := &proxy.Resolver{
		SystemPath: filepath.Join(dir, "etc", "dpull.conf"),
		UserPath:   filepath.Join(dir, "home", ".dpull", "dpull.conf"),
		LookupEnv:  func(string) (string, bool) { return "", false },
	}
	command := NewRootCommand(nil, resolver)
	command.SetOut(&bytes.Buffer{})
	command.SetArgs([]string{"proxy", "set", "-g", "socks5://localhost:7890"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	content, err := os.ReadFile(resolver.SystemPath)
	if err != nil {
		t.Fatalf("read global proxy config: %v", err)
	}
	if string(content) != "proxy=socks5://localhost:7890\n" {
		t.Fatalf("config = %q", content)
	}
}

func TestProxyDisableShowAndReset(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	resolver := &proxy.Resolver{
		SystemPath: filepath.Join(dir, "etc", "dpull.conf"),
		UserPath:   filepath.Join(dir, "home", ".dpull", "dpull.conf"),
		LookupEnv:  func(string) (string, bool) { return "", false },
	}
	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		command := NewRootCommand(nil, resolver)
		command.SetOut(&output)
		command.SetArgs(args)
		if err := command.ExecuteContext(context.Background()); err != nil {
			t.Fatalf("%v error = %v", args, err)
		}
		return output.String()
	}
	if output := run("proxy", "disable"); !strings.Contains(output, "Now using: disabled, direct connection (source: user config)") {
		t.Fatalf("disable output = %q", output)
	}
	if output := run("proxy"); !strings.Contains(output, "Status:      disabled") {
		t.Fatalf("proxy output = %q", output)
	}
	if output := run("proxy", "reset"); !strings.Contains(output, "Now using: "+proxy.BuiltinProxy+" (source: builtin)") {
		t.Fatalf("reset output = %q", output)
	}
	if output := run("proxy"); !strings.Contains(output, "Status:      enabled") {
		t.Fatalf("proxy output = %q", output)
	}
}

func TestRootCommandLogsProxyChoice(t *testing.T) {
	t.Parallel()
	temp := t.TempDir()
	recorder := &fakeRunner{}
	var log bytes.Buffer
	application := &app.Application{
		Cache: cache.New(filepath.Join(temp, ".crane")), Crane: &fakeRegistry{},
		Docker: docker.Client{Runner: recorder}, Log: logger.New(&log),
		Now: time.Now,
	}
	resolver := &proxy.Resolver{
		SystemPath: filepath.Join(temp, "system.conf"), UserPath: filepath.Join(temp, "user.conf"),
		LookupEnv: func(string) (string, bool) { return "", false },
	}
	command := NewRootCommand(application, resolver)
	command.SetArgs([]string{"--proxy", "direct", "nginx"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	for _, want := range []string{
		"Proxy:     direct connection, proxy disabled (source: --proxy)",
		"-> pulling with crane via direct connection, proxy disabled",
	} {
		if !strings.Contains(log.String(), want) {
			t.Fatalf("log does not contain %q:\n%s", want, log.String())
		}
	}
}

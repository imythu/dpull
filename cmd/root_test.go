package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
	if command.Name == "crane" && command.Args[0] == "config" {
		_, _ = fmt.Fprintln(command.Stdout, "{}")
		return nil
	}
	if command.Name == "docker" && len(command.Args) > 1 && command.Args[0] == "image" {
		return errors.New("image not found")
	}
	if command.Name == "crane" {
		path := command.Args[len(command.Args)-1]
		if err := os.WriteFile(path, []byte("archive"), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func TestRootCommandParsesArgumentsAndFlags(t *testing.T) {
	t.Parallel()
	temp := t.TempDir()
	recorder := &fakeRunner{}
	manager := cache.New(filepath.Join(temp, ".crane"))
	manager.Now = func() time.Time { return time.Unix(0, 99) }
	application := &app.Application{
		Cache: manager, Crane: crane.Client{Runner: recorder},
		Docker: docker.Client{Runner: recorder}, Log: logger.New(&bytes.Buffer{}),
		Now: func() time.Time { return time.Unix(10, 0) },
	}
	command := NewRootCommand(application)
	command.SetArgs([]string{"--proxy", "socks5://localhost:7890", "--keep", "nginx:latest"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if len(recorder.commands) != 4 {
		t.Fatalf("commands count = %d, want 4", len(recorder.commands))
	}
	wantPull := []string{"pull", "nginx:latest", filepath.Join(manager.Root, "99", "nginx_latest.tar")}
	if !reflect.DeepEqual(recorder.commands[2].Args, wantPull) {
		t.Fatalf("crane args = %v, want %v", recorder.commands[2].Args, wantPull)
	}
	if _, err := os.Stat(wantPull[2]); err != nil {
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

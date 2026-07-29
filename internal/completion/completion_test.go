package completion

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetect(t *testing.T) {
	t.Parallel()
	environment := map[string]string{"SHELL": "/usr/bin/zsh"}
	shell, err := Detect(func(key string) string { return environment[key] })
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if shell != "zsh" {
		t.Fatalf("Detect() = %q, want zsh", shell)
	}
}

func TestInstallPath(t *testing.T) {
	t.Parallel()
	path, err := InstallPath("/home/user", "fish", "linux")
	if err != nil {
		t.Fatalf("InstallPath() error = %v", err)
	}
	want := filepath.Join("/home/user", ".config", "fish", "completions", "dpull.fish")
	if path != want {
		t.Fatalf("InstallPath() = %q, want %q", path, want)
	}
}

func TestInstall(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "completions", "dpull")
	if err := Install(path, []byte("complete dpull")); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read installed completion: %v", err)
	}
	if string(content) != "complete dpull" {
		t.Fatalf("content = %q", content)
	}
}

package completion

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var Supported = []string{"bash", "zsh", "fish", "powershell"}

func Detect(getenv func(string) string) (string, error) {
	shell := strings.ToLower(filepath.Base(getenv("SHELL")))
	shell = strings.TrimSuffix(shell, filepath.Ext(shell))
	for _, supported := range Supported {
		if shell == supported || shell == "pwsh" && supported == "powershell" {
			return supported, nil
		}
	}
	if getenv("PSModulePath") != "" {
		return "powershell", nil
	}
	return "", fmt.Errorf("detect shell: specify one of %s", strings.Join(Supported, ", "))
}

func Validate(shell string) error {
	for _, supported := range Supported {
		if shell == supported {
			return nil
		}
	}
	return fmt.Errorf("unsupported shell %q; use %s", shell, strings.Join(Supported, ", "))
}

func InstallPath(home, shell, goos string) (string, error) {
	if err := Validate(shell); err != nil {
		return "", err
	}
	switch shell {
	case "bash":
		return filepath.Join(home, ".local", "share", "bash-completion", "completions", "dpull"), nil
	case "zsh":
		return filepath.Join(home, ".zfunc", "_dpull"), nil
	case "fish":
		return filepath.Join(home, ".config", "fish", "completions", "dpull.fish"), nil
	case "powershell":
		if goos == "windows" {
			return filepath.Join(home, "Documents", "PowerShell", "dpull-completion.ps1"), nil
		}
		return filepath.Join(home, ".config", "powershell", "dpull-completion.ps1"), nil
	default:
		return "", fmt.Errorf("resolve completion path for %q", shell)
	}
}

func Install(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create completion directory for %q: %w", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("write completion file %q: %w", path, err)
	}
	return nil
}

func DefaultPath(shell string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home for completion: %w", err)
	}
	return InstallPath(home, shell, runtime.GOOS)
}

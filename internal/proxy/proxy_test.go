package proxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePriority(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.conf")
	userPath := filepath.Join(dir, "user.conf")
	writeConfig(t, systemPath, "proxy=http://system:8080\n")
	writeConfig(t, userPath, "proxy: socks5://user:7890\n")
	resolver := &Resolver{
		SystemPath: systemPath,
		UserPath:   userPath,
		LookupEnv: func(key string) (string, bool) {
			return "http://environment:3128", key == EnvironmentName
		},
	}
	info, err := resolver.Resolve("")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if info.Effective != "http://environment:3128" || info.Source != SourceEnv {
		t.Fatalf("effective proxy = %q (%s)", info.Effective, info.Source)
	}
	info, err = resolver.Resolve("socks5://flag:1080")
	if err != nil {
		t.Fatalf("Resolve(flag) error = %v", err)
	}
	if info.Effective != "socks5://flag:1080" || info.Source != SourceFlag {
		t.Fatalf("flag proxy = %q (%s)", info.Effective, info.Source)
	}
}

func TestResolveFallsBackToSystem(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.conf")
	writeConfig(t, systemPath, "# dpull\nsocks5://system:7890\n")
	resolver := &Resolver{
		SystemPath: systemPath,
		UserPath:   filepath.Join(dir, "missing.conf"),
		LookupEnv:  func(string) (string, bool) { return "", false },
	}
	info, err := resolver.Resolve("")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if info.Effective != "socks5://system:7890" || info.Source != SourceSystem {
		t.Fatalf("effective proxy = %q (%s)", info.Effective, info.Source)
	}
}

func TestResolveUsesBuiltinByDefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	resolver := &Resolver{
		SystemPath: filepath.Join(dir, "missing-system.conf"),
		UserPath:   filepath.Join(dir, "missing-user.conf"),
		LookupEnv:  func(string) (string, bool) { return "", false },
	}
	info, err := resolver.Resolve("")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if info.Effective != "http://127.0.0.1:7890" || info.Source != SourceBuiltin {
		t.Fatalf("effective proxy = %q (%s)", info.Effective, info.Source)
	}
}

func TestValidateRejectsUnsupportedScheme(t *testing.T) {
	t.Parallel()
	if err := Validate("ftp://127.0.0.1:21"); err == nil {
		t.Fatal("Validate() error = nil, want unsupported scheme error")
	}
}

func TestResolveRejectsInvalidConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.conf")
	writeConfig(t, path, "unknown=value\n")
	resolver := &Resolver{
		SystemPath: path,
		UserPath:   filepath.Join(dir, "missing.conf"),
		LookupEnv:  func(string) (string, bool) { return "", false },
	}
	if _, err := resolver.Resolve(""); err == nil {
		t.Fatal("Resolve() error = nil, want invalid config error")
	}
}

func TestSetUserAndGlobalConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	resolver := &Resolver{
		SystemPath: filepath.Join(dir, "etc", "dpull.conf"),
		UserPath:   filepath.Join(dir, "home", ".dpull", "dpull.conf"),
		LookupEnv:  func(string) (string, bool) { return "", false },
	}
	path, err := resolver.Set("socks5://user:7890", false)
	if err != nil {
		t.Fatalf("Set(user) error = %v", err)
	}
	if path != resolver.UserPath {
		t.Fatalf("Set(user) path = %q, want %q", path, resolver.UserPath)
	}
	path, err = resolver.Set("http://global:8080", true)
	if err != nil {
		t.Fatalf("Set(global) error = %v", err)
	}
	if path != resolver.SystemPath {
		t.Fatalf("Set(global) path = %q, want %q", path, resolver.SystemPath)
	}
	info, err := resolver.Resolve("")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if info.Effective != "socks5://user:7890" || info.Source != SourceUser {
		t.Fatalf("effective proxy = %q (%s)", info.Effective, info.Source)
	}
}

func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

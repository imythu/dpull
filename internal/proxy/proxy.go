package proxy

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	BuiltinProxy    = "http://127.0.0.1:7890"
	EnvironmentName = "DPULL_PROXY"
	SystemConfig    = "/etc/dpull.conf"
)

type Source string

const (
	SourceBuiltin Source = "builtin"
	SourceSystem  Source = "/etc/dpull.conf"
	SourceUser    Source = "user config"
	SourceEnv     Source = "DPULL_PROXY"
	SourceFlag    Source = "--proxy"
)

type Info struct {
	Effective string
	Source    Source
	Builtin   string
	System    string
	User      string
	Env       string
	UserPath  string
}

type Resolver struct {
	SystemPath string
	UserPath   string
	LookupEnv  func(string) (string, bool)
}

func DefaultResolver() (*Resolver, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user home for proxy config: %w", err)
	}
	return &Resolver{
		SystemPath: SystemConfig,
		UserPath:   filepath.Join(home, ".dpull", "dpull.conf"),
		LookupEnv:  os.LookupEnv,
	}, nil
}

func (r *Resolver) Resolve(flagValue string) (Info, error) {
	info := Info{Effective: BuiltinProxy, Source: SourceBuiltin, Builtin: BuiltinProxy, UserPath: r.UserPath}
	var err error
	info.System, err = readConfig(r.SystemPath)
	if err != nil {
		return Info{}, err
	}
	apply(&info, info.System, SourceSystem)
	info.User, err = readConfig(r.UserPath)
	if err != nil {
		return Info{}, err
	}
	apply(&info, info.User, SourceUser)
	if value, exists := r.LookupEnv(EnvironmentName); exists {
		info.Env = strings.TrimSpace(value)
		apply(&info, info.Env, SourceEnv)
	}
	apply(&info, strings.TrimSpace(flagValue), SourceFlag)
	if err := Validate(info.Effective); err != nil {
		return Info{}, fmt.Errorf("validate %s proxy: %w", info.Source, err)
	}
	return info, nil
}

func (r *Resolver) Set(value string, global bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("set proxy: value is empty")
	}
	if err := Validate(value); err != nil {
		return "", fmt.Errorf("set proxy: %w", err)
	}
	path := r.UserPath
	if global {
		path = r.SystemPath
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("create proxy config directory %q: %w", parent, err)
	}
	content := []byte("proxy=" + value + "\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return "", fmt.Errorf("write proxy config %q: %w", path, err)
	}
	return path, nil
}

func Validate(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("parse URL %q: %w", value, err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "socks5":
	default:
		return fmt.Errorf("unsupported scheme %q; use http, https, or socks5", parsed.Scheme)
	}
	if parsed.Host == "" {
		return fmt.Errorf("proxy URL %q has no host", value)
	}
	return nil
}

func apply(info *Info, value string, source Source) {
	if value == "" {
		return
	}
	info.Effective = value
	info.Source = source
}

func readConfig(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("open proxy config %q: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		value, parseErr := parseLine(line)
		if parseErr != nil {
			return "", fmt.Errorf("parse proxy config %q: %w", path, parseErr)
		}
		return value, nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read proxy config %q: %w", path, err)
	}
	return "", nil
}

func parseLine(line string) (string, error) {
	if strings.Contains(line, "://") && !strings.HasPrefix(strings.ToLower(line), "proxy") {
		return strings.TrimSpace(line), nil
	}
	for _, separator := range []string{"=", ":"} {
		key, value, found := strings.Cut(line, separator)
		if found && strings.EqualFold(strings.TrimSpace(key), "proxy") {
			value = strings.Trim(strings.TrimSpace(value), "\"'")
			if value == "" {
				return "", fmt.Errorf("proxy value is empty")
			}
			return value, nil
		}
	}
	return "", fmt.Errorf("expected proxy=URL, proxy: URL, or a URL")
}

package crane

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"

	"github.com/imythu/dpull/internal/runner"
)

type Client struct {
	Runner runner.CommandRunner
	Binary string
}

func (c Client) Pull(ctx context.Context, image, target, proxy string) error {
	command := runner.TerminalCommand(c.binary(), "pull", image, target)
	if proxy != "" {
		command.Env = proxyEnvironment(os.Environ(), proxy)
	}
	if err := c.Runner.Run(ctx, command); err != nil {
		return fmt.Errorf("pull image %q: %w", image, err)
	}
	return nil
}

func (c Client) ImageID(ctx context.Context, image, proxy string) (string, error) {
	var output bytes.Buffer
	command := runner.Command{
		Name: c.binary(), Args: []string{"config", image},
		Env: proxyCommandEnvironment(proxy), Stdout: &output, Stderr: os.Stderr,
	}
	if err := c.Runner.Run(ctx, command); err != nil {
		return "", fmt.Errorf("get remote config for image %q: %w", image, err)
	}
	config := bytes.TrimSuffix(output.Bytes(), []byte("\n"))
	if len(config) == 0 {
		return "", fmt.Errorf("get remote config for image %q: crane returned empty output", image)
	}
	digest := sha256.Sum256(config)
	return fmt.Sprintf("sha256:%x", digest), nil
}

func (c Client) binary() string {
	if c.Binary != "" {
		return c.Binary
	}
	return "crane"
}

func proxyCommandEnvironment(proxy string) []string {
	if proxy == "" {
		return nil
	}
	return proxyEnvironment(os.Environ(), proxy)
}

func proxyEnvironment(base []string, proxy string) []string {
	env := append([]string(nil), base...)
	keys := []string{"ALL_PROXY", "HTTP_PROXY", "HTTPS_PROXY"}
	for _, key := range keys {
		env = setEnvironment(env, key, proxy)
	}
	return env
}

func setEnvironment(env []string, key, value string) []string {
	prefix := key + "="
	for i := range env {
		separator := strings.IndexByte(env[i], '=')
		if separator >= 0 && strings.EqualFold(env[i][:separator], key) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

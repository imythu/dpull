package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/imythu/dpull/internal/runner"
)

type Client struct {
	Runner runner.CommandRunner
}

func (c Client) HasDigest(ctx context.Context, image, digest string) (bool, error) {
	var output bytes.Buffer
	command := runner.Command{
		Name: "docker", Args: []string{"image", "inspect", "--format", "{{json .RepoDigests}}", image},
		Stdout: &output, Stderr: io.Discard,
	}
	if err := c.Runner.Run(ctx, command); err != nil {
		return false, nil
	}
	var repoDigests []string
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &repoDigests); err != nil {
		return false, fmt.Errorf("decode local digests for image %q: %w", image, err)
	}
	for _, local := range repoDigests {
		if strings.HasSuffix(local, "@"+digest) {
			return true, nil
		}
	}
	return false, nil
}

func (c Client) Load(ctx context.Context, tarPath string) error {
	command := runner.TerminalCommand("docker", "load", "-i", tarPath)
	if err := c.Runner.Run(ctx, command); err != nil {
		return fmt.Errorf("load image archive %q: %w", tarPath, err)
	}
	return nil
}

func (c Client) ComposeUp(ctx context.Context, composeFile string) error {
	args := []string{"compose"}
	if composeFile != "" {
		args = append(args, "-f", composeFile)
	}
	args = append(args, "up", "-d")
	if err := c.Runner.Run(ctx, runner.TerminalCommand("docker", args...)); err != nil {
		return fmt.Errorf("start compose services: %w", err)
	}
	return nil
}

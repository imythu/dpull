package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/imythu/dpull/internal/runner"
)

type Client struct {
	Runner runner.CommandRunner
}

func (c Client) ImageID(ctx context.Context, image string) (string, bool, error) {
	var output bytes.Buffer
	command := runner.Command{
		Name: "docker", Args: []string{"image", "inspect", "--format", "{{.Id}}", image},
		Stdout: &output, Stderr: io.Discard,
	}
	if err := c.Runner.Run(ctx, command); err != nil {
		return "", false, nil
	}
	id := string(bytes.TrimSpace(output.Bytes()))
	if id == "" {
		return "", false, fmt.Errorf("inspect local image %q: Docker returned an empty image ID", image)
	}
	return id, true, nil
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

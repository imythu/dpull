package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// Command describes an external command and its process environment.
type Command struct {
	Name   string
	Args   []string
	Env    []string
	Stdout io.Writer
	Stderr io.Writer
}

// CommandRunner executes external commands.
type CommandRunner interface {
	Run(ctx context.Context, command Command) error
}

// ExecRunner executes commands with os/exec.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, command Command) error {
	cmd := exec.CommandContext(ctx, command.Name, command.Args...)
	cmd.Stdout = command.Stdout
	cmd.Stderr = command.Stderr
	if command.Env != nil {
		cmd.Env = command.Env
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run %s: %w", command.Name, err)
	}
	return nil
}

func TerminalCommand(name string, args ...string) Command {
	return Command{Name: name, Args: args, Stdout: os.Stdout, Stderr: os.Stderr}
}

package runner

import (
	"context"
	"testing"
)

type mockRunner struct {
	commands []Command
}

func (m *mockRunner) Run(_ context.Context, command Command) error {
	m.commands = append(m.commands, command)
	return nil
}

func TestCommandRunnerMock(t *testing.T) {
	t.Parallel()
	mock := &mockRunner{}
	command := TerminalCommand("crane", "pull", "nginx", "nginx.tar")
	if err := mock.Run(context.Background(), command); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(mock.commands) != 1 || mock.commands[0].Name != "crane" {
		t.Fatalf("recorded commands = %#v", mock.commands)
	}
}

package docker

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/imythu/dpull/internal/runner"
)

type recordingRunner struct {
	commands []runner.Command
	err      error
	output   string
}

func (r *recordingRunner) Run(_ context.Context, command runner.Command) error {
	r.commands = append(r.commands, command)
	if command.Stdout != nil && r.output != "" {
		_, _ = fmt.Fprint(command.Stdout, r.output)
	}
	return r.err
}

func TestLoad(t *testing.T) {
	t.Parallel()
	recorder := &recordingRunner{}
	if err := (Client{Runner: recorder}).Load(context.Background(), "image.tar"); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := []string{"load", "-i", "image.tar"}
	if !reflect.DeepEqual(recorder.commands[0].Args, want) {
		t.Fatalf("args = %v, want %v", recorder.commands[0].Args, want)
	}
}

func TestHasDigestUsesCompleteReference(t *testing.T) {
	t.Parallel()
	recorder := &recordingRunner{output: `["ghcr.io/example/api@sha256:abc"]`}
	image := "ghcr.io/example/api:1.2.3"
	matches, err := (Client{Runner: recorder}).HasDigest(context.Background(), image, "sha256:abc")
	if err != nil {
		t.Fatalf("HasDigest() error = %v", err)
	}
	if !matches {
		t.Fatal("HasDigest() = false, want true")
	}
	want := []string{"image", "inspect", "--format", "{{json .RepoDigests}}", image}
	if !reflect.DeepEqual(recorder.commands[0].Args, want) {
		t.Fatalf("args = %v, want %v", recorder.commands[0].Args, want)
	}
	if recorder.commands[0].Stdout == nil || recorder.commands[0].Stderr == nil {
		t.Fatal("inspect output is not redirected")
	}
}

func TestHasDigestReturnsFalseWhenInspectFails(t *testing.T) {
	t.Parallel()
	recorder := &recordingRunner{err: errors.New("not found")}
	matches, err := (Client{Runner: recorder}).HasDigest(context.Background(), "nginx:missing", "sha256:abc")
	if err != nil {
		t.Fatalf("HasDigest() error = %v", err)
	}
	if matches {
		t.Fatal("HasDigest() = true, want false")
	}
}

func TestComposeUpWithFile(t *testing.T) {
	t.Parallel()
	recorder := &recordingRunner{}
	if err := (Client{Runner: recorder}).ComposeUp(context.Background(), "compose.yml"); err != nil {
		t.Fatalf("ComposeUp() error = %v", err)
	}
	want := []string{"compose", "-f", "compose.yml", "up", "-d"}
	if !reflect.DeepEqual(recorder.commands[0].Args, want) {
		t.Fatalf("args = %v, want %v", recorder.commands[0].Args, want)
	}
}

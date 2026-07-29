package crane

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/imythu/dpull/internal/runner"
)

type recordingRunner struct {
	command runner.Command
}

func (r *recordingRunner) Run(_ context.Context, command runner.Command) error {
	r.command = command
	if len(command.Args) > 0 && command.Args[0] == "config" {
		_, _ = fmt.Fprintln(command.Stdout, `{"architecture":"amd64"}`)
	}
	return nil
}

func TestPullSetsProxyEnvironment(t *testing.T) {
	t.Parallel()
	recorder := &recordingRunner{}
	client := Client{Runner: recorder}
	if err := client.Pull(context.Background(), "nginx", "nginx.tar", "socks5://localhost:7890"); err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if recorder.command.Name != "crane" || len(recorder.command.Args) != 3 {
		t.Fatalf("command = %#v", recorder.command)
	}
	for _, key := range []string{"ALL_PROXY=", "HTTP_PROXY=", "HTTPS_PROXY="} {
		found := false
		for _, value := range recorder.command.Env {
			if strings.HasPrefix(value, key) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("environment does not contain %s", key)
		}
	}
}

func TestImageID(t *testing.T) {
	t.Parallel()
	recorder := &recordingRunner{}
	id, err := (Client{Runner: recorder}).ImageID(context.Background(), "nginx:latest", "")
	if err != nil {
		t.Fatalf("ImageID() error = %v", err)
	}
	want := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(`{"architecture":"amd64"}`)))
	if id != want {
		t.Fatalf("ImageID() = %q, want %q", id, want)
	}
	if recorder.command.Args[0] != "config" || recorder.command.Args[1] != "nginx:latest" {
		t.Fatalf("command args = %v", recorder.command.Args)
	}
}

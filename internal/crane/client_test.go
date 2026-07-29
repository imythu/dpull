package crane

import (
	"context"
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
	if len(command.Args) > 0 && command.Args[0] == "digest" {
		_, _ = fmt.Fprintln(command.Stdout, "sha256:abc")
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

func TestDigest(t *testing.T) {
	t.Parallel()
	recorder := &recordingRunner{}
	digest, err := (Client{Runner: recorder}).Digest(context.Background(), "nginx:latest", "")
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	if digest != "sha256:abc" {
		t.Fatalf("Digest() = %q", digest)
	}
	if recorder.command.Args[0] != "digest" || recorder.command.Args[1] != "nginx:latest" {
		t.Fatalf("command args = %v", recorder.command.Args)
	}
}

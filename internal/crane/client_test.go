package crane

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

func pushRandomImage(t *testing.T) (string, string) {
	t.Helper()
	server := httptest.NewServer(registry.New())
	t.Cleanup(server.Close)
	host := strings.TrimPrefix(server.URL, "http://")
	image := host + "/library/fixture:latest"
	img, err := random.Image(256, 1)
	if err != nil {
		t.Fatalf("random.Image() error = %v", err)
	}
	ref, err := name.ParseReference(image)
	if err != nil {
		t.Fatalf("ParseReference() error = %v", err)
	}
	if err := remote.Write(ref, img); err != nil {
		t.Fatalf("remote.Write() error = %v", err)
	}
	configName, err := img.ConfigName()
	if err != nil {
		t.Fatalf("ConfigName() error = %v", err)
	}
	return image, configName.String()
}

func TestImageIDMatchesConfigDigest(t *testing.T) {
	t.Parallel()
	image, want := pushRandomImage(t)
	id, err := (Client{Insecure: true}).ImageID(context.Background(), image, Options{})
	if err != nil {
		t.Fatalf("ImageID() error = %v", err)
	}
	if id != want {
		t.Fatalf("ImageID() = %q, want %q", id, want)
	}
}

func TestPullWritesLoadableTarball(t *testing.T) {
	t.Parallel()
	image, want := pushRandomImage(t)
	target := filepath.Join(t.TempDir(), "fixture.tar")
	var progress bytes.Buffer
	if err := (Client{Insecure: true, Progress: &progress}).Pull(context.Background(), image, target, Options{}); err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	tag, err := name.NewTag(image)
	if err != nil {
		t.Fatalf("NewTag() error = %v", err)
	}
	img, err := tarball.ImageFromPath(target, &tag)
	if err != nil {
		t.Fatalf("read tarball: %v", err)
	}
	configName, err := img.ConfigName()
	if err != nil {
		t.Fatalf("ConfigName() error = %v", err)
	}
	if configName.String() != want {
		t.Fatalf("tarball image ID = %s, want %s", configName, want)
	}
	if !strings.Contains(progress.String(), "100.0%") {
		t.Fatalf("progress output = %q, want completed bar", progress.String())
	}
}

func TestOptionsRejectInvalidPlatform(t *testing.T) {
	t.Parallel()
	if _, err := (Client{}).options(context.Background(), Options{Platform: "linux/amd64/v1/x"}); err == nil {
		t.Fatal("options() error = nil, want invalid platform error")
	}
}

func TestProxyTransportUsesProxy(t *testing.T) {
	t.Parallel()
	for _, proxy := range []string{"http://127.0.0.1:7890", "socks5://localhost:1080"} {
		transport, err := proxyTransport(proxy)
		if err != nil {
			t.Fatalf("proxyTransport(%q) error = %v", proxy, err)
		}
		got, err := transport.Proxy(&http.Request{URL: &url.URL{Scheme: "https", Host: "registry-1.docker.io"}})
		if err != nil {
			t.Fatalf("Proxy() error = %v", err)
		}
		if got.String() != proxy {
			t.Fatalf("Proxy() = %v, want %s", got, proxy)
		}
	}
}

func TestProxyTransportEmptyIsDirect(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")
	transport, err := proxyTransport("")
	if err != nil {
		t.Fatalf("proxyTransport() error = %v", err)
	}
	if transport.Proxy != nil {
		t.Fatal("transport.Proxy is set, want direct connection ignoring HTTPS_PROXY")
	}
}

func TestImageIDFailsForMissingImage(t *testing.T) {
	t.Parallel()
	image, _ := pushRandomImage(t)
	missing := strings.Replace(image, "fixture", "missing", 1)
	if _, err := (Client{Insecure: true}).ImageID(context.Background(), missing, Options{}); err == nil {
		t.Fatal("ImageID() error = nil, want failure")
	}
}

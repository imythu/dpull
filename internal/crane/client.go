// Package crane pulls images in-process with the go-containerregistry crane
// library, so no external crane binary is required.
package crane

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"time"

	gcrane "github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// Options configures a single registry request.
type Options struct {
	// Proxy is an http://, https:// or socks5:// URL; empty connects
	// directly, ignoring HTTP_PROXY and friends.
	Proxy string
	// Platform is os/arch[/variant]; empty uses DefaultPlatform.
	Platform string
}

// Client talks to container registries.
type Client struct {
	// Progress receives the download progress bar; nil disables it.
	Progress io.Writer
	// Insecure allows plain HTTP registries; intended for tests.
	Insecure bool
}

// DefaultPlatform is the Linux platform matching the host architecture.
func DefaultPlatform() string {
	return "linux/" + runtime.GOARCH
}

// Pull downloads image and writes it to target as a docker-loadable tarball.
func (c Client) Pull(ctx context.Context, image, target string, options Options) error {
	craneOptions, err := c.options(ctx, options)
	if err != nil {
		return fmt.Errorf("pull image %q: %w", image, err)
	}
	img, err := gcrane.Pull(image, craneOptions...)
	if err != nil {
		return fmt.Errorf("pull image %q: %w", image, err)
	}
	tag, err := tarballTag(image)
	if err != nil {
		return fmt.Errorf("pull image %q: %w", image, err)
	}
	if err := c.write(target, tag, img); err != nil {
		return fmt.Errorf("pull image %q: %w", image, err)
	}
	return nil
}

// ImageID returns the Docker image ID of the remote image, which is the
// SHA-256 digest of its config blob.
func (c Client) ImageID(ctx context.Context, image string, options Options) (string, error) {
	craneOptions, err := c.options(ctx, options)
	if err != nil {
		return "", fmt.Errorf("get remote config for image %q: %w", image, err)
	}
	config, err := gcrane.Config(image, craneOptions...)
	if err != nil {
		return "", fmt.Errorf("get remote config for image %q: %w", image, err)
	}
	if len(config) == 0 {
		return "", fmt.Errorf("get remote config for image %q: registry returned empty config", image)
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(config)), nil
}

// write streams the image into a tarball. Layers are fetched lazily while
// writing, so the progress bar tracks the actual download.
func (c Client) write(target string, tag name.Tag, img v1.Image) error {
	if c.Progress == nil {
		return tarball.WriteToFile(target, tag, img)
	}
	bar := newProgressBar(c.Progress, time.Now)
	updates := make(chan v1.Update, 64)
	done := make(chan struct{})
	go func() {
		bar.consume(updates)
		close(done)
	}()
	err := tarball.WriteToFile(target, tag, img, tarball.WithProgress(updates))
	close(updates)
	<-done
	bar.finish()
	return err
}

// tarballTag returns the tag recorded in the archive. Digest references get
// the same placeholder tag the crane CLI uses.
func tarballTag(image string) (name.Tag, error) {
	ref, err := name.ParseReference(image)
	if err != nil {
		return name.Tag{}, fmt.Errorf("parse reference: %w", err)
	}
	switch ref := ref.(type) {
	case name.Tag:
		return ref, nil
	case name.Digest:
		return ref.Tag("i-was-a-digest"), nil
	default:
		return name.Tag{}, fmt.Errorf("unsupported reference %q", image)
	}
}

func (c Client) options(ctx context.Context, options Options) ([]gcrane.Option, error) {
	transport, err := proxyTransport(options.Proxy)
	if err != nil {
		return nil, err
	}
	platformName := options.Platform
	if platformName == "" {
		platformName = DefaultPlatform()
	}
	platform, err := v1.ParsePlatform(platformName)
	if err != nil {
		return nil, fmt.Errorf("parse platform %q: %w", platformName, err)
	}
	craneOptions := []gcrane.Option{
		gcrane.WithContext(ctx),
		gcrane.WithTransport(transport),
		gcrane.WithPlatform(platform),
	}
	if c.Insecure {
		craneOptions = append(craneOptions, gcrane.Insecure)
	}
	return craneOptions, nil
}

// proxyTransport returns an HTTP transport that routes through proxy when
// set and connects directly otherwise. net/http supports http, https and
// socks5 proxy URLs.
func proxyTransport(proxy string) (*http.Transport, error) {
	transport := remote.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if proxy == "" {
		return transport, nil
	}
	proxyURL, err := url.Parse(proxy)
	if err != nil {
		return nil, fmt.Errorf("parse proxy %q: %w", proxy, err)
	}
	transport.Proxy = http.ProxyURL(proxyURL)
	return transport, nil
}

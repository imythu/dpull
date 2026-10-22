// Package crane pulls images in-process with the go-containerregistry crane
// library, so no external crane binary is required.
package crane

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	gcrane "github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
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
	// CacheDir holds layer blobs and resumable partial downloads.
	CacheDir string
	// Progress receives the download progress bar; nil disables it.
	Progress io.Writer
	// Insecure allows plain HTTP registries; intended for tests.
	Insecure bool

	mu       sync.Mutex
	finished map[string]struct{}
}

// DefaultPlatform is the Linux platform matching the host architecture.
func DefaultPlatform() string {
	return "linux/" + runtime.GOARCH
}

// Pull downloads image and writes it to target as a docker-loadable tarball.
// Layers are downloaded into CacheDir first, so an interrupted download is
// retried and resumed instead of starting over.
func (c *Client) Pull(ctx context.Context, image, target string, options Options) error {
	if err := c.pull(ctx, image, target, options); err != nil {
		return fmt.Errorf("pull image %q: %w", image, err)
	}
	return nil
}

func (c *Client) pull(ctx context.Context, image, target string, options Options) error {
	craneOptions, err := c.options(ctx, options)
	if err != nil {
		return err
	}
	ref, err := name.ParseReference(image, c.nameOptions()...)
	if err != nil {
		return fmt.Errorf("parse reference: %w", err)
	}
	remoteImage, err := gcrane.Pull(image, craneOptions...)
	if err != nil {
		return err
	}
	manifest, err := remoteImage.Manifest()
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	// Fetch the config now so writing the archive needs no network access.
	if _, err := remoteImage.RawConfigFile(); err != nil {
		return fmt.Errorf("read image config: %w", err)
	}
	store, err := c.blobStore(ctx, ref.Context(), options.Proxy)
	if err != nil {
		return err
	}
	for _, layer := range manifest.Layers {
		store.bar.total += layer.Size
	}
	paths := make(map[v1.Hash]string, len(manifest.Layers))
	for _, layer := range manifest.Layers {
		path, err := store.fetch(ctx, layer)
		if err != nil {
			store.bar.finish()
			return err
		}
		paths[layer.Digest] = path
	}
	store.bar.finish()
	c.markFinished(paths)
	tag, err := tarballTag(ref)
	if err != nil {
		return err
	}
	local, err := partial.CompressedToImage(&cachedImage{remote: remoteImage, manifest: manifest, paths: paths})
	if err != nil {
		return fmt.Errorf("assemble image: %w", err)
	}
	if err := tarball.WriteToFile(target, tag, local); err != nil {
		return fmt.Errorf("write archive: %w", err)
	}
	return nil
}

// Prune removes the completed layer blobs that this client downloaded or
// reused. Unfinished partial downloads stay so a later run can resume them;
// the cache cleanup removes them once they expire.
func (c *Client) Prune() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var failures []error
	for path := range c.finished {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			failures = append(failures, err)
		}
	}
	c.finished = nil
	if err := errors.Join(failures...); err != nil {
		return fmt.Errorf("remove downloaded layers: %w", err)
	}
	return nil
}

func (c *Client) markFinished(paths map[v1.Hash]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finished == nil {
		c.finished = make(map[string]struct{}, len(paths))
	}
	for _, path := range paths {
		c.finished[path] = struct{}{}
	}
}

func (c *Client) blobStore(ctx context.Context, repo name.Repository, proxy string) (*blobStore, error) {
	if c.CacheDir == "" {
		return nil, errors.New("layer cache directory is not configured")
	}
	if err := os.MkdirAll(c.CacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create layer cache: %w", err)
	}
	base, err := proxyTransport(proxy)
	if err != nil {
		return nil, err
	}
	auth, err := authn.DefaultKeychain.Resolve(repo)
	if err != nil {
		return nil, fmt.Errorf("resolve registry credentials: %w", err)
	}
	authorized, err := transport.NewWithContext(ctx, repo.Registry, auth, base, []string{repo.Scope(transport.PullScope)})
	if err != nil {
		return nil, fmt.Errorf("authenticate to registry: %w", err)
	}
	progress := c.Progress
	if progress == nil {
		progress = io.Discard
	}
	bar := newProgressBar(progress, time.Now)
	return newBlobStore(c.CacheDir, &http.Client{Transport: authorized}, repo, bar), nil
}

// ImageID returns the Docker image ID of the remote image, which is the
// SHA-256 digest of its config blob.
func (c *Client) ImageID(ctx context.Context, image string, options Options) (string, error) {
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

func (c *Client) nameOptions() []name.Option {
	if c.Insecure {
		return []name.Option{name.Insecure}
	}
	return nil
}

// tarballTag returns the tag recorded in the archive. Digest references get
// the same placeholder tag the crane CLI uses.
func tarballTag(ref name.Reference) (name.Tag, error) {
	switch ref := ref.(type) {
	case name.Tag:
		return ref, nil
	case name.Digest:
		return ref.Tag("i-was-a-digest"), nil
	default:
		return name.Tag{}, fmt.Errorf("unsupported reference %q", ref)
	}
}

func (c *Client) options(ctx context.Context, options Options) ([]gcrane.Option, error) {
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

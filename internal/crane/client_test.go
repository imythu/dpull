package crane

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// fixture is a test registry holding one random single-layer image. It can
// cut layer downloads short to simulate a dropped connection.
type fixture struct {
	image   string
	imageID string
	layer   v1.Descriptor
	content []byte

	mu       sync.Mutex
	ranges   []string
	failures int
	cutAt    int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{}
	server := httptest.NewServer(f.wrap(registry.New()))
	t.Cleanup(server.Close)
	f.image = strings.TrimPrefix(server.URL, "http://") + "/library/fixture:latest"
	img, err := random.Image(512<<10, 1)
	if err != nil {
		t.Fatalf("random.Image() error = %v", err)
	}
	ref, err := name.ParseReference(f.image)
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
	f.imageID = configName.String()
	manifest, err := img.Manifest()
	if err != nil {
		t.Fatalf("Manifest() error = %v", err)
	}
	layers, err := img.Layers()
	if err != nil {
		t.Fatalf("Layers() error = %v", err)
	}
	reader, err := layers[0].Compressed()
	if err != nil {
		t.Fatalf("Compressed() error = %v", err)
	}
	defer func() { _ = reader.Close() }()
	if f.content, err = io.ReadAll(reader); err != nil {
		t.Fatalf("read layer: %v", err)
	}
	f.mu.Lock()
	f.layer = manifest.Layers[0]
	f.mu.Unlock()
	return f
}

// failNext makes the next count layer downloads stop after cutAt bytes.
func (f *fixture) failNext(count, cutAt int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures, f.cutAt = count, cutAt
}

func (f *fixture) layerRanges() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ranges...)
}

func (f *fixture) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		isLayer := r.Method == http.MethodGet && f.layer.Digest.Hex != "" &&
			strings.HasSuffix(r.URL.Path, "/blobs/"+f.layer.Digest.String())
		if !isLayer {
			f.mu.Unlock()
			next.ServeHTTP(w, r)
			return
		}
		f.ranges = append(f.ranges, r.Header.Get("Range"))
		cut := f.failures > 0
		if cut {
			f.failures--
		}
		cutAt := f.cutAt
		f.mu.Unlock()
		if !cut {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(&truncatingWriter{ResponseWriter: w, remaining: cutAt}, r)
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	})
}

type truncatingWriter struct {
	http.ResponseWriter
	remaining int
}

func (w *truncatingWriter) Write(data []byte) (int, error) {
	if len(data) > w.remaining {
		data = data[:w.remaining]
	}
	w.remaining -= len(data)
	return w.ResponseWriter.Write(data)
}

func (f *fixture) client(t *testing.T, progress io.Writer) *Client {
	t.Helper()
	return &Client{Insecure: true, CacheDir: filepath.Join(t.TempDir(), "blobs"), Progress: progress}
}

func (f *fixture) blobPath(client *Client) string {
	return filepath.Join(client.CacheDir, "sha256-"+f.layer.Digest.Hex)
}

func (f *fixture) fullRange(offset int) string {
	return "bytes=" + strconv.Itoa(offset) + "-" + strconv.FormatInt(f.layer.Size-1, 10)
}

func (f *fixture) pullAndCheck(t *testing.T, client *Client) {
	t.Helper()
	target := filepath.Join(t.TempDir(), "fixture.tar")
	if err := client.Pull(context.Background(), f.image, target, Options{}); err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	tag, err := name.NewTag(f.image)
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
	if configName.String() != f.imageID {
		t.Fatalf("tarball image ID = %s, want %s", configName, f.imageID)
	}
	layers, err := img.Layers()
	if err != nil {
		t.Fatalf("Layers() error = %v", err)
	}
	reader, err := layers[0].Compressed()
	if err != nil {
		t.Fatalf("Compressed() error = %v", err)
	}
	defer func() { _ = reader.Close() }()
	content, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(content, f.content) {
		t.Fatalf("tarball layer differs from registry layer (err = %v)", err)
	}
}

func TestImageIDMatchesConfigDigest(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id, err := f.client(t, nil).ImageID(context.Background(), f.image, Options{})
	if err != nil {
		t.Fatalf("ImageID() error = %v", err)
	}
	if id != f.imageID {
		t.Fatalf("ImageID() = %q, want %q", id, f.imageID)
	}
}

func TestPullWritesLoadableTarball(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	var progress bytes.Buffer
	f.pullAndCheck(t, f.client(t, &progress))
	if !strings.Contains(progress.String(), "100.0%") {
		t.Fatalf("progress output = %q, want completed bar", progress.String())
	}
}

func TestPullRetriesAndResumesDroppedDownload(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.failNext(1, 200<<10)
	var progress bytes.Buffer
	f.pullAndCheck(t, f.client(t, &progress))
	want := []string{"", f.fullRange(200 << 10)}
	if ranges := f.layerRanges(); strings.Join(ranges, "|") != strings.Join(want, "|") {
		t.Fatalf("layer request ranges = %q, want %q", ranges, want)
	}
	if !strings.Contains(progress.String(), "retrying in 1s (attempt 2/5)") {
		t.Fatalf("progress output = %q, want retry notice", progress.String())
	}
}

func TestPullResumesPartialFromPreviousRun(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	var progress bytes.Buffer
	client := f.client(t, &progress)
	half := len(f.content) / 2
	writeFile(t, f.blobPath(client)+partialSuffix, f.content[:half])
	f.pullAndCheck(t, client)
	if ranges := f.layerRanges(); len(ranges) != 1 || ranges[0] != f.fullRange(half) {
		t.Fatalf("layer request ranges = %q, want one resumed request", ranges)
	}
	if !strings.Contains(progress.String(), "resuming "+f.layer.Digest.Hex[:12]) {
		t.Fatalf("progress output = %q, want resume notice", progress.String())
	}
}

func TestPullRestartsCorruptPartial(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	client := f.client(t, nil)
	writeFile(t, f.blobPath(client)+partialSuffix, bytes.Repeat([]byte{'x'}, len(f.content)/2))
	f.pullAndCheck(t, client)
	if ranges := f.layerRanges(); len(ranges) != 2 || ranges[1] != "" {
		t.Fatalf("layer request ranges = %q, want resume then full restart", ranges)
	}
}

func TestPullReleasesPartialWhenCancelled(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.failNext(downloadAttempts, 100<<10)
	client := f.client(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := client.Pull(ctx, f.image, filepath.Join(t.TempDir(), "x.tar"), Options{}); err == nil {
		t.Fatal("Pull() error = nil, want failure")
	}
	info, err := os.Stat(f.blobPath(client) + partialSuffix)
	if err != nil || info.Size() != 100<<10 {
		t.Fatalf("released partial = %v, %v; want %d bytes kept for resuming", info, err, 100<<10)
	}
}

func TestPruneRemovesFinishedBlobsOnly(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	client := f.client(t, nil)
	f.pullAndCheck(t, client)
	other := filepath.Join(client.CacheDir, "sha256-other"+partialSuffix)
	writeFile(t, other, []byte("partial"))
	if err := client.Prune(); err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if _, err := os.Stat(f.blobPath(client)); !os.IsNotExist(err) {
		t.Fatalf("finished blob still exists: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("unrelated partial removed: %v", err)
	}
}

func TestClaimSkipsActiveDownloadsAndTakesStaleOnes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	final := filepath.Join(dir, "sha256-abc")
	active := final + partialSuffix + ".active"
	stale := final + partialSuffix + ".crashed"
	writeFile(t, active, []byte("active download, bigger"))
	writeFile(t, stale, []byte("stale"))
	old := time.Now().Add(-staleAfter - time.Minute)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}
	store := &blobStore{dir: dir, owner: "me"}
	path, offset, err := store.claim(final)
	if err != nil {
		t.Fatalf("claim() error = %v", err)
	}
	if path != final+partialSuffix+".me" || offset != int64(len("stale")) {
		t.Fatalf("claim() = %q, %d; want stale partial", path, offset)
	}
	if _, err := os.Stat(active); err != nil {
		t.Fatalf("active download was taken over: %v", err)
	}
}

func TestOptionsRejectInvalidPlatform(t *testing.T) {
	t.Parallel()
	if _, err := (&Client{}).options(context.Background(), Options{Platform: "linux/amd64/v1/x"}); err == nil {
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
	f := newFixture(t)
	missing := strings.Replace(f.image, "fixture", "missing", 1)
	if _, err := f.client(t, nil).ImageID(context.Background(), missing, Options{}); err == nil {
		t.Fatal("ImageID() error = nil, want failure")
	}
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

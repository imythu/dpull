package craneinstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallDownloadsAndExtractsCrane(t *testing.T) {
	t.Parallel()
	archive := craneArchive(t, "crane", []byte("binary"))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(archive)
	}))
	defer server.Close()
	var output bytes.Buffer
	installer := &Installer{
		BinDir: t.TempDir(), GOOS: "linux", GOARCH: "amd64", BaseURL: server.URL,
		Client: server.Client(), Input: strings.NewReader("y\n"), Output: &output,
		LookPath: func(string) (string, error) { return "", os.ErrNotExist },
	}
	path, err := installer.Install(context.Background(), "")
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read installed crane: %v", err)
	}
	if string(content) != "binary" {
		t.Fatalf("installed content = %q", content)
	}
}

func TestEnsurePromptsAndUsesManagedBinary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	managed := filepath.Join(dir, "crane")
	if err := os.WriteFile(managed, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write managed binary: %v", err)
	}
	installer := &Installer{
		BinDir: dir, GOOS: "linux", GOARCH: "amd64",
		Input: strings.NewReader("n\n"), Output: &bytes.Buffer{},
		LookPath: func(string) (string, error) { return "", os.ErrNotExist },
	}
	path, err := installer.Ensure(context.Background(), "http://127.0.0.1:7890")
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if path != managed {
		t.Fatalf("Ensure() = %q, want %q", path, managed)
	}
}

func TestDownloadClientUsesProxy(t *testing.T) {
	t.Parallel()
	installer := &Installer{Client: &http.Client{}}
	client, err := installer.downloadClient("http://127.0.0.1:7890")
	if err != nil {
		t.Fatalf("downloadClient() error = %v", err)
	}
	transport := client.Transport.(*http.Transport)
	request := httptest.NewRequest(http.MethodGet, "https://github.com/example", nil)
	proxyURL, err := transport.Proxy(request)
	if err != nil {
		t.Fatalf("Proxy() error = %v", err)
	}
	if proxyURL.String() != "http://127.0.0.1:7890" {
		t.Fatalf("proxy = %q", proxyURL)
	}
}

func craneArchive(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	header := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
	if err := tarWriter.WriteHeader(header); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tarWriter.Write(content); err != nil {
		t.Fatalf("write tar content: %v", err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return buffer.Bytes()
}

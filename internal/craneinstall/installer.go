package craneinstall

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	releaseBaseURL = "https://github.com/google/go-containerregistry/releases/latest/download"
	maxDownload    = 100 << 20
)

type Installer struct {
	BinDir   string
	GOOS     string
	GOARCH   string
	BaseURL  string
	Client   *http.Client
	Input    io.Reader
	Output   io.Writer
	LookPath func(string) (string, error)
}

func Default() (*Installer, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user home for crane installation: %w", err)
	}
	return &Installer{
		BinDir: filepath.Join(home, ".dpull", "bin"), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		BaseURL: releaseBaseURL, Client: &http.Client{Timeout: 5 * time.Minute},
		Input: os.Stdin, Output: os.Stdout, LookPath: exec.LookPath,
	}, nil
}

func (i *Installer) Ensure(ctx context.Context, proxyURL string) (string, error) {
	if path, found := i.Resolve(); found {
		return path, nil
	}
	target := i.targetPath()
	_, _ = fmt.Fprintf(i.Output, "\n[Dependency]\n  %-10s crane not found\n  %-10s %s\n  %-10s %s\n\nInstall crane now? [y/N] ", "Status:", "Proxy:", proxyURL, "Target:", target)
	answer, err := bufio.NewReader(i.Input).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read crane installation confirmation: %w", err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		return "", fmt.Errorf("crane is required; installation declined")
	}
	return i.Install(ctx, proxyURL)
}

func (i *Installer) Resolve() (string, bool) {
	if path, err := i.LookPath("crane"); err == nil {
		return path, true
	}
	target := i.targetPath()
	info, err := os.Stat(target)
	return target, err == nil && !info.IsDir()
}

func (i *Installer) Install(ctx context.Context, proxyURL string) (string, error) {
	archiveURL, err := i.archiveURL()
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return "", fmt.Errorf("create crane download request: %w", err)
	}
	client, err := i.downloadClient(proxyURL)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download crane from %q: %w", archiveURL, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download crane from %q: HTTP %s", archiveURL, response.Status)
	}
	binary, err := extractCrane(io.LimitReader(response.Body, maxDownload+1), i.GOOS)
	if err != nil {
		return "", fmt.Errorf("extract crane archive: %w", err)
	}
	if len(binary) > maxDownload {
		return "", fmt.Errorf("extract crane archive: binary exceeds %d bytes", maxDownload)
	}
	return i.writeBinary(binary)
}

func (i *Installer) downloadClient(proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		return i.Client, nil
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("parse crane download proxy %q: %w", proxyURL, err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if existing, ok := i.Client.Transport.(*http.Transport); ok {
		transport = existing.Clone()
	}
	transport.Proxy = http.ProxyURL(parsed)
	client := *i.Client
	client.Transport = transport
	return &client, nil
}

func (i *Installer) archiveURL() (string, error) {
	osName, archName, err := releasePlatform(i.GOOS, i.GOARCH)
	if err != nil {
		return "", err
	}
	asset := fmt.Sprintf("go-containerregistry_%s_%s.tar.gz", osName, archName)
	return strings.TrimRight(i.BaseURL, "/") + "/" + asset, nil
}

func releasePlatform(goos, goarch string) (string, string, error) {
	osNames := map[string]string{"linux": "Linux", "darwin": "Darwin", "windows": "Windows"}
	archNames := map[string]string{"amd64": "x86_64", "arm64": "arm64"}
	osName, osOK := osNames[goos]
	archName, archOK := archNames[goarch]
	if !osOK || !archOK {
		return "", "", fmt.Errorf("install crane: unsupported platform %s/%s", goos, goarch)
	}
	return osName, archName, nil
}

func extractCrane(reader io.Reader, goos string) ([]byte, error) {
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return nil, fmt.Errorf("open gzip stream: %w", err)
	}
	defer func() { _ = gzipReader.Close() }()
	wanted := "crane"
	if goos == "windows" {
		wanted = "crane.exe"
	}
	tarReader := tar.NewReader(gzipReader)
	for {
		header, nextErr := tarReader.Next()
		if nextErr == io.EOF {
			return nil, fmt.Errorf("archive does not contain %s", wanted)
		}
		if nextErr != nil {
			return nil, fmt.Errorf("read tar entry: %w", nextErr)
		}
		if filepath.Base(header.Name) == wanted && header.Typeflag == tar.TypeReg {
			binary, readErr := io.ReadAll(io.LimitReader(tarReader, maxDownload+1))
			if readErr != nil {
				return nil, fmt.Errorf("read %s from archive: %w", wanted, readErr)
			}
			return binary, nil
		}
	}
}

func (i *Installer) writeBinary(binary []byte) (string, error) {
	if err := os.MkdirAll(i.BinDir, 0o755); err != nil {
		return "", fmt.Errorf("create crane bin directory %q: %w", i.BinDir, err)
	}
	target := i.targetPath()
	temp, err := os.CreateTemp(i.BinDir, ".crane-*")
	if err != nil {
		return "", fmt.Errorf("create temporary crane binary: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if _, err := temp.Write(binary); err != nil {
		_ = temp.Close()
		return "", fmt.Errorf("write temporary crane binary: %w", err)
	}
	if err := temp.Close(); err != nil {
		return "", fmt.Errorf("close temporary crane binary: %w", err)
	}
	if err := os.Chmod(tempPath, 0o755); err != nil {
		return "", fmt.Errorf("make crane executable: %w", err)
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("replace existing crane binary %q: %w", target, err)
	}
	if err := os.Rename(tempPath, target); err != nil {
		return "", fmt.Errorf("install crane binary %q: %w", target, err)
	}
	_, _ = fmt.Fprintf(i.Output, "  %-10s installed\n  %-10s %s\n", "Status:", "Path:", target)
	return target, nil
}

func (i *Installer) targetPath() string {
	name := "crane"
	if i.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(i.BinDir, name)
}

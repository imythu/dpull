package crane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

const (
	downloadAttempts = 5
	firstRetryDelay  = time.Second
	maxRetryDelay    = 15 * time.Second
	// idleTimeout fails an attempt when no bytes arrive for this long.
	idleTimeout = 30 * time.Second
	// staleAfter is how long an unreleased partial download must be idle
	// before another run may take it over, e.g. after a crash.
	staleAfter = 2 * time.Minute

	partialSuffix = ".partial"
)

var errStalled = errors.New("download stalled")

// blobStore downloads layer blobs into dir with resumable, verified,
// retried transfers. Completed blobs are named <algorithm>-<hex>; partial
// downloads are <name>.partial when released and <name>.partial.<owner>
// while a run is writing them.
type blobStore struct {
	dir    string
	owner  string
	client *http.Client
	repo   name.Repository
	bar    *progressBar
	sleep  func(context.Context, time.Duration) error
}

func newBlobStore(dir string, client *http.Client, repo name.Repository, bar *progressBar) *blobStore {
	owner := strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	return &blobStore{dir: dir, owner: owner, client: client, repo: repo, bar: bar, sleep: sleepContext}
}

// fetch returns the path of the verified blob, downloading what is missing.
func (s *blobStore) fetch(ctx context.Context, desc v1.Descriptor) (string, error) {
	final := filepath.Join(s.dir, desc.Digest.Algorithm+"-"+desc.Digest.Hex)
	if info, err := os.Stat(final); err == nil && info.Size() == desc.Size {
		now := time.Now()
		_ = os.Chtimes(final, now, now)
		s.bar.reuse(desc.Size)
		return final, nil
	}
	path, offset, err := s.claim(final)
	if err != nil {
		return "", err
	}
	defer s.release(path, final)
	if offset > 0 {
		s.bar.notice(fmt.Sprintf("resuming %s from %s / %s", shortDigest(desc.Digest), formatBytes(offset), formatBytes(desc.Size)))
	}
	s.bar.reuse(offset)
	delay := firstRetryDelay
	for attempt := 1; ; attempt++ {
		err = s.download(ctx, desc, path)
		if err == nil {
			err = verify(path, desc)
			if err != nil {
				s.restart(path)
			}
		}
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return "", fmt.Errorf("download interrupted; the partial layer is kept for resuming within 1 hour: %w", ctx.Err())
		}
		if !retryable(err) || attempt == downloadAttempts {
			return "", fmt.Errorf("download layer %s: %w", desc.Digest, err)
		}
		s.bar.notice(fmt.Sprintf("layer %s: %v; retrying in %s (attempt %d/%d)",
			shortDigest(desc.Digest), err, delay, attempt+1, downloadAttempts))
		if err := s.sleep(ctx, delay); err != nil {
			return "", err
		}
		delay = min(delay*2, maxRetryDelay)
	}
	if err := os.Rename(path, final); err != nil {
		return "", fmt.Errorf("store layer %s: %w", desc.Digest, err)
	}
	return final, nil
}

// claim takes ownership of the largest resumable partial download for final,
// or of a new empty one, by renaming it to a name only this run uses.
func (s *blobStore) claim(final string) (string, int64, error) {
	own := final + partialSuffix + "." + s.owner
	candidates, err := filepath.Glob(final + partialSuffix + "*")
	if err != nil {
		return "", 0, fmt.Errorf("find partial downloads: %w", err)
	}
	var best string
	var bestSize int64 = -1
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		released := candidate == final+partialSuffix
		if !released && time.Since(info.ModTime()) < staleAfter {
			continue // another run is downloading it
		}
		if info.Size() > bestSize {
			best, bestSize = candidate, info.Size()
		}
	}
	if best != "" && os.Rename(best, own) == nil {
		return own, bestSize, nil
	}
	file, err := os.OpenFile(own, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", 0, fmt.Errorf("create partial download: %w", err)
	}
	_ = file.Close()
	return own, 0, nil
}

// release hands an unfinished download back so a later run can resume it.
func (s *blobStore) release(path, final string) {
	if _, err := os.Stat(path); err == nil {
		_ = os.Rename(path, final+partialSuffix)
	}
}

func (s *blobStore) download(ctx context.Context, desc v1.Descriptor, path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open partial download: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect partial download: %w", err)
	}
	offset := info.Size()
	if offset > desc.Size {
		if err := s.truncate(file, offset); err != nil {
			return err
		}
		offset = 0
	}
	if offset == desc.Size {
		return nil
	}
	requestCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, s.blobURL(desc.Digest), nil)
	if err != nil {
		return fmt.Errorf("build blob request: %w", err)
	}
	if offset > 0 {
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, desc.Size-1))
	}
	watchdog := time.AfterFunc(idleTimeout, func() { cancel(errStalled) })
	defer watchdog.Stop()
	response, err := s.client.Do(request)
	if err != nil {
		return stallCause(requestCtx, err)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusPartialContent && offset > 0:
	case response.StatusCode == http.StatusOK:
		// The server ignored the range request, so start over.
		if offset > 0 {
			if err := s.truncate(file, offset); err != nil {
				return err
			}
			offset = 0
		}
	case response.StatusCode == http.StatusRequestedRangeNotSatisfiable:
		if err := s.truncate(file, offset); err != nil {
			return err
		}
		return statusError{code: response.StatusCode}
	default:
		return statusError{code: response.StatusCode}
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("seek partial download: %w", err)
	}
	buffer := make([]byte, 256<<10)
	for {
		n, readErr := response.Body.Read(buffer)
		if n > 0 {
			watchdog.Reset(idleTimeout)
			if _, err := file.Write(buffer[:n]); err != nil {
				return fmt.Errorf("write partial download: %w", err)
			}
			s.advance(int64(n))
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return stallCause(requestCtx, readErr)
		}
	}
}

// restart discards a corrupt partial download.
func (s *blobStore) restart(path string) {
	if info, err := os.Stat(path); err == nil {
		s.advance(-info.Size())
	}
	_ = os.Truncate(path, 0)
}

func (s *blobStore) truncate(file *os.File, size int64) error {
	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("reset partial download: %w", err)
	}
	s.advance(-size)
	return nil
}

func (s *blobStore) advance(delta int64) {
	s.bar.add(delta)
}

func (s *blobStore) blobURL(digest v1.Hash) string {
	blob := url.URL{
		Scheme: s.repo.Scheme(),
		Host:   s.repo.RegistryStr(),
		Path:   fmt.Sprintf("/v2/%s/blobs/%s", s.repo.RepositoryStr(), digest),
	}
	return blob.String()
}

func verify(path string, desc v1.Descriptor) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open downloaded layer: %w", err)
	}
	defer func() { _ = file.Close() }()
	if desc.Digest.Algorithm != "sha256" {
		return nil
	}
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return fmt.Errorf("read downloaded layer: %w", err)
	}
	if size != desc.Size || hex.EncodeToString(hash.Sum(nil)) != desc.Digest.Hex {
		return errDigestMismatch
	}
	return nil
}

var errDigestMismatch = errors.New("downloaded data does not match the layer digest")

type statusError struct {
	code int
}

func (e statusError) Error() string {
	return fmt.Sprintf("registry returned HTTP %d %s", e.code, http.StatusText(e.code))
}

func retryable(err error) bool {
	var status statusError
	if errors.As(err, &status) {
		return status.code == http.StatusRequestTimeout || status.code == http.StatusTooManyRequests ||
			status.code == http.StatusRequestedRangeNotSatisfiable || status.code >= 500
	}
	// Network errors, stalls and corrupt data are all worth another try.
	return true
}

func stallCause(ctx context.Context, err error) error {
	if errors.Is(context.Cause(ctx), errStalled) {
		return fmt.Errorf("%w: no data for %s", errStalled, idleTimeout)
	}
	return err
}

func shortDigest(digest v1.Hash) string {
	return strings.TrimPrefix(digest.String(), "sha256:")[:12]
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

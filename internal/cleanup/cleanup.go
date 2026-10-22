package cleanup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

type WarningFunc func(format string, args ...any)

func All(root string) (int, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read cache root %q: %w", root, err)
	}
	removed := 0
	var failures []error
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			failures = append(failures, fmt.Errorf("remove cache entry %q: %w", path, err))
			continue
		}
		removed++
	}
	if err := errors.Join(failures...); err != nil {
		return removed, fmt.Errorf("clean all downloads: %w", err)
	}
	return removed, nil
}

// Expired removes run directories under root older than maxAge. Directories
// named in fileDirs are shared caches: only their files older than maxAge are
// removed, so entries in active use survive.
func Expired(root string, now time.Time, maxAge time.Duration, warn WarningFunc, fileDirs ...string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read cache root %q: %w", root, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if slices.Contains(fileDirs, entry.Name()) {
			expireFiles(path, now, maxAge, warn)
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			warn("inspect cache %s: %v", entry.Name(), infoErr)
			continue
		}
		if now.Sub(info.ModTime()) <= maxAge {
			continue
		}
		if removeErr := os.RemoveAll(path); removeErr != nil {
			warn("remove expired cache %s: %v", path, removeErr)
		}
	}
	return nil
}

func expireFiles(dir string, now time.Time, maxAge time.Duration, warn WarningFunc) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		warn("read cache %s: %v", dir, err)
		return
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) <= maxAge {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			warn("remove expired cache %s: %v", path, err)
		}
	}
}

package cleanup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExpiredOnlyRemovesOldFirstLevelDirectories(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	now := time.Now()
	oldDir := filepath.Join(root, "old")
	freshDir := filepath.Join(root, "fresh")
	for _, dir := range []string{oldDir, freshDir} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatalf("create fixture directory: %v", err)
		}
	}
	oldFile := filepath.Join(root, "unrelated.txt")
	if err := os.WriteFile(oldFile, []byte("keep"), 0o600); err != nil {
		t.Fatalf("create fixture file: %v", err)
	}
	oldTime := now.Add(-2 * time.Hour)
	if err := os.Chtimes(oldDir, oldTime, oldTime); err != nil {
		t.Fatalf("age directory: %v", err)
	}
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatalf("age file: %v", err)
	}
	if err := Expired(root, now, time.Hour, t.Errorf); err != nil {
		t.Fatalf("Expired() error = %v", err)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("old directory still exists, stat error = %v", err)
	}
	for _, path := range []string{freshDir, oldFile} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %q to remain: %v", path, err)
		}
	}
}

func TestAllRemovesFilesAndDirectories(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	directory := filepath.Join(root, "run")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatalf("create run directory: %v", err)
	}
	for _, path := range []string{filepath.Join(directory, "partial.tar"), filepath.Join(root, "image.tar")} {
		if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
			t.Fatalf("create cache fixture: %v", err)
		}
	}
	removed, err := All(root)
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if removed != 2 {
		t.Fatalf("All() removed = %d, want 2", removed)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read cleaned root: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("cache entries remain: %v", entries)
	}
}

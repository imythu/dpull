package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCreateRunDir(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), ".crane")
	manager := New(root)
	manager.Now = func() time.Time { return time.Unix(0, 12345) }
	dir, err := manager.CreateRunDir()
	if err != nil {
		t.Fatalf("CreateRunDir() error = %v", err)
	}
	if dir != filepath.Join(root, "12345") {
		t.Fatalf("CreateRunDir() = %q", dir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat run directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("run path is not a directory")
	}
}

func TestRemoveIfEmptyPreservesNonEmptyDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "image.tar"), []byte("tar"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := RemoveIfEmpty(dir); err != nil {
		t.Fatalf("RemoveIfEmpty() error = %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("non-empty directory was removed: %v", err)
	}
}

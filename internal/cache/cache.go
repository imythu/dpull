package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Manager struct {
	Root string
	Now  func() time.Time
}

func New(root string) *Manager {
	return &Manager{Root: root, Now: time.Now}
}

func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".crane"), nil
}

func (m *Manager) CreateRoot() error {
	if err := os.MkdirAll(m.Root, 0o755); err != nil {
		return fmt.Errorf("create cache root %q: %w", m.Root, err)
	}
	return nil
}

func (m *Manager) CreateRunDir() (string, error) {
	if err := m.CreateRoot(); err != nil {
		return "", err
	}
	for offset := int64(0); offset < 1000; offset++ {
		name := strconv.FormatInt(m.Now().UnixNano()+offset, 10)
		path := filepath.Join(m.Root, name)
		err := os.Mkdir(path, 0o755)
		if err == nil {
			return path, nil
		}
		if !os.IsExist(err) {
			return "", fmt.Errorf("create run cache %q: %w", path, err)
		}
	}
	return "", fmt.Errorf("create unique run cache under %q", m.Root)
}

func RemoveFile(path string) error {
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove cache file %q: %w", path, err)
	}
	return nil
}

func RemoveIfEmpty(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read run cache %q: %w", path, err)
	}
	if len(entries) == 0 {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove empty run cache %q: %w", path, err)
		}
	}
	return nil
}

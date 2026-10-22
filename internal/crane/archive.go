package crane

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// writeArchive writes a `docker save` style archive from the cached layer
// files in paths. Each layer file is closed as soon as it is copied, so the
// blob cache can be pruned afterwards; tarball.Write leaves them open, which
// makes deleting them fail on Windows.
func writeArchive(target string, tag name.Tag, config []byte, layers []v1.Descriptor, paths map[v1.Hash]string) (err error) {
	file, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close archive: %w", closeErr)
		}
	}()
	archive := tar.NewWriter(file)
	configName := fmt.Sprintf("sha256:%x", sha256.Sum256(config))
	if err := writeEntry(archive, configName, bytes.NewReader(config), int64(len(config))); err != nil {
		return err
	}
	names := make([]string, len(layers))
	written := make(map[v1.Hash]bool, len(layers))
	for i, layer := range layers {
		names[i] = layer.Digest.Hex + ".tar.gz"
		path, ok := paths[layer.Digest]
		if !ok {
			return fmt.Errorf("layer %s was not downloaded", layer.Digest)
		}
		if written[layer.Digest] {
			continue
		}
		if err := copyLayer(archive, names[i], path, layer.Size); err != nil {
			return err
		}
		written[layer.Digest] = true
	}
	manifest, err := json.Marshal(tarball.Manifest{{
		Config:   configName,
		RepoTags: []string{tag.String()},
		Layers:   names,
	}})
	if err != nil {
		return fmt.Errorf("encode archive manifest: %w", err)
	}
	if err := writeEntry(archive, "manifest.json", bytes.NewReader(manifest), int64(len(manifest))); err != nil {
		return err
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("finish archive: %w", err)
	}
	return nil
}

func copyLayer(archive *tar.Writer, entry, path string, size int64) error {
	layer, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open cached layer: %w", err)
	}
	defer func() { _ = layer.Close() }()
	return writeEntry(archive, entry, layer, size)
}

func writeEntry(archive *tar.Writer, entry string, content io.Reader, size int64) error {
	header := &tar.Header{Name: entry, Mode: 0o644, Typeflag: tar.TypeReg, Size: size}
	if err := archive.WriteHeader(header); err != nil {
		return fmt.Errorf("write archive entry %s: %w", entry, err)
	}
	written, err := io.Copy(archive, content)
	if err != nil {
		return fmt.Errorf("write archive entry %s: %w", entry, err)
	}
	if written != size {
		return fmt.Errorf("write archive entry %s: %w", entry, errors.New("size changed while copying"))
	}
	return nil
}

package crane

import (
	"fmt"
	"io"
	"os"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// cachedImage serves the remote manifest and config with layers read from
// the local blob cache.
type cachedImage struct {
	remote   v1.Image
	manifest *v1.Manifest
	paths    map[v1.Hash]string
}

func (i *cachedImage) RawConfigFile() ([]byte, error) { return i.remote.RawConfigFile() }

func (i *cachedImage) MediaType() (types.MediaType, error) { return i.remote.MediaType() }

func (i *cachedImage) RawManifest() ([]byte, error) { return i.remote.RawManifest() }

func (i *cachedImage) LayerByDigest(digest v1.Hash) (partial.CompressedLayer, error) {
	for _, layer := range i.manifest.Layers {
		if layer.Digest == digest {
			return &cachedLayer{descriptor: layer, path: i.paths[digest]}, nil
		}
	}
	return nil, fmt.Errorf("layer %s is not in the image manifest", digest)
}

type cachedLayer struct {
	descriptor v1.Descriptor
	path       string
}

func (l *cachedLayer) Digest() (v1.Hash, error) { return l.descriptor.Digest, nil }

func (l *cachedLayer) Size() (int64, error) { return l.descriptor.Size, nil }

func (l *cachedLayer) MediaType() (types.MediaType, error) { return l.descriptor.MediaType, nil }

func (l *cachedLayer) Compressed() (io.ReadCloser, error) {
	file, err := os.Open(l.path)
	if err != nil {
		return nil, fmt.Errorf("open cached layer: %w", err)
	}
	return file, nil
}

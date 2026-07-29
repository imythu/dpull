package compose

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

var DefaultFiles = []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"}

func Find() (string, error) {
	for _, path := range DefaultFiles {
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect compose file %q: %w", path, err)
		}
	}
	return "", fmt.Errorf("find compose file: none of %v exists", DefaultFiles)
}

func Parse(reader io.Reader) ([]string, error) {
	var config yaml.Node
	decoder := yaml.NewDecoder(reader)
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("decode compose YAML: %w", err)
	}
	services := mappingValue(documentRoot(&config), "services")
	if services == nil || services.Kind != yaml.MappingNode {
		return []string{}, nil
	}
	seen := make(map[string]struct{})
	images := make([]string, 0, len(services.Content)/2)
	for index := 1; index < len(services.Content); index += 2 {
		imageNode := mappingValue(services.Content[index], "image")
		if imageNode == nil || imageNode.Value == "" {
			continue
		}
		image := imageNode.Value
		if _, exists := seen[image]; exists {
			continue
		}
		seen[image] = struct{}{}
		images = append(images, image)
	}
	return images, nil
}

func documentRoot(node *yaml.Node) *yaml.Node {
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return node.Content[0]
	}
	return node
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return node.Content[index+1]
		}
	}
	return nil
}

func ParseFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open compose file %q: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	images, err := Parse(file)
	if err != nil {
		return nil, fmt.Errorf("parse compose file %q: %w", path, err)
	}
	return images, nil
}

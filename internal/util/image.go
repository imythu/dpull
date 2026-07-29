package util

import "strings"

// TarFilename converts an image reference into a portable cache filename.
func TarFilename(image string) string {
	replacer := strings.NewReplacer("/", "_", ":", "_")
	return replacer.Replace(image) + ".tar"
}

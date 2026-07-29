package compose

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseImagesInServiceOrder(t *testing.T) {
	t.Parallel()
	yamlText := `services:
  app:
    build: .
  redis:
    image: redis:7
  mysql:
    image: mysql:8
  redis-copy:
    image: redis:7
`
	images, err := Parse(strings.NewReader(yamlText))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want := []string{"redis:7", "mysql:8"}
	if !reflect.DeepEqual(images, want) {
		t.Fatalf("Parse() = %v, want %v", images, want)
	}
}

func TestParseRejectsInvalidYAML(t *testing.T) {
	t.Parallel()
	if _, err := Parse(strings.NewReader("services: [")); err == nil {
		t.Fatal("Parse() error = nil, want an error")
	}
}

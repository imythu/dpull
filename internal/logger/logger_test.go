package logger

import (
	"bytes"
	"strings"
	"testing"
)

func TestLoggerOutput(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	log := New(&output)
	log.Banner()
	log.Section("Init")
	log.Warning("cache %s", "failed")
	for _, expected := range []string{"dpull", "[Init]", "Warning: cache failed"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("output does not contain %q: %s", expected, output.String())
		}
	}
}

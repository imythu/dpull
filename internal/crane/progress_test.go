package crane

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestProgressBarPrintsStepsWhenNotTerminal(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	now := time.Unix(0, 0)
	bar := newProgressBar(&output, func() time.Time { return now })
	for complete := int64(0); complete <= 1000; complete += 50 {
		now = now.Add(100 * time.Millisecond)
		bar.update(complete, 1000)
	}
	bar.finish()
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 11 {
		t.Fatalf("lines = %d, want 11 (every 10%%):\n%s", len(lines), output.String())
	}
	last := lines[len(lines)-1]
	for _, want := range []string{"[########################]", "100.0%", "1000 B / 1000 B", "476 B/s"} {
		if !strings.Contains(last, want) {
			t.Fatalf("last line %q does not contain %q", last, want)
		}
	}
}

func TestProgressBarFinishDrawsThrottledState(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	bar := newProgressBar(&output, func() time.Time { return time.Unix(0, 0) })
	bar.update(10, 1000)
	bar.update(55, 1000)
	bar.finish()
	if !strings.Contains(output.String(), "5.5%") {
		t.Fatalf("output = %q, want final throttled state", output.String())
	}
}

func TestProgressBarSpeedExcludesReusedBytes(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	now := time.Unix(0, 0)
	bar := newProgressBar(&output, func() time.Time { return now })
	bar.total = 3000
	bar.reuse(2000)
	now = now.Add(2 * time.Second)
	bar.add(1000)
	bar.finish()
	if !strings.Contains(output.String(), "500 B/s") {
		t.Fatalf("output = %q, want speed of downloaded bytes only", output.String())
	}
}

func TestFormatBytes(t *testing.T) {
	t.Parallel()
	cases := map[int64]string{512: "512 B", 1536: "1.5 KiB", 5 << 20: "5.0 MiB", 3 << 30: "3.0 GiB"}
	for value, want := range cases {
		if got := formatBytes(value); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", value, got, want)
		}
	}
}

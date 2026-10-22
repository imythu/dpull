package crane

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const (
	progressWidth    = 24
	progressInterval = 100 * time.Millisecond
	// progressStep is the percentage between lines when output is not a terminal.
	progressStep = 10
)

// progressBar renders download progress. On a terminal it redraws one line in
// place; otherwise it prints a line every progressStep percent so logs stay
// readable.
type progressBar struct {
	out         io.Writer
	terminal    bool
	now         func() time.Time
	started     time.Time
	lastDraw    time.Time
	lastPercent int
	total       int64
	complete    int64
	// reused counts bytes already on disk, which do not count towards speed.
	reused int64
	drawn  bool
	dirty  bool
}

func newProgressBar(out io.Writer, now func() time.Time) *progressBar {
	return &progressBar{out: out, terminal: isTerminal(out), now: now, started: now(), lastPercent: -1}
}

// reuse records bytes taken from the cache instead of downloaded.
func (p *progressBar) reuse(size int64) {
	p.reused += size
	p.add(size)
}

// add records delta more (or, when negative, discarded) bytes.
func (p *progressBar) add(delta int64) {
	p.update(p.complete+delta, p.total)
}

// notice prints a message on its own line without breaking the bar.
func (p *progressBar) notice(message string) {
	if p.terminal && p.drawn {
		_, _ = fmt.Fprint(p.out, "\r\033[K")
	}
	_, _ = fmt.Fprintf(p.out, "  %-10s %s\n", "Notice:", message)
	p.lastDraw = time.Time{}
	p.dirty = true
}

func (p *progressBar) update(complete, total int64) {
	p.complete, p.total = complete, total
	if total <= 0 {
		return
	}
	percent := int(complete * 100 / total)
	if !p.due(percent) {
		p.dirty = true
		return
	}
	p.lastPercent = percent
	p.draw()
}

func (p *progressBar) due(percent int) bool {
	if percent >= 100 {
		return p.lastPercent < 100
	}
	if !p.terminal {
		return p.lastPercent < 0 || percent/progressStep != p.lastPercent/progressStep
	}
	now := p.now()
	if now.Sub(p.lastDraw) < progressInterval {
		return false
	}
	p.lastDraw = now
	return true
}

// finish draws the final state if it was throttled and ends the progress line.
func (p *progressBar) finish() {
	if p.dirty && p.total > 0 {
		p.draw()
	}
	if p.drawn && p.terminal {
		_, _ = fmt.Fprintln(p.out)
	}
}

func (p *progressBar) draw() {
	ratio := float64(p.complete) / float64(p.total)
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * progressWidth)
	bar := strings.Repeat("#", filled) + strings.Repeat("-", progressWidth-filled)
	line := fmt.Sprintf("  %-10s [%s] %5.1f%%  %s / %s  %s/s",
		"Progress:", bar, ratio*100, formatBytes(p.complete), formatBytes(p.total), formatBytes(p.speed()))
	if p.terminal {
		_, _ = fmt.Fprintf(p.out, "\r%s\033[K", line)
	} else {
		_, _ = fmt.Fprintln(p.out, line)
	}
	p.drawn, p.dirty = true, false
}

func (p *progressBar) speed() int64 {
	elapsed := p.now().Sub(p.started).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return int64(float64(p.complete-p.reused) / elapsed)
}

func formatBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	size, exp := float64(value)/unit, 0
	for size >= unit && exp < 4 {
		size /= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", size, "KMGTP"[exp])
}

func isTerminal(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

package crane

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
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
	drawn       bool
	dirty       bool
}

func newProgressBar(out io.Writer, now func() time.Time) *progressBar {
	return &progressBar{out: out, terminal: isTerminal(out), now: now, started: now(), lastPercent: -1}
}

// consume renders every update until updates is closed.
func (p *progressBar) consume(updates <-chan v1.Update) {
	for update := range updates {
		if update.Error != nil {
			continue
		}
		p.update(update.Complete, update.Total)
	}
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
	return int64(float64(p.complete) / elapsed)
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

package logger

import (
	"fmt"
	"io"
)

type Logger struct {
	output io.Writer
}

func New(output io.Writer) *Logger {
	return &Logger{output: output}
}

func (l *Logger) Banner() {
	l.Line("================================================")
	l.Line(" dpull")
	l.Line("================================================")
}

func (l *Logger) Section(name string) {
	l.Line("")
	l.Line("[" + name + "]")
}

func (l *Logger) Field(name, format string, args ...any) {
	value := fmt.Sprintf(format, args...)
	l.Line("  %-10s %s", name+":", value)
}

func (l *Logger) Step(format string, args ...any) {
	l.Line("  -> "+format, args...)
}

func (l *Logger) Separator() {
	l.Line("")
	l.Line("------------------------------------------------")
}

func (l *Logger) Line(format string, args ...any) {
	_, _ = fmt.Fprintf(l.output, format+"\n", args...)
}

func (l *Logger) Warning(format string, args ...any) {
	l.Line("  Warning: "+format, args...)
}

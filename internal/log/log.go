// Package log provides minimal colored logging (INFO/WARN/ERROR) to stderr.
package log

import (
	"fmt"
	"os"
	"sync/atomic"

	"github.com/fatih/color"
)

var verbose atomic.Bool

var (
	infoColor  = color.New(color.FgGreen)
	warnColor  = color.New(color.FgYellow)
	errorColor = color.New(color.FgRed)
)

// SetVerbose toggles verbose mode (e.g. pass-through of external command output).
func SetVerbose(v bool) { verbose.Store(v) }

// Verbose reports the current verbose flag.
func Verbose() bool { return verbose.Load() }

// Info prints a green [INFO] message to stderr.
func Info(format string, args ...any) {
	logf(infoColor, "INFO", format, args...)
}

// Warn prints a yellow [WARN] message to stderr.
func Warn(format string, args ...any) {
	logf(warnColor, "WARN", format, args...)
}

// Error prints a red [ERROR] message to stderr.
func Error(format string, args ...any) {
	logf(errorColor, "ERROR", format, args...)
}

func logf(c *color.Color, level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	_, _ = c.Fprintf(os.Stderr, "[%s] ", level)
	_, _ = fmt.Fprintln(os.Stderr, msg)
}

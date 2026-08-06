package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
)

// Level is the severity of a log record. Lower values are more verbose.
type Level int

const (
	LevelTrace Level = iota
	LevelDebug
	LevelInfo
	LevelError
	LevelFatal
)

func (l Level) String() string {
	switch l {
	case LevelTrace:
		return "TRACE"
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	default:
		return fmt.Sprintf("LEVEL(%d)", int(l))
	}
}

// ParseLevel accepts: trace, debug, info, error, fatal (case-insensitive).
// Unknown / empty → Info.
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace", "tr":
		return LevelTrace
	case "debug", "dbg":
		return LevelDebug
	case "info", "information":
		return LevelInfo
	case "error", "err":
		return LevelError
	case "fatal", "fatle": // tolerate common typo
		return LevelFatal
	default:
		return LevelInfo
	}
}

// Logger is a simple leveled logger. Safe for concurrent use.
type Logger struct {
	mu    sync.Mutex
	level Level
	out   *log.Logger
}

func New(level Level, w io.Writer) *Logger {
	if w == nil {
		w = os.Stdout
	}
	return &Logger{
		level: level,
		out:   log.New(w, "", log.LstdFlags|log.Lmicroseconds),
	}
}

func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	l.level = level
	l.mu.Unlock()
}

func (l *Logger) Level() Level {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.level
}

func (l *Logger) Enabled(level Level) bool {
	return level >= l.Level()
}

func (l *Logger) logf(level Level, format string, args ...interface{}) {
	if !l.Enabled(level) {
		return
	}
	msg := fmt.Sprintf(format, args...)
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.out.Output(3, fmt.Sprintf("[%s] %s", level.String(), msg))
}

func (l *Logger) Trace(format string, args ...interface{}) { l.logf(LevelTrace, format, args...) }
func (l *Logger) Debug(format string, args ...interface{}) { l.logf(LevelDebug, format, args...) }
func (l *Logger) Info(format string, args ...interface{})  { l.logf(LevelInfo, format, args...) }
func (l *Logger) Error(format string, args ...interface{}) { l.logf(LevelError, format, args...) }

// Fatal logs at FATAL and exits the process with status 1.
func (l *Logger) Fatal(format string, args ...interface{}) {
	l.logf(LevelFatal, format, args...)
	os.Exit(1)
}

var (
	defaultMu sync.RWMutex
	defaultL  = New(LevelInfo, os.Stdout)
)

// SetDefault replaces the package-level logger (used by Trace/Debug/…).
func SetDefault(l *Logger) {
	if l == nil {
		return
	}
	defaultMu.Lock()
	defaultL = l
	defaultMu.Unlock()
}

func Default() *Logger {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultL
}

func Trace(format string, args ...interface{}) { Default().Trace(format, args...) }
func Debug(format string, args ...interface{}) { Default().Debug(format, args...) }
func Info(format string, args ...interface{})  { Default().Info(format, args...) }
func Error(format string, args ...interface{}) { Default().Error(format, args...) }
func Fatal(format string, args ...interface{}) { Default().Fatal(format, args...) }
func Enabled(level Level) bool                 { return Default().Enabled(level) }

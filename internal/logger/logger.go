package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Level is a log severity level.
type Level string

const (
	DEBUG Level = "DEBUG"
	INFO  Level = "INFO"
	WARN  Level = "WARN"
	ERROR Level = "ERROR"
)

// Entry is a single structured log entry.
type Entry struct {
	Level     Level  `json:"level"`
	Component string `json:"component"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// Logger is a thread-safe rotating file logger with an in-memory ring buffer
// and optional Wails event emission for the frontend log console.
type Logger struct {
	mu       sync.Mutex
	ring     []Entry
	ringSize int
	file     *os.File
	filePath string
	maxBytes int64
	maxFiles int
	emit     func(string, interface{})
}

// Prefs is the minimal interface Logger needs from config.Preferences.
type Prefs struct {
	LogMaxSizeMB int
	LogMaxFiles  int
}

// New creates a new Logger. emit can be nil (no frontend events).
func New(prefs Prefs, logDir string, emit func(string, interface{})) *Logger {
	l := &Logger{
		ringSize: 500,
		ring:     make([]Entry, 0, 500),
		maxBytes: int64(prefs.LogMaxSizeMB) * 1024 * 1024,
		maxFiles: prefs.LogMaxFiles,
		emit:     emit,
	}
	if logDir != "" {
		os.MkdirAll(logDir, 0755)
		l.filePath = filepath.Join(logDir, "gitsync.log")
		l.openLogFile()
	}
	return l
}

func (l *Logger) log(level Level, component, msg string) {
	entry := Entry{
		Level:     level,
		Component: component,
		Message:   msg,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	// Ring buffer: drop oldest entry when full
	if len(l.ring) >= l.ringSize {
		l.ring = l.ring[1:]
	}
	l.ring = append(l.ring, entry)

	// Write to log file
	line := fmt.Sprintf("%s [%-5s] [%s] %s\n",
		entry.Timestamp, entry.Level, entry.Component, entry.Message)
	if l.file != nil {
		l.file.WriteString(line)
		l.rotateIfNeeded()
	}

	// Broadcast to frontend via Wails events
	if l.emit != nil {
		l.emit("log:entry", entry)
	}
}

// Info logs an informational message.
func (l *Logger) Info(component, format string, args ...interface{}) {
	l.log(INFO, component, fmt.Sprintf(format, args...))
}

// Warn logs a warning message.
func (l *Logger) Warn(component, format string, args ...interface{}) {
	l.log(WARN, component, fmt.Sprintf(format, args...))
}

// Error logs an error message.
func (l *Logger) Error(component, format string, args ...interface{}) {
	l.log(ERROR, component, fmt.Sprintf(format, args...))
}

// Debug logs a debug message.
func (l *Logger) Debug(component, format string, args ...interface{}) {
	l.log(DEBUG, component, fmt.Sprintf(format, args...))
}

// Recent returns the last n log entries from the ring buffer.
func (l *Logger) Recent(n int) []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n <= 0 || n >= len(l.ring) {
		result := make([]Entry, len(l.ring))
		copy(result, l.ring)
		return result
	}
	result := make([]Entry, n)
	copy(result, l.ring[len(l.ring)-n:])
	return result
}

// Clear empties the in-memory log ring buffer.
func (l *Logger) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ring = l.ring[:0]
}

// Flush flushes any buffered file writes.
func (l *Logger) Flush() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		l.file.Sync()
	}
}

// Close closes the log file.
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
}

func (l *Logger) openLogFile() {
	f, err := os.OpenFile(l.filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	l.file = f
}

func (l *Logger) rotateIfNeeded() {
	if l.file == nil || l.maxBytes == 0 {
		return
	}
	info, err := l.file.Stat()
	if err != nil || info.Size() < l.maxBytes {
		return
	}
	l.file.Close()
	l.file = nil

	// Shift existing rotated files: .log.2 → .log.3, .log.1 → .log.2
	for i := l.maxFiles - 1; i >= 1; i-- {
		oldPath := fmt.Sprintf("%s.%d", l.filePath, i)
		newPath := fmt.Sprintf("%s.%d", l.filePath, i+1)
		os.Rename(oldPath, newPath)
	}
	// Rename current log to .log.1
	os.Rename(l.filePath, l.filePath+".1")
	l.openLogFile()
}

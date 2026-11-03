package logger

import (
	"strings"
	"testing"
	"time"
)

func newTestLogger() *Logger {
	return New(Prefs{LogMaxSizeMB: 5, LogMaxFiles: 3}, "", nil)
}

func TestLogLevels(t *testing.T) {
	l := newTestLogger()
	l.Info("test", "info message %d", 1)
	l.Warn("test", "warn message")
	l.Error("test", "error message")
	l.Debug("test", "debug message")

	entries := l.Recent(100)
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}
	if entries[0].Level != INFO {
		t.Errorf("expected INFO, got %s", entries[0].Level)
	}
	if entries[2].Level != ERROR {
		t.Errorf("expected ERROR, got %s", entries[2].Level)
	}
}

func TestRingBuffer(t *testing.T) {
	l := newTestLogger()
	l.ringSize = 5

	for i := 0; i < 10; i++ {
		l.Info("test", "message %d", i)
	}

	entries := l.Recent(100)
	if len(entries) != 5 {
		t.Fatalf("ring buffer should hold 5 entries, got %d", len(entries))
	}
	// Should contain the last 5 messages (5-9)
	if !strings.Contains(entries[0].Message, "5") {
		t.Errorf("oldest retained entry should be 'message 5', got: %s", entries[0].Message)
	}
}

func TestTimestampFormat(t *testing.T) {
	l := newTestLogger()
	l.Info("test", "ts check")
	entries := l.Recent(1)
	_, err := time.Parse(time.RFC3339, entries[0].Timestamp)
	if err != nil {
		t.Errorf("timestamp not RFC3339: %v", err)
	}
}

func TestEmitCallback(t *testing.T) {
	emitted := 0
	l := New(Prefs{LogMaxSizeMB: 5, LogMaxFiles: 3}, "", func(name string, data interface{}) {
		if name == "log:entry" {
			emitted++
		}
	})
	l.Info("test", "fires event")
	l.Warn("test", "fires another")
	if emitted != 2 {
		t.Errorf("expected 2 emit calls, got %d", emitted)
	}
}

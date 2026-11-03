package watcher

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestDebouncerFires(t *testing.T) {
	var count atomic.Int32
	d := NewDebouncer(50 * time.Millisecond)
	d.Schedule("repo1", func() { count.Add(1) })

	time.Sleep(100 * time.Millisecond)
	if count.Load() != 1 {
		t.Errorf("expected 1 fire, got %d", count.Load())
	}
}

func TestDebouncerReset(t *testing.T) {
	var count atomic.Int32
	d := NewDebouncer(80 * time.Millisecond)

	// Schedule 5 times rapidly — only 1 should fire after settling
	for i := 0; i < 5; i++ {
		d.Schedule("repo1", func() { count.Add(1) })
		time.Sleep(20 * time.Millisecond)
	}

	time.Sleep(200 * time.Millisecond)
	if count.Load() != 1 {
		t.Errorf("expected 1 fire after debounce, got %d", count.Load())
	}
}

func TestDebouncerTrailingMaxWait(t *testing.T) {
	var count atomic.Int32
	// Idle 60ms, hard cap 150ms
	d := NewDebouncer(60*time.Millisecond, 150*time.Millisecond)

	start := time.Now()
	// Continuously poke schedule every 30ms for 250ms (would starve an un-capped debouncer)
	for time.Since(start) < 250*time.Millisecond {
		d.Schedule("repo-busy", func() { count.Add(1) })
		time.Sleep(30 * time.Millisecond)
	}

	// Because of the 150ms hard cap, it MUST have fired despite continuous pokes
	if count.Load() < 1 {
		t.Errorf("expected at least 1 fire due to trailing cap, got %d", count.Load())
	}
}

func TestDebouncerIsolated(t *testing.T) {
	var count atomic.Int32
	d := NewDebouncer(50 * time.Millisecond)

	// Two different repos should each fire independently
	d.Schedule("repo1", func() { count.Add(1) })
	d.Schedule("repo2", func() { count.Add(1) })

	time.Sleep(150 * time.Millisecond)
	if count.Load() != 2 {
		t.Errorf("expected 2 fires (one per repo), got %d", count.Load())
	}
}

func TestDebouncerCancelAll(t *testing.T) {
	var count atomic.Int32
	d := NewDebouncer(50 * time.Millisecond)
	d.Schedule("repo1", func() { count.Add(1) })
	d.CancelAll()

	time.Sleep(100 * time.Millisecond)
	if count.Load() != 0 {
		t.Errorf("expected 0 fires after CancelAll, got %d", count.Load())
	}
}

func TestDebouncerSetIntervals(t *testing.T) {
	d := NewDebouncer(30*time.Second, 120*time.Second)
	d.SetIntervals(45*time.Millisecond, 200*time.Millisecond)

	idle, maxWait := d.Intervals()
	if idle != 45*time.Millisecond || maxWait != 200*time.Millisecond {
		t.Fatalf("unexpected intervals after SetIntervals: idle=%v, maxWait=%v", idle, maxWait)
	}

	var count atomic.Int32
	d.Schedule("repo-dynamic", func() { count.Add(1) })
	time.Sleep(90 * time.Millisecond)
	if count.Load() != 1 {
		t.Errorf("expected 1 fire with updated 45ms interval, got %d", count.Load())
	}
}

func TestIsNoisePath(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"file.tmp", true},
		{"file.swp", true},
		{".hidden", true},
		{"#temp", true},
		{"Thumbs.db", true},
		{"desktop.ini", true},
		{"main.go", false},
		{"README.md", false},
		{"data.json", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNoisePath(tt.name); got != tt.expected {
				t.Errorf("isNoisePath(%q) = %v, want %v", tt.name, got, tt.expected)
			}
		})
	}
}

func TestIsGitPath(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{".git", true},
		{"C:/project/.git", true},
		{"C:/project/.git/HEAD", true},
		{"C:/project/.git/objects/4b", true},
		{`C:\project\.git\index`, true},
		{"C:/project/src/main.go", false},
		{"C:/project/git-helper.go", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := isGitPath(tt.path); got != tt.expected {
				t.Errorf("isGitPath(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestEnsureDefaultGitignore(t *testing.T) {
	dir := t.TempDir()
	ensureDefaultGitignore(dir)

	content, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("failed to read created .gitignore: %v", err)
	}
	if len(content) == 0 {
		t.Fatal(".gitignore is empty")
	}

	// Running a second time should not overwrite custom user edits
	customContent := "# Custom rule\nsecret.txt\n"
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(customContent), 0644)
	ensureDefaultGitignore(dir)

	afterContent, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if string(afterContent) != customContent {
		t.Errorf("ensureDefaultGitignore overwrote existing file")
	}
}

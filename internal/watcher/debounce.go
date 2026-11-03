package watcher

import (
	"sync"
	"time"
)

type debounceEntry struct {
	timer      *time.Timer
	firstEvent time.Time
	callback   func()
}

// Debouncer manages per-key timers that fire after idleDuration of inactivity,
// but are bounded by maxWaitDuration to prevent infinite starvation during continuous writes.
type Debouncer struct {
	mu              sync.Mutex
	entries         map[string]*debounceEntry
	idleDuration    time.Duration
	maxWaitDuration time.Duration
}

// NewDebouncer creates a Debouncer with an idle delay and an optional hard trailing cap.
// If maxWait is not provided, it defaults to max(idleDuration*4, 120s).
func NewDebouncer(idleDuration time.Duration, maxWait ...time.Duration) *Debouncer {
	if idleDuration <= 0 {
		idleDuration = 30 * time.Second
	}

	maxWaitDuration := idleDuration * 4
	if len(maxWait) > 0 && maxWait[0] > 0 {
		maxWaitDuration = maxWait[0]
	}
	if maxWaitDuration < idleDuration {
		maxWaitDuration = idleDuration * 4
	}

	return &Debouncer{
		entries:         make(map[string]*debounceEntry),
		idleDuration:    idleDuration,
		maxWaitDuration: maxWaitDuration,
	}
}

// SetIntervals dynamically updates the idle debounce delay and hard trailing cap at runtime.
func (d *Debouncer) SetIntervals(idleDuration time.Duration, maxWait ...time.Duration) {
	if idleDuration <= 0 {
		idleDuration = 30 * time.Second
	}

	maxWaitDuration := idleDuration * 4
	if len(maxWait) > 0 && maxWait[0] > 0 {
		maxWaitDuration = maxWait[0]
	}
	if maxWaitDuration < idleDuration {
		maxWaitDuration = idleDuration * 4
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.idleDuration = idleDuration
	d.maxWaitDuration = maxWaitDuration
}

// Intervals returns the current (idleDuration, maxWaitDuration) configuration.
func (d *Debouncer) Intervals() (time.Duration, time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.idleDuration, d.maxWaitDuration
}

// Schedule queues or resets a debounced execution for a key.
// If the key has experienced continuous events exceeding maxWaitDuration since the first event,
// the callback is executed immediately to prevent debounce starvation.
func (d *Debouncer) Schedule(key string, fn func()) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	entry, exists := d.entries[key]

	if exists {
		if entry.timer != nil {
			entry.timer.Stop()
		}
		entry.callback = fn

		// Check if we have hit or passed the trailing max-wait cap
		elapsed := now.Sub(entry.firstEvent)
		if elapsed >= d.maxWaitDuration {
			// Starvation threshold reached: fire immediately
			delete(d.entries, key)
			go fn()
			return
		}

		// Calculate remaining time before hard cap
		remaining := d.maxWaitDuration - elapsed
		delay := d.idleDuration
		if remaining < delay {
			delay = remaining
		}

		entry.timer = time.AfterFunc(delay, func() {
			d.trigger(key)
		})
		return
	}

	// First event in burst
	newEntry := &debounceEntry{
		firstEvent: now,
		callback:   fn,
	}
	newEntry.timer = time.AfterFunc(d.idleDuration, func() {
		d.trigger(key)
	})
	d.entries[key] = newEntry
}

func (d *Debouncer) trigger(key string) {
	d.mu.Lock()
	entry, exists := d.entries[key]
	if !exists {
		d.mu.Unlock()
		return
	}
	fn := entry.callback
	delete(d.entries, key)
	d.mu.Unlock()

	if fn != nil {
		fn()
	}
}

// Pending reports whether a key currently has a pending debounced timer.
func (d *Debouncer) Pending(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, exists := d.entries[key]
	return exists
}

// Cancel stops and removes a pending timer for a specific key.
func (d *Debouncer) Cancel(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if entry, exists := d.entries[key]; exists {
		if entry.timer != nil {
			entry.timer.Stop()
		}
		delete(d.entries, key)
	}
}

// CancelAll cancels all pending timers across all keys.
func (d *Debouncer) CancelAll() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, entry := range d.entries {
		if entry.timer != nil {
			entry.timer.Stop()
		}
	}
	d.entries = make(map[string]*debounceEntry)
}

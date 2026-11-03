package githubapi

import (
	"context"
	"sync"
	"time"

	"github.com/user/gitsync/internal/logger"
)

// RateLimitTracker monitors GitHub API rate limits and blocks callers when exhausted.
type RateLimitTracker struct {
	mu        sync.Mutex
	resetAt   time.Time
	remaining int
	log       *logger.Logger
}

// CheckAndWait blocks if the rate limit is exhausted, waiting until it resets.
// Returns early if the context is cancelled.
func (r *RateLimitTracker) CheckAndWait(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.remaining == 0 && !r.resetAt.IsZero() && time.Now().Before(r.resetAt) {
		waitDur := time.Until(r.resetAt) + 5*time.Second
		r.log.Warn("githubapi", "rate limit exhausted, sleeping %s until reset at %s",
			waitDur.Round(time.Second), r.resetAt.Format(time.Kitchen))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitDur):
		}
	}
	return nil
}

// UpdateFromResponse updates the rate limit state from a GitHub API response.
func (r *RateLimitTracker) UpdateFromResponse(remaining int, resetAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.remaining = remaining
	r.resetAt = resetAt

	if remaining < 100 && r.log != nil {
		r.log.Warn("githubapi", "rate limit low: %d requests remaining (resets %s)",
			remaining, resetAt.Format(time.Kitchen))
	}
}

// Remaining returns the current known remaining request count.
func (r *RateLimitTracker) Remaining() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.remaining
}

package githubapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimitTrackerNoBlock(t *testing.T) {
	tracker := &RateLimitTracker{
		remaining: 1000,
		log:       newNilLogger(),
	}
	ctx := context.Background()
	if err := tracker.CheckAndWait(ctx); err != nil {
		t.Errorf("should not block with remaining > 0: %v", err)
	}
}

func TestRateLimitTrackerUpdate(t *testing.T) {
	tracker := &RateLimitTracker{log: newNilLogger()}
	reset := time.Now().Add(1 * time.Hour)
	tracker.UpdateFromResponse(500, reset)
	if tracker.Remaining() != 500 {
		t.Errorf("expected 500 remaining, got %d", tracker.Remaining())
	}
}

func TestRateLimitTrackerContextCancel(t *testing.T) {
	tracker := &RateLimitTracker{
		remaining: 0,
		resetAt:   time.Now().Add(10 * time.Second),
		log:       newNilLogger(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	start := time.Now()
	err := tracker.CheckAndWait(ctx)
	if err == nil {
		t.Error("expected context cancellation error")
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Error("should have returned immediately on cancelled context")
	}
}

func TestVerifyToken_Mock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-RateLimit-Limit", "5000")
			w.Header().Set("X-RateLimit-Remaining", "4999")
			w.Header().Set("X-RateLimit-Reset", "1700000000")
			w.Header().Set("X-RateLimit-Used", "1")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"login": "octocat",
				"name": "The Octocat",
				"email": "octocat@github.com",
				"avatar_url": "https://avatars.githubusercontent.com/u/583231"
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	SetTestBaseURL(server.URL)
	defer SetTestBaseURL("")

	pool := NewPool(nil, newNilLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	user, rate, err := pool.VerifyToken(ctx, "dummy-pat")
	if err != nil {
		t.Fatalf("VerifyToken failed: %v", err)
	}

	if user.Username != "octocat" {
		t.Errorf("expected username octocat, got %s", user.Username)
	}
	if user.AvatarURL != "https://avatars.githubusercontent.com/u/583231" {
		t.Errorf("unexpected avatar url: %s", user.AvatarURL)
	}
	if rate == nil || rate.Remaining != 4999 {
		t.Errorf("expected remaining rate limit 4999, got %v", rate)
	}
}


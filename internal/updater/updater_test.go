package updater

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		v1       string
		v2       string
		expected int
	}{
		{"1.0.0", "1.0.0", 0},
		{"v1.0.0", "1.0.0", 0},
		{"1.1.0", "1.0.0", 1},
		{"v1.2.3", "v1.2.2", 1},
		{"2.0.0", "1.9.9", 1},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"0.9.9", "1.0.0", -1},
		{"1.0.0", "1.0.0-rc1", 1},
		{"1.0.0-rc1", "1.0.0", -1},
	}

	for _, tc := range tests {
		res := CompareSemver(tc.v1, tc.v2)
		if res != tc.expected {
			t.Errorf("CompareSemver(%q, %q) = %d; want %d", tc.v1, tc.v2, res, tc.expected)
		}
	}
}

func TestUpdaterCheck_WithNewerRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := GitHubRelease{
			TagName:     "v1.2.0",
			Name:        "GitSync v1.2.0",
			HTMLURL:     "https://github.com/MrShadowRIFAT/GitSync/releases/tag/v1.2.0",
			PublishedAt: "2026-10-31T12:00:00Z",
			Body:        "Performance improvements and dark mode updates",
			Assets: []GitHubAsset{
				{
					Name:               "gitsync.exe",
					BrowserDownloadURL: "https://github.com/MrShadowRIFAT/GitSync/releases/download/v1.2.0/gitsync.exe",
					Size:               15000000,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rel)
	}))
	defer server.Close()

	var emittedEvents []string
	svc := New(nil, func(event string, data interface{}) {
		emittedEvents = append(emittedEvents, event)
	})
	svc.SetAPIBaseURL(server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	info, err := svc.Check(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !info.HasUpdate {
		t.Errorf("expected HasUpdate to be true for v1.2.0 vs %s", info.CurrentVersion)
	}
	if info.LatestVersion != "1.2.0" {
		t.Errorf("expected LatestVersion to be '1.2.0', got %q", info.LatestVersion)
	}
	if info.DownloadURL != "https://github.com/MrShadowRIFAT/GitSync/releases/download/v1.2.0/gitsync.exe" {
		t.Errorf("expected download URL for exe, got %q", info.DownloadURL)
	}
}

func TestUpdaterCheck_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	svc := New(nil, nil)
	svc.SetAPIBaseURL(server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	info, err := svc.Check(ctx)
	if err != nil {
		t.Fatalf("expected nil error on 404, got %v", err)
	}
	if info.HasUpdate {
		t.Errorf("expected HasUpdate to be false on 404")
	}
}

func TestUpdaterInstallUpdate(t *testing.T) {
	mockPayload := []byte("MZ-mock-windows-executable-binary-payload-content")

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/download/gitsync.exe" {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(mockPayload)
			return
		}
		rel := GitHubRelease{
			TagName:     "v1.3.0",
			Name:        "GitSync v1.3.0",
			HTMLURL:     serverURL + "/releases/v1.3.0",
			PublishedAt: "2026-10-31T12:00:00Z",
			Body:        "**Full Changelog**: https://github.com/MrShadowRIFAT/GitSync/compare/v1.2.0...v1.3.0",
			Assets: []GitHubAsset{
				{
					Name:               "gitsync.exe",
					BrowserDownloadURL: serverURL + "/download/gitsync.exe",
					Size:               int64(len(mockPayload)),
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rel)
	}))
	defer server.Close()
	serverURL = server.URL

	svc := New(nil, nil)
	svc.SetAPIBaseURL(server.URL)

	var replacedStaged string
	svc.replaceHook = func(stagedPath, targetExePath string) error {
		replacedStaged = stagedPath
		data, err := os.ReadFile(stagedPath)
		if err != nil {
			return err
		}
		if string(data) != string(mockPayload) {
			t.Errorf("staged binary mismatch: got %q, want %q", string(data), string(mockPayload))
		}
		_ = os.Remove(stagedPath)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var lastPct int
	err := svc.InstallUpdate(ctx, func(pct int, status string) {
		lastPct = pct
	})
	if err != nil {
		t.Fatalf("InstallUpdate failed: %v", err)
	}
	if replacedStaged == "" {
		t.Errorf("expected replaceHook to be invoked with staged binary path")
	}
	if lastPct != 100 {
		t.Errorf("expected final progress to be 100, got %d", lastPct)
	}
}



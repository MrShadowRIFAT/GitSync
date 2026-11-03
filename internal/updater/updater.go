package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/user/gitsync/internal/logger"
)

const (
	// CurrentVersion is the active binary version of GitSync.
	CurrentVersion = "1.0.0"

	// DefaultOwner and DefaultRepo point to the official GitHub repository.
	DefaultOwner = "MrShadowRIFAT"
	DefaultRepo  = "GitSync"

	// DefaultCheckInterval is the 24-hour background auto-update polling cadence.
	DefaultCheckInterval = 24 * time.Hour
)

// GitHubRelease represents the GitHub REST API /releases/latest response.
type GitHubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	HTMLURL     string        `json:"html_url"`
	PublishedAt string        `json:"published_at"`
	Body        string        `json:"body"`
	Prerelease  bool          `json:"prerelease"`
	Draft       bool          `json:"draft"`
	Assets      []GitHubAsset `json:"assets"`
}

// GitHubAsset represents a downloadable release artifact.
type GitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// UpdateInfo holds the live status of the auto-updater check.
type UpdateInfo struct {
	CurrentVersion string    `json:"current_version"`
	LatestVersion  string    `json:"latest_version"`
	HasUpdate      bool      `json:"has_update"`
	ReleaseName    string    `json:"release_name"`
	ReleaseURL     string    `json:"release_url"`
	DownloadURL    string    `json:"download_url"`
	PublishedAt    string    `json:"published_at"`
	ReleaseNotes   string    `json:"release_notes"`
	LastChecked    time.Time `json:"last_checked"`
	Error          string    `json:"error"`
}

// Service manages background scheduled checks and manual queries for GitHub releases.
type Service struct {
	log            *logger.Logger
	emit           func(string, interface{})
	httpClient     *http.Client
	owner          string
	repo           string
	currentVersion string
	checkInterval  time.Duration
	apiBaseURL     string // for test mocking
	replaceHook    func(stagedPath, targetExePath string) error

	mu     sync.RWMutex
	cached *UpdateInfo
}

// New creates an instance of the auto-updater service.
func New(log *logger.Logger, emit func(string, interface{})) *Service {
	return &Service{
		log:            log,
		emit:           emit,
		httpClient:     &http.Client{Timeout: 12 * time.Second},
		owner:          DefaultOwner,
		repo:           DefaultRepo,
		currentVersion: CurrentVersion,
		checkInterval:  DefaultCheckInterval,
		cached: &UpdateInfo{
			CurrentVersion: CurrentVersion,
			LatestVersion:  CurrentVersion,
			HasUpdate:      false,
		},
	}
}

// SetAPIBaseURL overrides the GitHub API URL (useful in tests with httptest.Server).
func (s *Service) SetAPIBaseURL(baseURL string) {
	s.mu.Lock()
	s.apiBaseURL = baseURL
	s.mu.Unlock()
}

// Start launches the 24-hour background checking routine.
func (s *Service) Start(ctx context.Context) {
	go func() {
		if s.log != nil {
			s.log.Info("updater", "background auto-updater started (check interval: %v)", s.checkInterval)
		}

		// Initial check scheduled 5 seconds after startup to not slow down window display
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
			_, _ = s.Check(ctx)
		}

		ticker := time.NewTicker(s.checkInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = s.Check(ctx)
			}
		}
	}()
}

// Check queries GitHub for the latest release, evaluates semantic versions, and notifies via events.
func (s *Service) Check(ctx context.Context) (*UpdateInfo, error) {
	s.mu.RLock()
	baseURL := s.apiBaseURL
	owner := s.owner
	repo := s.repo
	currVer := s.currentVersion
	s.mu.RUnlock()

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
	if baseURL != "" {
		url = strings.TrimRight(baseURL, "/") + fmt.Sprintf("/repos/%s/%s/releases/latest", owner, repo)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "GitSync-Desktop/"+currVer)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := s.httpClient.Do(req)
	now := time.Now().UTC()
	if err != nil {
		info := &UpdateInfo{
			CurrentVersion: currVer,
			LatestVersion:  currVer,
			HasUpdate:      false,
			LastChecked:    now,
			Error:          fmt.Sprintf("Network error checking for updates: %v", err),
		}
		s.saveAndEmit(info)
		if s.log != nil {
			s.log.Warn("updater", "failed to check GitHub releases: %v", err)
		}
		return info, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// No release has been published yet on GitHub repo
		info := &UpdateInfo{
			CurrentVersion: currVer,
			LatestVersion:  currVer,
			HasUpdate:      false,
			LastChecked:    now,
			Error:          "",
		}
		s.saveAndEmit(info)
		if s.log != nil {
			s.log.Info("updater", "no releases found for %s/%s; current version is up to date", owner, repo)
		}
		return info, nil
	}

	if resp.StatusCode != http.StatusOK {
		errStr := fmt.Sprintf("GitHub API returned HTTP %d", resp.StatusCode)
		info := &UpdateInfo{
			CurrentVersion: currVer,
			LatestVersion:  currVer,
			HasUpdate:      false,
			LastChecked:    now,
			Error:          errStr,
		}
		s.saveAndEmit(info)
		return info, fmt.Errorf("%s", errStr)
	}

	var rel GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		info := &UpdateInfo{
			CurrentVersion: currVer,
			LatestVersion:  currVer,
			HasUpdate:      false,
			LastChecked:    now,
			Error:          fmt.Sprintf("Failed to decode release response: %v", err),
		}
		s.saveAndEmit(info)
		return info, err
	}

	cleanLatest := cleanVersion(rel.TagName)
	hasUpdate := CompareSemver(cleanLatest, currVer) > 0

	// Select best download URL (prefer gitsync.exe or windows asset, fallback to release HTML URL)
	downloadURL := rel.HTMLURL
	for _, asset := range rel.Assets {
		lower := strings.ToLower(asset.Name)
		if strings.HasSuffix(lower, ".exe") || strings.Contains(lower, "windows") {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	info := &UpdateInfo{
		CurrentVersion: currVer,
		LatestVersion:  cleanLatest,
		HasUpdate:      hasUpdate,
		ReleaseName:    rel.Name,
		ReleaseURL:     rel.HTMLURL,
		DownloadURL:    downloadURL,
		PublishedAt:    rel.PublishedAt,
		ReleaseNotes:   rel.Body,
		LastChecked:    now,
		Error:          "",
	}

	s.saveAndEmit(info)

	if hasUpdate {
		if s.log != nil {
			s.log.Info("updater", "new version available: %s (current: %s)", cleanLatest, currVer)
		}
		if s.emit != nil {
			s.emit("update:available", info)
			s.emit("toast:notify", map[string]interface{}{
				"id":        "update-" + cleanLatest,
				"type":      "info",
				"title":     "Update Available",
				"message":   fmt.Sprintf("GitSync v%s is ready to download", cleanLatest),
				"timestamp": now.Format(time.RFC3339),
			})
		}
	} else if s.log != nil {
		s.log.Info("updater", "GitSync is up to date (version %s)", currVer)
	}

	return info, nil
}

// GetInfo returns the latest cached update information.
func (s *Service) GetInfo() *UpdateInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cached == nil {
		return &UpdateInfo{
			CurrentVersion: s.currentVersion,
			LatestVersion:  s.currentVersion,
			HasUpdate:      false,
		}
	}
	cp := *s.cached
	return &cp
}

func (s *Service) saveAndEmit(info *UpdateInfo) {
	s.mu.Lock()
	s.cached = info
	s.mu.Unlock()

	if s.emit != nil {
		s.emit("update:status", info)
	}
}

// InstallUpdate downloads the latest release binary, stages it to a temporary executable,
// and triggers ReplaceAndRestart to swap the binary and relaunch GitSync.
func (s *Service) InstallUpdate(ctx context.Context, progressCb func(pct int, status string)) error {
	if progressCb == nil {
		progressCb = func(int, string) {}
	}

	report := func(pct int, status string) {
		progressCb(pct, status)
		if s.emit != nil {
			s.emit("update:progress", map[string]interface{}{
				"percent": pct,
				"status":  status,
			})
		}
	}

	info := s.GetInfo()
	if info == nil || !info.HasUpdate || info.DownloadURL == "" {
		checked, err := s.Check(ctx)
		if err != nil {
			return fmt.Errorf("failed to verify update metadata: %w", err)
		}
		info = checked
	}

	if info == nil || info.DownloadURL == "" {
		return fmt.Errorf("no downloadable update artifact found")
	}

	report(5, "Connecting to GitHub Release server...")

	dlClient := &http.Client{Timeout: 10 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, info.DownloadURL, nil)
	if err != nil {
		return fmt.Errorf("create update download request: %w", err)
	}
	req.Header.Set("User-Agent", "GitSync-Updater/"+s.currentVersion)

	resp, err := dlClient.Do(req)
	if err != nil {
		return fmt.Errorf("download update binary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download server returned HTTP %s", resp.Status)
	}

	currentExe, err := os.Executable()
	if err != nil {
		currentExe = filepath.Join(".", "GitSync.exe")
	}
	if abs, err := filepath.Abs(currentExe); err == nil {
		currentExe = abs
	}

	stagedPath := filepath.Join(os.TempDir(), fmt.Sprintf("GitSync-update-%d.exe", time.Now().UnixNano()))
	out, err := os.OpenFile(stagedPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("create staged update binary: %w", err)
	}

	totalSize := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 64*1024)
	lastEmit := time.Now()

	for {
		nr, rerr := resp.Body.Read(buf)
		if nr > 0 {
			nw, werr := out.Write(buf[:nr])
			if werr != nil {
				out.Close()
				_ = os.Remove(stagedPath)
				return fmt.Errorf("write update chunk: %w", werr)
			}
			if nr != nw {
				out.Close()
				_ = os.Remove(stagedPath)
				return io.ErrShortWrite
			}
			downloaded += int64(nw)

			if time.Since(lastEmit) > 150*time.Millisecond || (rerr == io.EOF && totalSize > 0) {
				lastEmit = time.Now()
				pct := 50
				if totalSize > 0 {
					pct = 10 + int((downloaded*80)/totalSize)
					if pct > 90 {
						pct = 90
					}
				}
				mbDown := float64(downloaded) / (1024 * 1024)
				if totalSize > 0 {
					mbTotal := float64(totalSize) / (1024 * 1024)
					report(pct, fmt.Sprintf("Downloading update (%.1f / %.1f MB)...", mbDown, mbTotal))
				} else {
					report(pct, fmt.Sprintf("Downloading update (%.1f MB)...", mbDown))
				}
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			out.Close()
			_ = os.Remove(stagedPath)
			return fmt.Errorf("read update stream: %w", rerr)
		}
	}

	if err := out.Close(); err != nil {
		_ = os.Remove(stagedPath)
		return fmt.Errorf("finalize staged binary: %w", err)
	}

	report(95, "Staging update and preparing restart...")

	s.mu.RLock()
	hook := s.replaceHook
	s.mu.RUnlock()

	if hook != nil {
		if err := hook(stagedPath, currentExe); err != nil {
			return err
		}
	} else {
		if err := ReplaceAndRestart(stagedPath, currentExe); err != nil {
			return err
		}
	}

	report(100, "Restarting GitSync...")
	if s.log != nil {
		s.log.Info("updater", "staged update binary at %s for replacement of %s", stagedPath, currentExe)
	}
	return nil
}

// ReplaceAndRestart spawns a background replacement script that waits for the current
// process to exit, overwrites targetExePath with stagedPath, and relaunches GitSync.
func ReplaceAndRestart(stagedPath, targetExePath string) error {
	if runtime.GOOS == "windows" {
		scriptPath := filepath.Join(os.TempDir(), fmt.Sprintf("gitsync-updater-%d.bat", time.Now().UnixNano()))
		scriptContent := fmt.Sprintf(`@echo off
ping 127.0.0.1 -n 3 > nul
move /Y "%s" "%s" > nul
start "" "%s"
del "%%~f0"
`, stagedPath, targetExePath, targetExePath)

		if err := os.WriteFile(scriptPath, []byte(scriptContent), 0700); err != nil {
			return fmt.Errorf("write updater batch script: %w", err)
		}

		cmd := exec.Command("cmd.exe", "/C", scriptPath)
		configureBackgroundCmd(cmd)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("launch updater script: %w", err)
		}
		return nil
	}

	// Non-Windows fallback: atomic rename and relaunch
	if err := os.Chmod(stagedPath, 0755); err != nil {
		return err
	}
	if err := os.Rename(stagedPath, targetExePath); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}
	cmd := exec.Command(targetExePath)
	_ = cmd.Start()
	return nil
}

// ─── Semantic Version Helpers ──────────────────────────────────────────────────

// CompareSemver compares two version strings (e.g., "1.1.0" vs "1.0.0").
// Returns 1 if v1 > v2, -1 if v1 < v2, and 0 if v1 == v2.
func CompareSemver(v1, v2 string) int {
	clean1 := cleanVersion(v1)
	clean2 := cleanVersion(v2)

	nums1, pre1 := splitVersionAndPrerelease(clean1)
	nums2, pre2 := splitVersionAndPrerelease(clean2)

	maxLen := len(nums1)
	if len(nums2) > maxLen {
		maxLen = len(nums2)
	}

	for i := 0; i < maxLen; i++ {
		var n1, n2 int
		if i < len(nums1) {
			n1 = nums1[i]
		}
		if i < len(nums2) {
			n2 = nums2[i]
		}

		if n1 > n2 {
			return 1
		}
		if n1 < n2 {
			return -1
		}
	}

	// If numeric portions are equal, standard release is higher than prerelease
	// e.g. "1.0.0" > "1.0.0-rc1"
	if pre1 == "" && pre2 != "" {
		return 1
	}
	if pre1 != "" && pre2 == "" {
		return -1
	}
	if pre1 > pre2 {
		return 1
	}
	if pre1 < pre2 {
		return -1
	}

	return 0
}

func cleanVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	return v
}

func splitVersionAndPrerelease(v string) ([]int, string) {
	parts := strings.SplitN(v, "-", 2)
	numParts := strings.Split(parts[0], ".")
	var nums []int
	for _, np := range numParts {
		val, err := strconv.Atoi(strings.TrimSpace(np))
		if err == nil {
			nums = append(nums, val)
		} else {
			nums = append(nums, 0)
		}
	}
	prerelease := ""
	if len(parts) > 1 {
		prerelease = parts[1]
	}
	return nums, prerelease
}


package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/user/gitsync/internal/config"
	"github.com/user/gitsync/internal/gitengine"
	"github.com/user/gitsync/internal/githubapi"
	"github.com/user/gitsync/internal/logger"
)

func TestIsSafePath(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"C:\\", false},
		{"/", false},
		{"C:\\Users", false}, // root users folder is shallow
		{"C:\\Users\\dev", true},
		{"C:\\Users\\dev\\projects\\repo", true},
	}

	for _, tt := range tests {
		got := isSafePath(tt.path)
		if got != tt.expected {
			t.Errorf("isSafePath(%q) = %v, want %v", tt.path, got, tt.expected)
		}
	}
}

func TestCleanupStaleIndexLocks(t *testing.T) {
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	os.MkdirAll(gitDir, 0755)

	lockPath := filepath.Join(gitDir, "index.lock")
	os.WriteFile(lockPath, []byte("lock content"), 0644)

	// Set mod time to 10 seconds ago (simulating stale lock)
	past := time.Now().Add(-10 * time.Second)
	os.Chtimes(lockPath, past, past)

	app := &App{
		log: nil,
	}

	// Mock config store with this repo
	cfgStore, err := config.NewStore()
	if err != nil {
		t.Fatalf("failed to create config store: %v", err)
	}
	cfgStore.Config().Repos = []config.RepoConfig{
		{
			ID:         "test-repo",
			LocalPath:  tmpDir,
			RemoteName: "test-repo",
		},
	}
	app.cfg = cfgStore

	app.cleanupStaleIndexLocks()

	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("expected stale index.lock to be removed, but it still exists")
	}
}

func TestApp_CheckDependencies(t *testing.T) {
	cfgStore, err := config.NewStore()
	if err != nil {
		t.Fatalf("failed to create config store: %v", err)
	}

	app := &App{
		cfg: cfgStore,
	}

	missing, details := app.CheckDependencies()
	t.Logf("App.CheckDependencies() => missing=%v, details=%s", missing, details)
}

func TestApp_OpenFolderDialog_NilContext(t *testing.T) {
	app := &App{
		ctx: nil,
	}

	path, err := app.OpenFolderDialog()
	if err == nil {
		t.Fatalf("expected error when app.ctx is nil, got path: %q", path)
	}
	if path != "" {
		t.Errorf("expected empty path on error, got %q", path)
	}

	// Also test SelectDirectory
	path2, err2 := app.SelectDirectory()
	if err2 == nil {
		t.Fatalf("expected error from SelectDirectory when app.ctx is nil, got path: %q", path2)
	}
	if path2 != "" {
		t.Errorf("expected empty path on error, got %q", path2)
	}
}

func TestSafeDirectoryDialog(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only test")
	}
	t.Log("openSafeDirectoryDialog helper compiled and ready")
}

func TestApp_InstallDependencies(t *testing.T) {
	testGitDir := filepath.Join(t.TempDir(), "portable-git")
	gitengine.SetOverridePortableGitDir(testGitDir)
	defer gitengine.SetOverridePortableGitDir("")

	// Create a dummy zip archive
	exeName := "git.exe"
	if runtime.GOOS != "windows" {
		exeName = "git"
	}

	dummyGitContent := []byte("#!/bin/sh\necho git version 2.47.1.test\n")

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create("cmd/" + exeName)
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := f.Write(dummyGitContent); err != nil {
		t.Fatalf("write zip content: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", buf.Len()))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
	}))
	defer server.Close()

	gitengine.SetOverrideDownloadURL(server.URL)
	defer gitengine.SetOverrideDownloadURL("")

	cfgStore, err := config.NewStore()
	if err != nil {
		t.Fatalf("failed to create config store: %v", err)
	}

	log := logger.New(logger.Prefs{LogMaxSizeMB: 5, LogMaxFiles: 2}, t.TempDir(), nil)
	defer log.Close()

	app := &App{
		cfg: cfgStore,
		log: log,
	}

	err = app.InstallDependencies()
	if err != nil {
		t.Fatalf("InstallDependencies failed: %v", err)
	}

	if app.git == nil || !app.git.Available() {
		t.Errorf("expected app.git to be initialized and available")
	}

	missing, gitPath := app.CheckDependencies()
	if missing {
		t.Errorf("expected CheckDependencies() to report missing=false after install, got details: %s", gitPath)
	}
}

func TestApp_ValidateAndSaveGitHubAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-RateLimit-Limit", "5000")
			w.Header().Set("X-RateLimit-Remaining", "4995")
			w.Header().Set("X-RateLimit-Reset", "1700000000")
			w.Header().Set("X-RateLimit-Used", "5")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"login": "octocat",
				"name": "Mona Lisa Octocat",
				"email": "octocat@github.com",
				"avatar_url": "https://avatars.githubusercontent.com/u/583231"
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	githubapi.SetTestBaseURL(server.URL)
	defer githubapi.SetTestBaseURL("")

	cfgStore, err := config.NewStore()
	if err != nil {
		t.Fatalf("failed to create config store: %v", err)
	}

	log := logger.New(logger.Prefs{LogMaxSizeMB: 5, LogMaxFiles: 2}, t.TempDir(), nil)
	defer log.Close()

	app := &App{
		cfg:    cfgStore,
		log:    log,
		github: githubapi.NewPool(cfgStore, log),
	}

	acc, err := app.ValidateAndSaveGitHubAccount("Personal Token", "ghp_mocktoken123456")
	if err != nil {
		t.Fatalf("ValidateAndSaveGitHubAccount failed: %v", err)
	}

	if acc.Username != "octocat" {
		t.Errorf("expected username octocat, got %s", acc.Username)
	}
	if acc.AvatarURL != "https://avatars.githubusercontent.com/u/583231" {
		t.Errorf("unexpected avatar url: %s", acc.AvatarURL)
	}
	if acc.Email != "octocat@github.com" {
		t.Errorf("unexpected email: %s", acc.Email)
	}

	// Verify account was encrypted and saved in config
	saved := cfgStore.FindAccount(acc.ID)
	if saved == nil {
		t.Fatalf("account %s not found in config", acc.ID)
	}
	decrypted, err := cfgStore.DecryptPAT(saved.EncryptedPAT)
	if err != nil || decrypted != "ghp_mocktoken123456" {
		t.Errorf("decrypted PAT mismatch: %s, err: %v", decrypted, err)
	}

	// Test updating the same account
	updated, err := app.ValidateAndSaveGitHubAccount("Work Token", "ghp_newtoken789012")
	if err != nil {
		t.Fatalf("updating account failed: %v", err)
	}
	if updated.ID != acc.ID {
		t.Errorf("expected account ID to stay the same on update, got %s vs %s", updated.ID, acc.ID)
	}
	if updated.Label != "Work Token" {
		t.Errorf("expected label 'Work Token', got %s", updated.Label)
	}

	// Clean up added test account from store
	_ = cfgStore.RemoveAccount(acc.ID)
}

func TestApp_SavePATAccount(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"login":"monalisa","name":"Mona Lisa","email":"mona@github.com","avatar_url":"https://avatars.githubusercontent.com/u/123"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer mockServer.Close()

	githubapi.SetTestBaseURL(mockServer.URL)
	defer githubapi.SetTestBaseURL("")

	cfgStore, err := config.NewStore()
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	log := logger.New(logger.Prefs{LogMaxSizeMB: 5, LogMaxFiles: 2}, t.TempDir(), nil)
	defer log.Close()

	app := &App{
		cfg:    cfgStore,
		log:    log,
		github: githubapi.NewPool(cfgStore, log),
	}

	acc, err := app.SavePATAccount("Work Token", "ghp_pat_token_test_12345")
	if err != nil {
		t.Fatalf("SavePATAccount error: %v", err)
	}
	if acc.Username != "monalisa" {
		t.Errorf("expected username monalisa, got %s", acc.Username)
	}
	if acc.Label != "Work Token" {
		t.Errorf("expected label Work Token, got %s", acc.Label)
	}

	saved := cfgStore.FindAccount(acc.ID)
	if saved == nil {
		t.Fatalf("account %s not persisted in store", acc.ID)
	}
	_ = cfgStore.RemoveAccount(acc.ID)
}

func TestApp_StartGitHubOAuth(t *testing.T) {
	// 1. Mock GitHub token exchange endpoint
	mockTokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":"gho_mock_oauth_flow_token","token_type":"bearer"}`)
	}))
	defer mockTokenServer.Close()

	// 2. Mock GitHub user API endpoint
	mockUserServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"login":"octocat_oauth","name":"Octo OAuth","email":"octo_oauth@github.com","avatar_url":"https://avatars.githubusercontent.com/u/999"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer mockUserServer.Close()

	githubapi.SetTestBaseURL(mockUserServer.URL)
	defer githubapi.SetTestBaseURL("")

	cfgStore, err := config.NewStore()
	if err != nil {
		t.Fatalf("failed to create config store: %v", err)
	}

	log := logger.New(logger.Prefs{LogMaxSizeMB: 5, LogMaxFiles: 2}, t.TempDir(), nil)
	defer log.Close()

	pool := githubapi.NewPool(cfgStore, log)
	oauth := pool.OAuth()
	oauth.SetPort(54324)
	oauth.SetEndpoints("", mockTokenServer.URL, "")

	app := &App{
		cfg:    cfgStore,
		log:    log,
		github: pool,
	}

	accChan := make(chan config.Account, 1)
	errChan := make(chan error, 1)

	go func() {
		acc, err := app.StartGitHubOAuth()
		if err != nil {
			errChan <- err
			return
		}
		accChan <- acc
	}()

	// Give the server 50ms to bind
	time.Sleep(50 * time.Millisecond)

	// Test CancelGitHubOAuth
	app.CancelGitHubOAuth()

	select {
	case err := <-errChan:
		if err == nil {
			t.Fatal("expected error on cancellation, got nil")
		}
	case <-accChan:
		t.Fatal("unexpected success after cancellation")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for OAuth cancellation")
	}
}




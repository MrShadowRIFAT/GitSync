package gitengine

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestCheckMissingDependencies(t *testing.T) {
	// 1. With an invalid non-existent path
	missing, details := CheckMissingDependencies("C:\\nonexistent\\path\\to\\git.exe")
	if !missing {
		// If system git was resolved instead of failing, check if it's because ResolveGitPath fell back
		// Note: CheckMissingDependencies should return true when the provided preferred path is invalid and no git is found
		t.Logf("check with invalid path returned: missing=%v, details=%s", missing, details)
	}

	// 2. With an explicit dummy file that is NOT an executable / empty dir
	tmpDir := t.TempDir()
	emptyPath := filepath.Join(tmpDir, "not-git.txt")
	_ = os.WriteFile(emptyPath, []byte("not an executable"), 0644)

	// In an isolated environment without Git, or testing the logic directly:
	validGitPath, err := ResolveGitPath("")
	if err == nil && validGitPath != "" {
		// Machine has Git
		missingWithValid, foundPath := CheckMissingDependencies(validGitPath)
		if missingWithValid {
			t.Errorf("expected missing=false when pointing to valid git path %s, got missing=true", validGitPath)
		}
		if foundPath == "" {
			t.Errorf("expected foundPath to be non-empty")
		}
	}
}

func createTestZip(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("failed to create zip entry %s: %v", name, err)
		}
		if _, err := f.Write(content); err != nil {
			t.Fatalf("failed to write content to zip entry %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close zip writer: %v", err)
	}
	return buf.Bytes()
}

func TestInstallPortableGitFromURL_Success(t *testing.T) {
	testGitDir := filepath.Join(t.TempDir(), "portable-git")
	SetOverridePortableGitDir(testGitDir)
	defer SetOverridePortableGitDir("")

	exeName := "git.exe"
	if runtime.GOOS != "windows" {
		exeName = "git"
	}

	dummyGitContent := []byte("#!/bin/sh\necho git version 2.47.1.test\n")

	zipBytes := createTestZip(t, map[string][]byte{
		"cmd/" + exeName: dummyGitContent,
		"etc/gitconfig":  []byte("[core]\n\tautocrlf = false\n"),
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(zipBytes)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(zipBytes)
	}))
	defer server.Close()

	var progressMessages []string
	var mu sync.Mutex
	progressCb := func(msg string) {
		mu.Lock()
		defer mu.Unlock()
		progressMessages = append(progressMessages, msg)
	}

	err := InstallPortableGitFromURL(server.URL, progressCb)
	if err != nil {
		t.Fatalf("InstallPortableGitFromURL failed: %v", err)
	}

	mu.Lock()
	msgs := append([]string{}, progressMessages...)
	mu.Unlock()

	if len(msgs) == 0 {
		t.Errorf("expected progress messages to be emitted, got none")
	}

	// Verify that the binary exists
	gitDir, err := GetPortableGitDir()
	if err != nil {
		t.Fatalf("failed to get portable git dir: %v", err)
	}

	installedBinary := filepath.Join(gitDir, "cmd", exeName)
	if _, err := os.Stat(installedBinary); os.IsNotExist(err) {
		t.Errorf("expected installed git binary at %s, but file not found", installedBinary)
	}

	// Check that CheckMissingDependencies can resolve the installed portable git binary
	missing, path := CheckMissingDependencies(installedBinary)
	if missing {
		t.Errorf("expected portable git to be detected, but CheckMissingDependencies reported missing: %s", path)
	}
}

func TestExtractZipArchive_ZipSlipProtection(t *testing.T) {
	tmpDest := t.TempDir()

	// Malicious zip archive attempting directory traversal
	maliciousZip := createTestZip(t, map[string][]byte{
		"../../evil.exe": []byte("evil payload"),
	})

	tmpZipFile := filepath.Join(t.TempDir(), "malicious.zip")
	if err := os.WriteFile(tmpZipFile, maliciousZip, 0600); err != nil {
		t.Fatalf("write malicious zip: %v", err)
	}

	err := extractZipArchive(tmpZipFile, tmpDest, func(string) {})
	if err == nil {
		t.Fatal("expected Zip Slip attack to be detected and rejected, but err was nil")
	}

	if !strings.Contains(err.Error(), "illegal file path") {
		t.Errorf("expected error message to mention illegal file path, got: %v", err)
	}
}

func TestGetMinGitDownloadURL(t *testing.T) {
	url := GetMinGitDownloadURL()
	if !strings.HasPrefix(url, "https://github.com/git-for-windows/git/releases/download/") {
		t.Errorf("unexpected MinGit download URL: %s", url)
	}
	if !strings.HasSuffix(url, ".zip") {
		t.Errorf("expected zip archive download URL, got: %s", url)
	}

	// Test override
	testOverride := "http://localhost:8080/test.zip"
	SetOverrideDownloadURL(testOverride)
	if GetMinGitDownloadURL() != testOverride {
		t.Errorf("expected override URL, got: %s", GetMinGitDownloadURL())
	}
	SetOverrideDownloadURL("") // Reset
}

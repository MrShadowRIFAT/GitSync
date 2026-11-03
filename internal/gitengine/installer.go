package gitengine

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/user/gitsync/internal/config"
)

var (
	// DefaultMinGitReleaseBase is the official Git for Windows release tag.
	DefaultMinGitVersion = "v2.47.1.windows.1"

	// overrideURL is used for unit testing.
	overrideURL   string
	overrideURLMu sync.RWMutex

	// overrideGitDir is used for unit testing isolation.
	overrideGitDir   string
	overrideGitDirMu sync.RWMutex
)

// SetOverrideDownloadURL sets an alternative download URL (e.g. for mock test servers).
func SetOverrideDownloadURL(url string) {
	overrideURLMu.Lock()
	defer overrideURLMu.Unlock()
	overrideURL = url
}

// SetOverridePortableGitDir sets an isolated destination directory for portable git (e.g. for unit tests).
func SetOverridePortableGitDir(dir string) {
	overrideGitDirMu.Lock()
	defer overrideGitDirMu.Unlock()
	overrideGitDir = dir
}

// GetMinGitDownloadURL returns the official GitHub release URL for MinGit based on system architecture.
func GetMinGitDownloadURL() string {
	overrideURLMu.RLock()
	defer overrideURLMu.RUnlock()
	if overrideURL != "" {
		return overrideURL
	}

	arch := "64-bit"
	if runtime.GOARCH == "386" {
		arch = "32-bit"
	}
	return fmt.Sprintf(
		"https://github.com/git-for-windows/git/releases/download/%s/MinGit-2.47.1-%s.zip",
		DefaultMinGitVersion,
		arch,
	)
}

// GetPortableGitDir returns the root directory for standalone portable Git (~/.gitsync/bin/git).
func GetPortableGitDir() (string, error) {
	overrideGitDirMu.RLock()
	defer overrideGitDirMu.RUnlock()
	if overrideGitDir != "" {
		return overrideGitDir, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".gitsync", "bin", "git"), nil
}

// GetPortableGitBinary returns the expected executable path (~/.gitsync/bin/git/cmd/git.exe).
func GetPortableGitBinary() (string, error) {
	dir, err := GetPortableGitDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(dir, "cmd", "git.exe"), nil
	}
	return filepath.Join(dir, "cmd", "git"), nil
}

// CheckMissingDependencies checks if git can be resolved via ResolveGitPath().
// Returns missing (true if Git is not resolved) and either the resolved path or error reason.
func CheckMissingDependencies(customPath ...string) (bool, string) {
	var preferred string
	if len(customPath) > 0 && customPath[0] != "" {
		preferred = customPath[0]
	} else {
		// Read preference if store is available
		if cfgStore, err := config.NewStore(); err == nil {
			preferred = cfgStore.Config().Preferences.GitPath
		}
	}

	path, err := ResolveGitPath(preferred)
	if err != nil {
		return true, err.Error()
	}
	return false, path
}

// InstallPortableGit securely downloads a lightweight portable Git (MinGit zip archive)
// from the official GitHub release URL, extracts it into ~/.gitsync/bin/git/cmd/git.exe,
// and updates preferences with the new path.
func InstallPortableGit(progressCb func(string)) error {
	return InstallPortableGitFromURL(GetMinGitDownloadURL(), progressCb)
}

// InstallPortableGitFromURL downloads and installs portable Git from a specified URL.
func InstallPortableGitFromURL(downloadURL string, progressCb func(string)) error {
	if progressCb == nil {
		progressCb = func(string) {}
	}

	gitDir, err := GetPortableGitDir()
	if err != nil {
		return err
	}
	targetExe, err := GetPortableGitBinary()
	if err != nil {
		return err
	}

	// 1. Ensure destination and temp directories exist
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		return fmt.Errorf("create portable git directory: %w", err)
	}

	home, _ := os.UserHomeDir()
	tempZipDir := filepath.Join(home, ".gitsync", "bin")
	overrideGitDirMu.RLock()
	currentOverride := overrideGitDir
	overrideGitDirMu.RUnlock()
	if currentOverride != "" {
		tempZipDir = filepath.Join(currentOverride, "temp")
	}
	_ = os.MkdirAll(tempZipDir, 0755)
	tempZipPath := filepath.Join(tempZipDir, fmt.Sprintf("git-download-%d.zip", time.Now().UnixNano()))
	defer os.Remove(tempZipPath)

	// 2. Download archive with progress
	progressCb("Connecting to official Git release repository...")
	client := &http.Client{
		Timeout: 10 * time.Minute,
	}

	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	req.Header.Set("User-Agent", "GitSync-DependencyInstaller/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download portable git failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download portable git: server returned HTTP %s", resp.Status)
	}

	out, err := os.OpenFile(tempZipPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create temporary download file: %w", err)
	}

	totalSize := resp.ContentLength
	progressCb("Downloading portable Git package...")

	var downloaded int64
	buf := make([]byte, 64*1024)
	lastReport := time.Now()

	for {
		nr, rerr := resp.Body.Read(buf)
		if nr > 0 {
			nw, werr := out.Write(buf[0:nr])
			if werr != nil {
				out.Close()
				return fmt.Errorf("write download chunk: %w", werr)
			}
			if nr != nw {
				out.Close()
				return io.ErrShortWrite
			}
			downloaded += int64(nw)

			// Report download progress every 200ms or on completion
			if time.Since(lastReport) > 200*time.Millisecond || (rerr == io.EOF && totalSize > 0) {
				lastReport = time.Now()
				if totalSize > 0 {
					pct := int((downloaded * 100) / totalSize)
					mbDownloaded := float64(downloaded) / (1024 * 1024)
					mbTotal := float64(totalSize) / (1024 * 1024)
					progressCb(fmt.Sprintf("Downloading Git package (%d%% - %.1f / %.1f MB)...", pct, mbDownloaded, mbTotal))
				} else {
					mbDownloaded := float64(downloaded) / (1024 * 1024)
					progressCb(fmt.Sprintf("Downloading Git package (%.1f MB received)...", mbDownloaded))
				}
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			out.Close()
			return fmt.Errorf("read download stream: %w", rerr)
		}
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close downloaded archive: %w", err)
	}

	// 3. Extract ZIP archive with Zip Slip protection
	progressCb("Extracting portable Git binaries...")
	if err := extractZipArchive(tempZipPath, gitDir, progressCb); err != nil {
		return fmt.Errorf("extract portable git: %w", err)
	}

	// 4. Verify that git executable exists
	resolvedExe := checkGitExecutable(targetExe)
	if resolvedExe == "" {
		// Try fallback locations inside gitDir
		fallbacks := []string{
			filepath.Join(gitDir, "cmd", "git.exe"),
			filepath.Join(gitDir, "bin", "git.exe"),
			filepath.Join(gitDir, "git.exe"),
			filepath.Join(gitDir, "cmd", "git"),
			filepath.Join(gitDir, "bin", "git"),
			filepath.Join(gitDir, "git"),
		}
		for _, fb := range fallbacks {
			if p := checkGitExecutable(fb); p != "" {
				resolvedExe = p
				break
			}
		}
	}

	if resolvedExe == "" {
		return fmt.Errorf("git binary could not be verified in %s after extraction", gitDir)
	}

	// Set executable permissions on non-Windows platforms
	if runtime.GOOS != "windows" {
		_ = os.Chmod(resolvedExe, 0755)
	}

	progressCb("Configuring GitSync preferences...")

	// 5. Update preferences with the new path (skip global store when running isolated in unit tests)
	if currentOverride == "" {
		if cfgStore, err := config.NewStore(); err == nil {
			prefs := cfgStore.Config().Preferences
			prefs.GitPath = resolvedExe
			if saveErr := cfgStore.UpdatePreferences(prefs); saveErr != nil {
				return fmt.Errorf("save configured git path: %w", saveErr)
			}
		}
	}

	// Test executing the binary if possible
	cmd := exec.Command(resolvedExe, "--version")
	configureHiddenCmd(cmd)
	if outBytes, err := cmd.Output(); err == nil {
		progressCb(fmt.Sprintf("Successfully verified %s", strings.TrimSpace(string(outBytes))))
	} else {
		progressCb("Portable Git configured and ready!")
	}

	return nil
}

// extractZipArchive safely unpacks a zip archive into targetDir, protecting against Zip Slip.
func extractZipArchive(zipFile, targetDir string, progressCb func(string)) error {
	reader, err := zip.OpenReader(zipFile)
	if err != nil {
		return fmt.Errorf("open zip archive: %w", err)
	}
	defer reader.Close()

	totalFiles := len(reader.File)
	cleanTarget := filepath.Clean(targetDir)

	lastReport := time.Now()
	for i, f := range reader.File {
		// Prevent Zip Slip vulnerability
		extractedPath := filepath.Join(cleanTarget, f.Name)
		cleanExtracted := filepath.Clean(extractedPath)
		if !strings.HasPrefix(cleanExtracted, cleanTarget+string(filepath.Separator)) && cleanExtracted != cleanTarget {
			return fmt.Errorf("illegal file path in zip archive: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(cleanExtracted, 0755); err != nil {
				return fmt.Errorf("create dir %s: %w", cleanExtracted, err)
			}
			continue
		}

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(cleanExtracted), 0755); err != nil {
			return fmt.Errorf("create parent dir for %s: %w", cleanExtracted, err)
		}

		// Extract file contents
		if err := extractZipFile(f, cleanExtracted); err != nil {
			return fmt.Errorf("extract file %s: %w", f.Name, err)
		}

		if time.Since(lastReport) > 250*time.Millisecond || i == totalFiles-1 {
			lastReport = time.Now()
			pct := ((i + 1) * 100) / totalFiles
			progressCb(fmt.Sprintf("Extracting Git components (%d%% - %d/%d files)...", pct, i+1, totalFiles))
		}
	}

	return nil
}

func extractZipFile(f *zip.File, destPath string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	// Truncate or create target file
	mode := f.Mode()
	if mode == 0 {
		mode = 0644
	}
	outFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, rc)
	return err
}

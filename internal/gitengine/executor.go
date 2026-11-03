package gitengine

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/user/gitsync/internal/logger"
)

// Executor wraps the system or portable git binary with logging.
type Executor struct {
	gitPath string
	log     *logger.Logger
}

// ResolveGitPath discovers a functional git binary, prioritizing user-configured paths,
// bundled portable Git installations, standard platform locations, and system PATH.
func ResolveGitPath(customPath string) (string, error) {
	// 1. Explicit user-configured path
	if customPath != "" {
		if path := checkGitExecutable(customPath); path != "" {
			return path, nil
		}
	}

	// 2. Portable Git next to current running executable
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidates := []string{
			filepath.Join(exeDir, "git", "cmd", "git.exe"),
			filepath.Join(exeDir, "git", "bin", "git.exe"),
			filepath.Join(exeDir, "bin", "git.exe"),
			filepath.Join(exeDir, "git.exe"),
			filepath.Join(exeDir, "portable-git", "bin", "git.exe"),
			filepath.Join(exeDir, "git", "bin", "git"),
			filepath.Join(exeDir, "bin", "git"),
			filepath.Join(exeDir, "git"),
		}
		for _, candidate := range candidates {
			if path := checkGitExecutable(candidate); path != "" {
				return path, nil
			}
		}
	}

	// 3. Portable Git in current working directory
	cwdCandidates := []string{
		filepath.Join(".", "git", "cmd", "git.exe"),
		filepath.Join(".", "git", "bin", "git.exe"),
		filepath.Join(".", "bin", "git.exe"),
		filepath.Join(".", "git.exe"),
		filepath.Join(".", "portable-git", "bin", "git.exe"),
		filepath.Join(".", "bin", "git"),
	}
	for _, candidate := range cwdCandidates {
		if path := checkGitExecutable(candidate); path != "" {
			return path, nil
		}
	}

	// 4. Standalone portable Git in ~/.gitsync/bin/git
	if home, err := os.UserHomeDir(); err == nil {
		gitsyncCandidates := []string{
			filepath.Join(home, ".gitsync", "bin", "git", "cmd", "git.exe"),
			filepath.Join(home, ".gitsync", "bin", "git", "bin", "git.exe"),
			filepath.Join(home, ".gitsync", "bin", "git", "git.exe"),
			filepath.Join(home, ".gitsync", "bin", "git", "cmd", "git"),
			filepath.Join(home, ".gitsync", "bin", "git", "bin", "git"),
			filepath.Join(home, ".gitsync", "bin", "git", "git"),
		}
		for _, candidate := range gitsyncCandidates {
			if path := checkGitExecutable(candidate); path != "" {
				return path, nil
			}
		}
	}

	// 5. Standard platform installation locations (Windows)
	if runtime.GOOS == "windows" {
		localAppData := os.Getenv("LOCALAPPDATA")
		programFiles := os.Getenv("ProgramFiles")
		programFilesX86 := os.Getenv("ProgramFiles(x86)")
		portableEnv := os.Getenv("PORTABLE_GIT")

		winCandidates := []string{
			filepath.Join(localAppData, "Programs", "Git", "cmd", "git.exe"),
			filepath.Join(localAppData, "Programs", "Git", "bin", "git.exe"),
			filepath.Join(programFiles, "Git", "cmd", "git.exe"),
			filepath.Join(programFiles, "Git", "bin", "git.exe"),
			filepath.Join(programFilesX86, "Git", "cmd", "git.exe"),
			filepath.Join(programFilesX86, "Git", "bin", "git.exe"),
			filepath.Join(portableEnv, "cmd", "git.exe"),
			filepath.Join(portableEnv, "bin", "git.exe"),
		}
		for _, candidate := range winCandidates {
			if candidate != "" {
				if path := checkGitExecutable(candidate); path != "" {
					return path, nil
				}
			}
		}
	}

	// 5. System PATH
	if path, err := exec.LookPath("git"); err == nil {
		if abs, absErr := filepath.Abs(path); absErr == nil {
			return abs, nil
		}
		return path, nil
	}
	if runtime.GOOS == "windows" {
		if path, err := exec.LookPath("git.exe"); err == nil {
			if abs, absErr := filepath.Abs(path); absErr == nil {
				return abs, nil
			}
			return path, nil
		}
	}

	return "", fmt.Errorf("git binary not found. Please install Git or specify a custom path in Preferences")
}

func checkGitExecutable(target string) string {
	if target == "" {
		return ""
	}
	info, err := os.Stat(target)
	if err != nil {
		return ""
	}
	if info.IsDir() {
		// If a directory was provided, check common git binary subpaths
		subpaths := []string{
			filepath.Join(target, "cmd", "git.exe"),
			filepath.Join(target, "bin", "git.exe"),
			filepath.Join(target, "git.exe"),
			filepath.Join(target, "bin", "git"),
			filepath.Join(target, "git"),
		}
		for _, sp := range subpaths {
			if spi, spErr := os.Stat(sp); spErr == nil && !spi.IsDir() {
				if abs, absErr := filepath.Abs(sp); absErr == nil {
					return abs
				}
				return sp
			}
		}
		return ""
	}

	if abs, absErr := filepath.Abs(target); absErr == nil {
		return abs
	}
	return target
}

// NewExecutor resolves the git binary path and creates an Executor.
func NewExecutor(log *logger.Logger, customPath ...string) (*Executor, error) {
	var preferred string
	if len(customPath) > 0 {
		preferred = customPath[0]
	}

	path, err := ResolveGitPath(preferred)
	if err != nil {
		return nil, err
	}
	return &Executor{gitPath: path, log: log}, nil
}

// Path returns the resolved git binary path.
func (e *Executor) Path() string {
	return e.gitPath
}

// Run executes a git command in the given working directory and returns stdout.
func (e *Executor) Run(dir string, args ...string) (string, error) {
	cmd := exec.Command(e.gitPath, args...)
	cmd.Dir = dir
	configureHiddenCmd(cmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	e.log.Debug("git", "$ git %s (in %s)", strings.Join(args, " "), dir)

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// IsRepo reports whether the given directory is inside a git repository.
func (e *Executor) IsRepo(dir string) bool {
	_, err := e.Run(dir, "rev-parse", "--git-dir")
	return err == nil
}

// HasRemote reports whether the repo has a remote named 'origin'.
func (e *Executor) HasRemote(dir string) bool {
	out, err := e.Run(dir, "remote")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "origin" {
			return true
		}
	}
	return false
}

// HasUncommittedChanges reports whether the repo has staged or unstaged changes.
func (e *Executor) HasUncommittedChanges(dir string) bool {
	out, err := e.Run(dir, "status", "--porcelain")
	return err == nil && strings.TrimSpace(out) != ""
}

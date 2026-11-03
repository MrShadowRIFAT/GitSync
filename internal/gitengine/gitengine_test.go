package gitengine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/user/gitsync/internal/config"
)

func TestBuildCommitMsg(t *testing.T) {
	msg := buildCommitMsg("")
	if !strings.HasPrefix(msg, "chore: auto-sync") {
		t.Errorf("unexpected default commit message: %q", msg)
	}
	if strings.Contains(msg, "{{timestamp}}") {
		t.Error("timestamp placeholder was not replaced")
	}

	custom := buildCommitMsg("feat: update {{timestamp}}")
	if !strings.HasPrefix(custom, "feat: update") {
		t.Errorf("custom template not applied: %q", custom)
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"my-repo", "my-repo"},
		{"user/repo", "user-repo"},
		{"my repo", "my_repo"},
	}
	for _, tt := range tests {
		got := sanitizeName(tt.input)
		if got != tt.expected {
			t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestSanitizeBranchName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"DESKTOP-ABC 123", "DESKTOP-ABC-123"},
		{"user@work/machine", "user-work-machine"},
		{"machine..name", "machine-name"},
		{"", "host"},
	}
	for _, tt := range tests {
		got := sanitizeBranchName(tt.input)
		if got != tt.expected {
			t.Errorf("sanitizeBranchName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestIsNetworkOrAuthError(t *testing.T) {
	tests := []struct {
		msg      string
		expected bool
	}{
		{"fatal: Could not resolve host: github.com", true},
		{"fatal: Authentication failed for 'https://github.com/...'", true},
		{"fatal: Connection refused", true},
		{"error: failed to push some refs to 'origin'", false},
		{"CONFLICT (content): Merge conflict in file.txt", false},
	}
	for _, tt := range tests {
		got := isNetworkOrAuthError(tt.msg)
		if got != tt.expected {
			t.Errorf("isNetworkOrAuthError(%q) = %v, want %v", tt.msg, got, tt.expected)
		}
	}
}

func TestSnapshotCreatesZip(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello world"), 0644)
	os.WriteFile(filepath.Join(dir, "data.json"), []byte(`{"key":"val"}`), 0644)

	backupDir := t.TempDir()

	e := &Engine{log: newNilLogger()}

	repo := &config.RepoConfig{
		LocalPath:  dir,
		RemoteName: "test-repo",
		BackupDir:  backupDir,
	}

	if err := e.Snapshot(repo); err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}

	// Verify zip was created in backupDir
	matches, err := filepath.Glob(filepath.Join(backupDir, "test-repo-*.zip"))
	if err != nil || len(matches) == 0 {
		t.Error("expected snapshot zip file in backup dir")
	}
}

func TestSnapshotSkipsWhenBackupsDisabled(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello world"), 0644)

	backupDir := t.TempDir()

	cfgStore, err := config.NewStore()
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	origBackups := cfgStore.Config().Preferences.BackupsEnabled
	defer func() {
		prefs := cfgStore.Config().Preferences
		prefs.BackupsEnabled = origBackups
		_ = cfgStore.UpdatePreferences(prefs)
	}()

	prefs := cfgStore.Config().Preferences
	prefs.BackupsEnabled = false
	_ = cfgStore.UpdatePreferences(prefs)

	e := &Engine{log: newNilLogger(), cfg: cfgStore}
	repo := &config.RepoConfig{
		LocalPath:  dir,
		RemoteName: "disabled-backup-repo",
		BackupDir:  backupDir,
	}

	if err := e.Snapshot(repo); err != nil {
		t.Fatalf("snapshot returned error when disabled: %v", err)
	}

	matches, _ := filepath.Glob(filepath.Join(backupDir, "disabled-backup-repo-*.zip"))
	if len(matches) != 0 {
		t.Errorf("expected 0 snapshot zip files when backups disabled, got %d", len(matches))
	}
}

func TestResolveGitPath(t *testing.T) {
	// 1. Should find system git if present
	path, err := ResolveGitPath("")
	if err != nil {
		t.Logf("system git not available: %v", err)
	} else if path == "" {
		t.Error("expected non-empty path when ResolveGitPath returns no error")
	}

	// 2. Custom path pointing to mock executable
	tmpDir := t.TempDir()
	mockGit := filepath.Join(tmpDir, "mock-git.exe")
	if err := os.WriteFile(mockGit, []byte("echo git"), 0755); err != nil {
		t.Fatalf("failed to write mock git: %v", err)
	}

	found, err := ResolveGitPath(mockGit)
	if err != nil {
		t.Fatalf("failed to resolve custom git path: %v", err)
	}
	if !strings.EqualFold(filepath.Clean(found), filepath.Clean(mockGit)) {
		t.Errorf("expected %s, got %s", mockGit, found)
	}

	// 3. Custom directory containing cmd/git.exe
	cmdDir := filepath.Join(tmpDir, "cmd")
	os.MkdirAll(cmdDir, 0755)
	cmdGit := filepath.Join(cmdDir, "git.exe")
	os.WriteFile(cmdGit, []byte("echo git"), 0755)

	foundDir, err := ResolveGitPath(tmpDir)
	if err != nil {
		t.Fatalf("failed to resolve directory git path: %v", err)
	}
	if !strings.EqualFold(filepath.Clean(foundDir), filepath.Clean(cmdGit)) {
		t.Errorf("expected %s, got %s", cmdGit, foundDir)
	}
}

func TestGitRefSnapshot(t *testing.T) {
	gitPath, err := ResolveGitPath("")
	if err != nil {
		t.Skip("git binary not available on system, skipping git ref snapshot test")
	}

	dir := t.TempDir()
	backupDir := t.TempDir()

	e := New(newNilLogger(), gitPath)
	if !e.Available() {
		t.Skip("git engine not available")
	}

	repo := &config.RepoConfig{
		LocalPath:  dir,
		RemoteName: "test-snapshot-repo",
		BackupDir:  backupDir,
	}

	// Initialize repo
	e.EnsureInit(repo)
	_ = e.configureAccountIdentity(dir, &config.Account{Username: "Tester", Email: "test@example.com"})

	// Create and commit a file
	filePath := filepath.Join(dir, "README.md")
	os.WriteFile(filePath, []byte("# Hello GitSync"), 0644)
	_, _ = e.exec.Run(dir, "add", "README.md")
	_, err = e.exec.Run(dir, "commit", "-m", "Initial commit")
	if err != nil {
		t.Fatalf("git commit failed: %v", err)
	}

	// Take snapshot
	if err := e.Snapshot(repo); err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}

	// Verify lightweight ref was created under refs/gitsync/backups/
	refsOut, err := e.exec.Run(dir, "for-each-ref", "--format=%(refname)", "refs/gitsync/backups")
	if err != nil {
		t.Fatalf("failed to list git refs: %v", err)
	}
	if !strings.Contains(refsOut, "refs/gitsync/backups/") {
		t.Errorf("expected backup ref under refs/gitsync/backups/, got: %q", refsOut)
	}

	// Verify zip was also created
	matches, _ := filepath.Glob(filepath.Join(backupDir, "test-snapshot-repo-*.zip"))
	if len(matches) == 0 {
		t.Error("expected backup zip file in backup dir")
	}
}

func TestUnstageLargeFiles(t *testing.T) {
	gitPath, err := ResolveGitPath("")
	if err != nil {
		t.Skip("git binary not available on system")
	}

	dir := t.TempDir()
	e := New(newNilLogger(), gitPath)
	repo := &config.RepoConfig{LocalPath: dir, RemoteName: "large-file-repo"}
	e.EnsureInit(repo)

	// Create a small file and a large file (> 100 bytes for test threshold)
	smallFile := filepath.Join(dir, "small.txt")
	os.WriteFile(smallFile, []byte("small file"), 0644)

	largeFile := filepath.Join(dir, "large.bin")
	os.WriteFile(largeFile, make([]byte, 500), 0644)

	_, _ = e.exec.Run(dir, "add", "--all")

	// Set threshold to 200 bytes
	if err := e.unstageLargeFiles(dir, 200); err != nil {
		t.Fatalf("unstageLargeFiles failed: %v", err)
	}

	// Verify large file was unstaged while small file remained staged
	staged, _ := e.exec.Run(dir, "diff", "--name-only", "--cached")
	if strings.Contains(staged, "large.bin") {
		t.Errorf("large.bin should have been unstaged, but is still staged: %s", staged)
	}
	if !strings.Contains(staged, "small.txt") {
		t.Errorf("small.txt should remain staged, but was not found: %s", staged)
	}
}

func TestConflictBranchIsolation(t *testing.T) {
	gitPath, err := ResolveGitPath("")
	if err != nil {
		t.Skip("git binary not available on system")
	}

	// 1. Create a bare remote repo
	remoteDir := t.TempDir()
	e := New(newNilLogger(), gitPath)
	_, err = e.exec.Run(remoteDir, "init", "--bare", "-b", "main")
	if err != nil {
		_, _ = e.exec.Run(remoteDir, "init", "--bare")
	}

	// 2. Clone to localDir1 and push initial commit
	local1 := t.TempDir()
	repo1 := &config.RepoConfig{LocalPath: local1, RemoteName: "repo1", Branch: "main"}
	e.EnsureInit(repo1)
	_ = e.configureAccountIdentity(local1, &config.Account{Username: "User1", Email: "user1@example.com"})
	_, _ = e.exec.Run(local1, "remote", "add", "origin", remoteDir)

	file1 := filepath.Join(local1, "conflict.txt")
	os.WriteFile(file1, []byte("base line\n"), 0644)
	_, _ = e.exec.Run(local1, "add", "conflict.txt")
	_, _ = e.exec.Run(local1, "commit", "-m", "base commit")
	_, err = e.exec.Run(local1, "push", "-u", "origin", "main")
	if err != nil {
		t.Fatalf("initial push failed: %v", err)
	}

	// 3. Clone to localDir2
	local2 := t.TempDir()
	_, err = e.exec.Run(local2, "clone", remoteDir, ".")
	if err != nil {
		t.Fatalf("clone to local2 failed: %v", err)
	}
	_ = e.configureAccountIdentity(local2, &config.Account{Username: "User2", Email: "user2@example.com"})

	// 4. In local1: edit conflict.txt and push to remote
	os.WriteFile(file1, []byte("remote update from user 1\n"), 0644)
	_, _ = e.exec.Run(local1, "commit", "-am", "remote update")
	_, _ = e.exec.Run(local1, "push", "origin", "main")

	// 5. In local2: make conflicting edit to conflict.txt
	file2 := filepath.Join(local2, "conflict.txt")
	os.WriteFile(file2, []byte("divergent local edit from user 2\n"), 0644)
	_, _ = e.exec.Run(local2, "commit", "-am", "local divergent commit")

	// 6. Run safePush on local2
	repo2 := &config.RepoConfig{
		LocalPath:  local2,
		RemoteName: "repo2",
		Branch:     "main",
		BackupDir:  t.TempDir(),
	}

	pushErr := e.safePush(repo2)
	if pushErr == nil {
		t.Fatal("expected safePush to return conflict error, but got nil")
	}
	if !strings.Contains(pushErr.Error(), "conflict detected") {
		t.Errorf("expected error to mention 'conflict detected', got: %v", pushErr)
	}

	// 7. Verify conflict branch was created in local2
	branchesOut, err := e.exec.Run(local2, "branch")
	if err != nil {
		t.Fatalf("failed to list branches: %v", err)
	}
	if !strings.Contains(branchesOut, "conflict/") {
		t.Errorf("expected conflict branch starting with 'conflict/', got: %s", branchesOut)
	}

	// 8. Verify main was reset back to origin's content ("remote update from user 1\n")
	mainContent, err := os.ReadFile(file2)
	if err != nil {
		t.Fatalf("failed to read reset file: %v", err)
	}
	if strings.TrimSpace(string(mainContent)) != "remote update from user 1" {
		t.Errorf("expected main branch to be reset to remote content, got: %q", string(mainContent))
	}
}

package gitengine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/user/gitsync/internal/config"
	"github.com/user/gitsync/internal/githubapi"
	"github.com/user/gitsync/internal/logger"
)

// Engine orchestrates all git operations for GitSync.
type Engine struct {
	exec       *Executor
	log        *logger.Logger
	cfg        *config.Store
	onSnapshot func(repoName, target string)
}

// SetConfigStore attaches the configuration store so Engine can read live preferences (e.g. BackupsEnabled).
func (e *Engine) SetConfigStore(cfg *config.Store) {
	if e != nil {
		e.cfg = cfg
	}
}

// SetOnSnapshot registers a callback invoked when a pre-sync backup snapshot is archived.
func (e *Engine) SetOnSnapshot(cb func(repoName, target string)) {
	e.onSnapshot = cb
}

// New creates a new Engine with optional custom git binary path. If git is not found,
// the engine will log a warning and return errors on use rather than panicking.
func New(log *logger.Logger, customGitPath ...string) *Engine {
	var preferred string
	if len(customGitPath) > 0 {
		preferred = customGitPath[0]
	}
	exec, err := NewExecutor(log, preferred)
	if err != nil {
		if log != nil {
			log.Warn("gitengine", "git binary unavailable: %v", err)
		}
		return &Engine{log: log}
	}
	if log != nil {
		log.Info("gitengine", "git found at: %s", exec.gitPath)
	}
	return &Engine{exec: exec, log: log}
}

// Available reports whether the git binary was found.
func (e *Engine) Available() bool { return e.exec != nil }

// GitPath returns the detected git binary path, or empty string if unavailable.
func (e *Engine) GitPath() string {
	if e.exec != nil {
		return e.exec.gitPath
	}
	return ""
}

// EnsureInitPath initializes a git repo at the given path if it doesn't exist.
func (e *Engine) EnsureInitPath(dir string, repo *config.RepoConfig, account ...*config.Account) error {
	if e.exec == nil {
		return fmt.Errorf("git binary unavailable")
	}
	if !e.exec.IsRepo(dir) {
		if err := e.init(dir); err != nil {
			if e.log != nil {
				e.log.Error("gitengine", "git init failed for %s: %v", dir, err)
			}
			return err
		}
		var acc *config.Account
		if len(account) > 0 {
			acc = account[0]
		}
		if repo != nil {
			if err := e.configureIdentity(dir, repo, acc); err != nil && e.log != nil {
				e.log.Warn("gitengine", "identity config failed: %v", err)
			}
		}
	}
	return nil
}

// EnsureInit initializes a git repo at the given path if it doesn't exist.
func (e *Engine) EnsureInit(repo *config.RepoConfig, account ...*config.Account) {
	_ = e.EnsureInitPath(repo.LocalPath, repo, account...)
}

// Sync performs a full auto-commit-and-push cycle.
func (e *Engine) Sync(ctx context.Context, repo *config.RepoConfig, account *config.Account, gh *githubapi.ClientPool) error {
	if e.exec == nil {
		return fmt.Errorf("git binary unavailable — please install Git")
	}

	dir := strings.TrimSpace(repo.LocalPath)
	if dir == "" {
		return nil
	}

	branch := strings.TrimSpace(repo.Branch)
	if branch == "" {
		branch = "main"
	}

	// 1. Ensure initialized
	if !e.exec.IsRepo(dir) {
		if err := e.init(dir); err != nil {
			return fmt.Errorf("git init: %w", err)
		}
	}

	// 2. Configure local identity dynamically from account
	if account != nil {
		if err := e.configureAccountIdentity(dir, account); err != nil {
			if e.log != nil {
				e.log.Warn("gitengine", "identity config warning: %v", err)
			}
		}
	}

	// 2b. Ensure the repository is checked out on the configured target branch
	// (creating or switching to the branch if the user changed it in settings)
	if err := e.ensureBranch(dir, branch); err != nil {
		return fmt.Errorf("checkout branch %s: %w", branch, err)
	}

	// 3. Take safety snapshots (git lightweight ref and zip archive)
	if err := e.Snapshot(repo); err != nil {
		if e.log != nil {
			e.log.Warn("gitengine", "snapshot skipped (non-fatal): %v", err)
		}
	}

	// 4. Stage all changes
	if _, err := e.exec.Run(dir, "add", "--all"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	// Guard against files exceeding GitHub's 100MB threshold (default 95MB safety limit)
	_ = e.unstageLargeFiles(dir, 95*1024*1024)

	// 5. Check if there's anything to commit
	status, err := e.exec.Run(dir, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) == "" {
		if e.log != nil {
			e.log.Debug("gitengine", "[%s] nothing to commit", repo.RemoteName)
		}
		// Ensure at least one commit exists on HEAD so pushing a brand-new repo/branch never fails with "src refspec does not match any"
		if !e.hasCommits(dir) {
			initMsg := buildCommitMsg(repo.CommitMsg, branch, "")
			if _, cErr := e.exec.Run(dir, "commit", "--allow-empty", "-m", initMsg); cErr != nil {
				return fmt.Errorf("initial git commit: %w", cErr)
			}
		}
		// Ensure remote origin (creates GitHub repo if needed) before pushing
		if err := e.ensureRemote(ctx, repo, account, gh); err != nil {
			return fmt.Errorf("remote setup: %w", err)
		}
		if e.exec.HasRemote(dir) {
			return e.safePush(repo)
		}
		return nil
	}

	// 6. Automated commit with template & smart file summary interpolation
	msg := buildCommitMsg(repo.CommitMsg, branch, status)
	if _, err := e.exec.Run(dir, "commit", "-m", msg); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	if e.log != nil {
		e.log.Info("gitengine", "[%s] committed: %s", repo.RemoteName, msg)
	}

	// 7. Ensure remote origin (creates GitHub repo if needed)
	if err := e.ensureRemote(ctx, repo, account, gh); err != nil {
		return fmt.Errorf("remote setup: %w", err)
	}

	// 8. Safe push with fetch, rebase, and conflict branch isolation fallback
	return e.safePush(repo)
}

// Clone clones a remote GitHub repository into targetDir (or links and pulls if targetDir already exists).
func (e *Engine) Clone(ctx context.Context, repo *config.RepoConfig, account *config.Account, gh *githubapi.ClientPool, targetDir string) error {
	if e.exec == nil {
		return fmt.Errorf("git binary unavailable — please install Git")
	}
	if account == nil || gh == nil {
		return fmt.Errorf("GitHub account is required to clone repository")
	}

	pat, err := gh.DecryptPAT(repo.AccountID)
	if err != nil {
		return fmt.Errorf("decrypt PAT: %w", err)
	}

	remoteURL := fmt.Sprintf("https://%s:%s@github.com/%s/%s.git",
		account.Username, pat, account.Username, repo.RemoteName)

	branch := strings.TrimSpace(repo.Branch)
	if branch == "" {
		branch = "main"
	}

	cleanedTarget := filepath.Clean(strings.TrimSpace(targetDir))
	parentDir := filepath.Dir(cleanedTarget)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}

	// Check if targetDir already exists and whether it is empty
	entries, readErr := os.ReadDir(cleanedTarget)
	isEmptyOrMissing := readErr != nil || len(entries) == 0

	if isEmptyOrMissing {
		_ = os.MkdirAll(cleanedTarget, 0755)
		if _, cloneErr := e.exec.Run(parentDir, "clone", remoteURL, cleanedTarget); cloneErr != nil {
			// Fallback if remote repository is completely empty or clone failed on non-empty dir
			if err := e.init(cleanedTarget); err != nil {
				return fmt.Errorf("git clone failed: %w", cloneErr)
			}
			_, _ = e.exec.Run(cleanedTarget, "remote", "add", "origin", remoteURL)
		}
	} else {
		// Directory already contains files: initialize if needed, set remote, and pull
		if !e.exec.IsRepo(cleanedTarget) {
			if err := e.init(cleanedTarget); err != nil {
				return fmt.Errorf("git init: %w", err)
			}
		}
		if e.exec.HasRemote(cleanedTarget) {
			_, _ = e.exec.Run(cleanedTarget, "remote", "set-url", "origin", remoteURL)
		} else {
			_, _ = e.exec.Run(cleanedTarget, "remote", "add", "origin", remoteURL)
		}
		if _, fetchErr := e.exec.Run(cleanedTarget, "fetch", "origin", branch); fetchErr == nil {
			_, _ = e.exec.Run(cleanedTarget, "checkout", "-B", branch, "origin/"+branch)
		}
	}

	_ = e.configureAccountIdentity(cleanedTarget, account)
	_ = e.ensureBranch(cleanedTarget, branch)
	if e.log != nil {
		e.log.Info("gitengine", "[%s] cloned/linked repository into %s", repo.RemoteName, cleanedTarget)
	}
	return nil
}

// Pull fetches and rebases the latest remote changes from GitHub into the local repository.
func (e *Engine) Pull(ctx context.Context, repo *config.RepoConfig, account *config.Account, gh *githubapi.ClientPool) error {
	if e.exec == nil {
		return fmt.Errorf("git binary unavailable — please install Git")
	}
	dir := strings.TrimSpace(repo.LocalPath)
	if dir == "" {
		return fmt.Errorf("repository is not linked to a local folder yet")
	}

	branch := strings.TrimSpace(repo.Branch)
	if branch == "" {
		branch = "main"
	}

	if !e.exec.IsRepo(dir) {
		if err := e.init(dir); err != nil {
			return fmt.Errorf("git init: %w", err)
		}
	}

	if account != nil {
		_ = e.configureAccountIdentity(dir, account)
	}
	if err := e.ensureRemote(ctx, repo, account, gh); err != nil {
		return fmt.Errorf("remote setup: %w", err)
	}
	_ = e.ensureBranch(dir, branch)

	// Fetch remote branch
	if _, err := e.exec.Run(dir, "fetch", "origin", branch); err != nil {
		if isRemoteRefNotFoundError(err.Error()) {
			return nil
		}
		return fmt.Errorf("fetch origin/%s: %w", branch, err)
	}

	// Stash uncommitted local changes if any
	stashed := false
	stashOut, _ := e.exec.Run(dir, "stash", "push", "-u", "-m", "gitsync-pull-autostash")
	if !strings.Contains(stashOut, "No local changes to save") && strings.TrimSpace(stashOut) != "" {
		stashed = true
	}

	_, pullErr := e.exec.Run(dir, "pull", "--rebase", "origin", branch)
	if stashed {
		_, _ = e.exec.Run(dir, "stash", "pop")
	}
	if pullErr != nil {
		_, _ = e.exec.Run(dir, "rebase", "--abort")
		return fmt.Errorf("git pull origin/%s: %w", branch, pullErr)
	}

	if e.log != nil {
		e.log.Info("gitengine", "[%s] pulled latest changes from origin/%s", repo.RemoteName, branch)
	}
	return nil
}

// ─── Internal helpers ──────────────────────────────────────────────────────────

func (e *Engine) init(dir string) error {
	if _, err := e.exec.Run(dir, "init", "-b", "main"); err != nil {
		// Fallback for older git versions that do not support -b flag
		if _, initErr := e.exec.Run(dir, "init"); initErr != nil {
			return initErr
		}
	}
	if e.log != nil {
		e.log.Info("gitengine", "initialized new git repo at %s", dir)
	}
	return nil
}

// hasCommits reports whether the repository has at least one commit on HEAD.
func (e *Engine) hasCommits(dir string) bool {
	_, err := e.exec.Run(dir, "rev-parse", "--verify", "HEAD")
	return err == nil
}

// ensureBranch guarantees that the repository's HEAD is on the target branch.
// If the repo has no commits yet, it updates the unborn HEAD symbolic ref;
// if commits exist and the current branch differs, it switches to or creates the target branch.
func (e *Engine) ensureBranch(dir, branch string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = "main"
	}

	if !e.hasCommits(dir) {
		_, err := e.exec.Run(dir, "symbolic-ref", "HEAD", "refs/heads/"+branch)
		return err
	}

	curBranch, err := e.exec.Run(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err == nil && strings.TrimSpace(curBranch) == branch {
		return nil
	}

	// Check if local branch already exists
	if _, refErr := e.exec.Run(dir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); refErr == nil {
		if _, chkErr := e.exec.Run(dir, "checkout", branch); chkErr != nil {
			return chkErr
		}
	} else {
		if _, chkErr := e.exec.Run(dir, "checkout", "-b", branch); chkErr != nil {
			return chkErr
		}
	}
	if e.log != nil {
		e.log.Info("gitengine", "switched local branch to %s in %s", branch, dir)
	}
	return nil
}

// configureIdentity sets core configs, default branch, and user credentials.
func (e *Engine) configureIdentity(dir string, repo *config.RepoConfig, account *config.Account) error {
	_, _ = e.exec.Run(dir, "config", "core.autocrlf", "false")

	branch := repo.Branch
	if branch == "" {
		branch = "main"
	}
	_, _ = e.exec.Run(dir, "symbolic-ref", "HEAD", "refs/heads/"+branch)

	if account != nil {
		return e.configureAccountIdentity(dir, account)
	}
	return nil
}

// configureAccountIdentity sets user.name and user.email dynamically from an Account.
func (e *Engine) configureAccountIdentity(dir string, account *config.Account) error {
	if account == nil {
		return nil
	}
	name := account.Username
	if account.Label != "" && account.Label != account.Username {
		name = account.Label
	}
	if name == "" {
		name = account.Username
	}
	if name != "" {
		if _, err := e.exec.Run(dir, "config", "user.name", name); err != nil {
			return err
		}
	}
	email := strings.TrimSpace(account.Email)
	if email == "" && account.Username != "" {
		email = fmt.Sprintf("%s@users.noreply.github.com", account.Username)
	}
	if email != "" {
		if _, err := e.exec.Run(dir, "config", "user.email", email); err != nil {
			return err
		}
	}
	return nil
}

// unstageLargeFiles removes any staged files exceeding maxBytes from the current index.
func (e *Engine) unstageLargeFiles(dir string, maxBytes int64) error {
	staged, err := e.exec.Run(dir, "diff", "--name-only", "--cached")
	if err != nil {
		return nil
	}
	for _, relPath := range strings.Split(strings.TrimSpace(staged), "\n") {
		relPath = strings.TrimSpace(relPath)
		if relPath == "" {
			continue
		}
		fullPath := filepath.Join(dir, relPath)
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}
		if info.Size() > maxBytes {
			if _, err := e.exec.Run(dir, "restore", "--staged", relPath); err != nil {
				if _, resetErr := e.exec.Run(dir, "reset", "HEAD", relPath); resetErr != nil {
					_, _ = e.exec.Run(dir, "rm", "--cached", relPath)
				}
			}
			if e.log != nil {
				e.log.Warn("gitengine", "unstaged large file exceeding %d MB: %s (%d MB)",
					maxBytes/(1024*1024), relPath, info.Size()/(1024*1024))
			}
		}
	}
	return nil
}

// ensureRemote creates or updates the 'origin' remote, creating the GitHub repo if needed.
func (e *Engine) ensureRemote(ctx context.Context, repo *config.RepoConfig, account *config.Account, gh *githubapi.ClientPool) error {
	if gh == nil || account == nil {
		return nil
	}

	pat, err := gh.DecryptPAT(repo.AccountID)
	if err != nil {
		return fmt.Errorf("decrypt PAT: %w", err)
	}

	// Ensure the GitHub repo exists
	_, err = gh.EnsureRepo(ctx, repo.AccountID, repo.RemoteName, repo.Visibility)
	if err != nil {
		return fmt.Errorf("ensure GitHub repo: %w", err)
	}

	// Configure user identity with account info
	if err := e.configureAccountIdentity(repo.LocalPath, account); err != nil && e.log != nil {
		e.log.Warn("gitengine", "identity config warning: %v", err)
	}

	// Set remote URL with embedded PAT for authentication
	remoteURL := fmt.Sprintf("https://%s:%s@github.com/%s/%s.git",
		account.Username, pat, account.Username, repo.RemoteName)

	if e.exec.HasRemote(repo.LocalPath) {
		_, err = e.exec.Run(repo.LocalPath, "remote", "set-url", "origin", remoteURL)
	} else {
		_, err = e.exec.Run(repo.LocalPath, "remote", "add", "origin", remoteURL)
	}
	return err
}

// safePush pushes to origin, falling back to fetch → rebase on divergence.
// If a merge conflict occurs, changes are safely isolated into a dedicated conflict
// branch (conflict/<hostname>/<timestamp>) and the working branch is reset to origin.
func (e *Engine) safePush(repo *config.RepoConfig) error {
	dir := repo.LocalPath
	branch := repo.Branch
	if branch == "" {
		branch = "main"
	}

	// 1. Initial push attempt
	if _, err := e.exec.Run(dir, "push", "-u", "origin", branch); err == nil {
		return nil
	} else if isNetworkOrAuthError(err.Error()) {
		// Network offline or authentication failures should not trigger rebase/conflict workflows
		return err
	}

	if e.log != nil {
		e.log.Warn("gitengine", "[%s] push rejected or remote divergence detected, attempting safe rebase", repo.RemoteName)
	}

	// 2. Fetch origin to update remote tracking refs
	if _, err := e.exec.Run(dir, "fetch", "origin", branch); err != nil {
		// If remote ref doesn't exist yet on origin, re-attempt push directly or return
		if isRemoteRefNotFoundError(err.Error()) {
			_, pushErr := e.exec.Run(dir, "push", "-u", "origin", branch)
			return pushErr
		}
		return fmt.Errorf("fetch origin/%s: %w", branch, err)
	}

	// 3. Stash uncommitted work before rebasing
	stashed := false
	stashOut, _ := e.exec.Run(dir, "stash", "push", "-m", "gitsync-auto-stash")
	if !strings.Contains(stashOut, "No local changes to save") && strings.TrimSpace(stashOut) != "" {
		stashed = true
	}

	// 4. Attempt rebase onto fetched origin/<branch>
	if _, err := e.exec.Run(dir, "pull", "--rebase", "origin", branch); err == nil {
		// Rebase succeeded!
		if stashed {
			if _, popErr := e.exec.Run(dir, "stash", "pop"); popErr != nil && e.log != nil {
				e.log.Warn("gitengine", "[%s] stash pop warning: %v", repo.RemoteName, popErr)
			}
		}
		// Push rebased branch
		if _, pushErr := e.exec.Run(dir, "push", "-u", "origin", branch); pushErr != nil {
			return fmt.Errorf("push after rebase failed for [%s]: %w", repo.RemoteName, pushErr)
		}
		return nil
	}

	// 5. Merge conflict occurred during rebase: execute safe conflict isolation
	if e.log != nil {
		e.log.Warn("gitengine", "[%s] merge conflict encountered during pull --rebase, starting conflict isolation", repo.RemoteName)
	}

	// Abort the in-flight rebase to restore local commit state
	_, _ = e.exec.Run(dir, "rebase", "--abort")

	// Construct conflict branch name: conflict/<hostname>/<timestamp>
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "host"
	}
	hostname = sanitizeBranchName(hostname)
	stamp := time.Now().Format("20060102-150405")
	conflictBranch := fmt.Sprintf("conflict/%s/%s", hostname, stamp)

	// Create and switch to the conflict branch with the local commits
	if _, err := e.exec.Run(dir, "checkout", "-b", conflictBranch); err != nil {
		if e.log != nil {
			e.log.Error("gitengine", "[%s] failed to create conflict branch %s: %v", repo.RemoteName, conflictBranch, err)
		}
	}

	// If stashed work existed, pop and commit onto the conflict branch so nothing is lost
	if stashed {
		if _, err := e.exec.Run(dir, "stash", "pop"); err == nil {
			if e.exec.HasUncommittedChanges(dir) {
				_, _ = e.exec.Run(dir, "add", "--all")
				_, _ = e.exec.Run(dir, "commit", "-m", fmt.Sprintf("chore: preserve uncommitted changes during conflict isolation %s", stamp))
			}
		}
	}

	// Try to push conflict branch to origin to preserve work remotely
	if _, err := e.exec.Run(dir, "push", "-u", "origin", conflictBranch); err != nil {
		if e.log != nil {
			e.log.Warn("gitengine", "[%s] could not push conflict branch %s to origin: %v", repo.RemoteName, conflictBranch, err)
		}
	} else if e.log != nil {
		e.log.Info("gitengine", "[%s] pushed conflict branch %s to origin", repo.RemoteName, conflictBranch)
	}

	// Switch back to primary branch
	if _, err := e.exec.Run(dir, "checkout", branch); err != nil && e.log != nil {
		e.log.Error("gitengine", "[%s] failed to switch back to %s: %v", repo.RemoteName, branch, err)
	}

	// Hard reset primary branch to origin/<branch> to restore clean sync state
	if _, err := e.exec.Run(dir, "reset", "--hard", "origin/"+branch); err != nil && e.log != nil {
		e.log.Error("gitengine", "[%s] failed to reset %s to origin/%s: %v", repo.RemoteName, branch, branch, err)
	}

	if e.log != nil {
		e.log.Warn(
			"gitengine",
			"[%s] merge conflict detected; changes safely isolated to branch '%s', local %s reset to origin",
			repo.RemoteName, conflictBranch, branch,
		)
	}

	return fmt.Errorf("conflict detected in [%s]: local commits safely isolated to branch %q, %s reset to origin",
		repo.RemoteName, conflictBranch, branch)
}

func buildCommitMsg(template string, branchAndStatus ...string) string {
	branch := "main"
	porcelain := ""
	if len(branchAndStatus) > 0 && strings.TrimSpace(branchAndStatus[0]) != "" {
		branch = strings.TrimSpace(branchAndStatus[0])
	}
	if len(branchAndStatus) > 1 {
		porcelain = branchAndStatus[1]
	}

	filesSummary := summarizeChangedFiles(porcelain)
	ts := time.Now().Format("2006-01-02 15:04:05")

	trimmed := strings.TrimSpace(template)
	if trimmed == "" {
		trimmed = "chore: auto-sync {{timestamp}}"
	}
	if strings.EqualFold(trimmed, "smart") || strings.EqualFold(trimmed, "{{smart}}") {
		if filesSummary != "" {
			return fmt.Sprintf("sync(%s): update %s", branch, filesSummary)
		}
		return fmt.Sprintf("sync(%s): auto-sync %s", branch, ts)
	}

	res := strings.ReplaceAll(trimmed, "{{timestamp}}", ts)
	res = strings.ReplaceAll(res, "{{branch}}", branch)
	if filesSummary == "" {
		filesSummary = "files"
	}
	res = strings.ReplaceAll(res, "{{files}}", filesSummary)
	return res
}

func summarizeChangedFiles(porcelain string) string {
	if strings.TrimSpace(porcelain) == "" {
		return ""
	}
	var names []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(porcelain), "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 3 {
			continue
		}
		pathPart := strings.TrimSpace(line[2:])
		if idx := strings.Index(pathPart, " -> "); idx != -1 {
			pathPart = strings.TrimSpace(pathPart[idx+4:])
		}
		pathPart = strings.Trim(pathPart, `"`)
		base := filepath.Base(pathPart)
		if base != "" && base != "." && !seen[base] {
			seen[base] = true
			names = append(names, base)
		}
	}
	if len(names) == 0 {
		return ""
	}
	if len(names) <= 2 {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, %s (+%d more)", names[0], names[1], len(names)-2)
}

func sanitizeBranchName(name string) string {
	r := strings.NewReplacer(" ", "-", "\\", "-", ":", "-", "/", "-", "@", "-", "..", "-")
	clean := r.Replace(name)
	var sb strings.Builder
	for _, ch := range clean {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.' {
			sb.WriteRune(ch)
		}
	}
	res := strings.Trim(sb.String(), "-.")
	if res == "" {
		return "host"
	}
	return res
}

func isNetworkOrAuthError(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "could not resolve host") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "network is unreachable") ||
		strings.Contains(lower, "operation timed out") ||
		strings.Contains(lower, "failed to connect") ||
		strings.Contains(lower, "authentication failed") ||
		strings.Contains(lower, "invalid username or password") ||
		strings.Contains(lower, "permission denied (publickey)") ||
		strings.Contains(lower, "bad credentials")
}

func isRemoteRefNotFoundError(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "couldn't find remote ref") ||
		strings.Contains(lower, "fatal: remote branch") ||
		strings.Contains(lower, "does not exist in the remote")
}

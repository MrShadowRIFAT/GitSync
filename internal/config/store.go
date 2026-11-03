package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Store manages reading, writing, and securing the application config.
type Store struct {
	mu     sync.RWMutex
	path   string
	config *AppConfig
	key    []byte // 32-byte machine-derived AES key
}

// NewStore creates a Store, loading from disk or initializing defaults.
func NewStore() (*Store, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}

	s := &Store{
		path: filepath.Join(dir, "config.json"),
		key:  deriveKey(),
	}

	if err := s.load(); err != nil {
		s.config = defaultConfig()
		if saveErr := s.Save(); saveErr != nil {
			return nil, fmt.Errorf("initialize default config: %w", saveErr)
		}
	}
	return s, nil
}

// Config returns a pointer to the live AppConfig under read lock.
func (s *Store) Config() *AppConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// FindAccount looks up an account by ID.
func (s *Store) FindAccount(id string) *Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.config.Accounts {
		if s.config.Accounts[i].ID == id {
			return &s.config.Accounts[i]
		}
	}
	return nil
}

// FindAccountByUsername looks up an account by its GitHub username (case-insensitive).
func (s *Store) FindAccountByUsername(username string) *Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.config.Accounts {
		if strings.EqualFold(s.config.Accounts[i].Username, username) {
			return &s.config.Accounts[i]
		}
	}
	return nil
}

// UpdateAccount updates an existing account by its ID and persists to disk.
func (s *Store) UpdateAccount(acc Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.config.Accounts {
		if s.config.Accounts[i].ID == acc.ID {
			s.config.Accounts[i] = acc
			return s.saveLocked()
		}
	}
	return fmt.Errorf("account %q not found", acc.ID)
}

// FindRepo looks up a repo by ID.
func (s *Store) FindRepo(id string) *RepoConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.config.Repos {
		if s.config.Repos[i].ID == id {
			return &s.config.Repos[i]
		}
	}
	return nil
}

// FindRepoByPath looks up a repo by its local directory path.
func (s *Store) FindRepoByPath(localPath string) *RepoConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	trimmed := strings.TrimSpace(localPath)
	if trimmed == "" {
		return nil
	}
	cleanTarget := filepath.Clean(trimmed)
	for i := range s.config.Repos {
		rp := strings.TrimSpace(s.config.Repos[i].LocalPath)
		if rp != "" && strings.EqualFold(filepath.Clean(rp), cleanTarget) {
			return &s.config.Repos[i]
		}
	}
	return nil
}

// FindRepoByAccountAndName looks up a repo by its account ID and remote repository name (case-insensitive).
func (s *Store) FindRepoByAccountAndName(accountID, remoteName string) *RepoConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.config.Repos {
		if s.config.Repos[i].AccountID == accountID && strings.EqualFold(s.config.Repos[i].RemoteName, remoteName) {
			return &s.config.Repos[i]
		}
	}
	return nil
}

// FindRepoByName looks up a repo by its remote repository name (case-insensitive).
func (s *Store) FindRepoByName(remoteName string) *RepoConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.config.Repos {
		if strings.EqualFold(s.config.Repos[i].RemoteName, remoteName) {
			return &s.config.Repos[i]
		}
	}
	return nil
}

// AddAccount appends a new account and persists to disk.
func (s *Store) AddAccount(acc Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.Accounts = append(s.config.Accounts, acc)
	return s.saveLocked()
}

// RemoveAccount deletes an account by ID and persists to disk.
func (s *Store) RemoveAccount(accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := make([]Account, 0, len(s.config.Accounts))
	for _, acc := range s.config.Accounts {
		if acc.ID != accountID {
			filtered = append(filtered, acc)
		}
	}
	s.config.Accounts = filtered
	return s.saveLocked()
}

// RemoveAccountAndRepos atomically removes an account and all of its associated remote-only
// (or linked) repositories in a single lock and single disk write.
func (s *Store) RemoveAccountAndRepos(accountID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Find the username of the account being removed (if known)
	var removedUsername string
	filteredAccounts := make([]Account, 0, len(s.config.Accounts))
	for _, acc := range s.config.Accounts {
		if acc.ID == accountID {
			removedUsername = strings.ToLower(strings.TrimSpace(acc.Username))
		} else {
			filteredAccounts = append(filteredAccounts, acc)
		}
	}
	s.config.Accounts = filteredAccounts

	// Set of currently remaining valid account IDs
	validAccounts := make(map[string]bool)
	for _, acc := range filteredAccounts {
		validAccounts[acc.ID] = true
	}

	var removedRepoIDs []string
	filteredRepos := make([]RepoConfig, 0, len(s.config.Repos))
	for _, r := range s.config.Repos {
		// Clean up repos directly linked to the removed account ID
		if r.AccountID == accountID {
			removedRepoIDs = append(removedRepoIDs, r.ID)
			continue
		}
		// Also clean up any orphan remote-only repos referencing obsolete account IDs
		if strings.TrimSpace(r.LocalPath) == "" && !validAccounts[r.AccountID] {
			removedRepoIDs = append(removedRepoIDs, r.ID)
			continue
		}
		filteredRepos = append(filteredRepos, r)
	}
	s.config.Repos = filteredRepos

	if err := s.saveLocked(); err != nil {
		return removedRepoIDs, err
	}
	_ = removedUsername
	return removedRepoIDs, nil
}

// AddRepo registers a new repo and persists to disk.
func (s *Store) AddRepo(repo RepoConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.Repos = append(s.config.Repos, repo)
	return s.saveLocked()
}

// RemoveRepo unregisters a repo by ID and persists to disk.
func (s *Store) RemoveRepo(repoID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	repos := s.config.Repos
	for i, r := range repos {
		if r.ID == repoID {
			s.config.Repos = append(repos[:i], repos[i+1:]...)
			return s.saveLocked()
		}
	}
	return nil
}

// UpdateRepo updates an existing repository configuration.
func (s *Store) UpdateRepo(repo RepoConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.config.Repos {
		if s.config.Repos[i].ID == repo.ID {
			s.config.Repos[i] = repo
			return s.saveLocked()
		}
	}
	return fmt.Errorf("repo %q not found", repo.ID)
}

// UpdatePreferences updates global preferences and persists to disk.
func (s *Store) UpdatePreferences(prefs Preferences) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.Preferences = prefs
	return s.saveLocked()
}

// EncryptPAT encrypts a plaintext PAT for storage using AES-256-GCM with a random nonce.
func (s *Store) EncryptPAT(plaintext string) (string, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("read random nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptPAT decrypts a stored encrypted PAT using AES-256-GCM.
func (s *Store) DecryptPAT(encrypted string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short: length %d, min %d", len(data), nonceSize)
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("gcm decrypt failed: %w", err)
	}
	return string(plaintext), nil
}

// Save persists the current config to disk using an atomic temporary-file write.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			time.Sleep(25 * time.Millisecond)
		}
		tmpPath := s.path + fmt.Sprintf(".tmp-%d-%d", time.Now().UnixNano(), attempt)
		if err := os.WriteFile(tmpPath, data, 0600); err != nil {
			lastErr = fmt.Errorf("write tmp config: %w", err)
			continue
		}

		// Windows-safe atomic replace:
		_ = os.Remove(s.path)
		if err := os.Rename(tmpPath, s.path); err != nil {
			_ = os.Remove(tmpPath)
			if wErr := os.WriteFile(s.path, data, 0600); wErr != nil {
				lastErr = wErr
				continue
			}
		}
		return nil
	}
	return lastErr
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	cfg := &AppConfig{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return err
	}
	if err := migrate(cfg); err != nil {
		return fmt.Errorf("migrate config: %w", err)
	}
	s.config = cfg
	return nil
}

// deriveKey creates a deterministic 32-byte key from hardware and OS identifiers.
func deriveKey() []byte {
	platformID := getPlatformMachineID()
	hostname, _ := os.Hostname()
	homeDir, _ := os.UserHomeDir()
	username := os.Getenv("USERNAME")
	if username == "" {
		username = os.Getenv("USER")
	}

	seed := fmt.Sprintf("%s|%s|%s|%s|%s|%s|gitsync-salt-v2",
		platformID, hostname, homeDir, username, runtime.GOOS, runtime.GOARCH)

	hash := sha256.Sum256([]byte(seed))
	return hash[:]
}

func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gitsync"), nil
}

func defaultConfig() *AppConfig {
	home, _ := os.UserHomeDir()
	return &AppConfig{
		Version:  currentVersion,
		Accounts: []Account{},
		Repos:    []RepoConfig{},
		Preferences: Preferences{
			GlobalPause:          false,
			DebounceSec:          30,
			MaxDebounceSec:       120, // 2-minute trailing cap
			BackupsEnabled:       true,
			BackupDir:            filepath.Join(home, ".gitsync", "backups"),
			LogMaxSizeMB:         5,
			LogMaxFiles:          3,
			StartMinimized:       false,
			LaunchOnBoot:         false,
			Theme:                "dark",
			ExcludeLargeFiles:    true,
			LargeFileThresholdMB: 95,
			ShowNotifications:    true,
		},
	}
}

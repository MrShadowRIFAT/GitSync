package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEncryptDecryptPAT(t *testing.T) {
	s := &Store{key: deriveKey()}
	original := "ghp_testtoken1234567890"

	encrypted, err := s.EncryptPAT(original)
	if err != nil {
		t.Fatalf("EncryptPAT failed: %v", err)
	}
	if encrypted == original {
		t.Fatal("encrypted text should differ from original")
	}

	decrypted, err := s.DecryptPAT(encrypted)
	if err != nil {
		t.Fatalf("DecryptPAT failed: %v", err)
	}
	if decrypted != original {
		t.Fatalf("expected %q, got %q", original, decrypted)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Version != currentVersion {
		t.Errorf("expected version %d, got %d", currentVersion, cfg.Version)
	}
	if cfg.Preferences.DebounceSec != 30 {
		t.Errorf("expected debounce 30, got %d", cfg.Preferences.DebounceSec)
	}
	if cfg.Preferences.MaxDebounceSec != 120 {
		t.Errorf("expected max debounce 120, got %d", cfg.Preferences.MaxDebounceSec)
	}
}

func TestMigrate(t *testing.T) {
	cfg := &AppConfig{Version: 0}
	if err := migrate(cfg); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	if cfg.Version != currentVersion {
		t.Errorf("expected version %d after migration, got %d", currentVersion, cfg.Version)
	}
	if cfg.Preferences.Theme != "dark" {
		t.Errorf("expected theme 'dark', got %q", cfg.Preferences.Theme)
	}
	if cfg.Preferences.MaxDebounceSec != 120 {
		t.Errorf("expected max debounce 120, got %d", cfg.Preferences.MaxDebounceSec)
	}
}

func TestStoreCRUDAndAtomicSave(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	store := &Store{
		path:   configPath,
		key:    deriveKey(),
		config: defaultConfig(),
	}

	if err := store.Save(); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Verify file exists on disk
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config file missing: %v", err)
	}

	// Test AddAccount and FindAccount
	acc := Account{
		ID:        "acc-1",
		Label:     "Work",
		Username:  "octocat",
		Email:     "octocat@github.com",
		CreatedAt: time.Now(),
	}
	if err := store.AddAccount(acc); err != nil {
		t.Fatalf("AddAccount failed: %v", err)
	}

	foundAcc := store.FindAccount("acc-1")
	if foundAcc == nil || foundAcc.Username != "octocat" {
		t.Fatalf("expected to find account, got %v", foundAcc)
	}

	foundByUsername := store.FindAccountByUsername("OCTOCAT")
	if foundByUsername == nil || foundByUsername.ID != "acc-1" {
		t.Fatalf("expected to find account by username, got %v", foundByUsername)
	}

	foundAcc.Label = "Updated Label"
	if err := store.UpdateAccount(*foundAcc); err != nil {
		t.Fatalf("UpdateAccount failed: %v", err)
	}
	if store.FindAccount("acc-1").Label != "Updated Label" {
		t.Fatalf("expected updated label, got %s", store.FindAccount("acc-1").Label)
	}

	// Test AddRepo, FindRepo, FindRepoByPath
	repo := RepoConfig{
		ID:         "repo-1",
		LocalPath:  tmpDir,
		RemoteName: "my-repo",
		AccountID:  "acc-1",
		Enabled:    true,
		Branch:     "main",
		CreatedAt:  time.Now(),
	}
	if err := store.AddRepo(repo); err != nil {
		t.Fatalf("AddRepo failed: %v", err)
	}

	foundRepo := store.FindRepo("repo-1")
	if foundRepo == nil || foundRepo.RemoteName != "my-repo" {
		t.Fatalf("expected to find repo, got %v", foundRepo)
	}

	foundByPath := store.FindRepoByPath(tmpDir)
	if foundByPath == nil || foundByPath.ID != "repo-1" {
		t.Fatalf("expected to find repo by path, got %v", foundByPath)
	}

	// Test RemoveRepo
	if err := store.RemoveRepo("repo-1"); err != nil {
		t.Fatalf("RemoveRepo failed: %v", err)
	}
	if store.FindRepo("repo-1") != nil {
		t.Fatal("expected repo to be removed")
	}
}

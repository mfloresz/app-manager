// Package storage manages persistent application state.
package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Status represents the current update status of a repo.
type Status string

const (
	StatusIdle        Status = "idle"
	StatusChecking    Status = "checking"
	StatusDownloading Status = "downloading"
	StatusStopping    Status = "stopping"
	StatusReplacing   Status = "replacing"
	StatusStarting    Status = "starting"
	StatusVerifying   Status = "verifying"
	StatusCompleted   Status = "completed"
	StatusFailed      Status = "failed"
)

// Repository represents a tracked repo/app pair with full state.
type Repository struct {
	ID             string `json:"id"`
	Owner          string `json:"owner"`
	Name           string `json:"name"`
	AppName        string `json:"app_name"`
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	AssetName      string `json:"asset_name,omitempty"`
	PlatformOS     string `json:"platform_os,omitempty"`
	PlatformArch   string `json:"platform_arch,omitempty"`
	PID            int    `json:"pid,omitempty"`
	Status         Status `json:"status"`
	LastCheck      string `json:"last_check,omitempty"`
	LastUpdate     string `json:"last_update,omitempty"`
	Progress       int    `json:"progress,omitempty"`
	Error          string `json:"error,omitempty"`
	Installed      bool   `json:"installed"`
	InstallPath    string `json:"install_path,omitempty"`
	CustomCommand  string `json:"custom_command,omitempty"`
}

// Store manages persistent state.
type Store struct {
	mu       sync.Mutex
	filePath string
	Repos    []Repository `json:"repos"`
}

// NewStore creates or loads a Store from a file.
func NewStore(filePath string) (*Store, error) {
	s := &Store{filePath: filePath}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			s.Repos = []Repository{}
			return nil
		}
		return fmt.Errorf("leyendo %s: %w", s.filePath, err)
	}
	if err := json.Unmarshal(data, &s.Repos); err != nil {
		return fmt.Errorf("parseando %s: %w", s.filePath, err)
	}
	// Ensure all repos have an ID
	for i := range s.Repos {
		if s.Repos[i].ID == "" {
			s.Repos[i].ID = fmt.Sprintf("repo-%d", i)
		}
		if s.Repos[i].Status == "" {
			s.Repos[i].Status = StatusIdle
		}
	}
	return nil
}

// Save persists the current state to disk atomically.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeLocked()
}

// Find returns a pointer to a repo by ID (under lock).
func (s *Store) Find(id string) *Repository {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Repos {
		if s.Repos[i].ID == id {
			return &s.Repos[i]
		}
	}
	return nil
}

// Update applies a mutator function to a repo under lock.
func (s *Store) Update(id string, fn func(r *Repository)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Repos {
		if s.Repos[i].ID == id {
			fn(&s.Repos[i])
			return
		}
	}
}

// List returns a copy of all repos.
func (s *Store) List() []Repository {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]Repository, len(s.Repos))
	copy(cp, s.Repos)
	return cp
}

// Add inserts a new repo and saves.
func (s *Store) Add(repo Repository) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Repos = append(s.Repos, repo)
	return s.writeLocked()
}

// Remove deletes a repo by ID and saves.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.Repos {
		if r.ID == id {
			s.Repos = append(s.Repos[:i], s.Repos[i+1:]...)
			return s.writeLocked()
		}
	}
	return fmt.Errorf("repositorio %s no encontrado", id)
}

// writeLocked marshals and atomically persists s.Repos to disk. The caller
// must hold s.mu; the JSON format is unchanged (top-level array).
func (s *Store) writeLocked() error {
	data, err := json.MarshalIndent(s.Repos, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(s.filePath, data, 0644)
}

// atomicWriteFile writes data to path atomically: the bytes go to a temp
// file in the same directory, are flushed and closed, then the temp file is
// renamed over the target, so an interruption never leaves a partially
// written target file. The temp file is removed on any failure.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".ap-manager-state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// AllRepos returns a direct pointer to the repos slice (caller must hold lock).
// For internal use by non-locking callers who already hold the lock.
func (s *Store) AllRepos() []Repository {
	return s.Repos
}

// Lock/Unlock for external iteration.
func (s *Store) Lock()   { s.mu.Lock() }
func (s *Store) Unlock() { s.mu.Unlock() }

// Helper to format time.
func Now() string {
	return time.Now().Format(time.RFC3339)
}

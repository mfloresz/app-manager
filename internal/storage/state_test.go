package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStoreAddSaveRemoveRoundTrip verifies that Add, Save (after Update) and
// Remove are all visible when a fresh Store is loaded from disk.
func TestStoreAddSaveRemoveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}

	// Add persists immediately.
	if err := s.Add(Repository{ID: "a/b", AppName: "app", Status: StatusIdle}); err != nil {
		t.Fatal(err)
	}
	// Update mutates memory; Save persists it.
	s.Update("a/b", func(r *Repository) {
		r.Status = StatusFailed
		r.Error = "boom"
	})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatalf("reload after add/save: %v", err)
	}
	repos := reloaded.List()
	if len(repos) != 1 {
		t.Fatalf("len(repos) = %d, want 1", len(repos))
	}
	if repos[0].ID != "a/b" || repos[0].Status != StatusFailed || repos[0].Error != "boom" {
		t.Errorf("unexpected reloaded repo: %+v", repos[0])
	}

	// Remove persists.
	if err := reloaded.Remove("a/b"); err != nil {
		t.Fatal(err)
	}
	reloaded2, err := NewStore(path)
	if err != nil {
		t.Fatalf("reload after remove: %v", err)
	}
	if got := len(reloaded2.List()); got != 0 {
		t.Errorf("len(repos) after remove = %d, want 0", got)
	}
}

// TestStoreRepeatedSavesRemainParseable saves many times and verifies the
// file stays a valid, current JSON document.
func TestStoreRepeatedSavesRemainParseable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Add(Repository{ID: "a/b", AppName: "app"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		s.Update("a/b", func(r *Repository) { r.Progress = i })
		if err := s.Save(); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatalf("file not parseable after repeated saves: %v", err)
	}
	repo := reloaded.Find("a/b")
	if repo == nil {
		t.Fatal("repo missing after reload")
	}
	if repo.Progress != 49 {
		t.Errorf("Progress = %d, want 49", repo.Progress)
	}
}

// TestAtomicSaveLeavesNoTempFiles verifies the atomic write does not leave
// temp files behind after successful saves.
func TestAtomicSaveLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if err := s.Add(Repository{ID: string(rune('a' + i)), AppName: "app"}); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		t.Errorf("expected only state.json in dir, got %d entries", len(entries))
	}
}

// TestSaveFailureDoesNotCreatePartialTarget verifies that a failed atomic
// write (unwritable/non-existent directory) neither creates the target nor
// leaves a temp file behind.
func TestSaveFailureDoesNotCreatePartialTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "state.json") // parent missing

	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err == nil {
		t.Fatal("expected Save to fail for a missing parent directory")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("no partial target expected, stat err = %v", err)
	}
	if entries, err := os.ReadDir(filepath.Dir(path)); err == nil && len(entries) != 0 {
		t.Errorf("expected no files in the missing-parent dir, got %d", len(entries))
	}
}

package process

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestSanitizeID verifies that distinct repo IDs always map to distinct,
// filesystem-safe keys, even when their sanitized prefixes would collide.
func TestSanitizeID(t *testing.T) {
	ids := []string{
		"a/yara", "a yara", "a_yara", "a-yara",
		"owner/name with spaces", "x/y", "X/Y", "yara", "",
	}
	seen := map[string]string{}
	for _, id := range ids {
		key := SanitizeID(id)
		if prev, dup := seen[key]; dup {
			t.Errorf("SanitizeID(%q) = %q collides with %q", id, key, prev)
		}
		seen[key] = id
		// Must not contain characters unsafe in file names.
		for _, r := range key {
			if r == '/' || r == '\\' || r == ' ' || r == ':' || r == '*' {
				t.Errorf("SanitizeID(%q) = %q contains unsafe character %q", id, key, r)
			}
		}
	}
	if SanitizeID("a/b") != SanitizeID("a/b") {
		t.Error("SanitizeID must be deterministic")
	}
	if SanitizeID("") != SanitizeID("") {
		t.Error("SanitizeID of an empty ID must be deterministic")
	}
}

// TestManagerPIDFilesPerRepoID verifies two repo IDs with the same AppName
// use distinct PID files that do not interfere with each other.
func TestManagerPIDFilesPerRepoID(t *testing.T) {
	dir := t.TempDir()
	pm := NewManager(dir)

	if err := pm.WritePid("a/yara", pidRecord{PID: 1111, ExecPath: "/a/yara"}); err != nil {
		t.Fatal(err)
	}
	if err := pm.WritePid("b/yara", pidRecord{PID: 2222, ExecPath: "/b/yara"}); err != nil {
		t.Fatal(err)
	}
	if a, b := pm.PidFile("a/yara"), pm.PidFile("b/yara"); a == b {
		t.Fatalf("PID files must differ per repo ID, both %q", a)
	}
	pidA, err := pm.ReadPid("a/yara")
	if err != nil || pidA.PID != 1111 {
		t.Errorf("ReadPid(a/yara) = %+v, %v; want PID 1111, nil", pidA, err)
	}
	pidB, err := pm.ReadPid("b/yara")
	if err != nil || pidB.PID != 2222 {
		t.Errorf("ReadPid(b/yara) = %+v, %v; want PID 2222, nil", pidB, err)
	}
	// Removing one repo's PID must not affect the other.
	pm.RemovePid("a/yara")
	if _, err := pm.ReadPid("a/yara"); err == nil {
		t.Error("a/yara PID file should be gone after RemovePid")
	}
	if pid, err := pm.ReadPid("b/yara"); err != nil || pid.PID != 2222 {
		t.Errorf("b/yara PID file should be intact: %+v, %v", pid, err)
	}
	// PID files must live inside PidDir.
	if got := filepath.Dir(pm.PidFile("b/yara")); got != dir {
		t.Errorf("PID file not in PidDir: %q, want dir %q", got, dir)
	}
}

// TestPIDRecordRoundTrip verifies the structured PID record is written as
// JSON atomically (no temp leftovers) and round-trips through ReadPid.
func TestPIDRecordRoundTrip(t *testing.T) {
	dir := t.TempDir()
	pm := NewManager(dir)
	rec := pidRecord{PID: 4242, ExecPath: "/usr/bin/app", StartTime: "12345"}
	if err := pm.WritePid("a/yara", rec); err != nil {
		t.Fatal(err)
	}
	got, err := pm.ReadPid("a/yara")
	if err != nil {
		t.Fatal(err)
	}
	if got != rec {
		t.Errorf("ReadPid = %+v, want %+v", got, rec)
	}
	// Atomic write: only the PID file remains in PidDir.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(pm.PidFile("a/yara")) {
		t.Errorf("expected only the PID file in PidDir, got %v", entries)
	}
	// The file content is the JSON record.
	data, err := os.ReadFile(pm.PidFile("a/yara"))
	if err != nil {
		t.Fatal(err)
	}
	var back pidRecord
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("PID file is not valid JSON: %v", err)
	}
	if back != rec {
		t.Errorf("PID file JSON = %+v, want %+v", back, rec)
	}
}

// TestPIDRecordLegacyRejected verifies a legacy plain-integer PID file fails
// closed: it is never considered running and never used to send a signal. The
// legacy file points at the test's own PID, so a buggy implementation that
// signaled it would kill the test suite.
func TestPIDRecordLegacyRejected(t *testing.T) {
	dir := t.TempDir()
	pm := NewManager(dir)
	if err := os.WriteFile(pm.PidFile("a/yara"), []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		t.Fatal(err)
	}
	if pm.IsRunning("a/yara") {
		t.Error("legacy PID record must not be considered running")
	}
	if _, err := pm.Stop("a/yara"); err == nil {
		t.Error("Stop must fail closed on a legacy PID record")
	}
	// The test process must still be alive: no signal was sent.
	if !ProcessExists(os.Getpid()) {
		t.Fatal("test process was signaled — legacy record must never be used to signal")
	}
}

// TestPIDRecordRejectsPIDOnlyJSON verifies that a structured JSON record with
// only a PID (no canonical executable path) fails closed: it is never
// considered running and never used to send a signal. The PID is the test's
// own, so signaling it would kill the test suite.
func TestPIDRecordRejectsPIDOnlyJSON(t *testing.T) {
	dir := t.TempDir()
	pm := NewManager(dir)
	data := []byte(`{"pid":` + strconv.Itoa(os.Getpid()) + `}`)
	if err := os.WriteFile(pm.PidFile("a/yara"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if pm.IsRunning("a/yara") {
		t.Error("PID-only JSON must not be considered running")
	}
	if _, err := pm.Stop("a/yara"); err == nil {
		t.Error("Stop must fail closed on PID-only JSON")
	}
	if !ProcessExists(os.Getpid()) {
		t.Fatal("test process was signaled — PID-only JSON must never be used to signal")
	}
}

// TestPidFileIsFilesystemSafe verifies the derived PID file name is a plain
// local file name (no separators, no traversal) even for hostile repo IDs.
func TestPidFileIsFilesystemSafe(t *testing.T) {
	dir := t.TempDir()
	pm := NewManager(dir)

	name := filepath.Base(pm.PidFile("owner/name with spaces"))
	if !filepath.IsLocal(name) {
		t.Errorf("PID file name is not a local path: %q", name)
	}
	// The full path must stay inside the PID dir.
	full := pm.PidFile("../evil/name")
	if filepath.Dir(full) != dir {
		t.Errorf("PID path escaped PidDir: %q", full)
	}
}

// TestInstallBinarySuccess verifies a complete install: destination content
// matches the source, the destination is executable (0755), no temp files
// are left behind, and the caller-owned source is untouched.
func TestInstallBinarySuccess(t *testing.T) {
	destDir := t.TempDir()
	src := filepath.Join(t.TempDir(), "src.bin")
	content := []byte("#!/bin/sh\necho installed\n")
	if err := os.WriteFile(src, content, 0644); err != nil {
		t.Fatal(err)
	}

	destPath, err := InstallBinary(src, destDir, "app")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(destDir, "app"); destPath != want {
		t.Errorf("destPath = %q, want %q", destPath, want)
	}
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, content) {
		t.Errorf("installed content = %q, want %q", data, content)
	}
	fi, err := os.Stat(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0111 == 0 {
		t.Errorf("installed binary is not executable: %v", fi.Mode())
	}
	// Only the installed binary may remain in the destination directory.
	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "app" {
		t.Errorf("expected only 'app' in dest dir, got %v", entries)
	}
	// The caller-owned source must be preserved.
	srcData, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(srcData, content) {
		t.Errorf("source was modified: %q", srcData)
	}
}

// TestInstallBinaryReplacesExisting verifies an existing destination is
// replaced by the new complete content (never a partial file).
func TestInstallBinaryReplacesExisting(t *testing.T) {
	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "app")
	if err := os.WriteFile(destPath, []byte("OLD"), 0644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "src.bin")
	content := []byte("NEW-COMPLETE")
	if err := os.WriteFile(src, content, 0644); err != nil {
		t.Fatal(err)
	}

	got, err := InstallBinary(src, destDir, "app")
	if err != nil {
		t.Fatal(err)
	}
	if got != destPath {
		t.Errorf("destPath = %q, want %q", got, destPath)
	}
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "NEW-COMPLETE" {
		t.Errorf("destination = %q, want %q (partial write?)", data, "NEW-COMPLETE")
	}
	fi, err := os.Stat(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0111 == 0 {
		t.Errorf("replaced binary is not executable: %v", fi.Mode())
	}
}

// TestInstallBinaryFailureCleansUp verifies a failed install (missing
// source) leaves no destination and no leftover temp files.
func TestInstallBinaryFailureCleansUp(t *testing.T) {
	destDir := t.TempDir()
	_, err := InstallBinary(filepath.Join(t.TempDir(), "missing-src"), destDir, "app")
	if err == nil {
		t.Fatal("expected an error for a missing source")
	}
	if _, statErr := os.Stat(filepath.Join(destDir, "app")); !os.IsNotExist(statErr) {
		t.Errorf("destination should not exist after failure, stat err = %v", statErr)
	}
	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("leftover files after failed install: %v", entries)
	}
}

// TestInstallBinaryCopyFailureLeavesNoPartialDest exercises the mid-copy
// cleanup path: a directory as source opens on POSIX but fails during the
// copy, so no partial destination may remain.
func TestInstallBinaryCopyFailureLeavesNoPartialDest(t *testing.T) {
	destDir := t.TempDir()
	_, err := InstallBinary(t.TempDir(), destDir, "app") // a directory as source
	if err == nil {
		t.Fatal("expected an error copying a directory source")
	}
	if _, statErr := os.Stat(filepath.Join(destDir, "app")); !os.IsNotExist(statErr) {
		t.Errorf("destination should not exist after failed copy, stat err = %v", statErr)
	}
	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("leftover temp files after failed copy: %v", entries)
	}
}

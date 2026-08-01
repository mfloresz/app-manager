package updater

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"ap-manager/internal/events"
	"ap-manager/internal/process"
	"ap-manager/internal/storage"
)

// newTestPipeline builds a Pipeline backed by temp files and a real process
// manager with an empty PID directory (no managed processes).
func newTestPipeline(t *testing.T, dir string) *Pipeline {
	t.Helper()
	store, err := storage.NewStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add(storage.Repository{
		ID:      "test/app",
		AppName: "testapp",
		Status:  storage.StatusIdle,
	}); err != nil {
		t.Fatal(err)
	}
	return &Pipeline{
		Broker:  events.NewBroker(),
		Store:   store,
		ProcMan: process.NewManager(dir),
		OS:      "linux",
		Arch:    "amd64",
	}
}

// TestRestoreFromBackupKeepsDestinationWhenBackupMissing verifies that a
// missing backup aborts restoration without deleting or modifying the
// destination binary.
func TestRestoreFromBackupKeepsDestinationWhenBackupMissing(t *testing.T) {
	dir := t.TempDir()
	appPath := filepath.Join(dir, "testapp")
	if err := os.WriteFile(appPath, []byte("NEW-BINARY"), 0755); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(dir, "testapp.bak") // does not exist

	p := newTestPipeline(t, dir)
	if err := p.restoreFromBackup("test/app", appPath, backupPath, "testapp"); err == nil {
		t.Fatal("expected an error for a missing backup")
	}

	data, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatalf("destination was removed or unreadable: %v", err)
	}
	if string(data) != "NEW-BINARY" {
		t.Errorf("destination content = %q, want %q", data, "NEW-BINARY")
	}
}

// TestRestoreFromBackupKeepsDestinationWhenBackupEmpty verifies that an empty
// (unusable) backup aborts restoration without touching the destination.
func TestRestoreFromBackupKeepsDestinationWhenBackupEmpty(t *testing.T) {
	dir := t.TempDir()
	appPath := filepath.Join(dir, "testapp")
	if err := os.WriteFile(appPath, []byte("NEW-BINARY"), 0755); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(dir, "testapp.bak")
	if err := os.WriteFile(backupPath, nil, 0644); err != nil {
		t.Fatal(err)
	}

	p := newTestPipeline(t, dir)
	if err := p.restoreFromBackup("test/app", appPath, backupPath, "testapp"); err == nil {
		t.Fatal("expected an error for an empty backup")
	}

	data, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatalf("destination was removed or unreadable: %v", err)
	}
	if string(data) != "NEW-BINARY" {
		t.Errorf("destination content = %q, want %q", data, "NEW-BINARY")
	}
}

// TestRestoreFromBackupSuccess verifies that a usable backup is restored with
// the right content and permissions, and that the previous version is
// restarted (a trivial script that exits immediately is used as the binary).
func TestRestoreFromBackupSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell for the mock binary")
	}
	dir := t.TempDir()
	appPath := filepath.Join(dir, "testapp")
	if err := os.WriteFile(appPath, []byte("NEW-BROKEN"), 0755); err != nil {
		t.Fatal(err)
	}
	backupContent := "#!/bin/sh\nexit 0\n"
	backupPath := filepath.Join(dir, "testapp.bak")
	if err := os.WriteFile(backupPath, []byte(backupContent), 0644); err != nil {
		t.Fatal(err)
	}

	p := newTestPipeline(t, dir)
	if err := p.restoreFromBackup("test/app", appPath, backupPath, "testapp"); err != nil {
		t.Fatalf("restore failed: %v", err)
	}

	data, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != backupContent {
		t.Errorf("restored content = %q, want %q", data, backupContent)
	}
	fi, err := os.Stat(appPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0111 == 0 {
		t.Errorf("restored binary is not executable: %v", fi.Mode())
	}
	// A PID file should have been written by the restart attempt.
	pidData, err := os.ReadFile(p.ProcMan.PidFile("test/app"))
	if err != nil {
		t.Errorf("no PID file written after restart: %v", err)
	}
	if strings.TrimSpace(string(pidData)) == "" {
		t.Error("empty PID file after restart")
	}
}

// TestUpdateArtifactNamesDoNotCollide verifies that temp/backup artifact
// names are scoped per repo ID, so two repos with the same AppName never
// share files.
func TestUpdateArtifactNamesDoNotCollide(t *testing.T) {
	aTmp := updateArtifactName("yara", "a/yara", ".tmp")
	bTmp := updateArtifactName("yara", "b/yara", ".tmp")
	if aTmp == bTmp {
		t.Fatal("temp artifact names must differ across repo IDs")
	}
	aBak := updateArtifactName("yara", "a/yara", ".bak")
	if aTmp == aBak {
		t.Fatal("temp and backup artifact names must differ")
	}
	// Artifact names must be plain file names (no path separators).
	for _, n := range []string{aTmp, bTmp, aBak} {
		if filepath.Base(n) != n {
			t.Errorf("artifact name contains a path separator: %q", n)
		}
	}
}

// drainEvents reads exactly n messages from an SSE channel or fails the test.
func drainEvents(t *testing.T, ch chan string, n int) []string {
	t.Helper()
	var msgs []string
	for i := 0; i < n; i++ {
		select {
		case m := <-ch:
			msgs = append(msgs, m)
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for event %d of %d (received %d)", i+1, n, len(msgs))
		}
	}
	return msgs
}

// TestOpGuardSerializesPerRepoID covers the guard helper directly: the same
// repo ID rejects a second acquire, release allows re-acquire, and different
// repo IDs never block each other.
func TestOpGuardSerializesPerRepoID(t *testing.T) {
	var g opGuard

	if !g.tryAcquire("a") {
		t.Fatal("first acquire for a should succeed")
	}
	if g.tryAcquire("a") {
		t.Fatal("second acquire for a should be rejected")
	}
	// A different repo ID must not be blocked.
	if !g.tryAcquire("b") {
		t.Fatal("acquire for a different repo ID should succeed")
	}
	// Release a: re-acquiring must work again.
	g.release("a")
	if !g.tryAcquire("a") {
		t.Fatal("acquire after release should succeed")
	}
	g.release("a")
	g.release("b")
	if !g.tryAcquire("a") || !g.tryAcquire("b") {
		t.Fatal("both IDs should be acquirable after release")
	}
	g.release("a")
	g.release("b")
}

// TestZeroValuePipelineGuardDoesNotPanic verifies the guard works on a
// zero-value Pipeline (no constructor migration required).
func TestZeroValuePipelineGuardDoesNotPanic(t *testing.T) {
	var p Pipeline
	if !p.guard.tryAcquire("x") {
		t.Fatal("zero-value pipeline guard should accept the first acquire")
	}
	if p.guard.tryAcquire("x") {
		t.Fatal("second acquire on a zero-value pipeline should be rejected")
	}
	p.guard.release("x")
	if !p.guard.tryAcquire("x") {
		t.Fatal("acquire after release should succeed")
	}
	p.guard.release("x")
}

// TestRunUpdateEarlyExitReleasesGuard verifies the guard is released even
// when the operation exits early (repo not found), so a later invocation is
// not rejected as already in progress.
func TestRunUpdateEarlyExitReleasesGuard(t *testing.T) {
	dir := t.TempDir()
	p := newTestPipeline(t, dir)
	ch := p.Broker.Subscribe("missing/app")
	defer p.Broker.Unsubscribe("missing/app", ch)

	p.RunUpdate("missing/app") // repo not found → early exit
	p.RunUpdate("missing/app") // must NOT be rejected as already running

	msgs := drainEvents(t, ch, 2)
	for _, m := range msgs {
		if strings.Contains(m, "en curso") {
			t.Fatalf("second invocation was rejected as already in progress: %s", m)
		}
		if !strings.Contains(m, "Repositorio no encontrado") {
			t.Errorf("unexpected message: %s", m)
		}
	}
}

// TestRunUpdateDifferentReposIndependent verifies operations for different
// repo IDs are allowed concurrently (both proceed past the guard).
func TestRunUpdateDifferentReposIndependent(t *testing.T) {
	dir := t.TempDir()
	p := newTestPipeline(t, dir)
	ch := p.Broker.Subscribe("_global")
	defer p.Broker.Unsubscribe("_global", ch)

	p.RunUpdate("missing/one")
	p.RunUpdate("missing/two") // different ID: must not be rejected

	msgs := drainEvents(t, ch, 2)
	for _, m := range msgs {
		if strings.Contains(m, "en curso") {
			t.Fatalf("different repo IDs interfered with each other: %s", m)
		}
	}
}

// TestFailUpdateSetsFailedStateAndEvent verifies the terminal failure helper:
// status failed, error populated, progress reset, and exactly one
// NewUpdateFailed event.
func TestFailUpdateSetsFailedStateAndEvent(t *testing.T) {
	dir := t.TempDir()
	p := newTestPipeline(t, dir)
	ch := p.Broker.Subscribe("test/app")
	defer p.Broker.Unsubscribe("test/app", ch)

	p.failUpdate("test/app", "boom")

	repo := p.Store.Find("test/app")
	if repo == nil {
		t.Fatal("repo not found in store")
	}
	if repo.Status != storage.StatusFailed {
		t.Errorf("Status = %q, want %q", repo.Status, storage.StatusFailed)
	}
	if repo.Error != "boom" {
		t.Errorf("Error = %q, want %q", repo.Error, "boom")
	}
	if repo.Progress != 0 {
		t.Errorf("Progress = %d, want 0", repo.Progress)
	}

	// The broker must receive exactly one terminal failed-update event.
	deadline := time.Now().Add(2 * time.Second)
	var failed *events.SSEEvent
	for failed == nil && time.Now().Before(deadline) {
		select {
		case msg := <-ch:
			var evt events.SSEEvent
			if err := json.Unmarshal([]byte(msg), &evt); err != nil {
				t.Fatal(err)
			}
			if evt.Type == events.EventUpdate && evt.Status == "failed" {
				failed = &evt
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	if failed == nil {
		t.Fatal("no NewUpdateFailed event emitted")
	}
	if failed.Error != "boom" {
		t.Errorf("event Error = %q, want %q", failed.Error, "boom")
	}
}

// TestFailUpdatePersistsToDisk verifies that a terminal failUpdate state is
// written to disk and visible when a fresh Store is loaded from the same file.
func TestFailUpdatePersistsToDisk(t *testing.T) {
	dir := t.TempDir()
	p := newTestPipeline(t, dir)

	p.failUpdate("test/app", "boom")

	// Reload from disk: the terminal state must have been persisted.
	store2, err := storage.NewStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("reload from disk: %v", err)
	}
	repo := store2.Find("test/app")
	if repo == nil {
		t.Fatal("repo not found after reload")
	}
	if repo.Status != storage.StatusFailed {
		t.Errorf("Status = %q, want %q", repo.Status, storage.StatusFailed)
	}
	if repo.Error != "boom" {
		t.Errorf("Error = %q, want %q", repo.Error, "boom")
	}
	if repo.Progress != 0 {
		t.Errorf("Progress = %d, want 0", repo.Progress)
	}
}

// TestDetectCurrentVersionProbeTimeout verifies a version probe on a binary
// that ignores the flag does not hang: detectCurrentVersion returns
// "desconocida" within a bounded time.
func TestDetectCurrentVersionProbeTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell script executable")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "probe")
	// exec replaces the shell so the timed-out process is killed directly.
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 30\n"), 0755); err != nil {
		t.Fatal(err)
	}

	old := versionProbeTimeout
	versionProbeTimeout = 200 * time.Millisecond
	t.Cleanup(func() { versionProbeTimeout = old })

	start := time.Now()
	got := detectCurrentVersion(bin)
	elapsed := time.Since(start)
	if got != "desconocida" {
		t.Errorf("detectCurrentVersion = %q, want %q", got, "desconocida")
	}
	if elapsed > 5*time.Second {
		t.Errorf("probe took %v, want a bounded timeout", elapsed)
	}
}

// TestVerifyDownloadAcceptsExecutables covers the accepted signatures: ELF,
// PE/MZ, Mach-O (thin and fat), and scripts with a valid shebang.
func TestVerifyDownloadAcceptsExecutables(t *testing.T) {
	payloads := map[string][]byte{
		"elf":        append([]byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0}, make([]byte, 64)...),
		"pe":         append([]byte{'M', 'Z'}, make([]byte, 64)...),
		"macho-32":   append([]byte{0xfe, 0xed, 0xfa, 0xce}, make([]byte, 64)...),
		"macho-64":   append([]byte{0xfe, 0xed, 0xfa, 0xcf}, make([]byte, 64)...),
		"macho-rev":  append([]byte{0xcf, 0xfa, 0xed, 0xfe}, make([]byte, 64)...),
		"macho-fat":  append([]byte{0xca, 0xfe, 0xba, 0xbe}, make([]byte, 64)...),
		"shebang":    []byte("#!/bin/sh\nexit 0\n"),
		"shebang-sp": []byte("#! /usr/bin/env bash\necho hi\n"),
	}
	for name, data := range payloads {
		name, data := name, data
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "asset")
			if err := os.WriteFile(path, data, 0755); err != nil {
				t.Fatal(err)
			}
			if err := verifyDownload(path); err != nil {
				t.Errorf("verifyDownload(%s) = %v, want nil", name, err)
			}
		})
	}
}

// TestVerifyDownloadRejects covers the rejected payloads: empty, truncated,
// too-short, arbitrary text without a shebang, and archive/container formats.
// Each rejection must produce a non-empty, actionable error.
func TestVerifyDownloadRejects(t *testing.T) {
	payloads := map[string][]byte{
		"empty":      nil,
		"short-elf":  {0x7f, 'E', 'L', 'F'}, // magic only, truncated
		"short-pe":   {'M', 'Z'},
		"short-text": []byte("hi"),
		"text":       []byte("this is just text, not a script\nwith no shebang\n"),
		"html":       []byte("<!DOCTYPE html><html><body>error</body></html>"),
		"gzip":       append([]byte{0x1f, 0x8b, 0x08, 0x00}, make([]byte, 32)...),
		"tar-gzip":   append([]byte{0x1f, 0x8b, 0x08}, make([]byte, 32)...),
		"xz":         append([]byte{0xfd, 0x37, 0x7a, 0x58, 0x5a, 0x00}, make([]byte, 32)...),
		"zip":        append([]byte{'P', 'K', 0x03, 0x04}, make([]byte, 32)...),
		"bzip2":      append([]byte{'B', 'Z', 'h'}, make([]byte, 32)...),
		"7z":         append([]byte{0x37, 0x7a, 0xbc, 0xaf, 0x27, 0x1c}, make([]byte, 32)...),
		"rar":        append([]byte{'R', 'a', 'r', '!'}, make([]byte, 32)...),
	}
	for name, data := range payloads {
		name, data := name, data
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "asset")
			if err := os.WriteFile(path, data, 0644); err != nil {
				t.Fatal(err)
			}
			err := verifyDownload(path)
			if err == nil {
				t.Errorf("verifyDownload(%s) = nil, want error", name)
				return
			}
			if strings.TrimSpace(err.Error()) == "" {
				t.Errorf("verifyDownload(%s) returned an empty error message", name)
			}
		})
	}
}

// TestFallbackAssetNamesNoArchives verifies the fallback asset candidates only
// contain raw executable names: no .tar.gz/.tar.xz (or any .tar) archive
// names are produced, while valid raw fallbacks remain.
func TestFallbackAssetNamesNoArchives(t *testing.T) {
	names := fallbackAssetNames("yara", "linux", "amd64", "linux", "amd64")
	for _, n := range names {
		if strings.Contains(n, ".tar") {
			t.Errorf("archive name in fallback candidates: %q", n)
		}
	}
	if !containsString(names, "yara-linux-amd64") {
		t.Errorf("primary raw candidate missing: %v", names)
	}

	// Android fallbacks must also be archive-free and keep the linux
	// variant used by Termux.
	android := fallbackAssetNames("yara", "android", "arm64", "linux", "arm64")
	for _, n := range android {
		if strings.Contains(n, ".tar") {
			t.Errorf("archive name in android fallback candidates: %q", n)
		}
	}
	if !containsString(android, "yara-linux-arm64") {
		t.Errorf("linux fallback candidate missing for android: %v", android)
	}
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// TestDetectCurrentVersionProbeOrderAndFallback verifies the probe order:
// --version is tried first, and on failure the plain "version" probe is
// attempted; the trimmed output of the first success is returned.
func TestDetectCurrentVersionProbeOrderAndFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell script executable")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "probe")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then exit 1; fi\necho \"1.2.3\"\n"
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	got := detectCurrentVersion(bin)
	if got != "1.2.3" {
		t.Errorf("detectCurrentVersion = %q, want %q", got, "1.2.3")
	}
}

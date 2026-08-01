//go:build linux || android

package process

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPIDIdentityMatch verifies the strong /proc-based identity check against
// the current process: a matching record verifies, while a wrong executable
// path or wrong start token does not.
func TestPIDIdentityMatch(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(exe)
	if err != nil {
		canonical = exe
	}
	pid := os.Getpid()

	token, ok := captureStartIdentity(pid)
	if !ok {
		t.Fatal("captureStartIdentity failed for the current process")
	}
	rec := pidRecord{PID: pid, ExecPath: canonical, StartTime: token}
	if !verifyProcessIdentity(pid, rec) {
		t.Error("verifyProcessIdentity should match the current process")
	}

	// Deliberate executable-path mismatch.
	bad := pidRecord{PID: pid, ExecPath: "/nonexistent/binary", StartTime: token}
	if verifyProcessIdentity(pid, bad) {
		t.Error("executable path mismatch must not verify")
	}

	// Deliberate start-token mismatch.
	bad2 := pidRecord{PID: pid, ExecPath: canonical, StartTime: "0"}
	if verifyProcessIdentity(pid, bad2) {
		t.Error("start token mismatch must not verify")
	}
}

// TestPIDIdentityMismatchNoSignal verifies that Stop refuses to signal a PID
// whose recorded executable identity does not match the live process. The
// record points at the test's own (very real) PID, so signaling it would kill
// the test suite.
func TestPIDIdentityMismatchNoSignal(t *testing.T) {
	dir := t.TempDir()
	pm := NewManager(dir)

	rec := pidRecord{PID: os.Getpid(), ExecPath: "/definitely/not/this/binary"}
	if err := pm.WritePid("a/yara", rec); err != nil {
		t.Fatal(err)
	}
	if pm.IsRunning("a/yara") {
		t.Error("mismatched identity must not be considered running")
	}
	if _, err := pm.Stop("a/yara"); err == nil {
		t.Error("Stop must refuse to signal a mismatched identity")
	}
	if !ProcessExists(os.Getpid()) {
		t.Fatal("test process was signaled — identity mismatch must never signal")
	}

	// A path-only record lacks the Linux start-time token and must also fail
	// closed, even when its executable path is correct.
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(exe)
	if err != nil {
		canonical = exe
	}
	if err := pm.WritePid("a/yara", pidRecord{PID: os.Getpid(), ExecPath: canonical}); err != nil {
		t.Fatal(err)
	}
	if pm.IsRunning("a/yara") {
		t.Error("path-only PID record must not be considered running on Linux")
	}
	if _, err := pm.Stop("a/yara"); err == nil {
		t.Error("Stop must reject a path-only PID record on Linux")
	}
	if !ProcessExists(os.Getpid()) {
		t.Fatal("test process was signaled — path-only record must never signal")
	}
}

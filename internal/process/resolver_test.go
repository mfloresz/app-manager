package process

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeExec creates a file with the given mode, creating parent dirs.
func writeExec(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// TestResolveAppBinaryPrefersConfiguredPath verifies that a valid configured
// install path wins even when the fallback search would find a different
// same-named executable.
func TestResolveAppBinaryPrefersConfiguredPath(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	writeExec(t, filepath.Join(dirA, "app"), "A", 0755)
	writeExec(t, filepath.Join(dirB, "bin", "app"), "B", 0755)
	t.Setenv("HOME", dirB) // fallback would find dirB/bin/app

	got, err := ResolveAppBinary("app", dirA)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dirA, "app"); got != want {
		t.Errorf("ResolveAppBinary = %q, want %q", got, want)
	}

	// Prove the fallback would have found a different binary: the resolver
	// must not silently pick it.
	fallback, err := FindAppBinary("app")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dirB, "bin", "app"); fallback != want {
		t.Errorf("FindAppBinary fallback = %q, want %q (test setup)", fallback, want)
	}
}

// TestResolveAppBinaryRejectsInvalidConfiguredPath verifies that an invalid
// configured path is an error and never falls back to the PATH search.
func TestResolveAppBinaryRejectsInvalidConfiguredPath(t *testing.T) {
	dirA := t.TempDir() // no "app" here
	dirB := t.TempDir()
	writeExec(t, filepath.Join(dirB, "bin", "app"), "B", 0755)
	t.Setenv("HOME", dirB) // fallback would find dirB/bin/app

	if _, err := ResolveAppBinary("app", dirA); err == nil {
		t.Fatal("expected an error for a missing configured binary (no fallback)")
	}
}

// TestResolveAppBinaryEmptyPathFallsBack verifies that an empty install path
// keeps the existing FindAppBinary behavior unchanged.
func TestResolveAppBinaryEmptyPathFallsBack(t *testing.T) {
	dirB := t.TempDir()
	writeExec(t, filepath.Join(dirB, "bin", "app"), "B", 0755)
	t.Setenv("HOME", dirB)

	got, err := ResolveAppBinary("app", "")
	if err != nil {
		t.Fatal(err)
	}
	want, err := FindAppBinary("app")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("ResolveAppBinary(empty) = %q, want %q", got, want)
	}
}

// TestResolveAppBinaryRejectsNonExecutable verifies that a non-executable
// file in the configured path is rejected.
func TestResolveAppBinaryRejectsNonExecutable(t *testing.T) {
	dirA := t.TempDir()
	writeExec(t, filepath.Join(dirA, "app"), "A", 0644)
	if _, err := ResolveAppBinary("app", dirA); err == nil {
		t.Fatal("expected an error for a non-executable configured binary")
	}
}

// TestResolveAppBinarySymlinkBehavior verifies that a symlink resolving to an
// executable regular file is accepted, the configured (symlink) path is
// preserved for start operations, and that replacement callers resolve it
// with EvalSymlinks. Broken or non-executable targets are rejected.
func TestResolveAppBinarySymlinkBehavior(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require privileges on Windows")
	}
	dirA := t.TempDir()
	dirT := t.TempDir()
	writeExec(t, filepath.Join(dirT, "app"), "TARGET", 0755)
	if err := os.Symlink(filepath.Join(dirT, "app"), filepath.Join(dirA, "app")); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveAppBinary("app", dirA)
	if err != nil {
		t.Fatal(err)
	}
	// The configured (symlink) path is preserved for start operations.
	if want := filepath.Join(dirA, "app"); got != want {
		t.Errorf("ResolveAppBinary = %q, want %q", got, want)
	}
	// The update path resolves the symlink to the real file.
	resolved, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatal(err)
	}
	wantResolved, err := filepath.EvalSymlinks(filepath.Join(dirT, "app"))
	if err != nil {
		t.Fatal(err)
	}
	if resolved != wantResolved {
		t.Errorf("EvalSymlinks(%q) = %q, want %q", got, resolved, wantResolved)
	}

	// A broken symlink must be rejected.
	dirC := t.TempDir()
	if err := os.Symlink(filepath.Join(dirC, "missing"), filepath.Join(dirC, "app")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveAppBinary("app", dirC); err == nil {
		t.Fatal("expected an error for a broken symlink")
	}

	// A symlink to a non-executable target must be rejected.
	dirD := t.TempDir()
	writeExec(t, filepath.Join(dirD, "app"), "NE", 0644)
	dirE := t.TempDir()
	if err := os.Symlink(filepath.Join(dirD, "app"), filepath.Join(dirE, "app")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveAppBinary("app", dirE); err == nil {
		t.Fatal("expected an error for a symlink to a non-executable target")
	}
}

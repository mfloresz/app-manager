package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildBinary compiles the real entry point (this package, ./cmd/ap-manager)
// and returns the path to the resulting executable.
func buildBinary(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "ap-manager")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build . failed: %v\n%s", err, out)
	}
	return bin
}

// runCLI runs the built binary and fails the test if it does not terminate
// within the bound, proving the command is non-blocking.
func runCLI(t *testing.T, bin string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestCLI builds the real entry point and verifies that --version and --help
// terminate with status 0 (and never start the HTTP server).
func TestCLI(t *testing.T) {
	bin := buildBinary(t)

	t.Run("version", func(t *testing.T) {
		out, err := runCLI(t, bin, "--version")
		if err != nil {
			t.Fatalf("ap-manager --version failed: %v\n%s", err, out)
		}
		if got := strings.TrimSpace(out); got != Version {
			t.Fatalf("--version printed %q, want %q", got, Version)
		}
	})

	t.Run("help", func(t *testing.T) {
		out, err := runCLI(t, bin, "--help")
		if err != nil {
			t.Fatalf("ap-manager --help failed: %v\n%s", err, out)
		}
		for _, want := range []string{"--version", "--help"} {
			if !strings.Contains(out, want) {
				t.Fatalf("--help output missing %q:\n%s", want, out)
			}
		}
	})
}

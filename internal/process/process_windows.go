//go:build windows

package process

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
)

func processExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

func killProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

func killProcessForce(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

func startProcess(path string, args ...string) (int, error) {
	cmd := exec.Command(path, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("exec: %w", err)
	}
	return cmd.Process.Pid, nil
}

func startProcessWithOutput(path string, stdout, stderr io.Writer, args ...string) (int, error) {
	cmd := exec.Command(path, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("exec: %w", err)
	}
	return cmd.Process.Pid, nil
}

func lookPath(name string) (string, error) {
	return exec.LookPath(name)
}

// captureStartIdentity: no strong process-start token is available via the
// standard library on Windows; the PID record keeps only the executable path.
func captureStartIdentity(pid int) (string, bool) {
	return "", false
}

// verifyProcessIdentity: Windows has no /proc and the standard library offers
// no way to read another process's executable path or start token without
// golang.org/x/sys, so verification is best-effort and limited to process
// existence (already checked by the caller). This is documented weaker than
// the Linux/Android path: PID reuse is not detectable on this platform.
func verifyProcessIdentity(pid int, rec pidRecord) bool {
	return processExists(pid)
}

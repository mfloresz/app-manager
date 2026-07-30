//go:build linux && !android

package process

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func processExists(pid int) bool {
	// Check /proc/{pid}/status directly — more reliable than Signal(0)
	// because Signal(0) can return true for zombie processes.
	statusPath := fmt.Sprintf("/proc/%d/status", pid)
	data, err := os.ReadFile(statusPath)
	if err != nil {
		return false
	}
	// Quick check: skip zombies
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "State:") {
			// A zombie has state Z — treat as non-existent
			return !strings.Contains(line, "Z")
		}
	}
	return true
}

func killProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("SIGTERM: %w", err)
	}
	return nil
}

func killProcessForce(pid int) error {
	// Send SIGKILL directly to the process — do NOT use process group
	// (-pid) because:
	//   1. Setsid changes PGID in ways that can cause the group kill to fail
	//   2. If the process became a zombie after SIGTERM, group kill returns
	//      ESRCH (ignored by old code), and the subsequent individual kill
	//      also returns ESRCH, giving a misleading error
	//   3. Sending to the process alone is sufficient
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		return fmt.Errorf("SIGKILL(%d): %w", pid, err)
	}
	return nil
}

func startProcess(path string, args ...string) (int, error) {
	cmd := exec.Command(path, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
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
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("exec: %w", err)
	}
	return cmd.Process.Pid, nil
}

func lookPath(name string) (string, error) {
	return exec.LookPath(name)
}

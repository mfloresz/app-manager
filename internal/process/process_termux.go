//go:build android

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
	// because Signal(0) can return true for zombie processes on Android/Termux.
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
	// Use syscall.Kill directly instead of os.FindProcess+Signal,
	// because os.FindProcess doesn't validate existence on Linux/Android.
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("SIGTERM(%d): %w", pid, err)
	}
	return nil
}

func killProcessForce(pid int) error {
	// Send SIGKILL directly via syscall.
	// Do NOT use process group kill (-pid) because:
	//   1. Setsid changes PGID in ways that can cause the group kill to fail
	//   2. If the process became a zombie after SIGTERM, group kill returns ESRCH
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

// captureStartIdentity returns the /proc/<pid>/stat starttime (field 22) as
// the process-start identity token for a freshly started process.
func captureStartIdentity(pid int) (string, bool) {
	return procStartTime(pid)
}

// verifyProcessIdentity compares the live process executable and start token
// with the recorded identity (strong /proc-based check). The recorded start
// token is required: a record without it (e.g. path-only JSON) never matches.
// A zombie, missing or mismatched process never matches either.
func verifyProcessIdentity(pid int, rec pidRecord) bool {
	if rec.ExecPath == "" || rec.StartTime == "" {
		return false
	}
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return false
	}
	if exe != rec.ExecPath {
		return false
	}
	st, ok := procStartTime(pid)
	if !ok || st != rec.StartTime {
		return false
	}
	return true
}

// procStartTime reads field 22 (starttime) from /proc/<pid>/stat, parsing the
// "(comm)" field safely by taking everything after the last ')'. It returns
// ok=false when the process does not exist or the field is missing.
func procStartTime(pid int) (string, bool) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", false
	}
	s := string(data)
	idx := strings.LastIndexByte(s, ')')
	if idx < 0 || idx+1 >= len(s) {
		return "", false
	}
	fields := strings.Fields(s[idx+1:])
	// fields[0] is state (field 3); starttime is field 22 -> index 19.
	if len(fields) < 20 {
		return "", false
	}
	return fields[19], true
}

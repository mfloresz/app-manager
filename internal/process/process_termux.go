//go:build android

package process

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func processLaunchPath(path string) string {
	if wrapper := termuxChrootPath(); wrapper != "" {
		return canonicalExecPath(wrapper)
	}
	return canonicalExecPath(path)
}

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

// termuxAppDir returns a writable working directory for child processes on
// Android/Termux. Relative application data (for example PocketBase's
// pb_data/data.db) should be created from the user's Termux home, not from the
// service cwd, which may be "/" when ap-manager runs in the background.
func termuxAppDir() string {
	for _, dir := range []string{os.Getenv("HOME"), os.Getenv("PREFIX")} {
		dir = strings.TrimRight(dir, "/")
		if dir == "" {
			continue
		}
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return dir
		}
	}
	return ""
}

// termuxChrootPath returns the trusted termux-chroot wrapper when it is
// installed. Direct execution of foreign Linux binaries on Android can be
// killed by the app's syscall filter (notably statx); termux-chroot uses
// proot to provide the syscall/path compatibility layer that makes those
// binaries run correctly. No shell is used when invoking the wrapper.
func termuxChrootPath() string {
	if prefix := strings.TrimRight(os.Getenv("PREFIX"), "/"); prefix != "" {
		candidate := filepath.Join(prefix, "bin", "termux-chroot")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	if candidate, err := exec.LookPath("termux-chroot"); err == nil {
		return candidate
	}
	return ""
}

func termuxCommandPath(path string) string {
	prefix := strings.TrimRight(os.Getenv("PREFIX"), "/")
	if prefix != "" {
		if rel, err := filepath.Rel(filepath.Join(prefix, "bin"), path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			// termux-chroot exposes $PREFIX/bin as /usr/bin in its root.
			// Use the name/path relative to that directory instead of the
			// host-side /data/data/... absolute path.
			return rel
		}
	}
	if home := strings.TrimRight(os.Getenv("HOME"), "/"); home != "" {
		if rel, err := filepath.Rel(home, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return rel
		}
	}
	return path
}

func appCommand(path string, args ...string) *exec.Cmd {
	if wrapper := termuxChrootPath(); wrapper != "" {
		argv := make([]string, 0, len(args)+1)
		argv = append(argv, termuxCommandPath(path))
		argv = append(argv, args...)
		return exec.Command(wrapper, argv...)
	}
	return exec.Command(path, args...)
}

func startProcess(path string, args ...string) (int, error) {
	cmd := appCommand(path, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	if dir := termuxAppDir(); dir != "" {
		cmd.Dir = dir
	}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("exec: %w", err)
	}
	return cmd.Process.Pid, nil
}

func startProcessWithOutput(path string, stdout, stderr io.Writer, args ...string) (int, func() error, error) {
	cmd := appCommand(path, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	if dir := termuxAppDir(); dir != "" {
		cmd.Dir = dir
	}
	if err := cmd.Start(); err != nil {
		return 0, nil, fmt.Errorf("exec: %w", err)
	}
	return cmd.Process.Pid, cmd.Wait, nil
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

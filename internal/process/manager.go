// Package process provides platform-abstracted process management.
//
// Instead of relying on external commands like pgrep/pkill,
// it uses PID files for tracking processes. When starting an app,
// the PID is saved to a file. When stopping, the PID is read and
// the process is signaled directly.
//
// Platform-specific files (process_linux.go, process_windows.go,
// process_darwin.go) provide the actual signal/exec implementations.
package process

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Manager handles starting, stopping, and monitoring processes.
type Manager struct {
	PidDir string
}

// NewManager creates a process manager.
// PidDir is where PID files are stored (usually the app directory).
func NewManager(pidDir string) *Manager {
	return &Manager{PidDir: pidDir}
}

// PidFile returns the path to the PID file for an app.
func (pm *Manager) PidFile(appName string) string {
	return filepath.Join(pm.PidDir, appName+".pid")
}

// WritePid saves the PID of a running process.
func (pm *Manager) WritePid(appName string, pid int) error {
	return os.WriteFile(pm.PidFile(appName), []byte(strconv.Itoa(pid)), 0644)
}

// ReadPid reads the PID from file.
func (pm *Manager) ReadPid(appName string) (int, error) {
	data, err := os.ReadFile(pm.PidFile(appName))
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("PID inválido: %w", err)
	}
	return pid, nil
}

// RemovePid deletes the PID file.
func (pm *Manager) RemovePid(appName string) {
	os.Remove(pm.PidFile(appName))
}

// ProcessExists checks if a process with the given PID exists and is alive.
func ProcessExists(pid int) bool {
	// This calls the platform-specific implementation
	return processExists(pid)
}

// KillProcess sends SIGTERM (or equivalent) to a process, then SIGKILL if needed.
func KillProcess(pid int) error {
	return killProcess(pid)
}

// StartProcess starts a background process detached from the parent.
func StartProcess(path string) (int, error) {
	return startProcess(path)
}

// IsRunning checks if an app is running by reading its PID file and
// verifying the process exists.
func (pm *Manager) IsRunning(appName string) bool {
	pid, err := pm.ReadPid(appName)
	if err != nil {
		return false
	}
	if !ProcessExists(pid) {
		// Stale PID file
		pm.RemovePid(appName)
		return false
	}
	return true
}

// Stop gracefully stops an app by PID, with force kill fallback.
// Returns true if the process was actually stopped.
func (pm *Manager) Stop(appName string) (bool, error) {
	pid, err := pm.ReadPid(appName)
	if err != nil {
		// No PID file means we try finding by other means as fallback
		return false, fmt.Errorf("no PID file para %s: %w", appName, err)
	}

	if !ProcessExists(pid) {
		pm.RemovePid(appName)
		return false, nil // already dead
	}

	// Try graceful kill first
	if err := KillProcess(pid); err != nil {
		return false, fmt.Errorf("error al detener PID %d: %w", pid, err)
	}

	// Wait and verify
	for i := 0; i < 10; i++ {
		if !ProcessExists(pid) {
			pm.RemovePid(appName)
			return true, nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Force kill children if they still exist
	if ProcessExists(pid) {
		if err := killProcessForce(pid); err != nil {
			return false, fmt.Errorf("error al forzar detención PID %d: %w", pid, err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	if ProcessExists(pid) {
		return false, fmt.Errorf("no se pudo detener el proceso %d", pid)
	}

	pm.RemovePid(appName)
	return true, nil
}

// Start starts an app, saves its PID, and returns the PID.
// It looks for the binary in multiple locations.
func (pm *Manager) Start(appName string, appPath string) (int, error) {
	// Verify the binary exists
	if _, err := os.Stat(appPath); err != nil {
		// Fall back to PATH lookup
		found, err := FindAppBinary(appName)
		if err != nil {
			return 0, fmt.Errorf("binario '%s' no encontrado: %w", appName, err)
		}
		appPath = found
	}

	pid, err := StartProcess(appPath)
	if err != nil {
		return 0, fmt.Errorf("error al iniciar %s: %w", appPath, err)
	}

	// Save PID
	if err := pm.WritePid(appName, pid); err != nil {
		// Non-fatal: process is running but we couldn't save PID
		fmt.Fprintf(os.Stderr, "Advertencia: no se pudo guardar PID: %v\n", err)
	}

	return pid, nil
}

// FindAppBinary locates an app binary in the same directory as the
// running executable or in PATH.
func FindAppBinary(name string) (string, error) {
	// First check same directory as the manager binary
	updaterPath, err := os.Executable()
	if err == nil {
		updaterPath, err = filepath.EvalSymlinks(updaterPath)
		if err == nil {
			dir := filepath.Dir(updaterPath)
			appPath := filepath.Join(dir, name)
			if _, err := os.Stat(appPath); err == nil {
				return filepath.Abs(appPath)
			}
		}
	}

	// Common additional paths for Termux and user installs
	extraDirs := []string{
		filepath.Join(os.Getenv("HOME"), "bin"),
		filepath.Join(os.Getenv("HOME"), ".local", "bin"),
		"/data/data/com.termux/files/usr/bin",
	}

	for _, dir := range extraDirs {
		appPath := filepath.Join(dir, name)
		if fi, err := os.Stat(appPath); err == nil {
			m := fi.Mode()
			if m&0111 != 0 { // is executable
				return filepath.Abs(appPath)
			}
		}
	}

	// Then check PATH
	if path, err := LookPath(name); err == nil {
		return filepath.Abs(path)
	}

	return "", fmt.Errorf("'%s' no encontrado en directorio, PATH ni rutas adicionales", name)
}

// LookPath searches for an executable in PATH.
// Wrap exec.LookPath for platform safety.
var LookPath = lookPath

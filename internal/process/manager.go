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
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ap-manager/internal/events"
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

// SanitizeID derives a stable, filesystem-safe key from a repo ID so that
// per-repo artifacts (PID files, temp/backup files) never collide across
// repos, even when they share the same AppName. The readable sanitized
// prefix is suffixed with a short hash so distinct IDs that sanitize
// identically (e.g. "a/b" and "a b") still map to distinct keys.
func SanitizeID(repoID string) string {
	if repoID == "" {
		repoID = "default"
	}
	var b strings.Builder
	for _, r := range repoID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	h := fnv.New32a()
	h.Write([]byte(repoID))
	return fmt.Sprintf("%s_%x", b.String(), h.Sum32())
}

// PidFile returns the path to the PID file for a repo. The file name is
// derived from the repo ID (see SanitizeID), so different repos never share
// a PID file even when they use the same AppName.
func (pm *Manager) PidFile(repoID string) string {
	return filepath.Join(pm.PidDir, SanitizeID(repoID)+".pid")
}

// pidRecord is the structured payload of a PID file. It ties a PID to the
// executable that was started and, when the platform provides one, a
// process-start identity token, so a reused or stale PID cannot be mistaken
// for the managed process.
type pidRecord struct {
	PID       int    `json:"pid"`
	ExecPath  string `json:"exec_path"`
	StartTime string `json:"start_time,omitempty"` // platform identity token
}

// errLegacyRecord indicates a PID file in the old plain-integer format (or
// otherwise unparseable), which must fail closed: it is never used to signal.
var errLegacyRecord = errors.New("registro PID obsoleto (formato anterior)")

// canonicalExecPath returns the absolute, symlink-resolved form of a binary
// path so it can be compared with the platform's reported executable path.
func canonicalExecPath(appPath string) string {
	p, err := filepath.Abs(appPath)
	if err != nil {
		p = appPath
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return p
}

// WritePid atomically persists the PID record for a repo: a unique temp file
// in PidDir is written, synced and renamed over the target so an interruption
// never leaves a partial record.
func (pm *Manager) WritePid(repoID string, rec pidRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	dest := pm.PidFile(repoID)
	tmp, err := os.CreateTemp(pm.PidDir, "."+SanitizeID(repoID)+".pid-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// ReadPid reads and parses the PID record for a repo. A record missing a
// positive PID or a non-empty canonical executable path (e.g. PID-only JSON
// or a legacy plain-integer file) yields errLegacyRecord so callers fail
// closed instead of signaling an unverified PID.
func (pm *Manager) ReadPid(repoID string) (pidRecord, error) {
	data, err := os.ReadFile(pm.PidFile(repoID))
	if err != nil {
		return pidRecord{}, err
	}
	var rec pidRecord
	if err := json.Unmarshal(data, &rec); err != nil || rec.PID <= 0 || rec.ExecPath == "" {
		return pidRecord{}, errLegacyRecord
	}
	return rec, nil
}

// RemovePid deletes the PID file.
func (pm *Manager) RemovePid(repoID string) {
	os.Remove(pm.PidFile(repoID))
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
func StartProcess(path string, args ...string) (int, error) {
	return startProcess(path, args...)
}

// IsRunning checks if an app is running by reading its PID record and
// verifying that the recorded process identity still matches the live
// process (executable path and start token where the platform provides
// them). A stale/legacy/mismatched record is never treated as running.
func (pm *Manager) IsRunning(repoID string) bool {
	rec, err := pm.ReadPid(repoID)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			// Legacy/corrupt record: remove and treat as not running.
			pm.RemovePid(repoID)
		}
		return false
	}
	if !ProcessExists(rec.PID) {
		// Stale PID file
		pm.RemovePid(repoID)
		return false
	}
	if !verifyProcessIdentity(rec.PID, rec) {
		// The PID no longer belongs to the recorded executable: stale record.
		pm.RemovePid(repoID)
		return false
	}
	return true
}

// Stop gracefully stops an app by PID, with force kill fallback.
// Returns true if the process was actually stopped. Before signaling, the
// stored record must still match the live process; a stale, legacy or
// mismatched record is never used to send a signal.
func (pm *Manager) Stop(repoID string) (bool, error) {
	rec, err := pm.ReadPid(repoID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("no PID file para %s: %w", repoID, err)
		}
		// Legacy/corrupt record: fail closed, never signal based on it.
		pm.RemovePid(repoID)
		return false, fmt.Errorf("registro PID obsoleto o ilegible para %s (%v); se eliminó — reinicia la aplicación bajo el gestor actual para volver a registrarla", repoID, err)
	}
	pid := rec.PID

	if !ProcessExists(pid) {
		pm.RemovePid(repoID)
		return false, nil // already dead
	}

	if !verifyProcessIdentity(pid, rec) {
		pm.RemovePid(repoID)
		return false, fmt.Errorf("el PID %d no corresponde al ejecutable registrado (%s); proceso obsoleto o PID reutilizado — no se detiene nada", pid, rec.ExecPath)
	}

	// Try graceful kill first
	if err := KillProcess(pid); err != nil {
		return false, fmt.Errorf("error al detener PID %d: %w", pid, err)
	}

	// Wait and verify
	for i := 0; i < 10; i++ {
		if !ProcessExists(pid) {
			pm.RemovePid(repoID)
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

	pm.RemovePid(repoID)
	return true, nil
}

// Start starts an app with optional arguments, saves its PID under the repo
// identity, and returns the PID. It looks for the binary in multiple
// locations.
func (pm *Manager) Start(appName, repoID, appPath string, args ...string) (int, error) {
	// Verify the binary exists
	if _, err := os.Stat(appPath); err != nil {
		// Fall back to PATH lookup
		found, err := FindAppBinary(appName)
		if err != nil {
			return 0, fmt.Errorf("binario '%s' no encontrado: %w", appName, err)
		}
		appPath = found
	}

	pid, err := StartProcess(appPath, args...)
	if err != nil {
		return 0, fmt.Errorf("error al iniciar %s: %w", appPath, err)
	}

	// Record the executable identity captured right after start, so later
	// IsRunning/Stop checks can detect PID reuse or stale records.
	rec := pidRecord{PID: pid, ExecPath: canonicalExecPath(appPath)}
	if token, ok := captureStartIdentity(pid); ok {
		rec.StartTime = token
	}
	if err := pm.WritePid(repoID, rec); err != nil {
		// Non-fatal: process is running but we couldn't save the record.
		fmt.Fprintf(os.Stderr, "Advertencia: no se pudo guardar PID: %v\n", err)
	}

	return pid, nil
}

// StartWithCapture starts an app and captures its stdout/stderr, emitting
// each line as an EventAppOutput via the SSE broker. The PID record is saved
// under the repo identity (repoID is also used for the broker events).
// It looks for the binary in multiple locations.
func (pm *Manager) StartWithCapture(appName, repoID, appPath string, broker *events.Broker, args ...string) (int, error) {
	// Verify the binary exists
	if _, err := os.Stat(appPath); err != nil {
		found, err := FindAppBinary(appName)
		if err != nil {
			return 0, fmt.Errorf("binario '%s' no encontrado: %w", appName, err)
		}
		appPath = found
	}

	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()

	pid, err := startProcessWithOutput(appPath, stdoutW, stderrW, args...)
	if err != nil {
		stdoutW.Close()
		stderrW.Close()
		return 0, fmt.Errorf("error al iniciar %s: %w", appPath, err)
	}

	// Stream stdout lines to the broker
	go func() {
		defer stdoutW.Close()
		scanner := bufio.NewScanner(stdoutR)
		for scanner.Scan() {
			broker.Emit(events.NewAppOutput(repoID, scanner.Text(), false))
		}
	}()

	// Stream stderr lines to the broker
	go func() {
		defer stderrW.Close()
		scanner := bufio.NewScanner(stderrR)
		for scanner.Scan() {
			broker.Emit(events.NewAppOutput(repoID, scanner.Text(), true))
		}
	}()

	// Record the executable identity captured right after start.
	rec := pidRecord{PID: pid, ExecPath: canonicalExecPath(appPath)}
	if token, ok := captureStartIdentity(pid); ok {
		rec.StartTime = token
	}
	if err := pm.WritePid(repoID, rec); err != nil {
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

// ResolveAppBinary locates an application binary honoring a configured
// install directory. When installPath is non-empty, only
// <installPath>/<appName> is considered and must be an executable regular
// file (or a symlink resolving to one); an invalid configured path is an
// error and never falls back to the PATH search. When installPath is empty
// the standard FindAppBinary lookup is used unchanged. The returned path is
// the configured/visible path (symlinks preserved) — callers that replace
// the binary must resolve it with filepath.EvalSymlinks first.
func ResolveAppBinary(appName, installPath string) (string, error) {
	if installPath == "" {
		return FindAppBinary(appName)
	}
	p := filepath.Join(installPath, appName)
	fi, err := os.Stat(p) // follows symlinks
	if err != nil {
		return "", fmt.Errorf("'%s' no encontrado en el directorio de instalación %s: %w", appName, installPath, err)
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("'%s' en %s no es un archivo regular", appName, p)
	}
	if fi.Mode()&0111 == 0 { // not executable
		return "", fmt.Errorf("'%s' en %s no es ejecutable", appName, p)
	}
	return filepath.Abs(p)
}

// LookPath searches for an executable in PATH.
// Wrap exec.LookPath for platform safety.
var LookPath = lookPath

// SplitArgs parses a custom command string into a slice of arguments using a
// small shell-like tokenizer. Environment variables are expanded first
// (os.ExpandEnv: $VAR and ${VAR}, including inside quoted text; unset
// variables expand to the empty string), then whitespace separates unquoted
// tokens; single quotes preserve their contents literally; double quotes
// preserve their contents (a backslash still escapes the next character
// inside double quotes); a backslash outside quotes escapes the next
// character. Empty quoted arguments are preserved (e.g. `--name ""`). No
// shell is invoked: no command substitution, globbing, pipes or other shell
// features are processed.
//
// Malformed input is deterministic: an unmatched quote extends to the end of
// the string (the quote is consumed), and a trailing backslash with no next
// character is kept literally.
func SplitArgs(cmd string) []string {
	if cmd == "" {
		return nil
	}
	// Expand environment variables before tokenizing so $VAR/${VAR} work
	// even inside quoted text (restores the previous os.ExpandEnv behavior).
	cmd = os.ExpandEnv(cmd)
	var args []string
	var cur strings.Builder
	flush := func() {
		args = append(args, cur.String())
		cur.Reset()
	}
	quoteSeen := false // a quoted segment (possibly empty) opened the token

	i := 0
	for i < len(cmd) {
		c := cmd[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if cur.Len() > 0 || quoteSeen {
				flush()
				quoteSeen = false
			}
			i++
		case c == '\'':
			// Single quotes: literal contents until the closing quote or
			// end of input.
			i++
			start := i
			for i < len(cmd) && cmd[i] != '\'' {
				i++
			}
			cur.WriteString(cmd[start:i])
			if i < len(cmd) {
				i++ // consume the closing quote
			}
			quoteSeen = true
		case c == '"':
			// Double quotes: contents, with backslash escaping the next
			// character, until the closing quote or end of input.
			i++
			for i < len(cmd) && cmd[i] != '"' {
				if cmd[i] == '\\' && i+1 < len(cmd) {
					i++
					cur.WriteByte(cmd[i])
					i++
					continue
				}
				cur.WriteByte(cmd[i])
				i++
			}
			if i < len(cmd) {
				i++ // consume the closing quote
			}
			quoteSeen = true
		case c == '\\':
			if i+1 < len(cmd) {
				i++
				cur.WriteByte(cmd[i])
				i++
			} else {
				// Trailing backslash: nothing to escape, keep it literally.
				cur.WriteByte('\\')
				i++
			}
		default:
			cur.WriteByte(cmd[i])
			i++
		}
	}
	if cur.Len() > 0 || quoteSeen {
		flush()
	}
	return args
}

// DetectInstallDir returns the best directory for installing binaries
// based on the current platform (Termux, Linux, etc.).
func DetectInstallDir() string {
	// Termux detection
	if prefix := os.Getenv("PREFIX"); prefix != "" {
		return filepath.Join(prefix, "bin")
	}
	home := os.Getenv("HOME")
	if home != "" {
		return filepath.Join(home, "bin")
	}
	exe, err := os.Executable()
	if err == nil {
		return filepath.Dir(exe)
	}
	return "/usr/local/bin"
}

// EnsureInstallDir creates the install directory if it doesn't exist.
func EnsureInstallDir(dir string) error {
	return os.MkdirAll(dir, 0755)
}

// BinaryExists checks if an app binary exists in known locations.
func BinaryExists(appName string) bool {
	_, err := FindAppBinary(appName)
	return err == nil
}

// InstallBinary installs a binary to a target directory and makes it
// executable. The complete source is copied to a unique temporary file in the
// same directory, synced, given executable permissions, closed, and then
// renamed over the destination, so a copy/sync/chmod failure never leaves a
// partially written executable. The final os.Rename replaces an existing
// destination on every supported platform (POSIX atomically; on Windows via
// MOVEFILE_REPLACE_EXISTING, which is best-effort and not atomic). On Windows
// the rename fails if the destination is locked (e.g. the executable is
// currently running); in that case the temporary file is removed and the
// prior destination is left untouched. A remove-then-rename fallback is
// intentionally not used: it cannot succeed where the replace-rename fails
// (a locked destination also cannot be deleted) and it would create a window
// where the destination does not exist.
// The caller-owned source is only read; it is never modified or removed here.
func InstallBinary(srcPath, destDir, appName string) (string, error) {
	if err := EnsureInstallDir(destDir); err != nil {
		return "", fmt.Errorf("crear directorio %s: %w", destDir, err)
	}
	destPath := filepath.Join(destDir, appName)

	// Unique temp file in the destination directory (same filesystem, so
	// the final rename replaces the destination atomically on POSIX).
	tmp, err := os.CreateTemp(destDir, "."+appName+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("crear archivo temporal en %s: %w", destDir, err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpPath)
	}

	in, err := os.Open(srcPath)
	if err != nil {
		cleanup()
		return "", fmt.Errorf("abrir binario %s: %w", srcPath, err)
	}
	if _, err := io.Copy(tmp, in); err != nil {
		in.Close()
		cleanup()
		return "", fmt.Errorf("copiar binario a %s: %w", tmpPath, err)
	}
	if err := in.Close(); err != nil {
		cleanup()
		return "", fmt.Errorf("cerrar binario fuente %s: %w", srcPath, err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return "", fmt.Errorf("sincronizar archivo temporal %s: %w", tmpPath, err)
	}
	if err := tmp.Chmod(0755); err != nil {
		cleanup()
		return "", fmt.Errorf("establecer permisos en %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("cerrar archivo temporal %s: %w", tmpPath, err)
	}
	// os.Rename replaces an existing destination on all supported platforms
	// (POSIX atomically; Windows best-effort via MOVEFILE_REPLACE_EXISTING).
	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("reemplazar %s: %w", destPath, err)
	}
	return destPath, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

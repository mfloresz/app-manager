package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ap-manager/internal/github"
	"ap-manager/internal/storage"
)

// buildPlatformSuffix maps GOOS/GOARCH to the asset suffix used in CI builds.
func buildPlatformSuffix(goos, goarch string) string {
	switch goos + "/" + goarch {
	case "linux/amd64":
		return "linux-amd64"
	case "linux/arm64":
		return "linux-arm64"
	case "linux/arm":
		return "linux-armv7"
	case "android/arm64":
		return "android-arm64"
	case "android/arm":
		return "android-armv7"
	case "darwin/amd64":
		return "darwin-amd64"
	case "darwin/arm64":
		return "darwin-arm64"
	default:
		return goos + "-" + goarch
	}
}

// getDetachedAttr returns SysProcAttr to detach a process from the parent.
func getDetachedAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setsid: true,
	}
}

// ── Supervisor detection ────────────────────────────────────

// selfServiceName is the systemd user unit name that install.sh creates for
// ap-manager; it is the coordination contract for supervised self-updates.
const selfServiceName = "ap-manager.service"

// supervisorMode identifies how the current ap-manager process is managed.
type supervisorMode string

const (
	modeManual      supervisorMode = "manual"
	modeSystemdUser supervisorMode = "systemd"
)

// detectSupervisorMode conservatively reports whether ap-manager is running
// under the systemd user manager. It only returns modeSystemdUser when
// systemd itself signals a managed process (INVOCATION_ID), a user runtime
// directory is present (XDG_RUNTIME_DIR, where the user manager socket
// lives), and both systemctl and systemd-run are on PATH.
func detectSupervisorMode(getenv func(string) string, hasCmd func(string) bool) supervisorMode {
	if getenv("INVOCATION_ID") != "" && getenv("XDG_RUNTIME_DIR") != "" && hasCmd("systemctl") && hasCmd("systemd-run") {
		return modeSystemdUser
	}
	return modeManual
}

// systemdUnitName derives a unique transient unit name from a unique script
// path (both share the same random suffix).
func systemdUnitName(scriptPath string) string {
	base := filepath.Base(scriptPath)
	base = strings.TrimSuffix(base, ".sh")
	return base + ".service"
}

// systemdRunArgs builds the systemd-run command line used to launch the
// self-update helper in its own transient user unit (outside the ap-manager
// service cgroup, so it survives the service being stopped). --collect
// garbage-collects the unit after the helper exits.
func systemdRunArgs(unitName, scriptPath string) []string {
	return []string{"--user", "--unit=" + unitName, "--collect", "/bin/sh", scriptPath}
}

// ── Self-update version gate ─────────────────────────────────

// updateDecision is the outcome of the self-update version gate.
type updateDecision int

const (
	updateProceed updateDecision = iota // latest is strictly newer (or current is dev)
	updateNoop                          // current and latest are equal
	updateOlder                         // latest is older than current (downgrade)
	updateUnknown                       // versions are not comparable
)

// reason returns a stable machine-readable description for API responses.
func (d updateDecision) reason() string {
	switch d {
	case updateProceed:
		return "update_available"
	case updateNoop:
		return "already_latest"
	case updateOlder:
		return "latest_older"
	default:
		return "unknown_versions"
	}
}

// normalizeSelfVersion strips a leading "v"/"V"/"version " prefix,
// mirroring the normalization used by the update pipeline. Longer prefixes
// are checked first so that "version 1.2.3" is not truncated to "ersion ...".
func normalizeSelfVersion(s string) string {
	s = strings.TrimSpace(s)
	for _, prefix := range []string{"version ", "Version ", "v", "V"} {
		if strings.HasPrefix(s, prefix) {
			s = s[len(prefix):]
		}
	}
	return s
}

// versionParts parses a dotted numeric version ("1.2.3") into numeric parts.
// It returns nil when s is not a plain dotted numeric version.
func versionParts(s string) []int {
	fields := strings.Split(s, ".")
	parts := make([]int, 0, len(fields))
	for _, f := range fields {
		if f == "" {
			return nil
		}
		n := 0
		for _, c := range f {
			if c < '0' || c > '9' {
				return nil
			}
			n = n*10 + int(c-'0')
		}
		parts = append(parts, n)
	}
	return parts
}

// compareVersions compares two dotted numeric versions: -1, 0 or 1 when a is
// respectively older, equal or newer than b. ok is false when either version
// is not a plain dotted numeric version.
func compareVersions(a, b string) (cmp int, ok bool) {
	pa, pb := versionParts(a), versionParts(b)
	if pa == nil || pb == nil {
		return 0, false
	}
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		va, vb := 0, 0
		if i < len(pa) {
			va = pa[i]
		}
		if i < len(pb) {
			vb = pb[i]
		}
		if va < vb {
			return -1, true
		}
		if va > vb {
			return 1, true
		}
	}
	return 0, true
}

// decideSelfUpdate gates whether the current ap-manager version may be
// replaced by the latest release. A "dev" build is eligible for any real
// (parseable) release. Otherwise the versions are normalized and compared:
// only a strictly newer latest version is an update; equal, older, or
// unparseable versions never trigger a replacement.
func decideSelfUpdate(current, latest string) updateDecision {
	c := normalizeSelfVersion(current)
	l := normalizeSelfVersion(latest)
	if c == "dev" {
		if versionParts(l) != nil {
			return updateProceed
		}
		return updateUnknown
	}
	if c == l {
		return updateNoop
	}
	cmp, ok := compareVersions(c, l)
	if !ok {
		return updateUnknown
	}
	switch {
	case cmp < 0:
		return updateProceed
	case cmp == 0:
		return updateNoop
	default:
		return updateOlder
	}
}

// handleSelfCheck checks GitHub for the latest version of ap-manager.
func (h *Handler) handleSelfCheck(w http.ResponseWriter, r *http.Request) {
	rel, err := h.Updater.GitHub.LatestRelease("mfloresz", "app-manager")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"current": h.Version,
			"error":   err.Error(),
		})
		return
	}

	decision := decideSelfUpdate(h.Version, rel.TagName)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"current":     h.Version,
		"latest":      rel.TagName,
		"has_update":  decision == updateProceed,
		"reason":      decision.reason(),
		"release_url": fmt.Sprintf("https://github.com/mfloresz/app-manager/releases/tag/%s", rel.TagName),
	})
}

// handleSelfUpdate generates and executes a self-update script for ap-manager.
func (h *Handler) handleSelfUpdate(w http.ResponseWriter, r *http.Request) {
	repo := h.Store.Find("mfloresz/app-manager")
	if repo == nil {
		http.Error(w, "Auto-repo no encontrado", http.StatusNotFound)
		return
	}

	h.selfUpdate(w, r, repo)
}

// selfUpdate handles the actual self-update flow (called from handleSelfUpdate or handleUpdateRepo).
func (h *Handler) selfUpdate(w http.ResponseWriter, r *http.Request, repo *storage.Repository) {
	// 1. Fetch latest release info
	rel, err := h.Updater.GitHub.LatestRelease("mfloresz", "app-manager")
	if err != nil {
		h.Broker.EmitError(repo.ID, "Error al consultar GitHub: "+err.Error())
		http.Error(w, "Error al consultar GitHub: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 1b. Conservative version gate: never replace unless the latest release
	// is strictly newer (or current is a dev build and the release is real).
	decision := decideSelfUpdate(h.Version, rel.TagName)
	if decision != updateProceed {
		h.Broker.EmitLog(repo.ID, fmt.Sprintf("ℹ️ Auto-actualización no iniciada: %s (actual %s, latest %s)", decision.reason(), h.Version, rel.TagName))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "no_update",
			"reason":  decision.reason(),
			"current": h.Version,
			"latest":  rel.TagName,
		})
		return
	}

	// 2. Build expected asset name: ap-manager-{suffix}-{tag}
	suffix := buildPlatformSuffix(h.OS, h.Arch)
	assetName := fmt.Sprintf("ap-manager-%s-%s", suffix, rel.TagName)

	// 3. Find matching asset
	asset := github.FindAsset(rel.Assets, assetName)
	if asset == nil {
		msg := fmt.Sprintf("No se encontró asset para esta plataforma: %s", assetName)
		h.Broker.EmitError(repo.ID, msg)
		http.Error(w, msg, http.StatusNotFound)
		return
	}

	// 4. Get current binary path and PID
	binaryPath, err := os.Executable()
	if err != nil {
		h.Broker.EmitError(repo.ID, "Error al obtener ruta del binario: "+err.Error())
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}
	binaryPath, err = filepath.EvalSymlinks(binaryPath)
	if err != nil {
		h.Broker.EmitError(repo.ID, "Error al resolver symlink: "+err.Error())
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}
	pid := os.Getpid()

	// 5. Detect supervisor context and generate the self-update script with
	// the mode/service identity baked in (the helper does not re-detect).
	mode := detectSupervisorMode(os.Getenv, func(name string) bool {
		_, err := exec.LookPath(name)
		return err == nil
	})
	serviceName := ""
	if mode == modeSystemdUser {
		serviceName = selfServiceName
	}
	scriptContent, logPath := generateSelfUpdateScript(binaryPath, pid, asset.DownloadURL, rel.TagName, mode, serviceName)

	// 6. Write script to a unique temp file
	scriptPath := uniqueTempPath("ap-manager-selfupdate-", ".sh")
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		h.Broker.EmitError(repo.ID, "Error al escribir script: "+err.Error())
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}
	unitName := systemdUnitName(scriptPath)

	// 7. Update repo status
	h.Store.Update(repo.ID, func(r *storage.Repository) {
		r.Status = storage.StatusChecking
		r.Progress = 0
	})

	// 8. Launch the helper, mode-aware (fail closed under a supervisor).
	if err := h.launchSelfUpdate(scriptPath, unitName, mode); err != nil {
		os.Remove(scriptPath)
		h.Broker.EmitError(repo.ID, "Error al lanzar la auto-actualización: "+err.Error())
		http.Error(w, "Error al lanzar la auto-actualización: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 9. Send log message before the process potentially dies
	if mode == modeSystemdUser {
		h.Broker.EmitLog(repo.ID, fmt.Sprintf("🔄 Auto-actualización a %s iniciada (modo systemd, unidad %s)", rel.TagName, unitName))
	} else {
		h.Broker.EmitLog(repo.ID, fmt.Sprintf("🔄 Auto-actualización a %s iniciada (modo manual)", rel.TagName))
	}
	h.Broker.EmitLog(repo.ID, fmt.Sprintf("📋 Log: %s", logPath))
	h.Broker.EmitLog(repo.ID, "⚠️ La conexión se cerrará durante la actualización. Refresca la página tras unos segundos.")

	// 10. Respond to client
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "self_updating",
		"new_version": rel.TagName,
		"mode":        string(mode),
		"log":         logPath,
		"script":      scriptPath,
	})
}

// launchSelfUpdate executes the generated update script according to the
// detected supervisor mode. Under the systemd user manager the helper is
// started through a transient user unit (so it runs outside the ap-manager
// service cgroup and survives the service being stopped); if the user manager
// is unavailable it fails closed instead of falling back to the manual nohup
// path. In manual mode the helper is detached directly.
func (h *Handler) launchSelfUpdate(scriptPath, unitName string, mode supervisorMode) error {
	if mode == modeSystemdUser {
		cmd := exec.Command("systemd-run", systemdRunArgs(unitName, scriptPath)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("systemd-run --user no disponible o falló: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	cmd := exec.Command("/bin/sh", scriptPath)
	cmd.SysProcAttr = getDetachedAttr()
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

// shellQuote wraps s in single quotes so it can be embedded safely inside a
// /bin/sh script; embedded single quotes are escaped.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// uniqueTempPath reserves a unique file path under the system temp dir.
func uniqueTempPath(prefix, suffix string) string {
	f, err := os.CreateTemp("", prefix+"*"+suffix)
	if err == nil {
		path := f.Name()
		f.Close()
		return path
	}
	// Fallback (CreateTemp failure): timestamped name, best effort.
	return filepath.Join(os.TempDir(), prefix+strconv.FormatInt(time.Now().UnixNano(), 36)+suffix)
}

// generateSelfUpdateScript creates a standalone shell script that performs the
// self-update. The script embeds all values so it doesn't depend on
// ap-manager at all. Every embedded shell value (binaryPath, downloadURL,
// version, logPath, mode, serviceName) is single-quote escaped via
// shellQuote; the script uses set -eu and fails fast on any failed
// download/copy/move/chmod/start operation. In modeSystemdUser the script
// coordinates lifecycle through the systemd user manager (stop/replace/start
// the ap-manager service, MainPID liveness) instead of kill/nohup; in manual
// mode it preserves the detached kill/nohup behavior.
func generateSelfUpdateScript(binaryPath string, pid int, downloadURL, version string, mode supervisorMode, serviceName string) (script string, logPath string) {
	logPath = uniqueTempPath("ap-manager-update-", ".log")

	script = fmt.Sprintf(`#!/bin/sh
# Self-update script for ap-manager
# Generated by ap-manager — do not edit manually
set -eu

BINARY=%s
PID=%d
URL=%s
VERSION=%s
LOG=%s
MODE=%s
SERVICE=%s
NEW_PID=0
EXPECTED_COMM="$(basename "$BINARY" | cut -c1-15)"

# Redirect output to log
exec > "$LOG" 2>&1

echo "[$(date)] Self-update started: $VERSION (mode: $MODE)"
echo "[$(date)] Binary: $BINARY"
echo "[$(date)] PID: $PID"

# ── Helpers ───────────────────────────────────────────────

# is_alive: true if the exact PID is alive and (when /proc is available)
# still belongs to ap-manager and is not a zombie.
is_alive() {
    kill -0 "$1" 2>/dev/null || return 1
    if [ -r "/proc/$1/comm" ]; then
        case "$(cat "/proc/$1/comm" 2>/dev/null || true)" in
            "$EXPECTED_COMM") ;;
            *) return 1 ;;
        esac
    fi
    if [ -r "/proc/$1/status" ]; then
        case "$(sed -n 's/^State:[[:space:]]*//p' "/proc/$1/status" 2>/dev/null || true)" in
            Z*) return 1 ;;
            *) ;;
        esac
    fi
    return 0
}

# service_main_pid: MainPID reported by systemd for the managed unit, or "".
service_main_pid() {
    systemctl --user show "$SERVICE" -p MainPID --value 2>/dev/null || true
}

# service_active: true when the managed unit is active.
service_active() {
    systemctl --user is-active --quiet "$SERVICE" 2>/dev/null
}

# wait_service_alive: polls the managed unit's MainPID until it is a real,
# alive process (rejects empty/0/zombie/wrong process), or times out.
wait_service_alive() {
    i=0
    while [ "$i" -lt 15 ]; do
        MPID="$(service_main_pid)"
        if [ -n "$MPID" ] && [ "$MPID" != "0" ] && is_alive "$MPID"; then
            return 0
        fi
        sleep 1
        i=$((i+1))
    done
    return 1
}

# stop_service: stops the managed unit, tolerating the already-stopped state.
stop_service() {
    if ! systemctl --user stop "$SERVICE" 2>/dev/null; then
        if service_active; then
            echo "[$(date)] ERROR: could not stop $SERVICE"
            return 1
        fi
        echo "[$(date)] INFO: $SERVICE was already stopped"
    else
        echo "[$(date)] Service $SERVICE stopped"
    fi
    return 0
}

# restore_backup: put the previous binary back and try to start it.
# Returns 0 on successful recovery, nonzero otherwise.
restore_backup() {
    echo "[$(date)] ERROR: restoring backup ..."
    case "$MODE" in
        systemd)
            # Stop the managed unit before restoring so the old and new
            # instances never run together.
            stop_service || true
            ;;
        *)
            if [ "$NEW_PID" != 0 ]; then
                kill -9 "$NEW_PID" 2>/dev/null || true
            fi
            kill -9 "$PID" 2>/dev/null || true
            ;;
    esac
    if [ ! -f "${BINARY}.bak" ]; then
        echo "[$(date)] ERROR: recovery failed: backup not found"
        return 1
    fi
    if ! mv "${BINARY}.bak" "$BINARY"; then
        echo "[$(date)] ERROR: recovery failed: could not restore backup"
        return 1
    fi
    chmod 755 "$BINARY" 2>/dev/null || true
    echo "[$(date)] Backup restored"
    case "$MODE" in
        systemd)
            if ! systemctl --user start "$SERVICE" 2>/dev/null; then
                echo "[$(date)] ERROR: recovery failed: could not start $SERVICE"
                return 1
            fi
            if ! wait_service_alive; then
                echo "[$(date)] ERROR: recovery failed: $SERVICE did not pass liveness"
                return 1
            fi
            ;;
        *)
            nohup "$BINARY" > /dev/null 2>&1 &
            RESTART_PID=$!
            sleep 2
            if ! is_alive "$RESTART_PID"; then
                echo "[$(date)] ERROR: recovery failed: previous version did not stay running"
                return 1
            fi
            ;;
    esac
    echo "[$(date)] Recovery OK: previous version restarted"
    return 0
}

# restore_on_exit: safety net — restore the backup whenever the script exits
# before the replacement has passed the liveness check.
restore_on_exit() {
    rm -f "${BINARY}.tmp"
    [ -f "${BINARY}.bak" ] || return 0
    echo "[$(date)] ERROR: update did not complete; restoring backup"
    restore_backup || true
}

trap restore_on_exit EXIT

# Wait for the HTTP response to be sent before touching the current process
sleep 3

# Download new version (fail on HTTP errors, follow redirects)
echo "[$(date)] Downloading from $URL ..."
if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o "${BINARY}.tmp" "$URL" || { echo "[$(date)] ERROR: curl download failed"; rm -f "${BINARY}.tmp"; exit 1; }
elif command -v wget >/dev/null 2>&1; then
    wget -qO "${BINARY}.tmp" "$URL" || { echo "[$(date)] ERROR: wget download failed"; rm -f "${BINARY}.tmp"; exit 1; }
else
    echo "[$(date)] ERROR: neither curl nor wget found"
    exit 1
fi

if [ ! -f "${BINARY}.tmp" ]; then
    echo "[$(date)] ERROR: download produced no file"
    exit 1
fi

# Make the temp file executable before validating it
chmod 755 "${BINARY}.tmp"
echo "[$(date)] Download complete"

# Validate the downloaded binary BEFORE stopping the current process:
# --version must exit 0 and print something. This rejects HTML/error
# bodies, truncated or non-executable files and wrong-architecture
# binaries without touching the running ap-manager. The real binary
# supports --version (non-blocking); timeout is used when available.
echo "[$(date)] Validating downloaded binary ..."
VERIFY_OUT=""
VERIFY_STATUS=0
if command -v timeout >/dev/null 2>&1; then
    VERIFY_OUT="$(timeout 10 "${BINARY}.tmp" --version 2>&1)" || VERIFY_STATUS=$?
else
    VERIFY_OUT="$("${BINARY}.tmp" --version 2>&1)" || VERIFY_STATUS=$?
fi
if [ "$VERIFY_STATUS" -ne 0 ] || [ -z "$VERIFY_OUT" ]; then
    echo "[$(date)] ERROR: downloaded binary failed validation (status=$VERIFY_STATUS)"
    rm -f "${BINARY}.tmp"
    exit 1
fi
echo "[$(date)] Validation OK: $VERIFY_OUT"

# Backup the current binary BEFORE stopping the current process
if ! cp "$BINARY" "${BINARY}.bak"; then
    echo "[$(date)] ERROR: could not create backup"
    rm -f "${BINARY}.tmp"
    exit 1
fi
if [ ! -f "${BINARY}.bak" ]; then
    echo "[$(date)] ERROR: backup missing after copy"
    rm -f "${BINARY}.tmp"
    exit 1
fi
echo "[$(date)] Backup created: ${BINARY}.bak"

# Stop the current process / managed unit
case "$MODE" in
    systemd)
        echo "[$(date)] Stopping service $SERVICE ..."
        stop_service || exit 1
        ;;
    *)
        echo "[$(date)] Killing PID $PID ..."
        kill "$PID" 2>/dev/null || true
        sleep 1
        if kill -0 "$PID" 2>/dev/null; then
            kill -9 "$PID" 2>/dev/null || true
        fi
        sleep 1
        ;;
esac

# Replace the binary
if ! mv "${BINARY}.tmp" "$BINARY"; then
    echo "[$(date)] ERROR: could not replace binary"
    if restore_backup; then
        exit 0
    fi
    exit 1
fi
if ! chmod 755 "$BINARY"; then
    echo "[$(date)] ERROR: could not chmod new binary"
    if restore_backup; then
        exit 0
    fi
    exit 1
fi
echo "[$(date)] Binary replaced"

# Start the new version and verify
case "$MODE" in
    systemd)
        echo "[$(date)] Starting service $SERVICE ..."
        if ! systemctl --user start "$SERVICE" 2>/dev/null; then
            echo "[$(date)] ERROR: could not start $SERVICE"
            if restore_backup; then
                exit 0
            fi
            exit 1
        fi
        if ! wait_service_alive; then
            echo "[$(date)] ERROR: $SERVICE did not pass the liveness check"
            if restore_backup; then
                exit 0
            fi
            exit 1
        fi
        echo "[$(date)] Service $SERVICE restarted successfully"
        ;;
    *)
        echo "[$(date)] Starting new version..."
        nohup "$BINARY" > /dev/null 2>&1 &
        NEW_PID=$!
        echo "[$(date)] New ap-manager started with PID $NEW_PID"
        LIVE=0
        i=0
        while [ "$i" -lt 10 ]; do
            if is_alive "$NEW_PID"; then
                LIVE=1
                break
            fi
            sleep 1
            i=$((i+1))
        done
        if [ "$LIVE" -ne 1 ]; then
            echo "[$(date)] ERROR: new ap-manager (PID $NEW_PID) did not pass the liveness check"
            if restore_backup; then
                exit 0
            fi
            exit 1
        fi
        echo "[$(date)] New ap-manager is running (PID $NEW_PID)"
        ;;
esac

# Replacement confirmed: safe to remove the backup
rm -f "${BINARY}.bak"
echo "[$(date)] Cleanup complete. Update to $VERSION finished."
`, shellQuote(binaryPath), pid, shellQuote(downloadURL), shellQuote(version), shellQuote(logPath), shellQuote(string(mode)), shellQuote(serviceName))

	return script, logPath
}

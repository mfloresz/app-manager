package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

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

	hasUpdate := rel.TagName != h.Version && !strings.HasSuffix(h.Version, rel.TagName)
	// "dev" always considers a real release as "new"
	if h.Version == "dev" {
		hasUpdate = true
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"current":      h.Version,
		"latest":       rel.TagName,
		"has_update":   hasUpdate,
		"release_url":  fmt.Sprintf("https://github.com/mfloresz/app-manager/releases/tag/%s", rel.TagName),
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

	// 5. Generate the self-update script
	scriptContent, logPath := generateSelfUpdateScript(binaryPath, pid, asset.DownloadURL, rel.TagName)

	// 6. Write script to temp file
	scriptPath := filepath.Join(os.TempDir(), "ap-manager-selfupdate.sh")
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		h.Broker.EmitError(repo.ID, "Error al escribir script: "+err.Error())
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}

	// 7. Update repo status
	h.Store.Update(repo.ID, func(r *storage.Repository) {
		r.Status = storage.StatusChecking
		r.Progress = 0
	})

	// 8. Execute the script detached
	cmd := exec.Command("/bin/sh", scriptPath)
	cmd.SysProcAttr = getDetachedAttr()
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		h.Broker.EmitError(repo.ID, "Error al ejecutar script: "+err.Error())
		http.Error(w, "Error al ejecutar script: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 9. Send log message before the process potentially dies
	h.Broker.EmitLog(repo.ID, fmt.Sprintf("🔄 Auto-actualización a %s iniciada (PID %d)", rel.TagName, cmd.Process.Pid))
	h.Broker.EmitLog(repo.ID, fmt.Sprintf("📋 Log: %s", logPath))
	h.Broker.EmitLog(repo.ID, "⚠️ La conexión se cerrará durante la actualización. Refresca la página tras unos segundos.")

	// 10. Respond to client
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "self_updating",
		"new_version": rel.TagName,
		"log":         logPath,
		"script":      scriptPath,
	})
}

// generateSelfUpdateScript creates a standalone shell script that performs the self-update.
// The script embeds all values so it doesn't depend on ap-manager at all.
func generateSelfUpdateScript(binaryPath string, pid int, downloadURL, version string) (script string, logPath string) {
	logPath = filepath.Join(os.TempDir(), "ap-manager-update.log")

	script = fmt.Sprintf(`#!/bin/sh
# Self-update script for ap-manager
# Generated by ap-manager — do not edit manually
BINARY="%s"
PID=%d
URL="%s"
VERSION="%s"
LOG="%s"

# Redirect output to log
exec > "$LOG" 2>&1

echo "[$(date)] Self-update started: %s"
echo "[$(date)] Binary: $BINARY"
echo "[$(date)] PID: $PID"

# Wait for HTTP response to be sent
sleep 3

# Download new version
echo "[$(date)] Downloading from $URL ..."
if command -v curl >/dev/null 2>&1; then
    curl -sL -o "${BINARY}.tmp" "$URL"
elif command -v wget >/dev/null 2>&1; then
    wget -qO "${BINARY}.tmp" "$URL"
else
    echo "[$(date)] ERROR: Neither curl nor wget found"
    exit 1
fi

if [ ! -f "${BINARY}.tmp" ]; then
    echo "[$(date)] ERROR: Download failed"
    exit 1
fi
chmod 755 "${BINARY}.tmp"
echo "[$(date)] Download complete"

# Backup current binary
cp "$BINARY" "${BINARY}.bak"
echo "[$(date)] Backup created: ${BINARY}.bak"

# Kill current ap-manager process (exact PID)
echo "[$(date)] Killing PID $PID ..."
kill "$PID" 2>/dev/null
sleep 1
# Force kill if still alive
if kill -0 "$PID" 2>/dev/null; then
    kill -9 "$PID" 2>/dev/null
fi
sleep 1

# Replace binary
mv "${BINARY}.tmp" "$BINARY"
echo "[$(date)] Binary replaced"

# Start new version
echo "[$(date)] Starting new version..."
nohup "$BINARY" > /dev/null 2>&1 &
NEW_PID=$!
echo "[$(date)] New ap-manager started with PID $NEW_PID"

# Cleanup backup after successful start
sleep 5
rm -f "${BINARY}.bak"
echo "[$(date)] Cleanup complete. Update to %s finished."
`, binaryPath, pid, downloadURL, version, logPath, version, version)

	return script, logPath
}

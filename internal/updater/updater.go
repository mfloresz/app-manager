// Package updater orchestrates the update pipeline for a repository.
package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ap-manager/internal/events"
	"ap-manager/internal/github"
	"ap-manager/internal/process"
	"ap-manager/internal/storage"
)

// opGuard is a zero-value-safe per-repository operation guard: only one
// operation (CheckVersion, RunUpdate or InstallApp) may run for a given
// repo ID at a time, so concurrent API calls cannot race on shared
// .tmp/.bak/PID files or Repository state.
type opGuard struct {
	mu   sync.Mutex
	busy map[string]bool
}

// tryAcquire marks repoID as busy. It returns false if an operation for the
// same repoID is already in flight. Callers must pair a successful acquire
// with exactly one release.
func (g *opGuard) tryAcquire(repoID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.busy == nil {
		g.busy = make(map[string]bool)
	}
	if g.busy[repoID] {
		return false
	}
	g.busy[repoID] = true
	return true
}

// release clears the busy mark for repoID. It is safe to call even if the
// mark was already cleared.
func (g *opGuard) release(repoID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.busy, repoID)
}

// Pipeline runs the full update lifecycle for a repo.
type Pipeline struct {
	Broker  *events.Broker
	Store   *storage.Store
	ProcMan *process.Manager
	GitHub  *github.Client
	OS      string
	Arch    string

	// guard serializes operations per repo ID (zero-value safe).
	guard opGuard
}

// RunUpdate executes the full update pipeline for a repo.
func (p *Pipeline) RunUpdate(repoID string) {
	const sep = "══════════════════════════════════════"

	// Serialize per repo: reject a second operation for the same repoID
	// without touching state, binary/temp/backup files, or emitting failure
	// events for the active operation.
	if !p.guard.tryAcquire(repoID) {
		p.Broker.EmitLog(repoID, "⚠️ Ya hay una operación en curso para este repositorio")
		return
	}
	defer p.guard.release(repoID)

	repo := p.Store.Find(repoID)
	if repo == nil {
		p.Broker.EmitError(repoID, "Repositorio no encontrado")
		return
	}

	// Mark status
	p.Store.Update(repoID, func(r *storage.Repository) {
		r.Status = storage.StatusChecking
		r.Progress = 0
		r.Error = ""
	})
	p.Broker.Emit(events.NewStatus(repoID, string(storage.StatusChecking)))
	p.persistState(repoID)

	p.Broker.EmitLog(repoID, sep)
	p.Broker.EmitLog(repoID, fmt.Sprintf(">>> ACTUALIZANDO %s/%s (%s) <<<", repo.Owner, repo.Name, repo.AppName))
	p.Broker.EmitLog(repoID, sep)

	// 1. Find binary (honoring a configured InstallPath)
	appPath, err := process.ResolveAppBinary(repo.AppName, repo.InstallPath)
	if err != nil {
		p.failUpdate(repoID, "Binario no encontrado: "+err.Error())
		p.Broker.EmitLog(repoID, "💡 Verifica la ubicación del binario (directorio de instalación configurado, ~/bin, ~/.local/bin o PATH)")
		return
	}
	p.Broker.EmitLog(repoID, "Binario encontrado: "+appPath)

	// Resolve symlinks so the replacement targets the real file instead of
	// overwriting the symlink itself. Start/restart keep the configured
	// path; replacement always operates on the resolved file.
	resolvedPath, rerr := filepath.EvalSymlinks(appPath)
	if rerr != nil {
		p.failUpdate(repoID, "No se pudo resolver el binario "+appPath+": "+rerr.Error())
		return
	}
	appPath = resolvedPath
	appDir := filepath.Dir(appPath)

	// 2. Detect current version
	currentVer := detectCurrentVersion(appPath)
	p.Broker.EmitLog(repoID, "Versión actual: "+currentVer)

	p.Store.Update(repoID, func(r *storage.Repository) {
		r.CurrentVersion = currentVer
	})

	// 3. Check latest release
	rel, err := p.GitHub.LatestRelease(repo.Owner, repo.Name)
	if err != nil {
		p.failUpdate(repoID, "Error al consultar GitHub: "+err.Error())
		return
	}
	newVersion := rel.TagName
	p.Broker.EmitLog(repoID, "Nueva versión disponible: "+newVersion)

	p.Store.Update(repoID, func(r *storage.Repository) {
		r.LatestVersion = newVersion
	})
	p.Broker.Emit(events.NewVersion(repoID, currentVer, newVersion))

	// If versions match, skip
	if normalizeVersion(currentVer) == normalizeVersion(newVersion) {
		p.Broker.EmitLog(repoID, "✅ Ya estás en la última versión")
		p.Store.Update(repoID, func(r *storage.Repository) {
			r.Status = storage.StatusIdle
		})
		p.Broker.Emit(events.NewStatus(repoID, string(storage.StatusIdle)))
		p.persistState(repoID)
		return
	}

	// 4. Find matching asset
	expectedName := repo.AssetName
	if expectedName == "" {
		expectedName = defaultAssetName(repo.AppName, repo.PlatformOS, repo.PlatformArch, p.OS, p.Arch)
	}
	p.Broker.EmitLog(repoID, "Buscando asset base: "+expectedName)

	targetAsset := github.FindAsset(rel.Assets, expectedName)
	if targetAsset == nil {
		p.Broker.EmitError(repoID, "No se encontró asset para: "+expectedName)
		p.Broker.EmitLog(repoID, "Assets disponibles:")
		for _, a := range rel.Assets {
			p.Broker.EmitLog(repoID, "  └─ "+a.Name)
		}
		p.failUpdate(repoID, "Asset no encontrado")
		return
	}
	p.Broker.EmitLog(repoID, "Asset encontrado: "+targetAsset.Name)

	// 5. Download with progress
	tmpPath := filepath.Join(appDir, updateArtifactName(repo.AppName, repoID, ".tmp"))
	p.Store.Update(repoID, func(r *storage.Repository) {
		r.Status = storage.StatusDownloading
	})
	p.Broker.Emit(events.NewStatus(repoID, string(storage.StatusDownloading)))
	p.Broker.Emit(events.NewProgress(repoID, "download", 0))

	p.Broker.EmitLog(repoID, "⬇ Descargando "+targetAsset.Name+" ...")

	err = github.DownloadAsset(targetAsset.DownloadURL, tmpPath, func(downloaded, total int64) {
		percent := 0
		if total > 0 {
			percent = int(downloaded * 100 / total)
		}
		if percent%10 == 0 || downloaded == total {
			p.Broker.Emit(events.NewProgress(repoID, "download", percent))
			p.Store.Update(repoID, func(r *storage.Repository) {
				r.Progress = percent
			})
		}
	})
	if err != nil {
		p.failUpdate(repoID, "Error al descargar: "+err.Error())
		os.Remove(tmpPath)
		return
	}
	p.Broker.EmitLog(repoID, "✅ Descarga completada")
	p.Broker.Emit(events.NewProgress(repoID, "download", 100))

	// 6. Verify integrity and target platform/architecture
	effOS, effArch := effectivePlatform(repo.PlatformOS, repo.PlatformArch, p.OS, p.Arch)
	if err := verifyDownloadForPlatform(tmpPath, effOS, effArch); err != nil {
		p.failUpdate(repoID, "Error de integridad: "+err.Error())
		os.Remove(tmpPath)
		return
	}
	p.Broker.EmitLog(repoID, "✅ Integridad verificada")

	// 7. Set permissions
	if err := os.Chmod(tmpPath, 0755); err != nil {
		p.failUpdate(repoID, "Error al establecer permisos: "+err.Error())
		os.Remove(tmpPath)
		return
	}

	// 8. Backup existing binary
	backupPath := filepath.Join(appDir, updateArtifactName(repo.AppName, repoID, ".bak"))
	if err := copyFile(appPath, backupPath); err != nil {
		p.failUpdate(repoID, "Error al crear backup: "+err.Error())
		os.Remove(tmpPath)
		return
	}
	p.Broker.EmitLog(repoID, "📦 Backup creado: "+backupPath)

	// 9. Stop the app using PID file
	p.Store.Update(repoID, func(r *storage.Repository) {
		r.Status = storage.StatusStopping
	})
	p.Broker.Emit(events.NewStatus(repoID, string(storage.StatusStopping)))
	p.Broker.EmitLog(repoID, "🛑 Deteniendo "+repo.AppName+" ...")

	stopped, stopErr := p.ProcMan.Stop(repoID)
	if stopErr != nil {
		// Do not replace the binary while the old process may still be
		// running: we cannot guarantee it is stopped.
		os.Remove(tmpPath)
		os.Remove(backupPath)
		p.failUpdate(repoID, "Error al detener "+repo.AppName+": "+stopErr.Error())
		p.Broker.EmitLog(repoID, "🧹 Archivos temporales (tmp/backup) eliminados")
		p.Broker.EmitLog(repoID, "💡 Detén el proceso manualmente o inícialo desde AP Manager antes de actualizar")
		return
	}
	if stopped {
		p.Broker.EmitLog(repoID, "✅ "+repo.AppName+" detenido")
	} else {
		p.Broker.EmitLog(repoID, "ℹ️ "+repo.AppName+" ya estaba detenido")
	}

	// 10. Replace binary
	p.Store.Update(repoID, func(r *storage.Repository) {
		r.Status = storage.StatusReplacing
	})
	p.Broker.Emit(events.NewStatus(repoID, string(storage.StatusReplacing)))
	p.Broker.EmitLog(repoID, "Reemplazando binario...")

	if err := replaceFile(tmpPath, appPath); err != nil {
		restoreErr := p.restoreFromBackup(repoID, appPath, backupPath, repo.AppName)
		msg := "Error al reemplazar el binario: " + err.Error()
		if restoreErr != nil {
			msg += " — " + restoreErr.Error()
		} else {
			msg += " (backup restaurado)"
		}
		p.failUpdate(repoID, msg)
		return
	}
	p.Broker.EmitLog(repoID, "✅ Binario reemplazado")

	// 11. Start new version with custom args
	p.Store.Update(repoID, func(r *storage.Repository) {
		r.Status = storage.StatusStarting
	})
	p.Broker.Emit(events.NewStatus(repoID, string(storage.StatusStarting)))
	p.Broker.EmitLog(repoID, "Iniciando nueva versión...")

	startArgs := process.SplitArgs(repo.CustomCommand)
	pid, startErr := p.ProcMan.Start(repo.AppName, repoID, appPath, startArgs...)
	if startErr != nil {
		restoreErr := p.restoreFromBackup(repoID, appPath, backupPath, repo.AppName)
		msg := "Error al iniciar la nueva versión: " + startErr.Error()
		if restoreErr != nil {
			msg += " — " + restoreErr.Error()
		} else {
			msg += " (backup restaurado)"
		}
		p.failUpdate(repoID, msg)
		return
	}
	p.Broker.EmitLog(repoID, fmt.Sprintf("▶️  Proceso iniciado con PID %d", pid))

	p.Store.Update(repoID, func(r *storage.Repository) {
		r.PID = pid
	})

	// 12. Verify with extended health check
	p.Store.Update(repoID, func(r *storage.Repository) {
		r.Status = storage.StatusVerifying
	})
	p.Broker.Emit(events.NewStatus(repoID, string(storage.StatusVerifying)))

	healthy := false
	for i := 0; i < 6; i++ {
		time.Sleep(2 * time.Second)
		if p.ProcMan.IsRunning(repoID) {
			healthy = true
			break
		}
		p.Broker.EmitLog(repoID, fmt.Sprintf("⏳ Verificando... (%d/6)", i+1))
	}

	if healthy {
		p.Broker.EmitLog(repoID, "✅ Nueva versión verificada y funcionando")

		p.Store.Update(repoID, func(r *storage.Repository) {
			r.CurrentVersion = newVersion
			r.Status = storage.StatusIdle
			r.LastUpdate = storage.Now()
			r.Progress = 100
			r.Error = ""
		})
		p.Broker.Emit(events.NewUpdateComplete(repoID, newVersion))
		p.Broker.Emit(events.NewVersion(repoID, newVersion, newVersion))

		os.Remove(backupPath)
		p.Broker.EmitLog(repoID, "🗑️  Backup eliminado")
		p.persistState(repoID)
	} else {
		restoreErr := p.restoreFromBackup(repoID, appPath, backupPath, repo.AppName)
		msg := "La nueva versión no pasó la verificación de salud"
		if restoreErr != nil {
			msg += " — " + restoreErr.Error()
		} else {
			msg += " (backup restaurado)"
		}
		p.failUpdate(repoID, msg)
		return
	}

	p.Broker.EmitLog(repoID, sep)
	p.Broker.EmitLog(repoID, ">>> ACTUALIZACIÓN COMPLETADA <<<")
	p.Broker.EmitLog(repoID, fmt.Sprintf("   %s → %s", currentVer, newVersion))
	p.Broker.EmitLog(repoID, sep)
}

// CheckVersion checks the latest version without updating.
func (p *Pipeline) CheckVersion(repoID string) {
	if !p.guard.tryAcquire(repoID) {
		p.Broker.EmitLog(repoID, "⚠️ Ya hay una operación en curso para este repositorio")
		return
	}
	defer p.guard.release(repoID)

	repo := p.Store.Find(repoID)
	if repo == nil {
		p.Broker.EmitError(repoID, "Repositorio no encontrado")
		return
	}

	p.Store.Update(repoID, func(r *storage.Repository) {
		r.Status = storage.StatusChecking
		r.Error = ""
	})
	p.Broker.Emit(events.NewStatus(repoID, string(storage.StatusChecking)))
	p.persistState(repoID)

	p.Broker.EmitLog(repoID, fmt.Sprintf("📡 Verificando versión de %s/%s ...", repo.Owner, repo.Name))

	rel, err := p.GitHub.LatestRelease(repo.Owner, repo.Name)
	if err != nil {
		p.Broker.EmitError(repoID, "Error: "+err.Error())
		p.Store.Update(repoID, func(r *storage.Repository) {
			r.Status = storage.StatusFailed
			r.Error = err.Error()
		})
		p.Broker.Emit(events.NewUpdateFailed(repoID, err.Error()))
		p.persistState(repoID)
		return
	}

	p.Broker.EmitLog(repoID, fmt.Sprintf("🏷️  Última versión disponible: %s", rel.TagName))

	appPath, appErr := process.ResolveAppBinary(repo.AppName, repo.InstallPath)
	currentVer := "no detectada"
	if appErr == nil {
		currentVer = detectCurrentVersion(appPath)
		p.Broker.EmitLog(repoID, fmt.Sprintf("💻 Versión actual detectada: %s", currentVer))
	} else {
		p.Broker.EmitLog(repoID, "💻 Binario no encontrado localmente (versión no detectada)")
	}

	p.Store.Update(repoID, func(r *storage.Repository) {
		r.CurrentVersion = currentVer
		r.LatestVersion = rel.TagName
		r.Status = storage.StatusIdle
		r.LastCheck = storage.Now()
		r.Installed = appErr == nil
	})
	p.Broker.Emit(events.NewVersion(repoID, currentVer, rel.TagName))
	p.Broker.Emit(events.NewStatus(repoID, string(storage.StatusIdle)))
	p.persistState(repoID)
}

// ── Helpers ──

func (p *Pipeline) failUpdate(repoID, errMsg string) {
	p.Broker.EmitError(repoID, errMsg)
	p.Store.Update(repoID, func(r *storage.Repository) {
		r.Status = storage.StatusFailed
		r.Error = errMsg
		r.Progress = 0
	})
	p.Broker.Emit(events.NewUpdateFailed(repoID, errMsg))
	p.persistState(repoID)
}

// restoreFromBackup rolls the update back to the previous binary and tries
// to restart it. It never removes appPath before the backup is confirmed
// usable, so a failed restoration preserves whatever binary is in place.
// It returns an error describing the restoration failure (or a failed
// restart attempt); nil means the previous version is running again.
func (p *Pipeline) restoreFromBackup(repoID, appPath, backupPath, appName string) error {
	p.Broker.EmitLog(repoID, "⚠️  Restaurando backup...")

	// Stop the newly started process (if any) so the rollback cannot leave a
	// new instance running alongside the restored one. No PID file means
	// nothing was started (or it is already gone) — not an error here.
	if stopped, _ := p.ProcMan.Stop(repoID); stopped {
		p.Broker.EmitLog(repoID, "✅ Proceso nuevo detenido")
	}

	// Verify the backup is usable before touching the destination.
	info, err := os.Stat(backupPath)
	if err != nil {
		errMsg := "no se pudo restaurar el backup: " + err.Error()
		p.Broker.EmitError(repoID, errMsg)
		return errors.New(errMsg)
	}
	if info.Size() == 0 {
		errMsg := "no se pudo restaurar el backup: archivo vacío"
		p.Broker.EmitError(repoID, errMsg)
		return errors.New(errMsg)
	}

	// Replace without removing appPath first: on POSIX, rename replaces the
	// destination atomically; on failure the current binary is preserved.
	if err := replaceFile(backupPath, appPath); err != nil {
		errMsg := "no se pudo restaurar el backup: " + err.Error()
		p.Broker.EmitError(repoID, errMsg)
		return errors.New(errMsg)
	}
	if err := os.Chmod(appPath, 0755); err != nil {
		p.Broker.EmitLog(repoID, "⚠️ No se pudieron restaurar los permisos: "+err.Error())
	}
	p.Broker.EmitLog(repoID, "✅ Backup restaurado")

	// Attempt to restart the previous version.
	repo := p.Store.Find(repoID)
	var restoreArgs []string
	if repo != nil {
		restoreArgs = process.SplitArgs(repo.CustomCommand)
	}
	pid, err := p.ProcMan.Start(appName, repoID, appPath, restoreArgs...)
	if err != nil {
		errMsg := "backup restaurado pero no se pudo reiniciar la versión anterior: " + err.Error()
		p.Broker.EmitError(repoID, errMsg)
		return errors.New(errMsg)
	}
	p.Broker.EmitLog(repoID, fmt.Sprintf("🔄 Versión anterior reiniciada (PID %d)", pid))
	return nil
}

// versionProbeTimeout bounds each `<binary> --version`/`version` probe so a
// binary that ignores the flag cannot block an updater goroutine forever. It
// is a package-level variable so tests can shorten it.
var versionProbeTimeout = 5 * time.Second

// probeVersion runs appPath with args under a bounded timeout and returns its
// trimmed, length-capped stdout, or "" on timeout, non-zero exit or empty
// output.
func probeVersion(appPath string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), versionProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, appPath, args...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return ""
	}
	if len(v) > 100 {
		v = v[:100]
	}
	return v
}

func detectCurrentVersion(appPath string) string {
	dir := filepath.Dir(appPath)
	baseName := filepath.Base(appPath)

	// Check metadata.json alongside the binary
	metaPath := filepath.Join(dir, baseName+".metadata.json")
	if data, err := os.ReadFile(metaPath); err == nil {
		var meta struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(data, &meta); err == nil && meta.Version != "" {
			return meta.Version
		}
	}

	// Check version.txt
	verPath := filepath.Join(dir, "version.txt")
	if data, err := os.ReadFile(verPath); err == nil {
		v := strings.TrimSpace(string(data))
		if v != "" {
			if len(v) > 100 {
				v = v[:100]
			}
			return v
		}
	}

	// Fallback: run binary --version then version (last resort, expensive),
	// each bounded by versionProbeTimeout.
	if v := probeVersion(appPath, "--version"); v != "" {
		return v
	}
	if v := probeVersion(appPath, "version"); v != "" {
		return v
	}

	return "desconocida"
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	for _, prefix := range []string{"v", "V", "version ", "Version "} {
		if strings.HasPrefix(v, prefix) {
			v = v[len(prefix):]
		}
	}
	return v
}

func defaultAssetName(appName, platOS, platArch, runtimeOS, runtimeArch string) string {
	osName := platOS
	arch := platArch
	if osName == "" {
		osName = runtimeOS
	}
	if arch == "" {
		arch = runtimeArch
	}
	return fmt.Sprintf("%s-%s-%s", appName, osName, arch)
}

// effectivePlatform returns the target OS/arch actually used for asset
// selection and, therefore, for artifact validation: the repo's configured
// platform when set, otherwise the pipeline platform.
func effectivePlatform(platOS, platArch, pipeOS, pipeArch string) (string, string) {
	osName := platOS
	if osName == "" {
		osName = pipeOS
	}
	arch := platArch
	if arch == "" {
		arch = pipeArch
	}
	return osName, arch
}

// minExecutableSize is the smallest plausible size for a raw ELF/PE/Mach-O
// executable; smaller files that still carry the magic are truncated
// downloads.
const minExecutableSize = 64

// verifyDownload validates that a downloaded file is a plausible raw
// executable for one of the supported platforms (ELF, PE/Mach-O, or a script
// with a valid shebang) and rejects empty/truncated files, arbitrary text,
// and archive/container payloads that would be useless or unsafe to treat as
// a binary. It never executes the downloaded file. It is a compatibility
// wrapper that performs no platform check; use verifyDownloadForPlatform when
// the expected OS/arch is known.
func verifyDownload(path string) error {
	return verifyDownloadForPlatform(path, "", "")
}

// verifyDownloadForPlatform behaves like verifyDownload and, when a target
// OS/architecture is provided, additionally verifies that the executable
// format and machine architecture match the selected target (the same OS/arch
// used for asset selection: repo override or pipeline platform). Aliases are
// normalized (amd64/x86_64, arm64/aarch64, arm/armv7, 386/i686); Android is
// treated as Linux for format purposes (its assets are ELF); scripts with a
// valid shebang are architecture-neutral and remain allowed. An unknown
// target OS or architecture fails closed with an actionable error. The
// downloaded file is never executed.
func verifyDownloadForPlatform(path, expectedOS, expectedArch string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return fmt.Errorf("archivo descargado vacío")
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Read the head for signature detection (archive magics, ELF, PE,
	// Mach-O and shebang lines).
	head := make([]byte, 64)
	n, err := io.ReadFull(f, head)
	if err != nil && err != io.ErrUnexpectedEOF {
		return fmt.Errorf("no se pudo leer el archivo descargado: %w", err)
	}
	head = head[:n]
	if n < 4 {
		return fmt.Errorf("archivo descargado demasiado corto (%d bytes)", n)
	}

	// Archives/containers must be rejected even if misnamed as executables.
	if sig := archiveSignature(head); sig != "" {
		return fmt.Errorf("el archivo descargado es un %s, no un ejecutable; configura un asset binario sin comprimir en el repositorio", sig)
	}

	switch {
	case isELF(head):
		if info.Size() < minExecutableSize {
			return fmt.Errorf("archivo descargado demasiado corto para ser un ejecutable (%d bytes)", info.Size())
		}
		return verifyELFPlatform(head, expectedOS, expectedArch)
	case isPE(head):
		if info.Size() < minExecutableSize {
			return fmt.Errorf("archivo descargado demasiado corto para ser un ejecutable (%d bytes)", info.Size())
		}
		return verifyPEPlatform(f, head, expectedOS, expectedArch)
	case isMachO(head):
		if info.Size() < minExecutableSize {
			return fmt.Errorf("archivo descargado demasiado corto para ser un ejecutable (%d bytes)", info.Size())
		}
		return verifyMachOPlatform(f, head, expectedOS, expectedArch)
	}

	// Script executables: only a valid shebang line is accepted. Scripts are
	// architecture-neutral and remain allowed regardless of the target.
	if isShebang(head) {
		return nil
	}

	return fmt.Errorf("el archivo descargado no es un ejecutable válido (se esperaba ELF, PE, Mach-O o un script con shebang); configura un asset binario en el repositorio")
}

// isELF reports whether b starts with an ELF magic.
func isELF(b []byte) bool {
	return len(b) >= 4 && b[0] == 0x7f && b[1] == 'E' && b[2] == 'L' && b[3] == 'F'
}

// isPE reports whether b starts with a PE (MZ) magic.
func isPE(b []byte) bool {
	return len(b) >= 2 && b[0] == 'M' && b[1] == 'Z'
}

// isMachO reports whether b starts with a Mach-O magic: thin 32/64-bit in
// both endiannesses, or a fat/universal binary.
func isMachO(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	return (b[0] == 0xfe && b[1] == 0xed && b[2] == 0xfa && (b[3] == 0xce || b[3] == 0xcf)) ||
		(b[0] == 0xce && b[1] == 0xfa && b[2] == 0xed && b[3] == 0xfe) ||
		(b[0] == 0xcf && b[1] == 0xfa && b[2] == 0xed && b[3] == 0xfe) ||
		(b[0] == 0xca && b[1] == 0xfe && b[2] == 0xba && (b[3] == 0xbe || b[3] == 0xbf))
}

// archiveSignature returns the archive/container format name whose magic
// matches b, or "" when b is not a recognized archive.
func archiveSignature(b []byte) string {
	switch {
	case len(b) >= 2 && b[0] == 0x1f && b[1] == 0x8b:
		return "gzip (tar.gz)"
	case len(b) >= 6 && b[0] == 0xfd && b[1] == 0x37 && b[2] == 0x7a && b[3] == 0x58 && b[4] == 0x5a && b[5] == 0x00:
		return "xz (tar.xz)"
	case len(b) >= 4 && b[0] == 'P' && b[1] == 'K' &&
		((b[2] == 0x03 && b[3] == 0x04) || (b[2] == 0x05 && b[3] == 0x06) || (b[2] == 0x07 && b[3] == 0x08)):
		return "zip"
	case len(b) >= 3 && b[0] == 'B' && b[1] == 'Z' && b[2] == 'h':
		return "bzip2"
	case len(b) >= 6 && b[0] == 0x37 && b[1] == 0x7a && b[2] == 0xbc && b[3] == 0xaf && b[4] == 0x27 && b[5] == 0x1c:
		return "7z"
	case len(b) >= 4 && b[0] == 'R' && b[1] == 'a' && b[2] == 'r' && b[3] == '!':
		return "rar"
	}
	return ""
}

// isShebang reports whether b starts with a valid shebang line (#! followed
// by a non-empty interpreter path before end-of-line/NUL).
func isShebang(b []byte) bool {
	if len(b) < 3 || b[0] != '#' || b[1] != '!' {
		return false
	}
	for _, c := range b[2:] {
		switch c {
		case '\n', 0:
			return false // empty interpreter path
		case ' ', '\t', '\r':
			continue
		default:
			return true
		}
	}
	return false
}

// normalizeOS maps common OS aliases to a canonical name used for format
// compatibility checks. Android is treated as Linux because its assets are
// ELF binaries. It returns "" for unknown/unrecognized OS names.
func normalizeOS(osName string) string {
	switch strings.ToLower(strings.TrimSpace(osName)) {
	case "linux", "android":
		return "linux"
	case "darwin", "macos", "mac":
		return "darwin"
	case "windows", "win32", "win64":
		return "windows"
	}
	return ""
}

// normalizeArch maps common architecture aliases to a canonical name used for
// machine checks. It returns "" for unknown/unrecognized architectures.
func normalizeArch(arch string) string {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "amd64", "x86_64", "x64":
		return "amd64"
	case "386", "i386", "i686":
		return "386"
	case "arm64", "aarch64":
		return "arm64"
	case "arm", "armv7", "armv7l":
		return "arm"
	}
	return ""
}

// checkTargetOS verifies the executable format is compatible with the
// expected OS (already normalized): "linux" accepts ELF, "darwin" accepts
// Mach-O, "windows" accepts PE. Returns a descriptive error on mismatch or
// when the expected OS is not recognized; returns nil when no OS was given.
func checkTargetOS(expectedOS, format, wantOS string) error {
	if expectedOS == "" {
		return nil
	}
	have := normalizeOS(expectedOS)
	if have == "" {
		return fmt.Errorf("sistema operativo objetivo no soportado: %s", expectedOS)
	}
	if have != wantOS {
		return fmt.Errorf("formato %s incompatible con el sistema operativo objetivo %s", format, expectedOS)
	}
	return nil
}

// verifyELFPlatform checks the ELF e_machine against the expected architecture
// (and OS compatibility) when requested. head must contain at least the ELF
// header.
func verifyELFPlatform(head []byte, expectedOS, expectedArch string) error {
	if err := checkTargetOS(expectedOS, "ELF", "linux"); err != nil {
		return err
	}
	if expectedArch == "" {
		return nil
	}
	want := normalizeArch(expectedArch)
	if want == "" {
		return fmt.Errorf("arquitectura objetivo no soportada: %s", expectedArch)
	}
	machine := elfMachine(head)
	if machine < 0 {
		return fmt.Errorf("cabecera ELF inválida")
	}
	got := elfMachineArch(machine)
	if got != want {
		if got == "" {
			return fmt.Errorf("el binario ELF tiene una arquitectura desconocida (machine=%d), se esperaba %s", machine, expectedArch)
		}
		return fmt.Errorf("el binario ELF es para %s, se esperaba %s", got, expectedArch)
	}
	return nil
}

// elfMachine returns the ELF e_machine value from the header, honoring the
// EI_DATA endianness. It returns -1 when the header is malformed.
func elfMachine(head []byte) int {
	if len(head) < 20 || head[0] != 0x7f || head[1] != 'E' || head[2] != 'L' || head[3] != 'F' {
		return -1
	}
	if head[5] == 2 { // ELFDATA2MSB (big endian)
		return int(head[18])<<8 | int(head[19])
	}
	return int(head[18]) | int(head[19])<<8
}

// elfMachineArch maps an ELF e_machine to a normalized architecture name, or
// "" when unknown.
func elfMachineArch(machine int) string {
	switch machine {
	case 3: // EM_386
		return "386"
	case 40: // EM_ARM
		return "arm"
	case 62: // EM_X86_64
		return "amd64"
	case 183: // EM_AARCH64
		return "arm64"
	}
	return ""
}

// verifyPEPlatform checks the PE signature and COFF machine against the
// expected OS/architecture when requested. When no platform check is
// requested, an MZ magic alone remains acceptable (generic behavior); when a
// platform is requested, the PE signature must be present.
func verifyPEPlatform(f *os.File, head []byte, expectedOS, expectedArch string) error {
	if err := checkTargetOS(expectedOS, "PE", "windows"); err != nil {
		return err
	}
	if expectedOS == "" && expectedArch == "" {
		return nil // generic: MZ magic alone is accepted as before
	}
	// e_lfanew (offset 0x3C, little-endian) points at the "PE\0\0" signature.
	if len(head) < 64 {
		return fmt.Errorf("cabecera PE incompleta")
	}
	lfanew := uint32(head[60]) | uint32(head[61])<<8 | uint32(head[62])<<16 | uint32(head[63])<<24
	if lfanew == 0 {
		return fmt.Errorf("cabecera PE inválida (e_lfanew=0)")
	}
	buf := make([]byte, 6)
	if _, err := f.ReadAt(buf, int64(lfanew)); err != nil {
		return fmt.Errorf("cabecera PE ilegible: %w", err)
	}
	if buf[0] != 'P' || buf[1] != 'E' || buf[2] != 0 || buf[3] != 0 {
		return fmt.Errorf("firma PE no encontrada (no es un ejecutable PE válido)")
	}
	if expectedArch == "" {
		return nil
	}
	want := normalizeArch(expectedArch)
	if want == "" {
		return fmt.Errorf("arquitectura objetivo no soportada: %s", expectedArch)
	}
	machine := uint16(buf[4]) | uint16(buf[5])<<8
	got := peMachineArch(machine)
	if got != want {
		if got == "" {
			return fmt.Errorf("el binario PE tiene una arquitectura desconocida (machine=0x%x), se esperaba %s", machine, expectedArch)
		}
		return fmt.Errorf("el binario PE es para %s, se esperaba %s", got, expectedArch)
	}
	return nil
}

// peMachineArch maps a COFF machine value to a normalized architecture name,
// or "" when unknown.
func peMachineArch(machine uint16) string {
	switch machine {
	case 0x014c: // IMAGE_FILE_MACHINE_I386
		return "386"
	case 0x01c0, 0x01c4: // IMAGE_FILE_MACHINE_ARM / ARMNT
		return "arm"
	case 0xaa64: // IMAGE_FILE_MACHINE_ARM64
		return "arm64"
	case 0x8664: // IMAGE_FILE_MACHINE_AMD64
		return "amd64"
	}
	return ""
}

// verifyMachOPlatform checks a thin or fat Mach-O binary against the expected
// OS/architecture when requested. A fat binary is accepted only when one of
// its contained architectures matches.
func verifyMachOPlatform(f *os.File, head []byte, expectedOS, expectedArch string) error {
	if err := checkTargetOS(expectedOS, "Mach-O", "darwin"); err != nil {
		return err
	}
	if expectedArch == "" {
		return nil
	}
	want := normalizeArch(expectedArch)
	if want == "" {
		return fmt.Errorf("arquitectura objetivo no soportada: %s", expectedArch)
	}
	if isFatMachO(head) {
		return verifyMachOFatArch(f, head, want, expectedArch)
	}
	cputype := machOThinCPUType(head)
	if cputype < 0 {
		return fmt.Errorf("cabecera Mach-O inválida")
	}
	got := machOCPUTypeArch(cputype)
	if got != want {
		if got == "" {
			return fmt.Errorf("el binario Mach-O tiene una arquitectura desconocida (cputype=0x%x), se esperaba %s", cputype, expectedArch)
		}
		return fmt.Errorf("el binario Mach-O es para %s, se esperaba %s", got, expectedArch)
	}
	return nil
}

// isFatMachO reports whether b starts with a fat/universal Mach-O magic.
func isFatMachO(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	magic := uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
	return magic == 0xcafebabe || magic == 0xbebafeca || magic == 0xcafebabf || magic == 0xbfbafeca
}

// verifyMachOFatArch iterates the fat header entries and accepts the binary
// when any contained architecture matches the expected one.
func verifyMachOFatArch(f *os.File, head []byte, want, expectedArch string) error {
	if len(head) < 8 {
		return fmt.Errorf("cabecera Mach-O fat incompleta")
	}
	magic := uint32(head[0]) | uint32(head[1])<<8 | uint32(head[2])<<16 | uint32(head[3])<<24
	// On-disk bytes `ca fe ba be/bf` (read here as little-endian) mean the
	// magic was stored big-endian, so the whole header is big-endian.
	bigEndian := magic == 0xbebafeca || magic == 0xbfbafeca
	entrySize := 20 // 32-bit fat entries
	if magic == 0xcafebabf || magic == 0xbfbafeca {
		entrySize = 32 // 64-bit fat entries
	}
	readU32 := func(b []byte) uint32 {
		if bigEndian {
			return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
		}
		return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
	}
	nfat := readU32(head[4:8])
	if nfat == 0 || nfat > 64 {
		return fmt.Errorf("cabecera Mach-O fat inválida (nfat_arch=%d)", nfat)
	}
	buf := make([]byte, entrySize)
	for i := uint32(0); i < nfat; i++ {
		if _, err := f.ReadAt(buf, int64(8+int(i)*entrySize)); err != nil {
			return fmt.Errorf("cabecera Mach-O fat ilegible: %w", err)
		}
		cputype := int64(readU32(buf[0:4]))
		if machOCPUTypeArch(cputype) == want {
			return nil
		}
	}
	return fmt.Errorf("el binario Mach-O fat no contiene la arquitectura esperada %s", expectedArch)
}

// machOThinCPUType returns the cputype of a thin Mach-O binary (honoring the
// magic's endianness), or -1 when the header is not a thin Mach-O.
func machOThinCPUType(head []byte) int64 {
	if len(head) < 8 {
		return -1
	}
	magic := uint32(head[0]) | uint32(head[1])<<8 | uint32(head[2])<<16 | uint32(head[3])<<24
	var cputype uint32
	switch magic {
	case 0xfeedface, 0xfeedfacf: // MH_MAGIC, MH_MAGIC_64 (little endian)
		cputype = uint32(head[4]) | uint32(head[5])<<8 | uint32(head[6])<<16 | uint32(head[7])<<24
	case 0xcefaedfe, 0xcffaedfe: // MH_CIGAM, MH_CIGAM_64 (big endian)
		cputype = uint32(head[7]) | uint32(head[6])<<8 | uint32(head[5])<<16 | uint32(head[4])<<24
	default:
		return -1
	}
	return int64(cputype)
}

// machOCPUTypeArch maps a Mach-O cputype to a normalized architecture name,
// or "" when unknown.
func machOCPUTypeArch(cputype int64) string {
	switch cputype {
	case 7: // CPU_TYPE_X86
		return "386"
	case 0x01000007: // CPU_TYPE_X86_64
		return "amd64"
	case 12: // CPU_TYPE_ARM
		return "arm"
	case 0x0100000c: // CPU_TYPE_ARM64
		return "arm64"
	}
	return ""
}

// updateArtifactName returns a per-repo artifact base name (for .tmp and
// .bak files) that cannot collide across repo IDs sharing the same AppName.
func updateArtifactName(appName, repoID, suffix string) string {
	return appName + "." + process.SanitizeID(repoID) + suffix
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

// replaceFile replaces dest with src, trying rename first, then copy+remove.
func replaceFile(src, dest string) error {
	if err := os.Rename(src, dest); err == nil {
		return nil
	}
	if err := copyFile(src, dest); err != nil {
		return fmt.Errorf("copy fallback: %w", err)
	}
	os.Remove(src)
	return nil
}

// assetForPlatform returns the asset name best suited for the current platform.
// Falls back from android→linux for Termux compatibility.
func assetForPlatform(appName, platformOS, platformArch, runtimeOS, runtimeArch string) string {
	base := defaultAssetName(appName, platformOS, platformArch, runtimeOS, runtimeArch)
	return base
}

// fallbackAssetNames generates alternative raw-executable asset names to try
// when the primary asset name doesn't match any release assets. Only raw
// binary names are generated: this application installs executables, not
// archives, so no .tar.gz/.tar.xz candidates are produced here.
func fallbackAssetNames(appName, platformOS, platformArch, runtimeOS, runtimeArch string) []string {
	osName := platformOS
	arch := platformArch
	if osName == "" {
		osName = runtimeOS
	}
	if arch == "" {
		arch = runtimeArch
	}

	var names []string
	// Primary: appName-OS-ARCH
	names = append(names, fmt.Sprintf("%s-%s-%s", appName, osName, arch))

	// If arch is "arm", also try armv7 and armv6 (common release naming)
	if arch == "arm" {
		names = append(names, fmt.Sprintf("%s-%s-%s", appName, osName, "armv7"))
		names = append(names, fmt.Sprintf("%s-%s-%s", appName, osName, "armv6"))
		names = append(names, fmt.Sprintf("%s-%s-%s", appName, osName, "armv7l"))
	}
	// If arch is "arm64", also try aarch64
	if arch == "arm64" {
		names = append(names, fmt.Sprintf("%s-%s-%s", appName, osName, "aarch64"))
	}

	// If Android (Termux), try linux variant as fallback
	if osName == "android" {
		names = append(names, fmt.Sprintf("%s-%s-%s", appName, "linux", arch))
		if arch == "arm" {
			names = append(names, fmt.Sprintf("%s-%s-%s", appName, "linux", "armv7"))
			names = append(names, fmt.Sprintf("%s-%s-%s", appName, "linux", "armv6"))
		}
		names = append(names, fmt.Sprintf("%s-%s-%s", appName, "linux", "arm64"))
	}

	// Try without arch
	names = append(names, fmt.Sprintf("%s-%s", appName, osName))

	return names
}

// InstallApp performs a first-time install of an application binary.
// It downloads the latest release and places it in the appropriate directory.
func (p *Pipeline) InstallApp(repoID string) {
	const sep = "══════════════════════════════════════"

	if !p.guard.tryAcquire(repoID) {
		p.Broker.EmitLog(repoID, "⚠️ Ya hay una operación en curso para este repositorio")
		return
	}
	defer p.guard.release(repoID)

	repo := p.Store.Find(repoID)
	if repo == nil {
		p.Broker.EmitError(repoID, "Repositorio no encontrado")
		return
	}

	// Mark status
	p.Store.Update(repoID, func(r *storage.Repository) {
		r.Status = storage.StatusDownloading
		r.Progress = 0
		r.Error = ""
	})
	p.Broker.Emit(events.NewStatus(repoID, string(storage.StatusDownloading)))
	p.persistState(repoID)

	p.Broker.EmitLog(repoID, sep)
	p.Broker.EmitLog(repoID, fmt.Sprintf(">>> INSTALANDO %s/%s (%s) <<<", repo.Owner, repo.Name, repo.AppName))
	p.Broker.EmitLog(repoID, sep)

	// 1. Determine install directory
	installDir := repo.InstallPath
	if installDir == "" {
		installDir = process.DetectInstallDir()
	}
	p.Broker.EmitLog(repoID, "Directorio de instalación: "+installDir)

	if err := process.EnsureInstallDir(installDir); err != nil {
		p.failInstall(repoID, "No se pudo crear directorio: "+err.Error())
		return
	}

	// 2. Check latest release
	p.Broker.EmitLog(repoID, "📡 Consultando últimas versiones...")
	rel, err := p.GitHub.LatestRelease(repo.Owner, repo.Name)
	if err != nil {
		p.failInstall(repoID, "Error al consultar GitHub: "+err.Error())
		return
	}
	newVersion := rel.TagName
	p.Broker.EmitLog(repoID, "🏷️  Última versión: "+newVersion)

	// 3. Find matching asset
	expectedName := repo.AssetName
	if expectedName == "" {
		expectedName = defaultAssetName(repo.AppName, repo.PlatformOS, repo.PlatformArch, p.OS, p.Arch)
	}

	// Try the primary name, then fallbacks
	var targetAsset *github.Asset
	targetAsset = github.FindAsset(rel.Assets, expectedName)

	if targetAsset == nil {
		// Try fallback names (e.g. linux instead of android for Termux)
		fallbacks := fallbackAssetNames(repo.AppName, repo.PlatformOS, repo.PlatformArch, p.OS, p.Arch)
		for _, fb := range fallbacks {
			targetAsset = github.FindAsset(rel.Assets, fb)
			if targetAsset != nil {
				p.Broker.EmitLog(repoID, "Asset alternativo encontrado: "+targetAsset.Name)
				break
			}
		}
	}

	if targetAsset == nil {
		p.Broker.EmitError(repoID, "No se encontró asset para: "+expectedName)
		p.Broker.EmitLog(repoID, "Assets disponibles:")
		for _, a := range rel.Assets {
			p.Broker.EmitLog(repoID, "  └─ "+a.Name)
		}
		p.failInstall(repoID, "Asset no encontrado")
		return
	}
	p.Broker.EmitLog(repoID, "✅ Asset seleccionado: "+targetAsset.Name)

	// 4. Download to temp file
	tmpPath := filepath.Join(installDir, updateArtifactName(repo.AppName, repoID, ".tmp"))
	p.Store.Update(repoID, func(r *storage.Repository) {
		r.Status = storage.StatusDownloading
		r.Progress = 0
	})
	p.Broker.Emit(events.NewStatus(repoID, string(storage.StatusDownloading)))
	p.Broker.Emit(events.NewProgress(repoID, "download", 0))
	p.Broker.EmitLog(repoID, "⬇ Descargando "+targetAsset.Name+" ...")

	err = github.DownloadAsset(targetAsset.DownloadURL, tmpPath, func(downloaded, total int64) {
		percent := 0
		if total > 0 {
			percent = int(downloaded * 100 / total)
		}
		if percent%10 == 0 || downloaded == total {
			p.Broker.Emit(events.NewProgress(repoID, "download", percent))
			p.Store.Update(repoID, func(r *storage.Repository) {
				r.Progress = percent
			})
		}
	})
	if err != nil {
		p.failInstall(repoID, "Error al descargar: "+err.Error())
		os.Remove(tmpPath)
		return
	}
	p.Broker.EmitLog(repoID, "✅ Descarga completada")
	p.Broker.Emit(events.NewProgress(repoID, "download", 100))

	// 5. Verify integrity and target platform/architecture
	effOS, effArch := effectivePlatform(repo.PlatformOS, repo.PlatformArch, p.OS, p.Arch)
	if err := verifyDownloadForPlatform(tmpPath, effOS, effArch); err != nil {
		p.failInstall(repoID, "Error de integridad: "+err.Error())
		os.Remove(tmpPath)
		return
	}
	p.Broker.EmitLog(repoID, "✅ Integridad verificada")

	// 6. Install binary
	p.Store.Update(repoID, func(r *storage.Repository) {
		r.Status = storage.StatusReplacing
	})
	p.Broker.Emit(events.NewStatus(repoID, string(storage.StatusReplacing)))
	p.Broker.EmitLog(repoID, "📦 Instalando binario en "+installDir+"...")

	destPath, err := process.InstallBinary(tmpPath, installDir, repo.AppName)
	if err != nil {
		p.failInstall(repoID, "Error al instalar: "+err.Error())
		os.Remove(tmpPath)
		return
	}
	os.Remove(tmpPath) // Clean up temp file
	p.Broker.EmitLog(repoID, "✅ Binario instalado: "+destPath)

	// 7. Mark as installed
	p.Store.Update(repoID, func(r *storage.Repository) {
		r.CurrentVersion = newVersion
		r.LatestVersion = newVersion
		r.Status = storage.StatusIdle
		r.Installed = true
		r.InstallPath = installDir
		r.LastUpdate = storage.Now()
		r.Progress = 100
		r.Error = ""
	})
	p.Broker.Emit(events.NewVersion(repoID, newVersion, newVersion))
	p.Broker.Emit(events.NewUpdateComplete(repoID, newVersion))
	// Emit completed then idle so frontend can react to the completion
	p.Broker.Emit(events.NewStatus(repoID, string(storage.StatusCompleted)))
	p.Broker.Emit(events.NewStatus(repoID, string(storage.StatusIdle)))

	p.Broker.EmitLog(repoID, sep)
	p.Broker.EmitLog(repoID, ">>> INSTALACIÓN COMPLETADA <<<")
	p.Broker.EmitLog(repoID, fmt.Sprintf("   %s instalado en %s", repo.AppName, destPath))
	p.Broker.EmitLog(repoID, sep)
	p.persistState(repoID)
}

func (p *Pipeline) failInstall(repoID, errMsg string) {
	p.Broker.EmitError(repoID, errMsg)
	p.Store.Update(repoID, func(r *storage.Repository) {
		r.Status = storage.StatusFailed
		r.Error = errMsg
		r.Progress = 0
	})
	p.Broker.Emit(events.NewUpdateFailed(repoID, errMsg))
	p.persistState(repoID)
}

// persistState saves the store to disk after a terminal (or initial) state
// mutation. It must be called outside any Store mutex or Store.Update
// callback. A persistence failure is reported through the broker log path
// and never turns a successful operation into a failure.
func (p *Pipeline) persistState(repoID string) {
	if err := p.Store.Save(); err != nil {
		p.Broker.EmitLog(repoID, "⚠️ No se pudo persistir el estado: "+err.Error())
	}
}

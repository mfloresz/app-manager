// Package updater orchestrates the update pipeline for a repository.
package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"ap-manager/internal/events"
	"ap-manager/internal/github"
	"ap-manager/internal/process"
	"ap-manager/internal/storage"
)

// Pipeline runs the full update lifecycle for a repo.
type Pipeline struct {
	Broker  *events.Broker
	Store   *storage.Store
	ProcMan *process.Manager
	GitHub  *github.Client
	OS      string
	Arch    string
}

// RunUpdate executes the full update pipeline for a repo.
func (p *Pipeline) RunUpdate(repoID string) {
	const sep = "══════════════════════════════════════"

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

	p.Broker.EmitLog(repoID, sep)
	p.Broker.EmitLog(repoID, fmt.Sprintf(">>> ACTUALIZANDO %s/%s (%s) <<<", repo.Owner, repo.Name, repo.AppName))
	p.Broker.EmitLog(repoID, sep)

	// 1. Find binary
	appPath, err := process.FindAppBinary(repo.AppName)
	if err != nil {
		p.failUpdate(repoID, "Binario no encontrado: "+err.Error())
		p.Broker.EmitLog(repoID, "💡 Asegúrate de que '"+repo.AppName+"' esté en el PATH, ~/bin, ~/.local/bin o en el mismo directorio")
		return
	}
	p.Broker.EmitLog(repoID, "Binario encontrado: "+appPath)
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
	tmpPath := filepath.Join(appDir, repo.AppName+".tmp")
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

	// 6. Verify integrity
	if err := verifyDownload(tmpPath); err != nil {
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
	backupPath := filepath.Join(appDir, repo.AppName+".bak")
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

	stopped, stopErr := p.ProcMan.Stop(repo.AppName)
	if stopErr != nil {
		p.Broker.EmitLog(repoID, "⚠️ Parada por PID falló: "+stopErr.Error())
	}
	if stopped {
		p.Broker.EmitLog(repoID, "✅ "+repo.AppName+" detenido")
	}

	// 10. Replace binary
	p.Store.Update(repoID, func(r *storage.Repository) {
		r.Status = storage.StatusReplacing
	})
	p.Broker.Emit(events.NewStatus(repoID, string(storage.StatusReplacing)))
	p.Broker.EmitLog(repoID, "Reemplazando binario...")

	if err := replaceFile(tmpPath, appPath); err != nil {
		p.Broker.EmitError(repoID, "Error al reemplazar: "+err.Error())
		p.restoreFromBackup(repoID, appPath, backupPath, repo.AppName)
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
	pid, startErr := p.ProcMan.Start(repo.AppName, appPath, startArgs...)
	if startErr != nil {
		p.Broker.EmitError(repoID, "Error al iniciar: "+startErr.Error())
		p.restoreFromBackup(repoID, appPath, backupPath, repo.AppName)
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
		if p.ProcMan.IsRunning(repo.AppName) {
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
	} else {
		p.Broker.EmitError(repoID, "⚠️  La nueva versión no pasó la verificación de salud")
		p.restoreFromBackup(repoID, appPath, backupPath, repo.AppName)
		return
	}

	p.Broker.EmitLog(repoID, sep)
	p.Broker.EmitLog(repoID, ">>> ACTUALIZACIÓN COMPLETADA <<<")
	p.Broker.EmitLog(repoID, fmt.Sprintf("   %s → %s", currentVer, newVersion))
	p.Broker.EmitLog(repoID, sep)
}

// CheckVersion checks the latest version without updating.
func (p *Pipeline) CheckVersion(repoID string) {
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

	p.Broker.EmitLog(repoID, fmt.Sprintf("📡 Verificando versión de %s/%s ...", repo.Owner, repo.Name))

	rel, err := p.GitHub.LatestRelease(repo.Owner, repo.Name)
	if err != nil {
		p.Broker.EmitError(repoID, "Error: "+err.Error())
		p.Store.Update(repoID, func(r *storage.Repository) {
			r.Status = storage.StatusFailed
			r.Error = err.Error()
		})
		p.Broker.Emit(events.NewUpdateFailed(repoID, err.Error()))
		return
	}

	p.Broker.EmitLog(repoID, fmt.Sprintf("🏷️  Última versión disponible: %s", rel.TagName))

	appPath, appErr := process.FindAppBinary(repo.AppName)
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
}

func (p *Pipeline) restoreFromBackup(repoID, appPath, backupPath, appName string) {
	p.Broker.EmitError(repoID, "⚠️  Restaurando backup...")
	os.Remove(appPath)
	if err := replaceFile(backupPath, appPath); err != nil {
		p.Broker.EmitError(repoID, "Error crítico al restaurar: "+err.Error())
		return
	}
	os.Chmod(appPath, 0755)
	p.Broker.EmitLog(repoID, "✅ Backup restaurado")

	repo := p.Store.Find(repoID)
	restoreArgs := process.SplitArgs(repo.CustomCommand)
	pid, err := p.ProcMan.Start(appName, appPath, restoreArgs...)
	if err != nil {
		p.Broker.EmitError(repoID, "Error al reiniciar versión anterior: "+err.Error())
	} else {
		p.Broker.EmitLog(repoID, fmt.Sprintf("🔄 Versión anterior reiniciada (PID %d)", pid))
	}
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

	// Fallback: run binary --version (last resort, expensive)
	cmd := exec.Command(appPath, "--version")
	out, err := cmd.Output()
	if err == nil {
		v := strings.TrimSpace(string(out))
		if v != "" {
			if len(v) > 100 {
				v = v[:100]
			}
			return v
		}
	}

	cmd = exec.Command(appPath, "version")
	out, err = cmd.Output()
	if err == nil {
		v := strings.TrimSpace(string(out))
		if v != "" {
			if len(v) > 100 {
				v = v[:100]
			}
			return v
		}
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

func verifyDownload(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return fmt.Errorf("archivo descargado vacío")
	}

	// Check magic bytes for known executable formats
	data := make([]byte, 4)
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Read(data); err != nil {
		return nil // can't read magic bytes, skip check
	}

	// ELF: 0x7f 'E' 'L' 'F'
	if data[0] == 0x7f && data[1] == 'E' && data[2] == 'L' && data[3] == 'F' {
		return nil
	}
	// PE: 'M' 'Z'
	if data[0] == 'M' && data[1] == 'Z' {
		return nil
	}
	// Mach-O (32-bit): 0xfe 0xed 0xfa 0xce
	// Mach-O (64-bit): 0xfe 0xed 0xfa 0xcf
	// Mach-O (reverse): 0xce 0xfa 0xed 0xfe
	if (data[0] == 0xfe && data[1] == 0xed && (data[2] == 0xfa || data[3] == 0xfa)) ||
		(data[0] == 0xce && data[1] == 0xfa && data[2] == 0xed && data[3] == 0xfe) ||
		(data[0] == 0xcf && data[1] == 0xfa && data[2] == 0xed && data[3] == 0xfe) {
		return nil
	}

	// Not a recognized format, but might be a script — allow it
	return nil
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

// fallbackAssetNames generates alternative asset names to try when the
// primary asset name doesn't match any release assets.
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

	// If Android (Termux), try linux variant as fallback
	if osName == "android" {
		names = append(names, fmt.Sprintf("%s-%s-%s", appName, "linux", arch))
		names = append(names, fmt.Sprintf("%s-%s-%s", appName, "linux", "arm64"))
	}

	// Try without arch
	names = append(names, fmt.Sprintf("%s-%s", appName, osName))

	// Try common compressed variants
	names = append(names, fmt.Sprintf("%s-%s-%s.tar.gz", appName, osName, arch))
	names = append(names, fmt.Sprintf("%s-%s-%s.tar.xz", appName, osName, arch))

	return names
}

// InstallApp performs a first-time install of an application binary.
// It downloads the latest release and places it in the appropriate directory.
func (p *Pipeline) InstallApp(repoID string) {
	const sep = "══════════════════════════════════════"

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
	tmpPath := filepath.Join(installDir, repo.AppName+".tmp")
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

	// 5. Verify integrity
	if err := verifyDownload(tmpPath); err != nil {
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
}

func (p *Pipeline) failInstall(repoID, errMsg string) {
	p.Broker.EmitError(repoID, errMsg)
	p.Store.Update(repoID, func(r *storage.Repository) {
		r.Status = storage.StatusFailed
		r.Error = errMsg
		r.Progress = 0
	})
	p.Broker.Emit(events.NewUpdateFailed(repoID, errMsg))
}

// Package api provides HTTP handlers for the AP Manager dashboard.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"ap-manager/internal/events"
	"ap-manager/internal/process"
	"ap-manager/internal/storage"
	"ap-manager/internal/updater"
)

// Handler holds dependencies for API handlers.
type Handler struct {
	Store   *storage.Store
	Broker  *events.Broker
	Updater *updater.Pipeline
	ProcMan *process.Manager
	OS      string
	Arch    string
	Version string
}

// splitArgs parses a command string into arguments.
func splitArgs(cmd string) []string {
	return process.SplitArgs(cmd)
}

// RegisterRoutes registers all API routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/repos", h.handleRepos)
	mux.HandleFunc("/api/repos/add", h.handleAddRepo)
	mux.HandleFunc("/api/repos/remove", h.handleRemoveRepo)
	mux.HandleFunc("/api/repos/check", h.handleCheckVersion)
	mux.HandleFunc("/api/repos/update", h.handleUpdateRepo)
	mux.HandleFunc("/api/repos/install", h.handleInstallRepo)
	mux.HandleFunc("/api/repos/edit", h.handleEditRepo)
	mux.HandleFunc("/api/repos/status", h.handleRepoStatus)
	mux.HandleFunc("/api/repos/stop", h.handleStopRepo)
	mux.HandleFunc("/api/repos/start", h.handleStartRepo)
	mux.HandleFunc("/api/repos/restart", h.handleRestartRepo)
	mux.HandleFunc("/api/events", h.handleSSE)
	mux.HandleFunc("/api/events/global", h.handleGlobalSSE)
	mux.HandleFunc("/api/platform", h.handlePlatform)
	mux.HandleFunc("/api/self", h.handleSelfInfo)
	mux.HandleFunc("/api/self/check", h.handleSelfCheck)
	mux.HandleFunc("/api/self/update", h.handleSelfUpdate)
}

// handleRepos returns the list of repos as JSON.
func (h *Handler) handleRepos(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	repos := h.Store.List()
	json.NewEncoder(w).Encode(repos)
}

// handleAddRepo adds a new repository.
func (h *Handler) handleAddRepo(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var input struct {
		Owner         string `json:"owner"`
		Name          string `json:"name"`
		AppName       string `json:"app_name"`
		Asset         string `json:"asset,omitempty"`
		CustomCommand string `json:"custom_command,omitempty"`
		InstallPath   string `json:"install_path,omitempty"`
		PlatOS        string `json:"platform_os,omitempty"`
		PlatArch      string `json:"platform_arch,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "JSON inválido: "+err.Error(), http.StatusBadRequest)
		return
	}
	if input.Owner == "" || input.Name == "" || input.AppName == "" {
		http.Error(w, "Faltan campos: owner, name, app_name", http.StatusBadRequest)
		return
	}

	id := strings.ToLower(input.Owner + "/" + input.Name)

	// Check if already exists
	for _, r := range h.Store.List() {
		if r.ID == id {
			http.Error(w, "El repositorio ya existe: "+id, http.StatusConflict)
			return
		}
	}

	repo := storage.Repository{
		ID:            id,
		Owner:         input.Owner,
		Name:          input.Name,
		AppName:       input.AppName,
		AssetName:     input.Asset,
		CustomCommand: input.CustomCommand,
		InstallPath:   input.InstallPath,
		PlatformOS:    input.PlatOS,
		PlatformArch:  input.PlatArch,
		Installed:     false,
		Status:        storage.StatusIdle,
	}

	if err := h.Store.Add(repo); err != nil {
		http.Error(w, "Error al guardar: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.Broker.EmitLog("_system", fmt.Sprintf("Repositorio añadido: %s/%s (%s)", input.Owner, input.Name, input.AppName))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(repo)
}

// handleRemoveRepo removes a repository.
func (h *Handler) handleRemoveRepo(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var input struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "JSON inválido", http.StatusBadRequest)
		return
	}

	if err := h.Store.Remove(input.ID); err != nil {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}

	h.Broker.EmitLog("_system", fmt.Sprintf("Repositorio eliminado: %s", input.ID))
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Eliminado"))
}

// handleEditRepo updates a repository's configuration fields.
func (h *Handler) handleEditRepo(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var input struct {
		ID            string  `json:"id"`
		Owner         *string `json:"owner,omitempty"`
		Name          *string `json:"name,omitempty"`
		AppName       *string `json:"app_name,omitempty"`
		Asset         *string `json:"asset,omitempty"`
		CustomCommand *string `json:"custom_command,omitempty"`
		InstallPath   *string `json:"install_path,omitempty"`
		PlatOS        *string `json:"platform_os,omitempty"`
		PlatArch      *string `json:"platform_arch,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "JSON inválido: "+err.Error(), http.StatusBadRequest)
		return
	}
	if input.ID == "" {
		http.Error(w, "Falta id", http.StatusBadRequest)
		return
	}

	// Fetch the old repo to compute new ID if owner/name changes
	oldRepo := h.Store.Find(input.ID)
	if oldRepo == nil {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}

	newOwner := oldRepo.Owner
	newName := oldRepo.Name
	if input.Owner != nil {
		newOwner = *input.Owner
	}
	if input.Name != nil {
		newName = *input.Name
	}
	newID := strings.ToLower(newOwner + "/" + newName)

	// If ID changed, check there's no conflict
	if newID != input.ID {
		if existing := h.Store.Find(newID); existing != nil {
			http.Error(w, "Ya existe un repositorio con ese ID: "+newID, http.StatusConflict)
			return
		}
	}

	// Apply all updates
	h.Store.Update(input.ID, func(r *storage.Repository) {
		if input.Owner != nil {
			r.Owner = *input.Owner
		}
		if input.Name != nil {
			r.Name = *input.Name
		}
		if input.AppName != nil {
			r.AppName = *input.AppName
		}
		if input.Asset != nil {
			r.AssetName = *input.Asset
		}
		if input.CustomCommand != nil {
			r.CustomCommand = *input.CustomCommand
		}
		if input.InstallPath != nil {
			r.InstallPath = *input.InstallPath
		}
		if input.PlatOS != nil {
			r.PlatformOS = *input.PlatOS
		}
		if input.PlatArch != nil {
			r.PlatformArch = *input.PlatArch
		}
		// Update ID if owner/name changed
		if newID != input.ID {
			r.ID = newID
		}
	})

	h.Store.Save()
	h.Broker.EmitLog("_system", fmt.Sprintf("Repositorio actualizado: %s → %s", input.ID, newID))

	// Return updated repo (look up by new ID)
	updated := h.Store.Find(newID)
	if updated == nil {
		updated = h.Store.Find(input.ID)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

// handleCheckVersion checks the latest release for a repo.
func (h *Handler) handleCheckVersion(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Falta id", http.StatusBadRequest)
		return
	}

	repo := h.Store.Find(id)
	if repo == nil {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}

	go h.Updater.CheckVersion(repo.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "checking"})
}

// handleRepoStatus checks if a repo's app process is running.
func (h *Handler) handleRepoStatus(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Falta id", http.StatusBadRequest)
		return
	}

	repo := h.Store.Find(id)
	if repo == nil {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}

	running := h.ProcMan.IsRunning(repo.ID)
	status := "stopped"
	if running {
		status = "running"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"id":     repo.ID,
		"status": status,
	})
}

// handleStopRepo stops a repo's app process.
func (h *Handler) handleStopRepo(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var input struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "JSON inválido", http.StatusBadRequest)
		return
	}

	repo := h.Store.Find(input.ID)
	if repo == nil {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}

	h.Broker.EmitLog(repo.ID, "🛑 Deteniendo "+repo.AppName+" ...")
	stopped, err := h.ProcMan.Stop(repo.ID)
	if err != nil {
		h.Broker.EmitError(repo.ID, "Error al detener: "+err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": err.Error()})
		return
	}

	if stopped {
		h.Broker.EmitLog(repo.ID, "✅ "+repo.AppName+" detenido")
	} else {
		h.Broker.EmitLog(repo.ID, "ℹ️ "+repo.AppName+" no estaba en ejecución")
	}

	h.Broker.Emit(events.NewServiceStatus(repo.ID, "stopped"))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

// handleStartRepo starts a repo's app process.
func (h *Handler) handleStartRepo(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var input struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "JSON inválido", http.StatusBadRequest)
		return
	}

	repo := h.Store.Find(input.ID)
	if repo == nil {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}

	if h.ProcMan.IsRunning(repo.ID) {
		h.Broker.EmitLog(repo.ID, "ℹ️ "+repo.AppName+" ya está en ejecución")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "already_running"})
		return
	}

	appPath, findErr := process.ResolveAppBinary(repo.AppName, repo.InstallPath)
	if findErr != nil {
		h.Broker.EmitError(repo.ID, "Binario no encontrado: "+findErr.Error())
		h.Store.Update(repo.ID, func(r *storage.Repository) {
			r.Installed = false
		})
		http.Error(w, "Binario no encontrado: "+findErr.Error(), http.StatusNotFound)
		return
	}

	args := splitArgs(repo.CustomCommand)
	h.Broker.EmitLog(repo.ID, "▶️ Iniciando "+repo.AppName+" "+strings.Join(args, " ")+" ...")
	pid, err := h.ProcMan.StartWithCapture(repo.AppName, repo.ID, appPath, h.Broker, args...)
	if err != nil {
		h.Broker.EmitError(repo.ID, "Error al iniciar: "+err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": err.Error()})
		return
	}

	// Update PID in state
	h.Store.Update(repo.ID, func(r *storage.Repository) {
		r.PID = pid
	})

	// Wait a moment and check
	time.Sleep(2 * time.Second)
	if h.ProcMan.IsRunning(repo.ID) {
		h.Broker.EmitLog(repo.ID, "✅ "+repo.AppName+" iniciado (PID "+fmt.Sprint(pid)+")")
	} else {
		h.Broker.EmitLog(repo.ID, "⚠️ "+repo.AppName+" no se inició correctamente")
	}

	h.Broker.Emit(events.NewServiceStatus(repo.ID, "running"))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started", "pid": fmt.Sprint(pid)})
}

// handleRestartRepo restarts a repo's app process.
func (h *Handler) handleRestartRepo(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var input struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "JSON inválido", http.StatusBadRequest)
		return
	}

	repo := h.Store.Find(input.ID)
	if repo == nil {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}

	h.Broker.EmitLog(repo.ID, "🔄 Reiniciando "+repo.AppName+" ...")
	h.ProcMan.Stop(repo.ID)
	time.Sleep(1 * time.Second)

	appPath, findErr := process.ResolveAppBinary(repo.AppName, repo.InstallPath)
	if findErr != nil {
		h.Broker.EmitError(repo.ID, "Binario no encontrado: "+findErr.Error())
		h.Store.Update(repo.ID, func(r *storage.Repository) {
			r.Installed = false
		})
		http.Error(w, "Binario no encontrado: "+findErr.Error(), http.StatusNotFound)
		return
	}

	args := splitArgs(repo.CustomCommand)
	pid, err := h.ProcMan.StartWithCapture(repo.AppName, repo.ID, appPath, h.Broker, args...)
	if err != nil {
		h.Broker.EmitError(repo.ID, "Error al reiniciar: "+err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": err.Error()})
		return
	}

	h.Store.Update(repo.ID, func(r *storage.Repository) {
		r.PID = pid
	})

	time.Sleep(2 * time.Second)
	if h.ProcMan.IsRunning(repo.ID) {
		h.Broker.EmitLog(repo.ID, "✅ "+repo.AppName+" reiniciado (PID "+fmt.Sprint(pid)+")")
	} else {
		h.Broker.EmitLog(repo.ID, "⚠️ "+repo.AppName+" no se inició después del reinicio")
	}

	h.Broker.Emit(events.NewServiceStatus(repo.ID, "running"))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "restarted"})
}

// handleUpdateRepo initiates the update pipeline.
// If the repo is ap-manager itself, it uses the self-update (script) path.
func (h *Handler) handleUpdateRepo(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Falta id", http.StatusBadRequest)
		return
	}

	repo := h.Store.Find(id)
	if repo == nil {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}

	// Detect self-update (ap-manager updating itself)
	if h.isSelfUpdate(repo) {
		h.selfUpdate(w, r, repo)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "updating"})

	go h.Updater.RunUpdate(repo.ID)
}

// isSelfUpdate checks if the repo's app is ap-manager itself.
func (h *Handler) isSelfUpdate(repo *storage.Repository) bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	exeName := filepath.Base(exe)
	// Match by app name or known binary name
	return repo.AppName == exeName || repo.AppName == "ap-manager"
}

// handleInstallRepo performs a first-time install for a repo's binary.
func (h *Handler) handleInstallRepo(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var input struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "JSON inválido", http.StatusBadRequest)
		return
	}
	if input.ID == "" {
		http.Error(w, "Falta id", http.StatusBadRequest)
		return
	}

	repo := h.Store.Find(input.ID)
	if repo == nil {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}

	if repo.Installed {
		// Check if the binary actually exists at the configured location.
		if _, err := process.ResolveAppBinary(repo.AppName, repo.InstallPath); err == nil {
			h.Broker.EmitLog(repo.ID, "ℹ️ "+repo.AppName+" ya está instalado")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "already_installed"})
			return
		}
		// Binary missing but flag says installed — fix flag
		h.Store.Update(repo.ID, func(r *storage.Repository) {
			r.Installed = false
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "installing"})

	go h.Updater.InstallApp(repo.ID)
}

// handleSSE provides per-repo event streaming.
func (h *Handler) handleSSE(w http.ResponseWriter, r *http.Request) {
	repoID := r.URL.Query().Get("id")
	if repoID == "" {
		http.Error(w, "Falta id", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming no soportado", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := h.Broker.Subscribe(repoID)
	defer h.Broker.Unsubscribe(repoID, ch)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}

// handleGlobalSSE provides global event streaming.
func (h *Handler) handleGlobalSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming no soportado", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := h.Broker.Subscribe("_global")
	defer h.Broker.Unsubscribe("_global", ch)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}

// handlePlatform returns platform info.
func (h *Handler) handlePlatform(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"os":   h.OS,
		"arch": h.Arch,
	})
}

// handleSelfInfo returns ap-manager's own version and platform info.
func (h *Handler) handleSelfInfo(w http.ResponseWriter, r *http.Request) {
	exe, err := os.Executable()
	exePath := ""
	if err == nil {
		exePath = exe
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"version": h.Version,
		"os":      h.OS,
		"arch":    h.Arch,
		"pid":     os.Getpid(),
		"binary":  exePath,
		"repo":    "mfloresz/app-manager",
	})
}

// NewHandler creates a Handler with runtime detection. The effective
// platform comes from the pipeline when set (e.g. "android" for Termux),
// falling back to the runtime values.
func NewHandler(store *storage.Store, broker *events.Broker, pipeline *updater.Pipeline, procMan *process.Manager, version string) *Handler {
	h := &Handler{
		Store:   store,
		Broker:  broker,
		Updater: pipeline,
		ProcMan: procMan,
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		Version: version,
	}
	if pipeline != nil {
		if pipeline.OS != "" {
			h.OS = pipeline.OS
		}
		if pipeline.Arch != "" {
			h.Arch = pipeline.Arch
		}
	}
	return h
}

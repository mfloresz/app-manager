// Package api provides HTTP handlers for the AP Manager dashboard.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
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
}

// RegisterRoutes registers all API routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/repos", h.handleRepos)
	mux.HandleFunc("/api/repos/add", h.handleAddRepo)
	mux.HandleFunc("/api/repos/remove", h.handleRemoveRepo)
	mux.HandleFunc("/api/repos/check", h.handleCheckVersion)
	mux.HandleFunc("/api/repos/update", h.handleUpdateRepo)
	mux.HandleFunc("/api/repos/status", h.handleRepoStatus)
	mux.HandleFunc("/api/repos/stop", h.handleStopRepo)
	mux.HandleFunc("/api/repos/start", h.handleStartRepo)
	mux.HandleFunc("/api/repos/restart", h.handleRestartRepo)
	mux.HandleFunc("/api/events", h.handleSSE)
	mux.HandleFunc("/api/events/global", h.handleGlobalSSE)
	mux.HandleFunc("/api/platform", h.handlePlatform)
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
		Owner    string `json:"owner"`
		Name     string `json:"name"`
		AppName  string `json:"app_name"`
		Asset    string `json:"asset,omitempty"`
		PlatOS   string `json:"platform_os,omitempty"`
		PlatArch string `json:"platform_arch,omitempty"`
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
	for _, repo := range h.Store.List() {
		if repo.ID == id {
			http.Error(w, "El repositorio ya existe: "+id, http.StatusConflict)
			return
		}
	}

	repo := storage.Repository{
		ID:           id,
		Owner:        input.Owner,
		Name:         input.Name,
		AppName:      input.AppName,
		AssetName:    input.Asset,
		PlatformOS:   input.PlatOS,
		PlatformArch: input.PlatArch,
		Status:       storage.StatusIdle,
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

	running := h.ProcMan.IsRunning(repo.AppName)
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
	stopped, err := h.ProcMan.Stop(repo.AppName)
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

	if h.ProcMan.IsRunning(repo.AppName) {
		h.Broker.EmitLog(repo.ID, "ℹ️ "+repo.AppName+" ya está en ejecución")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "already_running"})
		return
	}

	appPath, err := process.FindAppBinary(repo.AppName)
	if err != nil {
		h.Broker.EmitError(repo.ID, "Binario no encontrado: "+err.Error())
		http.Error(w, "Binario no encontrado: "+err.Error(), http.StatusNotFound)
		return
	}

	h.Broker.EmitLog(repo.ID, "▶️ Iniciando "+repo.AppName+" ...")
	pid, err := h.ProcMan.Start(repo.AppName, appPath)
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
	if h.ProcMan.IsRunning(repo.AppName) {
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
	h.ProcMan.Stop(repo.AppName)
	time.Sleep(1 * time.Second)

	appPath, err := process.FindAppBinary(repo.AppName)
	if err != nil {
		h.Broker.EmitError(repo.ID, "Binario no encontrado: "+err.Error())
		http.Error(w, "Binario no encontrado: "+err.Error(), http.StatusNotFound)
		return
	}

	pid, err := h.ProcMan.Start(repo.AppName, appPath)
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
	if h.ProcMan.IsRunning(repo.AppName) {
		h.Broker.EmitLog(repo.ID, "✅ "+repo.AppName+" reiniciado (PID "+fmt.Sprint(pid)+")")
	} else {
		h.Broker.EmitLog(repo.ID, "⚠️ "+repo.AppName+" no se inició después del reinicio")
	}

	h.Broker.Emit(events.NewServiceStatus(repo.ID, "running"))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "restarted"})
}

// handleUpdateRepo initiates the update pipeline.
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "updating"})

	go h.Updater.RunUpdate(repo.ID)
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

// NewHandler creates a Handler with runtime detection.
func NewHandler(store *storage.Store, broker *events.Broker, pipeline *updater.Pipeline, procMan *process.Manager) *Handler {
	return &Handler{
		Store:   store,
		Broker:  broker,
		Updater: pipeline,
		ProcMan: procMan,
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}
}

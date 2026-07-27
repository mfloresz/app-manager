package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ──────────────────────────────────────────────
// Config & State
// ──────────────────────────────────────────────

const defaultPort = ":8080"

var (
	reposFile = getEnvDefault("REPOS_FILE", "repos.json")
	port      = getEnvDefault("PORT", defaultPort)

	// SSE broadcast
	logCh    = make(chan sseMsg, 512)
	clients   = make(map[string]map[chan string]struct{}) // repoID -> {channels}
	globalCh = make(chan string, 256)
	clientsMu sync.Mutex
)

type sseMsg struct {
	RepoID string
	Text   string
}

// Repository represents a tracked repo/app pair.
type Repository struct {
	ID             string `json:"id"`
	Owner          string `json:"owner"`
	Name           string `json:"name"`
	AppName        string `json:"app_name"`
	CurrentVersion string `json:"current_version"`
	AssetName      string `json:"asset_name,omitempty"` // override: "myapp-linux-amd64"
	PlatformOS     string `json:"platform_os,omitempty"`
	PlatformArch   string `json:"platform_arch,omitempty"`
}

// AppState holds all repos and is persisted to disk.
type AppState struct {
	Repos []Repository `json:"repos"`
	mu    sync.Mutex
}

var state AppState

// ──────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────

func getEnvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func repoAPIURL(r *Repository) string {
	return fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", r.Owner, r.Name)
}

func repoURL(r *Repository) string {
	return fmt.Sprintf("https://github.com/%s/%s", r.Owner, r.Name)
}

// defaultAssetName builds the expected asset name: "appName-OS-ARCH"
func defaultAssetName(r *Repository) string {
	os := r.PlatformOS
	arch := r.PlatformArch
	if os == "" {
		os = runtime.GOOS
	}
	if arch == "" {
		arch = runtime.GOARCH
	}
	return fmt.Sprintf("%s-%s-%s", r.AppName, os, arch)
}

// ──────────────────────────────────────────────
// State persistence
// ──────────────────────────────────────────────

func loadState() {
	data, err := os.ReadFile(reposFile)
	if err != nil {
		if os.IsNotExist(err) {
			state.Repos = []Repository{}
			return
		}
		fmt.Fprintf(os.Stderr, "Error leyendo %s: %v\n", reposFile, err)
		state.Repos = []Repository{}
		return
	}
	if err := json.Unmarshal(data, &state.Repos); err != nil {
		fmt.Fprintf(os.Stderr, "Error parseando %s: %v\n", reposFile, err)
		state.Repos = []Repository{}
		return
	}
	// Ensure all repos have an ID
	for i := range state.Repos {
		if state.Repos[i].ID == "" {
			state.Repos[i].ID = fmt.Sprintf("repo-%d", i)
		}
	}
}

func saveState() error {
	state.mu.Lock()
	defer state.mu.Unlock()
	data, err := json.MarshalIndent(state.Repos, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(reposFile, data, 0644)
}

func findRepo(id string) *Repository {
	state.mu.Lock()
	defer state.mu.Unlock()
	for i := range state.Repos {
		if state.Repos[i].ID == id {
			return &state.Repos[i]
		}
	}
	return nil
}

func updateRepo(id string, fn func(r *Repository)) {
	state.mu.Lock()
	defer state.mu.Unlock()
	for i := range state.Repos {
		if state.Repos[i].ID == id {
			fn(&state.Repos[i])
			return
		}
	}
}

// ──────────────────────────────────────────────
// SSE broadcasting
// ──────────────────────────────────────────────

func emit(repoID, msg string) {
	ts := time.Now().Format("15:04:05")
	formatted := fmt.Sprintf("[%s] %s", ts, msg)
	fmt.Println(formatted)

	logCh <- sseMsg{RepoID: repoID, Text: formatted}
}

func emitErr(repoID, msg string) {
	ts := time.Now().Format("15:04:05")
	formatted := fmt.Sprintf("[%s] ERROR: %s", ts, msg)
	fmt.Fprintln(os.Stderr, formatted)
	logCh <- sseMsg{RepoID: repoID, Text: formatted}
}

func broadcastLoop() {
	for msg := range logCh {
		clientsMu.Lock()
		// Send to repo-specific channels
		if chans, ok := clients[msg.RepoID]; ok {
			for ch := range chans {
				select {
				case ch <- "[repo-" + msg.RepoID + "] " + msg.Text:
				default:
				}
			}
		}
		// Send to global channels ("_global")
		if chans, ok := clients["_global"]; ok {
			for ch := range chans {
				select {
				case ch <- "[global] " + msg.Text:
				default:
				}
			}
		}
		clientsMu.Unlock()
	}
}

// ──────────────────────────────────────────────
// GitHub release checker
// ──────────────────────────────────────────────

type release struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
}

func checkLatestRelease(r *Repository) (*release, error) {
	apiURL := repoAPIURL(r)
	emit(r.ID, "Consultando: "+apiURL)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("conexión: %w", err)
	}
	defer resp.Body.Close()

	// GitHub may rate-limit
	if resp.StatusCode == 403 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("rate-limit (HTTP 403): %s", string(body))
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decodificar JSON: %w", err)
	}
	return &rel, nil
}

// detectCurrentVersion tries to get the version of the installed binary.
func detectCurrentVersion(appPath string) string {
	// Try `app --version`
	cmd := exec.Command(appPath, "--version")
	out, err := cmd.Output()
	if err == nil {
		v := strings.TrimSpace(string(out))
		if v != "" {
			return v
		}
	}
	// Try `app version`
	cmd = exec.Command(appPath, "version")
	out, err = cmd.Output()
	if err == nil {
		v := strings.TrimSpace(string(out))
		if v != "" {
			return v
		}
	}
	return "desconocida"
}

// ──────────────────────────────────────────────
// HTTP server
// ──────────────────────────────────────────────

func main() {
	loadState()
	saveState() // ensure file exists

	fmt.Printf("=== AP Manager Dashboard ===\n")
	fmt.Printf("Plataforma: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Repositorios: %d\n", len(state.Repos))
	fmt.Printf("Puerto: %s\n", port)
	fmt.Printf("Datos: %s\n", reposFile)

	go broadcastLoop()

	mux := http.NewServeMux()

	// API
	mux.HandleFunc("/api/repos", handleRepos)
	mux.HandleFunc("/api/repos/add", handleAddRepo)
	mux.HandleFunc("/api/repos/remove", handleRemoveRepo)
	mux.HandleFunc("/api/repos/check", handleCheckVersion)
	mux.HandleFunc("/api/repos/update", handleUpdateRepo)
	mux.HandleFunc("/api/repos/status", handleRepoStatus)
	mux.HandleFunc("/api/repos/stop", handleStopRepo)
	mux.HandleFunc("/api/repos/start", handleStartRepo)
	mux.HandleFunc("/api/repos/restart", handleRestartRepo)
	mux.HandleFunc("/api/events", handleSSE)
	mux.HandleFunc("/api/events/global", handleGlobalSSE)
	mux.HandleFunc("/api/platform", handlePlatform)

	// Frontend (SPA)
	mux.HandleFunc("/", serveDashboard)

	fmt.Println("Servidor iniciado en http://localhost" + port)
	if err := http.ListenAndServe(port, mux); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// ──────────────────────────────────────────────
// API Handlers
// ──────────────────────────────────────────────

func serveDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(dashboardHTML))
}

func handlePlatform(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"os":   runtime.GOOS,
		"arch": runtime.GOARCH,
	})
}

// handleRepos returns the list of repos as JSON.
func handleRepos(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	state.mu.Lock()
	defer state.mu.Unlock()
	json.NewEncoder(w).Encode(state.Repos)
}

// handleAddRepo adds a new repository.
func handleAddRepo(w http.ResponseWriter, r *http.Request) {
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

	// Build ID
	id := strings.ToLower(input.Owner + "/" + input.Name)

	state.mu.Lock()
	// Check if already exists
	for _, repo := range state.Repos {
		if repo.ID == id {
			state.mu.Unlock()
			http.Error(w, "El repositorio ya existe: "+id, http.StatusConflict)
			return
		}
	}

	repo := Repository{
		ID:           id,
		Owner:        input.Owner,
		Name:         input.Name,
		AppName:      input.AppName,
		AssetName:    input.Asset,
		PlatformOS:   input.PlatOS,
		PlatformArch: input.PlatArch,
	}
	state.Repos = append(state.Repos, repo)
	state.mu.Unlock()

	if err := saveState(); err != nil {
		http.Error(w, "Error al guardar: "+err.Error(), http.StatusInternalServerError)
		return
	}

	emit("_system", fmt.Sprintf("Repositorio añadido: %s/%s (%s)", input.Owner, input.Name, input.AppName))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(repo)
}

// handleRemoveRepo removes a repository.
func handleRemoveRepo(w http.ResponseWriter, r *http.Request) {
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

	state.mu.Lock()
	found := false
	for i, repo := range state.Repos {
		if repo.ID == input.ID {
			state.Repos = append(state.Repos[:i], state.Repos[i+1:]...)
			found = true
			break
		}
	}
	state.mu.Unlock()

	if !found {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}

	saveState()
	emit("_system", fmt.Sprintf("Repositorio eliminado: %s", input.ID))
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Eliminado"))
}

// handleCheckVersion checks the latest release for a repo without updating.
func handleCheckVersion(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Falta id", http.StatusBadRequest)
		return
	}

	repo := findRepo(id)
	if repo == nil {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}

	go checkVersionForRepo(repo.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "checking"})
}

func checkVersionForRepo(repoID string) {
	repo := findRepo(repoID)
	if repo == nil {
		return
	}

	emit(repo.ID, fmt.Sprintf("📡 Verificando versión de %s/%s ...", repo.Owner, repo.Name))

	rel, err := checkLatestRelease(repo)
	if err != nil {
		emitErr(repo.ID, "Error: "+err.Error())
		updateRepo(repoID, func(r *Repository) {
			r.CurrentVersion = "error: " + err.Error()
		})
		saveState()
		return
	}

	emit(repo.ID, fmt.Sprintf("🏷️  Última versión disponible: %s", rel.TagName))

	// Try to detect current version from local binary
	appPath, err := findAppBinary(repo.AppName)
	currentVer := "no detectada"
	if err == nil {
		currentVer = detectCurrentVersion(appPath)
		emit(repo.ID, fmt.Sprintf("💻 Versión actual detectada: %s", currentVer))
	} else {
		emit(repo.ID, "💻 Binario no encontrado localmente (versión no detectada)")
	}

	updateRepo(repoID, func(r *Repository) {
		r.CurrentVersion = currentVer
	})
	saveState()

	// Emit current + latest as structured data for the UI to pick up
	emit(repo.ID, fmt.Sprintf("VERSION_DATA|%s|%s", currentVer, rel.TagName))
}

// handleRepoStatus checks if a repo's app process is currently running.
func handleRepoStatus(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Falta id", http.StatusBadRequest)
		return
	}

	repo := findRepo(id)
	if repo == nil {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}

	running := isProcessRunning(repo.AppName)
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
func handleStopRepo(w http.ResponseWriter, r *http.Request) {
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

	repo := findRepo(input.ID)
	if repo == nil {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}

	emit(repo.ID, "🛑 Deteniendo "+repo.AppName+" ...")
	killApp(repo.AppName)
	time.Sleep(1 * time.Second)

	if isProcessRunning(repo.AppName) {
		emitErr(repo.ID, "No se pudo detener "+repo.AppName)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "No se pudo detener el proceso"})
		return
	}

	emit(repo.ID, "✅ "+repo.AppName+" detenido")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

// handleStartRepo starts a repo's app process.
func handleStartRepo(w http.ResponseWriter, r *http.Request) {
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

	repo := findRepo(input.ID)
	if repo == nil {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}

	if isProcessRunning(repo.AppName) {
		emit(repo.ID, "ℹ️ "+repo.AppName+" ya está en ejecución")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "already_running"})
		return
	}

	appPath, err := findAppBinary(repo.AppName)
	if err != nil {
		emitErr(repo.ID, "Binario no encontrado: "+err.Error())
		http.Error(w, "Binario no encontrado: "+err.Error(), http.StatusNotFound)
		return
	}

	emit(repo.ID, "▶️ Iniciando "+repo.AppName+" ...")
	if err := startApp(appPath); err != nil {
		emitErr(repo.ID, "Error al iniciar: "+err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": err.Error()})
		return
	}

	time.Sleep(2 * time.Second)
	if isProcessRunning(repo.AppName) {
		emit(repo.ID, "✅ "+repo.AppName+" iniciado")
	}

	// Detect and store version
	currentVer := detectCurrentVersion(appPath)
	updateRepo(repo.ID, func(r *Repository) {
		r.CurrentVersion = currentVer
	})
	saveState()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

// handleRestartRepo restarts a repo's app process.
func handleRestartRepo(w http.ResponseWriter, r *http.Request) {
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

	repo := findRepo(input.ID)
	if repo == nil {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}

	emit(repo.ID, "🔄 Reiniciando "+repo.AppName+" ...")
	killApp(repo.AppName)
	time.Sleep(1 * time.Second)

	appPath, err := findAppBinary(repo.AppName)
	if err != nil {
		emitErr(repo.ID, "Binario no encontrado: "+err.Error())
		http.Error(w, "Binario no encontrado: "+err.Error(), http.StatusNotFound)
		return
	}

	if err := startApp(appPath); err != nil {
		emitErr(repo.ID, "Error al reiniciar: "+err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": err.Error()})
		return
	}

	time.Sleep(2 * time.Second)
	if isProcessRunning(repo.AppName) {
		emit(repo.ID, "✅ "+repo.AppName+" reiniciado")
	} else {
		emitErr(repo.ID, "⚠️  "+repo.AppName+" no se inició después del reinicio")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "restarted"})
}

// handleUpdateRepo performs the update for a single repo.
func handleUpdateRepo(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Falta id", http.StatusBadRequest)
		return
	}

	repo := findRepo(id)
	if repo == nil {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "updating"})

	go runUpdateForRepo(repo.ID)
}

// handleSSE provides per-repo log streaming.
func handleSSE(w http.ResponseWriter, r *http.Request) {
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

	ch := make(chan string, 128)
	clientsMu.Lock()
	if clients[repoID] == nil {
		clients[repoID] = make(map[chan string]struct{})
	}
	clients[repoID][ch] = struct{}{}
	clientsMu.Unlock()

	defer func() {
		clientsMu.Lock()
		delete(clients[repoID], ch)
		if len(clients[repoID]) == 0 {
			delete(clients, repoID)
		}
		clientsMu.Unlock()
	}()

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
func handleGlobalSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming no soportado", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan string, 128)
	clientsMu.Lock()
	if clients["_global"] == nil {
		clients["_global"] = make(map[chan string]struct{})
	}
	clients["_global"][ch] = struct{}{}
	clientsMu.Unlock()

	defer func() {
		clientsMu.Lock()
		delete(clients["_global"], ch)
		if len(clients["_global"]) == 0 {
			delete(clients, "_global")
		}
		clientsMu.Unlock()
	}()

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

// ──────────────────────────────────────────────
// Update pipeline (per-repo)
// ──────────────────────────────────────────────

func runUpdateForRepo(repoID string) {
	const sep = "══════════════════════════════════════"

	repo := findRepo(repoID)
	if repo == nil {
		emitErr(repoID, "Repositorio no encontrado")
		return
	}

	emit(repo.ID, sep)
	emit(repo.ID, fmt.Sprintf(">>> ACTUALIZANDO %s/%s (%s) <<<", repo.Owner, repo.Name, repo.AppName))
	emit(repo.ID, sep)

	// 1. Find binary
	appPath, err := findAppBinary(repo.AppName)
	if err != nil {
		emitErr(repo.ID, "Binario no encontrado: "+err.Error())
		emit(repo.ID, "💡 Asegúrate de que '"+repo.AppName+"' esté en el PATH o en el mismo directorio")
		return
	}
	emit(repo.ID, "Binario encontrado: "+appPath)
	appDir := filepath.Dir(appPath)

	// 2. Detect current version before update
	currentVer := detectCurrentVersion(appPath)
	emit(repo.ID, "Versión actual: "+currentVer)

	// 3. Check latest release
	rel, err := checkLatestRelease(repo)
	if err != nil {
		emitErr(repo.ID, "Error al consultar GitHub: "+err.Error())
		return
	}
	newVersion := rel.TagName
	emit(repo.ID, "Nueva versión disponible: "+newVersion)

	// 4. Find matching asset — try multiple strategies
	expectedName := repo.AssetName
	if expectedName == "" {
		expectedName = defaultAssetName(repo)
	}
	emit(repo.ID, "Buscando asset base: "+expectedName)

	// Strategies in order of preference:
	//   1. Exact match
	//   2. Prefix match (asset starts with expected name)
	//   3. Contains match (e.g. "translator-server-linux-amd64-v0.7.1" contains "translator-server-linux-amd64")
	var targetAsset *asset
	for i, a := range rel.Assets {
		if a.Name == expectedName {
			targetAsset = &rel.Assets[i]
			break
		}
	}
	if targetAsset == nil {
		// Try prefix match
		for i, a := range rel.Assets {
			if strings.HasPrefix(a.Name, expectedName) {
				targetAsset = &rel.Assets[i]
				emit(repo.ID, "  └─ Match por prefijo: "+a.Name)
				break
			}
		}
	}
	if targetAsset == nil {
		// Try contains match (asset name includes expected name somewhere)
		for i, a := range rel.Assets {
			if strings.Contains(a.Name, expectedName) {
				targetAsset = &rel.Assets[i]
				emit(repo.ID, "  └─ Match por substring: "+a.Name)
				break
			}
		}
	}
	if targetAsset == nil {
		emitErr(repo.ID, "No se encontró asset para: "+expectedName)
		emit(repo.ID, "Assets disponibles:")
		for _, a := range rel.Assets {
			emit(repo.ID, "  └─ "+a.Name)
		}
		return
	}
	emit(repo.ID, "Asset encontrado: "+targetAsset.Name)

	// 5. Download
	tmpPath := filepath.Join(appDir, repo.AppName+".tmp")
	if err := downloadFile(targetAsset.DownloadURL, tmpPath); err != nil {
		emitErr(repo.ID, "Error al descargar: "+err.Error())
		return
	}
	emit(repo.ID, "✅ Descarga completada")

	// 6. Set permissions
	if err := os.Chmod(tmpPath, 0755); err != nil {
		emitErr(repo.ID, "Error al establecer permisos: "+err.Error())
		os.Remove(tmpPath)
		return
	}

	// 7. Backup
	backupPath := filepath.Join(appDir, repo.AppName+".bak")
	if err := copyFile(appPath, backupPath); err != nil {
		emitErr(repo.ID, "Error al crear backup: "+err.Error())
		os.Remove(tmpPath)
		return
	}
	emit(repo.ID, "📦 Backup creado: "+backupPath)

	// 8. Stop the app
	emit(repo.ID, "Deteniendo "+repo.AppName+" ...")
	killApp(repo.AppName)
	time.Sleep(1 * time.Second)

	// 9. Replace binary
	emit(repo.ID, "Reemplazando binario...")
	if err := os.Rename(tmpPath, appPath); err != nil {
		emitErr(repo.ID, "Error al reemplazar: "+err.Error())
		restoreBackup(repo.ID, appPath, backupPath, repo.AppName)
		return
	}
	emit(repo.ID, "✅ Binario reemplazado")

	// 10. Start new version
	emit(repo.ID, "Iniciando nueva versión...")
	if err := startApp(appPath); err != nil {
		emitErr(repo.ID, "Error al iniciar: "+err.Error())
		restoreBackup(repo.ID, appPath, backupPath, repo.AppName)
		return
	}

	// 11. Verify
	time.Sleep(3 * time.Second)
	if isProcessRunning(repo.AppName) {
		emit(repo.ID, "✅ Nueva versión verificada y funcionando")

		// Update stored version
		updateRepo(repoID, func(r *Repository) {
			r.CurrentVersion = newVersion
		})
		saveState()

		os.Remove(backupPath)
		emit(repo.ID, "🗑️  Backup eliminado")
	} else {
		emitErr(repo.ID, "⚠️  La nueva versión no está en ejecución")
		restoreBackup(repo.ID, appPath, backupPath, repo.AppName)
		return
	}

	// Emit structured version data for UI (current version is now the new version)
	emit(repo.ID, fmt.Sprintf("VERSION_DATA|%s|%s", newVersion, newVersion))

	emit(repo.ID, sep)
	emit(repo.ID, ">>> ACTUALIZACIÓN COMPLETADA <<<")
	emit(repo.ID, fmt.Sprintf("   %s → %s", currentVer, newVersion))
	emit(repo.ID, sep)
}

// ──────────────────────────────────────────────
// Binary helpers
// ──────────────────────────────────────────────

func findAppBinary(name string) (string, error) {
	// First check same directory as updater
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

	// Then check PATH
	if path, err := exec.LookPath(name); err == nil {
		return filepath.Abs(path)
	}

	return "", fmt.Errorf("'%s' no encontrado en directorio ni en PATH", name)
}

func downloadFile(url, destPath string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("conexión: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("crear archivo: %w", err)
	}
	defer out.Close()

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		os.Remove(destPath)
		return fmt.Errorf("escritura: %w", err)
	}
	fmt.Printf("Descargados %d bytes a %s\n", written, destPath)
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

	if _, err = io.Copy(out, in); err != nil {
		return err
	}

	// Preservar permisos del archivo original
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, srcInfo.Mode())
}

func killApp(name string) {
	// Usar pgrep para obtener PIDs, soportando nombres > 15 chars
	out, err := exec.Command("pgrep", "-f", name).Output()
	if err == nil {
		for _, pidStr := range strings.Fields(string(out)) {
			pid := strings.TrimSpace(pidStr)
			if pid != "" {
				exec.Command("kill", pid).Run()
			}
		}
	}
	time.Sleep(1 * time.Second)
	// Force kill si sigue vivo
	out, err = exec.Command("pgrep", "-f", name).Output()
	if err == nil {
		for _, pidStr := range strings.Fields(string(out)) {
			pid := strings.TrimSpace(pidStr)
			if pid != "" {
				exec.Command("kill", "-9", pid).Run()
			}
		}
	}
}

func startApp(path string) error {
	cmd := exec.Command(path)
	cmd.Stdout = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("exec: %w", err)
	}
	// Esperar un momento para detectar fallos inmediatos
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			msg := err.Error()
			if sb := stderr.String(); sb != "" {
				msg += ": " + strings.TrimSpace(sb)
			}
			return fmt.Errorf("proceso terminó con error: %s", msg)
		}
	case <-time.After(2 * time.Second):
		// Proceso sigue vivo tras 2s, probablemente OK
	}
	return nil
}

func isProcessRunning(name string) bool {
	return exec.Command("pgrep", "-f", name).Run() == nil
}

func restoreBackup(repoID, appPath, backupPath, appName string) {
	emitErr(repoID, "⚠️  Restaurando backup...")
	// Matar el proceso nuevo (si sigue vivo) antes de restaurar
	killApp(appName)
	os.Remove(appPath)
	if err := os.Rename(backupPath, appPath); err != nil {
		emitErr(repoID, "Error crítico: "+err.Error())
		return
	}
	// Los permisos se preservan del backup (copyFile ahora preserva permisos)
	emit(repoID, "✅ Backup restaurado")

	if err := startApp(appPath); err != nil {
		emitErr(repoID, "Error al reiniciar: "+err.Error())
	} else {
		emit(repoID, "🔄 Versión anterior reiniciada")
	}
}

// ──────────────────────────────────────────────
// Dashboard HTML (embedded SPA)
// ──────────────────────────────────────────────

const dashboardHTML = `<!DOCTYPE html>
<html lang="es">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>AP Manager — Dashboard</title>
<style>
:root {
  --bg: #0d1117;
  --surface: #161b22;
  --surface2: #1c2333;
  --border: #30363d;
  --text: #c9d1d9;
  --text2: #8b949e;
  --accent: #58a6ff;
  --green: #3fb950;
  --orange: #d29922;
  --red: #f85149;
  --radius: 10px;
}
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Noto Sans,sans-serif;background:var(--bg);color:var(--text);min-height:100vh}
.header{background:var(--surface);border-bottom:1px solid var(--border);padding:1rem 2rem;display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:1rem}
.header h1{font-size:1.4rem;font-weight:700;color:#f0f6fc;display:flex;align-items:center;gap:.5rem}
.header h1 span{color:var(--accent)}
.header .sub{font-size:.82rem;color:var(--text2)}
.layout{display:grid;grid-template-columns:1fr 340px;gap:1.5rem;padding:1.5rem 2rem;max-width:1400px;margin:0 auto}
@media(max-width:900px){.layout{grid-template-columns:1fr;padding:1rem}}

/* Cards grid */
.repos-grid{display:flex;flex-direction:column;gap:1rem}

/* Repo Card */
.repo-card{background:var(--surface);border:1px solid var(--border);border-radius:var(--radius);padding:1.25rem;transition:border-color .2s}
.repo-card:hover{border-color:var(--accent)}
.repo-card .top{display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:.75rem;flex-wrap:wrap;gap:.5rem}
.repo-card .title{font-size:1.05rem;font-weight:600;color:#f0f6fc}
.repo-card .title a{color:var(--accent);text-decoration:none}
.repo-card .title a:hover{text-decoration:underline}
.repo-card .title .owner{color:var(--text2);font-weight:400}
.repo-card .badge{font-size:.72rem;padding:.15rem .5rem;border-radius:20px;font-weight:500}
.badge-idle{background:var(--surface2);color:var(--text2);border:1px solid var(--border)}
.badge-checking{background:#1a2a3a;color:var(--accent);border:1px solid var(--accent)}
.badge-updating{background:#2a1a1a;color:var(--orange);border:1px solid var(--orange)}
.badge-done{background:#1a2a1a;color:var(--green);border:1px solid var(--green)}
.badge-error{background:#2a1a1a;color:var(--red);border:1px solid var(--red)}
.badge-running{background:#0a2a0a;color:var(--green);border:1px solid var(--green)}
.badge-stopped{background:#2a1a1a;color:var(--text2);border:1px solid var(--border)}

.repo-card .versions{display:flex;gap:2rem;margin-bottom:.75rem;flex-wrap:wrap}
.repo-card .version-box{flex:1;min-width:120px}
.repo-card .version-box .label{font-size:.72rem;text-transform:uppercase;letter-spacing:.04em;color:var(--text2);margin-bottom:.15rem}
.repo-card .version-box .value{font-size:1.1rem;font-weight:600;font-family:'SF Mono','Courier New',monospace}
.value-current{color:var(--text)}
.value-new{color:var(--green)}
.value-new.pending{color:var(--orange)}
.value-na{color:var(--text2);font-weight:400!important}
.value-error{color:var(--red)}

.repo-card .actions{display:flex;gap:.5rem;flex-wrap:wrap}
.repo-card .actions button{display:inline-flex;align-items:center;gap:.35rem;padding:.5rem 1rem;font-size:.82rem;font-weight:500;border-radius:6px;border:1px solid var(--border);cursor:pointer;transition:all .15s;background:var(--surface2);color:var(--text)}
.repo-card .actions button:hover{background:var(--border)}
.repo-card .actions button.primary{background:var(--accent);color:#fff;border-color:var(--accent)}
.repo-card .actions button.primary:hover{background:#79c0ff}
.repo-card .actions button.danger{color:var(--red);border-color:var(--red)}
.repo-card .actions button.danger:hover{background:var(--red);color:#fff}
.repo-card .actions button:disabled{opacity:.4;cursor:not-allowed}

/* Log console */
.log-panel{position:sticky;top:1.5rem}
.log-panel h3{font-size:.85rem;text-transform:uppercase;letter-spacing:.05em;color:var(--text2);margin-bottom:.5rem;display:flex;justify-content:space-between;align-items:center}
.log-panel h3 button{background:none;border:1px solid var(--border);color:var(--text2);padding:.2rem .6rem;border-radius:4px;cursor:pointer;font-size:.72rem}
.log-panel h3 button:hover{background:var(--surface2);color:var(--text)}
.log-console{background:#010409;border:1px solid var(--border);border-radius:var(--radius);padding:.75rem;height:calc(100vh - 200px);overflow-y:auto;font-family:'SF Mono','Courier New',monospace;font-size:.78rem;line-height:1.55;white-space:pre-wrap;word-break:break-all}
.log-console .line{color:var(--green);margin-bottom:1px}
.log-console .line.err{color:var(--red)}
.log-console .line.info{color:var(--accent)}
.log-console .line.warn{color:var(--orange)}
.log-console .line.system{color:var(--text2);font-style:italic}

/* Add repo modal */
.modal-overlay{display:none;position:fixed;top:0;left:0;width:100%;height:100%;background:rgba(0,0,0,.6);z-index:100;justify-content:center;align-items:center}
.modal-overlay.active{display:flex}
.modal{background:var(--surface);border:1px solid var(--border);border-radius:12px;padding:2rem;max-width:480px;width:92%;box-shadow:0 16px 48px rgba(0,0,0,.5)}
.modal h2{font-size:1.2rem;margin-bottom:1rem;color:#f0f6fc}
.modal label{display:block;font-size:.82rem;color:var(--text2);margin-top:.75rem;margin-bottom:.25rem}
.modal input{width:100%;padding:.6rem .75rem;background:var(--bg);border:1px solid var(--border);border-radius:6px;color:var(--text);font-size:.9rem;outline:none;transition:border-color .15s}
.modal input:focus{border-color:var(--accent)}
.modal .hint{font-size:.72rem;color:var(--text2);margin-top:.25rem}
.modal .modal-actions{display:flex;gap:.75rem;margin-top:1.25rem;justify-content:flex-end}
.modal .modal-actions button{padding:.6rem 1.25rem;border-radius:6px;font-size:.85rem;font-weight:500;cursor:pointer;border:1px solid var(--border);transition:all .15s}
.modal .modal-actions .cancel{background:var(--surface2);color:var(--text)}
.modal .modal-actions .cancel:hover{background:var(--border)}
.modal .modal-actions .submit{background:var(--accent);color:#fff;border-color:var(--accent)}
.modal .modal-actions .submit:hover{background:#79c0ff}
.modal .error-msg{color:var(--red);font-size:.82rem;margin-top:.5rem;display:none}

.empty-state{text-align:center;padding:3rem 1rem;color:var(--text2)}
.empty-state .icon{font-size:2.5rem;margin-bottom:.5rem}
.empty-state h3{font-size:1.1rem;margin-bottom:.25rem;color:var(--text)}
.empty-state p{font-size:.85rem;margin-bottom:1rem}
.empty-state button{background:var(--accent);color:#fff;border:none;padding:.6rem 1.5rem;border-radius:6px;font-size:.9rem;cursor:pointer}

/* Toast */
.toast{position:fixed;bottom:1.5rem;right:1.5rem;background:var(--surface);border:1px solid var(--border);border-radius:var(--radius);padding:.75rem 1.25rem;font-size:.85rem;box-shadow:0 8px 24px rgba(0,0,0,.4);z-index:200;display:none;max-width:360px}
.toast.show{display:block;animation:fadeIn .3s}
.toast.success{border-color:var(--green)}
.toast.error{border-color:var(--red)}
@keyframes fadeIn{from{opacity:0;transform:translateY(10px)}to{opacity:1;transform:translateY(0)}}

/* Spinner */
.spinner{display:inline-block;width:12px;height:12px;border:2px solid var(--border);border-top-color:var(--accent);border-radius:50%;animation:spin .6s linear infinite;vertical-align:middle;margin-right:4px}
@keyframes spin{to{transform:rotate(360deg)}}

/* Diff display */
.diff{display:inline-flex;align-items:center;gap:.4rem}
.diff-arrow{color:var(--text2);font-size:.8rem}
</style>
</head>
<body>
<div class="header">
  <div>
    <h1>⬡ <span>AP</span> Manager</h1>
    <div class="sub">Dashboard de actualización multi-repositorio</div>
  </div>
  <div style="display:flex;gap:.75rem;align-items:center">
    <span id="plat" class="sub">Plataforma: —</span>
    <button onclick="showAddModal()" style="background:var(--accent);color:#fff;border:none;padding:.5rem 1rem;border-radius:6px;font-size:.82rem;cursor:pointer;font-weight:500">+ Añadir Repositorio</button>
  </div>
</div>

<div class="layout">
  <div>
    <div id="repos-container">
      <div class="empty-state" id="empty-state">
        <div class="icon">📦</div>
        <h3>No hay repositorios</h3>
        <p>Añade un repositorio de GitHub para empezar a rastrear versiones</p>
        <button onclick="showAddModal()">+ Añadir Repositorio</button>
      </div>
      <div class="repos-grid" id="repos-grid"></div>
    </div>
  </div>

  <div class="log-panel">
    <h3>
      <span>📋 Registro de eventos</span>
      <button onclick="clearLog()">Limpiar</button>
    </h3>
    <div class="log-console" id="global-log">
      <div class="line system">⬡ AP Manager iniciado — esperando eventos...</div>
    </div>
  </div>
</div>

<!-- Add Modal -->
<div class="modal-overlay" id="add-modal">
  <div class="modal">
    <h2>Añadir Repositorio</h2>
    <label for="in-owner">Usuario / Organización</label>
    <input id="in-owner" type="text" placeholder="mfloresz" autocomplete="off">
    <label for="in-repo">Repositorio</label>
    <input id="in-repo" type="text" placeholder="yara" autocomplete="off">
    <label for="in-app">Nombre del binario</label>
    <input id="in-app" type="text" placeholder="yara" autocomplete="off">
    <div class="hint" id="asset-preview">Asset esperado: <span id="asset-name">yara-linux-amd64</span></div>
    <label for="in-asset">Nombre exacto del asset <small style="color:var(--text2)">(opcional, si no sigue el patrón nombre-OS-arq)</small></label>
    <input id="in-asset" type="text" placeholder="yara-v2.0.0-linux-amd64" autocomplete="off">
    <div class="error-msg" id="modal-error"></div>
    <div class="modal-actions">
      <button class="cancel" onclick="hideAddModal()">Cancelar</button>
      <button class="submit" onclick="addRepo()">Añadir</button>
    </div>
  </div>
</div>

<!-- Toast -->
<div class="toast" id="toast"></div>

<script>
// ── State ──
let repos = [];
let repoStatus = {}; // repoID -> {status:'idle'|'checking'|'updating'}

// ── Init ──
fetch('/api/platform').then(r=>r.json()).then(d=>{
  const platEl = document.getElementById('plat');
  platEl.textContent = 'Plataforma: ' + d.os + '/' + d.arch;
  document.getElementById('asset-name').textContent = 'yara-' + d.os + '-' + d.arch;
}).catch(()=>{});

loadRepos();

// Update asset preview
document.getElementById('in-app').addEventListener('input', updateAssetPreview);
document.getElementById('in-owner').addEventListener('input', updateRepoExample);
document.getElementById('in-repo').addEventListener('input', updateRepoExample);

function updateAssetPreview() {
  const app = document.getElementById('in-app').value || 'yara';
  fetch('/api/platform').then(r=>r.json()).then(d=>{
    document.getElementById('asset-name').textContent = app + '-' + d.os + '-' + d.arch;
  }).catch(()=>{});
}
function updateRepoExample() {
  const owner = document.getElementById('in-owner').value || 'mfloresz';
  const repo = document.getElementById('in-repo').value || 'yara';
  document.getElementById('asset-preview').querySelector('small').textContent = 'junto al binario o en PATH';
}

// ── Global SSE ──
const globalLog = document.getElementById('global-log');
const globalSrc = new EventSource('/api/events/global');
globalSrc.onmessage = function(e) {
  const d = document.createElement('div');
  d.className = 'line';
  const t = e.data;
  if (t.includes('ERROR')) d.className += ' err';
  else if (t.includes('Sistema')) d.className += ' system';
  else if (t.includes('Repositorio')) d.className += ' info';
  d.textContent = t;
  globalLog.appendChild(d);
  globalLog.scrollTop = globalLog.scrollHeight;
};

// ── Load Repos ──
function loadRepos() {
  fetch('/api/repos')
    .then(r => r.json())
    .then(data => {
      repos = data;
      renderRepos();
      startServicePolling();
    })
    .catch(err => {
      console.error('Error loading repos:', err);
      showToast('Error al cargar repositorios', 'error');
    });
}

// ── Render ──
function renderRepos() {
  const grid = document.getElementById('repos-grid');
  const empty = document.getElementById('empty-state');

  if (repos.length === 0) {
    grid.innerHTML = '';
    empty.style.display = 'block';
    return;
  }

  empty.style.display = 'none';
  grid.innerHTML = repos.map(repo => renderCard(repo)).join('');
}

function renderCard(repo) {
  const status = repoStatus[repo.id] || { status: 'idle' };
  const s = status.status;

  // Determine version display
  const hasCurrent = repo.current_version && repo.current_version !== '' && !repo.current_version.startsWith('error') && repo.current_version !== 'no detectada';
  const currentDisplay = hasCurrent ? repo.current_version : '—';
  const currentClass = hasCurrent ? 'value-current' : 'value-na';

  // Acción (check/update) badge
  const actionBadgeClass = s === 'checking' ? 'badge-checking' :
                     s === 'updating' ? 'badge-updating' :
                     s === 'done' ? 'badge-done' :
                     s === 'error' ? 'badge-error' : 'badge-idle';
  const actionBadgeText = s === 'checking' ? 'Verificando...' :
                    s === 'updating' ? 'Actualizando...' :
                    s === 'done' ? 'Completado' :
                    s === 'error' ? 'Error' : '—';

  // Service status badge
  const svcStatus = repoStatus[repo.id]?.serviceStatus || 'unknown';
  const svcBadgeClass = svcStatus === 'running' ? 'badge-running' : 'badge-stopped';
  const svcBadgeText = svcStatus === 'running' ? '● Activo' : '○ Inactivo';

  const isBusy = s === 'checking' || s === 'updating';

  const logId = 'log-' + repo.id.replace(/\//g, '_');

  return '<div class="repo-card" id="card-' + repo.id.replace(/\//g, '_') + '">' +
    '<div class="top">' +
      '<div class="title">' +
        '<span class="owner">' + escHtml(repo.owner) + '</span>/<a href="' + repoURL(repo) + '" target="_blank" rel="noopener">' + escHtml(repo.name) + '</a>' +
        ' <span style="font-weight:400;color:var(--text2);font-size:.85rem">(' + escHtml(repo.app_name) + ')</span>' +
      '</div>' +
      '<div style="display:flex;gap:.4rem;align-items:center">' +
        '<span class="badge ' + svcBadgeClass + '">' + svcBadgeText + '</span>' +
        (s !== 'idle' ? '<span class="badge ' + actionBadgeClass + '">' + (isBusy ? '<span class="spinner"></span>' : '') + actionBadgeText + '</span>' : '') +
      '</div>' +
    '</div>' +
    '<div class="versions">' +
      '<div class="version-box">' +
        '<div class="label">Versión Actual</div>' +
        '<div class="value ' + currentClass + '" id="current-' + repo.id.replace(/\//g, '_') + '">' + currentDisplay + '</div>' +
      '</div>' +
      '<div class="version-box">' +
        '<div class="label">Nueva Versión</div>' +
        '<div class="value value-new pending" id="newver-' + repo.id.replace(/\//g, '_') + '">—</div>' +
      '</div>' +
    '</div>' +
    '<div class="actions">' +
      '<button onclick="checkVersion(\'' + repo.id + '\')" ' + (isBusy ? 'disabled' : '') + '>' +
        (s === 'checking' ? '<span class="spinner"></span>' : '📡') + ' Verificar' +
      '</button>' +
      '<button class="primary" onclick="updateRepo(\'' + repo.id + '\')" ' + (isBusy ? 'disabled' : '') + '>' +
        (s === 'updating' ? '<span class="spinner"></span>' : '⬇') + ' Actualizar' +
      '</button>' +
      '<button onclick="restartService(\'' + repo.id + '\')" ' + (isBusy ? 'disabled' : '') + ' title="Reiniciar servicio">' +
        '🔄 Reiniciar' +
      '</button>' +
      '<button onclick="stopService(\'' + repo.id + '\')" ' + (isBusy || svcStatus !== 'running' ? 'disabled' : '') + ' title="Detener servicio">' +
        '⏹ Detener' +
      '</button>' +
      '<button onclick="startService(\'' + repo.id + '\')" ' + (isBusy || svcStatus === 'running' ? 'disabled' : '') + ' title="Iniciar servicio">' +
        '▶ Iniciar' +
      '</button>' +
      '<button class="danger" onclick="removeRepo(\'' + repo.id + '\')" ' + (isBusy ? 'disabled' : '') + '>' +
        '✕ Eliminar' +
      '</button>' +
    '</div>' +
    '<div id="log-' + repo.id.replace(/\//g, '_') + '" style="margin-top:.75rem;background:#010409;border:1px solid var(--border);border-radius:6px;padding:.5rem;height:0;overflow-y:auto;font-family:monospace;font-size:.75rem;line-height:1.5;transition:height .2s"></div>' +
  '</div>';
}

function escHtml(s) {
  const d = document.createElement('div');
  d.textContent = s;
  return d.innerHTML;
}

function repoURL(repo) {
  return 'https://github.com/' + repo.owner + '/' + repo.name;
}

// ── Per-repo SSE ──
const repoSSEs = {};

function safeId(id) {
  return id.replace(/[\/\s]/g, '_');
}

function connectRepoSSE(repoId) {
  if (repoSSEs[repoId]) return; // already connected

  const src = new EventSource('/api/events?id=' + encodeURIComponent(repoId));
  repoSSEs[repoId] = src;

  src.onmessage = function(e) {
    const sid = safeId(repoId);
    const logEl = document.getElementById('log-' + sid);
    if (!logEl) return;

    // Expand log on first message
    if (logEl.style.height === '0px' || logEl.style.height === '') {
      logEl.style.height = '200px';
    }

    const d = document.createElement('div');
    d.className = 'line';
    const t = e.data;

    // Check for VERSION_DATA
    if (t.includes('VERSION_DATA|')) {
      const parts = t.split('VERSION_DATA|');
      const data = parts[1].split('|');
      if (data.length >= 2) {
        const curEl = document.getElementById('current-' + sid);
        const newEl = document.getElementById('newver-' + sid);
        if (curEl) { curEl.textContent = data[0]; curEl.className = 'value value-current'; }
        if (newEl) { newEl.textContent = data[1]; newEl.className = 'value value-new'; }
      }
      return;
    }

    // Strip prefix for display
    const displayText = t.replace(/^\[repo-[^\]]+\]\s*/, '');

    if (displayText.includes('ERROR')) d.className += ' err';
    else if (displayText.includes('ACTUALIZACIÓN COMPLETADA')) d.className += ' info';
    else if (displayText.includes('✅')) d.className += ' info';
    else if (displayText.includes('⚠️')) d.className += ' warn';

    d.textContent = displayText;
    logEl.appendChild(d);
    logEl.scrollTop = logEl.scrollHeight;
  };

  src.onerror = function() {
    // Will auto-reconnect
  };
}

// ── Actions ──
function checkVersion(id) {
  if (!repoStatus[id]) repoStatus[id] = {};
  repoStatus[id].status = 'checking';
  renderRepos();
  connectRepoSSE(id);

  fetch('/api/repos/check?id=' + encodeURIComponent(id))
    .then(r => r.json())
    .then(data => {
      if (data.status !== 'checking') throw new Error(data.status);
      // Wait a bit then refresh
      setTimeout(() => {
        repoStatus[id].status = 'idle';
        renderRepos();
        loadRepos(); // refresh data
      }, 2000);
    })
    .catch(err => {
      repoStatus[id].status = 'error';
      renderRepos();
      showToast('Error al verificar: ' + err.message, 'error');
    });
}

function updateRepo(id) {
  if (!repoStatus[id]) repoStatus[id] = {};
  // Capturar la versión actual ANTES de empezar la actualización.
  // Sin esto, lastVersion = undefined y el poll detecta
  // "cambio" en la primera ejecución, marcando como completado
  // aunque el binario se esté descargando.
  const currentRepo = repos.find(r => r.id === id);
  repoStatus[id].lastVersion = currentRepo ? currentRepo.current_version : undefined;
  repoStatus[id].status = 'updating';
  // Marcar actualización activa para suprimir cambios de badge de servicio
  repoStatus[id]._updating = true;
  renderRepos();
  connectRepoSSE(id);

  fetch('/api/repos/update?id=' + encodeURIComponent(id))
    .then(r => r.json())
    .then(data => {
      if (data.status !== 'updating') throw new Error(data.status);
      // Poll until done
      const checkDone = setInterval(() => {
        fetch('/api/repos')
          .then(r => r.json())
          .then(allRepos => {
            const updated = allRepos.find(r => r.id === id);
            if (updated && updated.current_version && updated.current_version !== repoStatus[id]?.lastVersion) {
              clearInterval(checkDone);
              repoStatus[id].status = 'done';
              repoStatus[id].lastVersion = updated.current_version;
              repoStatus[id]._updating = false;
              repos = allRepos;
              renderRepos();
              setTimeout(() => {
                if (repoStatus[id]) {
                  repoStatus[id].status = 'idle';
                }
                renderRepos();
                fetchServiceStatus(id);
              }, 4000);
            }
          })
          .catch(() => {});
      }, 3000);
      // Safety timeout — si la actualización no termina, limpiar estado
      setTimeout(() => {
        clearInterval(checkDone);
        if (repoStatus[id] && repoStatus[id].status === 'updating') {
          repoStatus[id].status = 'error';
          repoStatus[id]._updating = false;
          renderRepos();
          showToast('La actualización tardó demasiado', 'error');
        }
      }, 120000);
    })
    .catch(err => {
      repoStatus[id].status = 'error';
      repoStatus[id]._updating = false;
      renderRepos();
      showToast('Error al actualizar: ' + err.message, 'error');
    });
}

function removeRepo(id) {
  if (!confirm('¿Eliminar este repositorio del dashboard?')) return;

  fetch('/api/repos/remove', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({id: id})
  })
  .then(r => {
    if (!r.ok) throw new Error('HTTP ' + r.status);
    // Close SSE
    if (repoSSEs[id]) {
      repoSSEs[id].close();
      delete repoSSEs[id];
    }
    delete repoStatus[id];
    loadRepos();
    showToast('Repositorio eliminado', 'success');
  })
  .catch(err => {
    showToast('Error al eliminar: ' + err.message, 'error');
  });
}

// ── Add Modal ──
function showAddModal() {
  document.getElementById('add-modal').classList.add('active');
  document.getElementById('modal-error').style.display = 'none';
  document.getElementById('in-owner').focus();
}

function hideAddModal() {
  document.getElementById('add-modal').classList.remove('active');
}

function addRepo() {
  const owner = document.getElementById('in-owner').value.trim();
  const name = document.getElementById('in-repo').value.trim();
  const appName = document.getElementById('in-app').value.trim();
  const asset = document.getElementById('in-asset').value.trim();
  const errorEl = document.getElementById('modal-error');

  if (!owner || !name || !appName) {
    errorEl.textContent = 'Completa todos los campos obligatorios';
    errorEl.style.display = 'block';
    return;
  }

  const payload = { owner, name, app_name: appName };
  if (asset) payload.asset = asset;

  fetch('/api/repos/add', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify(payload)
  })
  .then(r => {
    if (r.status === 409) throw new Error('El repositorio ya existe');
    if (!r.ok) throw new Error('HTTP ' + r.status);
    return r.json();
  })
  .then(data => {
    hideAddModal();
    // Clear fields
    document.getElementById('in-owner').value = '';
    document.getElementById('in-repo').value = '';
    document.getElementById('in-app').value = '';
    document.getElementById('in-asset').value = '';
    showToast('Repositorio añadido: ' + data.owner + '/' + data.name, 'success');
    loadRepos();
    // Auto-connect SSE for new repo
    connectRepoSSE(data.id);
  })
  .catch(err => {
    errorEl.textContent = err.message;
    errorEl.style.display = 'block';
  });
}

// ── Service status polling ──
let servicePollInterval = null;

function fetchServiceStatus(id) {
  fetch('/api/repos/status?id=' + encodeURIComponent(id))
    .then(r => r.json())
    .then(data => {
      if (!repoStatus[id]) repoStatus[id] = {};
      repoStatus[id].serviceStatus = data.status;

      // No actualizar el badge de servicio si hay una actualización activa
      if (repoStatus[id]._updating) return;

      // Update badge in DOM directly without full re-render
      const sid = safeId(id);
      const badgeEl = document.querySelector('#card-' + sid + ' .badge');
      if (badgeEl) {
        if (data.status === 'running') {
          badgeEl.className = 'badge badge-running';
          badgeEl.textContent = '● Activo';
        } else {
          badgeEl.className = 'badge badge-stopped';
          badgeEl.textContent = '○ Inactivo';
        }
      }
      // Update button states
      const cardEl = document.getElementById('card-' + sid);
      if (cardEl) {
        const btns = cardEl.querySelectorAll('.actions button');
        const isRunning = data.status === 'running';
        btns.forEach(btn => {
          if (btn.textContent.includes('Detener')) btn.disabled = !isRunning;
          if (btn.textContent.includes('Iniciar')) btn.disabled = isRunning;
        });
      }
    })
    .catch(() => {});
}

function startService(id) {
  if (!confirm('¿Iniciar el servicio ' + id + '?')) return;
  fetch('/api/repos/start', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({id: id})
  })
  .then(r => r.json())
  .then(data => {
    if (data.status === 'started' || data.status === 'already_running') {
      showToast('Servicio iniciado: ' + id, 'success');
      connectRepoSSE(id);
    } else {
      showToast('Error al iniciar: ' + (data.message || data.status), 'error');
    }
    fetchServiceStatus(id);
  })
  .catch(err => {
    showToast('Error al iniciar: ' + err.message, 'error');
  });
}

function stopService(id) {
  if (!confirm('¿Detener el servicio ' + id + '?')) return;
  fetch('/api/repos/stop', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({id: id})
  })
  .then(r => r.json())
  .then(data => {
    if (data.status === 'stopped') {
      showToast('Servicio detenido: ' + id, 'success');
    } else {
      showToast('Error al detener: ' + (data.message || data.status), 'error');
    }
    fetchServiceStatus(id);
  })
  .catch(err => {
    showToast('Error al detener: ' + err.message, 'error');
  });
}

function restartService(id) {
  if (!confirm('¿Reiniciar el servicio ' + id + '?')) return;
  fetch('/api/repos/restart', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({id: id})
  })
  .then(r => r.json())
  .then(data => {
    if (data.status === 'restarted') {
      showToast('Servicio reiniciado: ' + id, 'success');
      connectRepoSSE(id);
    } else {
      showToast('Error al reiniciar: ' + (data.message || data.status), 'error');
    }
    fetchServiceStatus(id);
  })
  .catch(err => {
    showToast('Error al reiniciar: ' + err.message, 'error');
  });
}

// Start periodic service status polling
function startServicePolling() {
  if (servicePollInterval) clearInterval(servicePollInterval);
  servicePollInterval = setInterval(() => {
    repos.forEach(repo => fetchServiceStatus(repo.id));
  }, 10000);
  // Also do an immediate fetch after repos load
  setTimeout(() => {
    repos.forEach(repo => fetchServiceStatus(repo.id));
  }, 500);
}

// ── Log / Toast ──
function clearLog() {
  globalLog.innerHTML = '<div class="line system">Registro limpiado.</div>';
}

function showToast(msg, type) {
  const el = document.getElementById('toast');
  el.textContent = msg;
  el.className = 'toast show ' + (type || '');
  setTimeout(() => { el.className = 'toast'; }, 4000);
}

// Close modal on overlay click
document.getElementById('add-modal').addEventListener('click', function(e) {
  if (e.target === this) hideAddModal();
});

// Enter key in modal
document.addEventListener('keydown', function(e) {
  if (e.key === 'Escape') hideAddModal();
  if (e.key === 'Enter' && document.getElementById('add-modal').classList.contains('active')) {
    addRepo();
  }
});
</script>
</body>
</html>`

func init() {
	_ = dashboardHTML
}

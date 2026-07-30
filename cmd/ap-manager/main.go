// AP Manager — Dashboard de actualización multi-repositorio
//
// Arquitectura:
//   cmd/ap-manager/main.go         — Punto de entrada
//   internal/api/handlers.go       — Handlers HTTP
//   internal/dashboard/html.go     — Frontend SPA embebido
//   internal/events/sse.go         — Broker de eventos SSE
//   internal/events/types.go       — Tipos de eventos estructurados
//   internal/github/client.go      — Cliente GitHub Releases API
//   internal/process/manager.go    — Gestor de procesos multiplataforma
//   internal/storage/state.go      — Persistencia de estado
//   internal/updater/updater.go    — Pipeline de actualización
//   platform/process_*.go          — Implementaciones por plataforma
package main

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"

	"ap-manager/internal/api"
	"ap-manager/internal/dashboard"
	"ap-manager/internal/events"
	"ap-manager/internal/github"
	"ap-manager/internal/process"
	"ap-manager/internal/storage"
	"ap-manager/internal/updater"
)

// Version se inyecta en build via ldflags: -X main.Version=$(VERSION)
var Version = "dev"

func getEnvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	reposFile := getEnvDefault("REPOS_FILE", "repos.json")
	port := getEnvDefault("PORT", ":8080")

	fmt.Println("=== AP Manager Dashboard ===")
	fmt.Printf("Versión: %s\n", Version)
	fmt.Printf("Plataforma: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Puerto: %s\n", port)
	fmt.Printf("Datos: %s\n", reposFile)

	// ── Initialize state store ──
	store, err := storage.NewStore(reposFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error al cargar estado: %v\n", err)
		os.Exit(1)
	}
	// Ensure file exists with current state
	store.Save()

	fmt.Printf("Repositorios: %d\n", len(store.List()))

	// ── Initialize services ──
	broker := events.NewBroker()
	ghClient := github.NewClient()
	procMan := process.NewManager(".") // PID files in CWD

	// Detect platform
	platOS := runtime.GOOS
	platArch := runtime.GOARCH

	// Detect Termux (Android)
	termuxPrefix := os.Getenv("PREFIX")
	if termuxPrefix != "" {
		fmt.Printf("Termux detectado: %s\n", termuxPrefix)
		platOS = "android"
	}

	pipeline := &updater.Pipeline{
		Broker:  broker,
		Store:   store,
		ProcMan: procMan,
		GitHub:  ghClient,
		OS:      platOS,
		Arch:    platArch,
	}

	// ── Add self repo if not present ──
	selfID := strings.ToLower("mfloresz" + "/" + "app-manager")
	if store.Find(selfID) == nil {
		selfRepo := storage.Repository{
			ID:             selfID,
			Owner:          "mfloresz",
			Name:           "app-manager",
			AppName:        "ap-manager",
			CurrentVersion: Version,
			PlatformOS:     platOS,
			PlatformArch:   platArch,
			Installed:      true,
			Status:         storage.StatusIdle,
		}
		if err := store.Add(selfRepo); err != nil {
			fmt.Fprintf(os.Stderr, "Error al añadir auto-repo: %v\n", err)
		} else {
			fmt.Printf("Auto-repo añadido: %s (%s)\n", selfID, Version)
		}
	} else {
		// Sync current version on every startup
		store.Update(selfID, func(r *storage.Repository) {
			r.CurrentVersion = Version
			r.PlatformOS = platOS
			r.PlatformArch = platArch
		})
	}
	store.Save()

	// ── Set up HTTP routes ──
	mux := http.NewServeMux()

	handler := api.NewHandler(store, broker, pipeline, procMan, Version)
	handler.RegisterRoutes(mux)

	// Dashboard SPA (catch-all)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(dashboard.HTML))
	})

	// ── Start server ──
	fmt.Println("Servidor iniciado en http://localhost" + port)
	if err := http.ListenAndServe(port, mux); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

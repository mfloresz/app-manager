#!/usr/bin/env bash
set -euo pipefail

# ================================================================
# Test script for AP Manager (multi-repo dashboard)
#
# Creates mock binaries and a mock GitHub API for each app,
# then starts the dashboard so you can:
#   - Add repos via the UI
#   - Check versions
#   - Update binaries per-repo
# ================================================================

TESTDIR="$(mktemp -d)"
go_build_temp() { go build "$@"; }
cleanup() { rm -rf "$TESTDIR"; kill "${UPDATER_PID:-}" "${MOCK_PID:-}" 2>/dev/null || true; }
trap cleanup EXIT

echo "=== Test Environment ==="
echo "Temp dir: $TESTDIR"

# --- Build updater (shipped entry point) ---
go build -o "$TESTDIR/ap-manager" ./cmd/ap-manager
echo "✓ AP Manager built"

# --- Detect platform ---
PLATFORM="$(go env GOOS)-$(go env GOARCH)"
echo "Platform: $PLATFORM"

# ---------------------------------------------------------------
# APP 1: yara (mock security scanner)
# ---------------------------------------------------------------
mkdir -p "$TESTDIR/yara-src"

# Old version
cat > "$TESTDIR/yara-src/main.go" <<'GOEOF'
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Print("yara 4.5.0")
		return
	}
	fmt.Println("yara 4.5.0 (old)")
}
GOEOF
go_build_temp -o "$TESTDIR/yara" "$TESTDIR/yara-src/main.go"
echo "✓ yara 4.5.0 (old) created"

# New version
cat > "$TESTDIR/yara-src/new_main.go" <<'GOEOF'
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Print("yara 4.6.0")
		return
	}
	fmt.Println("yara 4.6.0 (new)")
}
GOEOF
go_build_temp -o "$TESTDIR/yara-new" "$TESTDIR/yara-src/new_main.go"
echo "✓ yara 4.6.0 (new) built"

# ---------------------------------------------------------------
# APP 2: httpx (mock HTTP toolkit)
# ---------------------------------------------------------------
mkdir -p "$TESTDIR/httpx-src"

# Old version
cat > "$TESTDIR/httpx-src/main.go" <<'GOEOF'
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Print("httpx 1.6.0")
		return
	}
	fmt.Println("httpx 1.6.0 (old)")
}
GOEOF
go_build_temp -o "$TESTDIR/httpx" "$TESTDIR/httpx-src/main.go"
echo "✓ httpx 1.6.0 (old) created"

# New version
cat > "$TESTDIR/httpx-src/new_main.go" <<'GOEOF'
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Print("httpx 1.7.0")
		return
	}
	fmt.Println("httpx 1.7.0 (new)")
}
GOEOF
go_build_temp -o "$TESTDIR/httpx-new" "$TESTDIR/httpx-src/new_main.go"
echo "✓ httpx 1.7.0 (new) built"

# ---------------------------------------------------------------
# Mock GitHub API
# ---------------------------------------------------------------
mkdir -p "$TESTDIR/mock-api"
cat > "$TESTDIR/mock-api/main.go" <<'GOEOF'
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

type Asset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
}

type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

func main() {
	port := os.Args[1]

	// Map of owner/repo -> (tag, asset name, binary path)
	apps := map[string]struct {
		tag    string
		asset  string
		binary string
	}{
		"mfloresz/yara":  {"v4.6.0", os.Args[2], os.Args[3]},
		"projectdiscovery/httpx": {"v1.7.0", os.Args[4], os.Args[5]},
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Parse: /repos/{owner}/{name}/releases/latest
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/repos/"), "/")
		if len(parts) < 2 {
			http.Error(w, "bad path", 400)
			return
		}
		key := parts[0] + "/" + parts[1]

		app, ok := apps[key]
		if !ok {
			http.Error(w, "not found: "+key, 404)
			return
		}

		rel := Release{
			TagName: app.tag,
			Assets: []Asset{
				{
					Name:        app.asset,
					DownloadURL: fmt.Sprintf("http://localhost:%s/download/%s", port, key),
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rel)
	})

	http.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/download/")
		app, ok := apps[key]
		if !ok {
			http.Error(w, "not found", 404)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeFile(w, r, app.binary)
	})

	fmt.Printf("Mock GitHub API en puerto %s\n", port)
	fmt.Printf("  Endpoints:\n")
	for key, app := range apps {
		fmt.Printf("    /repos/%s/releases/latest -> %s (%s)\n", key, app.tag, app.asset)
	}
	http.ListenAndServe(":"+port, nil)
}
GOEOF
go_build_temp -o "$TESTDIR/mock-api" "$TESTDIR/mock-api/main.go"

ASSET_YARA="yara-${PLATFORM}"
ASSET_HTTPX="httpx-${PLATFORM}"

"$TESTDIR/mock-api" "9999" "$ASSET_YARA" "$TESTDIR/yara-new" "$ASSET_HTTPX" "$TESTDIR/httpx-new" &
MOCK_PID=$!
sleep 1
echo "✓ Mock GitHub API running (PID $MOCK_PID)"

# ---------------------------------------------------------------
# Start AP Manager with override GITHUB_API prefix
# ---------------------------------------------------------------
export REPOS_FILE="$TESTDIR/repos.json"
export PORT=":8080"
# Point the real binary's GitHub client at the local mock API
# (http://localhost:9999/repos/{owner}/{name}/releases/latest).
export GITHUB_API_PREFIX="http://localhost:9999"

# Pre-populate repos.json with initial repos pointing to mock
cat > "$TESTDIR/repos.json" <<JSON
[
  {
    "id": "mfloresz/yara",
    "owner": "mfloresz",
    "name": "yara",
    "app_name": "yara",
    "current_version": ""
  },
  {
    "id": "projectdiscovery/httpx",
    "owner": "projectdiscovery",
    "name": "httpx",
    "app_name": "httpx",
    "current_version": ""
  }
]
JSON

# Copy to the working dir too
cp "$TESTDIR/yara" "$TESTDIR/httpx" "$TESTDIR/"

cd "$TESTDIR"
"$TESTDIR/ap-manager" &
UPDATER_PID=$!
sleep 1

echo ""
echo "================================================"
echo "  TODO LISTO — abre http://localhost:8080"
echo "================================================"
echo ""
echo "Repos pre-cargados:"
echo "  - mfloresz/yara       (bin: yara,       asset: $ASSET_YARA)"
echo "  - projectdiscovery/httpx (bin: httpx,  asset: $ASSET_HTTPX)"
echo ""
echo "Mock API: http://localhost:9999"
echo ""
echo "Para probar:"
echo "  1. Abre http://localhost:8080"
echo "  2. Haz clic en 'Verificar' en cada repo"
echo "  3. Luego 'Actualizar' para actualizar uno por uno"
echo "  4. También puedes añadir más repos desde la UI"
echo ""
echo "Presiona Ctrl+C para detener todo"
echo ""

cd - >/dev/null

# Wait — let user interact
wait
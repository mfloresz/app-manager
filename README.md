# AP Manager

**AP Manager** is a multi-repository dashboard application that allows users to manage, monitor, and update applications distributed across multiple GitHub repositories from a single interface. It automates version checking, binary downloading, and process management for hosted tools.

---

## Purpose

AP Manager exists to provide a centralized web dashboard for tracking and updating applications that are distributed as GitHub releases. Instead of manually checking each repository for new versions, downloading binaries, and restarting processes, AP Manager handles the full lifecycle through a web UI and REST API.

## Repository Type

- **Web Application** — Go backend with an embedded SPA frontend
- **CLI/Dashboard Tool** — Serves a web dashboard and exposes a REST API
- **Cross-platform Build System** — Builds binaries for Linux, macOS, and Android

## Structure Overview

```text
app-manager/
├── cmd/ap-manager/
│   └── main.go                  # Application entry point
├── internal/
│   ├── api/
│   │   └── handlers.go          # REST API HTTP handlers
│   ├── dashboard/
│   │   └── html.go              # Embedded SPA (web dashboard)
│   ├── events/
│   │   ├── sse.go               # SSE (Server-Sent Events) broker
│   │   └── types.go             # Structured event types
│   ├── github/
│   │   └── client.go            # GitHub Releases API client
│   ├── process/
│   │   ├── manager.go           # Platform-abstracted process manager
│   │   ├── process_linux.go     # Linux process handling (PID files)
│   │   ├── process_darwin.go    # macOS process handling
│   │   ├── process_termux.go    # Android/Termux process handling
│   │   └── process_windows.go   # Windows process handling
│   ├── storage/
│   │   └── state.go             # Persistent state (repos.json)
│   └── updater/
│       └── updater.go           # Update pipeline orchestration
├── .github/
│   └── workflows/
│       └── build.yml            # GitHub Actions CI/CD (build + release)
├── bin/                         # Compiled binary output directory
├── cmd/                         # Command entry points
├── home/                        # Project notes and changelog
├── internal/                    # Application packages
├── .gitignore
├── AGENTS.md                    # Agent instructions (release workflow)
├── Makefile                     # Build automation
<<<<<<< HEAD
├── repos.json                   # Repository configuration
└── test_updater.sh              # Test script
=======
├── ap-manager                   # (build artifact — gitignored)
├── go.mod                       # Go module definition (go 1.26.4)
├── go.sum                       # Go module checksums
├── repos.json                   # Repository configuration (runtime state)
├── test_updater.sh              # Integration test script with mock API
├── updater.go                   # (build artifact — gitignored)
└── README.md
>>>>>>> c099e86 (Update README.md)
```

## Key Areas

| Area | Purpose |
| ---- | ------- |
| `cmd/ap-manager/` | Application entry point (`main.go`) |
| `internal/api/` | REST API handlers for repo CRUD, version checks, and updates |
| `internal/dashboard/` | Embedded single-page application (HTML/JS/CSS served inline) |
| `internal/events/` | SSE broker and structured event types for real-time updates |
| `internal/github/` | GitHub Releases API client for fetching latest versions and assets |
| `internal/process/` | Platform-abstracted process management using PID files |
| `internal/storage/` | Persistent state management backed by `repos.json` |
| `internal/updater/` | Full update pipeline: find binary → fetch release → download → replace → restart |
| `.github/workflows/` | CI/CD: multi-platform builds and GitHub Release automation |
| `Makefile` | Build targets for all supported platforms |
| `repos.json` | Runtime configuration: tracked repos, versions, and status |

## Setup / Usage

### Prerequisites

- **Go** 1.26 or later
- **Git**
- **curl** or **wget** (for downloading release assets)

### Installation

1. **Clone the repository**

   ```bash
   git clone <repository-url>
   cd app-manager
   ```

2. **Build the application**

   ```bash
   make build
   ```

   This compiles for `linux/amd64` and places the binary at `bin/ap-manager-linux-amd64-dev`.

3. **Configure repositories**

   Edit `repos.json` to add the GitHub repositories you want to manage:

   ```json
   [
     {
       "id": "owner/repo",
       "owner": "owner",
       "name": "repo",
       "app_name": "binary-name",
       "current_version": "",
       "latest_version": "",
       "status": "idle",
       "installed": false
     }
   ]
   ```

4. **Run the application**

   ```bash
   ./bin/ap-manager-linux-amd64-dev
   ```

   Or use the `run` Make target:

   ```bash
   make run
   ```

5. **Open the dashboard**

   Navigate to `http://localhost:8080` in your browser.

### Environment Variables

| Variable | Default | Description |
| -------- | ------- | ----------- |
| `REPOS_FILE` | `repos.json` | Path to the repository configuration file |
| `PORT` | `:8080` | HTTP server port |
| `GITHUB_API_PREFIX` | *(none)* | Override for GitHub API endpoint (useful for proxies or testing) |

### Development Mode

```bash
make dev
```

This runs the application directly with `go run`, useful for development without compiling first.

## Workflows

### Adding a Repository

1. Open the dashboard at `http://localhost:8080`
2. Use the "Add Repository" form to enter the GitHub owner, repo name, and binary name
3. Save — the repo is persisted to `repos.json`

### Checking for Updates

1. From the dashboard, click **Check Version** for any tracked repository
2. The app queries the GitHub Releases API for the latest release tag
3. The dashboard displays current vs. latest version comparison

### Updating an Application

1. Click **Update** for the target repository in the dashboard
2. AP Manager downloads the latest release asset matching the current platform
3. The binary is replaced and the process is restarted automatically
4. Real-time progress is streamed via SSE events

### Building for Multiple Platforms

```bash
# Single platform
make build               # linux/amd64
make linux-arm64         # linux/arm64
make linux-armv7         # linux/arm/v7
make darwin-amd64        # macOS/amd64 (Intel)
make darwin-arm64        # macOS/arm64 (Apple Silicon)
make android             # Android/arm64 (Termux)
make android-armv7       # Android/arm/v7 (requires Android NDK)

# All platforms
make all

# Compress binaries with UPX
make compress
```

### Release Process

1. Update the version number in the codebase
2. Commit changes: `git commit -am "chore: prepare release vX.Y.Z"`
3. Create an annotated tag: `git tag -a vX.Y.Z -m "Release vX.Y.Z"`
4. Push the tag: `git push origin main --tags`
5. GitHub Actions automatically builds binaries for all platforms and creates a release

## Dependencies

### External Dependencies

| Dependency | Purpose |
| ---------- | ------- |
| Go 1.26+ | Runtime and build system |
| GitHub Releases API | Source of truth for version tags and release assets |
| UPX (optional) | Binary compression for smaller release artifacts |

### Go Module Dependencies

Managed via `go.mod` and `go.sum`. Run `go mod tidy` to synchronize.

## API Endpoints

The REST API provides endpoints for programmatic access:

| Method | Endpoint | Description |
| ------ | -------- | ----------- |
| `GET` | `/repos` | List all tracked repositories |
| `POST` | `/repos` | Add a new repository |
| `PUT` | `/repos/{id}` | Update repository configuration |
| `DELETE` | `/repos/{id}` | Remove a repository |
| `GET` | `/repos/{id}/version` | Check latest version for a repository |
| `POST` | `/repos/{id}/update` | Trigger an update for a repository |
| `GET` | `/events` | SSE endpoint for real-time event stream |

## Testing

Run the integration test script, which creates a mock GitHub API and test binaries:

```bash
./test_updater.sh
```

This script:
- Builds mock application binaries (old and new versions)
- Starts a mock GitHub API server
- Pre-populates `repos.json` with test repositories
- Starts AP Manager against the mock environment
- Opens the dashboard for manual testing of add/check/update flows

## Documentation

- **Changelog**: `home/misael/Dev/app-manager/CHANGELOG.md`
- **Agent Instructions**: `AGENTS.md` (release workflow and conventions)
- **CI/CD Configuration**: `.github/workflows/build.yml`

## Maintenance Notes

### Conventions

- **Version tags** must use the `v` prefix (e.g., `v0.2.1`) to trigger the CI release pipeline. Tags like `0.2.1` (without `v`) will **not** trigger builds.
- **Annotated tags only**: `git tag -a vX.Y.Z -m "Release vX.Y.Z"`. Never use lightweight tags.
- **Changelog format** follows the Keep a Changelog-inspired structure with sections for Breaking Changes, What's New, Fixes, and Housekeeping.
- **Release notes** must always be written in English.

### Build Artifacts

The following files are build artifacts and are gitignored:
- `bin/ap-manager-*` — compiled binaries
- `ap-manager` — local build output (root level)
- `updater` — local build output (root level)
- `repos.json` — runtime state file (should not be committed with local data)

### Platform Support

| Platform | Architecture | Make Target | Notes |
| -------- | ------------ | ----------- | ----- |
| Linux | amd64 | `make build` | Default build target |
| Linux | arm64 | `make linux-arm64` | ARM64 servers |
| Linux | armv7 | `make linux-armv7` | 32-bit ARM devices |
| macOS | amd64 | `make darwin-amd64` | Intel Macs |
| macOS | arm64 | `make darwin-arm64` | Apple Silicon |
| Android | arm64 | `make android` | Termux environments |
| Android | armv7 | `make android-armv7` | Requires Android NDK and cross-compiler |

### Known Limitations

- Android armv7 builds require the Android NDK and a configured cross-compiler (`armv7a-linux-androideabi21-clang`)
- The embedded dashboard SPA is served inline — no separate frontend build step
- SSE is used for real-time updates; WebSocket support is planned for a future release
- Process management uses PID files rather than OS-level process tracking

## License

[License information to be added]

## Support

For issues, feature requests, and support, please [create an issue](https://github.com/mfloresz/app-manager/issues) in the repository.

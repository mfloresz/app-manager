# AP Manager

AP Manager is a multi-repository dashboard application that allows users to manage and monitor multiple GitHub-hosted applications from a single interface. It provides version checking, binary updating, and real-time status monitoring.

## Overview

AP Manager serves as a centralized dashboard for managing applications distributed across different GitHub repositories. It automates the process of:
- Adding and managing repositories
- Checking the latest versions of managed applications
- Updating application binaries
- Monitoring application status and health
- Receiving real-time updates via Server-Sent Events (SSE)

## Features

- **Repository Management**: Add, remove, and monitor multiple GitHub repositories
- **Version Control**: Automatically check for updates and compare with current versions
- **Binary Management**: Download and install latest application binaries
- **Real-time Updates**: Live status monitoring via WebSocket-like SSE
- **Cross-Platform**: Build binaries for Linux, macOS, Android, and various architectures
- **REST API**: Backend API for dashboard communication
- **Web Dashboard**: Rich web interface for management
- **Multi-process**: Safe process management and monitoring

## Repository Structure

```
ap-manager/
├── bin/                         # Compiled binaries
├── cmd/                         # Command line interface
│   └── ap-manager/             # Main entry point
├── internal/                    # Application internals
│   ├── api/                     # REST API handlers
│   │   └── handlers.go         # API implementation
│   ├── dashboard/               # Web dashboard
│   │   └── html.go             # Dashboard HTML templates
│   ├── events/                  # Event system (SSE)
│   │   ├── sse.go              # SSE server implementation
│   │   └── types.go            # Event data types
│   ├── github/                  # GitHub integration
│   │   └── client.go           # GitHub API client
│   ├── process/                 # Application process management
│   │   ├── manager.go          # Process manager
│   │   ├── process_*.go        # Platform-specific process handlers
│   ├── storage/                 # Persistent state storage
│   │   └── state.go            # State management
│   └── updater/                 # Update logic (empty - to be implemented)
├── .github/                     # GitHub Actions CI/CD
│   └── workflows/              # CI workflows
│       └── build.yml           # Build automation
├── bin/                         # Build output directory
├── go.mod                       # Go module configuration
├── Makefile                     # Build automation
├── repos.json                   # Repository configuration
└── test_updater.sh              # Test script
```

## Quick Start

### Prerequisites

- Go 1.26+
- Git
- curl or wget (for downloading)

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

   The binary will be placed in `bin/ap-manager-linux-amd64-dev`

3. **Run the application**
   ```bash
   ./bin/ap-manager-linux-amd64-dev
   ```

### Development Mode

For development with hot reload, use:
```bash
make dev
```

### Available Commands

Run `make help` to see all available targets:

- `build`: Compile for Linux/amd64
- `linux-arm64`: Compile for Linux/arm64
- `linux-armv7`: Compile for Linux/arm (v7)
- `darwin-amd64`: Compile for macOS/amd64 (Intel)
- `darwin-arm64`: Compile for macOS/arm64 (Apple Silicon)
- `android`: Compile for Android/arm64 (Termux)
- `android-armv7`: Compile for Android/arm (v7)
- `all`: Build for all platforms
- `compress`: Compress binaries with UPX
- `run`: Run the Linux/amd64 binary
- `clean`: Clean compiled binaries
- `dev`: Run in development mode

## Configuration

### Repository Configuration

The application uses `repos.json` to store repository configuration:

```json
[
  {
    "id": "mfloresz/yara",
    "owner": "mfloresz",
    "name": "yara",
    "app_name": "translator-server",
    "current_version": "v0.7.1",
    "latest_version": "",
    "status": "checking"
  }
]
```

### Environment Variables

- `REPOS_FILE`: Path to repos.json file (default: `repos.json`)
- `PORT`: Server port (default: `:8080`)
- `GITHUB_API_PREFIX`: Override for GitHub API endpoint

## Usage Examples

### Adding a Repository

The dashboard web interface allows you to:
1. Navigate to the "Add Repository" page
2. Enter repository details (owner, name, app name, etc.)
3. Save to automatically add to `repos.json`

### Checking Versions

From the dashboard, you can:
1. Click "Check Version" for any repository
2. View current vs latest version comparison
3. See update availability

### Updating Applications

To update an application:
1. From dashboard, click "Update" for the target repository
2. Application will download latest version
3. Binary will be installed and process restarted

## Running Tests

To run the test suite:

```bash
./test_updater.sh
```

This script creates a test environment with mock applications and APIs to demonstrate the functionality.

## Building for Different Platforms

Use the `make` targets to build for different platforms:

```bash
# Linux/ARM64
make linux-arm64

# macOS/ARM64 (Apple Silicon)
make darwin-arm64

# Android/ARM64 (Termux)
make android

# Build for all platforms
make all
```

## API Endpoints

The REST API provides endpoints for:
- **GET /repos**: List all repositories
- **POST /repos**: Add a new repository
- **PUT /repos/{id}**: Update repository
- **DELETE /repos/{id}**: Remove repository
- **GET /repos/{id}/version**: Check version
- **POST /repos/{id}/update**: Trigger update
- **GET /events**: SSE endpoint for real-time updates

## Docker (if applicable)

[Docker support documentation would go here]

## Roadmap

- [ ] WebSocket support (replacing SSE)
- [ ] User authentication
- [ ] Multi-user support
- [ ] Advanced filtering and search
- [ ] Import/export functionality
- [ ] Docker image generation
- [ ] More platform support (Windows, FreeBSD, etc.)

## Contributing

### Development Workflow

1. Create a feature branch
2. Make your changes
3. Test thoroughly
4. Submit a pull request

### Code Quality

- Follow Go idiomatic style
- Use proper error handling
- Write tests for new functionality
- Maintain consistency with existing patterns

## License

[License information should be added here]

## Support

For issues and support, please [create an issue](https://github.com/owner/repo/issues) in the repository.

## Acknowledgements

- Special thanks to all contributors
- Built with Go and modern web technologies
- Inspired by multi-repository management tools
# Changelog

## [v0.5.0]

### What's new

* Added support for private GitHub repositories. Set the `GITHUB_TOKEN` environment variable (a fine-grained token with `Contents: read`, or a classic token with `repo` scope) and release lookups and asset downloads are authenticated against the GitHub API. Asset downloads now always go through the API asset endpoint (with `Accept: application/octet-stream`), which is the only path that serves private-repo assets; public repos behave as before. The token is read from the environment only — it is never written to `repos.json` — and also raises the API rate limit from 60 to 5000 requests/hour.
* Updates now preserve the running state of the app. An app that is not currently managed by AP Manager (stopped, or started outside the dashboard) can finally be updated: the binary is replaced in place without starting anything, and managed apps keep the previous stop → replace → restart flow unchanged. Stopping an app that was never started now responds cleanly instead of returning an error.

### Fixes

* Version checks against private repositories no longer fail with a misleading `HTTP 404`. GitHub answers 404 (not 401) for repos the caller cannot see; without a token the error now explicitly hints at configuring `GITHUB_TOKEN`.

## [v0.4.4]

### Fixes

* Fixed service console mixing manager/updater messages with app output. `/api/repos/log` now returns only service stdout/stderr (`app_output`), manager messages go to the global registry only, and clearing the global registry persists via `POST /api/events/clear` while preserving per-service output.
* Fixed process detection on Android/Termux always reporting `DETENIDO`. The PID record stored the `termux-chroot` wrapper path, but the recorded PID's `/proc/<pid>/exe` is `proot` (the wrapper script execs it), so the identity check could never match and every status poll deleted the PID file. The record now stores the real application binary and the Android identity check falls back to matching the wrapper's cmdline (which always embeds the launched command) once the start-time token rules out PID reuse.
* Fixed stopping apps on Android/Termux leaving the real app orphaned. `Stop` only signaled the `termux-chroot`/`proot` wrapper while the app runs as its child, so the child survived holding its ports and the next start failed to bind. `Stop` (graceful and force paths) now also signals the wrapper's direct children discovered via `/proc`.

## [v0.4.3]

### Fixes

* Fixed self-update on Android/Termux. The generated update helper hardcoded `/bin/sh`, which does not exist in the Termux filesystem (the shell lives under `$PREFIX/bin/sh`). The launcher now resolves a POSIX shell at runtime — preferring `$PREFIX/bin/sh`, then `PATH`, then `/bin/sh` — and embeds the same path as the script's shebang so the file stays self-identifying.

### Housekeeping

* Trimmed the release build matrix to `linux-amd64` and `android-armv7` only. Other targets (`linux-arm64`, `linux-armv7`, `darwin-amd64`, `darwin-arm64`, `android-arm64`) are kept as commented entries so they can be re-enabled without consulting git history.

> **Note:** self-update on Termux/arm64 will fail to find an asset until that target is re-enabled in the workflow. Users on that platform should download the release manually for now.

---

## [v0.4.2]

### Fixes

* Fixed Android/Termux binaries that call blocked syscalls such as `statx` when launched from the dashboard. Child processes now use the installed `termux-chroot`/`proot` compatibility layer when available, while preserving correct paths inside the chroot and tracking the wrapper process safely.

---

## [v0.4.1]

### Fixes

* Fixed Android/Termux apps crashing with `SIGSYS` on `statx` when launched via the dashboard. When `ap-manager` runs as a service its working directory is `/`, which is not writable for the Termux uid, so any app that opens relative paths (e.g. PocketBase's `pb_data/data.db`) fails on metadata syscalls. Child processes now inherit a writable working directory (`$PREFIX` or `$HOME`) on Android, matching the behavior users previously got by wrapping launches in `termux-chroot`.

---

## [v0.3.0]

### What's new

* Added automatic self-update support for downloading and replacing the application binary
* Added `install.sh` for simplified installation and initial CLI setup
* Added per-repository application consoles with captured process output
* Added repository owner and name editing
* Added automatic repository configuration support
* Added platform and architecture verification for update assets
* Added support for process management across Linux, macOS, Windows, and Termux

### Fixes

* Improved process identity and PID validation to avoid managing the wrong process
* Improved installer input validation and CLI verification
* Improved updater reliability and platform-specific asset handling

### Housekeeping

* Reorganized updater implementation under `internal/updater`
* Added extensive automated test coverage for the updater, process manager, storage, API, and GitHub client
* Updated CI to verify generated binaries using `--version`
* Removed the obsolete top-level updater implementation

---

### References

Previous version: https://github.com/mfloresz/app-manager/releases/tag/v0.4.1
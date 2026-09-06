# Changelog

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
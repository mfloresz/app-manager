# Changelog

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

Previous version: https://github.com/mfloresz/app-manager/releases/tag/v0.2.1
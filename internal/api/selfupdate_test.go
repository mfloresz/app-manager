package api

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"ap-manager/internal/updater"
)

// TestShellQuote verifies that shellQuote produces single-quoted values that
// round-trip through /bin/sh unchanged, including metacharacters.
func TestShellQuote(t *testing.T) {
	values := []string{
		"plain",
		"with space",
		"with'single'quotes",
		`with"double"quotes`,
		"with$dollar and `backtick`",
		`with\backslash`,
		"with;semicolon&and|pipe",
		"with*glob?[chars]",
		"with\nnewline",
		"https://github.com/mfloresz/app-manager/releases/download/v0.2.1/ap-manager-linux-amd64-v0.2.1?foo=1&bar=2",
		"/opt/my apps/ap-manager",
		"/tmp/it's here/ap-manager",
	}
	for i, v := range values {
		i, v := i, v
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			q := shellQuote(v)
			if !strings.HasPrefix(q, "'") || !strings.HasSuffix(q, "'") {
				t.Fatalf("shellQuote(%q) = %q: not wrapped in single quotes", v, q)
			}
			if runtime.GOOS != "windows" {
				// Round-trip through a real POSIX shell.
				cmd := exec.Command("/bin/sh", "-c", `printf '%s' `+q)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("sh round-trip failed for %q: %v (%s)", v, err, out)
				}
				if string(out) != v {
					t.Fatalf("sh round-trip mismatch: got %q, want %q (quoted: %q)", out, v, q)
				}
			}
		})
	}
}

// TestGenerateSelfUpdateScriptInvariants checks the hardened generated script:
// fail-fast options, escaped assignments, HTTP-failure download flags, step
// ordering, and that the backup is only removed after the liveness check.
func TestGenerateSelfUpdateScriptInvariants(t *testing.T) {
	binaryPath := "/opt/my apps/ap-manager"
	downloadURL := "https://github.com/mfloresz/app-manager/releases/download/v0.2.1/ap-manager-linux-amd64-v0.2.1?foo=1&bar=2"
	version := "v0.2.1"
	script, logPath := generateSelfUpdateScript(binaryPath, 1234, downloadURL, version, modeManual, "")
	t.Cleanup(func() { os.Remove(logPath) })

	checks := []struct {
		name string
		want string
	}{
		{"shebang", "#!/bin/sh"},
		{"fail fast", "set -eu"},
		{"binary assignment", "BINARY=" + shellQuote(binaryPath)},
		{"url assignment", "URL=" + shellQuote(downloadURL)},
		{"version assignment", "VERSION=" + shellQuote(version)},
		{"log assignment", "LOG=" + shellQuote(logPath)},
		{"pid assignment", "PID=1234"},
		{"mode manual", "MODE=" + shellQuote("manual")},
		{"service empty in manual", "SERVICE=" + shellQuote("")},
		{"curl fail+location", "curl -fsSL"},
		{"wget fallback", "wget -qO"},
		{"log redirect", `exec > "$LOG" 2>&1`},
		{"backup removal", `rm -f "${BINARY}.bak"`},
	}
	for _, c := range checks {
		if !strings.Contains(script, c.want) {
			t.Errorf("%s: script missing %q", c.name, c.want)
		}
	}

	// No raw double-quoted interpolation of the values (unescaped).
	for _, raw := range []string{
		`BINARY="` + binaryPath,
		`URL="` + downloadURL,
		`VERSION="` + version,
		`LOG="` + logPath,
	} {
		if strings.Contains(script, raw) {
			t.Errorf("script contains raw unescaped interpolation: %q", raw)
		}
	}

	// Ordering: validation and backup must precede the kill; the backup
	// removal must only happen after the liveness check passed.
	order := []struct {
		name  string
		first string
		last  string
	}{
		{"validation before kill", "Validating downloaded binary", "Killing PID"},
		{"backup before kill", "Backup created", "Killing PID"},
		{"liveness before backup removal", "New ap-manager is running", `rm -f "${BINARY}.bak"`},
	}
	for _, o := range order {
		i := strings.Index(script, o.first)
		j := strings.Index(script, o.last)
		if i < 0 || j < 0 {
			t.Errorf("%s: missing markers %q/%q", o.name, o.first, o.last)
			continue
		}
		if i > j {
			t.Errorf("%s: %q (%d) must appear before %q (%d)", o.name, o.first, i, o.last, j)
		}
	}
}

// TestUniqueTempPath verifies that generated temp paths are unique and carry
// the requested prefix/suffix.
func TestUniqueTempPath(t *testing.T) {
	a := uniqueTempPath("ap-manager-test-", ".log")
	b := uniqueTempPath("ap-manager-test-", ".log")
	t.Cleanup(func() { os.Remove(a) })
	t.Cleanup(func() { os.Remove(b) })
	if a == b {
		t.Fatalf("uniqueTempPath returned the same path twice: %s", a)
	}
	if !strings.HasPrefix(a, os.TempDir()) || !strings.HasSuffix(a, ".log") {
		t.Fatalf("unexpected path %q", a)
	}
}

// TestCompareVersions covers the dotted numeric version comparison helper.
func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"1.2.3", "1.2.3", 0, true},
		{"1.2.3", "1.2.4", -1, true},
		{"1.10.0", "1.9.0", 1, true},
		{"1.2", "1.2.0", 0, true},
		{"1.2.3", "garbage", 0, false},
		{"", "1.0", 0, false},
		{"1..3", "1.0.3", 0, false},
		{"1.2.4-rc1", "1.2.4", 0, false},
	}
	for _, tt := range tests {
		got, ok := compareVersions(tt.a, tt.b)
		if got != tt.want || ok != tt.ok {
			t.Errorf("compareVersions(%q, %q) = (%d, %v), want (%d, %v)", tt.a, tt.b, got, ok, tt.want, tt.ok)
		}
	}
}

// TestDecideSelfUpdate covers the conservative self-update version gate.
func TestDecideSelfUpdate(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    updateDecision
	}{
		{"equal with v prefix", "v1.2.3", "1.2.3", updateNoop},
		{"exact equal", "v1.2.3", "v1.2.3", updateNoop},
		{"prefix normalization", "Version 1.2.3", "v1.2.3", updateNoop},
		{"different part counts equal", "1.2", "1.2.0", updateNoop},
		{"newer latest", "v1.9.0", "v1.10.0", updateProceed},
		{"minor update", "v1.2.3", "v1.3.0", updateProceed},
		{"latest older rejected", "v1.10.0", "v1.9.0", updateOlder},
		{"dev accepted", "dev", "v1.2.3", updateProceed},
		{"dev with unparseable latest", "dev", "nightly", updateUnknown},
		{"dev equal", "dev", "dev", updateUnknown},
		{"malformed current rejected", "garbage", "v1.2.3", updateUnknown},
		{"malformed latest rejected", "v1.2.3", "not-a-version", updateUnknown},
		{"prerelease suffix not parseable", "v1.2.3", "v1.2.4-rc1", updateUnknown},
		{"equal garbage is noop", "foo", "foo", updateNoop},
		{"empty latest", "v1.2.3", "", updateUnknown},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := decideSelfUpdate(tt.current, tt.latest); got != tt.want {
				t.Errorf("decideSelfUpdate(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

// TestHandlerPlatformPropagation verifies that NewHandler takes the effective
// platform from the pipeline (e.g. android for Termux) and falls back to the
// runtime values when the pipeline carries no platform.
func TestHandlerPlatformPropagation(t *testing.T) {
	tests := []struct {
		name     string
		pipeline *updater.Pipeline
		wantOS   string
		wantSfx  string
	}{
		{"termux android", &updater.Pipeline{OS: "android", Arch: "arm64"}, "android", "android-arm64"},
		{"linux", &updater.Pipeline{OS: "linux", Arch: "amd64"}, "linux", "linux-amd64"},
		{"empty pipeline falls back to runtime", &updater.Pipeline{}, runtime.GOOS, buildPlatformSuffix(runtime.GOOS, runtime.GOARCH)},
		{"nil pipeline falls back to runtime", nil, runtime.GOOS, buildPlatformSuffix(runtime.GOOS, runtime.GOARCH)},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(nil, nil, tt.pipeline, nil, "dev")
			if h.OS != tt.wantOS {
				t.Errorf("OS = %q, want %q", h.OS, tt.wantOS)
			}
			if got := buildPlatformSuffix(h.OS, h.Arch); got != tt.wantSfx {
				t.Errorf("buildPlatformSuffix(%q, %q) = %q, want %q", h.OS, h.Arch, got, tt.wantSfx)
			}
		})
	}
}

// TestGenerateSelfUpdateScriptSystemdInvariants checks the supervised-mode
// script: systemd lifecycle markers, MainPID liveness, expected-state
// tolerance, and the stop/replace/start/verify ordering.
func TestGenerateSelfUpdateScriptSystemdInvariants(t *testing.T) {
	script, logPath := generateSelfUpdateScript("/opt/ap-manager", 1234, "https://example.com/dl", "v1.2.3", modeSystemdUser, "ap-manager.service")
	t.Cleanup(func() { os.Remove(logPath) })

	checks := []struct {
		name string
		want string
	}{
		{"mode systemd", "MODE=" + shellQuote("systemd")},
		{"service identity", "SERVICE=" + shellQuote("ap-manager.service")},
		{"systemctl stop", "systemctl --user stop"},
		{"systemctl start", "systemctl --user start"},
		{"mainpid liveness", "-p MainPID --value"},
		{"already stopped tolerated", "was already stopped"},
	}
	for _, c := range checks {
		if !strings.Contains(script, c.want) {
			t.Errorf("%s: script missing %q", c.name, c.want)
		}
	}

	// Ordering: stop before replace; start after replace; MainPID liveness
	// must pass before the backup is removed.
	order := []struct {
		name  string
		first string
		last  string
	}{
		{"stop before replace", "Stopping service", "Binary replaced"},
		{"start after replace", "Binary replaced", "Starting service"},
		{"liveness before backup removal", "restarted successfully", `rm -f "${BINARY}.bak"`},
	}
	for _, o := range order {
		i := strings.Index(script, o.first)
		j := strings.Index(script, o.last)
		if i < 0 || j < 0 {
			t.Errorf("%s: missing markers %q/%q", o.name, o.first, o.last)
			continue
		}
		if i > j {
			t.Errorf("%s: %q (%d) must appear before %q (%d)", o.name, o.first, i, o.last, j)
		}
	}
}

// TestDetectSupervisorMode covers the conservative systemd detection: every
// signal (INVOCATION_ID, XDG_RUNTIME_DIR, systemctl and systemd-run on PATH)
// must be present for supervised mode; any missing signal falls back to
// manual mode.
func TestDetectSupervisorMode(t *testing.T) {
	env := func(kv map[string]string) func(string) string {
		return func(k string) string { return kv[k] }
	}
	has := func(names ...string) func(string) bool {
		set := make(map[string]bool)
		for _, n := range names {
			set[n] = true
		}
		return func(n string) bool { return set[n] }
	}
	tests := []struct {
		name string
		env  map[string]string
		cmds []string
		want supervisorMode
	}{
		{"fully supervised", map[string]string{"INVOCATION_ID": "abc", "XDG_RUNTIME_DIR": "/run/user/1000"}, []string{"systemctl", "systemd-run"}, modeSystemdUser},
		{"no invocation id", map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000"}, []string{"systemctl", "systemd-run"}, modeManual},
		{"no xdg runtime dir", map[string]string{"INVOCATION_ID": "abc"}, []string{"systemctl", "systemd-run"}, modeManual},
		{"no systemctl", map[string]string{"INVOCATION_ID": "abc", "XDG_RUNTIME_DIR": "/run/user/1000"}, []string{"systemd-run"}, modeManual},
		{"no systemd-run", map[string]string{"INVOCATION_ID": "abc", "XDG_RUNTIME_DIR": "/run/user/1000"}, []string{"systemctl"}, modeManual},
		{"no environment", map[string]string{}, nil, modeManual},
	}
	for _, tt := range tests {
		got := detectSupervisorMode(env(tt.env), has(tt.cmds...))
		if got != tt.want {
			t.Errorf("%s: detectSupervisorMode = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestSystemdRunArgs verifies the systemd-run command construction for the
// supervised helper launch.
func TestSystemdRunArgs(t *testing.T) {
	got := systemdRunArgs("ap-manager-selfupdate-abc123.service", "/tmp/ap-manager-selfupdate-abc123.sh")
	want := []string{"--user", "--unit=ap-manager-selfupdate-abc123.service", "--collect", "/bin/sh", "/tmp/ap-manager-selfupdate-abc123.sh"}
	if len(got) != len(want) {
		t.Fatalf("systemdRunArgs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("systemdRunArgs = %v, want %v", got, want)
		}
	}
}

// TestSystemdUnitName verifies the transient unit name is derived from the
// unique script path (same random suffix, .service extension).
func TestSystemdUnitName(t *testing.T) {
	got := systemdUnitName("/tmp/ap-manager-selfupdate-abc123.sh")
	if want := "ap-manager-selfupdate-abc123.service"; got != want {
		t.Errorf("systemdUnitName = %q, want %q", got, want)
	}
}

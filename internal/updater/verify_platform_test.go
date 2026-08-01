package updater

import (
	"os"
	"path/filepath"
	"testing"
)

// Synthetic executable headers (no real binaries, no execution).

// elfHeader builds a minimal ELF header of the given class (1=32, 2=64), data
// encoding (1=LSB, 2=MSB) and e_machine.
func elfHeader(class, data byte, machine uint16, bigEndian bool) []byte {
	h := make([]byte, 64)
	copy(h, []byte{0x7f, 'E', 'L', 'F', class, data})
	if bigEndian {
		h[18] = byte(machine >> 8)
		h[19] = byte(machine)
	} else {
		h[18] = byte(machine)
		h[19] = byte(machine >> 8)
	}
	return h
}

// peHeader builds a minimal PE file (DOS stub + PE signature + COFF machine).
func peHeader(machine uint16) []byte {
	h := make([]byte, 512)
	copy(h, []byte{'M', 'Z'})
	h[0x3c] = 0x80 // e_lfanew -> PE signature at 0x80
	copy(h[0x80:], []byte{'P', 'E', 0, 0})
	h[0x84] = byte(machine) // COFF machine (little-endian)
	h[0x85] = byte(machine >> 8)
	return h
}

// peMZOnly builds a file with only an MZ magic and no valid PE signature.
func peMZOnly() []byte {
	h := make([]byte, 128)
	copy(h, []byte{'M', 'Z'})
	h[0x3c] = 0x80 // e_lfanew points at zero bytes: no "PE\0\0"
	return h
}

// machOThinHeader builds a thin 64-bit Mach-O header (little-endian) with the
// given cputype.
func machOThinHeader(cputype uint32) []byte {
	h := make([]byte, 64)
	h[0], h[1], h[2], h[3] = 0xcf, 0xfa, 0xed, 0xfe // MH_MAGIC_64, little-endian storage
	h[4] = byte(cputype)
	h[5] = byte(cputype >> 8)
	h[6] = byte(cputype >> 16)
	h[7] = byte(cputype >> 24)
	return h
}

// machOFatHeader builds a 32-bit fat/universal Mach-O header (big-endian)
// with one entry per cputype, padded to at least minExecutableSize bytes.
func machOFatHeader(cputypes ...uint32) []byte {
	n := len(cputypes)
	raw := 8 + n*20
	if raw < minExecutableSize {
		raw = minExecutableSize
	}
	h := make([]byte, raw)
	h[0], h[1], h[2], h[3] = 0xca, 0xfe, 0xba, 0xbe // FAT_MAGIC
	h[4] = byte(n >> 24)
	h[5] = byte(n >> 16)
	h[6] = byte(n >> 8)
	h[7] = byte(n)
	for i, ct := range cputypes {
		off := 8 + i*20
		h[off] = byte(ct >> 24)
		h[off+1] = byte(ct >> 16)
		h[off+2] = byte(ct >> 8)
		h[off+3] = byte(ct)
	}
	return h
}

// TestVerifyDownloadForPlatform covers architecture-aware validation with
// synthetic headers: accepted/rejected ELF, PE, Mach-O thin/fat, Android-as-
// ELF, architecture aliases, OS mismatches, scripts, and malformed headers.
func TestVerifyDownloadForPlatform(t *testing.T) {
	tests := []struct {
		name         string
		data         []byte
		expectedOS   string
		expectedArch string
		wantErr      bool
	}{
		// ELF
		{"elf amd64 linux", elfHeader(2, 1, 62, false), "linux", "amd64", false},
		{"elf amd64 alias x86_64", elfHeader(2, 1, 62, false), "linux", "x86_64", false},
		{"elf arm64 android", elfHeader(2, 1, 183, false), "android", "arm64", false},
		{"elf arm64 alias aarch64", elfHeader(2, 1, 183, false), "linux", "aarch64", false},
		{"elf arm expected armv7", elfHeader(2, 1, 40, false), "linux", "armv7", false},
		{"elf 386 expected i686", elfHeader(1, 1, 3, false), "linux", "i686", false},
		{"elf big endian arm64", elfHeader(2, 2, 183, true), "linux", "arm64", false},
		{"elf arch mismatch", elfHeader(2, 1, 62, false), "linux", "arm64", true},
		{"elf unknown machine", elfHeader(2, 1, 0, false), "linux", "amd64", true},
		{"elf wrong os darwin", elfHeader(2, 1, 62, false), "darwin", "amd64", true},
		{"elf unknown os", elfHeader(2, 1, 62, false), "freebsd", "amd64", true},
		{"elf unknown arch", elfHeader(2, 1, 62, false), "linux", "mips", true},

		// PE
		{"pe amd64 windows", peHeader(0x8664), "windows", "amd64", false},
		{"pe 386 windows", peHeader(0x014c), "windows", "386", false},
		{"pe arm64 alias aarch64", peHeader(0xaa64), "windows", "aarch64", false},
		{"pe arch mismatch", peHeader(0x8664), "windows", "arm64", true},
		{"pe wrong os linux", peHeader(0x8664), "linux", "amd64", true},
		{"pe mz without signature", peMZOnly(), "windows", "amd64", true},

		// Mach-O thin
		{"macho thin amd64 darwin", machOThinHeader(0x01000007), "darwin", "amd64", false},
		{"macho thin arm64 alias aarch64", machOThinHeader(0x0100000c), "darwin", "aarch64", false},
		{"macho thin 386", machOThinHeader(7), "darwin", "386", false},
		{"macho thin arch mismatch", machOThinHeader(0x0100000c), "darwin", "amd64", true},
		{"macho thin wrong os linux", machOThinHeader(0x01000007), "linux", "amd64", true},

		// Mach-O fat
		{"macho fat contains amd64", machOFatHeader(0x0100000c, 0x01000007), "darwin", "amd64", false},
		{"macho fat contains 386", machOFatHeader(7, 12), "darwin", "386", false},
		{"macho fat no match", machOFatHeader(0x0100000c), "darwin", "amd64", true},

		// Scripts and generic behavior
		{"shebang arch neutral", []byte("#!/bin/sh\nexit 0\n"), "linux", "arm64", false},
		{"no platform requested accepts elf", elfHeader(2, 1, 62, false), "", "", false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "asset")
			if err := os.WriteFile(path, tt.data, 0755); err != nil {
				t.Fatal(err)
			}
			err := verifyDownloadForPlatform(path, tt.expectedOS, tt.expectedArch)
			if tt.wantErr && err == nil {
				t.Errorf("verifyDownloadForPlatform(%s) = nil, want error", tt.name)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("verifyDownloadForPlatform(%s) = %v, want nil", tt.name, err)
			}
		})
	}
}

// TestVerifyDownloadForPlatformErrorMessages verifies the errors identify the
// expected and detected target (feedback invariant).
func TestVerifyDownloadForPlatformErrorMessages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asset")
	if err := os.WriteFile(path, elfHeader(2, 1, 62, false), 0755); err != nil {
		t.Fatal(err)
	}
	err := verifyDownloadForPlatform(path, "linux", "arm64")
	if err == nil {
		t.Fatal("expected an architecture mismatch error")
	}
	for _, want := range []string{"amd64", "arm64"} {
		if !stringsContains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err.Error(), want)
		}
	}
}

// stringsContains is a tiny helper avoiding an import in this test file.
func stringsContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

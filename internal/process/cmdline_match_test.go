package process

import (
	"strings"
	"testing"
)

// TestArgvMentionsExecDirect verifies matching for a directly launched
// process (plain NUL-separated argv with absolute paths).
func TestArgvMentionsExecDirect(t *testing.T) {
	cmdline := "/data/data/com.termux/files/usr/bin/translator-server\x00--port\x008902\x00"
	execPath := "/data/data/com.termux/files/usr/bin/translator-server"
	if !argvMentionsExec(cmdline, execPath) {
		t.Error("direct argv must mention the recorded binary")
	}
	if argvMentionsExec(cmdline, "/data/data/com.termux/files/usr/bin/other-app") {
		t.Error("direct argv must not mention an unrelated binary")
	}
}

// TestArgvMentionsExecWrapperShC verifies the Termux wrapper case: the
// recorded PID is proot invoked as `sh -c "<app> <args>"`, so the whole
// command line lives inside a single argv element. This is the cmdline
// shape observed live on-device (PID 20516).
func TestArgvMentionsExecWrapperShC(t *testing.T) {
	tokens := []string{
		"/data/data/com.termux/files/usr/bin/proot",
		"--kill-on-exit",
		"-b", "/data:/data",
		"--cwd=/home",
		"sh",
		"-c",
		"translator-server --port 8902 --data-dir=/data/data/com.termux/files/home/yara_data",
	}
	cmdline := strings.Join(tokens, "\x00") + "\x00"
	execPath := "/data/data/com.termux/files/usr/bin/translator-server"
	if !argvMentionsExec(cmdline, execPath) {
		t.Error("wrapper `sh -c` cmdline must mention the recorded binary by basename")
	}
	if argvMentionsExec(cmdline, "/data/data/com.termux/files/usr/bin/ap-manager") {
		t.Error("wrapper cmdline must not mention an unrelated binary")
	}
}

// TestArgvMentionsExecEdgeCases covers empty inputs and quote trimming.
func TestArgvMentionsExecEdgeCases(t *testing.T) {
	if argvMentionsExec("", "/usr/bin/app") {
		t.Error("empty cmdline must not match")
	}
	if argvMentionsExec("app\x00", "") {
		t.Error("empty execPath must not match")
	}
	if !argvMentionsExec("'translator-server'\x00--port\x00", "/data/data/com.termux/files/usr/bin/translator-server") {
		t.Error("quoted argv word must match by basename")
	}
	// A bare relative name in argv (termux-chroot relativizes the binary
	// path before exec) must still match the absolute recorded path.
	if !argvMentionsExec("translator-server\x00--port\x008902\x00", "/data/data/com.termux/files/usr/bin/translator-server") {
		t.Error("relative argv[0] must match the absolute recorded binary by basename")
	}
}

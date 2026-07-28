//go:build windows

package process

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func processExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

func killProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

func killProcessForce(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

func startProcess(path string, args ...string) (int, error) {
	cmd := exec.Command(path, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("exec: %w", err)
	}
	return cmd.Process.Pid, nil
}

func lookPath(name string) (string, error) {
	return exec.LookPath(name)
}

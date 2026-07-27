//go:build darwin

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
	return process.Signal(syscall.Signal(0)) == nil
}

func killProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("SIGTERM: %w", err)
	}
	return nil
}

func killProcessForce(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	syscall.Kill(-pid, syscall.SIGKILL)
	return process.Signal(syscall.SIGKILL)
}

func startProcess(path string) (int, error) {
	cmd := exec.Command(path)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("exec: %w", err)
	}
	return cmd.Process.Pid, nil
}

func lookPath(name string) (string, error) {
	return exec.LookPath(name)
}

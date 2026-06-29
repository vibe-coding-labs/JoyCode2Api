//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func setProcAttrDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateProcess(proc *os.Process) error {
	return proc.Signal(syscall.SIGTERM)
}

func killProcess(proc *os.Process) error {
	return proc.Signal(syscall.SIGKILL)
}

func isProcessAlive(proc *os.Process) bool {
	return proc.Signal(syscall.Signal(0)) == nil
}

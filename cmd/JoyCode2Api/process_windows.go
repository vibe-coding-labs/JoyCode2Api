//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

const processQueryLimitedInformation = 0x1000

func setProcAttrDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

func terminateProcess(proc *os.Process) error {
	return exec.Command("taskkill", "/T", "/PID", fmt.Sprint(proc.Pid)).Run()
}

func killProcess(proc *os.Process) error {
	return exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprint(proc.Pid)).Run()
}

func isProcessAlive(proc *os.Process) bool {
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(proc.Pid))
	if err != nil {
		return false
	}
	syscall.CloseHandle(h)
	return true
}

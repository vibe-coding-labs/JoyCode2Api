//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const nssm = "nssm.exe"

func findNSSM() (string, error) {
	if path, err := exec.LookPath(nssm); err == nil {
		return path, nil
	}
	exe, _ := os.Executable()
	localNSSM := filepath.Join(filepath.Dir(exe), nssm)
	if _, err := os.Stat(localNSSM); err == nil {
		return localNSSM, nil
	}
	return "", fmt.Errorf("nssm.exe not found in PATH or alongside the binary\n  Download from https://nssm.cc and place in PATH")
}

func installService(port int) error {
	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine binary path: %w", err)
	}
	binPath, err = filepath.Abs(binPath)
	if err != nil {
		return fmt.Errorf("cannot resolve binary path: %w", err)
	}

	nssmPath, err := findNSSM()
	if err != nil {
		return err
	}

	serviceName := serviceLabel
	home, _ := os.UserHomeDir()
	logPath := filepath.Join(home, logDir)
	if err := os.MkdirAll(logPath, 0755); err != nil {
		return fmt.Errorf("cannot create log directory: %w", err)
	}

	appParams := fmt.Sprintf("serve --port %d --skip-validation", port)

	cmds := [][]string{
		{nssmPath, "install", serviceName, binPath},
		{nssmPath, "set", serviceName, "AppParameters", appParams},
		{nssmPath, "set", serviceName, "DisplayName", "JoyCode API Proxy"},
		{nssmPath, "set", serviceName, "Description", "JoyCode API Proxy - OpenAI/Anthropic compatible"},
		{nssmPath, "set", serviceName, "Start", "SERVICE_AUTO_START"},
		{nssmPath, "set", serviceName, "AppStdout", filepath.Join(logPath, "stdout.log")},
		{nssmPath, "set", serviceName, "AppStderr", filepath.Join(logPath, "stderr.log")},
		{nssmPath, "set", serviceName, "AppEnvironmentExtra", "HOME=" + home},
		{nssmPath, "start", serviceName},
	}

	for _, c := range cmds {
		out, err := exec.Command(c[0], c[1:]...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s failed: %s: %w", strings.Join(c, " "), string(out), err)
		}
	}

	fmt.Printf("Service installed and started.\n")
	fmt.Printf("  Name:   %s\n", serviceName)
	fmt.Printf("  Binary: %s\n", binPath)
	fmt.Printf("  Port:   %d\n", port)
	fmt.Printf("\nManage with:\n")
	fmt.Printf("  nssm status %s\n", serviceName)
	fmt.Printf("  nssm stop %s\n", serviceName)
	fmt.Printf("  nssm remove %s confirm\n", serviceName)
	return nil
}

func uninstallService() error {
	nssmPath, err := findNSSM()
	if err != nil {
		return err
	}

	serviceName := serviceLabel

	exec.Command(nssmPath, "stop", serviceName).Run()

	out, err := exec.Command(nssmPath, "remove", serviceName, "confirm").CombinedOutput()
	if err != nil {
		outStr := string(out)
		if strings.Contains(outStr, "Can't open service") {
			fmt.Println("Service not installed.")
			return nil
		}
		return fmt.Errorf("nssm remove failed: %s: %w", outStr, err)
	}

	fmt.Println("Service stopped and removed.")
	return nil
}

func serviceStatus() error {
	nssmPath, err := findNSSM()
	if err != nil {
		return err
	}

	serviceName := serviceLabel
	out, err := exec.Command(nssmPath, "status", serviceName).CombinedOutput()
	if err != nil {
		outStr := string(out)
		if strings.Contains(outStr, "Can't open service") {
			fmt.Println("Service not installed.")
			return nil
		}
		return fmt.Errorf("nssm status failed: %s: %w", outStr, err)
	}

	fmt.Printf("Service status: %s\n", strings.TrimSpace(string(out)))
	return nil
}

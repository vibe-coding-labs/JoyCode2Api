//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

func installService(port int) error {
	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine binary path: %w", err)
	}
	binPath, err = filepath.Abs(binPath)
	if err != nil {
		return fmt.Errorf("cannot resolve binary path: %w", err)
	}

	configDir := filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("cannot create systemd directory: %w", err)
	}

	unitData := struct {
		Description string
		BinaryPath  string
		Port        int
		HomeDir     string
	}{
		Description: "JoyCode API Proxy",
		BinaryPath:  binPath,
		Port:        port,
		HomeDir:     os.Getenv("HOME"),
	}

	unitTmpl := `[Unit]
Description={{.Description}}
After=network.target

[Service]
Type=simple
ExecStart={{.BinaryPath}} serve --port {{.Port}} --skip-validation
Restart=always
RestartSec=5
Environment=HOME={{.HomeDir}}

[Install]
WantedBy=default.target
`

	unitPath := filepath.Join(configDir, serviceLabel+".service")
	f, err := os.Create(unitPath)
	if err != nil {
		return fmt.Errorf("cannot create unit file: %w", err)
	}
	defer f.Close()

	t, err := template.New("unit").Parse(unitTmpl)
	if err != nil {
		return err
	}
	if err := t.Execute(f, unitData); err != nil {
		return err
	}

	cmds := [][]string{
		{"systemctl", "--user", "daemon-reload"},
		{"systemctl", "--user", "enable", serviceLabel + ".service"},
		{"systemctl", "--user", "start", serviceLabel + ".service"},
	}
	for _, args := range cmds {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("%s failed: %s: %w", args[0], string(out), err)
		}
	}

	fmt.Printf("Service installed and started.\n")
	fmt.Printf("  Unit:   %s\n", unitPath)
	fmt.Printf("  Port:   %d\n", port)
	return nil
}

func uninstallService() error {
	exec.Command("systemctl", "--user", "stop", serviceLabel+".service").Run()
	exec.Command("systemctl", "--user", "disable", serviceLabel+".service").Run()

	configDir := filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user")
	unitPath := filepath.Join(configDir, serviceLabel+".service")

	if _, err := os.Stat(unitPath); os.IsNotExist(err) {
		fmt.Println("Service not installed (unit file not found).")
		return nil
	}

	if err := os.Remove(unitPath); err != nil {
		return fmt.Errorf("cannot remove unit file: %w", err)
	}
	exec.Command("systemctl", "--user", "daemon-reload").Run()

	fmt.Println("Service stopped and removed.")
	return nil
}

func serviceStatus() error {
	configDir := filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user")
	unitPath := filepath.Join(configDir, serviceLabel+".service")
	if _, err := os.Stat(unitPath); os.IsNotExist(err) {
		fmt.Println("Service not installed.")
		return nil
	}

	out, err := exec.Command("systemctl", "--user", "status", serviceLabel+".service").CombinedOutput()
	if err != nil {
		fmt.Printf("Service status:\n%s\n", string(out))
		return nil
	}
	fmt.Printf("Service status:\n%s\n", string(out))
	return nil
}

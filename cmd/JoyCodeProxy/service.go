package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	"github.com/spf13/cobra"
)

const (
	serviceLabel = "com.joycode.proxy"
	plistName    = serviceLabel + ".plist"
	logDir       = ".joycode-proxy/logs"
)

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "Manage macOS launchd service",
	Long:  "Install, uninstall, or check status of JoyCode Proxy as a macOS launchd service.",
}

var serviceInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install and start the launchd service",
	RunE: func(cmd *cobra.Command, args []string) error {
		binPath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("cannot determine binary path: %w", err)
		}
		binPath, err = filepath.Abs(binPath)
		if err != nil {
			return fmt.Errorf("cannot resolve binary path: %w", err)
		}

		home, _ := os.UserHomeDir()
		logPath := filepath.Join(home, logDir)
		if err := os.MkdirAll(logPath, 0755); err != nil {
			return fmt.Errorf("cannot create log directory: %w", err)
		}

		plistData := struct {
			Label      string
			BinaryPath string
			Port       int
			HomeDir    string
			StdoutLog  string
			StderrLog  string
		}{
			Label:      serviceLabel,
			BinaryPath: binPath,
			Port:       servePort,
			HomeDir:    home,
			StdoutLog:  filepath.Join(logPath, "stdout.log"),
			StderrLog:  filepath.Join(logPath, "stderr.log"),
		}

		tmpl := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{.Label}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.BinaryPath}}</string>
        <string>serve</string>
        <string>--port</string>
        <string>{{.Port}}</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>ThrottleInterval</key>
    <integer>10</integer>
    <key>StandardOutPath</key>
    <string>{{.StdoutLog}}</string>
    <key>StandardErrorPath</key>
    <string>{{.StderrLog}}</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>HOME</key>
        <string>{{.HomeDir}}</string>
    </dict>
</dict>
</plist>`

		plistPath := filepath.Join(home, "Library", "LaunchAgents", plistName)
		f, err := os.Create(plistPath)
		if err != nil {
			return fmt.Errorf("cannot create plist: %w", err)
		}
		defer f.Close()

		t, err := template.New("plist").Parse(tmpl)
		if err != nil {
			return err
		}
		if err := t.Execute(f, plistData); err != nil {
			return err
		}

		out, err := exec.Command("launchctl", "load", plistPath).CombinedOutput()
		if err != nil {
			return fmt.Errorf("launchctl load failed: %s: %w", string(out), err)
		}

		fmt.Printf("Service installed and started.\n")
		fmt.Printf("  Label:   %s\n", serviceLabel)
		fmt.Printf("  Plist:   %s\n", plistPath)
		fmt.Printf("  Port:    %d\n", servePort)
		fmt.Printf("  Logs:    %s/\n", logPath)
		return nil
	},
}

var serviceUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Stop and remove the launchd service",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		plistPath := filepath.Join(home, "Library", "LaunchAgents", plistName)

		exec.Command("launchctl", "unload", plistPath).Run()

		if _, err := os.Stat(plistPath); os.IsNotExist(err) {
			fmt.Println("Service not installed (plist not found).")
			return nil
		}

		if err := os.Remove(plistPath); err != nil {
			return fmt.Errorf("cannot remove plist: %w", err)
		}

		fmt.Println("Service stopped and removed.")
		return nil
	},
}

var serviceStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check service status",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		plistPath := filepath.Join(home, "Library", "LaunchAgents", plistName)
		if _, err := os.Stat(plistPath); os.IsNotExist(err) {
			fmt.Println("Service not installed.")
			return nil
		}

		out, err := exec.Command("launchctl", "list").Output()
		if err != nil {
			return fmt.Errorf("launchctl list failed: %w", err)
		}

		found := false
		lines := splitLines(string(out))
		for _, line := range lines {
			if containsStr(line, serviceLabel) {
				fmt.Printf("Service status: %s\n", line)
				found = true
				break
			}
		}
		if !found {
			fmt.Println("Service installed but not running (plist exists, not in launchctl).")
			fmt.Println("Run 'JoyCodeProxy service install' to start it.")
		}

		fmt.Printf("\nLogs: %s/\n", filepath.Join(home, logDir))
		return nil
	},
}

func init() {
	serviceCmd.AddCommand(serviceInstallCmd)
	serviceCmd.AddCommand(serviceUninstallCmd)
	serviceCmd.AddCommand(serviceStatusCmd)
	serviceCmd.PersistentFlags().IntVarP(&servePort, "port", "p", 34891, "bind port")
	rootCmd.AddCommand(serviceCmd)
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

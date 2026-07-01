package main

import (
	"github.com/spf13/cobra"
)

const (
	serviceLabel = "com.joycode.proxy"
	plistName    = serviceLabel + ".plist"
	logDir       = ".joycode-proxy/logs"
)

var serviceCmd = &cobra.Command{
	Use:     "service",
	Short:   "管理后台服务（安装/卸载/状态）",
	Long:    "将 JoyCode Proxy 安装为系统后台服务，支持开机自启和崩溃自动重启。自动适配 macOS (launchd)、Linux (systemd) 和 Windows (NSSM)。",
	GroupID: "service",
}

var serviceInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "安装并启动后台服务",
	Long:  "将代理安装为系统后台服务。安装后自动启动，支持开机自启和崩溃自动重启。",
	Example: `  # 使用默认端口 34891 安装
  joycode-proxy service install

  # 指定端口
  joycode-proxy service install -p 8080`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return installService(servePort)
	},
}

var serviceUninstallCmd = &cobra.Command{
	Use:     "uninstall",
	Short:   "停止并移除后台服务",
	Example: `  joycode-proxy service uninstall`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return uninstallService()
	},
}

var serviceStatusCmd = &cobra.Command{
	Use:     "status",
	Short:   "查看服务运行状态",
	Example: `  joycode-proxy service status`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return serviceStatus()
	},
}

func init() {
	serviceCmd.AddCommand(serviceInstallCmd)
	serviceCmd.AddCommand(serviceUninstallCmd)
	serviceCmd.AddCommand(serviceStatusCmd)
	serviceCmd.PersistentFlags().IntVarP(&servePort, "port", "p", 34891, "绑定端口")
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

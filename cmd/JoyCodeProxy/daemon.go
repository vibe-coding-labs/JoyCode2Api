package main

import (
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/logrot"
)

const (
	daemonChildEnv    = "_JOYCODE_DAEMON_CHILD"
	daemonSupervisorEnv = "_JOYCODE_DAEMON_SUPERVISOR"
	daemonPortEnv     = "_JOYCODE_DAEMON_PORT"
	daemonVerboseEnv  = "_JOYCODE_DAEMON_VERBOSE"
	daemonSkipValEnv  = "_JOYCODE_DAEMON_SKIP_VALIDATION"
	pidFileName       = ".joycode-proxy/daemon.pid"
	logFileName       = ".joycode-proxy/logs/daemon.log"
	maxRestartDelay   = 30 * time.Second
	baseRestartDelay  = 1 * time.Second
)

var (
	daemonPIDFile string
	daemonLogFile string
)

var daemonCmd = &cobra.Command{
	Use:     "daemon",
	Short:   "以守护进程模式运行（崩溃自动重启）",
	Long:    "以后台守护进程模式启动代理服务。自动在后台运行，崩溃后自动重启（指数退避），日志写入文件。",
	GroupID: "service",
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "启动守护进程",
	Long: "启动 JoyCode Proxy 守护进程。Supervisor 进程监控子进程，" +
		"子进程崩溃时自动重启（1s → 2s → 4s → ... → 30s 指数退避）。",
	Example: `  # 使用默认端口启动
  joycode-proxy daemon start

  # 指定端口
  joycode-proxy daemon start -p 8080`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return startDaemon()
	},
}

var daemonStopCmd = &cobra.Command{
	Use:     "stop",
	Short:   "停止守护进程",
	Example: `  joycode-proxy daemon stop`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return stopDaemon()
	},
}

var daemonRestartCmd = &cobra.Command{
	Use:     "restart",
	Short:   "重启守护进程",
	Example: `  joycode-proxy daemon restart`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := stopDaemon(); err != nil {
			log.Printf("stop warning: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
		return startDaemon()
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:     "status",
	Short:   "查看守护进程状态",
	Example: `  joycode-proxy daemon status`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return daemonStatusCmdRun()
	},
}

var daemonLogsCmd = &cobra.Command{
	Use:     "logs",
	Short:   "查看守护进程日志（最后 N 行）",
	Example: `  joycode-proxy daemon logs
  joycode-proxy daemon logs -n 50`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return tailDaemonLogs(daemonLines)
	},
}

var daemonLines int

func init() {
	home, _ := os.UserHomeDir()
	daemonPIDFile = filepath.Join(home, pidFileName)
	daemonLogFile = filepath.Join(home, logFileName)

	daemonLogsCmd.Flags().IntVarP(&daemonLines, "lines", "n", 20, "显示最后 N 行日志")

	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonRestartCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	daemonCmd.AddCommand(daemonLogsCmd)
	daemonCmd.PersistentFlags().IntVarP(&servePort, "port", "p", 34891, "绑定端口")
	rootCmd.AddCommand(daemonCmd)
}

// startDaemon forks a supervisor process that monitors the server child.
func startDaemon() error {
	if os.Getenv(daemonChildEnv) != "" || os.Getenv(daemonSupervisorEnv) == "1" {
		return fmt.Errorf("already running as daemon (nested start not allowed)")
	}

	if pid, running := checkRunningDaemon(); running {
		return fmt.Errorf("daemon already running (PID %d). Use 'daemon restart' or 'daemon stop' first", pid)
	}

	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine binary path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(daemonLogFile), 0755); err != nil {
		return fmt.Errorf("cannot create log directory: %w", err)
	}

	env := append(os.Environ(),
		daemonSupervisorEnv+"=1",
		daemonPortEnv+"="+strconv.Itoa(servePort),
	)
	if verbose {
		env = append(env, daemonVerboseEnv+"=1")
	}
	if skipValidation {
		env = append(env, daemonSkipValEnv+"=1")
	}

	cmd := exec.Command(binPath)
	cmd.Env = env
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon supervisor: %w", err)
	}

	cmd.Process.Release()

	// Wait briefly for PID file to be written
	time.Sleep(200 * time.Millisecond)
	pidData, err := readPIDFile()
	if err != nil {
		fmt.Printf("Daemon supervisor started (PID %d, port %d)\n", cmd.Process.Pid, servePort)
	} else {
		fmt.Printf("Daemon started (PID %d, port %d)\n", pidData.PID, pidData.Port)
	}
	fmt.Printf("  Logs: %s\n", daemonLogFile)
	fmt.Printf("  PID:  %s\n", daemonPIDFile)
	return nil
}

func stopDaemon() error {
	pidData, err := readPIDFile()
	if err != nil {
		fmt.Println("Daemon not running (PID file not found).")
		return nil
	}

	proc, err := os.FindProcess(pidData.PID)
	if err != nil {
		removePIDFile()
		fmt.Println("Daemon not running (process not found).")
		return nil
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		removePIDFile()
		fmt.Printf("Daemon process %d not responding: %v\n", pidData.PID, err)
		return nil
	}

	done := make(chan error, 1)
	go func() {
		_, err := proc.Wait()
		done <- err
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		proc.Signal(syscall.SIGKILL)
	}

	removePIDFile()
	fmt.Printf("Daemon stopped (was PID %d)\n", pidData.PID)
	return nil
}

func daemonStatusCmdRun() error {
	pidData, err := readPIDFile()
	if err != nil {
		fmt.Println("Daemon not running (no PID file).")
		return nil
	}

	proc, err := os.FindProcess(pidData.PID)
	if err != nil {
		fmt.Printf("Daemon PID %d — process lookup failed\n", pidData.PID)
		return nil
	}

	if err := proc.Signal(syscall.Signal(0)); err != nil {
		fmt.Printf("Daemon PID %d — NOT running (stale PID file)\n", pidData.PID)
		removePIDFile()
		return nil
	}

	fmt.Printf("Daemon running (PID %d, port %d, started %s)\n",
		pidData.PID, pidData.Port, pidData.StartedAt)
	fmt.Printf("  Logs: %s\n", daemonLogFile)
	return nil
}

func tailDaemonLogs(n int) error {
	data, err := os.ReadFile(daemonLogFile)
	if err != nil {
		fmt.Println("No daemon log file found.")
		return nil
	}
	lines := splitLines(string(data))
	start := len(lines) - n
	if start < 0 {
		start = 0
	}
	for _, line := range lines[start:] {
		fmt.Println(line)
	}
	return nil
}

// runAsDaemonChild redirects logs to daemon log file with rotation.
func runAsDaemonChild() {
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, logDir)
	cfg := logrot.DefaultConfig(logDir, "daemon")
	rw, err := logrot.New(cfg)
	if err != nil {
		log.Fatalf("[daemon] cannot open log file: %v", err)
	}
	log.SetOutput(rw)
	slog.SetDefault(slog.New(slog.NewTextHandler(rw, &slog.HandlerOptions{Level: slog.LevelInfo})))
	log.Printf("[daemon] child process started (PID %d)", os.Getpid())
}

// RunSupervisor starts a supervisor loop that spawns and monitors the child process.
func RunSupervisor(port int) {
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, logDir)
	cfg := logrot.DefaultConfig(logDir, "daemon")
	rw, err := logrot.New(cfg)
	if err != nil {
		log.Fatalf("[supervisor] cannot open log: %v", err)
	}
	defer rw.Close()
	log.SetOutput(rw)
	slog.SetDefault(slog.New(slog.NewTextHandler(rw, &slog.HandlerOptions{Level: slog.LevelInfo})))

	log.Printf("[supervisor] starting (PID %d, port %d)", os.Getpid(), port)

	writePIDFile(daemonPID{
		PID:       os.Getpid(),
		Port:      port,
		StartedAt: time.Now().Format(time.RFC3339),
	})

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var mu sync.Mutex
	delay := baseRestartDelay

	for {
		binPath, err := os.Executable()
		if err != nil {
			log.Fatalf("[supervisor] cannot find binary: %v", err)
		}

		args := []string{"serve", "--port", strconv.Itoa(port)}
		if os.Getenv(daemonVerboseEnv) == "1" {
			args = append(args, "-v")
		}
		if os.Getenv(daemonSkipValEnv) == "1" {
			args = append(args, "--skip-validation")
		}

		cmd := exec.Command(binPath, args...)
		cmd.Env = append(os.Environ(), daemonChildEnv+"=1")
		cmd.Stdout = rw
		cmd.Stderr = rw
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		log.Printf("[supervisor] spawning child process")
		if err := cmd.Start(); err != nil {
			log.Printf("[supervisor] failed to start child: %v", err)
			mu.Lock()
			time.Sleep(delay)
			delay = minDuration(delay*2, maxRestartDelay)
			mu.Unlock()
			continue
		}

		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()

		select {
		case err := <-done:
			if err != nil {
				log.Printf("[supervisor] child crashed: %v — restarting in %v", err, delay)
			} else {
				log.Printf("[supervisor] child exited cleanly — restarting in %v", delay)
			}
			mu.Lock()
			time.Sleep(delay)
			delay = minDuration(delay*2, maxRestartDelay)
			mu.Unlock()

		case sig := <-sigCh:
			log.Printf("[supervisor] received %v — shutting down", sig)
			cmd.Process.Signal(syscall.SIGTERM)
			cmd.Wait()
			removePIDFile()
			log.Printf("[supervisor] stopped")
			return
		}
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// --- PID file management ---

type daemonPID struct {
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	StartedAt string `json:"started_at"`
}

func writePIDFile(data daemonPID) error {
	if err := os.MkdirAll(filepath.Dir(daemonPIDFile), 0755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(data, "", "  ")
	return os.WriteFile(daemonPIDFile, b, 0644)
}

func readPIDFile() (daemonPID, error) {
	var data daemonPID
	b, err := os.ReadFile(daemonPIDFile)
	if err != nil {
		return data, err
	}
	err = json.Unmarshal(b, &data)
	return data, err
}

func removePIDFile() {
	os.Remove(daemonPIDFile)
}

func checkRunningDaemon() (int, bool) {
	data, err := readPIDFile()
	if err != nil {
		return 0, false
	}
	proc, err := os.FindProcess(data.PID)
	if err != nil {
		return 0, false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		removePIDFile()
		return 0, false
	}
	return data.PID, true
}

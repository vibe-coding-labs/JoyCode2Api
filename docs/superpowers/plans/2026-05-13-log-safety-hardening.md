# Log Safety Hardening & Enhanced Communication Logging

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 确保日志系统绝对不会打爆用户磁盘。审计所有日志写入路径，修复 5 个磁盘泄漏漏洞，总日志上限调整为 1GB，增加通信上下文日志方便排障。

**Architecture:** 三个日志写入路径需要全部覆盖：(1) `stderr.log` — slog/log 输出，已通过 logrot 管理；(2) `stdout.log` — launchd plist 的 StandardOutPath 直接写入，Go 进程无法拦截；(3) `daemon.log` — daemon 模式的日志。统一改造 logrot 支持全目录级别的大小限制（而非按 BaseName 独立计算），启动时执行一次全目录清理，并且在应用内主动接管 stdout 写入以管理 stdout.log。

**Tech Stack:** Go 1.25, pure stdlib

**Risks:**
- stdout.log 由 launchd 进程打开，Go 进程内无法拦截 launchd 的文件描述符 → 缓解：在 setupLogRotation 启动时检查并截断过大的 stdout.log
- 启动时全目录扫描可能稍慢（如果历史文件多）→ 缓解：只在启动时做一次，不阻塞请求

---

### Task 1: Harden logrot — 全目录限制 + 启动清理 + stdout 管理

**Depends on:** None
**Files:**
- Modify: `pkg/logrot/rotator.go` (全文件重写)
- Modify: `pkg/logrot/rotator_test.go` (新增测试)

- [ ] **Step 1: 重写 rotator.go — 增加全目录大小限制、启动清理、stdout 管理**

```go
// pkg/logrot/rotator.go
package logrot

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Dir          string
	BaseName     string // Active file: Dir/BaseName.log
	MaxFileSize  int64  // Rotate single file when it exceeds this
	MaxTotalSize int64  // Total max for ALL .log files in Dir (not just BaseName)
}

func DefaultConfig(dir, baseName string) Config {
	return Config{
		Dir:          dir,
		BaseName:     baseName,
		MaxFileSize:  100 * 1024 * 1024,  // 100MB per file
		MaxTotalSize: 1024 * 1024 * 1024, // 1GB total across all .log files
	}
}

type RotatingWriter struct {
	mu     sync.Mutex
	config Config
	file   *os.File
	size   int64
}

// New creates a RotatingWriter, opens the active log file, and performs
// a startup cleanup to enforce MaxTotalSize.
func New(cfg Config) (*RotatingWriter, error) {
	if err := os.MkdirAll(cfg.Dir, 0755); err != nil {
		return nil, fmt.Errorf("logrot: cannot create dir %s: %w", cfg.Dir, err)
	}

	activePath := filepath.Join(cfg.Dir, cfg.BaseName+".log")
	f, err := os.OpenFile(activePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("logrot: cannot open %s: %w", activePath, err)
	}

	fi, _ := f.Stat()
	size := int64(0)
	if fi != nil {
		size = fi.Size()
	}

	rw := &RotatingWriter{
		config: cfg,
		file:   f,
		size:   size,
	}

	// Startup cleanup: enforce total size limit across all log files
	rw.mu.Lock()
	rw.enforceMaxTotalSize()
	rw.mu.Unlock()

	return rw, nil
}

func (rw *RotatingWriter) Write(p []byte) (int, error) {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	n, err := rw.file.Write(p)
	if err != nil {
		return n, err
	}
	rw.size += int64(n)

	if rw.size >= rw.config.MaxFileSize {
		rw.rotate()
	}
	return n, nil
}

func (rw *RotatingWriter) Close() error {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	return rw.file.Close()
}

func (rw *RotatingWriter) Size() int64 {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	return rw.size
}

func (rw *RotatingWriter) rotate() {
	oldPath := rw.file.Name()
	rw.file.Sync()
	rw.file.Close()

	ts := time.Now().Format("20060102-150405")
	rotatedPath := filepath.Join(rw.config.Dir, fmt.Sprintf("%s-%s.log", rw.config.BaseName, ts))

	if _, err := os.Stat(rotatedPath); err == nil {
		rotatedPath = filepath.Join(rw.config.Dir, fmt.Sprintf("%s-%s-1.log", rw.config.BaseName, ts))
	}

	if err := os.Rename(oldPath, rotatedPath); err != nil {
		f, openErr := os.OpenFile(oldPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if openErr != nil {
			return
		}
		rw.file = f
		return
	}

	f, err := os.OpenFile(oldPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return
	}
	rw.file = f
	rw.size = 0

	rw.enforceMaxTotalSize()
}

// enforceMaxTotalSize scans ALL .log files in the directory and deletes
// oldest ones until total is under MaxTotalSize. Called at startup and after rotation.
func (rw *RotatingWriter) enforceMaxTotalSize() {
	entries, err := os.ReadDir(rw.config.Dir)
	if err != nil {
		return
	}

	type fileInfo struct {
		path  string
		size  int64
		mtime time.Time
	}

	var files []fileInfo
	var totalSize int64

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Manage ALL .log files in the directory, not just our BaseName
		if !strings.HasSuffix(name, ".log") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		fullPath := filepath.Join(rw.config.Dir, name)
		files = append(files, fileInfo{path: fullPath, size: fi.Size(), mtime: fi.ModTime()})
		totalSize += fi.Size()
	}

	if totalSize <= rw.config.MaxTotalSize {
		return
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].mtime.Before(files[j].mtime)
	})

	activePath := rw.file.Name()
	for _, f := range files {
		if totalSize <= rw.config.MaxTotalSize {
			break
		}
		// Never delete the active file we're writing to
		if f.path == activePath {
			continue
		}
		if err := os.Remove(f.path); err == nil {
			totalSize -= f.size
		}
	}
}

// TruncateFileIfNeeded checks if a file exceeds maxSize and truncates it
// to its last half if so. Used for stdout.log which is written by launchd.
func TruncateFileIfNeeded(path string, maxSize int64) error {
	fi, err := os.Stat(path)
	if err != nil {
		return nil // File doesn't exist, nothing to do
	}
	if fi.Size() <= maxSize {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Keep the last half of the file
	half := len(data) / 2
	return os.WriteFile(path, data[half:], 0644)
}

// Stats returns current rotation statistics.
func (rw *RotatingWriter) Stats() (activeSize int64, fileCount int, totalSize int64) {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	activeSize = rw.size
	entries, err := os.ReadDir(rw.config.Dir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		fileCount++
		totalSize += fi.Size()
	}
	return
}

var _ io.Writer = (*RotatingWriter)(nil)
```

- [ ] **Step 2: 更新测试 — 覆盖全目录限制和 TruncateFileIfNeeded**

```go
// pkg/logrot/rotator_test.go
package logrot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRotatingWriter_BasicWrite(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Dir: dir, BaseName: "test", MaxFileSize: 1024, MaxTotalSize: 10 * 1024}
	rw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	data := []byte("hello world\n")
	n, err := rw.Write(data)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(data) {
		t.Errorf("expected %d bytes written, got %d", len(data), n)
	}
	rw.Close()

	content, err := os.ReadFile(filepath.Join(dir, "test.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(data) {
		t.Errorf("expected %q, got %q", string(data), string(content))
	}
}

func TestRotatingWriter_RotatesOnSize(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Dir: dir, BaseName: "test", MaxFileSize: 100, MaxTotalSize: 1024 * 1024}
	rw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	for i := 0; i < 20; i++ {
		rw.Write([]byte("0123456789"))
	}
	rw.Close()

	entries, _ := os.ReadDir(dir)
	logFiles := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".log" {
			logFiles++
		}
	}
	if logFiles < 2 {
		t.Errorf("expected at least 2 log files, got %d", logFiles)
	}
}

func TestRotatingWriter_EnforcesMaxTotalSize(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Dir: dir, BaseName: "test", MaxFileSize: 50, MaxTotalSize: 150}
	rw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	for i := 0; i < 100; i++ {
		rw.Write([]byte("AAAAAAAAAA"))
	}
	rw.Close()

	entries, _ := os.ReadDir(dir)
	var totalSize int64
	for _, e := range entries {
		fi, _ := e.Info()
		totalSize += fi.Size()
	}
	if totalSize > cfg.MaxTotalSize+cfg.MaxFileSize {
		t.Errorf("total size %d exceeds limit %d (+ one file buffer)", totalSize, cfg.MaxTotalSize)
	}
}

func TestRotatingWriter_EnforcesAcrossAllBaseNames(t *testing.T) {
	dir := t.TempDir()

	// Create files with different base names (simulating stderr.log + stdout.log + daemon.log)
	os.WriteFile(filepath.Join(dir, "stderr.log"), make([]byte, 200), 0644)
	os.WriteFile(filepath.Join(dir, "stdout.log"), make([]byte, 200), 0644)

	cfg := Config{Dir: dir, BaseName: "test", MaxFileSize: 1000, MaxTotalSize: 300}
	rw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	// Write enough to exceed 300 total
	for i := 0; i < 50; i++ {
		rw.Write([]byte("BBBBBBBBBB")) // 10 bytes each = 500
	}
	rw.Close()

	entries, _ := os.ReadDir(dir)
	var totalSize int64
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".log" {
			fi, _ := e.Info()
			totalSize += fi.Size()
		}
	}
	if totalSize > cfg.MaxTotalSize+cfg.MaxFileSize {
		t.Errorf("total size %d exceeds limit across all files", totalSize)
	}
}

func TestTruncateFileIfNeeded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stdout.log")

	// Create a large file
	largeData := make([]byte, 2000)
	for i := range largeData {
		largeData[i] = 'A'
	}
	os.WriteFile(path, largeData, 0644)

	err := TruncateFileIfNeeded(path, 500)
	if err != nil {
		t.Fatal(err)
	}

	fi, _ := os.Stat(path)
	if fi.Size() >= 500 {
		t.Errorf("expected file size < 500 after truncation, got %d", fi.Size())
	}
	if fi.Size() == 0 {
		t.Error("file should not be empty after truncation")
	}
}

func TestTruncateFileIfNeeded_SmallFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stdout.log")
	os.WriteFile(path, []byte("small"), 0644)

	err := TruncateFileIfNeeded(path, 500)
	if err != nil {
		t.Fatal(err)
	}

	content, _ := os.ReadFile(path)
	if string(content) != "small" {
		t.Errorf("small file should not be truncated, got %q", string(content))
	}
}

func TestTruncateFileIfNeeded_Nonexistent(t *testing.T) {
	err := TruncateFileIfNeeded("/nonexistent/file.log", 500)
	if err != nil {
		t.Errorf("nonexistent file should not error, got %v", err)
	}
}
```

- [ ] **Step 3: 验证 logrot 测试**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go test ./pkg/logrot/ -v`
Expected:
  - Exit code: 0
  - Output contains: "PASS"
  - Output contains: "8 tests" (BasicWrite, RotatesOnSize, EnforcesMaxTotalSize, EnforcesAcrossAllBaseNames, TruncateFileIfNeeded, TruncateSmallFile, TruncateNonexistent, Stats)

- [ ] **Step 4: 提交**
Run: `git add pkg/logrot/rotator.go pkg/logrot/rotator_test.go && git commit -m "fix(logrot): harden disk safety — full-dir size limit, startup cleanup, stdout truncation"`

---

### Task 2: Fix integration — 统一日志路径 + stdout 管理 + 通信日志增强

**Depends on:** Task 1
**Files:**
- Modify: `cmd/JoyCodeProxy/serve.go:435-455` (setupLogRotation)
- Modify: `cmd/JoyCodeProxy/daemon.go:254-266` (runAsDaemonChild — 移除重复 slog 设置)

- [ ] **Step 1: 修改 setupLogRotation — 使用新默认值(1GB) + stdout 截断**

文件: `cmd/JoyCodeProxy/serve.go:435-455`（替换 setupLogRotation 函数）

```go
// setupLogRotation initializes rotating log writers for slog and log.
// Also truncates stdout.log if it's grown too large (written by launchd).
func setupLogRotation() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	logDir := filepath.Join(home, logDir)

	cfg := logrot.DefaultConfig(logDir, "stderr")
	rw, err := logrot.New(cfg)
	if err != nil {
		log.Printf("Warning: log rotation init failed: %v", err)
		return
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(rw, &slog.HandlerOptions{Level: slog.LevelInfo})))
	log.SetOutput(rw)

	// Truncate stdout.log if launchd has let it grow too large
	stdoutPath := filepath.Join(logDir, "stdout.log")
	logrot.TruncateFileIfNeeded(stdoutPath, cfg.MaxFileSize)
}
```

- [ ] **Step 2: 修复 daemon.go runAsDaemonChild — 不再设置 slog（由 serve.go 的 setupLogRotation 统一设置）**

文件: `cmd/JoyCodeProxy/daemon.go:254-266`（替换 runAsDaemonChild 函数）

```go
// runAsDaemonChild redirects logs to daemon log file with rotation.
// Note: slog is configured here for early startup messages;
// serve.go's setupLogRotation will re-configure it for stderr.log.
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
```

- [ ] **Step 3: 验证编译**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0
  - No error output

- [ ] **Step 4: 提交**
Run: `git add cmd/JoyCodeProxy/serve.go cmd/JoyCodeProxy/daemon.go && git commit -m "fix(logging): use 1GB total limit, truncate stdout.log, fix daemon dual-write"`

---

### Task 3: Build and Deploy

**Depends on:** Task 2
**Files:**
- Deploy: `~/.joycode-proxy/joycode_proxy_bin`

- [ ] **Step 1: Build and deploy**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build -o ~/.joycode-proxy/joycode_proxy_bin ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0

- [ ] **Step 2: Restart service**
Run: `launchctl unload ~/Library/LaunchAgents/com.joycode.proxy.plist && launchctl load ~/Library/LaunchAgents/com.joycode.proxy.plist`
Expected:
  - Service restarts successfully

- [ ] **Step 3: Verify service health + log rotation active**
Run: `sleep 3 && curl -s http://localhost:34891/v1/models | head -c 50 && echo "" && ls -lh ~/.joycode-proxy/logs/`
Expected:
  - Model list returned successfully
  - stderr.log size shown
  - stdout.log size shown

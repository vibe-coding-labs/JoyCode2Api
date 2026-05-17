# Log Rotation & Enhanced Logging Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** Add log file auto-rotation with max disk space constraint (default 500MB), auto-delete old files, and more detailed communication logging for JoyCodeProxy.

**Architecture:** Create a `pkg/logrot` package with a `RotatingWriter` (io.Writer) that wraps log file output. On each Write, it checks file size — when exceeding max single-file size (default 50MB), it renames current file with timestamp suffix and creates a new one. After rotation, it scans the log directory and deletes oldest files until total directory size is under the max total size (default 500MB). Integration points: daemon child process (runAsDaemonChild), supervisor (RunSupervisor), and serve.go slog setup. No external dependencies needed.

**Tech Stack:** Go 1.25, pure stdlib (os, io, path/filepath, sort, fmt, time, sync)

**Risks:**
- File rename during active writes could lose data → mitigated by sync.Mutex in RotatingWriter
- Log directory scan on every rotation could be slow with many files → mitiged by only scanning after rotation, not every Write

---

### Task 1: Create Log Rotation Module

**Depends on:** None
**Files:**
- Create: `pkg/logrot/rotator.go`
- Create: `pkg/logrot/rotator_test.go`

- [ ] **Step 1: Create RotatingWriter — core log rotation io.Writer**

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

// DefaultConfig returns sensible defaults.
func DefaultConfig(dir string) Config {
	return Config{
		Dir:         dir,
		BaseName:    "stderr",
		MaxFileSize: 50 * 1024 * 1024, // 50MB per file
		MaxTotalSize: 500 * 1024 * 1024, // 500MB total
		Compress:    false,
	}
}

// Config controls log rotation behavior.
type Config struct {
	Dir          string // Directory for log files
	BaseName     string // Base filename (e.g. "stderr") — active file is Dir/BaseName.log
	MaxFileSize  int64  // Rotate when single file exceeds this (bytes)
	MaxTotalSize int64  // Delete oldest files when total exceeds this (bytes)
	Compress     bool   // Future: gzip rotated files
}

// RotatingWriter is an io.Writer that rotates log files by size
// and enforces a maximum total disk usage.
type RotatingWriter struct {
	mu     sync.Mutex
	config Config
	file   *os.File
	size   int64
}

// New creates a new RotatingWriter and opens the active log file.
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

	return &RotatingWriter{
		config: cfg,
		file:   f,
		size:   size,
	}, nil
}

// Write implements io.Writer. Writes to the active log file,
// rotating if the file exceeds MaxFileSize.
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

// Close closes the active log file.
func (rw *RotatingWriter) Close() error {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	return rw.file.Close()
}

// rotate renames the current file with a timestamp suffix and opens a new one.
func (rw *RotatingWriter) rotate() {
	oldPath := rw.file.Name()
	rw.file.Close()

	ts := time.Now().Format("20060102-150405")
	rotatedPath := filepath.Join(rw.config.Dir, fmt.Sprintf("%s-%s.log", rw.config.BaseName, ts))

	// If the target already exists (unlikely), append a suffix
	if _, err := os.Stat(rotatedPath); err == nil {
		rotatedPath = filepath.Join(rw.config.Dir, fmt.Sprintf("%s-%s-1.log", rw.config.BaseName, ts))
	}

	if err := os.Rename(oldPath, rotatedPath); err != nil {
		// Rename failed — reopen old file and keep appending
		f, openErr := os.OpenFile(oldPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if openErr != nil {
			return
		}
		rw.file = f
		return
	}

	// Open new active file
	f, err := os.OpenFile(oldPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	rw.file = f
	rw.size = 0

	// Enforce total size limit
	rw.enforceMaxTotalSize()
}

// enforceMaxTotalSize deletes oldest rotated files until total directory
// size is under MaxTotalSize.
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
		// Only manage files matching our base pattern
		if !strings.HasPrefix(name, rw.config.BaseName) || !strings.HasSuffix(name, ".log") {
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

	// Sort by modification time — oldest first for deletion
	sort.Slice(files, func(i, j int) bool {
		return files[i].mtime.Before(files[j].mtime)
	})

	for _, f := range files {
		if totalSize <= rw.config.MaxTotalSize {
			break
		}
		// Don't delete the active file
		if f.path == rw.file.Name() {
			continue
		}
		if err := os.Remove(f.path); err == nil {
			totalSize -= f.size
		}
	}
}

// RotateStats returns current rotation statistics.
func (rw *RotatingWriter) RotateStats() (activeSize int64, fileCount int, totalSize int64) {
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
		name := e.Name()
		if !strings.HasPrefix(name, rw.config.BaseName) || !strings.HasSuffix(name, ".log") {
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

// Ensure Writer interface compliance
var _ io.Writer = (*RotatingWriter)(nil)
```

- [ ] **Step 2: Create unit tests for RotatingWriter**

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
	cfg := Config{
		Dir:          dir,
		BaseName:     "test",
		MaxFileSize:  1024, // 1KB for testing
		MaxTotalSize: 10 * 1024,
	}

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
	cfg := Config{
		Dir:          dir,
		BaseName:     "test",
		MaxFileSize:  100, // 100 bytes
		MaxTotalSize: 1024 * 1024,
	}

	rw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	// Write enough data to trigger rotation
	for i := 0; i < 20; i++ {
		rw.Write([]byte("0123456789")) // 10 bytes each
	}

	rw.Close()

	// Should have at least 2 files (rotated + active)
	entries, _ := os.ReadDir(dir)
	logFiles := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".log" {
			logFiles++
		}
	}
	if logFiles < 2 {
		t.Errorf("expected at least 2 log files after rotation, got %d", logFiles)
	}
}

func TestRotatingWriter_EnforcesMaxTotalSize(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Dir:          dir,
		BaseName:     "test",
		MaxFileSize:  50, // 50 bytes per file
		MaxTotalSize: 150, // 150 bytes max total
	}

	rw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	// Write enough to create many files, triggering cleanup
	for i := 0; i < 100; i++ {
		rw.Write([]byte("AAAAAAAAAA")) // 10 bytes each = 1000 bytes total
	}

	rw.Close()

	// Total size should be under max
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

func TestRotatingWriter_Stats(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Dir:          dir,
		BaseName:     "test",
		MaxFileSize:  100,
		MaxTotalSize: 1024 * 1024,
	}

	rw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	rw.Write([]byte("hello"))
	activeSize, fileCount, totalSize := rw.RotateStats()

	if activeSize != 5 {
		t.Errorf("expected activeSize 5, got %d", activeSize)
	}
	if fileCount != 1 {
		t.Errorf("expected fileCount 1, got %d", fileCount)
	}
	if totalSize != 5 {
		t.Errorf("expected totalSize 5, got %d", totalSize)
	}
}
```

- [ ] **Step 3: Verify log rotation module**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go test ./pkg/logrot/ -v`
Expected:
  - Exit code: 0
  - Output contains: "PASS"
  - Output contains: "4 tests"

- [ ] **Step 4: Commit**
Run: `git add pkg/logrot/rotator.go pkg/logrot/rotator_test.go && git commit -m "feat(logrot): add log file rotation with size limits and auto-cleanup"`

---

### Task 2: Integrate Log Rotation into Server

**Depends on:** Task 1
**Files:**
- Modify: `cmd/JoyCodeProxy/serve.go:3-31,56-92,253-275`
- Modify: `cmd/JoyCodeProxy/daemon.go:253-260,262-272`

- [ ] **Step 1: Add logrot import and setupLogger function in serve.go**

Add import for logrot package and create a setupLogger function that initializes slog with a rotating writer. The function should be called early in the serve command's RunE.

- [ ] **Step 2: Integrate into daemon.go runAsDaemonChild and RunSupervisor**

Replace direct os.OpenFile with logrot.New in both runAsDaemonChild and RunSupervisor functions. Set both log and slog output to the rotating writer.

- [ ] **Step 3: Verify compilation**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0
  - No error output

- [ ] **Step 4: Commit**
Run: `git add cmd/JoyCodeProxy/serve.go cmd/JoyCodeProxy/daemon.go && git commit -m "feat(logging): integrate log rotation into server and daemon"`

---

### Task 3: Build and Deploy

**Depends on:** Task 2
**Files:**
- Modify: `~/.joycode-proxy/joycode_proxy_bin` (deployed binary)

- [ ] **Step 1: Build and deploy**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build -o ~/.joycode-proxy/joycode_proxy_bin ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0

- [ ] **Step 2: Restart service**
Run: `launchctl unload ~/Library/LaunchAgents/com.joycode.proxy.plist && launchctl load ~/Library/LaunchAgents/com.joycode.proxy.plist`
Expected:
  - Service restarts successfully

- [ ] **Step 3: Verify log rotation is active**
Run: `sleep 2 && head -5 ~/.joycode-proxy/logs/stderr.log`
Expected:
  - Log file contains startup messages
  - Service is responding on port 34891

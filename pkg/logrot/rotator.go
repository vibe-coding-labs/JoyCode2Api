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
// oldest ones until total is under MaxTotalSize.
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
		return nil
	}
	if fi.Size() <= maxSize {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Keep the last portion that fits under maxSize
	keep := int(maxSize)
	if keep > len(data) {
		keep = len(data)
	}
	return os.WriteFile(path, data[len(data)-keep:], 0644)
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

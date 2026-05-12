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

	// Create pre-existing files with different base names (simulating stderr + stdout + daemon)
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
		rw.Write([]byte("BBBBBBBBBB"))
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
	if fi.Size() > 500 {
		t.Errorf("expected file size <= 500 after truncation, got %d", fi.Size())
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

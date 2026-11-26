package platform

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupLoggingWritesToFile(t *testing.T) {
	restoreLogger(t)
	dir := t.TempDir()

	path, closeLog, err := SetupLogging(dir)
	if err != nil {
		t.Fatalf("SetupLogging: %v", err)
	}
	defer closeLog()

	log.Print("startup failed: test")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "startup failed: test") {
		t.Fatalf("log file does not contain the message: %q", data)
	}
	if filepath.Dir(path) != filepath.Join(dir, "logs") {
		t.Fatalf("log path = %q, want under %q", path, filepath.Join(dir, "logs"))
	}
}

func TestSetupLoggingRotatesFullLog(t *testing.T) {
	restoreLogger(t)
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	path := filepath.Join(logDir, logFileName)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", logMaxBytes)), 0o644); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	_, closeLog, err := SetupLogging(dir)
	if err != nil {
		t.Fatalf("SetupLogging: %v", err)
	}
	defer closeLog()

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("rotated log missing: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat new log: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("new log size = %d, want 0", info.Size())
	}
}

func TestFanoutSurvivesFailingWriter(t *testing.T) {
	var got strings.Builder
	w := fanout{failingWriter{}, &got}
	n, err := w.Write([]byte("kept"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len("kept") {
		t.Fatalf("n = %d, want %d", n, len("kept"))
	}
	if got.String() != "kept" {
		t.Fatalf("second writer got %q", got.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("no stderr") }

func restoreLogger(t *testing.T) {
	t.Helper()
	output, flags := log.Writer(), log.Flags()
	t.Cleanup(func() {
		log.SetOutput(output)
		log.SetFlags(flags)
	})
}

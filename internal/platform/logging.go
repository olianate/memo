package platform

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

const (
	logFileName = "memo.log"
	logMaxBytes = 2 << 20 // 2 MiB per file
	logBackups  = 3
)

// SetupLogging points the standard logger at <dir>/logs/memo.log and returns the
// file path plus a close function. Production builds are linked with
// -H windowsgui, so stderr goes nowhere: without a file, a startup failure is
// invisible and looks like "double-click does nothing". Logging must never stop
// the app, so callers may keep running when this returns an error.
func SetupLogging(dir string) (path string, closeLog func(), err error) {
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", func() {}, err
	}
	path = filepath.Join(logDir, logFileName)
	rotateLog(path)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return "", func() {}, err
	}
	// The file comes first: a GUI build has no valid stderr handle, and
	// io.MultiWriter would stop before the file if stderr failed first.
	log.SetOutput(fanout{file, os.Stderr})
	log.SetFlags(log.LstdFlags)
	return path, func() { _ = file.Close() }, nil
}

// rotateLog renames a full log to memo.log.1 and shifts the older backups down.
// Rotation happens before the file is opened, so a rename is always safe.
func rotateLog(path string) {
	info, err := os.Stat(path)
	if err != nil || info.Size() < logMaxBytes {
		return
	}
	for i := logBackups; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", path, i), fmt.Sprintf("%s.%d", path, i+1))
	}
	_ = os.Rename(path, path+".1")
}

// fanout writes to every destination and ignores individual failures. A closed
// or missing stderr handle must not swallow the copy that goes to the log file.
type fanout []io.Writer

func (f fanout) Write(p []byte) (int, error) {
	for _, dst := range f {
		if dst != nil {
			_, _ = dst.Write(p)
		}
	}
	return len(p), nil
}

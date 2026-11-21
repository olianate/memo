package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// DataDir is the OS application-data directory for memo. MEMO_DATA_DIR
// overrides it for development, seeding and smoke tests.
func DataDir() (string, error) {
	if dir := os.Getenv("MEMO_DATA_DIR"); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "memo"), nil
}

// EnsureDataDir creates the app data directory and its attachments folder.
func EnsureDataDir() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(dir, "attachments"), 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// OpenFile opens a file with the OS default application. Paths never cross the
// webview boundary; this is called from Go after resolving an attachment id.
func OpenFile(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// RevealPath opens the OS file manager at a directory.
func RevealPath(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

//go:build linux

package platform

import (
	"os/exec"
)

// ShowError displays a native error dialog through whichever dialog helper the
// desktop provides. Startup failures happen before Wails can create a window,
// and a headless session simply has nowhere to show it. Best-effort by design.
func ShowError(title, message string) {
	candidates := [][]string{
		{"zenity", "--error", "--no-wrap", "--title", title, "--text", message},
		{"kdialog", "--title", title, "--error", message},
		{"xmessage", "-center", title + "\n\n" + message},
	}
	for _, cmd := range candidates {
		if _, err := exec.LookPath(cmd[0]); err != nil {
			continue
		}
		_ = exec.Command(cmd[0], cmd[1:]...).Run()
		return
	}
}

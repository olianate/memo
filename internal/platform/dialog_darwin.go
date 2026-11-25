//go:build darwin

package platform

import (
	"os/exec"
	"strings"
)

// ShowError displays a native alert. Startup failures happen before Wails can
// create a window, so this shells out to osascript. The call is best-effort.
func ShowError(title, message string) {
	script := "display alert " + appleScriptString(title) +
		" message " + appleScriptString(message) + " as critical"
	_ = exec.Command("osascript", "-e", script).Run()
}

// appleScriptString quotes a value for an AppleScript string literal.
func appleScriptString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\r\n", `\n`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return `"` + s + `"`
}

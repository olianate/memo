//go:build darwin

package services

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// launchInstaller mounts the downloaded DMG after this process exits, copies
// memo.app into place and opens the new bundle. If the copy is not possible
// (for example the app runs translocated), it opens the DMG for a manual drag.
func (s *UpdateService) launchInstaller(installer string) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("无法定位应用包：%w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	appBundle := ""
	if index := strings.LastIndex(executable, ".app/Contents/MacOS/"); index >= 0 {
		appBundle = executable[:index+len(".app")]
	}
	if strings.Contains(appBundle, "/AppTranslocation/") || strings.HasPrefix(appBundle, "/Volumes/") {
		// The running bundle is not writable in place; propose the standard
		// location and fall back to opening the DMG for a manual install.
		appBundle = "/Applications/" + filepath.Base(appBundle)
	}

	script := fmt.Sprintf(`#!/bin/sh
DMG=%s
DEST=%s
PARENT=%d
while kill -0 "$PARENT" 2>/dev/null; do
    sleep 1
done
MOUNT="$(mktemp -d /tmp/memo-update.XXXXXX)"
if ! hdiutil attach "$DMG" -nobrowse -quiet -mountpoint "$MOUNT"; then
    open "$DMG"
    exit 0
fi
SRC=""
for candidate in "$MOUNT"/*.app; do
    if [ -d "$candidate" ]; then
        SRC="$candidate"
        break
    fi
done
INSTALLED=0
if [ -n "$SRC" ] && [ -n "$DEST" ]; then
    STAGE="${DEST}.memo-update"
    BACKUP="${DEST}.memo-backup"
    rm -rf "$STAGE" "$BACKUP"
    if ditto "$SRC" "$STAGE"; then
        if mv "$DEST" "$BACKUP" 2>/dev/null; then
            if mv "$STAGE" "$DEST" 2>/dev/null; then
                rm -rf "$BACKUP"
                INSTALLED=1
            else
                mv "$BACKUP" "$DEST"
            fi
        else
            rm -rf "$DEST"
            if mv "$STAGE" "$DEST" 2>/dev/null; then
                INSTALLED=1
            fi
        fi
    fi
fi
hdiutil detach "$MOUNT" -quiet 2>/dev/null
rmdir "$MOUNT" 2>/dev/null
if [ "$INSTALLED" = "1" ]; then
    open "$DEST"
    rm -rf "$(dirname "$DMG")"
else
    open "$DMG"
fi
rm -f "$0"
`, shellQuote(installer), shellQuote(appBundle), os.Getpid())

	scriptPath := filepath.Join(filepath.Dir(installer), "install-update.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		return fmt.Errorf("无法写入安装脚本：%w", err)
	}
	cmd := exec.Command("/bin/sh", scriptPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("无法启动安装程序：%w", err)
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func windowsInstalledByMSI() bool { return false }

//go:build windows

package services

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
)

const (
	windowsDetachedProcess       = 0x00000008
	windowsCreateNewProcessGroup = 0x00000200
	// windowsInstallDir is the per-user target used by the NSIS release build.
	windowsInstallDir = `Programs\memo\memo.exe`
)

// launchInstaller starts a detached PowerShell helper. The helper waits for
// this process to exit, runs the downloaded installer silently, and relaunches
// memo.
func (s *UpdateService) launchInstaller(installer string) error {
	executable, err := os.Executable()
	if err != nil {
		executable = ""
	} else if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}

	script := fmt.Sprintf(`
$ErrorActionPreference = 'SilentlyContinue'
$parent = %d
$installer = '%s'
$original = '%s'
$deadline = (Get-Date).AddSeconds(120)
while ((Get-Process -Id $parent -ErrorAction SilentlyContinue) -and ((Get-Date) -lt $deadline)) {
    Start-Sleep -Milliseconds 500
}
if ([IO.Path]::GetExtension($installer) -eq '.msi') {
    $msiArgs = '/i "' + $installer + '" /qn /norestart'
    Start-Process -FilePath 'msiexec.exe' -ArgumentList $msiArgs -Wait
} else {
    Start-Process -FilePath $installer -ArgumentList '/S' -Wait
}
$targets = @(
    (Join-Path $env:LOCALAPPDATA '%s'),
    $original,
    (Join-Path $env:ProgramFiles 'olianate\memo\memo.exe')
)
foreach ($target in $targets) {
    if ($target -and (Test-Path -LiteralPath $target)) {
        Start-Process -FilePath $target
        break
    }
}
Remove-Item -LiteralPath (Split-Path -Parent $installer) -Recurse -Force
`, os.Getpid(), powershellQuote(installer), powershellQuote(executable), windowsInstallDir)

	encoded := base64.StdEncoding.EncodeToString(utf16LE(script))
	cmd := exec.Command(
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-WindowStyle", "Hidden",
		"-EncodedCommand", encoded,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windowsDetachedProcess | windowsCreateNewProcessGroup,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("无法启动安装程序：%w", err)
	}
	return nil
}

func powershellQuote(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

// windowsInstalledByMSI reports whether memo was installed from the WiX MSI.
// memo.wxs writes HKCU\Software\memo\installed; the NSIS package does not.
func windowsInstalledByMSI() bool {
	subkey, err := syscall.UTF16PtrFromString(`Software\memo`)
	if err != nil {
		return false
	}
	var key syscall.Handle
	if err := syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, subkey, 0, syscall.KEY_QUERY_VALUE, &key); err != nil {
		return false
	}
	defer syscall.RegCloseKey(key)

	name, err := syscall.UTF16PtrFromString("installed")
	if err != nil {
		return false
	}
	var kind uint32
	var data [4]byte
	size := uint32(len(data))
	if err := syscall.RegQueryValueEx(key, name, nil, &kind, &data[0], &size); err != nil {
		return false
	}
	return kind == syscall.REG_DWORD && size >= 4 && binary.LittleEndian.Uint32(data[:]) == 1
}

func utf16LE(value string) []byte {
	units := utf16.Encode([]rune(value))
	out := make([]byte, 0, len(units)*2)
	for _, unit := range units {
		out = append(out, byte(unit), byte(unit>>8))
	}
	return out
}

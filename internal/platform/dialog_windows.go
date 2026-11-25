//go:build windows

package platform

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var procMessageBoxW = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")

// ShowError displays a native, topmost error box. A packaged build starts
// before Wails has a window, so this talks to the OS directly. The call is
// best-effort: a dialog that cannot be shown must never turn into a panic.
func ShowError(title, message string) {
	caption, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	text, err := windows.UTF16PtrFromString(message)
	if err != nil {
		return
	}
	// MB_ICONERROR | MB_TOPMOST | MB_SETFOREGROUND.
	const flags = 0x00000010 | 0x00040000 | 0x00010000
	_, _, _ = procMessageBoxW.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(caption)), flags)
}

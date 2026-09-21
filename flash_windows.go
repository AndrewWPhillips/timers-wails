//go:build windows

package main

import (
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	// flashwStop etc are for FLASHWINFO.dwFlags, from winuser.h.
	flashwStop  = 0x00000000
	flashwAll   = 0x00000003 // caption + taskbar button
	flashwTimer = 0x00000004 // keep flashing until FLASHW_STOP
)

type flashwinfo struct {
	cbSize    uint32
	hwnd      syscall.Handle
	dwFlags   uint32
	uCount    uint32
	dwTimeout uint32
}

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	procFlashWindowEx = user32.NewProc("FlashWindowEx")
)

// flashTaskbar starts or stops a continuously blinking taskbar button
func flashTaskbar(win *application.WebviewWindow, enabled bool) {
	hwnd := syscall.Handle(uintptr(win.NativeWindow()))
	if hwnd == 0 {
		return
	}

	info := flashwinfo{hwnd: hwnd}
	info.cbSize = uint32(unsafe.Sizeof(info))
	if enabled {
		info.dwFlags = flashwAll | flashwTimer
	} else {
		info.dwFlags = flashwStop
	}

	_, _, _ = procFlashWindowEx.Call(uintptr(unsafe.Pointer(&info)))
}

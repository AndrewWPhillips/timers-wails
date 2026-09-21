//go:build !windows

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// flashTaskbar defers to Wails' own attention request — a bouncing dock icon
// on macOS — on platforms where the continuous-blink taskbar behaviour that
// flash_windows.go implements does not apply.
func flashTaskbar(win *application.WebviewWindow, enabled bool) {
	win.Flash(enabled)
}

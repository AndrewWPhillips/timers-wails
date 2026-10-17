//go:build !server

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// revealInFileManager opens the system's file manager (File Explorer, Finder,
// or the default Linux file manager) at path's folder, with the file itself
// selected where the platform supports that -- Linux file managers generally
// just open the folder.
func revealInFileManager(path string) error {
	app := application.Get()
	if app == nil {
		return nil
	}
	return app.Env.OpenFileManager(path, true)
}

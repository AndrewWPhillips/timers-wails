//go:build server

package main

import "errors"

// revealInFileManager is unavailable in the headless server build: the request
// comes from a browser, possibly on another machine, and opening a file
// manager would launch it on the server instead.
func revealInFileManager(string) error {
	return errors.New("opening the settings folder is not available in this build")
}

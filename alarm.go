package main

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// alarmSoundPath is where the frontend fetches the user's chosen alarm sound.
//
// The webview cannot load an arbitrary file:// path off disk, so the sound is
// served over the app's own asset server instead. Doing it as middleware rather
// than as a bound service keeps ServeHTTP out of the generated bindings.
const alarmSoundPath = "/alarm/current"

// alarmMiddleware serves the configured alarm sound, and passes everything else
// through to the normal asset handler.
func alarmMiddleware(svc *TimerService) application.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != alarmSoundPath {
				next.ServeHTTP(w, r)
				return
			}

			// The frontend passes the exact file it wants (see useAlarm.ts) so
			// that Test can preview a sound the user has picked but not yet
			// saved, without that pick having to be persisted first. Real
			// alarms pass the persisted path here too, so this is the only
			// source of truth the handler needs -- falling back to the saved
			// setting only covers a request that omits it.
			path := r.URL.Query().Get("f")
			if path == "" {
				path = svc.alarmFile()
			}
			if path == "" {
				// No custom sound configured: the frontend falls back to the
				// synthesised beep when this 404s.
				http.NotFound(w, r)
				return
			}

			info, err := os.Stat(path)
			if err != nil || info.IsDir() {
				// The file has been moved or deleted since it was chosen.
				http.NotFound(w, r)
				return
			}

			file, err := os.Open(path)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer file.Close()

			// The path stays the same when the user picks a different sound, so
			// the response must not be cached.
			w.Header().Set("Cache-Control", "no-store")
			// ServeContent picks the content type from the name and handles
			// range requests, which some browsers need for <audio>.
			http.ServeContent(w, r, filepath.Base(path), info.ModTime(), file)
		})
	}
}

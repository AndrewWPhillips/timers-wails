// Command timers is a desktop countdown timer app: any number of timers can run
// at once, each set by hand or from a configurable preset, and each flashes and
// sounds an alarm when it reaches zero.
package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"

	"github.com/andrewwphillips/timers-wails/internal/settings"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	path, err := settings.DefaultPath()
	if err != nil {
		log.Fatalf("timers settings error: %v", err)
	}

	// Keep WebView2's data (cache, cookies...) alongside timers.json rather than
	// in its default %APPDATA%\timers.exe, so the app has a single folder.
	// WebView2 adds its own EBWebView subfolder here. Ignored off Windows.
	dataDir := filepath.Dir(path)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("timers settings error: %v", err)
	}

	store := settings.NewStore(path)
	timers := NewTimerService(store)

	// The window's saved position is needed before the window exists, which is
	// before the service loads everything else at startup. A read error is
	// ignored here; the service reports it when it loads the file again.
	saved, _ := store.Load()

	app := application.New(application.Options{
		Name:        "Timers",
		Description: "Countdown timers",
		Services: []application.Service{
			application.NewService(timers),
		},
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(assets),
			Middleware: alarmMiddleware(timers),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		Windows: application.WindowsOptions{
			WebviewUserDataPath: dataDir,
		},
	})

	opts := application.WebviewWindowOptions{
		Title:            "Timers",
		Width:            500,
		Height:           780,
		MinWidth:         350,
		MaxWidth:         700,
		MinHeight:        520,
		BackgroundColour: application.NewRGB(17, 18, 23),
		URL:              "/",
	}
	applySavedBounds(&opts, saved.Window)

	// Assigned before Run so that the service's tick loop, which starts during
	// Run, never sees a nil window value.
	timers.window = app.Window.NewWithOptions(opts)
	timers.rememberWindowOnClose()

	if err := app.Run(); err != nil {
		log.Fatalf("timers app error: %v", err)
	}
}

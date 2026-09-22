// Command timers is a desktop countdown timer app: any number of timers can run
// at once, each set by hand or from a configurable preset, and each flashes and
// sounds an alarm when it reaches zero.
package main

import (
	"embed"
	"log"

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

	timers := NewTimerService(settings.NewStore(path))

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
	})

	// Assigned before Run so that the service's tick loop, which starts during
	// Run, never sees a nil window value.
	timers.window = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Timers",
		Width:            500,
		Height:           700,
		MinWidth:         350,
		MaxWidth:         700,
		MinHeight:        520,
		BackgroundColour: application.NewRGB(17, 18, 23),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatalf("timers app error: %v", err)
	}
}

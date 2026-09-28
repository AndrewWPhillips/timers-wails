package main

import (
	"github.com/andrewwphillips/timers-wails/internal/settings"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// titleBarHeight is how much of the top of a restored window must land on a
// screen for it to count as reachable -- enough to grab and drag it.
const titleBarHeight = 40

// applySavedBounds opens the window where it was last left. Whether that is
// still on a screen cannot be checked here -- Wails only learns the screens
// inside app.Run -- so keepWindowOnScreen does that later.
func applySavedBounds(opts *application.WebviewWindowOptions, b *settings.WindowBounds) {
	if b == nil {
		return
	}

	opts.InitialPosition = application.WindowXY
	opts.X, opts.Y = b.X, b.Y
	opts.Width = clamp(b.Width, opts.MinWidth, opts.MaxWidth)
	opts.Height = clamp(b.Height, opts.MinHeight, opts.MaxHeight)
	if b.Maximised {
		opts.StartState = application.WindowStateMaximised
	}
}

// keepWindowOnScreen centres the window instead if its saved position is no
// longer on any screen, e.g. it was last on a monitor that has since been
// unplugged, where it would otherwise open invisibly.
//
// It is called from ServiceStartup because that is the earliest point it can
// work: app.Run initialises the platform (which is when Wails learns the
// screens) before starting services, but only creates the window after
// services have started. So the screens are known here, and Center() on a
// window not yet created just changes where it will open (SetPosition, by
// contrast, does nothing before creation).
//
// Only Windows reports its screens this early; Linux and macOS do so later,
// leaving the list empty here, so the saved position is trusted (see
// reachable). That is fine in practice: macOS and most Linux window managers
// keep new windows on a visible screen themselves, and Wayland ignores an
// app's requested position anyway. Windows is the one that doesn't.
func (s *TimerService) keepWindowOnScreen(saved *settings.WindowBounds) {
	app := application.Get()
	if saved == nil || s.window == nil || app == nil {
		return
	}
	if !reachable(app.Screen.GetAll(), saved) {
		s.window.Center()
	}
}

// reachable reports whether the top strip of the window (its title bar) overlaps
// the work area of any screen. With no screen information at all -- some
// platforms only report screens once the app is running -- the saved bounds
// are trusted.
func reachable(screens []*application.Screen, b *settings.WindowBounds) bool {
	if len(screens) == 0 {
		return true
	}
	for _, sc := range screens {
		wa := sc.WorkArea
		overlapX := min(b.X+b.Width, wa.X+wa.Width) - max(b.X, wa.X)
		overlapY := min(b.Y+titleBarHeight, wa.Y+wa.Height) - max(b.Y, wa.Y)
		if overlapX >= titleBarHeight && overlapY > 0 {
			return true
		}
	}
	return false
}

// clamp limits v to lo..hi, where 0 means "no limit" as in the window options.
func clamp(v, lo, hi int) int {
	if lo > 0 && v < lo {
		v = lo
	}
	if hi > 0 && v > hi {
		v = hi
	}
	return v
}

// rememberWindowOnClose records the window's bounds as it closes; the service's
// final save at shutdown then writes them to the settings file.
//
// It has to be a hook on WindowClosing rather than something read at
// shutdown: by the time services shut down the window has already been
// destroyed, so its bounds can no longer be read. Hooks run before Wails'
// own closing listener destroys the window, and on the event goroutine rather
// than the main thread, so reading the bounds here (which hops to the main
// thread) is safe.
func (s *TimerService) rememberWindowOnClose() {
	s.window.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
		s.rememberWindow()
	})
}

func (s *TimerService) rememberWindow() {
	w := s.window
	// A minimised window reports a meaningless off-screen position on Windows
	// (-32000, -32000); keep the previously saved one instead.
	if w == nil || w.IsMinimised() {
		return
	}
	maximised := w.IsMaximised()
	b := w.Bounds()
	if b.Width <= 0 || b.Height <= 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var next settings.WindowBounds
	if s.cfg.Window != nil {
		next = *s.cfg.Window
	}
	next.Maximised = maximised
	// A maximised window's bounds are the whole screen, and its normal size is
	// not available, so keep the previously saved normal bounds for
	// un-maximising after a restart -- unless there are none yet.
	if !maximised || next.Width == 0 {
		next.X, next.Y, next.Width, next.Height = b.X, b.Y, b.Width, b.Height
	}
	s.cfg.Window = &next
}

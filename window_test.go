package main

import (
	"testing"

	"github.com/andrewwphillips/timers-wails/internal/settings"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestReachable(t *testing.T) {
	// A 1920x1080 primary screen with a taskbar, and a second screen to its left.
	screens := []*application.Screen{
		{WorkArea: application.Rect{X: 0, Y: 0, Width: 1920, Height: 1040}},
		{WorkArea: application.Rect{X: -1280, Y: 0, Width: 1280, Height: 984}},
	}

	tests := []struct {
		name string
		b    settings.WindowBounds
		want bool
	}{
		{"fully on primary", settings.WindowBounds{X: 100, Y: 100, Width: 500, Height: 780}, true},
		{"on the left-hand screen", settings.WindowBounds{X: -900, Y: 50, Width: 500, Height: 780}, true},
		{"mostly off the right edge but title bar still grabbable",
			settings.WindowBounds{X: 1850, Y: 100, Width: 500, Height: 780}, true},
		{"only a sliver on screen", settings.WindowBounds{X: 1900, Y: 100, Width: 500, Height: 780}, false},
		{"title bar above the top of every screen",
			settings.WindowBounds{X: 100, Y: -300, Width: 500, Height: 780}, false},
		{"on a monitor that has been unplugged",
			settings.WindowBounds{X: 3000, Y: 100, Width: 500, Height: 780}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := reachable(screens, &tc.b); got != tc.want {
				t.Errorf("reachable(%+v) = %v, want %v", tc.b, got, tc.want)
			}
		})
	}
}

func TestReachableTrustsBoundsWithNoScreenInfo(t *testing.T) {
	b := settings.WindowBounds{X: 5000, Y: 5000, Width: 500, Height: 780}
	if !reachable(nil, &b) {
		t.Error("reachable with no screens = false, want true (nothing to check against)")
	}
}

func TestClamp(t *testing.T) {
	if got := clamp(300, 350, 700); got != 350 {
		t.Errorf("clamp below min = %d, want 350", got)
	}
	if got := clamp(900, 350, 700); got != 700 {
		t.Errorf("clamp above max = %d, want 700", got)
	}
	if got := clamp(2000, 520, 0); got != 2000 {
		t.Errorf("clamp with no max = %d, want 2000", got)
	}
}

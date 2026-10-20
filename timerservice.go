package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/andrewwphillips/timers-wails/internal/settings"
	"github.com/andrewwphillips/timers-wails/internal/timer"
	"github.com/andrewwphillips/timers-wails/internal/timersync"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Events sent to the frontend.
const (
	// EventExpired carries only the timers that expired on this tick, and is
	// what starts the alarm.
	EventExpired = "timers:expired"
	// EventChanged carries the whole list after a change Go made by itself, so
	// the frontend can resynchronise without polling.
	EventChanged = "timers:changed"
)

// tickInterval is how often deadlines are reconciled. 200ms keeps the alarm
// imperceptibly close to zero while costing nothing measurable: the tick is a
// single pass over a small slice.
const tickInterval = 200 * time.Millisecond

// ExpiredEvent sent in EventExpired - more than one timer can expire at the same time (esp. after machine was off/sleeping)
// — so this is always a list.
type ExpiredEvent struct {
	Timers []timer.Timer `json:"timers"`
}

// ChangedEvent sent in EventChanged - the complete list of timers
type ChangedEvent struct {
	Timers []timer.Timer `json:"timers"`
}

// Preferences is the user-editable part of the settings. Other persisted values
// such as the list of timers and saved window position (see Settings struct) are
// persisted too but are not directly editable by the user.
type Preferences struct {
	Presets []settings.Preset `json:"presets"`
	Alarm   settings.Alarm    `json:"alarm"`
	// Autostart is whether the app is registered to launch at login. It is not
	// stored in the settings file: the OS registration is the source of truth.
	Autostart bool `json:"autostart"`
	// Sync is where, and as whom, running timers are shared with other devices.
	// DeviceID is filled in by Go and ignored if the frontend sends one.
	Sync settings.Sync `json:"sync"`
	// AutostartAvailable is false where autostart is unsupported (e.g. the
	// headless server build). Read-only: SavePreferences ignores it.
	AutostartAvailable bool `json:"autostartAvailable"`
}

func init() {
	// Registering the events gives the binding generator what it needs to emit
	// TypeScript types for each payload, so Events.On is checked at compile
	// time in the frontend rather than being an untyped any.
	application.RegisterEvent[ExpiredEvent](EventExpired)
	application.RegisterEvent[ChangedEvent](EventChanged)
}

// TimerService is the Wails-bound adapter over the countdown engine. It is
// deliberately thin: scheduling policy and state transitions live in
// internal/timer, which is testable without a running application.
type TimerService struct {
	manager *timer.Manager
	store   *settings.Store

	mu  sync.Mutex
	cfg settings.Settings

	// window is assigned by main before app.Run, and only read afterwards.
	window *application.WebviewWindow

	stop chan struct{}
	done chan struct{}

	// flashing is touched only by the run goroutine.
	flashing bool

	// Sharing timers through a folder (see syncservice.go). syncMu is taken
	// before mu, and held across a change to the manager and the log entry
	// recording it. syncLog and syncCancel are guarded by it; syncOn mirrors
	// whether syncLog is set, for persist, which must not take syncMu.
	syncMu     sync.Mutex
	syncLog    *timersync.Log
	syncCancel context.CancelFunc
	syncOn     atomic.Bool
}

// NewTimerService returns a service backed by store.
func NewTimerService(store *settings.Store) *TimerService {
	return &TimerService{
		manager: timer.New(),
		store:   store,
		cfg:     settings.Default(),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

// ServiceName is used by Wails for logging.
func (s *TimerService) ServiceName() string { return "TimerService" }

// ServiceStartup loads the saved settings, restores the timers and starts the
// reconciliation loop.
func (s *TimerService) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	cfg, err := s.store.Load()
	if err != nil {
		// A damaged settings file should not stop the app from opening; Load
		// already returned usable defaults.
		slog.Warn("could not read settings, continuing with defaults",
			"path", s.store.Path(), "error", err)
	}

	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()

	// The window has not been created yet, but the screens are now known.
	s.keepWindowOnScreen(cfg.Window)

	// Anything whose deadline passed while the app was closed comes back marked
	// as alarming. No event is emitted for it: the frontend has not subscribed
	// yet at this point, so it picks these up from the initial ListTimers
	// instead. Alarming being part of the timer state, rather than only an
	// event, is what makes that work.
	if fired := s.manager.Restore(cfg.Timers, time.Now()); len(fired) > 0 {
		slog.Info("timers expired while the app was closed", "count", len(fired))
	}

	s.syncMu.Lock()
	if err := s.startSync(); err != nil {
		slog.Warn("timer sharing is off", "folder", cfg.Sync.Folder, "error", err)
	}
	s.syncMu.Unlock()

	go s.run()
	return nil
}

// ServiceShutdown stops the loop and writes a final snapshot so that a timer
// running at exit is still running at the next launch.
func (s *TimerService) ServiceShutdown() error {
	close(s.stop)
	<-s.done
	s.syncMu.Lock()
	s.stopSync()
	s.syncMu.Unlock()
	// This also writes the window bounds recorded as the window closed.
	s.persist()
	return nil
}

// run reconciles deadlines until the service shuts down.
func (s *TimerService) run() {
	defer close(s.done)

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case now := <-ticker.C:
			if fired := s.manager.Tick(now); len(fired) > 0 {
				s.persist()
				s.emit(EventExpired, ExpiredEvent{Timers: fired})
				s.emit(EventChanged, ChangedEvent{Timers: s.manager.List()})
			}
			s.syncFlash()
		}
	}
}

// syncFlash keeps the taskbar button flashing while any timer is alarming.
// Called only from run, so flashing needs no lock. Driving it from the tick
// rather than from each mutation means a dismissal from the frontend is picked
// up without the frontend needing to know about the taskbar at all.
func (s *TimerService) syncFlash() {
	want := s.manager.Alarming()
	if want == s.flashing || s.window == nil {
		return
	}
	flashTaskbar(s.window, want)
	s.flashing = want
}

// emit sends an event to the frontend, if the application is running.
func (s *TimerService) emit(name string, data any) {
	app := application.Get()
	if app == nil {
		return
	}
	app.Event.Emit(name, data)
}

// persist writes the current preferences and timer snapshot.
//
// It is called after each change rather than on a schedule: a running timer's
// stored deadline is immutable, so nothing needs saving between transitions.
func (s *TimerService) persist() {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()

	cfg.Timers = s.timersToSave()
	if err := s.store.Save(cfg); err != nil {
		slog.Error("could not save settings", "path", s.store.Path(), "error", err)
	}
}

// timersToSave is the timers to store in the settings file: none while they are
// shared, since the sync folder then holds them and a stale copy here would
// come back if sharing were later turned on from another folder.
func (s *TimerService) timersToSave() []timer.Timer {
	if s.syncOn.Load() {
		return nil
	}
	return s.manager.Snapshot()
}

// CreateTimer starts a new timer for the given duration. Minutes and seconds
// are not limited to 59, so a preset of 7200 seconds can be passed straight
// through without the caller converting it first. color is the originating
// preset's colour, or "" for a timer started without one.
func (s *TimerService) CreateTimer(hours, minutes, seconds int, label, color string) (timer.Timer, error) {
	d := time.Duration(hours)*time.Hour +
		time.Duration(minutes)*time.Minute +
		time.Duration(seconds)*time.Second

	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	t, err := s.manager.Create(d, label, color, time.Now())
	if err == nil {
		s.persist()
	}
	return s.syncResult(t, err, timersync.Entry{
		Op: timersync.OpNew, Label: t.Label, Color: t.Color, TotalMS: t.TotalMS, EndsAt: t.EndsAtMS,
	})
}

// PauseTimer freezes a running timer.
func (s *TimerService) PauseTimer(id string) (timer.Timer, error) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	t, err := s.manager.Pause(id, time.Now())
	if err == nil {
		s.persist()
	}
	return s.syncResult(t, err, timersync.Entry{Op: timersync.OpPause, RemainingMS: t.RemainingMS})
}

// ResumeTimer restarts a paused timer from where it left off.
func (s *TimerService) ResumeTimer(id string) (timer.Timer, error) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	t, err := s.manager.Resume(id, time.Now())
	if err == nil {
		s.persist()
	}
	return s.syncResult(t, err, timersync.Entry{Op: timersync.OpResume, EndsAt: t.EndsAtMS})
}

// RestartTimer runs a timer again from its full duration.
func (s *TimerService) RestartTimer(id string) (timer.Timer, error) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	t, err := s.manager.Restart(id, time.Now())
	if err == nil {
		s.persist()
	}
	return s.syncResult(t, err, timersync.Entry{Op: timersync.OpRestart, EndsAt: t.EndsAtMS})
}

// DismissTimer silences an expired timer but keeps it in the list.
func (s *TimerService) DismissTimer(id string) (timer.Timer, error) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	t, err := s.manager.Dismiss(id, time.Now())
	if err == nil {
		s.persist()
	}
	return s.syncResult(t, err, timersync.Entry{Op: timersync.OpDismiss, EndsAt: t.EndsAtMS, RemainingMS: t.RemainingMS})
}

// RenameTimer changes a timer's label. Surrounding whitespace is trimmed; an
// empty label is allowed and the card shows it as "Untitled".
func (s *TimerService) RenameTimer(id, label string) (timer.Timer, error) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	t, err := s.manager.Rename(id, strings.TrimSpace(label))
	if err == nil {
		s.persist()
	}
	return s.syncResult(t, err, timersync.Entry{Op: timersync.OpRename, Label: t.Label})
}

// DeleteTimer removes a timer.
func (s *TimerService) DeleteTimer(id string) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	if err := s.manager.Delete(id); err != nil {
		return err
	}
	s.persist()
	s.record(timersync.Entry{Op: timersync.OpDelete, ID: id})
	return nil
}

// ListTimers returns every timer. The frontend calls this once on mount and
// then keeps in step through events.
func (s *TimerService) ListTimers() []timer.Timer {
	return s.manager.List()
}

// GetPreferences returns the presets and alarm settings.
func (s *TimerService) GetPreferences() Preferences {
	s.mu.Lock()
	p := Preferences{Presets: s.cfg.Presets, Alarm: s.cfg.Alarm, Sync: s.cfg.Sync}
	s.mu.Unlock()

	p.Autostart, p.AutostartAvailable = autostartState()
	return p
}

// OpenSettingsFolder opens the folder holding the settings file in the
// system's file manager, with the file selected where the platform supports
// it. Not available in the headless server build (see settingsfolder*.go).
func (s *TimerService) OpenSettingsFolder() error {
	return revealInFileManager(s.store.Path())
}

// GetTimerControls returns the hours, minutes and seconds last set in the main
// window's time controls, so they can be restored when the app opens.
func (s *TimerService) GetTimerControls() settings.TimerControls {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Controls
}

// SaveTimerControls remembers the time controls' current values. The
// frontend calls it (debounced) whenever they change.
func (s *TimerService) SaveTimerControls(c settings.TimerControls) {
	s.mu.Lock()
	s.cfg.Controls = c
	s.mu.Unlock()
	s.persist()
}

// autostartState reports whether the app is registered to launch at login,
// and whether that is supported at all (it is not in the server build).
func autostartState() (enabled, available bool) {
	app := application.Get()
	if app == nil {
		return false, false
	}
	enabled, err := app.Autostart.IsEnabled()
	switch {
	case errors.Is(err, application.ErrAutostartNotSupported):
		return false, false
	case err != nil:
		slog.Warn("could not read run-at-startup state", "error", err)
		return false, true
	}
	return enabled, true
}

// PresetColors returns the fixed palette a new preset's colour is drawn
// from. Go is the only place this list is defined -- the frontend fetches it
// rather than keeping its own hardcoded copy, which had drifted out of sync
// with this one in practice.
func (s *TimerService) PresetColors() []string {
	return settings.PresetColors
}

// SavePreferences stores new presets and alarm settings and returns them as
// stored, which may differ from what was sent: out-of-range values are
// repaired rather than rejected. It also registers or unregisters the app to
// run at login if p.Autostart differs from the current registration.
func (s *TimerService) SavePreferences(p Preferences) (Preferences, error) {
	s.mu.Lock()
	oldSync := s.cfg.Sync
	s.cfg.Presets = p.Presets
	s.cfg.Alarm = p.Alarm
	// The device id is Go's; only the folder and name come from the frontend.
	s.cfg.Sync.Folder = p.Sync.Folder
	s.cfg.Sync.DeviceName = p.Sync.DeviceName
	s.cfg.Normalise()
	cfg := s.cfg
	s.mu.Unlock()

	// Switch sharing on, off or to another folder before saving, so the
	// settings file never keeps timers that have moved into the shared log.
	syncErr := s.applySync(oldSync, cfg.Sync)
	s.mu.Lock()
	if syncErr != nil {
		// Sharing did not start, so leave it off rather than half set up.
		s.cfg.Sync.Folder = ""
	}
	cfg = s.cfg
	saved := Preferences{Presets: cfg.Presets, Alarm: cfg.Alarm, Sync: cfg.Sync}
	s.mu.Unlock()

	cfg.Timers = s.timersToSave()
	if err := s.store.Save(cfg); err != nil {
		saved.Autostart, saved.AutostartAvailable = autostartState()
		return saved, err
	}

	err := applyAutostart(p.Autostart)
	if syncErr != nil {
		err = errors.Join(syncErr, err)
	}
	// Report what the OS actually has now, whether or not the change worked.
	saved.Autostart, saved.AutostartAvailable = autostartState()
	return saved, err
}

// applyAutostart registers or unregisters the app to launch at login, if that
// is supported and differs from the current registration.
func applyAutostart(want bool) error {
	enabled, available := autostartState()
	if !available || want == enabled {
		return nil
	}
	app := application.Get()
	var err error
	if want {
		err = app.Autostart.Enable()
	} else {
		err = app.Autostart.Disable()
	}
	if err != nil {
		return fmt.Errorf("could not change run-at-startup: %w", err)
	}
	return nil
}

// PickAlarmFile opens a native file picker and returns the chosen path, or an
// empty string if the user cancelled.
func (s *TimerService) PickAlarmFile() (string, error) {
	app := application.Get()
	if app == nil {
		return "", nil
	}

	dialog := app.Dialog.OpenFile()
	dialog.SetTitle("Choose an alarm sound")
	dialog.CanChooseFiles(true)
	dialog.CanChooseDirectories(false)
	dialog.AddFilter("Audio files", "*.wav;*.mp3;*.ogg;*.flac;*.m4a;*.aac")

	return dialog.PromptForSingleSelection()
}

// alarmFile returns the configured custom sound path, empty when the built-in
// synthesised beep should be used instead.
func (s *TimerService) alarmFile() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Alarm.SoundFile
}

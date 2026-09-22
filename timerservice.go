package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/andrewwphillips/timers-wails/internal/settings"
	"github.com/andrewwphillips/timers-wails/internal/timer"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Event names emitted to the frontend.
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

// ExpiredEvent is the payload of EventExpired. Several timers can come due in
// the same tick — and a long machine sleep can bring a whole batch due at once
// — so this is always a list.
type ExpiredEvent struct {
	Timers []timer.Timer `json:"timers"`
}

// ChangedEvent is the payload of EventChanged.
type ChangedEvent struct {
	Timers []timer.Timer `json:"timers"`
}

// Preferences is the user-editable part of the settings. Timers are persisted
// too but are not the user's to edit, so they are kept out of this type.
type Preferences struct {
	Presets []settings.Preset `json:"presets"`
	Alarm   settings.Alarm    `json:"alarm"`
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

	// Anything whose deadline passed while the app was closed comes back marked
	// as alarming. No event is emitted for it: the frontend has not subscribed
	// yet at this point, so it picks these up from the initial ListTimers
	// instead. Alarming being part of the timer state, rather than only an
	// event, is what makes that work.
	if fired := s.manager.Restore(cfg.Timers, time.Now()); len(fired) > 0 {
		slog.Info("timers expired while the app was closed", "count", len(fired))
	}

	go s.run()
	return nil
}

// ServiceShutdown stops the loop and writes a final snapshot so that a timer
// running at exit is still running at the next launch.
func (s *TimerService) ServiceShutdown() error {
	close(s.stop)
	<-s.done
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

	cfg.Timers = s.manager.Snapshot()
	if err := s.store.Save(cfg); err != nil {
		slog.Error("could not save settings", "path", s.store.Path(), "error", err)
	}
}

// CreateTimer starts a new timer for the given duration. Minutes and seconds
// are not limited to 59, so a preset of 7200 seconds can be passed straight
// through without the caller converting it first. color is the originating
// preset's colour, or "" for a timer started without one.
func (s *TimerService) CreateTimer(hours, minutes, seconds int, label, color string) (timer.Timer, error) {
	d := time.Duration(hours)*time.Hour +
		time.Duration(minutes)*time.Minute +
		time.Duration(seconds)*time.Second

	t, err := s.manager.Create(d, label, color, time.Now())
	if err != nil {
		return timer.Timer{}, err
	}
	s.persist()
	return t, nil
}

// PauseTimer freezes a running timer.
func (s *TimerService) PauseTimer(id string) (timer.Timer, error) {
	t, err := s.manager.Pause(id, time.Now())
	if err != nil {
		return timer.Timer{}, err
	}
	s.persist()
	return t, nil
}

// ResumeTimer restarts a paused timer from where it left off.
func (s *TimerService) ResumeTimer(id string) (timer.Timer, error) {
	t, err := s.manager.Resume(id, time.Now())
	if err != nil {
		return timer.Timer{}, err
	}
	s.persist()
	return t, nil
}

// RestartTimer runs a timer again from its full duration.
func (s *TimerService) RestartTimer(id string) (timer.Timer, error) {
	t, err := s.manager.Restart(id, time.Now())
	if err != nil {
		return timer.Timer{}, err
	}
	s.persist()
	return t, nil
}

// DismissTimer silences an expired timer but keeps it in the list.
func (s *TimerService) DismissTimer(id string) (timer.Timer, error) {
	t, err := s.manager.Dismiss(id)
	if err != nil {
		return timer.Timer{}, err
	}
	s.persist()
	return t, nil
}

// DeleteTimer removes a timer.
func (s *TimerService) DeleteTimer(id string) error {
	if err := s.manager.Delete(id); err != nil {
		return err
	}
	s.persist()
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
	defer s.mu.Unlock()
	return Preferences{Presets: s.cfg.Presets, Alarm: s.cfg.Alarm}
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
// repaired rather than rejected.
func (s *TimerService) SavePreferences(p Preferences) (Preferences, error) {
	s.mu.Lock()
	s.cfg.Presets = p.Presets
	s.cfg.Alarm = p.Alarm
	s.cfg.Normalise()
	saved := Preferences{Presets: s.cfg.Presets, Alarm: s.cfg.Alarm}
	cfg := s.cfg
	s.mu.Unlock()

	cfg.Timers = s.manager.Snapshot()
	if err := s.store.Save(cfg); err != nil {
		return saved, err
	}
	return saved, nil
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

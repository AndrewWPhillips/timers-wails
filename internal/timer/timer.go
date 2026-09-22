// Package timer implements the countdown engine.
//
// The engine is deliberately free of any Wails or I/O dependency so that it can
// be unit tested directly. Every method that needs the current time takes it as
// a parameter rather than calling time.Now, which makes the state machine fully
// deterministic under test.
//
// Timers are stored as absolute deadlines (EndsAtMS) rather than as counters
// that are decremented on a tick. Remaining time is always derived. That is what
// lets the app survive dropped frames, webview throttling, machine sleep and
// full restarts without any of them needing special handling.
package timer

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// MaxDuration caps a single timer. Long enough for any plausible use, short
// enough that a typo cannot create a timer that outlives the machine.
const MaxDuration = 100*time.Hour - time.Second

// State is the lifecycle state of a timer.
type State string

const (
	// StateRunning means the timer is counting down towards EndsAtMS.
	StateRunning State = "running"
	// StatePaused means the timer holds a fixed RemainingMS and is not counting.
	StatePaused State = "paused"
	// StateExpired means the timer reached zero. It stays in this state, and
	// keeps alarming, until the user dismisses or restarts it.
	StateExpired State = "expired"
)

// Errors returned by Manager. They are wrapped with the timer id where useful.
var (
	ErrNotFound    = errors.New("timer not found")
	ErrBadDuration = errors.New("duration must be greater than zero")
	ErrTooLong     = fmt.Errorf("duration must not exceed %v", MaxDuration)
	ErrNotRunning  = errors.New("timer is not running")
	ErrNotPaused   = errors.New("timer is not paused")
)

// Timer is a single countdown. It is serialised both to the frontend and to the
// settings file, so the JSON tags are part of two contracts.
//
// EndsAtMS is meaningful only while running; RemainingMS is meaningful only
// while paused or expired. Keeping both lets a paused timer be persisted without
// inventing a fake deadline.
type Timer struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	TotalMS     int64  `json:"totalMs"`
	RemainingMS int64  `json:"remainingMs"`
	EndsAtMS    int64  `json:"endsAtMs"`
	State       State  `json:"state"`
	CreatedMS   int64  `json:"createdMs"`
	// Alarming is true from the moment a timer expires until it is dismissed.
	// It is separate from State so that a dismissed timer can stay visible at
	// zero without the alarm restarting whenever the list is re-read.
	Alarming bool `json:"alarming"`
	// Color is copied from the preset that created this timer, at creation
	// time -- it is not looked up live, so editing or deleting the preset
	// later cannot change the colour of a timer already running. Empty for a
	// timer created without a preset (or one created before this field
	// existed); the frontend falls back to its default colour in that case.
	Color string `json:"color"`
}

// Remaining returns the time left at now, never negative.
func (t Timer) Remaining(now time.Time) time.Duration {
	if t.State != StateRunning {
		return time.Duration(t.RemainingMS) * time.Millisecond
	}
	d := time.UnixMilli(t.EndsAtMS).Sub(now)
	if d < 0 {
		return 0
	}
	return d
}

// Manager owns the set of timers. It is safe for concurrent use: the Wails
// service calls it from the frontend's goroutines while the tick loop calls it
// from its own.
type Manager struct {
	mu     sync.Mutex
	timers map[string]*Timer
	order  []string // insertion order, so the UI list is stable
}

// New returns an empty Manager.
func New() *Manager {
	return &Manager{timers: make(map[string]*Timer)}
}

func newID() string {
	var b [8]byte
	// crypto/rand.Read is documented never to fail; it panics on a broken
	// system rather than returning an error.
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Create adds a new timer and starts it running immediately, which is what the
// user means by "set a timer". color is copied from the originating preset,
// if any, and is otherwise the empty string.
func (m *Manager) Create(d time.Duration, label, color string, now time.Time) (Timer, error) {
	if d <= 0 {
		return Timer{}, ErrBadDuration
	}
	if d > MaxDuration {
		return Timer{}, ErrTooLong
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	t := &Timer{
		ID:          newID(),
		Label:       label,
		Color:       color,
		TotalMS:     d.Milliseconds(),
		RemainingMS: d.Milliseconds(),
		EndsAtMS:    now.Add(d).UnixMilli(),
		State:       StateRunning,
		CreatedMS:   now.UnixMilli(),
	}
	m.timers[t.ID] = t
	m.order = append(m.order, t.ID)
	return *t, nil
}

// get returns the timer for id. The caller must hold m.mu.
func (m *Manager) get(id string) (*Timer, error) {
	t, ok := m.timers[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return t, nil
}

// Pause freezes a running timer, converting its deadline back into a remaining
// duration.
func (m *Manager) Pause(id string, now time.Time) (Timer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, err := m.get(id)
	if err != nil {
		return Timer{}, err
	}
	if t.State != StateRunning {
		return Timer{}, fmt.Errorf("%w: %s", ErrNotRunning, id)
	}

	t.RemainingMS = t.Remaining(now).Milliseconds()
	t.EndsAtMS = 0
	t.State = StatePaused
	return *t, nil
}

// Resume converts a paused timer's remaining duration back into a deadline.
func (m *Manager) Resume(id string, now time.Time) (Timer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, err := m.get(id)
	if err != nil {
		return Timer{}, err
	}
	if t.State != StatePaused {
		return Timer{}, fmt.Errorf("%w: %s", ErrNotPaused, id)
	}

	t.EndsAtMS = now.Add(time.Duration(t.RemainingMS) * time.Millisecond).UnixMilli()
	t.State = StateRunning
	return *t, nil
}

// Restart sets a timer back to its full duration and runs it, whatever state it
// was in. This is how an expired timer is reused.
func (m *Manager) Restart(id string, now time.Time) (Timer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, err := m.get(id)
	if err != nil {
		return Timer{}, err
	}

	t.RemainingMS = t.TotalMS
	t.EndsAtMS = now.Add(time.Duration(t.TotalMS) * time.Millisecond).UnixMilli()
	t.State = StateRunning
	t.Alarming = false
	return *t, nil
}

// Dismiss silences an expired timer without removing it, so the user can still
// see which timer finished and restart it if they want.
func (m *Manager) Dismiss(id string) (Timer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, err := m.get(id)
	if err != nil {
		return Timer{}, err
	}
	t.Alarming = false
	return *t, nil
}

// Delete removes a timer.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, err := m.get(id); err != nil {
		return err
	}
	delete(m.timers, id)
	for i, existing := range m.order {
		if existing == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	return nil
}

// List returns every timer in insertion order.
func (m *Manager) List() []Timer {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.list()
}

// list returns every timer in insertion order. The caller must hold m.mu.
func (m *Manager) list() []Timer {
	out := make([]Timer, 0, len(m.order))
	for _, id := range m.order {
		if t, ok := m.timers[id]; ok {
			out = append(out, *t)
		}
	}
	return out
}

// Alarming reports whether any timer is currently sounding. The service uses
// this to decide when to stop flashing the taskbar button.
func (m *Manager) Alarming() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, t := range m.timers {
		if t.Alarming {
			return true
		}
	}
	return false
}

// Tick expires every running timer whose deadline has passed and returns those
// that expired on this call, in insertion order.
//
// Driving expiry from a single reconciliation pass rather than from one
// time.AfterFunc per timer means machine sleep and long stalls need no special
// handling: whenever the tick next runs, everything overdue expires together.
func (m *Manager) Tick(now time.Time) []Timer {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.expireDue(now)
}

// expireDue expires all overdue running timers. The caller must hold m.mu.
func (m *Manager) expireDue(now time.Time) []Timer {
	var fired []Timer
	for _, id := range m.order {
		t, ok := m.timers[id]
		if !ok || t.State != StateRunning {
			continue
		}
		if now.UnixMilli() < t.EndsAtMS {
			continue
		}
		t.State = StateExpired
		t.RemainingMS = 0
		t.Alarming = true
		fired = append(fired, *t)
	}
	return fired
}

// Snapshot returns the timers for persistence.
func (m *Manager) Snapshot() []Timer {
	return m.List()
}

// Restore replaces the contents of the Manager with a persisted snapshot and
// immediately expires anything whose deadline passed while the app was closed.
// It returns the timers that expired during the restore.
func (m *Manager) Restore(saved []Timer, now time.Time) []Timer {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.timers = make(map[string]*Timer, len(saved))
	m.order = m.order[:0]

	// Persisted order should already be stable, but sorting by creation time
	// keeps the list sane if the file has been hand-edited.
	sorted := append([]Timer(nil), saved...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].CreatedMS < sorted[j].CreatedMS
	})

	for i := range sorted {
		t := sorted[i]
		if t.ID == "" {
			t.ID = newID()
		}
		if _, clash := m.timers[t.ID]; clash {
			continue
		}
		m.timers[t.ID] = &t
		m.order = append(m.order, t.ID)
	}

	return m.expireDue(now)
}

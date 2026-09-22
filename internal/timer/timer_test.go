package timer

import (
	"errors"
	"testing"
	"time"
)

// base is a fixed reference time. Using a constant instant rather than
// time.Now keeps every assertion below exact.
var base = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

func TestCreateValidation(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want error
	}{
		{"zero", 0, ErrBadDuration},
		{"negative", -time.Second, ErrBadDuration},
		{"too long", MaxDuration + time.Second, ErrTooLong},
		{"one second", time.Second, nil},
		{"max exactly", MaxDuration, nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := New()
			_, err := m.Create(tc.d, "", "", base)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Create(%v) error = %v, want %v", tc.d, err, tc.want)
			}
		})
	}
}

func TestCreateStartsRunning(t *testing.T) {
	m := New()

	got, err := m.Create(5*time.Minute, "tea", "", base)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got.State != StateRunning {
		t.Errorf("State = %q, want %q", got.State, StateRunning)
	}
	if got.Label != "tea" {
		t.Errorf("Label = %q, want %q", got.Label, "tea")
	}
	if want := base.Add(5 * time.Minute).UnixMilli(); got.EndsAtMS != want {
		t.Errorf("EndsAtMS = %d, want %d", got.EndsAtMS, want)
	}
	if got.TotalMS != (5 * time.Minute).Milliseconds() {
		t.Errorf("TotalMS = %d, want %d", got.TotalMS, (5 * time.Minute).Milliseconds())
	}
	if got.Alarming {
		t.Error("new timer should not be alarming")
	}
}

func TestRemainingIsDerivedWhileRunning(t *testing.T) {
	m := New()
	created, err := m.Create(time.Minute, "", "", base)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	tests := []struct {
		name string
		at   time.Time
		want time.Duration
	}{
		{"at start", base, time.Minute},
		{"part way", base.Add(20 * time.Second), 40 * time.Second},
		{"at zero", base.Add(time.Minute), 0},
		{"past deadline clamps", base.Add(2 * time.Minute), 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := created.Remaining(tc.at); got != tc.want {
				t.Errorf("Remaining(%v) = %v, want %v", tc.at, got, tc.want)
			}
		})
	}
}

func TestPauseAndResumePreserveRemaining(t *testing.T) {
	m := New()
	created, _ := m.Create(time.Minute, "", "", base)

	paused, err := m.Pause(created.ID, base.Add(20*time.Second))
	if err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if paused.State != StatePaused {
		t.Errorf("State = %q, want %q", paused.State, StatePaused)
	}
	if want := (40 * time.Second).Milliseconds(); paused.RemainingMS != want {
		t.Errorf("RemainingMS = %d, want %d", paused.RemainingMS, want)
	}
	if paused.EndsAtMS != 0 {
		t.Errorf("EndsAtMS = %d, want 0 while paused", paused.EndsAtMS)
	}

	// A paused timer must not drift, however long it sits paused.
	if got := paused.Remaining(base.Add(time.Hour)); got != 40*time.Second {
		t.Errorf("Remaining while paused = %v, want %v", got, 40*time.Second)
	}

	resumedAt := base.Add(time.Hour)
	resumed, err := m.Resume(created.ID, resumedAt)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.State != StateRunning {
		t.Errorf("State = %q, want %q", resumed.State, StateRunning)
	}
	if want := resumedAt.Add(40 * time.Second).UnixMilli(); resumed.EndsAtMS != want {
		t.Errorf("EndsAtMS = %d, want %d", resumed.EndsAtMS, want)
	}
}

func TestPauseResumeWrongState(t *testing.T) {
	m := New()
	created, _ := m.Create(time.Minute, "", "", base)

	if _, err := m.Resume(created.ID, base); !errors.Is(err, ErrNotPaused) {
		t.Errorf("Resume on running timer error = %v, want %v", err, ErrNotPaused)
	}

	if _, err := m.Pause(created.ID, base); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if _, err := m.Pause(created.ID, base); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Pause on paused timer error = %v, want %v", err, ErrNotRunning)
	}
}

func TestUnknownIDIsNotFound(t *testing.T) {
	m := New()

	if _, err := m.Pause("nope", base); !errors.Is(err, ErrNotFound) {
		t.Errorf("Pause error = %v, want %v", err, ErrNotFound)
	}
	if _, err := m.Resume("nope", base); !errors.Is(err, ErrNotFound) {
		t.Errorf("Resume error = %v, want %v", err, ErrNotFound)
	}
	if _, err := m.Restart("nope", base); !errors.Is(err, ErrNotFound) {
		t.Errorf("Restart error = %v, want %v", err, ErrNotFound)
	}
	if _, err := m.Dismiss("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Dismiss error = %v, want %v", err, ErrNotFound)
	}
	if err := m.Delete("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete error = %v, want %v", err, ErrNotFound)
	}
}

func TestTickExpiresDueTimersOnce(t *testing.T) {
	m := New()
	short, _ := m.Create(time.Minute, "short", "", base)
	long, _ := m.Create(time.Hour, "long", "", base)

	if fired := m.Tick(base.Add(30 * time.Second)); len(fired) != 0 {
		t.Fatalf("Tick before any deadline fired %d timers, want 0", len(fired))
	}

	fired := m.Tick(base.Add(time.Minute))
	if len(fired) != 1 {
		t.Fatalf("Tick at deadline fired %d timers, want 1", len(fired))
	}
	if fired[0].ID != short.ID {
		t.Errorf("fired ID = %s, want %s", fired[0].ID, short.ID)
	}
	if fired[0].State != StateExpired {
		t.Errorf("State = %q, want %q", fired[0].State, StateExpired)
	}
	if !fired[0].Alarming {
		t.Error("expired timer should be alarming")
	}
	if fired[0].RemainingMS != 0 {
		t.Errorf("RemainingMS = %d, want 0", fired[0].RemainingMS)
	}

	// An already-expired timer must not fire again on the next tick, or the
	// alarm would retrigger several times a second.
	if again := m.Tick(base.Add(2 * time.Minute)); len(again) != 0 {
		t.Errorf("Tick re-fired %d already-expired timers, want 0", len(again))
	}

	// The long timer is untouched.
	if got := m.Tick(base.Add(time.Hour)); len(got) != 1 || got[0].ID != long.ID {
		t.Errorf("Tick at long deadline = %v, want just %s", got, long.ID)
	}
}

// A machine sleeping through several deadlines must expire all of them on the
// next tick, not just one.
func TestTickAfterSleepExpiresEverythingOverdue(t *testing.T) {
	m := New()
	m.Create(time.Minute, "a", "", base)
	m.Create(2*time.Minute, "b", "", base)
	m.Create(3*time.Minute, "c", "", base)
	m.Create(time.Hour, "d", "", base)

	fired := m.Tick(base.Add(10 * time.Minute))
	if len(fired) != 3 {
		t.Fatalf("Tick after sleep fired %d timers, want 3", len(fired))
	}
	for i, label := range []string{"a", "b", "c"} {
		if fired[i].Label != label {
			t.Errorf("fired[%d].Label = %q, want %q", i, fired[i].Label, label)
		}
	}
}

func TestPausedTimerNeverExpires(t *testing.T) {
	m := New()
	created, _ := m.Create(time.Minute, "", "", base)
	if _, err := m.Pause(created.ID, base.Add(10*time.Second)); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	if fired := m.Tick(base.Add(time.Hour)); len(fired) != 0 {
		t.Errorf("Tick expired %d paused timers, want 0", len(fired))
	}
}

func TestRestartFromExpired(t *testing.T) {
	m := New()
	created, _ := m.Create(time.Minute, "", "", base)
	m.Tick(base.Add(time.Minute))

	restartAt := base.Add(2 * time.Hour)
	restarted, err := m.Restart(created.ID, restartAt)
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if restarted.State != StateRunning {
		t.Errorf("State = %q, want %q", restarted.State, StateRunning)
	}
	if restarted.Alarming {
		t.Error("restarted timer should not be alarming")
	}
	if want := restartAt.Add(time.Minute).UnixMilli(); restarted.EndsAtMS != want {
		t.Errorf("EndsAtMS = %d, want %d", restarted.EndsAtMS, want)
	}
	if restarted.RemainingMS != restarted.TotalMS {
		t.Errorf("RemainingMS = %d, want TotalMS %d", restarted.RemainingMS, restarted.TotalMS)
	}
}

func TestDismissSilencesButKeepsTimer(t *testing.T) {
	m := New()
	created, _ := m.Create(time.Minute, "", "", base)
	m.Tick(base.Add(time.Minute))

	if !m.Alarming() {
		t.Fatal("Alarming() = false after expiry, want true")
	}

	dismissed, err := m.Dismiss(created.ID)
	if err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	if dismissed.Alarming {
		t.Error("dismissed timer should not be alarming")
	}
	if dismissed.State != StateExpired {
		t.Errorf("State = %q, want %q — dismissing must not remove the timer", dismissed.State, StateExpired)
	}
	if m.Alarming() {
		t.Error("Alarming() = true after dismissing the only timer, want false")
	}
	if len(m.List()) != 1 {
		t.Errorf("List() length = %d, want 1", len(m.List()))
	}
}

func TestDeleteAndListOrder(t *testing.T) {
	m := New()
	a, _ := m.Create(time.Minute, "a", "", base)
	b, _ := m.Create(time.Minute, "b", "", base)
	c, _ := m.Create(time.Minute, "c", "", base)

	if err := m.Delete(b.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got := m.List()
	if len(got) != 2 {
		t.Fatalf("List() length = %d, want 2", len(got))
	}
	if got[0].ID != a.ID || got[1].ID != c.ID {
		t.Errorf("List() = [%s %s], want [%s %s] — insertion order must survive a delete",
			got[0].Label, got[1].Label, a.Label, c.Label)
	}
}

func TestRestoreExpiresWhatElapsedWhileClosed(t *testing.T) {
	// Snapshot taken at base: one timer due in 1 minute, one due in 1 hour,
	// one paused.
	saved := []Timer{
		{
			ID: "due", Label: "due", TotalMS: 60000, RemainingMS: 60000,
			EndsAtMS: base.Add(time.Minute).UnixMilli(),
			State:    StateRunning, CreatedMS: base.UnixMilli(),
		},
		{
			ID: "later", Label: "later", TotalMS: 3600000, RemainingMS: 3600000,
			EndsAtMS: base.Add(time.Hour).UnixMilli(),
			State:    StateRunning, CreatedMS: base.Add(time.Millisecond).UnixMilli(),
		},
		{
			ID: "held", Label: "held", TotalMS: 60000, RemainingMS: 45000,
			EndsAtMS: 0,
			State:    StatePaused, CreatedMS: base.Add(2 * time.Millisecond).UnixMilli(),
		},
	}

	m := New()
	// Reopened 10 minutes later: "due" should have gone off while closed.
	fired := m.Restore(saved, base.Add(10*time.Minute))

	if len(fired) != 1 {
		t.Fatalf("Restore fired %d timers, want 1", len(fired))
	}
	if fired[0].ID != "due" {
		t.Errorf("fired ID = %s, want due", fired[0].ID)
	}
	if !fired[0].Alarming {
		t.Error("a timer that expired while closed should be alarming on restore")
	}

	got := m.List()
	if len(got) != 3 {
		t.Fatalf("List() length = %d, want 3", len(got))
	}

	byID := map[string]Timer{}
	for _, tm := range got {
		byID[tm.ID] = tm
	}
	if byID["later"].State != StateRunning {
		t.Errorf("later.State = %q, want %q", byID["later"].State, StateRunning)
	}
	// The restored deadline is absolute, so the remaining time reflects the
	// wall-clock gap rather than restarting from full.
	if want := 50 * time.Minute; byID["later"].Remaining(base.Add(10*time.Minute)) != want {
		t.Errorf("later remaining = %v, want %v",
			byID["later"].Remaining(base.Add(10*time.Minute)), want)
	}
	if byID["held"].State != StatePaused {
		t.Errorf("held.State = %q, want %q", byID["held"].State, StatePaused)
	}
	if byID["held"].RemainingMS != 45000 {
		t.Errorf("held.RemainingMS = %d, want 45000", byID["held"].RemainingMS)
	}
}

func TestRestoreReplacesPreviousContents(t *testing.T) {
	m := New()
	m.Create(time.Minute, "stale", "", base)

	m.Restore([]Timer{{
		ID: "fresh", TotalMS: 1000, RemainingMS: 1000,
		EndsAtMS: base.Add(time.Hour).UnixMilli(),
		State:    StateRunning, CreatedMS: base.UnixMilli(),
	}}, base)

	got := m.List()
	if len(got) != 1 || got[0].ID != "fresh" {
		t.Errorf("List() after Restore = %v, want just the restored timer", got)
	}
}

func TestSnapshotRoundTripsThroughRestore(t *testing.T) {
	m := New()
	m.Create(time.Hour, "a", "", base)
	paused, _ := m.Create(time.Hour, "b", "", base)
	m.Pause(paused.ID, base.Add(time.Minute))

	snap := m.Snapshot()

	restored := New()
	restored.Restore(snap, base.Add(time.Minute))

	got := restored.List()
	if len(got) != len(snap) {
		t.Fatalf("restored %d timers, want %d", len(got), len(snap))
	}
	for i := range snap {
		if got[i] != snap[i] {
			t.Errorf("timer %d = %+v, want %+v", i, got[i], snap[i])
		}
	}
}

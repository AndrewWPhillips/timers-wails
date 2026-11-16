package timer

import (
	"testing"
	"time"
)

func TestRepeatStartsAgainAndAlarms(t *testing.T) {
	m := New()
	tm, _ := m.Create(time.Minute, "", "", base)
	if _, err := m.SetRepeat(tm.ID, true, base); err != nil {
		t.Fatalf("SetRepeat: %v", err)
	}

	fired := m.Tick(base.Add(time.Minute))
	if len(fired) != 1 {
		t.Fatalf("Tick fired %d timers, want 1", len(fired))
	}
	got := m.List()[0]
	if got.State != StateRunning || !got.Alarming {
		t.Fatalf("after first period: %+v", got)
	}
	if want := base.Add(2 * time.Minute).UnixMilli(); got.EndsAtMS != want {
		t.Fatalf("EndsAtMS = %d, want %d", got.EndsAtMS, want)
	}

	// Ack: stays running, silent.
	got, _ = m.Dismiss(tm.ID, base.Add(90*time.Second))
	if got.State != StateRunning || got.Alarming {
		t.Fatalf("after dismiss: %+v", got)
	}

	// A long stall skips whole periods and stays in phase.
	m.Tick(base.Add(5*time.Minute + 10*time.Second))
	got = m.List()[0]
	if want := base.Add(6 * time.Minute).UnixMilli(); got.EndsAtMS != want || !got.Alarming {
		t.Fatalf("after stall: %+v, want EndsAtMS %d alarming", got, want)
	}
}

func TestRepeatOffWhileAlarmingExpires(t *testing.T) {
	m := New()
	tm, _ := m.Create(time.Minute, "", "", base)
	m.SetRepeat(tm.ID, true, base)
	m.Tick(base.Add(time.Minute))

	got, _ := m.SetRepeat(tm.ID, false, base.Add(80*time.Second))
	if got.State != StateExpired || !got.Alarming {
		t.Fatalf("after repeat off: %+v", got)
	}
	if want := base.Add(time.Minute).UnixMilli(); got.EndsAtMS != want {
		t.Fatalf("EndsAtMS = %d, want last deadline %d", got.EndsAtMS, want)
	}
}

func TestRepeatOnWhileAlarmingResumesInPhase(t *testing.T) {
	m := New()
	tm, _ := m.Create(time.Minute, "", "", base)
	m.Tick(base.Add(time.Minute))

	got, _ := m.SetRepeat(tm.ID, true, base.Add(150*time.Second))
	if got.State != StateRunning || !got.Alarming {
		t.Fatalf("after repeat on: %+v", got)
	}
	if want := base.Add(3 * time.Minute).UnixMilli(); got.EndsAtMS != want {
		t.Fatalf("EndsAtMS = %d, want %d", got.EndsAtMS, want)
	}
}

func TestRepeatRestoreAfterSleep(t *testing.T) {
	m := New()
	tm, _ := m.Create(time.Minute, "", "", base)
	m.SetRepeat(tm.ID, true, base)
	saved := m.Snapshot()

	m2 := New()
	fired := m2.Restore(saved, base.Add(10*time.Minute+time.Second))
	if len(fired) != 1 || fired[0].ID != tm.ID {
		t.Fatalf("Restore fired %+v", fired)
	}
	got := m2.List()[0]
	if got.State != StateRunning || !got.Alarming || !got.Repeat {
		t.Fatalf("restored: %+v", got)
	}
}

package timersync

import (
	"testing"
	"time"

	"github.com/andrewwphillips/timers-wails/internal/timer"
)

// TestFoldRepeatMatchesManager drives a Manager through a repeating timer's
// life, writing the entries the service would, and checks the fold of those
// entries agrees with the Manager at every step.
func TestFoldRepeatMatchesManager(t *testing.T) {
	m := timer.New()
	var es []Entry
	seq := int64(0)
	add := func(e Entry, ts time.Time) {
		seq++
		e.V, e.TS, e.Dev, e.Seq = Version, ts.UnixMilli(), "a", seq
		es = append(es, e)
	}
	check := func(step string, now time.Time) {
		t.Helper()
		m.Tick(now)
		want := m.List()[0]
		got := only(t, Fold(es, now))
		if got.State != want.State || got.Alarming != want.Alarming || got.EndsAtMS != want.EndsAtMS ||
			got.Repeat != want.Repeat || (got.State != timer.StateRunning && got.RemainingMS != want.RemainingMS) {
			t.Fatalf("%s:\nfold    %+v\nmanager %+v", step, got, want)
		}
	}
	dismiss := func(now time.Time) {
		tm, _ := m.Dismiss(m.List()[0].ID, now)
		endsAt := tm.EndsAtMS
		if tm.State == timer.StateRunning && tm.Repeat {
			endsAt = tm.LastEndMS()
		}
		add(Entry{Op: OpDismiss, ID: tm.ID, EndsAt: endsAt, RemainingMS: tm.RemainingMS}, now)
	}
	setRepeat := func(on bool, now time.Time) {
		m.Tick(now)
		tm, _ := m.SetRepeat(m.List()[0].ID, on, now)
		add(Entry{Op: OpRepeat, ID: tm.ID, Repeat: on}, now)
	}
	after := func(d time.Duration) time.Time { return t0.Add(d) }

	tm, _ := m.Create(time.Minute, "tea", "", t0)
	add(Entry{Op: OpNew, ID: tm.ID, Label: tm.Label, TotalMS: tm.TotalMS, EndsAt: tm.EndsAtMS}, t0)
	setRepeat(true, after(time.Second))
	check("running", after(30*time.Second))
	check("first repeat", after(70*time.Second))
	dismiss(after(80 * time.Second))
	check("acked", after(90*time.Second))
	check("second repeat", after(130*time.Second))
	check("third repeat, unacked", after(190*time.Second))
	setRepeat(false, after(200*time.Second))
	check("repeat off while alarming", after(210*time.Second))
	setRepeat(true, after(220*time.Second))
	check("repeat back on", after(230*time.Second))
	dismiss(after(240 * time.Second))
	setRepeat(false, after(250*time.Second))
	check("repeat off after ack", after(260*time.Second))
	check("expires normally", after(310*time.Second))
	dismiss(after(320 * time.Second))
	check("dismissed", after(330*time.Second))
	setRepeat(true, after(340*time.Second))
	check("repeat on while dismissed stays expired", after(400*time.Second))
}

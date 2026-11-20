package timersync

import (
	"sort"
	"time"

	"github.com/andrewwphillips/timers-wails/internal/timer"
)

// foldState is a timer being rebuilt, plus what the final Timer has no room for.
type foldState struct {
	t timer.Timer
	// dismissedEnds is the EndsAt of the run most recently dismissed.
	dismissedEnds int64
	// dismissedOverrun is that run's RemainingMS (zero or negative) at dismissal.
	dismissedOverrun int64
	// lastPassed is the deadline a repeating timer most recently went past and
	// started again from, or zero if it has not repeated since it was (re)started.
	lastPassed int64
}

// advance starts a repeating timer again for as many periods as have passed by
// ts. It follows timer.Manager's rule exactly, so both agree on the deadline.
func (st *foldState) advance(ts int64) {
	t := &st.t
	if t.State != timer.StateRunning || !t.Repeat || t.TotalMS <= 0 || ts < t.EndsAtMS {
		return
	}
	k := (ts-t.EndsAtMS)/t.TotalMS + 1
	t.EndsAtMS += k * t.TotalMS
	t.RemainingMS = t.TotalMS
	st.lastPassed = t.EndsAtMS - t.TotalMS
}

// repeatAlarming reports whether a repeating timer is still alarming for the
// deadline it last went past.
func (st *foldState) repeatAlarming() bool {
	return st.lastPassed != 0 && st.dismissedEnds != st.lastPassed
}

// Fold rebuilds the timers from every device's entries. It sorts a copy of
// entries, so the caller's order (and so the order the files were read in) does
// not matter.
//
// A timer whose end has passed comes out expired and alarming unless a dismiss
// names that run -- this is also how a device that was off rings when it next
// starts. Entries for a timer with no `new` entry are ignored (it was pruned, or
// has not synced yet), and nothing after a `delete` counts.
func Fold(entries []Entry, now time.Time) []timer.Timer {
	es := append([]Entry(nil), entries...)
	sortEntries(es)

	states := make(map[string]*foldState)
	deleted := make(map[string]bool)

	for _, e := range es {
		if e.ID == "" || deleted[e.ID] {
			continue
		}
		st := states[e.ID]
		if e.Op == OpNew {
			if st == nil {
				states[e.ID] = &foldState{t: timer.Timer{
					ID:          e.ID,
					Label:       e.Label,
					Color:       e.Color,
					TotalMS:     e.TotalMS,
					RemainingMS: e.TotalMS,
					EndsAtMS:    e.EndsAt,
					State:       timer.StateRunning,
					CreatedMS:   e.TS,
				}}
			}
			continue
		}
		if st == nil {
			continue
		}
		t := &st.t
		// Bring a repeating timer up to the moment of this entry first, so the
		// entry sees the period it was actually made in.
		st.advance(e.TS)
		switch e.Op {
		case OpPause:
			// A pause made at or after the end came too late: the timer had
			// already expired (clocks can differ a little between devices).
			if t.State == timer.StateRunning && e.TS < t.EndsAtMS {
				t.State = timer.StatePaused
				t.RemainingMS = e.RemainingMS
				t.EndsAtMS = 0
			}
		case OpResume:
			if t.State == timer.StatePaused {
				t.State = timer.StateRunning
				t.EndsAtMS = e.EndsAt
			}
		case OpRestart:
			t.State = timer.StateRunning
			t.RemainingMS = t.TotalMS
			t.EndsAtMS = e.EndsAt
			st.lastPassed = 0
		case OpRepeat:
			// Mirrors timer.Manager.SetRepeat.
			if t.Repeat == e.Repeat {
				break
			}
			expiredAlarming := t.State == timer.StateRunning && e.TS >= t.EndsAtMS &&
				st.dismissedEnds != t.EndsAtMS
			switch {
			case e.Repeat && expiredAlarming:
				t.Repeat = true
				st.advance(e.TS)
			case !e.Repeat && t.State == timer.StateRunning && st.repeatAlarming():
				// Back to an ordinary timer, expired at the deadline it last
				// passed; the final pass below marks it expired.
				t.Repeat = false
				t.EndsAtMS = st.lastPassed
				st.lastPassed = 0
			case e.Repeat && t.State == timer.StateRunning && e.TS >= t.EndsAtMS:
				// Already expired and dismissed: it stays expired (until
				// restarted) rather than picking up repeating from here.
				t.Repeat = true
				t.State = timer.StateExpired
				t.RemainingMS = 0
			default:
				t.Repeat = e.Repeat
				st.lastPassed = 0
			}
		case OpRename:
			t.Label = e.Label
		case OpDismiss:
			st.dismissedEnds = e.EndsAt
			st.dismissedOverrun = e.RemainingMS
		case OpDelete:
			deleted[e.ID] = true
			delete(states, e.ID)
		}
	}

	out := make([]timer.Timer, 0, len(states))
	for _, st := range states {
		st.advance(now.UnixMilli())
		t := st.t
		if t.State == timer.StateRunning && now.UnixMilli() >= t.EndsAtMS {
			t.State = timer.StateExpired
			t.RemainingMS = 0
		}
		if t.State == timer.StateExpired {
			t.Alarming = st.dismissedEnds != t.EndsAtMS
		} else {
			t.Alarming = st.repeatAlarming()
		}
		if t.State == timer.StateExpired && !t.Alarming {
			t.RemainingMS = st.dismissedOverrun
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedMS != out[j].CreatedMS {
			return out[i].CreatedMS < out[j].CreatedMS
		}
		return out[i].ID < out[j].ID
	})
	return out
}

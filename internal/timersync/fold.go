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
		t := st.t
		if t.State == timer.StateRunning && now.UnixMilli() >= t.EndsAtMS {
			t.State = timer.StateExpired
			t.RemainingMS = 0
		}
		t.Alarming = t.State == timer.StateExpired && st.dismissedEnds != t.EndsAtMS
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

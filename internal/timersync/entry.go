// Package timersync shares running timers between devices through a folder
// that another tool (Syncthing, Dropbox, ...) keeps in step.
//
// Each device appends to a file of its own, so no file ever has two writers and
// the sync tool never has to resolve a conflict. A device builds its timers by
// folding every file's entries together (see Fold); nothing is applied
// incrementally, so folding the same entries always gives the same timers.
package timersync

import "sort"

// Version is the entry format version written to every line.
const Version = 1

// Op is the kind of change an Entry records.
type Op string

const (
	// OpHello records the device's display name. It has no timer id.
	OpHello   Op = "hello"
	OpNew     Op = "new"
	OpPause   Op = "pause"
	OpResume  Op = "resume"
	OpRestart Op = "restart"
	OpRename  Op = "rename"
	// OpDismiss silences the run of a timer that ends at EndsAt. Naming the run
	// stops a late dismissal from silencing a timer restarted since. RemainingMS
	// carries the (negative) overrun at the moment of dismissal, so every device
	// shows how late it was dismissed.
	OpDismiss Op = "dismiss"
	OpDelete  Op = "delete"
)

// Entry is one line of a device's log. Entries carry absolute values (EndsAt,
// RemainingMS) rather than "now", so every device computes the same timer.
type Entry struct {
	V   int    `json:"v"`
	TS  int64  `json:"ts"`  // unix ms when the change was made
	Dev string `json:"dev"` // device id
	Seq int64  `json:"seq"` // per-device counter, breaks timestamp ties
	Op  Op     `json:"op"`
	ID  string `json:"id,omitempty"` // timer id

	Name        string `json:"name,omitempty"` // OpHello
	Label       string `json:"label,omitempty"`
	Color       string `json:"color,omitempty"`
	TotalMS     int64  `json:"totalMs,omitempty"`
	RemainingMS int64  `json:"remainingMs,omitempty"`
	EndsAt      int64  `json:"endsAt,omitempty"` // unix ms
}

// sortEntries puts entries in the one order every device replays them in.
func sortEntries(es []Entry) {
	sort.SliceStable(es, func(i, j int) bool {
		a, b := es[i], es[j]
		if a.TS != b.TS {
			return a.TS < b.TS
		}
		if a.Dev != b.Dev {
			return a.Dev < b.Dev
		}
		return a.Seq < b.Seq
	})
}

package timersync

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andrewwphillips/timers-wails/internal/timer"
)

var t0 = time.UnixMilli(1_000_000)

func ms(d time.Duration) int64 { return d.Milliseconds() }

func at(offset time.Duration) int64 { return t0.Add(offset).UnixMilli() }

func newEntry(id string, ts int64, total time.Duration) Entry {
	return Entry{V: Version, TS: ts, Dev: "a", Seq: 1, Op: OpNew, ID: id, Label: "tea", TotalMS: ms(total), EndsAt: ts + ms(total)}
}

func only(t *testing.T, ts []timer.Timer) timer.Timer {
	t.Helper()
	if len(ts) != 1 {
		t.Fatalf("want 1 timer, got %d: %+v", len(ts), ts)
	}
	return ts[0]
}

func TestFoldRunningAndExpired(t *testing.T) {
	es := []Entry{newEntry("x", at(0), 5*time.Minute)}

	got := only(t, Fold(es, t0.Add(time.Minute)))
	if got.State != timer.StateRunning || got.Alarming {
		t.Fatalf("running timer: %+v", got)
	}

	// Expired while this device was off: rings.
	got = only(t, Fold(es, t0.Add(time.Hour)))
	if got.State != timer.StateExpired || !got.Alarming || got.RemainingMS != 0 {
		t.Fatalf("expired timer: %+v", got)
	}
}

func TestFoldDismissSilencesOnlyThatRun(t *testing.T) {
	end := at(5 * time.Minute)
	es := []Entry{
		newEntry("x", at(0), 5*time.Minute),
		{TS: at(6 * time.Minute), Dev: "b", Seq: 1, Op: OpDismiss, ID: "x", EndsAt: end},
	}
	if got := only(t, Fold(es, t0.Add(time.Hour))); got.Alarming {
		t.Fatalf("dismissed timer still alarming: %+v", got)
	}

	// A restart makes a new run; the old dismissal must not silence it.
	es = append(es, Entry{TS: at(10 * time.Minute), Dev: "a", Seq: 2, Op: OpRestart, ID: "x", EndsAt: at(15 * time.Minute)})
	got := only(t, Fold(es, t0.Add(time.Hour)))
	if !got.Alarming {
		t.Fatalf("restarted run should alarm: %+v", got)
	}
}

func TestFoldDismissKeepsOverrun(t *testing.T) {
	end := at(5 * time.Minute)
	es := []Entry{
		newEntry("x", at(0), 5*time.Minute),
		{TS: at(5*time.Minute + 30*time.Second), Dev: "b", Seq: 1, Op: OpDismiss, ID: "x", EndsAt: end, RemainingMS: -30000},
	}
	got := only(t, Fold(es, t0.Add(time.Hour)))
	if got.Alarming || got.RemainingMS != -30000 {
		t.Fatalf("dismissed timer should keep its overrun: %+v", got)
	}
}

func TestFoldDismissOfUnexpiredRunDoesNotPreSilence(t *testing.T) {
	// A dismiss naming a different EndsAt (e.g. from a skewed clock) is inert.
	es := []Entry{
		newEntry("x", at(0), 5*time.Minute),
		{TS: at(time.Minute), Dev: "b", Seq: 1, Op: OpDismiss, ID: "x", EndsAt: at(4 * time.Minute)},
	}
	if got := only(t, Fold(es, t0.Add(time.Hour))); !got.Alarming {
		t.Fatalf("mismatched dismiss silenced the alarm: %+v", got)
	}
}

func TestFoldPauseResume(t *testing.T) {
	es := []Entry{
		newEntry("x", at(0), 10*time.Minute),
		{TS: at(2 * time.Minute), Dev: "b", Seq: 1, Op: OpPause, ID: "x", RemainingMS: ms(8 * time.Minute)},
	}
	got := only(t, Fold(es, t0.Add(time.Hour)))
	if got.State != timer.StatePaused || got.RemainingMS != ms(8*time.Minute) || got.EndsAtMS != 0 || got.Alarming {
		t.Fatalf("paused timer: %+v", got)
	}

	es = append(es, Entry{TS: at(30 * time.Minute), Dev: "a", Seq: 2, Op: OpResume, ID: "x", EndsAt: at(38 * time.Minute)})
	got = only(t, Fold(es, t0.Add(31*time.Minute)))
	if got.State != timer.StateRunning || got.EndsAtMS != at(38*time.Minute) {
		t.Fatalf("resumed timer: %+v", got)
	}
}

func TestFoldPauseAfterEndIsIgnored(t *testing.T) {
	es := []Entry{
		newEntry("x", at(0), time.Minute),
		{TS: at(time.Minute), Dev: "b", Seq: 1, Op: OpPause, ID: "x"},
	}
	got := only(t, Fold(es, t0.Add(time.Hour)))
	if got.State != timer.StateExpired || !got.Alarming {
		t.Fatalf("late pause should not stop the alarm: %+v", got)
	}
}

func TestFoldDeleteIsTerminal(t *testing.T) {
	es := []Entry{
		newEntry("x", at(0), time.Minute),
		{TS: at(time.Second), Dev: "b", Seq: 1, Op: OpDelete, ID: "x"},
		{TS: at(2 * time.Second), Dev: "a", Seq: 2, Op: OpRestart, ID: "x", EndsAt: at(time.Hour)},
		newEntry("x", at(3*time.Second), time.Minute),
	}
	if got := Fold(es, t0); len(got) != 0 {
		t.Fatalf("deleted timer came back: %+v", got)
	}
}

func TestFoldIgnoresOrphansAndRenames(t *testing.T) {
	es := []Entry{
		{TS: at(0), Dev: "b", Seq: 1, Op: OpPause, ID: "ghost"},
		newEntry("x", at(time.Second), time.Hour),
		{TS: at(2 * time.Second), Dev: "b", Seq: 2, Op: OpRename, ID: "x", Label: "coffee"},
		{TS: at(3 * time.Second), Dev: "a", Seq: 9, Op: OpHello, Name: "Home"},
	}
	got := only(t, Fold(es, t0))
	if got.Label != "coffee" {
		t.Fatalf("label = %q", got.Label)
	}
}

func TestFoldIsIndependentOfInputOrder(t *testing.T) {
	es := []Entry{
		newEntry("x", at(0), time.Hour),
		{TS: at(time.Minute), Dev: "b", Seq: 1, Op: OpPause, ID: "x", RemainingMS: ms(59 * time.Minute)},
		{TS: at(2 * time.Minute), Dev: "a", Seq: 2, Op: OpResume, ID: "x", EndsAt: at(61 * time.Minute)},
		{TS: at(3 * time.Minute), Dev: "b", Seq: 2, Op: OpRename, ID: "x", Label: "z"},
	}
	want := Fold(es, t0.Add(10*time.Minute))
	rev := make([]Entry, len(es))
	for i, e := range es {
		rev[len(es)-1-i] = e
	}
	got := Fold(rev, t0.Add(10*time.Minute))
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("order changed the result:\n%+v\n%+v", got, want)
	}
}

func TestFoldTiesBreakByDeviceThenSeq(t *testing.T) {
	// Pause and rename at the same millisecond from different devices: the
	// outcome must not depend on which file was read first.
	base := newEntry("x", at(0), time.Hour)
	p := Entry{TS: at(time.Minute), Dev: "b", Seq: 1, Op: OpPause, ID: "x", RemainingMS: 1}
	r := Entry{TS: at(time.Minute), Dev: "a", Seq: 5, Op: OpResume, ID: "x", EndsAt: at(time.Hour)}
	one := Fold([]Entry{base, p, r}, t0)
	two := Fold([]Entry{r, p, base}, t0)
	if one[0] != two[0] {
		t.Fatalf("tie broken differently: %+v vs %+v", one[0], two[0])
	}
}

func TestLogAppendReadAndHealTornLine(t *testing.T) {
	dir := t.TempDir()
	l, err := OpenLog(dir, "1234567890abcdef", "Home PC!")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(l.Path()) != "Home-PC-12345678"+Extension {
		t.Fatalf("file name = %s", filepath.Base(l.Path()))
	}
	if err := l.Append(Entry{Op: OpHello, Name: "Home PC!"}, t0); err != nil {
		t.Fatal(err)
	}

	// Simulate a crash mid-write: a partial line with no terminator.
	f, _ := os.OpenFile(l.Path(), os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"v":1,"ts":5,"op":"ne`)
	f.Close()

	if got := ReadAll(dir); len(got) != 1 {
		t.Fatalf("partial line should be ignored, got %d entries", len(got))
	}
	if err := l.Append(newEntry("x", 0, time.Minute), t0); err != nil {
		t.Fatal(err)
	}
	got := ReadAll(dir)
	if len(got) != 2 || got[1].ID != "x" || got[1].Seq != 2 {
		t.Fatalf("next entry was glued to the torn line: %+v", got)
	}
}

func TestLogTimestampsNeverGoBackwards(t *testing.T) {
	dir := t.TempDir()
	l, _ := OpenLog(dir, "aaaaaaaa", "d")
	_ = l.Append(Entry{Op: OpHello}, t0)
	_ = l.Append(Entry{Op: OpHello}, t0.Add(-time.Hour)) // clock stepped back
	es := ReadAll(dir)
	if es[1].TS <= es[0].TS {
		t.Fatalf("timestamps went backwards: %d then %d", es[0].TS, es[1].TS)
	}
}

func TestOpenLogResumesSeqAndFindsRenamedFile(t *testing.T) {
	dir := t.TempDir()
	l, _ := OpenLog(dir, "bbbbbbbbcccc", "Old name")
	_ = l.Append(Entry{Op: OpHello}, t0)
	_ = l.Append(Entry{Op: OpHello}, t0)

	l2, _ := OpenLog(dir, "bbbbbbbbcccc", "New name")
	if l2.Path() != l.Path() {
		t.Fatalf("renamed device got a new file: %s vs %s", l2.Path(), l.Path())
	}
	_ = l2.Append(Entry{Op: OpHello}, t0)
	if es := ReadAll(dir); es[2].Seq != 3 {
		t.Fatalf("seq did not continue: %+v", es)
	}
}

func TestPruneDropsIdleTimersOnly(t *testing.T) {
	dir := t.TempDir()
	now := t0.Add(200 * time.Hour)

	a, _ := OpenLog(dir, "aaaaaaaa", "a")
	b, _ := OpenLog(dir, "bbbbbbbb", "b")
	_ = a.Append(Entry{Op: OpHello, Name: "a"}, t0)
	_ = a.Append(newEntry("old", 0, time.Minute), t0)                    // idle for 200h
	_ = a.Append(newEntry("busy", 0, time.Minute), t0)                   // kept alive by b below
	_ = a.Append(newEntry("fresh", 0, time.Minute), now.Add(-time.Hour)) // recent
	_ = b.Append(Entry{Op: OpDismiss, ID: "busy"}, now.Add(-time.Hour))

	bBefore, _ := os.ReadFile(b.Path())
	if err := a.Prune(now); err != nil {
		t.Fatal(err)
	}
	if err := b.Prune(now); err != nil {
		t.Fatal(err)
	}

	ids := map[string]bool{}
	for _, e := range ReadAll(dir) {
		ids[e.ID+"/"+string(e.Op)] = true
	}
	if ids["old/new"] {
		t.Error("idle timer's entries were kept")
	}
	if !ids["busy/new"] || !ids["fresh/new"] || !ids["/hello"] {
		t.Errorf("live entries were dropped: %v", ids)
	}
	if after, _ := os.ReadFile(b.Path()); string(after) != string(bBefore) {
		t.Error("prune rewrote a file with nothing to drop")
	}
	if _, err := os.Stat(a.Path() + ".tmp"); err == nil {
		t.Error("temp file left behind")
	}
}

func TestWatchSeesOtherDevicesButNotOwn(t *testing.T) {
	dir := t.TempDir()
	own, _ := OpenLog(dir, "aaaaaaaa", "me")
	other, _ := OpenLog(dir, "bbbbbbbb", "you")

	changed := make(chan struct{}, 4)
	ctx, cancel := contextWithCancel(t)
	defer cancel()
	go Watch(ctx, dir, own.Path(), 10*time.Millisecond, func() { changed <- struct{}{} })

	_ = own.Append(Entry{Op: OpHello}, t0)
	select {
	case <-changed:
		t.Fatal("own write woke the watcher")
	case <-time.After(100 * time.Millisecond):
	}

	_ = other.Append(Entry{Op: OpHello}, t0)
	select {
	case <-changed:
	case <-time.After(2 * time.Second):
		t.Fatal("other device's write was not noticed")
	}
}

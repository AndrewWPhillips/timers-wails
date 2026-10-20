package timersync

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Extension of a device's log file. Temp files used while pruning do not end in
// it, so readers never mistake one for a log.
const Extension = ".jsonl"

// RetainFor is how long an idle timer's entries are kept. It matches the longest
// timer, so a running timer's entries never age out while it is still counting.
const RetainFor = 100 * time.Hour

// Log is this device's own file in the sync folder. It is safe for concurrent
// use. The file is opened for each append rather than held open: a held handle
// would stop Prune replacing the file on Windows.
type Log struct {
	dir  string
	dev  string
	path string

	mu     sync.Mutex
	seq    int64
	lastTS int64
}

// OpenLog returns the log for device id in dir, creating the folder and the
// file if needed. An existing file for this device is found by its id suffix,
// so renaming the device does not orphan the file.
func OpenLog(dir, id, name string) (*Log, error) {
	if dir == "" || id == "" {
		return nil, errors.New("sync folder and device id are required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating sync folder: %w", err)
	}
	l := &Log{dir: dir, dev: id}

	matches, _ := filepath.Glob(filepath.Join(dir, "*-"+shortID(id)+Extension))
	if len(matches) > 0 {
		l.path = matches[0]
	} else {
		l.path = filepath.Join(dir, safeName(name)+"-"+shortID(id)+Extension)
	}

	for _, e := range readFile(l.path) {
		if e.Dev == id {
			l.seq = max(l.seq, e.Seq)
			l.lastTS = max(l.lastTS, e.TS)
		}
	}
	return l, nil
}

// Path is the log file's location. Its own writes should not wake the watcher.
func (l *Log) Path() string { return l.path }

// Append stamps e (version, time, device, sequence number) and adds it to the
// file. The time never goes backwards within one device, so a device's own
// changes replay in the order they were made even if its clock is adjusted.
func (l *Log) Append(e Entry, now time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.seq++
	l.lastTS = max(now.UnixMilli(), l.lastTS+1)
	e.V, e.TS, e.Dev, e.Seq = Version, l.lastTS, l.dev, l.seq

	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("opening %s: %w", l.path, err)
	}
	defer f.Close()

	// A crash part-way through a write leaves a line with no terminator. Start a
	// fresh line so the next entry is not glued on to it.
	if !endsWithNewline(f) {
		line = append([]byte{'\n'}, line...)
	}
	// One Write call, so a reader sees the whole line or (briefly) none of it.
	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("writing %s: %w", l.path, err)
	}
	return nil
}

// endsWithNewline reports whether f is empty or its last byte is '\n'.
func endsWithNewline(f *os.File) bool {
	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return true
	}
	var b [1]byte
	if _, err := f.ReadAt(b[:], st.Size()-1); err != nil {
		return true
	}
	return b[0] == '\n'
}

// Prune drops this device's entries for every timer whose latest entry, on any
// device, is older than RetainFor, and keeps everything else (including the
// device's hello). It rewrites the file to a temp file and renames it over the
// original, retrying for about a second because on Windows the sync tool may
// have the file open. Other devices' files are never touched.
func (l *Log) Prune(now time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	latest := make(map[string]int64)
	for _, e := range ReadAll(l.dir) {
		if e.ID != "" {
			latest[e.ID] = max(latest[e.ID], e.TS)
		}
	}
	cutoff := now.Add(-RetainFor).UnixMilli()

	own := readFile(l.path)
	var buf bytes.Buffer
	dropped := 0
	for _, e := range own {
		if e.ID != "" && latest[e.ID] < cutoff {
			dropped++
			continue
		}
		line, err := json.Marshal(e)
		if err != nil {
			return err
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if dropped == 0 {
		return nil
	}

	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	var err error
	for range 10 {
		if err = os.Rename(tmp, l.path); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = os.Remove(tmp)
	return fmt.Errorf("replacing %s: %w", l.path, err)
}

// ReadAll returns every entry from every log in dir. A missing folder, an
// unreadable file, a line that is not a whole entry (including an unterminated
// last line, which may still be being written) and an unknown version are all
// skipped rather than reported: one bad file must not stop the others syncing.
func ReadAll(dir string) []Entry {
	paths, _ := filepath.Glob(filepath.Join(dir, "*"+Extension))
	var all []Entry
	for _, p := range paths {
		all = append(all, readFile(p)...)
	}
	return all
}

func readFile(path string) []Entry {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return parse(bytes.NewReader(data))
}

func parse(r io.Reader) []Entry {
	br := bufio.NewReader(r)
	var out []Entry
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			// io.EOF with a partial line: unterminated, so ignore it.
			return out
		}
		var e Entry
		if json.Unmarshal(bytes.TrimSpace(line), &e) != nil || e.V != Version {
			continue
		}
		out = append(out, e)
	}
}

// shortID is the part of a device id used in its file name.
func shortID(id string) string {
	id = strings.ReplaceAll(id, "-", "")
	if len(id) > 8 {
		id = id[:8]
	}
	return id
}

// safeName turns a device name into something safe in a file name on every OS.
func safeName(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 40 {
		s = s[:40]
	}
	if s == "" {
		return "device"
	}
	return s
}

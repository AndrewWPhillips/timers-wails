// Package settings holds the user's configurable preferences and the persisted
// snapshot of their timers, and reads and writes them as JSON.
//
// Writes go through a temporary file and a rename so that a crash or a power cut
// midway through a save cannot leave a truncated file behind — losing the last
// change is acceptable, losing every preset is not.
package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/andrewwphillips/timers-wails/internal/timer"
)

// Version is the schema version written to the file. It exists so that a future
// change of shape can be migrated rather than silently misread.
const Version = 1

// Preset is one of the quick-set buttons.
type Preset struct {
	Label   string `json:"label"`
	Seconds int    `json:"seconds"`
}

// Alarm describes how an expired timer should sound.
type Alarm struct {
	// SoundFile is the path to a user-supplied sound. When empty the frontend
	// synthesises a beep instead, so the app always has a working alarm with
	// no bundled audio asset.
	SoundFile string `json:"soundFile"`
	// Volume is 0..1.
	Volume float64 `json:"volume"`
	// Muted silences the alarm sound; the visual flash still happens.
	Muted bool `json:"muted"`
}

// Settings is the whole persisted document.
type Settings struct {
	Version int           `json:"version"`
	Presets []Preset      `json:"presets"`
	Alarm   Alarm         `json:"alarm"`
	Timers  []timer.Timer `json:"timers"`
}

// DefaultPresets are the quick-set buttons a new install starts with.
func DefaultPresets() []Preset {
	return []Preset{
		{Label: "1 min", Seconds: 60},
		{Label: "5 min", Seconds: 5 * 60},
		{Label: "15 min", Seconds: 15 * 60},
		{Label: "1 hour", Seconds: 60 * 60},
		{Label: "2 hours", Seconds: 2 * 60 * 60},
	}
}

// Default returns the settings used when there is no file yet.
func Default() Settings {
	return Settings{
		Version: Version,
		Presets: DefaultPresets(),
		Alarm:   Alarm{Volume: 0.7},
	}
}

// Normalise repairs anything out of range, so that a hand-edited or
// partially-written file cannot put the UI into a broken state. It is applied
// on both load and save.
func (s *Settings) Normalise() {
	s.Version = Version

	valid := s.Presets[:0]
	for _, p := range s.Presets {
		if p.Seconds <= 0 {
			continue
		}
		if p.Label == "" {
			p.Label = formatPresetLabel(p.Seconds)
		}
		valid = append(valid, p)
	}
	s.Presets = valid

	// An empty preset row would leave the user no way back to the defaults.
	if len(s.Presets) == 0 {
		s.Presets = DefaultPresets()
	}

	switch {
	case s.Alarm.Volume <= 0:
		s.Alarm.Volume = 0.7
	case s.Alarm.Volume > 1:
		s.Alarm.Volume = 1
	}

	if s.Timers == nil {
		s.Timers = []timer.Timer{}
	}
}

// formatPresetLabel derives a readable label for a preset that has none.
func formatPresetLabel(seconds int) string {
	switch {
	case seconds%3600 == 0:
		return fmt.Sprintf("%d hour", seconds/3600)
	case seconds%60 == 0:
		return fmt.Sprintf("%d min", seconds/60)
	default:
		return fmt.Sprintf("%d sec", seconds)
	}
}

// Store reads and writes the settings file.
type Store struct {
	path string
}

// NewStore returns a Store backed by the file at path.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Path reports the file the Store reads and writes.
func (s *Store) Path() string {
	return s.path
}

// DefaultPath is the per-user settings file, e.g.
// %AppData%\timers\timers.json on Windows.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating user config dir: %w", err)
	}
	return filepath.Join(dir, "timers", "timers.json"), nil
}

// Load reads the settings file. A missing file is not an error: it yields the
// defaults, which is exactly what a first run needs.
func (s *Store) Load() (Settings, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Default(), fmt.Errorf("reading %s: %w", s.path, err)
	}

	var loaded Settings
	if err := json.Unmarshal(data, &loaded); err != nil {
		return Default(), fmt.Errorf("parsing %s: %w", s.path, err)
	}

	loaded.Normalise()
	return loaded, nil
}

// Save writes the settings atomically: a temporary file in the same directory
// (so the rename cannot cross a filesystem boundary) is renamed over the target.
func (s *Store) Save(cfg Settings) error {
	cfg.Normalise()

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding settings: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".timers-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	// Best-effort cleanup: harmless once the rename has consumed the file.
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", tmpName, err)
	}
	// Flush to disk before the rename, otherwise a power cut can leave the
	// renamed file present but empty.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmpName, err)
	}

	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replacing %s: %w", s.path, err)
	}
	return nil
}

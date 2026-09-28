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
	"math"
	"os"
	"path/filepath"
	"regexp"

	"github.com/andrewwphillips/timers-wails/internal/timer"
)

// Version is the schema version written to the file. It exists so that a future
// change of shape can be migrated rather than silently misread.
const Version = 1

// Preset is one of the quick-set buttons.
type Preset struct {
	Label   string `json:"label"`
	Seconds int    `json:"seconds"`
	// Color is a "#rrggbb" hex string, used for a timer's progress bar and
	// Pause button. Repaired by Normalise if missing or malformed.
	Color string `json:"color"`
}

// PresetColors is the fixed palette a new preset's colour is drawn from.
// Deliberately mixes bright, light shades with deeper, more saturated ones
// (rather than 16 colours of similar tone) so entries contrast with each
// other, not just with the background. It leans on red/rose more than a
// "safe" palette would, which does put a couple of entries near the alarm's
// own red/orange (see style.css --alarm/--alarm-bright) -- accepted as a
// trade-off for having real reds available. The order is shuffled (not
// grouped by hue) so that consecutive presets -- which get consecutive
// palette entries -- read as visually distinct rather than a gradient.
var PresetColors = []string{
	"#f87171", // red (light)
	"#22d3ee", // cyan
	"#fbbf24", // amber
	"#7c3aed", // violet (deep)
	"#4ade80", // green
	"#b91c1c", // red (deep)
	"#818cf8", // indigo
	"#a3e635", // lime
	"#c026d3", // fuchsia (deep)
	"#2dd4bf", // teal
	"#fb7185", // rose
	"#0284c7", // blue (deep)
	"#f472b6", // pink
	"#ca8a04", // gold (deep yellow)
	"#c084fc", // purple
	"#059669", // emerald (deep)
}

var hexColorRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// nextPresetColor returns the first palette colour not in use, or the first
// palette colour at all once every one of the 16 is already taken.
func nextPresetColor(used map[string]bool) string {
	for _, c := range PresetColors {
		if !used[c] {
			return c
		}
	}
	return PresetColors[0]
}

// Alarm describes how an expired timer should sound.
type Alarm struct {
	// SoundFile is the path to a user-supplied sound. When empty the frontend
	// synthesises a beep instead, so the app always has a working alarm with
	// no bundled audio asset.
	SoundFile string `json:"soundFile"`
	// The alarm starts at StartVolume and rises linearly to EndVolume over
	// RampSeconds, so it can begin gently and get insistent. Volumes are 0..1
	// and Normalise keeps StartVolume no higher than EndVolume; when they are
	// equal the volume is simply constant. A RampSeconds of 0 plays at
	// EndVolume straight away.
	StartVolume float64 `json:"startVolume"`
	EndVolume   float64 `json:"endVolume"`
	RampSeconds int     `json:"rampSeconds"`
	// Muted silences the alarm sound; the visual flash still happens.
	Muted bool `json:"muted"`
}

// VolumeStep is the granularity of the volume sliders, and the lowest end
// volume (an end volume of zero would make the alarm silent, which is what
// Muted is for).
const VolumeStep = 0.05

// MaxRampSeconds caps how long the alarm takes to reach full volume.
const MaxRampSeconds = 600

const (
	DefaultVolume      = 0.8
	DefaultRampSeconds = 60
)

// roundVolume snaps v to two decimal places, so repeated repairs cannot build
// up float noise like 0.6499999999.
func roundVolume(v float64) float64 {
	return math.Round(v*100) / 100
}

// WindowBounds is the main window's position and size when the app last
// closed, in device-independent pixels, so it reopens where the user left it.
// If it closed maximised, the bounds are the previously saved normal ones, so
// un-maximising after a restart returns to them.
type WindowBounds struct {
	X         int  `json:"x"`
	Y         int  `json:"y"`
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximised bool `json:"maximised,omitempty"`
}

// Settings is the whole persisted document.
type Settings struct {
	Version int           `json:"version"`
	Presets []Preset      `json:"presets"`
	Alarm   Alarm         `json:"alarm"`
	Timers  []timer.Timer `json:"timers"`
	// Window = window location at last close or nil if this is the 1st run. It is used to
	// set the main window location when the app is reopened but may be adjusted later (only
	// for OS=Windows) if the config file settings now have a location outside any screen,
	Window *WindowBounds `json:"window,omitempty"`
}

// DefaultPresets are the quick-set buttons a new install starts with.
func DefaultPresets() []Preset {
	return []Preset{
		{Label: "1 min", Seconds: 60, Color: PresetColors[0]},
		{Label: "5 min", Seconds: 5 * 60, Color: PresetColors[1]},
		{Label: "15 min", Seconds: 15 * 60, Color: PresetColors[2]},
		//{Label: "30 min", Seconds: 60 * 60, Color: PresetColors[3]},
		{Label: "1 hour", Seconds: 60 * 60, Color: PresetColors[4]},
		//{Label: "2 hours", Seconds: 2 * 60 * 60, Color: PresetColors[5]},
	}
}

// Default returns the settings used when there is no file yet.
func Default() Settings {
	return Settings{
		Version: Version,
		Presets: DefaultPresets(),
		Alarm:   Alarm{StartVolume: DefaultVolume, EndVolume: DefaultVolume, RampSeconds: DefaultRampSeconds},
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

	// Repair a missing or malformed colour (a hand-edited file, or one saved
	// before this field existed). This deliberately does not force every
	// preset to have a unique colour -- two presets sharing one because the
	// user picked it that way are left alone; only a colour that was never
	// validly set is replaced.
	used := make(map[string]bool, len(s.Presets))
	for _, p := range s.Presets {
		if hexColorRE.MatchString(p.Color) {
			used[p.Color] = true
		}
	}
	for i, p := range s.Presets {
		if hexColorRE.MatchString(p.Color) {
			continue
		}
		c := nextPresetColor(used)
		s.Presets[i].Color = c
		used[c] = true
	}

	s.Alarm.normalise()

	// A zero or negative size can only come from a hand-edited or corrupt
	// file; forget it and fall back to the default placement.
	if s.Window != nil && (s.Window.Width <= 0 || s.Window.Height <= 0) {
		s.Window = nil
	}

	if s.Timers == nil {
		s.Timers = []timer.Timer{}
	}
}

// normalise keeps the volumes in 0..1 with StartVolume no higher than
// EndVolume, and the ramp within 0..MaxRampSeconds.
func (a *Alarm) normalise() {
	switch {
	case a.EndVolume <= 0:
		// Zero is never a valid end volume, so it means "not set".
		a.EndVolume = DefaultVolume
	case a.EndVolume < VolumeStep:
		a.EndVolume = VolumeStep
	case a.EndVolume > 1:
		a.EndVolume = 1
	}
	a.EndVolume = roundVolume(a.EndVolume)

	if a.StartVolume < 0 {
		a.StartVolume = 0
	}
	if a.StartVolume > a.EndVolume {
		a.StartVolume = a.EndVolume
	}
	a.StartVolume = roundVolume(a.StartVolume)

	switch {
	case a.RampSeconds < 0:
		a.RampSeconds = 0
	case a.RampSeconds > MaxRampSeconds:
		a.RampSeconds = MaxRampSeconds
	}
}

// migrateLegacyAlarm carries over the single "volume" setting from files
// written before start/end volumes existed. It becomes both the start and end
// volume, so an upgraded install sounds exactly as it did before; the ramp
// gets its default, ready for if the user lowers the start volume later.
func migrateLegacyAlarm(data []byte, s *Settings) {
	var legacy struct {
		Alarm struct {
			Volume *float64 `json:"volume"`
		} `json:"alarm"`
	}
	if json.Unmarshal(data, &legacy) != nil || legacy.Alarm.Volume == nil {
		return
	}
	if s.Alarm.EndVolume == 0 {
		s.Alarm.StartVolume = *legacy.Alarm.Volume
		s.Alarm.EndVolume = *legacy.Alarm.Volume
		s.Alarm.RampSeconds = DefaultRampSeconds
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

	migrateLegacyAlarm(data, &loaded)
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

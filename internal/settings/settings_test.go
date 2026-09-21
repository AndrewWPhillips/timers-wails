package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/andrewwphillips/timers-wails/internal/timer"
)

func storeInTempDir(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "timers.json"))
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	s := storeInTempDir(t)

	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}

	if len(got.Presets) != len(DefaultPresets()) {
		t.Errorf("got %d presets, want %d", len(got.Presets), len(DefaultPresets()))
	}
	if got.Version != Version {
		t.Errorf("Version = %d, want %d", got.Version, Version)
	}
}

func TestDefaultPresetsMatchTheSpecifiedTimes(t *testing.T) {
	want := []int{60, 300, 900, 3600, 7200}

	got := DefaultPresets()
	if len(got) != len(want) {
		t.Fatalf("got %d presets, want %d", len(got), len(want))
	}
	for i, seconds := range want {
		if got[i].Seconds != seconds {
			t.Errorf("preset %d = %ds (%q), want %ds", i, got[i].Seconds, got[i].Label, seconds)
		}
		if got[i].Label == "" {
			t.Errorf("preset %d has no label", i)
		}
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := storeInTempDir(t)

	want := Settings{
		Presets: []Preset{{Label: "Brew", Seconds: 210}},
		Alarm:   Alarm{SoundFile: `C:\sounds\gong.wav`, Volume: 0.4},
		Timers: []timer.Timer{{
			ID: "abc", Label: "pasta", TotalMS: 600000, RemainingMS: 600000,
			EndsAtMS: 1789000000000, State: timer.StateRunning, CreatedMS: 1788000000000,
		}},
	}

	if err := s.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(got.Presets) != 1 || got.Presets[0] != want.Presets[0] {
		t.Errorf("Presets = %+v, want %+v", got.Presets, want.Presets)
	}
	if got.Alarm != want.Alarm {
		t.Errorf("Alarm = %+v, want %+v", got.Alarm, want.Alarm)
	}
	if len(got.Timers) != 1 || got.Timers[0] != want.Timers[0] {
		t.Errorf("Timers = %+v, want %+v", got.Timers, want.Timers)
	}
}

func TestSaveCreatesMissingDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", "timers.json")
	s := NewStore(path)

	if err := s.Save(Default()); err != nil {
		t.Fatalf("Save into missing directories: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("settings file not created: %v", err)
	}
}

func TestSaveLeavesNoTempFilesBehind(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(filepath.Join(dir, "timers.json"))

	for range 3 {
		if err := s.Save(Default()); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("directory holds %v, want only timers.json", names)
	}
}

func TestSaveOverwritesExistingFile(t *testing.T) {
	s := storeInTempDir(t)

	first := Default()
	first.Presets = []Preset{{Label: "one", Seconds: 1}}
	if err := s.Save(first); err != nil {
		t.Fatalf("Save: %v", err)
	}

	second := Default()
	second.Presets = []Preset{{Label: "two", Seconds: 2}}
	if err := s.Save(second); err != nil {
		t.Fatalf("Save over existing file: %v", err)
	}

	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Presets) != 1 || got.Presets[0].Label != "two" {
		t.Errorf("Presets = %+v, want the second save", got.Presets)
	}
}

func TestLoadCorruptFileFallsBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "timers.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := NewStore(path).Load()
	if err == nil {
		t.Error("Load on corrupt file returned no error, want one so the app can log it")
	}
	// The caller is expected to carry on with what it was handed, so the
	// returned value must still be usable rather than a zero Settings.
	if len(got.Presets) != len(DefaultPresets()) {
		t.Errorf("got %d presets, want the defaults", len(got.Presets))
	}
}

func TestNormalise(t *testing.T) {
	tests := []struct {
		name  string
		in    Settings
		check func(*testing.T, Settings)
	}{
		{
			name: "drops presets with no duration",
			in:   Settings{Presets: []Preset{{Label: "ok", Seconds: 60}, {Label: "bad", Seconds: 0}, {Label: "worse", Seconds: -5}}},
			check: func(t *testing.T, s Settings) {
				if len(s.Presets) != 1 || s.Presets[0].Label != "ok" {
					t.Errorf("Presets = %+v, want only the valid one", s.Presets)
				}
			},
		},
		{
			name: "restores defaults when every preset is dropped",
			in:   Settings{Presets: []Preset{{Seconds: 0}}},
			check: func(t *testing.T, s Settings) {
				if len(s.Presets) != len(DefaultPresets()) {
					t.Errorf("got %d presets, want the defaults back", len(s.Presets))
				}
			},
		},
		{
			name: "labels an unlabelled preset",
			in:   Settings{Presets: []Preset{{Seconds: 3600}, {Seconds: 300}, {Seconds: 45}}},
			check: func(t *testing.T, s Settings) {
				want := []string{"1 hour", "5 min", "45 sec"}
				for i, w := range want {
					if s.Presets[i].Label != w {
						t.Errorf("preset %d label = %q, want %q", i, s.Presets[i].Label, w)
					}
				}
			},
		},
		{
			name: "clamps volume above one",
			in:   Settings{Presets: DefaultPresets(), Alarm: Alarm{Volume: 4}},
			check: func(t *testing.T, s Settings) {
				if s.Alarm.Volume != 1 {
					t.Errorf("Volume = %v, want 1", s.Alarm.Volume)
				}
			},
		},
		{
			name: "replaces a zero volume with the default",
			in:   Settings{Presets: DefaultPresets(), Alarm: Alarm{Volume: 0}},
			check: func(t *testing.T, s Settings) {
				if s.Alarm.Volume != 0.7 {
					t.Errorf("Volume = %v, want 0.7", s.Alarm.Volume)
				}
			},
		},
		{
			name: "sets the current version",
			in:   Settings{Presets: DefaultPresets(), Version: 0},
			check: func(t *testing.T, s Settings) {
				if s.Version != Version {
					t.Errorf("Version = %d, want %d", s.Version, Version)
				}
			},
		},
		{
			name: "gives Timers a non-nil slice so it encodes as [] not null",
			in:   Settings{Presets: DefaultPresets()},
			check: func(t *testing.T, s Settings) {
				if s.Timers == nil {
					t.Error("Timers is nil, want an empty slice")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in
			got.Normalise()
			tc.check(t, got)
		})
	}
}

func TestSavedFileIsReadableJSON(t *testing.T) {
	s := storeInTempDir(t)
	if err := s.Save(Default()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var generic map[string]any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("saved file is not valid JSON: %v", err)
	}
	for _, key := range []string{"version", "presets", "alarm", "timers"} {
		if _, ok := generic[key]; !ok {
			t.Errorf("saved file has no %q key", key)
		}
	}
}

func TestDefaultPathIsUnderUserConfigDir(t *testing.T) {
	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if filepath.Base(got) != "timers.json" {
		t.Errorf("DefaultPath = %q, want it to end in timers.json", got)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("DefaultPath = %q, want an absolute path", got)
	}
}

func TestLoadReportsUnreadablePath(t *testing.T) {
	// A directory where a file is expected: ReadFile fails with something
	// other than "not exist", so Load must surface it rather than silently
	// pretending this is a first run.
	dir := t.TempDir()
	if _, err := NewStore(dir).Load(); err == nil {
		t.Error("Load on a directory returned no error, want one")
	}
}

func TestSaveReportsUncreatableDirectory(t *testing.T) {
	// A plain file standing where the settings directory should be makes
	// MkdirAll fail.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s := NewStore(filepath.Join(blocker, "timers.json"))
	if err := s.Save(Default()); err == nil {
		t.Error("Save under a file path returned no error, want one")
	}
}

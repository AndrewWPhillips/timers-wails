package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/andrewwphillips/timers-wails/internal/settings"
	"github.com/andrewwphillips/timers-wails/internal/timer"
	"github.com/andrewwphillips/timers-wails/internal/timersync"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// How often the sync folder is checked for other devices' changes, and how often
// this device's own log is pruned.
const (
	syncPollInterval  = 2 * time.Second
	syncPruneInterval = time.Hour
)

// syncing reports whether sharing timers through a folder is on. Callers must
// hold s.syncMu.
func (s *TimerService) syncing() bool { return s.syncLog != nil }

// startSync turns sharing on if the settings name a folder. Callers must hold
// s.syncMu. On failure sharing stays off and the timers keep working locally.
//
// Timers this device already has that the folder does not (those saved before
// sync was on, or when switching folders) are added to this device's log, then
// the timers are rebuilt from every device's log.
func (s *TimerService) startSync() error {
	s.mu.Lock()
	cfg := s.cfg.Sync
	s.mu.Unlock()
	if cfg.Folder == "" {
		return nil
	}

	if cfg.DeviceID == "" {
		cfg.DeviceID = newDeviceID()
	}
	if cfg.DeviceName == "" {
		cfg.DeviceName, _ = os.Hostname()
	}
	s.mu.Lock()
	s.cfg.Sync = cfg
	s.mu.Unlock()

	log, err := timersync.OpenLog(cfg.Folder, cfg.DeviceID, cfg.DeviceName)
	if err != nil {
		return err
	}
	now := time.Now()
	if err := log.Append(timersync.Entry{Op: timersync.OpHello, Name: cfg.DeviceName}, now); err != nil {
		return err
	}
	if err := log.Prune(now); err != nil {
		slog.Warn("could not prune the sync log", "error", err)
	}
	s.exportMissing(log, cfg.Folder)

	s.syncLog = log
	s.syncOn.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	s.syncCancel = cancel
	go timersync.Watch(ctx, cfg.Folder, log.Path(), syncPollInterval, func() {
		s.syncMu.Lock()
		defer s.syncMu.Unlock()
		if s.syncLog == log {
			s.refold(true)
		}
	})
	go s.pruneLoop(ctx, log)

	s.refold(false)
	return nil
}

// stopSync turns sharing off. Callers must hold s.syncMu. The timers stay as
// they are and are saved locally again.
func (s *TimerService) stopSync() {
	if s.syncCancel != nil {
		s.syncCancel()
	}
	s.syncLog, s.syncCancel = nil, nil
	s.syncOn.Store(false)
}

func (s *TimerService) pruneLoop(ctx context.Context, log *timersync.Log) {
	t := time.NewTicker(syncPruneInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := log.Prune(time.Now()); err != nil {
				slog.Warn("could not prune the sync log", "error", err)
			}
		}
	}
}

// exportMissing writes the timers this device holds into log, unless the folder
// already knows them.
func (s *TimerService) exportMissing(log *timersync.Log, dir string) {
	known := make(map[string]bool)
	for _, t := range timersync.Fold(timersync.ReadAll(dir), time.Now()) {
		known[t.ID] = true
	}
	now := time.Now()
	for _, t := range s.manager.List() {
		if known[t.ID] {
			continue
		}
		endsAt := t.EndsAtMS
		if t.State == timer.StatePaused {
			endsAt = now.Add(time.Duration(t.RemainingMS) * time.Millisecond).UnixMilli()
		}
		write := func(e timersync.Entry) {
			e.ID = t.ID
			if err := log.Append(e, now); err != nil {
				slog.Error("could not write to the sync log", "error", err)
			}
		}
		write(timersync.Entry{Op: timersync.OpNew, Label: t.Label, Color: t.Color, TotalMS: t.TotalMS, EndsAt: endsAt})
		if t.Repeat {
			write(timersync.Entry{Op: timersync.OpRepeat, Repeat: true})
		}
		switch {
		case t.State == timer.StatePaused:
			write(timersync.Entry{Op: timersync.OpPause, RemainingMS: t.RemainingMS})
		case t.State == timer.StateExpired && !t.Alarming:
			write(timersync.Entry{Op: timersync.OpDismiss, EndsAt: t.EndsAtMS, RemainingMS: t.RemainingMS})
		}
	}
}

// refold rebuilds the timers from every device's log and tells the frontend.
// When alarm is true, timers that have just started alarming also raise the
// alarm. Callers must hold s.syncMu.
func (s *TimerService) refold(alarm bool) {
	s.mu.Lock()
	dir := s.cfg.Sync.Folder
	s.mu.Unlock()

	timers := timersync.Fold(timersync.ReadAll(dir), time.Now())
	started := s.manager.Replace(timers)
	if alarm && len(started) > 0 {
		s.emit(EventExpired, ExpiredEvent{Timers: started})
	}
	s.emit(EventChanged, ChangedEvent{Timers: s.manager.List()})
}

// record adds a change this device made to its log. Callers must hold s.syncMu.
func (s *TimerService) record(e timersync.Entry) {
	if !s.syncing() {
		return
	}
	if err := s.syncLog.Append(e, time.Now()); err != nil {
		slog.Error("could not write to the sync log", "error", err)
	}
}

// syncResult runs a change to one timer and records it. It holds s.syncMu so a
// refold cannot see the change in the manager before it is in the log (which
// would briefly undo it).
func (s *TimerService) syncResult(t timer.Timer, err error, e timersync.Entry) (timer.Timer, error) {
	if err != nil {
		return timer.Timer{}, err
	}
	e.ID = t.ID
	s.record(e)
	return t, nil
}

// PickSyncFolder opens a native folder picker and returns the chosen folder, or
// an empty string if the user cancelled.
func (s *TimerService) PickSyncFolder() (string, error) {
	app := application.Get()
	if app == nil {
		return "", nil
	}
	dialog := app.Dialog.OpenFile()
	dialog.SetTitle("Choose the folder to share timers through")
	dialog.CanChooseFiles(false)
	dialog.CanChooseDirectories(true)
	dialog.CanCreateDirectories(true)
	return dialog.PromptForSingleSelection()
}

// applySync starts, stops or restarts sharing after the settings changed.
func (s *TimerService) applySync(old, now settings.Sync) error {
	if old.Folder == now.Folder && old.DeviceName == now.DeviceName {
		return nil
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	s.stopSync()
	if err := s.startSync(); err != nil {
		return fmt.Errorf("could not start syncing: %w", err)
	}
	return nil
}

func newDeviceID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

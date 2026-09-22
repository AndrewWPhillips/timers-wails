import { computed, onUnmounted, ref } from "vue";
import { Events } from "@wailsio/runtime";

import { TimerService } from "../../bindings/github.com/andrewwphillips/timers-wails";
import type { Alarm, Preset } from "../../bindings/github.com/andrewwphillips/timers-wails/internal/settings";
import type { Timer } from "../../bindings/github.com/andrewwphillips/timers-wails/internal/timer";

/**
 * Preferences with the slice guaranteed present.
 *
 * Go encodes a nil slice as null, so the generated Preferences type has
 * `presets: Preset[] | null`. Coercing once here, at the boundary, keeps every
 * component downstream free of null checks.
 */
export interface Prefs {
  presets: Preset[];
  alarm: Alarm;
}

/**
 * The app's timer state.
 *
 * Go holds the authoritative state; this mirrors it. Mutations go to Go and the
 * updated timer it returns is written back here, so the two never drift. Changes
 * Go makes on its own — a timer expiring, or a batch of them expiring together
 * after the machine wakes — arrive as events.
 *
 * Note there is no per-tick traffic across the bridge in either direction: the
 * countdown itself is derived locally from each timer's deadline.
 */
export function useTimers() {
  const timers = ref<Timer[]>([]);
  const preferences = ref<Prefs>({ presets: [], alarm: { soundFile: "", volume: 0.7, muted: false } });
  /** The fixed palette a new preset's colour is drawn from. Fetched from Go
   *  (the only place it is defined) rather than hardcoded here too. */
  const presetColors = ref<string[]>([]);
  const error = ref("");
  const loading = ref(true);

  /** True while any timer is waiting to be dismissed. Drives the alarm. */
  const alarming = computed(() => timers.value.some((t) => t.alarming));

  function replace(updated: Timer): void {
    const index = timers.value.findIndex((t) => t.id === updated.id);
    if (index === -1) {
      timers.value = [...timers.value, updated];
      return;
    }
    const next = timers.value.slice();
    next[index] = updated;
    timers.value = next;
  }

  /** Runs a binding call, surfacing any failure rather than losing it. */
  async function attempt<T>(action: () => Promise<T>): Promise<T | undefined> {
    try {
      const result = await action();
      error.value = "";
      return result;
    } catch (err) {
      error.value = err instanceof Error ? err.message : String(err);
      return undefined;
    }
  }

  async function refresh(): Promise<void> {
    // ListTimers returns null rather than [] for an empty list, since that is
    // how Go encodes a nil slice.
    const list = await attempt(() => TimerService.ListTimers());
    timers.value = list ?? [];
  }

  async function load(): Promise<void> {
    loading.value = true;
    const prefs = await attempt(() => TimerService.GetPreferences());
    if (prefs !== undefined) {
      preferences.value = { presets: prefs.presets ?? [], alarm: prefs.alarm };
    }
    const colors = await attempt(() => TimerService.PresetColors());
    presetColors.value = colors ?? [];
    await refresh();
    loading.value = false;
  }

  async function create(
    hours: number,
    minutes: number,
    seconds: number,
    label: string,
    color = "",
  ): Promise<boolean> {
    const created = await attempt(() => TimerService.CreateTimer(hours, minutes, seconds, label, color));
    if (created === undefined) {
      return false;
    }
    timers.value = [...timers.value, created];
    return true;
  }

  async function pause(id: string): Promise<void> {
    const updated = await attempt(() => TimerService.PauseTimer(id));
    if (updated !== undefined) replace(updated);
  }

  async function resume(id: string): Promise<void> {
    const updated = await attempt(() => TimerService.ResumeTimer(id));
    if (updated !== undefined) replace(updated);
  }

  async function restart(id: string): Promise<void> {
    const updated = await attempt(() => TimerService.RestartTimer(id));
    if (updated !== undefined) replace(updated);
  }

  async function dismiss(id: string): Promise<void> {
    const updated = await attempt(() => TimerService.DismissTimer(id));
    if (updated !== undefined) replace(updated);
  }

  async function remove(id: string): Promise<void> {
    const removed = await attempt(async () => {
      await TimerService.DeleteTimer(id);
      return true;
    });
    if (removed) {
      timers.value = timers.value.filter((t) => t.id !== id);
    }
  }

  async function savePreferences(next: Prefs): Promise<boolean> {
    // Go returns the settings as stored, which may have been repaired, so the
    // UI shows what was actually saved rather than what was typed.
    const saved = await attempt(() => TimerService.SavePreferences(next));
    if (saved === undefined) {
      return false;
    }
    preferences.value = { presets: saved.presets ?? [], alarm: saved.alarm };
    return true;
  }

  async function pickAlarmFile(): Promise<string | undefined> {
    return attempt(() => TimerService.PickAlarmFile());
  }

  /** ids of timers that expired most recently, so the UI can bring them into view. */
  const justExpired = ref<string[]>([]);

  const offChanged = Events.On("timers:changed", (event) => {
    timers.value = event.data.timers ?? [];
  });

  const offExpired = Events.On("timers:expired", (event) => {
    justExpired.value = (event.data.timers ?? []).map((t) => t.id);
  });

  onUnmounted(() => {
    offChanged();
    offExpired();
  });

  return {
    timers,
    preferences,
    presetColors,
    error,
    loading,
    alarming,
    justExpired,
    load,
    refresh,
    create,
    pause,
    resume,
    restart,
    dismiss,
    remove,
    savePreferences,
    pickAlarmFile,
  };
}

<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref, watch } from "vue";

import NewTimerForm from "./components/NewTimerForm.vue";
import PresetBar from "./components/PresetBar.vue";
import SettingsDialog from "./components/SettingsDialog.vue";
import TimerCard from "./components/TimerCard.vue";
import { startAlarm, stopAlarm, unlockAudio } from "./composables/useAlarm";
import { useTicker } from "./composables/useTicker";
import { useTimers } from "./composables/useTimers";

const now = useTicker();
const {
  timers,
  preferences,
  presetColors,
  error,
  loading,
  alarming,
  justExpired,
  load,
  create,
  pause,
  resume,
  restart,
  dismiss,
  remove,
  savePreferences,
  pickAlarmFile,
} = useTimers();

const showSettings = ref(false);
const cards = ref<HTMLElement | null>(null);

function alarmOptions() {
  return {
    volume: preferences.value.alarm.volume,
    muted: preferences.value.alarm.muted,
    soundFile: preferences.value.alarm.soundFile,
  };
}

/**
 * The alarm follows the state rather than the event.
 *
 * Driving it from "is any timer alarming" covers the awkward case for free: a
 * timer that expired while the app was closed comes back already marked as
 * alarming, so it sounds on launch without needing an event that nothing was
 * listening for at the time.
 */
watch(alarming, (on) => {
  if (on) {
    startAlarm(alarmOptions());
  } else {
    stopAlarm();
  }
});

/** Bring a timer that has just gone off into view; with many timers running the
 *  one that finished may well be scrolled off screen. */
watch(justExpired, async (ids) => {
  if (ids.length === 0) {
    return;
  }
  await nextTick();
  const card = cards.value?.querySelector(`[data-timer-id="${ids[0]}"]`);
  card?.scrollIntoView({ behavior: "smooth", block: "nearest" });
});

async function startPreset(seconds: number, label: string, color: string): Promise<void> {
  await create(0, 0, seconds, label, color);
}

async function onSave(next: typeof preferences.value): Promise<void> {
  if (await savePreferences(next)) {
    showSettings.value = false;
  }
}

let testTimeout: ReturnType<typeof setTimeout> | null = null;

/**
 * Plays a preview using whatever is currently on screen in the settings
 * dialog, not the last-saved preferences -- otherwise Test would keep
 * previewing stale values until the user hits Save and reopens the dialog.
 * Always audible even if "Silent" is ticked, since previewing is the point.
 */
function testAlarm(alarm: { soundFile: string; volume: number }): void {
  stopAlarm();
  startAlarm({ volume: alarm.volume, muted: false, soundFile: alarm.soundFile });
  if (testTimeout !== null) {
    clearTimeout(testTimeout);
  }
  testTimeout = setTimeout(() => {
    // Never silence a real alarm that started while the sample was playing.
    if (!alarming.value) {
      stopAlarm();
    }
  }, 3500);
}

/**
 * WebView2 follows Chromium's autoplay policy: an AudioContext built before any
 * user interaction stays suspended and plays nothing. Unlocking on the first
 * pointer or key event means the alarm is always armed by the time a timer the
 * user set can possibly finish.
 */
function armAudio(): void {
  unlockAudio();
  window.removeEventListener("pointerdown", armAudio);
  window.removeEventListener("keydown", armAudio);
}

onMounted(() => {
  window.addEventListener("pointerdown", armAudio);
  window.addEventListener("keydown", armAudio);
  void load();
});

onUnmounted(() => {
  window.removeEventListener("pointerdown", armAudio);
  window.removeEventListener("keydown", armAudio);
  if (testTimeout !== null) {
    clearTimeout(testTimeout);
  }
  stopAlarm();
});
</script>

<template>
  <main class="app">
    <header class="top">
      <h1>Timers</h1>
      <span v-if="timers.length" class="count">{{ timers.length }} running</span>
    </header>

    <section class="panel">
      <PresetBar :presets="preferences.presets" @start="startPreset" @edit="showSettings = true" />
      <NewTimerForm @start="create" />
    </section>

    <p v-if="error" class="error" role="alert">{{ error }}</p>

    <section ref="cards" class="cards">
      <TimerCard
        v-for="timer in timers"
        :key="timer.id"
        :data-timer-id="timer.id"
        :timer="timer"
        :now="now"
        @pause="pause"
        @resume="resume"
        @restart="restart"
        @dismiss="dismiss"
        @remove="remove"
      />

      <p v-if="!loading && timers.length === 0" class="empty">
        No timers yet. Pick a preset above, or set your own.
      </p>
    </section>

    <SettingsDialog
      v-if="showSettings"
      :preferences="preferences"
      :preset-colors="presetColors"
      :pick-alarm-file="pickAlarmFile"
      @save="onSave"
      @close="showSettings = false"
      @test="testAlarm"
    />
  </main>
</template>

<style scoped>
.app {
  max-width: 640px;
  margin: 0 auto;
  padding: 18px 18px 28px;
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-height: 100vh;
}

.top {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
}

h1 {
  margin: 0;
  font-size: 1.15rem;
  font-weight: 600;
  letter-spacing: -0.01em;
}

.count {
  color: var(--muted);
  font-size: 0.78rem;
}

.panel {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: 12px;
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.cards {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.empty {
  color: var(--muted);
  font-size: 0.85rem;
  text-align: center;
  padding: 28px 0;
  margin: 0;
}

.error {
  background: var(--danger-soft);
  border: 1px solid var(--danger);
  color: var(--text);
  border-radius: 8px;
  padding: 8px 12px;
  font-size: 0.82rem;
  margin: 0;
}
</style>

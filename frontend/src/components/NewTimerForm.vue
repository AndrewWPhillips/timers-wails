<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";

import { TimerService } from "../../bindings/github.com/andrewwphillips/timers-wails";
import { vDragNumber } from "../directives/dragNumber";
import { endTimeTooltip, formatDuration, formatEndTime, msUntilEndTimeChanges, toSeconds } from "../lib/format";

// No name is asked for here: a new timer starts as "Untitled" and can be
// renamed at any time by clicking its name on the card.
const emit = defineEmits<{
  start: [hours: number, minutes: number, seconds: number];
}>();

// Start at zero; the values last used are restored from the settings file on
// mount (see below).
const hours = ref(0);
const minutes = ref(0);
const seconds = ref(0);

/** Empty inputs read as 0 rather than NaN, so a cleared field is not an error. */
function value(input: number | string): number {
  const n = typeof input === "number" ? input : Number(input);
  return Number.isFinite(n) && n > 0 ? Math.floor(n) : 0;
}

/**
 * The controls are remembered across restarts, as the three values entered
 * rather than a total, so 80 minutes comes back as 80 minutes. Changes are
 * saved after a short pause, since dragging a value fires an input event for
 * every step.
 */
const saveDelayMs = 400;
let saveTimer: ReturnType<typeof setTimeout> | null = null;
/** Not until the saved values have been restored, so the restore itself (or
 *  an early edit racing it) never overwrites them with the zero defaults. */
let restored = false;

onMounted(async () => {
  try {
    const saved = await TimerService.GetTimerControls();
    hours.value = saved.hours;
    minutes.value = saved.minutes;
    seconds.value = saved.seconds;
  } catch {
    // Not fatal: the controls just start at zero.
  }
  restored = true;
});

watch([hours, minutes, seconds], () => {
  if (!restored) {
    return;
  }
  if (saveTimer !== null) {
    clearTimeout(saveTimer);
  }
  saveTimer = setTimeout(saveControls, saveDelayMs);
});

function saveControls(): void {
  saveTimer = null;
  void TimerService.SaveTimerControls({
    hours: value(hours.value),
    minutes: value(minutes.value),
    seconds: value(seconds.value),
  }).catch(() => {
    // Losing the remembered controls is harmless; nothing to tell the user.
  });
}

onUnmounted(() => {
  if (saveTimer !== null) {
    clearTimeout(saveTimer);
    saveControls();
  }
});

const totalSeconds = computed(() => toSeconds(value(hours.value), value(minutes.value), value(seconds.value)));
const valid = computed(() => totalSeconds.value > 0);

/**
 * When a timer started now would end, to the nearest minute. `now` is only
 * moved on when the rounded result would actually change -- once a minute --
 * rather than ticking every second, and is refreshed straight away whenever
 * the duration is edited.
 */
const now = ref(Date.now());
let clockTimer: ReturnType<typeof setTimeout> | null = null;

const endTime = computed(() => formatEndTime(now.value + totalSeconds.value * 1000, now.value));

function refreshClock(): void {
  now.value = Date.now();
  if (clockTimer !== null) {
    clearTimeout(clockTimer);
  }
  // A little past the change point, so the rounding has definitely flipped.
  const delay = msUntilEndTimeChanges(now.value + totalSeconds.value * 1000) + 50;
  clockTimer = setTimeout(refreshClock, delay);
}

watch(totalSeconds, refreshClock);

/** Timers in a hidden or minimised window can be delayed by the browser, so
 *  catch up as soon as it is visible again. */
function onVisibility(): void {
  if (document.visibilityState === "visible") {
    refreshClock();
  }
}

onMounted(() => {
  refreshClock();
  document.addEventListener("visibilitychange", onVisibility);
});

onUnmounted(() => {
  if (clockTimer !== null) {
    clearTimeout(clockTimer);
  }
  document.removeEventListener("visibilitychange", onVisibility);
});

function submit(): void {
  if (!valid.value) {
    return;
  }
  emit("start", value(hours.value), value(minutes.value), value(seconds.value));
}
</script>

<template>
  <form class="form" @submit.prevent="submit">
    <div class="row">
      <div class="fields">
        <label class="field time hours">
          <span>Hour</span>
          <input v-model="hours" v-drag-number type="number" min="0" max="99" inputmode="numeric" />
        </label>
        <label class="field time">
          <span>Min</span>
          <input v-model="minutes" v-drag-number type="number" min="0" inputmode="numeric" />
        </label>
        <label class="field time">
          <span>Sec</span>
          <input v-model="seconds" v-drag-number type="number" min="0" inputmode="numeric" />
        </label>
      </div>

      <!-- The end time sits beside Start when there is room, or above it in a
           narrow window (see the media query below). -->
      <div class="go">
        <p class="hint" :class="{ muted: !valid }" :title="valid ? endTimeTooltip : undefined">
          <template v-if="valid">
            <span class="long-hint">{{ endTime }} ({{ formatDuration(totalSeconds) }})</span>
            <span class="short-hint">{{ endTime }}</span>
          </template>
          <template v-else>
            <span class="long-hint">Click and drag to set the time, then click Start</span>
            <span class="short-hint">Set time, click Start</span>
          </template>
        </p>

        <button type="submit" class="start" :disabled="!valid">Start</button>
      </div>
    </div>
  </form>
</template>

<style scoped>
.form {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.fields {
  display: flex;
  gap: 8px;
}

.row {
  display: flex;
  align-items: flex-end;
  gap: 8px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

/* Minutes and seconds are not limited to 59, so they are wide enough for
   three digits beside the native up/down spinner (which reserves its space
   even when hidden). Hours never exceed 99, so that one can be narrower. */
.field.time {
  flex: none;
  width: 64px;
}

.field.time.hours {
  width: 52px;
}

.field span {
  font-size: 0.72rem;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--muted);
}

.field em {
  font-style: normal;
  text-transform: none;
  letter-spacing: 0;
  opacity: 0.6;
}

input {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: 8px;
  color: var(--text);
  font-size: 1rem;
  padding: 8px 6px;
  width: 100%;
  font-variant-numeric: tabular-nums;
}

input:focus {
  outline: none;
  border-color: var(--accent);
}

.field.time input {
  cursor: ns-resize;
}

.field.time input.is-scrubbing {
  border-color: var(--accent);
  background: var(--surface-raised);
}

.start {
  background: var(--accent);
  color: #fff;
  border: none;
  border-radius: 8px;
  padding: 9px 20px;
  font-size: 0.9rem;
  font-weight: 600;
  cursor: pointer;
  white-space: nowrap;
}

.start:hover:not(:disabled) {
  background: var(--accent-bright);
}

.start:disabled {
  opacity: 0.4;
  cursor: default;
}

.hint {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin: 0;
  font-size: 0.78rem;
  color: var(--text);
}

.hint.muted {
  color: var(--muted);
}

/* The end time and Start, side by side: the end time takes the space between
   the time fields and Start. */
.go {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: flex-end;
  gap: 8px;
}

/* The long hint (the end time plus the duration, or the full instructions)
   only fits in a wide window; otherwise the short one is used (the end time
   alone, or "Set time, click Start"). */
.hint .long-hint {
  display: none;
}

@media (min-width: 420px) {
  .hint .long-hint {
    display: inline;
  }

  .hint .short-hint {
    display: none;
  }
}

/* In a narrow window the time fields and Start leave no room for the end
   time beside them (at the 350px minimum it would have zero width), so it
   moves up above Start: still starting just right of the SEC field, and
   allowed to wrap. Start stays at the right. */
@media (max-width: 459px) {
  .go {
    flex-direction: column;
    align-items: stretch;
    gap: 4px;
  }

  .hint {
    flex: none;
    overflow: visible;
    white-space: normal;
    font-size: 0.72rem;
    line-height: 1.25;
  }

  .start {
    align-self: flex-end;
  }
}
</style>

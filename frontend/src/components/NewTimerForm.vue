<script setup lang="ts">
import { computed, ref } from "vue";

import { TimerService } from "../../bindings/github.com/andrewwphillips/timers-wails";
import { vDragNumber } from "../directives/dragNumber";
import { formatDuration, toSeconds } from "../lib/format";

const emit = defineEmits<{
  start: [hours: number, minutes: number, seconds: number, label: string];
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

function submit(): void {
  if (!valid.value) {
    return;
  }
  emit("start", value(hours.value), value(minutes.value), value(seconds.value), label.value.trim());
  label.value = "";
}
</script>

<template>
  <form class="form" @submit.prevent="submit">
    <div class="row">
      <label class="field grow">
        <input v-model="label" type="text" maxlength="60" placeholder="Describe what you want to time (optional)." />
      </label>
    </div>

    <div class="row">
      <div class="fields">
        <label class="field time">
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

      <p class="hint" :class="{ muted: !valid }">
        {{ valid ? formatDuration(totalSeconds) : "Set a duration to start a timer" }}
      </p>

      <button type="submit" class="start" :disabled="!valid">Start</button>
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

.field.time {
  flex: none;
  width: 60px;
}

.grow {
  flex: 1;
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
  padding: 8px 10px;
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
</style>

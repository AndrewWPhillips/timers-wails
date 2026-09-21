<script setup lang="ts">
import { computed } from "vue";

import { State } from "../../bindings/github.com/andrewwphillips/timers-wails/internal/timer";
import type { Timer } from "../../bindings/github.com/andrewwphillips/timers-wails/internal/timer";
import { formatDuration, formatRemaining, progress } from "../lib/format";

const props = defineProps<{
  timer: Timer;
  /** Shared clock, so every card on screen ticks from one animation loop. */
  now: number;
}>();

const emit = defineEmits<{
  pause: [id: string];
  resume: [id: string];
  restart: [id: string];
  dismiss: [id: string];
  remove: [id: string];
}>();

const running = computed(() => props.timer.state === State.StateRunning);
const paused = computed(() => props.timer.state === State.StatePaused);
const expired = computed(() => props.timer.state === State.StateExpired);

/**
 * Remaining time, derived from the deadline rather than counted down, so a
 * dropped frame or a throttled background tab can never make it drift.
 *
 * Clamped to the timer's own total as well as to zero. The display clock can lag
 * real time while the window is hidden, and without the upper clamp a render
 * during that window would briefly show more time remaining than the timer was
 * ever set for.
 */
const remainingMs = computed(() => {
  if (!running.value) {
    return props.timer.remainingMs;
  }
  const left = props.timer.endsAtMs - props.now;
  return Math.min(props.timer.totalMs, Math.max(0, left));
});

const display = computed(() => formatRemaining(remainingMs.value));
const done = computed(() => progress(remainingMs.value, props.timer.totalMs));
const total = computed(() => formatDuration(Math.round(props.timer.totalMs / 1000)));
</script>

<template>
  <article
    class="card"
    :class="{ 'is-alarming': timer.alarming, 'is-expired': expired, 'is-paused': paused }"
  >
    <header class="head">
      <h2 class="label">{{ timer.label || total }}</h2>
      <button class="icon" title="Delete timer" aria-label="Delete timer" @click="emit('remove', timer.id)">
        &times;
      </button>
    </header>

    <div class="track" role="progressbar" :aria-valuenow="Math.round(done * 100)" aria-valuemin="0" aria-valuemax="100">
      <div class="fill" :style="{ width: `${done * 100}%` }"></div>
    </div>

    <footer class="actions">
      <p class="time" :class="{ wide: display.length > 5 }">{{ display }}</p>

      <span class="buttons">
        <button @click="emit('restart', timer.id)">Restart</button>
        <button v-if="timer.alarming" class="primary" @click="emit('dismiss', timer.id)">Dismiss</button>
        <button v-if="running" class="primary" @click="emit('pause', timer.id)">Pause</button>
        <button v-if="paused" class="primary" @click="emit('resume', timer.id)">Resume</button>
      </span>
    </footer>
  </article>
</template>

<style scoped>
.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: 12px;
  padding: 14px 16px 12px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.is-paused {
  opacity: 0.75;
}

.is-expired {
  border-color: var(--alarm);
}

/* The flash that announces a finished timer. Held to a slow, two-step pulse:
   fast strobing is both unpleasant and an accessibility hazard. */
.is-alarming {
  animation: flash 1s ease-in-out infinite;
}

@keyframes flash {
  0%,
  100% {
    background: var(--surface);
    border-color: var(--alarm);
  }
  50% {
    background: var(--alarm-soft);
    border-color: var(--alarm-bright);
  }
}

@media (prefers-reduced-motion: reduce) {
  .is-alarming {
    animation: none;
    background: var(--alarm-soft);
    border-color: var(--alarm-bright);
  }
}

.head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
}

.label {
  margin: 0;
  font-size: 0.95rem;
  font-weight: 600;
  color: var(--text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.icon {
  background: none;
  border: none;
  color: var(--muted);
  font-size: 1.3rem;
  line-height: 1;
  padding: 0 4px;
  cursor: pointer;
  border-radius: 6px;
}

.icon:hover {
  color: var(--danger);
  background: var(--surface-raised);
}

.time {
  margin: 0;
  font-variant-numeric: tabular-nums;
  font-size: 1.5rem;
  font-weight: 600;
  line-height: 1;
  color: var(--text);
}

.time.wide {
  font-size: 1.25rem;
}

.is-expired .time {
  color: var(--alarm-bright);
}

.track {
  /* 50% thicker than the original 4px. */
  height: 6px;
  background: var(--surface-raised);
  border-radius: 999px;
  overflow: hidden;
}

.fill {
  height: 100%;
  background: var(--accent);
  border-radius: 999px;
  /* Matches the 100ms clock, so the bar glides rather than stepping. */
  transition: width 100ms linear;
}

.is-expired .fill {
  background: var(--alarm-bright);
}

.actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.buttons {
  display: flex;
  gap: 6px;
}

button:not(.icon) {
  background: var(--surface-raised);
  color: var(--text);
  border: 1px solid transparent;
  border-radius: 7px;
  padding: 5px 11px;
  font-size: 0.8rem;
  cursor: pointer;
}

button:not(.icon):hover {
  border-color: var(--line-bright);
}

button.primary {
  background: var(--accent);
  color: #fff;
}

button.primary:hover {
  background: var(--accent-bright);
}
</style>

<script setup lang="ts">
import { computed, nextTick, ref } from "vue";

import { State } from "../../bindings/github.com/andrewwphillips/timers-wails/internal/timer";
import type { Timer } from "../../bindings/github.com/andrewwphillips/timers-wails/internal/timer";
import { textColorOn } from "../lib/colors";
import { formatRemaining, progress } from "../lib/format";

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
  rename: [id: string, label: string];
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
 *
 * An expired timer that is still alarming keeps counting past its deadline, so
 * the display goes negative and shows how long ago it finished. Dismissing
 * freezes that overrun into remainingMs, which the backend stores.
 */
const remainingMs = computed(() => {
  if (expired.value && props.timer.alarming) {
    return Math.min(0, props.timer.endsAtMs - props.now);
  }
  if (!running.value) {
    return props.timer.remainingMs;
  }
  const left = props.timer.endsAtMs - props.now;
  return Math.min(props.timer.totalMs, Math.max(0, left));
});

const display = computed(() => formatRemaining(remainingMs.value));
/** The timer's full length, shown after the remaining time ("3:00 / 5:00"). */
const totalDisplay = computed(() => formatRemaining(props.timer.totalMs));
const done = computed(() => progress(remainingMs.value, props.timer.totalMs));

/** The preset colour this timer was started with, if any -- exposed as CSS
 *  custom properties so the progress bar and the Pause/Resume/Dismiss buttons
 *  can all read it, along with a dark or white text colour that stays
 *  readable on it (the palette mixes light and deep colours). With no colour
 *  (a timer started without a preset, or one created before this field
 *  existed) the CSS falls back to the default accent with white text. */
const colorStyle = computed(() => {
  const fg = textColorOn(props.timer.color);
  return fg ? { "--timer-color": props.timer.color, "--timer-fg": fg } : {};
});

/** In-place renaming: clicking the name swaps it for a text box. */
const editing = ref(false);
const draft = ref("");
const labelInput = ref<HTMLInputElement | null>(null);

async function startEdit(): Promise<void> {
  draft.value = props.timer.label;
  editing.value = true;
  await nextTick();
  labelInput.value?.focus();
  labelInput.value?.select();
}

/** Enter or clicking away saves. The guard matters: removing the text box
 *  after Enter can fire its blur too, which would otherwise save twice. */
function commit(): void {
  if (!editing.value) {
    return;
  }
  editing.value = false;
  const label = draft.value.trim();
  if (label !== props.timer.label) {
    emit("rename", props.timer.id, label);
  }
}

function cancel(): void {
  editing.value = false;
}
</script>

<template>
  <article
    class="card"
    :class="{ 'is-alarming': timer.alarming, 'is-expired': expired, 'is-paused': paused }"
    :style="colorStyle"
  >
    <header class="head">
      <h2 class="label">
        <!-- maxlength matches the new-timer text box; the placeholder matches
             what a blank name will display. -->
        <input
          v-if="editing"
          ref="labelInput"
          v-model="draft"
          class="label-input"
          type="text"
          maxlength="60"
          placeholder="Untitled"
          aria-label="Timer name"
          @keydown.enter.prevent="commit"
          @keydown.esc.prevent="cancel"
          @blur="commit"
        />
        <!-- Fills the whole heading, so clicking anywhere on it renames. A
             blank name shows a faded "Untitled" rather than nothing, which
             would leave nothing visible to click to rename it again. -->
        <button
          v-else
          class="label-btn"
          :class="{ untitled: !timer.label }"
          title="Click to rename"
          @click="startEdit"
        >
          {{ timer.label || "Untitled" }}
        </button>
      </h2>
      <button class="icon" title="Delete timer" aria-label="Delete timer" @click="emit('remove', timer.id)">
        &times;
      </button>
    </header>

    <div class="track" role="progressbar" :aria-valuenow="Math.round(done * 100)" aria-valuemin="0" aria-valuemax="100">
      <div class="fill" :style="{ width: `${done * 100}%` }"></div>
    </div>

    <footer class="actions">
      <!-- The total is never shorter than the remaining time, so its length
           decides whether hours are showing and the text needs shrinking. -->
      <p class="time" :class="{ wide: totalDisplay.length > 5 }">
        {{ display }}<span class="total"> / {{ totalDisplay }}</span>
      </p>

      <span class="buttons">
        <button @click="emit('restart', timer.id)">Restart</button>
        <button v-if="timer.alarming" class="primary" @click="emit('dismiss', timer.id)">Dismiss</button>
        <button v-if="running" class="primary pause" @click="emit('pause', timer.id)">Pause</button>
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
  flex: 1;
  min-width: 0;
  margin: 0;
  font-size: 0.95rem;
  font-weight: 600;
  color: var(--text);
}

/* The name, as a plain-looking button so it can be clicked (or tabbed to)
   to rename. */
.label-btn {
  display: block;
  /* Full width of the heading, so a click anywhere on it renames. */
  width: 100%;
  background: none;
  border: none;
  padding: 0;
  font: inherit;
  color: inherit;
  text-align: left;
  cursor: text;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.label-btn.untitled {
  color: var(--muted);
  font-weight: 400;
  font-style: italic;
}

.label-btn:hover,
.label-btn:focus-visible {
  outline: none;
  text-decoration: underline dotted;
  text-underline-offset: 3px;
}

.label-input {
  width: 100%;
  box-sizing: border-box;
  font: inherit;
  color: var(--text);
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: 6px;
  padding: 1px 6px;
}

.label-input:focus {
  outline: none;
  border-color: var(--accent);
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
  /* Keeps "remaining / total" on one line beside the buttons. */
  white-space: nowrap;
}

.time.wide {
  font-size: 1.25rem;
}

/* The total after the remaining time: dimmer and a little smaller, so the
   live countdown stays the thing that stands out. */
.total {
  color: var(--muted);
  font-size: 0.7em;
  font-weight: 500;
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
  background: var(--timer-color, var(--accent));
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

.buttons button:not(.icon) {
  background: var(--surface-raised);
  color: var(--text);
  border: 1px solid transparent;
  border-radius: 7px;
  padding: 5px 11px;
  font-size: 0.8rem;
  cursor: pointer;
}

.buttons button:not(.icon):hover {
  border-color: var(--line-bright);
}

/* Pause, Resume and Dismiss take the timer's preset colour, with text chosen
   to stay readable on it; Restart keeps the plain style above. Scoped to
   .buttons like the base rule, so these still win on specificity and source
   order. brightness() gives a generic hover lift that works for any
   user-picked colour, not just the built-in accent. */
.buttons button.primary {
  background: var(--timer-color, var(--accent));
  color: var(--timer-fg, #fff);
}

.buttons button.primary:hover {
  filter: brightness(1.15);
}
</style>

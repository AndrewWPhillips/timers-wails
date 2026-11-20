<script setup lang="ts">
import { computed, ref, watch } from "vue";

import type { Alarm } from "../../bindings/github.com/andrewwphillips/timers-wails/internal/settings";
import type { Prefs } from "../composables/useTimers";
import { vDragNumber } from "../directives/dragNumber";
import { nextColor } from "../lib/colors";
import { formatDuration, splitDuration, toSeconds } from "../lib/format";

const props = defineProps<{
  preferences: Prefs;
  /** The fixed palette a new preset's colour is drawn from, fetched from Go
   *  by the parent (see useTimers.ts) so it is defined in exactly one place. */
  presetColors: string[];
  /** Opens the native file picker and resolves to the chosen path, or
   *  undefined if the user cancelled it or it failed. Owned by the parent
   *  since it is a thin wrapper over the bound Go call. */
  pickAlarmFile: () => Promise<string | undefined>;
  /** Same, for the folder that timers are shared through. */
  pickSyncFolder: () => Promise<string | undefined>;
  /** Opens the settings file's folder in the system file manager. */
  openSettingsFolder: () => Promise<void>;
  /** True while a Test preview is playing, so the Test button offers Stop. */
  testing: boolean;
}>();

const emit = defineEmits<{
  save: [preferences: Prefs];
  close: [];
  // Carries the dialog's own in-progress values, not the last-saved ones, so
  // Test always previews what is currently on screen -- including edits that
  // have not been saved yet.
  test: [alarm: Alarm];
  "stop-test": [];
}>();

/**
 * True while the native "choose a sound" dialog is open.
 *
 * The picker is a separate OS-level window with its own lifecycle -- closing
 * our Settings dialog does not close it. Without this guard, a user can (a)
 * click "Choose sound..." more than once, opening several native pickers at
 * once, and (b) close Settings while one is still open, then confirm it
 * afterwards, updating soundFile on a dialog that already looks gone. Tracking
 * it here lets every close path (X, Cancel, Save, the backdrop) disable itself
 * for as long as a pick is outstanding, so the two dialogs behave like
 * properly nested modals instead of two independent windows.
 */
const pickingSound = ref(false);

/**
 * Picking a file only updates this dialog's own local state, exactly like
 * every other field here (a preset's minutes, the volume slider, ...) --
 * it does not touch the saved settings. It used to save immediately so Test
 * could preview it before the main Save was clicked, but that meant Cancel
 * could not undo a pick the way it undoes every other edit. Test no longer
 * needs that: it passes this exact (possibly unsaved) path straight to Go
 * (see useAlarm.ts / alarm.go), so there is nothing left that requires an
 * early save.
 */
async function onChooseSound(): Promise<void> {
  if (pickingSound.value) {
    // Belt-and-braces: the button that triggers this is disabled while a pick
    // is already in flight, so this should be unreachable.
    return;
  }
  pickingSound.value = true;
  try {
    const path = await props.pickAlarmFile();
    if (path) {
      soundFile.value = path;
    }
  } finally {
    pickingSound.value = false;
  }
}

/** Like onChooseSound, for the sync folder. Shares pickingSound: it guards
 *  against any native picker being open, whichever one it is. */
async function onChooseSyncFolder(): Promise<void> {
  if (pickingSound.value) {
    return;
  }
  pickingSound.value = true;
  try {
    const path = await props.pickSyncFolder();
    if (path) {
      syncFolder.value = path;
    }
  } finally {
    pickingSound.value = false;
  }
}

/**
 * Ignore any attempt to close while a sound pick is outstanding: the native
 * file dialog is a separate window with its own lifecycle, and letting this
 * dialog disappear out from under it is what let a stale, forgotten pick
 * update a dialog that already looked closed.
 */
function requestClose(): void {
  if (!pickingSound.value) {
    emit("close");
  }
}

/** A preset broken into editable fields. */
interface Row {
  /** Stable identity for the v-for key, so a row's DOM (and the swatch being
   *  dragged) moves with it when the list is reordered. Never saved. */
  id: number;
  label: string;
  color: string;
  hours: number;
  minutes: number;
  seconds: number;
}

const rows = ref<Row[]>([]);
let nextRowId = 0;
const startVolume = ref(0.8);
const endVolume = ref(0.8);
const rampSeconds = ref(60);
/** Start and end volume are equal, so the volume is constant and the ramp
 *  duration has no effect. (Start can never exceed end, so >= only ever
 *  matches equal values; both are rounded to 2dp, so the compare is exact.) */
const noRise = computed(() => startVolume.value >= endVolume.value);
const muted = ref(false);
/** Run at login. Like every other field, only applied (by Go) on Save. */
const autostart = ref(false);
const soundFile = ref("");
/** Folder timers are shared through ("" = not shared), and this device's name. */
const syncFolder = ref("");
const syncName = ref("");

/** Slider granularity, and the lowest end volume (matches settings.VolumeStep
 *  in Go, which enforces the same on save). */
const volumeStep = 0.05;
const maxRampSeconds = 600;

const round2 = (v: number) => Math.round(v * 100) / 100;

/**
 * The start volume may equal the end volume but never exceed it. Rather than
 * block a drag at the other slider, the other slider is pushed along:
 * dragging start above end carries end up with it, and dragging end below
 * start carries start down.
 */
function onStartVolume(event: Event): void {
  const v = round2(Number((event.target as HTMLInputElement).value));
  startVolume.value = v;
  if (endVolume.value < v) {
    endVolume.value = Math.max(v, volumeStep);
  }
}

/** The end volume never goes below one step -- an end of zero would just be
 *  a silent alarm, which is what the Silent checkbox is for. The clamped value
 *  is written back to the slider: if it leaves the model unchanged, Vue has
 *  nothing to re-render and the thumb would stay where it was dragged. */
function onEndVolume(event: Event): void {
  const input = event.target as HTMLInputElement;
  const v = Math.max(volumeStep, round2(Number(input.value)));
  endVolume.value = v;
  input.value = String(v);
  if (startVolume.value > v) {
    startVolume.value = v;
  }
}

function rampValue(): number {
  const n = Number(rampSeconds.value);
  return Number.isFinite(n) ? Math.min(maxRampSeconds, Math.max(0, Math.floor(n))) : 0;
}

function alarmValues(): Alarm {
  return {
    soundFile: soundFile.value,
    startVolume: startVolume.value,
    endVolume: endVolume.value,
    rampSeconds: rampValue(),
    muted: muted.value,
  };
}

/** Reload the form whenever the stored preferences change, including after a
 *  save, so the user sees the values as they were actually stored. */
watch(
  () => props.preferences,
  (prefs) => {
    rows.value = prefs.presets.map((preset) => ({
      id: nextRowId++,
      label: preset.label,
      // Guards against a brief invalid value in the native colour input if a
      // legacy/empty colour is shown before a save round-trips through Go's
      // own repair logic.
      color: preset.color || props.presetColors[0] || "",
      ...splitDuration(preset.seconds),
    }));
    startVolume.value = prefs.alarm.startVolume;
    endVolume.value = prefs.alarm.endVolume;
    rampSeconds.value = prefs.alarm.rampSeconds;
    muted.value = prefs.alarm.muted;
    autostart.value = prefs.autostart;
    soundFile.value = prefs.alarm.soundFile;
    syncFolder.value = prefs.sync.folder;
    syncName.value = prefs.sync.deviceName;
  },
  { immediate: true, deep: true },
);

function field(value: number | string): number {
  const n = typeof value === "number" ? value : Number(value);
  return Number.isFinite(n) && n > 0 ? Math.floor(n) : 0;
}

function rowSeconds(row: Row): number {
  return toSeconds(field(row.hours), field(row.minutes), field(row.seconds));
}

function addRow(): void {
  const color = nextColor(
    rows.value.map((r) => r.color),
    props.presetColors,
  );
  rows.value = [...rows.value, { id: nextRowId++, label: "", color, hours: 0, minutes: 10, seconds: 0 }];
}

function removeRow(index: number): void {
  rows.value = rows.value.filter((_, i) => i !== index);
}

/**
 * Drag-to-reorder, using a preset's colour swatch as the handle.
 *
 * The swatch is still a native colour input, so a plain click must keep
 * opening the colour picker. The drag only engages once the pointer has
 * moved past a small threshold (as v-drag-number does); after a real drag
 * the click the browser fires on release is swallowed, so the picker does
 * not pop open at the end of every reorder.
 *
 * The row is moved live while dragging: whenever the pointer crosses the
 * midpoint of another row, the dragged row takes that row's place. Move and
 * up are tracked on window rather than through pointer capture, because the
 * swatch element is itself moved around the DOM as the list reorders, and
 * moving a node can drop its capture.
 */
const DRAG_THRESHOLD = 4;
const rowList = ref<HTMLUListElement | null>(null);
/** The row being dragged, once the drag has engaged (for its highlight). */
const draggingId = ref<number | null>(null);

function onSwatchPointerDown(row: Row, event: PointerEvent): void {
  if (event.button !== 0) {
    return;
  }
  const startY = event.clientY;
  const pointerId = event.pointerId;
  let engaged = false;

  const move = (e: PointerEvent): void => {
    if (e.pointerId !== pointerId) {
      return;
    }
    if (!engaged) {
      if (Math.abs(e.clientY - startY) < DRAG_THRESHOLD) {
        return;
      }
      engaged = true;
      draggingId.value = row.id;
      (event.target as HTMLElement).blur();
    }
    reorderTo(row.id, e.clientY);
  };

  const up = (e: PointerEvent): void => {
    if (e.pointerId !== pointerId) {
      return;
    }
    window.removeEventListener("pointermove", move);
    window.removeEventListener("pointerup", up);
    window.removeEventListener("pointercancel", up);
    if (!engaged) {
      return; // a plain click: let it open the colour picker as usual
    }
    draggingId.value = null;
    // The click (if any) follows straight after pointerup in the same task;
    // block it so the picker doesn't open. The timeout clears the blocker if
    // no click comes (e.g. released off the swatch, or pointercancel).
    const block = (c: MouseEvent): void => {
      c.preventDefault();
      c.stopPropagation();
    };
    window.addEventListener("click", block, { capture: true, once: true });
    setTimeout(() => window.removeEventListener("click", block, { capture: true }), 0);
  };

  window.addEventListener("pointermove", move);
  window.addEventListener("pointerup", up);
  window.addEventListener("pointercancel", up);
}

/** Move the row with this id to the position under clientY: its new index is
 *  the number of other rows whose midpoint is above the pointer. The DOM
 *  read here is always up to date, since Vue re-renders in a microtask,
 *  before the next pointermove arrives. */
function reorderTo(id: number, clientY: number): void {
  const from = rows.value.findIndex((r) => r.id === id);
  const items = rowList.value?.children;
  if (from < 0 || !items || items.length !== rows.value.length) {
    return;
  }
  let to = 0;
  for (let i = 0; i < items.length; i++) {
    if (i === from) {
      continue;
    }
    const rect = items[i].getBoundingClientRect();
    if (rect.top + rect.height / 2 < clientY) {
      to++;
    }
  }
  if (to !== from) {
    const next = rows.value.slice();
    const [moved] = next.splice(from, 1);
    next.splice(to, 0, moved);
    rows.value = next;
  }
}

function save(): void {
  const presets = rows.value
    .map((row) => ({
      // An empty label is filled in by Go from the duration, so the user does
      // not have to name every preset.
      label: row.label.trim(),
      color: row.color,
      seconds: rowSeconds(row),
    }))
    .filter((preset) => preset.seconds > 0);

  emit("save", {
    presets,
    alarm: alarmValues(),
    autostart: autostart.value,
    autostartAvailable: props.preferences.autostartAvailable,
    sync: { ...props.preferences.sync, folder: syncFolder.value, deviceName: syncName.value },
  });
}

function clearSound(): void {
  soundFile.value = "";
}
</script>

<template>
  <div class="backdrop" @click.self="requestClose">
    <section class="dialog" role="dialog" aria-modal="true" aria-label="Settings">
      <header class="head">
        <!-- Clicking the title opens the folder holding the settings file. -->
        <h2>
          <button
            class="title-btn"
            title="Open the settings folder"
            @click="openSettingsFolder()"
          >
            Settings
          </button>
        </h2>
        <button class="icon" aria-label="Close" :disabled="pickingSound" @click="requestClose">&times;</button>
      </header>

      <div class="body">
        <label
          class="check startup"
          :title="preferences.autostartAvailable ? '' : 'Not available in this build'"
        >
          <input v-model="autostart" type="checkbox" :disabled="!preferences.autostartAvailable" />
          <span>Run Timers at startup</span>
        </label>

        <div class="row row-header">
          <h3>Presets</h3>
          <span class="hms-heading" aria-hidden="true">
            <span class="col-heading">Hour</span>
            <span class="col-heading">Min</span>
            <span class="col-heading">Sec</span>
          </span>
          <span></span>
        </div>
        <ul ref="rowList" class="rows" :class="{ reordering: draggingId !== null }">
          <li
            v-for="(row, index) in rows"
            :key="row.id"
            class="row"
            :class="{ dragging: draggingId === row.id }"
          >
            <!-- Click to change the colour; drag to reorder the presets. A
                 native colour input can't hold content, so the reorder icon
                 is overlaid on it and lets every pointer event through. -->
            <span class="swatch-wrap">
              <input
                v-model="row.color"
                class="swatch"
                type="color"
                title="Click to change colour, drag to reorder"
                :aria-label="`Colour for ${row.label || 'preset'}`"
                @pointerdown="onSwatchPointerDown(row, $event)"
              />
              <svg
                class="reorder-icon"
                viewBox="0 0 14 12"
                aria-hidden="true"
              >
                <path d="M7 0.5 10.5 3.5H3.5Z M3.5 8.5H10.5L7 11.5Z" fill="currentColor" />
                <path d="M1 5H13M1 7H13" stroke="currentColor" stroke-width="1.1" />
              </svg>
            </span>
            <input v-model="row.label" class="label" type="text" maxlength="32" placeholder="Label" />
            <span class="hms">
              <input v-model="row.hours" v-drag-number class="num" type="number" min="0" max="99" aria-label="Hours" />
              <input v-model="row.minutes" v-drag-number class="num" type="number" min="0" max="59" aria-label="Minutes" />
              <input v-model="row.seconds" v-drag-number class="num" type="number" min="0" max="59" aria-label="Seconds" />
            </span>
            <button class="icon danger" title="Remove" aria-label="Remove" @click="removeRow(index)">&times;</button>
          </li>
        </ul>
        <button class="add" @click="addRow">+ Add preset</button>
        <p v-if="rows.length === 0" class="note">
          No presets - selecting Save will use the defaults.
        </p>

        <!-- Silent sits beside the heading, above everything it disables, so
             it can't be switched on yet scrolled out of sight below them. -->
        <div class="alarm-head">
          <h3>Alarm</h3>
          <label class="check">
            <input v-model="muted" type="checkbox" />
            <span>Silent &mdash; flash only, no sound</span>
          </label>
        </div>
        <div class="sound">
          <p class="current">
            <template v-if="soundFile">{{ soundFile }}</template>
            <template v-else>Built-in beep</template>
          </p>
          <div class="sound-actions">
            <button :disabled="pickingSound || muted" @click="onChooseSound">
              {{ pickingSound ? "Choosing…" : "Choose sound…" }}
            </button>
            <button v-if="soundFile" :disabled="muted" @click="clearSound">Use beep</button>
            <button v-if="testing" @click="emit('stop-test')">Stop</button>
            <button v-else :disabled="muted" @click="emit('test', alarmValues())">Test</button>
          </div>
        </div>

        <!-- Silent makes every control here meaningless, so the whole block is
             disabled and greyed out, not just the sliders. -->
        <div class="levels" :class="{ disabled: muted }">
          <label for="start-volume">Start volume</label>
          <input
            id="start-volume"
            :value="startVolume"
            type="range"
            min="0"
            max="1"
            :step="volumeStep"
            :disabled="muted"
            @input="onStartVolume"
          />
          <output for="start-volume">{{ Math.round(startVolume * 100) }}%</output>

          <label for="end-volume">End volume</label>
          <input
            id="end-volume"
            :value="endVolume"
            type="range"
            min="0"
            max="1"
            :step="volumeStep"
            :disabled="muted"
            @input="onEndVolume"
          />
          <output for="end-volume">{{ Math.round(endVolume * 100) }}%</output>

          <!-- With equal volumes there is nothing to rise, so the ramp is
               disabled too. Only dimmed here when not muted: when muted the
               whole block is already dimmed, and dimming again would compound. -->
          <label for="ramp-seconds" :class="{ dim: noRise && !muted }">Rise over</label>
          <span class="ramp" :class="{ dim: noRise && !muted }">
            <input
              id="ramp-seconds"
              v-model.number="rampSeconds"
              v-drag-number
              class="ramp-input"
              type="number"
              min="0"
              :max="maxRampSeconds"
              inputmode="numeric"
              :disabled="muted || noRise"
            />
            <span>seconds</span>
          </span>
          <span></span>
        </div>

        <h3>Share timers</h3>
        <div class="sound">
          <p class="current">
            <template v-if="syncFolder">{{ syncFolder }}</template>
            <template v-else>Not shared - timers stay on this device</template>
          </p>
          <div class="sound-actions">
            <button :disabled="pickingSound" @click="onChooseSyncFolder">Choose folder&hellip;</button>
            <button v-if="syncFolder" @click="syncFolder = ''">Stop sharing</button>
          </div>
          <input
            v-if="syncFolder"
            v-model="syncName"
            type="text"
            maxlength="40"
            placeholder="This device's name (e.g. Home PC)"
            aria-label="This device's name"
          />
          <p class="note">
            See timers on all your devices using this shared location.
            You can use cloud storage like Google Drive, DropBox, etc or
            peer-to-peer file synchronisation like SyncThing (recommended).
            For best results also ensure your clocks are aligned.
          </p>
        </div>
      </div>

      <footer class="foot">
        <p class="summary">
          {{ rows.length }} preset<template v-if="rows.length !== 1">s</template>
          <template v-if="rows.length">
            &middot; {{ rows.map((r) => formatDuration(rowSeconds(r))).join(", ") }}
          </template>
        </p>
        <div>
          <button :disabled="pickingSound" @click="requestClose">Cancel</button>
          <button class="primary" :disabled="pickingSound" @click="save">Save</button>
        </div>
      </footer>
    </section>
  </div>
</template>

<style scoped>
.backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
  z-index: 10;
}

.dialog {
  background: var(--bg);
  border: 1px solid var(--line-bright);
  border-radius: 14px;
  width: min(560px, 100%);
  max-height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.head,
.foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 12px 16px;
}

.head {
  border-bottom: 1px solid var(--line);
}

.foot {
  border-top: 1px solid var(--line);
}

.head h2 {
  margin: 0;
  font-size: 1rem;
}

/* The title, as a plain-looking button: clicking it opens the settings
   folder. Overrides the dialog's general button styling. */
.title-btn {
  background: none;
  border: none;
  padding: 0;
  font: inherit;
  color: inherit;
  cursor: pointer;
}

.title-btn:hover,
.title-btn:focus-visible {
  border: none;
  outline: none;
  text-decoration: underline dotted;
  text-underline-offset: 3px;
}

.body {
  padding: 4px 16px 16px;
  overflow-y: auto;
}

h3 {
  font-size: 0.74rem;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--muted);
  margin: 18px 0 8px;
}

.row-header h3 {
  /* Spans the swatch and label columns, so the heading starts flush left
     rather than indented by the swatch. */
  grid-column: 1 / 3;
  /* Same implicit min-width: auto issue as .label -- without this, "Presets"
     forces the 1fr column wider than the data rows' label input can match,
     shifting every column after it out of alignment at narrow widths. */
  min-width: 0;
}

.rows {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.row-header {
  margin-bottom: 2px;
}

/* Grid, not flex: the header row and every preset row need identical column
   widths so the group headings line up exactly with the inputs below them.
   Every column is a fixed width, not auto -- the header row's cells are
   either empty or hold short static text, while the data rows' hold real
   inputs/buttons; with auto columns that mismatch would make the two rows
   compute a different width for the flexible label column and throw the
   alignment off. Hour/min/sec are grouped into one column (a tight inner
   flex row, see .hms below) so they sit close together and leave more of
   the narrow dialog to the label. */
.row {
  display: grid;
  grid-template-columns: 28px 1fr 124px 20px;
  align-items: center;
  gap: 3px;
  border-radius: 8px;
}

/* The row being dragged by its swatch: lifted slightly so it is clear which
   one is moving as the others shuffle past it. */
.row.dragging {
  background: var(--surface-raised);
  box-shadow: 0 0 0 2px var(--accent);
}

/* While reordering, keep the grabbing cursor everywhere (the pointer soon
   leaves the swatch) and stop the drag selecting text in the label inputs. */
.rows.reordering,
.rows.reordering * {
  cursor: grabbing !important;
  user-select: none;
}

.hms-heading,
.hms {
  display: flex;
  align-items: center;
  gap: 2px;
}

.hms-heading {
  /* Anchored to the bottom of the (taller, h3-containing) header row, same
     reason as .col-heading below. */
  align-self: end;
}

.hms-heading .col-heading {
  width: 40px; /* matches .num */
}

.col-heading {
  /* Anchored to the bottom of the (taller, h3-containing) header row, so it
     sits immediately above the input column it labels rather than floating
     in the middle of the row. */
  align-self: end;
  text-align: center;
  font-size: 0.5rem;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--muted);
}

input {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: 7px;
  color: var(--text);
  padding: 6px 6px;
  font-size: 0.85rem;
}

input:focus {
  outline: none;
  border-color: var(--accent);
}

.label {
  /* Grid items get an implicit min-width: auto, which stops them shrinking
     below their content's intrinsic width -- without this override, the
     label input would refuse to shrink and overflow the row at narrow
     window widths, defeating the whole point of the 1fr column. */
  min-width: 0;
}

.num {
  width: 40px;
  /* Tighter than the generic input padding -- at 40px wide, a 2-digit value
     plus the native spinner arrows needs the extra room to avoid clipping. */
  padding: 6px 2px;
  font-variant-numeric: tabular-nums;
  cursor: ns-resize;
}

.num.is-scrubbing {
  border-color: var(--accent);
  background: var(--surface-raised);
}

.swatch {
  width: 28px;
  height: 28px;
  padding: 2px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface);
  /* A normal arrow, not a hand: the grabbing cursor only appears once a drag
     has actually engaged (see .reordering). */
  cursor: default;
  /* Lets a touch drag on the swatch reorder rather than scroll the dialog. */
  touch-action: none;
}

.swatch-wrap {
  position: relative;
  display: block;
  width: 28px;
  height: 28px;
}

.swatch-wrap .swatch {
  display: block;
}

/* Centred over the swatch; pointer-events: none so clicks and drags land on
   the colour input underneath. */
.reorder-icon {
  position: absolute;
  left: 50%;
  top: 50%;
  width: 14px;
  height: 12px;
  transform: translate(-50%, -50%);
  pointer-events: none;
  /* A fixed mid grey whatever the swatch colour -- a subtle hint, so it is
     fine if it is faint on some colours. */
  color: #606060;
}

.swatch::-webkit-color-swatch-wrapper {
  padding: 2px;
}

.swatch::-webkit-color-swatch {
  border: none;
  border-radius: 4px;
}

.icon {
  background: none;
  border: none;
  color: var(--muted);
  cursor: pointer;
  padding: 4px 6px;
  border-radius: 6px;
  font-size: 0.9rem;
}

.icon:hover {
  background: var(--surface-raised);
  color: var(--text);
}

.icon.danger:hover {
  color: var(--danger);
}

.icon.danger {
  width: 20px;
  padding: 4px 0;
}

/* E.g. the Close button while a native picker is open -- grey it out rather
   than leaving a live-looking button that does nothing. */
.icon:disabled {
  color: var(--line-bright);
  cursor: default;
}

.icon:disabled:hover {
  background: none;
  color: var(--line-bright);
}

.add {
  margin-top: 8px;
  background: none;
  border: 1px dashed var(--line-bright);
  color: var(--muted);
  border-radius: 8px;
  padding: 7px 12px;
  font-size: 0.82rem;
  cursor: pointer;
}

.add:hover {
  color: var(--text);
}

.note {
  color: var(--muted);
  font-size: 0.78rem;
  margin: 8px 0 0;
}

.sound {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.current {
  margin: 0;
  font-size: 0.82rem;
  color: var(--text);
  word-break: break-all;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: 8px;
  padding: 8px 10px;
}

.sound-actions {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

/* Label | control | value, as a grid so the three rows' sliders and values
   line up with each other. */
.levels {
  display: grid;
  grid-template-columns: auto 1fr 42px;
  align-items: center;
  gap: 8px 10px;
  margin-top: 14px;
  font-size: 0.82rem;
  color: var(--muted);
}

.levels input[type="range"] {
  width: 100%;
  min-width: 0;
  padding: 0;
}

.levels output {
  font-variant-numeric: tabular-nums;
  color: var(--text);
  text-align: right;
}

.ramp {
  display: flex;
  align-items: center;
  gap: 6px;
}

.ramp-input {
  width: 56px;
  padding: 4px 4px;
  font-variant-numeric: tabular-nums;
  cursor: ns-resize;
}

/* Silent is ticked: none of these have any effect, so grey out the whole
   block, not just the inputs, so the values don't look live either. */
.levels.disabled {
  opacity: 0.4;
}

.levels input:disabled {
  cursor: default;
}

.levels .dim {
  opacity: 0.4;
}

/* Takes over the h3's usual margins so the heading and the Silent checkbox
   share one line (wrapping only if the window is very narrow). */
.alarm-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px 16px;
  margin: 18px 0 8px;
}

.alarm-head h3 {
  margin: 0;
}

.check {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 0.82rem;
  color: var(--muted);
}

.check input {
  padding: 0;
}

/* First thing in the dialog body, above the Presets heading. */
.startup {
  margin-top: 12px;
}

.startup:has(input:disabled) {
  opacity: 0.5;
}

.summary {
  margin: 0;
  font-size: 0.74rem;
  color: var(--muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.foot div {
  display: flex;
  gap: 6px;
}

button {
  background: var(--surface-raised);
  color: var(--text);
  border: 1px solid transparent;
  border-radius: 7px;
  padding: 6px 12px;
  font-size: 0.82rem;
  cursor: pointer;
}

button:hover {
  border-color: var(--line-bright);
}

button:disabled {
  opacity: 0.5;
  cursor: default;
}

button:disabled:hover {
  border-color: transparent;
}

button.primary {
  background: var(--accent);
  color: #fff;
}

button.primary:hover {
  background: var(--accent-bright);
}
</style>

<script setup lang="ts">
import { ref, watch } from "vue";

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
}>();

const emit = defineEmits<{
  save: [preferences: Prefs];
  close: [];
  // Carries the dialog's own in-progress values, not the last-saved ones, so
  // Test always previews what is currently on screen -- including edits that
  // have not been saved yet.
  test: [alarm: Alarm];
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
  label: string;
  color: string;
  hours: number;
  minutes: number;
  seconds: number;
}

const rows = ref<Row[]>([]);
const volume = ref(0.7);
const muted = ref(false);
const soundFile = ref("");

/** Reload the form whenever the stored preferences change, including after a
 *  save, so the user sees the values as they were actually stored. */
watch(
  () => props.preferences,
  (prefs) => {
    rows.value = prefs.presets.map((preset) => ({
      label: preset.label,
      // Guards against a brief invalid value in the native colour input if a
      // legacy/empty colour is shown before a save round-trips through Go's
      // own repair logic.
      color: preset.color || props.presetColors[0] || "",
      ...splitDuration(preset.seconds),
    }));
    volume.value = prefs.alarm.volume;
    muted.value = prefs.alarm.muted;
    soundFile.value = prefs.alarm.soundFile;
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
  rows.value = [...rows.value, { label: "", color, hours: 0, minutes: 10, seconds: 0 }];
}

function removeRow(index: number): void {
  rows.value = rows.value.filter((_, i) => i !== index);
}

function move(index: number, delta: number): void {
  const target = index + delta;
  if (target < 0 || target >= rows.value.length) {
    return;
  }
  const next = rows.value.slice();
  [next[index], next[target]] = [next[target], next[index]];
  rows.value = next;
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
    alarm: { soundFile: soundFile.value, volume: volume.value, muted: muted.value },
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
        <h2>Settings</h2>
        <button class="icon" aria-label="Close" :disabled="pickingSound" @click="requestClose">&times;</button>
      </header>

      <div class="body">
        <div class="row row-header">
          <h3>Presets</h3>
          <span></span>
          <span class="hms-heading" aria-hidden="true">
            <span class="col-heading">Hour</span>
            <span class="col-heading">Min</span>
            <span class="col-heading">Sec</span>
          </span>
          <span class="col-heading" aria-hidden="true">Order</span>
          <span></span>
        </div>
        <ul class="rows">
          <li v-for="(row, index) in rows" :key="index" class="row">
            <input v-model="row.label" class="label" type="text" maxlength="32" placeholder="Label" />
            <input
              v-model="row.color"
              class="swatch"
              type="color"
              title="Preset colour"
              :aria-label="`Colour for ${row.label || 'preset'}`"
            />
            <span class="hms">
              <input v-model="row.hours" v-drag-number class="num" type="number" min="0" max="99" aria-label="Hours" />
              <input v-model="row.minutes" v-drag-number class="num" type="number" min="0" max="59" aria-label="Minutes" />
              <input v-model="row.seconds" v-drag-number class="num" type="number" min="0" max="59" aria-label="Seconds" />
            </span>
            <span class="arrows">
              <button
                class="icon arrow"
                title="Move up"
                aria-label="Move up"
                :disabled="index === 0"
                @click="move(index, -1)"
              >&uarr;</button>
              <button
                class="icon arrow"
                title="Move down"
                aria-label="Move down"
                :disabled="index === rows.length - 1"
                @click="move(index, 1)"
              >&darr;</button>
            </span>
            <button class="icon danger" title="Remove" aria-label="Remove" @click="removeRow(index)">&times;</button>
          </li>
        </ul>
        <button class="add" @click="addRow">+ Add preset</button>
        <p v-if="rows.length === 0" class="note">
          No saved presets found - using the default presets.
        </p>

        <h3>Alarm</h3>
        <div class="sound">
          <p class="current">
            <template v-if="soundFile">{{ soundFile }}</template>
            <template v-else>Built-in beep</template>
          </p>
          <div class="sound-actions">
            <button :disabled="pickingSound" @click="onChooseSound">
              {{ pickingSound ? "Choosing…" : "Choose sound…" }}
            </button>
            <button v-if="soundFile" @click="clearSound">Use beep</button>
            <button @click="emit('test', { soundFile, volume, muted })">Test</button>
          </div>
        </div>

        <label class="volume">
          <span>Volume</span>
          <input v-model.number="volume" type="range" min="0.05" max="1" step="0.05" />
          <output>{{ Math.round(volume * 100) }}%</output>
        </label>

        <label class="check">
          <input v-model="muted" type="checkbox" />
          <span>Silent &mdash; flash only, no sound</span>
        </label>
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
   alignment off. Hour/min/sec and the two arrow buttons are each grouped
   into one column (a tight inner flex row, see .hms/.arrows below) so they
   sit close together and leave more of the narrow dialog to the label. */
.row {
  display: grid;
  grid-template-columns: 1fr 28px 124px 34px 20px;
  align-items: center;
  gap: 3px;
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

.arrows {
  display: flex;
  align-items: center;
  gap: 2px;
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
  cursor: pointer;
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

.icon.arrow {
  width: 16px;
  text-align: center;
  font-size: 1rem;
  font-weight: 1000;
  padding: 2px 0;
}

/* Move-up on the first row and move-down on the last have nowhere to go --
   grey them out rather than leaving a live-looking button that does nothing. */
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

.volume {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 14px;
  font-size: 0.82rem;
  color: var(--muted);
}

.volume input {
  flex: 1;
  padding: 0;
}

.volume output {
  font-variant-numeric: tabular-nums;
  color: var(--text);
  width: 42px;
  text-align: right;
}

.check {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
  font-size: 0.82rem;
  color: var(--muted);
}

.check input {
  padding: 0;
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

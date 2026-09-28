<script setup lang="ts">
import type { Preset } from "../../bindings/github.com/andrewwphillips/timers-wails/internal/settings";
import { textColorOn } from "../lib/colors";

defineProps<{ presets: Preset[] }>();

/** Paints a preset button in its own colour, with readable text on top. Falls
 *  back to the default button style (via the CSS var() fallbacks) if the
 *  preset has no valid colour. */
function presetStyle(preset: Preset): Record<string, string> {
  const text = textColorOn(preset.color);
  return text ? { "--preset-bg": preset.color, "--preset-fg": text } : {};
}

const emit = defineEmits<{
  start: [seconds: number, label: string, color: string];
  edit: [];
}>();
</script>

<template>
  <div class="bar">
    <button
      v-for="preset in presets"
      :key="`${preset.label}-${preset.seconds}`"
      class="preset"
      :style="presetStyle(preset)"
      @click="emit('start', preset.seconds, preset.label, preset.color)"
    >
      {{ preset.label }}
    </button>

    <button class="edit" title="Edit presets" aria-label="Edit presets" @click="emit('edit')">Edit</button>
  </div>
</template>

<style scoped>
.bar {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.preset {
  background: var(--preset-bg, var(--surface-raised));
  color: var(--preset-fg, var(--text));
  border: 1px solid transparent;
  border-radius: 999px;
  padding: 6px 14px;
  font-size: 0.82rem;
  font-weight: 600;
  cursor: pointer;
}

.preset:hover {
  filter: brightness(1.15);
}

.edit {
  background: none;
  border: 1px dashed var(--line-bright);
  color: var(--muted);
  border-radius: 999px;
  padding: 6px 12px;
  font-size: 0.82rem;
  cursor: pointer;
  margin-left: auto;
}

.edit:hover {
  color: var(--text);
  border-color: var(--muted);
}
</style>

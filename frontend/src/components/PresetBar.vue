<script setup lang="ts">
import type { Preset } from "../../bindings/github.com/andrewwphillips/timers-wails/internal/settings";

defineProps<{ presets: Preset[] }>();

const emit = defineEmits<{
  start: [seconds: number, label: string];
  edit: [];
}>();
</script>

<template>
  <div class="bar">
    <button
      v-for="preset in presets"
      :key="`${preset.label}-${preset.seconds}`"
      class="preset"
      @click="emit('start', preset.seconds, preset.label)"
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
  background: var(--surface-raised);
  color: var(--text);
  border: 1px solid var(--line);
  border-radius: 999px;
  padding: 6px 14px;
  font-size: 0.82rem;
  cursor: pointer;
}

.preset:hover {
  border-color: var(--accent);
  color: var(--accent-bright);
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

<template>
  <div class="workspace-controls">
    <label class="sr-only" for="saved-workspace-select">Saved workspace</label>
    <select
      id="saved-workspace-select"
      class="workspace-select"
      :value="workspace?.id || ''"
      aria-label="Saved workspace"
      @change="selectSavedWorkspace"
    >
      <option v-for="item in workspaces" :key="item.id" :value="item.id">
        {{ item.slug }}
      </option>
    </select>
    <button
      type="button"
      class="workspace-dir"
      :title="cwd"
      :aria-label="`Choose workspace folder. Current path: ${cwd}`"
      @click="showDirectoryPicker = true"
    >
      <span>{{ workspace?.slug || directoryName }}</span>
      <small>{{ cwd }}</small>
    </button>
    <button type="button" class="workspace-refresh" aria-label="Refresh workspace" @click="emit('refresh')">
      ↻
    </button>

    <DirectoryPickerModal
      :is-open="showDirectoryPicker"
      :initial-path="cwd"
      @close="showDirectoryPicker = false"
      @select="selectDirectory"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import type { Workspace } from "../../services/api";
import DirectoryPickerModal from "./DirectoryPickerModal.vue";

const props = defineProps<{
  cwd: string;
  workspace?: Workspace | null;
  workspaces?: Workspace[];
}>();
const emit = defineEmits<{
  refresh: [];
  "change-directory": [path: string];
  "select-workspace": [id: string];
}>();

const showDirectoryPicker = ref(false);
const directoryName = computed(() => props.cwd.split("/").filter(Boolean).pop() || props.cwd || "Home");

function selectDirectory(path: string) {
  showDirectoryPicker.value = false;
  if (path && path !== props.cwd) emit("change-directory", path);
}

function selectSavedWorkspace(event: Event) {
  const id = (event.target as HTMLSelectElement).value;
  if (id && id !== props.workspace?.id) emit("select-workspace", id);
}
</script>

<style scoped>
.workspace-controls {
  display: flex;
  align-items: center;
  flex: 0 0 auto;
  min-width: 0;
  border-bottom: 1px solid var(--border);
  background: var(--bg-secondary);
}

.workspace-select {
  width: min(16rem, 28%);
  padding: 0.55rem 0.65rem;
  border: 0;
  border-right: 1px solid var(--border);
  background: var(--bg-secondary);
  color: var(--text-primary);
}

.workspace-dir {
  min-width: 0;
  flex: 1;
  padding: 0.55rem 0.75rem;
  overflow: hidden;
  border: 0;
  background: transparent;
  color: var(--text-primary);
  font-weight: 650;
  text-align: left;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.workspace-dir span,
.workspace-dir small {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
}

.workspace-dir small {
  margin-top: 0.15rem;
  color: var(--text-tertiary);
  font: 0.68rem ui-monospace, SFMono-Regular, Menlo, monospace;
  font-weight: 400;
}

.workspace-refresh {
  padding: 0.55rem 0.75rem;
  border: 0;
  background: transparent;
  color: var(--text-secondary);
  font-size: 1.1rem;
}

@media (max-width: 600px) {
  .workspace-select {
    width: 38%;
  }
}
</style>

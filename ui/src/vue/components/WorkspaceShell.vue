<template>
  <div class="workspace-shell">
    <WorkspaceControls
      :cwd="cwd"
      :workspace="workspace"
      :workspaces="workspaces"
      @change-directory="emit('change-directory', $event)"
      @select-workspace="emit('select-workspace', $event)"
      @refresh="refreshNonce++"
    />
    <nav class="workspace-tabs" role="tablist" aria-label="Workspace view">
      <button
        v-for="pane in panes"
        :key="pane"
        type="button"
        role="tab"
        :aria-selected="activePane === pane"
        :class="{ active: activePane === pane }"
        @click="activePane = pane"
      >
        {{ paneLabels[pane] }}
      </button>
    </nav>

    <section v-show="activePane === 'chat'" class="workspace-view workspace-chat-pane" aria-label="Shelley conversation">
      <slot />
    </section>
    <section v-if="activePane === 'files'" class="workspace-view workspace-files-pane" aria-label="Workspace files">
      <WorkspaceNavigator
        class="workspace-pane"
        :cwd="cwd"
        :refresh-nonce="refreshNonce"
        @open-file="openFile"
        @open-diff="emit('open-diff')"
      />
    </section>
    <section v-show="activePane === 'workbench'" class="workspace-view workspace-workbench-pane" aria-label="Workbench">
      <slot name="workbench" :open-request="effectiveOpenRequest" />
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import WorkspaceControls from "./WorkspaceControls.vue";
import WorkspaceNavigator from "./WorkspaceNavigator.vue";
import type { Workspace } from "../../services/api";

type WorkspacePane = "chat" | "files" | "workbench";

const paneLabels: Record<WorkspacePane, string> = {
  chat: "Chat",
  files: "Files",
  workbench: "Workbench",
};

const props = defineProps<{
  cwd: string;
  workspace?: Workspace | null;
  workspaces?: Workspace[];
  openRequest?: { path: string; nonce: number } | null;
  activePane?: WorkspacePane;
}>();
const emit = defineEmits<{
  "change-directory": [path: string];
  "select-workspace": [id: string];
  "open-diff": [];
  "update:activePane": [pane: WorkspacePane];
}>();

const panes = ["chat", "files", "workbench"] as const;
const activePane = computed({
  get: () => props.activePane || "chat",
  set: (pane: WorkspacePane) => emit("update:activePane", pane),
});
const refreshNonce = ref(0);
const effectiveOpenRequest = ref<{ path: string; nonce: number } | null>(null);

function openFile(path: string) {
  effectiveOpenRequest.value = { path, nonce: Date.now() };
  activePane.value = "workbench";
}

watch(
  () => props.openRequest,
  (request) => {
    if (!request) return;
    effectiveOpenRequest.value = request;
    activePane.value = "workbench";
  },
);
</script>

<style scoped>
.workspace-shell {
  min-width: 0;
  min-height: 0;
  height: 100%;
  flex: 1;
  display: flex;
  flex-direction: column;
  background: var(--bg-primary);
}

.workspace-view,
.workspace-pane {
  min-width: 0;
  min-height: 0;
}

.workspace-view {
  flex: 1;
}

.workspace-chat-pane {
  display: flex;
  flex-direction: column;
}

.workspace-files-pane,
.workspace-workbench-pane {
  overflow: hidden;
}

.workspace-tabs {
  display: flex;
  flex: 0 0 auto;
  border-bottom: 1px solid var(--border);
  background: var(--bg-secondary);
}

.workspace-tabs button {
  min-width: 6rem;
  padding: 0.55rem 0.9rem;
  border: 0;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--text-secondary);
}

.workspace-tabs button.active {
  border-color: var(--accent, #3b82f6);
  color: var(--text-primary);
}
</style>

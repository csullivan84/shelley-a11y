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
    <section v-show="activePane === 'files'" class="workspace-view workspace-files-pane" aria-label="Workspace files">
      <WorkspaceNavigator
        class="workspace-pane"
        :cwd="cwd"
        :refresh-nonce="refreshNonce"
        @open-file="openFile"
        @open-diff="emit('open-diff')"
      />
    </section>
    <section v-show="activePane === 'workbench'" class="workspace-view workspace-workbench-pane" aria-label="Workbench">
      <slot name="workbench" :open-request="effectiveOpenRequest" :active="activePane === 'workbench'" />
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
  position: relative;
  min-width: 0;
  min-height: 0;
  height: 100%;
  flex: 1;
  display: flex;
  flex-direction: column;
  background-color: var(--bg-base);
  background-image:
    radial-gradient(circle at 12% 18%, var(--retro-amber-glow), transparent 34%),
    radial-gradient(circle at 88% 76%, var(--retro-green-glow), transparent 38%),
    linear-gradient(var(--retro-grid-line) 1px, transparent 1px),
    linear-gradient(90deg, var(--retro-grid-line) 1px, transparent 1px),
    repeating-linear-gradient(
      0deg,
      var(--retro-scanline) 0,
      var(--retro-scanline) 1px,
      transparent 1px,
      transparent 4px
    );
  background-size: auto, auto, 32px 32px, 32px 32px, auto;
}

.workspace-shell::after {
  content: "";
  position: absolute;
  z-index: 0;
  top: 9.75rem;
  right: 1.25rem;
  width: 5rem;
  height: 3px;
  border-radius: 999px;
  background: linear-gradient(
    90deg,
    #5aa44a 0 20%,
    #f1c232 20% 40%,
    #e69138 40% 60%,
    #cc4125 60% 80%,
    #674ea7 80% 100%
  );
  box-shadow: 0 0 10px var(--retro-amber-glow);
  opacity: 0.75;
  pointer-events: none;
}

.workspace-view,
.workspace-pane {
  min-width: 0;
  min-height: 0;
}

.workspace-view {
  flex: 1;
  position: relative;
  z-index: 1;
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
  position: relative;
  z-index: 3;
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

@media (max-width: 600px) {
  .workspace-shell::after {
    right: 0.75rem;
    width: 3rem;
  }
}

@media (forced-colors: active) {
  .workspace-shell {
    background-image: none;
  }

  .workspace-shell::after {
    display: none;
  }
}
</style>

<template>
  <div class="workspace-shell">
    <nav class="workspace-mobile-tabs" aria-label="Workspace pane">
      <button
        v-for="pane in panes"
        :key="pane"
        type="button"
        :class="{ active: mobilePane === pane }"
        :aria-pressed="mobilePane === pane"
        @click="mobilePane = pane"
      >
        {{ pane }}
      </button>
    </nav>
    <WorkspaceNavigator
      class="workspace-pane workspace-files-pane"
      :class="{ 'mobile-active': mobilePane === 'Files' }"
      :cwd="cwd"
      :workspace="workspace"
      :workspaces="workspaces"
      :refresh-nonce="refreshNonce"
      @open-file="openFile"
      @change-directory="emit('change-directory', $event)"
      @select-workspace="emit('select-workspace', $event)"
      @open-diff="emit('open-diff')"
    />
    <WorkspaceEditor
      class="workspace-pane workspace-code-pane"
      :class="{ 'mobile-active': mobilePane === 'Editor' }"
      :cwd="cwd"
      :open-request="effectiveOpenRequest"
      @comment="sendComment"
      @saved="refreshNonce++"
    />
    <section
      class="workspace-pane workspace-chat-pane"
      :class="{ 'mobile-active': mobilePane === 'Shelley' }"
      aria-label="Shelley conversation"
    >
      <slot />
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from "vue";
import WorkspaceEditor from "./WorkspaceEditor.vue";
import WorkspaceNavigator from "./WorkspaceNavigator.vue";
import type { Workspace } from "../../services/api";

const props = defineProps<{
  cwd: string;
  workspace?: Workspace | null;
  workspaces?: Workspace[];
  openRequest?: { path: string; nonce: number } | null;
}>();
const emit = defineEmits<{
  comment: [text: string];
  "change-directory": [path: string];
  "select-workspace": [id: string];
  "open-diff": [];
}>();

const panes = ["Files", "Editor", "Shelley"] as const;
const mobilePane = ref<(typeof panes)[number]>("Shelley");
const refreshNonce = ref(0);
const effectiveOpenRequest = ref<{ path: string; nonce: number } | null>(null);

function openFile(path: string) {
  effectiveOpenRequest.value = { path, nonce: Date.now() };
  mobilePane.value = "Editor";
}

function sendComment(text: string) {
  mobilePane.value = "Shelley";
  emit("comment", text);
}

watch(
  () => props.openRequest,
  (request) => {
    if (!request) return;
    effectiveOpenRequest.value = request;
    mobilePane.value = "Editor";
  },
);
</script>

<style scoped>
.workspace-shell {
  min-width: 0;
  min-height: 0;
  height: 100%;
  flex: 1;
  display: grid;
  grid-template-columns: minmax(12rem, 16rem) minmax(22rem, 1fr) minmax(25rem, 40%);
  background: var(--bg-primary);
}

.workspace-pane {
  min-width: 0;
  min-height: 0;
}

.workspace-code-pane,
.workspace-chat-pane {
  border-right: 1px solid var(--border);
}

.workspace-chat-pane {
  display: flex;
  flex-direction: column;
  border-right: 0;
}

.workspace-mobile-tabs {
  display: none;
}

@media (max-width: 980px) {
  .workspace-shell {
    display: flex;
    flex-direction: column;
  }

  .workspace-mobile-tabs {
    display: flex;
    flex: 0 0 auto;
    border-bottom: 1px solid var(--border);
    background: var(--bg-secondary);
  }

  .workspace-mobile-tabs button {
    flex: 1;
    padding: 0.55rem;
    border: 0;
    border-bottom: 2px solid transparent;
    background: transparent;
    color: var(--text-secondary);
  }

  .workspace-mobile-tabs button.active {
    border-color: var(--accent, #3b82f6);
    color: var(--text-primary);
  }

  .workspace-pane {
    display: none;
    flex: 1;
  }

  .workspace-pane.mobile-active {
    display: flex;
  }
}
</style>

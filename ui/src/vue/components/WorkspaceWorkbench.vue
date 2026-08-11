<template>
  <div class="workspace-workbench">
    <WorkspaceEditor
      class="workspace-workbench-editor"
      :cwd="cwd"
      :open-request="openRequest"
      @comment="emit('comment', $event)"
      @saved="emit('saved', $event)"
    />
    <div class="workspace-workbench-terminals">
      <TerminalPanel
        :terminals="terminals"
        :conversation-id="conversationId"
        :workspace-id="workspaceId"
        :auto-focus-id="autoFocusId"
        :can-insert-into-input="true"
        @attached="(id, termId) => emit('attached', id, termId)"
        @close="emit('close', $event)"
        @insert-into-input="emit('insert-into-input', $event)"
        @auto-focus-consumed="emit('auto-focus-consumed')"
        @active-terminal-exited="emit('active-terminal-exited')"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import WorkspaceEditor from "./WorkspaceEditor.vue";
import TerminalPanel from "./TerminalPanel.vue";
import type { EphemeralTerminal } from "./terminalTypes";

defineProps<{
  cwd: string;
  openRequest?: { path: string; nonce: number } | null;
  terminals: EphemeralTerminal[];
  conversationId?: string | null;
  workspaceId?: string;
  autoFocusId?: string | null;
}>();
const emit = defineEmits<{
  comment: [text: string];
  saved: [path: string];
  attached: [id: string, termId: string];
  close: [id: string];
  "insert-into-input": [text: string];
  "auto-focus-consumed": [];
  "active-terminal-exited": [];
}>();
</script>

<style scoped>
.workspace-workbench {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  height: 100%;
  background: var(--bg-primary);
}

.workspace-workbench-editor {
  min-height: 0;
  flex: 1;
}

.workspace-workbench-terminals {
  min-height: 0;
  flex: 0 0 auto;
}
</style>

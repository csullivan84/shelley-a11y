<template>
  <section class="workspace-editor" aria-label="File editor">
    <div class="workspace-tabs" role="tablist" aria-label="Open files">
      <div v-for="path in tabs" :key="path" class="workspace-tab-wrap">
        <button
          type="button"
          role="tab"
          class="workspace-tab"
          :class="{ active: path === activePath }"
          :aria-selected="path === activePath"
          :title="path"
          @click="activePath = path"
        >
          {{ basename(path) }}<span v-if="dirtyPaths.has(path)" aria-label="unsaved"> •</span>
        </button>
        <button
          type="button"
          class="workspace-tab-close"
          :aria-label="`Close ${basename(path)}`"
          @click="closeTab(path)"
        >
          ×
        </button>
      </div>
      <span v-if="tabs.length === 0" class="workspace-tabs-empty">No files open</span>
    </div>

    <div class="workspace-editor-toolbar">
      <code class="workspace-editor-path">{{ activePath ? relativePath(activePath) : cwd }}</code>
      <span class="workspace-save-status" role="status" aria-live="polite">
        <template v-if="saveStatus === 'saving'">Saving…</template>
        <template v-else-if="saveStatus === 'saved'">Saved</template>
        <template v-else-if="saveStatus === 'error'">Save failed</template>
      </span>
      <Button
        label="Ask Shelley"
        size="small"
        severity="secondary"
        :disabled="!activePath"
        @click="askShelley"
      />
    </div>

    <div class="workspace-editor-body">
      <div v-if="error" class="workspace-editor-state workspace-editor-error" role="alert">
        {{ error }}
      </div>
      <div v-else-if="loading" class="workspace-editor-state">Loading editor…</div>
      <div v-else-if="!activePath" class="workspace-editor-state workspace-editor-welcome">
        <strong>Open a file from the workspace.</strong>
        <span>Your tabs and editor position persist when Shelley reloads.</span>
      </div>
      <div ref="containerRef" class="workspace-monaco" :class="{ hidden: !activePath || !!error }" />
    </div>
  </section>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, shallowRef, watch } from "vue";
import type * as Monaco from "monaco-editor";
import Button from "primevue/button";
import { api } from "../../services/api";
import { loadMonaco } from "../../services/monaco";
import { isDarkModeActive } from "../../services/theme";

const props = defineProps<{
  cwd: string;
  openRequest?: { path: string; nonce: number } | null;
}>();
const emit = defineEmits<{
  comment: [text: string];
  saved: [path: string];
}>();

type SaveStatus = "idle" | "saving" | "saved" | "error";

const tabs = ref<string[]>([]);
const activePath = ref<string | null>(null);
const containerRef = ref<HTMLDivElement | null>(null);
const editor = shallowRef<Monaco.editor.IStandaloneCodeEditor | null>(null);
const models = new Map<string, Monaco.editor.ITextModel>();
const viewStates = new Map<string, Monaco.editor.ICodeEditorViewState | null>();
const dirtyPaths = ref(new Set<string>());
const loading = ref(false);
const error = ref<string | null>(null);
const saveStatus = ref<SaveStatus>("idle");
let monaco: typeof Monaco | null = null;
let saveTimer: number | null = null;
let statusTimer: number | null = null;
let contentListener: Monaco.IDisposable | null = null;
let switchingModel = false;

function storageKey() {
  return `shelley.workspace.files:${props.cwd}`;
}

function basename(path: string) {
  return path.split("/").filter(Boolean).pop() || path;
}

function relativePath(path: string) {
  const root = props.cwd.replace(/\/+$/, "") + "/";
  return path.startsWith(root) ? path.slice(root.length) : path;
}

function persistTabs() {
  localStorage.setItem(storageKey(), JSON.stringify({ tabs: tabs.value, active: activePath.value }));
}

function restoreTabs() {
  tabs.value = [];
  activePath.value = null;
  if (!props.cwd) return;
  const raw = localStorage.getItem(storageKey());
  if (!raw) return;
  try {
    const stored = JSON.parse(raw) as { tabs?: string[]; active?: string };
    tabs.value = Array.isArray(stored.tabs) ? stored.tabs.filter((path) => typeof path === "string") : [];
    activePath.value =
      (stored.active && tabs.value.includes(stored.active) ? stored.active : tabs.value[0]) || null;
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : "Invalid saved workspace state";
  }
}

async function ensureEditor() {
  if (editor.value || !containerRef.value) return;
  monaco = await loadMonaco();
  editor.value = monaco.editor.create(containerRef.value, {
    theme: isDarkModeActive() ? "vs-dark" : "vs",
    automaticLayout: true,
    minimap: { enabled: false },
    fontSize: 13,
    tabSize: 2,
    insertSpaces: true,
    scrollBeyondLastLine: false,
    wordWrap: "off",
    accessibilitySupport: "auto",
  });
}

async function modelFor(path: string): Promise<Monaco.editor.ITextModel> {
  const existing = models.get(path);
  if (existing) return existing;
  const result = await api.readFile(path);
  if (!monaco) throw new Error("Editor is not loaded");
  const uri = monaco.Uri.file(path);
  const alreadyCreated = monaco.editor.getModel(uri);
  const model = alreadyCreated ?? monaco.editor.createModel(result.content, undefined, uri);
  models.set(path, model);
  return model;
}

async function showActiveFile() {
  const path = activePath.value;
  if (!path) {
    editor.value?.setModel(null);
    persistTabs();
    return;
  }
  loading.value = true;
  error.value = null;
  try {
    await nextTick();
    await ensureEditor();
    if (!editor.value || activePath.value !== path) return;
    const previous = editor.value.getModel()?.uri.fsPath;
    if (previous) viewStates.set(previous, editor.value.saveViewState());
    const model = await modelFor(path);
    if (activePath.value !== path) return;
    switchingModel = true;
    editor.value.setModel(model);
    const state = viewStates.get(path);
    if (state) editor.value.restoreViewState(state);
    editor.value.focus();
    switchingModel = false;
    contentListener?.dispose();
    contentListener = model.onDidChangeContent(() => {
      if (switchingModel) return;
      dirtyPaths.value = new Set(dirtyPaths.value).add(path);
      scheduleSave(path);
    });
    persistTabs();
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : `Failed to open ${path}`;
  } finally {
    if (activePath.value === path) loading.value = false;
  }
}

function openFile(path: string) {
  if (!path) return;
  if (!tabs.value.includes(path)) tabs.value = [...tabs.value, path];
  activePath.value = path;
}

function scheduleSave(path: string) {
  if (saveTimer) window.clearTimeout(saveTimer);
  saveTimer = window.setTimeout(() => void save(path), 500);
}

async function save(path: string) {
  if (saveTimer) {
    window.clearTimeout(saveTimer);
    saveTimer = null;
  }
  const model = models.get(path);
  if (!model || !dirtyPaths.value.has(path)) return;
  saveStatus.value = "saving";
  try {
    await api.writeFile(path, model.getValue());
    const nextDirty = new Set(dirtyPaths.value);
    nextDirty.delete(path);
    dirtyPaths.value = nextDirty;
    saveStatus.value = "saved";
    emit("saved", path);
    if (statusTimer) window.clearTimeout(statusTimer);
    statusTimer = window.setTimeout(() => (saveStatus.value = "idle"), 1800);
  } catch (cause) {
    saveStatus.value = "error";
    error.value = cause instanceof Error ? cause.message : `Failed to save ${path}`;
  }
}

async function closeTab(path: string) {
  await save(path);
  const index = tabs.value.indexOf(path);
  tabs.value = tabs.value.filter((tab) => tab !== path);
  if (activePath.value === path) {
    activePath.value = tabs.value[Math.min(index, tabs.value.length - 1)] || null;
  }
  viewStates.delete(path);
  models.get(path)?.dispose();
  models.delete(path);
  persistTabs();
}

function askShelley() {
  if (!activePath.value || !editor.value) return;
  const model = editor.value.getModel();
  const selection = editor.value.getSelection();
  if (!model || !selection) return;
  const selected = model.getValueInRange(selection).trim();
  const start = selection.startLineNumber;
  const end = selection.endLineNumber;
  const reference = `${relativePath(activePath.value)}#L${start}${end === start ? "" : `-L${end}`}`;
  const body = selected ? `\n\n\`\`\`\n${selected}\n\`\`\`` : "";
  emit("comment", `Please look at ${reference}.${body}`);
}

watch(
  () => props.cwd,
  () => {
    restoreTabs();
    void showActiveFile();
  },
  { immediate: true },
);
watch(activePath, () => void showActiveFile());
watch(
  () => props.openRequest,
  (request) => {
    if (request?.path) openFile(request.path);
  },
  { deep: false },
);

onBeforeUnmount(() => {
  if (saveTimer) window.clearTimeout(saveTimer);
  if (statusTimer) window.clearTimeout(statusTimer);
  const active = activePath.value;
  if (active) void save(active);
  contentListener?.dispose();
  editor.value?.dispose();
  for (const model of models.values()) model.dispose();
  models.clear();
});
</script>

<style scoped>
.workspace-editor {
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  background: var(--bg-primary);
}

.workspace-tabs {
  min-height: 2.25rem;
  display: flex;
  overflow-x: auto;
  border-bottom: 1px solid var(--border);
  background: var(--bg-secondary);
}

.workspace-tab-wrap {
  display: flex;
  flex: 0 0 auto;
  border-right: 1px solid var(--border);
}

.workspace-tab,
.workspace-tab-close {
  border: 0;
  background: transparent;
  color: var(--text-secondary);
}

.workspace-tab {
  padding: 0.55rem 0.35rem 0.55rem 0.75rem;
}

.workspace-tab.active {
  background: var(--bg-primary);
  color: var(--text-primary);
}

.workspace-tab-close {
  padding: 0.45rem 0.55rem 0.45rem 0.25rem;
  cursor: pointer;
}

.workspace-tabs-empty {
  padding: 0.55rem 0.75rem;
  color: var(--text-tertiary);
  font-size: 0.8rem;
}

.workspace-editor-toolbar {
  min-height: 2.65rem;
  display: flex;
  align-items: center;
  gap: 0.65rem;
  padding: 0.35rem 0.65rem;
  border-bottom: 1px solid var(--border);
}

.workspace-editor-path {
  min-width: 0;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-secondary);
}

.workspace-save-status {
  min-width: 4.5rem;
  color: var(--text-tertiary);
  font-size: 0.8rem;
  text-align: right;
}

.workspace-editor-body {
  min-height: 0;
  flex: 1;
  position: relative;
}

.workspace-monaco {
  position: absolute;
  inset: 0;
}

.workspace-monaco.hidden {
  visibility: hidden;
}

.workspace-editor-state {
  position: absolute;
  inset: 0;
  z-index: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  gap: 0.5rem;
  padding: 2rem;
  color: var(--text-secondary);
  text-align: center;
}

.workspace-editor-error {
  color: var(--error-text);
}
</style>

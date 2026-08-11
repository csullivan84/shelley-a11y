<template>
  <aside class="workspace-navigator" aria-label="Workspace navigation">
    <div class="workspace-nav-header">
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
      <button type="button" class="workspace-refresh" aria-label="Refresh workspace" @click="refresh">
        ↻
      </button>
    </div>

    <div class="workspace-nav-tabs" role="tablist" aria-label="Workspace navigation view">
      <button
        type="button"
        role="tab"
        :aria-selected="view === 'files'"
        :class="{ active: view === 'files' }"
        @click="view = 'files'"
      >
        Files
      </button>
      <button
        type="button"
        role="tab"
        :aria-selected="view === 'history'"
        :class="{ active: view === 'history' }"
        @click="view = 'history'"
      >
        History
      </button>
    </div>

    <template v-if="view === 'files'">
      <label class="sr-only" for="workspace-file-filter">Filter workspace files</label>
      <input
        id="workspace-file-filter"
        v-model="query"
        class="workspace-file-filter"
        type="search"
        placeholder="Filter files…"
        spellcheck="false"
      />
      <div v-if="filesError" class="workspace-nav-state error" role="alert">{{ filesError }}</div>
      <div v-else-if="filesLoading" class="workspace-nav-state">Loading files…</div>
      <ul v-else class="workspace-file-list" aria-label="Files">
        <li v-for="file in files" :key="file.path">
          <button
            type="button"
            class="workspace-file-row"
            :title="file.path"
            @click="emit('open-file', absolutePath(file.path))"
          >
            <span class="workspace-file-name">{{ file.path }}</span>
            <span
              v-if="gitStatus[file.path]"
              class="workspace-file-status"
              :aria-label="gitStatus[file.path]"
            >
              {{ statusLetter(gitStatus[file.path]) }}
            </span>
          </button>
        </li>
        <li v-if="files.length === 0" class="workspace-nav-state">No matching files.</li>
      </ul>
      <p v-if="filesTruncated" class="workspace-nav-note">Showing the first 500 matches.</p>
    </template>

    <template v-else>
      <div v-if="historyError" class="workspace-nav-state error" role="alert">
        {{ historyError }}
      </div>
      <div v-else-if="historyLoading" class="workspace-nav-state">Loading history…</div>
      <div v-else-if="!gitAvailable" class="workspace-nav-state">This folder is not a Git repository.</div>
      <div v-else class="workspace-history-wrap">
        <table class="workspace-history-table">
          <caption class="sr-only">Git commit index</caption>
          <thead>
            <tr>
              <th scope="col">Commit</th>
              <th scope="col">Message</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="commit in commits"
              :key="commit.hash"
              :class="{ selected: selectedCommit === commit.hash }"
            >
              <td>
                <button type="button" @click="selectCommit(commit.hash)">
                  {{ commit.shortHash }}
                </button>
              </td>
              <td>
                <button type="button" @click="selectCommit(commit.hash)">
                  <span>{{ commit.subject }}</span>
                  <small>{{ commit.author }}</small>
                </button>
              </td>
            </tr>
          </tbody>
        </table>
        <section v-if="commitDetail" class="workspace-commit-detail" aria-label="Commit files">
          <strong>{{ commitDetail.subject }}</strong>
          <button
            v-for="file in commitDetail.files"
            :key="file.path"
            type="button"
            @click="emit('open-file', absolutePath(file.path))"
          >
            <span>{{ file.path }}</span>
            <small>+{{ file.additions }} −{{ file.deletions }}</small>
          </button>
          <Button label="Open full diff" size="small" severity="secondary" @click="emit('open-diff')" />
        </section>
      </div>
    </template>

    <DirectoryPickerModal
      :is-open="showDirectoryPicker"
      :initial-path="cwd"
      @close="showDirectoryPicker = false"
      @select="selectDirectory"
    />
  </aside>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import Button from "primevue/button";
import { api } from "../../services/api";
import type { GitCommitDetail, GitGraphCommit } from "../../types";
import type { Workspace } from "../../services/api";
import DirectoryPickerModal from "./DirectoryPickerModal.vue";

const props = defineProps<{
  cwd: string;
  workspace?: Workspace | null;
  workspaces?: Workspace[];
  refreshNonce?: number;
}>();
const emit = defineEmits<{
  "open-file": [path: string];
  "change-directory": [path: string];
  "select-workspace": [id: string];
  "open-diff": [];
}>();

const view = ref<"files" | "history">("files");
const query = ref("");
const files = ref<Array<{ path: string }>>([]);
const filesLoading = ref(false);
const filesError = ref<string | null>(null);
const filesTruncated = ref(false);
const gitStatus = ref<Record<string, "added" | "modified" | "deleted">>({});
const gitAvailable = ref(true);
const historyLoading = ref(false);
const historyError = ref<string | null>(null);
const commits = ref<GitGraphCommit[]>([]);
const selectedCommit = ref<string | null>(null);
const commitDetail = ref<GitCommitDetail | null>(null);
const showDirectoryPicker = ref(false);
let searchTimer: number | null = null;
let searchController: AbortController | null = null;

const directoryName = computed(() => props.cwd.split("/").filter(Boolean).pop() || props.cwd || "Home");

function absolutePath(relative: string) {
  return `${props.cwd.replace(/\/+$/, "")}/${relative}`;
}

function statusLetter(status: "added" | "modified" | "deleted") {
  return status === "added" ? "A" : status === "deleted" ? "D" : "M";
}

async function loadFiles() {
  if (!props.cwd) return;
  searchController?.abort();
  const controller = new AbortController();
  searchController = controller;
  filesLoading.value = true;
  filesError.value = null;
  try {
    const result = await api.findFiles(props.cwd, query.value.trim(), controller.signal, 500);
    if (controller.signal.aborted) return;
    files.value = result.matches;
    filesTruncated.value = result.truncated;
  } catch (cause) {
    if (controller.signal.aborted) return;
    filesError.value = cause instanceof Error ? cause.message : "Failed to load files";
  } finally {
    if (searchController === controller) filesLoading.value = false;
  }
}

async function loadGitStatus() {
  if (!props.cwd) return;
  try {
    const diffs = await api.getGitDiffs(props.cwd);
    const working = diffs.diffs.find((diff) => diff.id === "working");
    if (!working) throw new Error("Git did not return a working-tree entry");
    const changed = await api.getGitDiffFiles("working", props.cwd);
    gitStatus.value = Object.fromEntries(changed.map((file) => [file.path, file.status]));
    gitAvailable.value = true;
  } catch (cause) {
    const message = cause instanceof Error ? cause.message : String(cause);
    if (message.includes("not a git repository")) {
      gitAvailable.value = false;
      gitStatus.value = {};
      return;
    }
    filesError.value = message;
  }
}

async function loadHistory() {
  if (!props.cwd) return;
  historyLoading.value = true;
  historyError.value = null;
  try {
    const result = await api.getGitGraph(props.cwd, 200, "all");
    commits.value = result.commits;
    gitAvailable.value = true;
  } catch (cause) {
    const message = cause instanceof Error ? cause.message : String(cause);
    if (message.includes("not a git repository")) {
      gitAvailable.value = false;
      commits.value = [];
    } else {
      historyError.value = message;
    }
  } finally {
    historyLoading.value = false;
  }
}

async function selectCommit(hash: string) {
  selectedCommit.value = hash;
  historyError.value = null;
  try {
    commitDetail.value = await api.getGitCommitDetail(props.cwd, hash);
  } catch (cause) {
    historyError.value = cause instanceof Error ? cause.message : "Failed to load commit";
  }
}

function refresh() {
  void loadFiles();
  void loadGitStatus();
  if (view.value === "history") void loadHistory();
}

function selectDirectory(path: string) {
  showDirectoryPicker.value = false;
  if (path && path !== props.cwd) emit("change-directory", path);
}

function selectSavedWorkspace(event: Event) {
  const id = (event.target as HTMLSelectElement).value;
  if (id && id !== props.workspace?.id) emit("select-workspace", id);
}

watch(query, () => {
  if (searchTimer) window.clearTimeout(searchTimer);
  searchTimer = window.setTimeout(() => void loadFiles(), 120);
});
watch(
  [() => props.cwd, () => props.refreshNonce],
  () => {
    query.value = "";
    selectedCommit.value = null;
    commitDetail.value = null;
    refresh();
  },
  { immediate: true },
);
watch(view, (next) => {
  if (next === "history" && commits.value.length === 0) void loadHistory();
});
</script>

<style scoped>
.workspace-navigator {
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--border);
  background: var(--bg-secondary);
  color: var(--text-primary);
}

.workspace-nav-header,
.workspace-nav-tabs {
  display: flex;
  align-items: center;
  border-bottom: 1px solid var(--border);
}

.workspace-nav-header {
  flex-wrap: wrap;
}

.workspace-select {
  width: 100%;
  padding: 0.45rem 0.65rem;
  border: 0;
  border-bottom: 1px solid var(--border);
  background: var(--bg-secondary);
  color: var(--text-primary);
}

.workspace-dir {
  min-width: 0;
  flex: 1;
  padding: 0.65rem 0.75rem;
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
  padding: 0.45rem 0.65rem;
  border: 0;
  background: transparent;
  color: var(--text-secondary);
  font-size: 1.1rem;
}

.workspace-nav-tabs button {
  flex: 1;
  padding: 0.5rem;
  border: 0;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--text-secondary);
}

.workspace-nav-tabs button.active {
  border-color: var(--accent, #3b82f6);
  color: var(--text-primary);
}

.workspace-file-filter {
  margin: 0.55rem;
  padding: 0.5rem;
  border: 1px solid var(--border);
  border-radius: 0.35rem;
  background: var(--bg-primary);
  color: var(--text-primary);
}

.workspace-file-list,
.workspace-history-wrap {
  min-height: 0;
  flex: 1;
  overflow: auto;
}

.workspace-file-list {
  margin: 0;
  padding: 0;
  list-style: none;
}

.workspace-file-row {
  width: 100%;
  display: flex;
  gap: 0.35rem;
  padding: 0.34rem 0.65rem;
  border: 0;
  background: transparent;
  color: var(--text-secondary);
  font: 0.78rem ui-monospace, SFMono-Regular, Menlo, monospace;
  text-align: left;
}

.workspace-file-row:hover,
.workspace-file-row:focus-visible {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.workspace-file-name {
  min-width: 0;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.workspace-file-status {
  color: var(--warning-text);
  font-weight: 700;
}

.workspace-nav-state,
.workspace-nav-note {
  padding: 0.8rem;
  color: var(--text-tertiary);
  font-size: 0.8rem;
}

.workspace-nav-state.error {
  color: var(--error-text);
}

.workspace-history-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.76rem;
}

.workspace-history-table th {
  position: sticky;
  top: 0;
  z-index: 1;
  padding: 0.4rem;
  background: var(--bg-secondary);
  color: var(--text-tertiary);
  text-align: left;
}

.workspace-history-table td {
  border-top: 1px solid var(--border);
  vertical-align: top;
}

.workspace-history-table button,
.workspace-commit-detail button {
  width: 100%;
  display: flex;
  flex-direction: column;
  padding: 0.4rem;
  border: 0;
  background: transparent;
  color: var(--text-secondary);
  text-align: left;
}

.workspace-history-table tr.selected {
  background: var(--bg-hover);
}

.workspace-history-table small,
.workspace-commit-detail small {
  color: var(--text-tertiary);
}

.workspace-commit-detail {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  padding: 0.7rem;
  border-top: 1px solid var(--border);
}
</style>

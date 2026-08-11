<!-- Vue port of src/App.tsx. Owns global state: conversation list,
     current/viewed conversation, drawer, command palette + modals, ephemeral
     terminals, the global SSE stream, URL/slug sync, page title, and global
     keyboard shortcuts. Renders ConversationDrawer, ChatInterface,
     CommandPalette and the modals.

     Worker pool: the React App wrapped everything in @pierre/diffs
     WorkerPoolContextProvider. PatchTool.vue uses the @pierre/diffs SSR
     (synchronous) API and needs no worker pool, so no provider is rendered
     here. -->
<template>
  <div v-if="onboardingLoading" class="loading-container">
    <div class="loading-content">
      <div class="spinner" style="margin: 0 auto 1rem" />
      <p class="text-secondary">Loading Shelley…</p>
    </div>
  </div>

  <div v-else-if="onboardingError" class="error-container" role="alert" aria-live="assertive">
    <div class="error-content">
      <p class="error-message" style="margin-bottom: 1rem">{{ onboardingError }}</p>
      <Button label="Retry" @click="loadOnboarding" />
    </div>
  </div>

  <OnboardingPage
    v-else-if="onboardingStatus && (!onboardingStatus.complete || showProviderSetup)"
    :status="onboardingStatus"
    @complete="handleOnboardingComplete"
  />

  <!-- Loading gate -->
  <div v-else-if="workspaceLoading || (loading && conversations.length === 0)" class="loading-container">
    <div class="loading-content">
      <div class="spinner" style="margin: 0 auto 1rem" />
      <p class="text-secondary">{{ t("loading") }}</p>
    </div>
  </div>

  <div
    v-else-if="error && (conversations.length === 0 || !currentWorkspace)"
    class="error-container"
    role="alert"
    aria-live="assertive"
  >
    <div class="error-content">
      <p class="error-message" style="margin-bottom: 1rem">{{ error }}</p>
      <Button :label="t('retry')" @click="retryStartup" />
    </div>
  </div>

  <template v-else>
    <div v-if="banner" class="top-banner" :title="banner">{{ banner }}</div>
    <div class="app-container">
      <ConversationDrawer
        :is-open="drawerOpen"
        :is-collapsed="drawerCollapsed"
        :conversations="conversations"
        :current-conversation-id="currentConversationId"
        :viewed-conversation="viewedConversation"
        :show-active-trigger="showActiveTrigger"
        @close="drawerOpen = false"
        @toggle-collapse="toggleDrawerCollapsed"
        @select-conversation="selectConversation"
        @new-conversation="startNewConversation"
        @archived="handleConversationArchived"
        @unarchived="handleConversationUnarchived"
        @renamed="handleConversationRenamed"
        @open-herds="navigateHerd(null)"
      />

      <div class="main-content">
        <HerdsPage
          v-if="herdsRouteActive"
          :herd-id="herdsRouteId"
          :conversations="topLevelConversations"
          @back="leaveHerds"
          @navigate-herd="navigateHerd"
          @open-conversation="openConversationFromHerd"
          @terminals-closed="removeClosedTerminals"
        />
        <WorkspaceShell
          v-else
          :cwd="workspaceCwd"
          :workspace="currentWorkspace"
          :workspaces="workspaces"
          :open-request="workspaceOpenRequest"
          @comment="onEditorComment"
          @change-directory="changeWorkspaceDirectory"
          @select-workspace="selectWorkspaceById"
          @open-diff="diffViewerTrigger++"
        >
          <ChatInterface
            :conversation-id="currentConversationId"
            :workspace-id="currentWorkspace?.id"
            :stream-status="streamStatus"
            :reconnect-nonce="reconnectNonce"
            :on-open-drawer="() => (drawerOpen = true)"
            :on-new-conversation="startNewConversation"
            :on-select-conversation="selectConversation"
            :on-archive-conversation="archiveFromChat"
            :current-conversation="currentConversation"
            :on-conversation-update="updateConversation"
            :on-first-message="handleFirstMessage"
            :on-draft-created="onDraftCreated"
            :on-distill-new-generation="handleDistillNewGeneration"
            :most-recent-cwd="workspaceCwd"
            :is-drawer-collapsed="drawerCollapsed"
            :on-toggle-drawer-collapse="toggleDrawerCollapsed"
            :open-diff-viewer-trigger="diffViewerTrigger"
            :open-git-graph-trigger="gitGraphTrigger"
            :open-terminal-trigger="terminalTrigger"
            :models-refresh-trigger="modelsRefreshTrigger"
            :cwd-sync-trigger="cwdSyncTrigger"
            :on-cwd-change="setWorkspaceDirectory"
            :on-open-models-modal="() => (modelsModalOpen = true)"
            :on-open-file-finder="openFileFinder"
            :ephemeral-terminals="conversationTerminals"
            :set-ephemeral-terminals="setEphemeralTerminals"
            :on-terminal-attached="handleTerminalAttached"
            :on-terminal-close="handleTerminalClose"
            :navigate-user-message-trigger="navigateUserMessageTrigger"
            :on-conversation-unarchived="handleConversationUnarchived"
            :external-comment-text="editorCommentText"
          />
        </WorkspaceShell>
      </div>

      <CommandPalette
        :is-open="commandPaletteOpen"
        :conversations="topLevelConversations"
        :current-conversation="currentConversation || null"
        :cwd="workspaceCwd"
        :has-cwd="commandPaletteHasCwd"
        @close="onCommandPaletteClose"
        @new-conversation="
          () => {
            startNewConversation();
            commandPaletteOpen = false;
          }
        "
        @new-conversation-with-cwd="
          (cwd) => {
            startNewConversationWithCwd(cwd);
            commandPaletteOpen = false;
          }
        "
        @set-conversation-cwd="
          (cwd) => {
            setConversationCwd(cwd);
            commandPaletteOpen = false;
          }
        "
        @select-conversation="
          (c) => {
            selectConversation(c);
            commandPaletteOpen = false;
          }
        "
        @archive-conversation="archiveFromPalette"
        @open-diff-viewer="
          () => {
            diffViewerTrigger++;
            commandPaletteOpen = false;
          }
        "
        @open-git-graph="
          () => {
            gitGraphTrigger++;
            commandPaletteOpen = false;
          }
        "
        @open-terminal="
          () => {
            terminalTrigger++;
            commandPaletteOpen = false;
          }
        "
        @open-herds="
          () => {
            navigateHerd(null);
            commandPaletteOpen = false;
          }
        "
        @open-file-finder="openFileFinder"
        @open-models-modal="
          () => {
            modelsModalOpen = true;
            commandPaletteOpen = false;
          }
        "
        @open-notifications-modal="
          () => {
            notificationsModalOpen = true;
            commandPaletteOpen = false;
          }
        "
        @open-feature-flags-modal="
          () => {
            featureFlagsModalOpen = true;
            commandPaletteOpen = false;
          }
        "
        @next-conversation="navigateToNextConversation"
        @previous-conversation="navigateToPreviousConversation"
        @next-user-message="navigateToNextUserMessage"
        @previous-user-message="navigateToPreviousUserMessage"
      />

      <ModelsModal
        :is-open="modelsModalOpen"
        @close="
          () => {
            modelsModalOpen = false;
            focusMessageInputIfUnfocused();
          }
        "
        @models-changed="modelsRefreshTrigger++"
        @open-providers="openProviderSetup"
      />

      <NotificationsModal
        :is-open="notificationsModalOpen"
        @close="
          () => {
            notificationsModalOpen = false;
            focusMessageInputIfUnfocused();
          }
        "
      />

      <FeatureFlagsModal
        :is-open="featureFlagsModalOpen"
        @close="
          () => {
            featureFlagsModalOpen = false;
            focusMessageInputIfUnfocused();
          }
        "
      />

      <FileFinderModal
        :is-open="fileFinderOpen"
        :initial-dir="finderDir"
        @close="fileFinderOpen = false"
        @select="openFileInEditor"
      />

      <div v-if="drawerOpen" class="backdrop hide-on-desktop" @click="drawerOpen = false" />
    </div>

    <!-- Recomputation-counter overlay (performance-hud flag). -->
    <PerfHud v-if="perfHudEnabled" />
  </template>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, provide, ref, watch } from "vue";
import ChatInterface from "./components/ChatInterface.vue";
import ConversationDrawer from "./components/ConversationDrawer.vue";
import CommandPalette from "./components/CommandPalette.vue";
import HerdsPage from "./components/HerdsPage.vue";
import ModelsModal from "./components/ModelsModal.vue";
import NotificationsModal from "./components/NotificationsModal.vue";
import FeatureFlagsModal from "./components/FeatureFlagsModal.vue";
import FileFinderModal from "./components/FileFinderModal.vue";
import OnboardingPage from "./components/OnboardingPage.vue";
import WorkspaceShell from "./components/WorkspaceShell.vue";
import Button from "primevue/button";
import type { EphemeralTerminal } from "./components/terminalTypes";
import { focusMessageInputIfUnfocused } from "../utils/focusMessageInput";
import {
  type Conversation,
  type ConversationWithState,
  type ConversationListPatchEvent,
} from "../types";
import { api, type OnboardingStatus, type Workspace } from "../services/api";
import { messageStore } from "../services/messageStore";
import {
  reduceConversationListPatch,
  type ConversationListState,
} from "../services/conversationListStream";
import { connectGlobalStream, type StreamStatus } from "../services/globalStream";
import { handleNotificationEvent } from "../services/notifications";
import { initializeA11yTrace } from "../services/a11yTrace";
import { initialDrawerCollapsed, saveDrawerCollapsedPreference } from "../utils/drawerStartup";
import { perfCount } from "../utils/perf";
import { useI18n } from "./composables/i18n";
import { ConversationsListKey, CurrentConversationIdKey } from "./composables/subagentLive";
import { WorkspaceContextKey } from "./composables/workspaceContext";
import { useFeatureFlag } from "./composables/featureFlags";
import PerfHud from "./components/PerfHud.vue";

const perfHudEnabled = useFeatureFlag("performance-hud");

const { t } = useI18n();

// ---- URL/slug helpers ----
function isGeneratedId(slug: string | null): boolean {
  if (!slug) return true;
  return /^c[a-z0-9]+$/i.test(slug);
}
function getSlugFromPath(): string | null {
  const path = window.location.pathname;
  if (path.startsWith("/c/")) {
    const slug = path.slice(3);
    if (slug) return slug;
  }
  return null;
}
function isNewPath(): boolean {
  return window.location.pathname === "/new";
}
function isHerdsPath(): boolean {
  return window.location.pathname === "/herds" || window.location.pathname.startsWith("/herds/");
}
function getHerdIdFromPath(): string | null {
  const path = window.location.pathname;
  if (path === "/herds") return null;
  if (path.startsWith("/herds/")) {
    const id = path.slice("/herds/".length).split("/")[0];
    return id || null;
  }
  return null;
}

function getWorkspaceSlugFromPath(): string | null {
  const parts = window.location.pathname.split("/").filter(Boolean);
  if (parts.length !== 1) return null;
  const slug = parts[0];
  if (["new", "herds", "c", "api", "export", "debug", "version"].includes(slug)) return null;
  return slug;
}

// Captured BEFORE render so URL-updating effects don't clobber it.
const initialSlugFromUrl = getSlugFromPath();
const initialWorkspaceSlug = getWorkspaceSlugFromPath();
// The root is the workspace and a fresh Shelley query. Existing conversations
// are opened only through an explicit /c/:slug URL or the conversation drawer.
const initialIsNew = !getSlugFromPath();
const herdsRouteActive = ref(isHerdsPath());
const herdsRouteId = ref<string | null>(getHerdIdFromPath());

function navigateHerd(id: string | null) {
  herdsRouteActive.value = true;
  herdsRouteId.value = id;
  if (id) window.history.pushState({}, "", `/herds/${id}`);
  else window.history.pushState({}, "", "/herds");
  document.title = id ? `Herd - Shelley Agent` : "Herds - Shelley Agent";
}

function leaveHerds() {
  herdsRouteActive.value = false;
  herdsRouteId.value = null;
  window.history.pushState({}, "", currentWorkspace.value ? `/${currentWorkspace.value.slug}` : "/");
  updatePageTitle(currentConversation.value);
}

function openConversationFromHerd(conversationId: string) {
  herdsRouteActive.value = false;
  herdsRouteId.value = null;
  const found = conversations.value.find((c) => c.conversation_id === conversationId);
  if (found) {
    selectConversation(found);
  } else {
    currentConversationId.value = conversationId;
    window.history.pushState({}, "", `/c/${conversationId}`);
  }
}

const closedTerminalIds = new Set<string>();

function removeClosedTerminals(termIds: string[]) {
  for (const id of termIds) closedTerminalIds.add(id);
  const closed = new Set(termIds);
  setEphemeralTerminals((prev) => prev.filter((term) => !term.termId || !closed.has(term.termId)));
}

function updateUrlWithSlug(conversation: Conversation | undefined) {
  const currentSlug = getSlugFromPath();
  let newSlug: string | null = null;
  if (conversation?.slug && !isGeneratedId(conversation.slug)) {
    newSlug = conversation.slug;
  } else if (conversation?.is_draft) {
    newSlug = conversation.conversation_id;
  }
  if (currentSlug !== newSlug) {
    if (newSlug) window.history.replaceState({}, "", `/c/${newSlug}`);
    else if (currentWorkspace.value) {
      window.history.replaceState({}, "", `/${currentWorkspace.value.slug}`);
    } else window.history.replaceState({}, "", "/");
  }
}

function updatePageTitle(conversation: Conversation | undefined) {
  const hostname = window.__SHELLEY_INIT__?.hostname;
  const parts: string[] = [];
  if (conversation?.slug && !isGeneratedId(conversation.slug)) parts.push(conversation.slug);
  if (hostname) parts.push(hostname);
  parts.push("Shelley Agent");
  document.title = parts.join(" - ");
}

const banner = window.__SHELLEY_INIT__?.banner;

// ---- state ----
const conversations = ref<ConversationWithState[]>([]);
const currentConversationId = ref<string | null>(null);
// Subagent tool widgets (SubagentTool.vue) join their slug against the live
// conversation list to show what the subagent is doing right now.
provide(ConversationsListKey, conversations);
provide(CurrentConversationIdKey, currentConversationId);
const viewedConversation = ref<Conversation | null>(null);
const drawerOpen = ref(false);
const drawerCollapsed = ref(false);
const commandPaletteOpen = ref(false);
const diffViewerTrigger = ref(0);
const gitGraphTrigger = ref(0);
const terminalTrigger = ref(0);
const modelsModalOpen = ref(false);
const notificationsModalOpen = ref(false);
const featureFlagsModalOpen = ref(false);
// Fuzzy file finder (Cmd/Ctrl+Shift+P) + the generic editor it opens.
const fileFinderOpen = ref(false);
const workspaceOpenRequest = ref<{ path: string; nonce: number } | null>(null);
// Comment submitted from the file editor's comment mode, to be injected into
// the chat message input. Fresh object per submit so the watcher always fires.
const editorCommentText = ref<{ text: string } | null>(null);
const modelsRefreshTrigger = ref(0);
const cwdSyncTrigger = ref(0);
const navigateUserMessageTrigger = ref(0);
const loading = ref(true);
const error = ref<string | null>(null);
const ephemeralTerminals = ref<EphemeralTerminal[]>([]);
const streamStatus = ref<StreamStatus>("connected");
const reconnectNonce = ref(0);
const showActiveTrigger = ref(0);
const workspaces = ref<Workspace[]>([]);
const currentWorkspace = ref<Workspace | null>(null);
const workspaceLoading = ref(true);
const onboardingStatus = ref<OnboardingStatus | null>(null);
const onboardingLoading = ref(true);
const onboardingError = ref<string | null>(null);
const showProviderSetup = ref(false);

// ---- non-reactive refs ----
let initialSlugResolved = false;
let startupDrawerStateApplied = false;
let conversationListHash: string | null = null;
let globalStreamHandle: { forceReconnect: () => void; close: () => void } | null = null;

// setEphemeralTerminals supports both array and updater-function forms (parity
// with React's setState) so ChatInterface can call it like the React prop.
function setEphemeralTerminals(
  next: EphemeralTerminal[] | ((prev: EphemeralTerminal[]) => EphemeralTerminal[]),
) {
  ephemeralTerminals.value = typeof next === "function" ? next(ephemeralTerminals.value) : next;
}

function handleTerminalAttached(id: string, termId: string) {
  setEphemeralTerminals((prev) => prev.map((tm) => (tm.id === id ? { ...tm, termId } : tm)));
}

function handleTerminalClose(id: string) {
  setEphemeralTerminals((prev) => {
    const tm = prev.find((x) => x.id === id);
    if (tm && tm.termId) {
      fetch(`/api/terminals/${encodeURIComponent(tm.termId)}`, { method: "DELETE" }).catch((err) =>
        console.warn("failed to delete terminal:", err),
      );
    }
    return prev.filter((x) => x.id !== id);
  });
}

// ---- derived ----
const topLevelConversations = computed(() =>
  conversations.value.filter((c) => !c.parent_conversation_id),
);

const currentConversation = computed<ConversationWithState | undefined>(() => {
  const found = conversations.value.find(
    (conv) => conv.conversation_id === currentConversationId.value,
  );
  if (found) return found;
  if (viewedConversation.value?.conversation_id === currentConversationId.value) {
    return {
      ...viewedConversation.value,
      working: false,
      subagent_count: 0,
      max_sequence_id: 0,
    } as ConversationWithState;
  }
  return undefined;
});

// Ordinary terminals belong to the active workspace. Herd membership
// supersedes workspace placement until the terminal is detached from the herd.
const conversationTerminals = computed(() =>
  ephemeralTerminals.value.filter(
    (terminal) =>
      !terminal.herdId && terminal.workspaceId === currentWorkspace.value?.id,
  ),
);

const workspaceCwd = computed(
  () =>
    currentWorkspace.value?.path ||
    window.__SHELLEY_INIT__?.default_cwd ||
    window.__SHELLEY_INIT__?.home_dir ||
    "",
);
const commandPaletteHasCwd = computed(() => workspaceCwd.value !== "");
const finderDir = workspaceCwd;
provide(WorkspaceContextKey, { cwd: workspaceCwd, openFile: openFileInEditor });

// ---- navigation ----
function navigateToNextConversation() {
  const list = topLevelConversations.value;
  if (list.length === 0) return;
  const currentIndex = list.findIndex((c) => c.conversation_id === currentConversationId.value);
  const nextIndex = currentIndex < 0 ? 0 : Math.min(currentIndex + 1, list.length - 1);
  const next = list[nextIndex];
  currentConversationId.value = next.conversation_id;
  viewedConversation.value = next;
}
function navigateToPreviousConversation() {
  const list = topLevelConversations.value;
  if (list.length === 0) return;
  const currentIndex = list.findIndex((c) => c.conversation_id === currentConversationId.value);
  const prevIndex = currentIndex < 0 ? 0 : Math.max(currentIndex - 1, 0);
  const prev = list[prevIndex];
  currentConversationId.value = prev.conversation_id;
  viewedConversation.value = prev;
}
function navigateToNextUserMessage() {
  navigateUserMessageTrigger.value = Math.abs(navigateUserMessageTrigger.value) + 1;
}
function navigateToPreviousUserMessage() {
  navigateUserMessageTrigger.value = -(Math.abs(navigateUserMessageTrigger.value) + 1);
}

// ---- conversation list patch handling ----
// The conversation list and the hash it was produced under are a single
// coupled value: the patch stream is a strict old_hash->new_hash chain, so a
// patch may only be applied to the exact list its old_hash describes. The
// reactive `conversations` ref is the template's source of truth; we mirror it
// (plus the hash) into listState and only ever advance both together via
// commitListState. This matches the React app's coupled {list, hash} state so
// neither client can apply a patch built against new state onto a stale list.
function listStateNow(): ConversationListState {
  return { list: conversations.value, hash: conversationListHash };
}

function commitListState(next: ConversationListState) {
  conversations.value = next.list;
  conversationListHash = next.hash;
}

function recoverConversationListStream() {
  conversationListHash = null;
  globalStreamHandle?.forceReconnect();
}

function handleConversationListPatch(event: ConversationListPatchEvent) {
  perfCount("app.listPatch");
  const prev = conversations.value;
  const result = reduceConversationListPatch(listStateNow(), event);
  if (!result.ok) {
    if (result.reason === "hash-mismatch") {
      console.warn("conversation list patch hash mismatch; recovering via reconnect", {
        eventOldHash: result.eventOldHash,
        currentHash: conversationListHash,
      });
    } else {
      console.error(
        "failed to apply conversation list patch; recovering via reconnect",
        result.error,
        { patch: event.patch, prevLen: prev.length },
      );
    }
    recoverConversationListStream();
    return;
  }
  for (const removedId of result.removedIds) {
    void messageStore.delete(removedId);
  }
  for (const conv of result.state.list) {
    messageStore.setMaxSequenceIdKnown(conv.conversation_id, conv.max_sequence_id);
    // Seed from `working` — the list's authoritative working flag, which the
    // drawer indicator also renders — so the status bar and the conversation
    // list (the source of truth) never disagree.
    messageStore.setAgentWorking(conv.conversation_id, conv.working);
  }
  commitListState(result.state);
}

// ---- initial slug resolution ----
async function resolveInitialSlug(convs: Conversation[]): Promise<Conversation | null> {
  if (initialSlugResolved) return null;
  initialSlugResolved = true;
  const urlSlug = initialSlugFromUrl;
  if (!urlSlug) return null;
  const existingConv = convs.find((c) => c.slug === urlSlug || c.conversation_id === urlSlug);
  if (existingConv) return existingConv;
  try {
    const conv = await api.getConversationBySlug(urlSlug);
    if (conv) return conv;
  } catch (err) {
    console.error("Failed to resolve slug:", err);
  }
  window.history.replaceState({}, "", "/");
  return null;
}

async function loadConversations() {
  try {
    loading.value = true;
    error.value = null;
    const snapshot = await api.getConversationsSnapshot();
    for (const conv of snapshot.conversations) {
      messageStore.setMaxSequenceIdKnown(conv.conversation_id, conv.max_sequence_id);
    }
    const activeIds = snapshot.conversations.map((c) => c.conversation_id);
    void messageStore.pruneStale(activeIds, 7 * 24 * 60 * 60 * 1000);
    const streamHash = conversationListHash;
    if (!streamHash) {
      commitListState({ list: snapshot.conversations, hash: snapshot.hash });
    }
    const currentList = streamHash ? conversations.value : snapshot.conversations;
    if (!startupDrawerStateApplied) {
      drawerCollapsed.value = initialDrawerCollapsed(currentList, localStorage);
      startupDrawerStateApplied = true;
    }
    const topLevel = currentList.filter((c) => !c.parent_conversation_id);

    const slugConv = await resolveInitialSlug(currentList);
    if (slugConv) {
      currentConversationId.value = slugConv.conversation_id;
      viewedConversation.value = slugConv;
    } else if (!initialIsNew && topLevel.length > 0) {
      currentConversationId.value = topLevel[0].conversation_id;
      viewedConversation.value = topLevel[0];
    }
  } catch (err) {
    console.error("Failed to load conversations:", err);
    error.value = "Failed to load conversations. Please refresh the page.";
  } finally {
    loading.value = false;
  }
}

async function loadWorkspaces() {
  workspaceLoading.value = true;
  try {
    const available = await api.getWorkspaces();
    workspaces.value = available;
    let selected: Workspace | undefined;
    if (initialWorkspaceSlug) {
      selected = await api.getWorkspace(initialWorkspaceSlug);
    } else {
      const savedSlug = localStorage.getItem("shelley_selected_workspace");
      selected = available.find((workspace) => workspace.slug === savedSlug) || available[0];
    }
    if (!selected) {
      const initialPath =
        window.__SHELLEY_INIT__?.default_cwd || window.__SHELLEY_INIT__?.home_dir || "";
      if (!initialPath) throw new Error("Shelley did not provide an initial workspace path");
      selected = await api.ensureWorkspace(initialPath);
      workspaces.value = [selected];
    }
    setCurrentWorkspace(selected, false);
    if (window.location.pathname === "/") {
      window.history.replaceState({}, "", `/${selected.slug}`);
    }
  } finally {
    workspaceLoading.value = false;
  }
}

async function loadOnboarding() {
  onboardingLoading.value = true;
  onboardingError.value = null;
  try {
    onboardingStatus.value = await api.getOnboarding();
  } catch (cause) {
    onboardingError.value =
      cause instanceof Error ? cause.message : "Failed to load Shelley onboarding";
  } finally {
    onboardingLoading.value = false;
  }
}

function retryStartup() {
  error.value = null;
  void loadWorkspaces().catch((cause) => {
    error.value = cause instanceof Error ? cause.message : "Failed to load workspaces";
  });
  void loadConversations();
}

function handleOnboardingComplete(status: OnboardingStatus) {
  onboardingStatus.value = status;
  showProviderSetup.value = false;
  modelsRefreshTrigger.value++;
  startNewConversation();
}

function openProviderSetup() {
  modelsModalOpen.value = false;
  showProviderSetup.value = true;
}

// ---- conversation actions ----
function startNewConversation() {
  currentConversationId.value = null;
  viewedConversation.value = null;
  const path = currentWorkspace.value ? `/${currentWorkspace.value.slug}` : "/new";
  window.history.replaceState({}, "", path);
  drawerOpen.value = false;
}

async function startNewConversationWithCwd(cwd: string) {
  if (!(await openWorkspacePath(cwd, true))) return;
  currentConversationId.value = null;
  viewedConversation.value = null;
  drawerOpen.value = false;
  cwdSyncTrigger.value++;
}

function setConversationCwd(cwd: string) {
  void openWorkspacePath(cwd, false);
  const conv =
    conversations.value.find((c) => c.conversation_id === currentConversationId.value) ||
    (viewedConversation.value?.conversation_id === currentConversationId.value
      ? viewedConversation.value
      : null);
  if (conv?.is_draft) {
    api.updateDraft(conv.conversation_id, { cwd }).catch((err) => {
      console.debug("Could not persist draft cwd (likely already promoted):", err);
    });
  }
  cwdSyncTrigger.value++;
}

function selectConversation(conversation: Conversation) {
  herdsRouteActive.value = false;
  herdsRouteId.value = null;
  currentConversationId.value = conversation.conversation_id;
  viewedConversation.value = conversation;
  if (conversation.cwd) void openWorkspacePath(conversation.cwd, false);
  drawerOpen.value = false;
}

function toggleDrawerCollapsed() {
  drawerCollapsed.value = !drawerCollapsed.value;
  saveDrawerCollapsedPreference(drawerCollapsed.value, localStorage);
}

function updateConversation(updatedConversation: Conversation) {
  if (updatedConversation.conversation_id === currentConversationId.value) {
    viewedConversation.value = updatedConversation;
  }
}

function handleConversationArchived(
  conversationId: string,
  nextConversation?: Conversation | null,
) {
  void messageStore.delete(conversationId);
  if (currentConversationId.value === conversationId) {
    if (nextConversation && nextConversation.conversation_id !== conversationId) {
      currentConversationId.value = nextConversation.conversation_id;
      viewedConversation.value = nextConversation;
      return;
    }
    const remaining = conversations.value.filter(
      (conv) => conv.conversation_id !== conversationId && !conv.parent_conversation_id,
    );
    currentConversationId.value = remaining.length > 0 ? remaining[0].conversation_id : null;
    viewedConversation.value = remaining.length > 0 ? remaining[0] : null;
  }
}

function handleConversationUnarchived(conversation: Conversation) {
  if (conversation.conversation_id === currentConversationId.value) {
    viewedConversation.value = conversation;
  }
  showActiveTrigger.value++;
}

function handleConversationRenamed(conversation: Conversation) {
  if (conversation.conversation_id === currentConversationId.value) {
    viewedConversation.value = conversation;
  }
}

async function archiveFromChat(conversationId: string) {
  await api.archiveConversation(conversationId);
  handleConversationArchived(conversationId);
}

async function archiveFromPalette(conversationId: string) {
  try {
    await api.archiveConversation(conversationId);
    handleConversationArchived(conversationId);
  } catch (err) {
    console.error("Failed to archive conversation:", err);
  }
}

function onDraftCreated(id: string) {
  currentConversationId.value = id;
}

function onCommandPaletteClose() {
  commandPaletteOpen.value = false;
  focusMessageInputIfUnfocused();
}

// Open the fuzzy file finder (also reachable via the command palette).
function openFileFinder() {
  commandPaletteOpen.value = false;
  fileFinderOpen.value = true;
}

// Finder selected a file: close it and open the generic editor on that path.
function openFileInEditor(absPath: string) {
  fileFinderOpen.value = false;
  workspaceOpenRequest.value = { path: absPath, nonce: Date.now() };
}

function setCurrentWorkspace(workspace: Workspace, navigate: boolean) {
  currentWorkspace.value = workspace;
  localStorage.setItem("shelley_selected_workspace", workspace.slug);
  const index = workspaces.value.findIndex((item) => item.id === workspace.id);
  if (index >= 0) {
    workspaces.value = workspaces.value.map((item) => (item.id === workspace.id ? workspace : item));
  } else {
    workspaces.value = [workspace, ...workspaces.value];
  }
  if (navigate) window.history.pushState({}, "", `/${workspace.slug}`);
}

async function openWorkspacePath(path: string, navigate: boolean): Promise<boolean> {
  try {
    const workspace = await api.ensureWorkspace(path);
    setCurrentWorkspace(workspace, navigate);
    return true;
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : "Failed to open workspace";
    return false;
  }
}

function setWorkspaceDirectory(path: string) {
  void openWorkspacePath(path, false);
}

async function changeWorkspaceDirectory(path: string) {
  await startNewConversationWithCwd(path);
}

function selectWorkspaceById(id: string) {
  const workspace = workspaces.value.find((item) => item.id === id);
  if (!workspace) return;
  setCurrentWorkspace(workspace, true);
  startNewConversation();
}

// A comment submitted from the file editor's comment mode: hand it to
// ChatInterface for injection into the message input.
function onEditorComment(text: string) {
  editorCommentText.value = { text };
}

async function handleFirstMessage(
  message: string,
  model: string,
  cwd?: string,
  toolOverrides?: Record<string, "on" | "off">,
  thinkingLevel?: "off" | "minimal" | "low" | "medium" | "high" | "xhigh",
) {
  try {
    const hasOverrides = toolOverrides && Object.keys(toolOverrides).length > 0;
    const hasThinking = !!thinkingLevel;
    const convOpts =
      hasOverrides || hasThinking
        ? {
            ...(hasOverrides ? { tool_overrides: toolOverrides } : {}),
            ...(hasThinking ? { thinking_level: thinkingLevel } : {}),
          }
        : undefined;
    const response = await api.sendMessageWithNewConversation({
      message,
      model,
      cwd,
      conversation_options: convOpts,
    });
    const newConversationId = response.conversation_id;
    messageStore.setAgentWorking(newConversationId, true);
    currentConversationId.value = newConversationId;
  } catch (err) {
    console.error("Failed to send first message:", err);
    error.value = err instanceof Error ? err.message : "Failed to send message";
    throw err;
  }
}

async function handleDistillNewGeneration(
  sourceConversationId: string,
  model: string,
  cwd?: string,
  method?: "default" | "compact",
  instructions?: string,
) {
  try {
    await api.distillNewGeneration(sourceConversationId, model, cwd, method, instructions);
    currentConversationId.value = sourceConversationId;
  } catch (err) {
    console.error("Failed to compact into new generation:", err);
    error.value = "Failed to compact conversation";
    throw err;
  }
}

// ---- global keyboard shortcuts (incl. Ctrl+M chord) ----
let chordPending = false;
let chordTimer: number | null = null;
function clearChord() {
  chordPending = false;
  if (chordTimer !== null) {
    clearTimeout(chordTimer);
    chordTimer = null;
  }
}
const isMac = navigator.platform.toUpperCase().includes("MAC");

function handleKeyDown(e: KeyboardEvent) {
  if (chordPending) {
    clearChord();
    if (e.key === "n" || e.key === "N") {
      e.preventDefault();
      navigateToNextUserMessage();
      return;
    }
    if (e.key === "p" || e.key === "P") {
      e.preventDefault();
      navigateToPreviousUserMessage();
      return;
    }
    return;
  }

  if (e.ctrlKey && !e.metaKey && !e.altKey && (e.key === "m" || e.key === "M")) {
    e.preventDefault();
    chordPending = true;
    chordTimer = window.setTimeout(clearChord, 1500);
    return;
  }

  if (isMac && e.ctrlKey && !e.metaKey) return;
  const modifierPressed = isMac ? e.metaKey : e.ctrlKey;

  if (modifierPressed && e.key === "k") {
    e.preventDefault();
    commandPaletteOpen.value = !commandPaletteOpen.value;
    return;
  }

  // Cmd/Ctrl+Shift+P opens the fuzzy file finder. `e.key` is "P" (uppercase)
  // when Shift is held; also accept "p" defensively across layouts.
  if (modifierPressed && e.shiftKey && (e.key === "p" || e.key === "P")) {
    e.preventDefault();
    fileFinderOpen.value = true;
    return;
  }

  if (e.altKey && !e.ctrlKey && !e.metaKey && !e.shiftKey && e.key === "ArrowDown") {
    e.preventDefault();
    navigateToNextConversation();
    return;
  }

  if (e.altKey && !e.ctrlKey && !e.metaKey && !e.shiftKey && e.key === "ArrowUp") {
    e.preventDefault();
    navigateToPreviousConversation();
    return;
  }
}

// ---- popstate (back/forward + SubagentTool navigation) ----
async function handlePopState() {
  if (isHerdsPath()) {
    herdsRouteActive.value = true;
    herdsRouteId.value = getHerdIdFromPath();
    return;
  }
  herdsRouteActive.value = false;
  herdsRouteId.value = null;
  const workspaceSlug = getWorkspaceSlugFromPath();
  if (workspaceSlug) {
    try {
      setCurrentWorkspace(await api.getWorkspace(workspaceSlug), false);
      currentConversationId.value = null;
      viewedConversation.value = null;
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : "Failed to open workspace";
    }
    return;
  }
  if (isNewPath()) {
    currentConversationId.value = null;
    viewedConversation.value = null;
    return;
  }
  const slug = getSlugFromPath();
  if (!slug) return;
  const existingConv = conversations.value.find(
    (c) => c.slug === slug || c.conversation_id === slug,
  );
  if (existingConv) {
    currentConversationId.value = existingConv.conversation_id;
    viewedConversation.value = existingConv;
    return;
  }
  try {
    const conv = await api.getConversationBySlug(slug);
    if (conv) {
      currentConversationId.value = conv.conversation_id;
      viewedConversation.value = conv;
    }
  } catch (err) {
    console.error("Failed to navigate to conversation:", err);
  }
}

// ---- page title + URL sync ----
watch(
  [currentConversationId, viewedConversation, conversations, herdsRouteActive],
  () => {
    if (herdsRouteActive.value) return;
    const currentConv =
      viewedConversation.value?.conversation_id === currentConversationId.value
        ? viewedConversation.value
        : conversations.value.find((conv) => conv.conversation_id === currentConversationId.value);
    if (currentConv) {
      updatePageTitle(currentConv);
      updateUrlWithSlug(currentConv);
    }
  },
  { deep: false },
);

// ---- lifecycle ----
onMounted(() => {
  initializeA11yTrace();
  void loadOnboarding();
  void loadWorkspaces().catch((cause) => {
    error.value = cause instanceof Error ? cause.message : "Failed to load workspaces";
  });
  // Hydrate persistent terminals from the server.
  let cancelled = false;
  fetch("/api/terminals")
    .then((r) => (r.ok ? r.json() : []))
    .then(
      (
        rows: Array<{
          id: string;
          command: string;
          cwd: string;
          created_at: string;
          owner_type?: "workspace" | "herd" | "unowned";
          workspace_id?: string;
          herd_id?: string;
          herd_name?: string;
        }>,
      ) => {
      if (cancelled || !Array.isArray(rows) || rows.length === 0) return;
      setEphemeralTerminals((prev) => {
        const have = new Set(prev.map((tm) => tm.termId).filter(Boolean));
        const restored: EphemeralTerminal[] = rows
          .filter(
            (r) =>
              r.owner_type === "workspace" &&
              !!r.workspace_id &&
              !have.has(r.id) &&
              !closedTerminalIds.has(r.id),
          )
          .map((r) => ({
            id: r.id,
            termId: r.id,
            command: r.command,
            cwd: r.cwd,
            createdAt: new Date(r.created_at || Date.now()),
            workspaceId: r.workspace_id,
            herdId: r.owner_type === "herd" ? r.herd_id : undefined,
            herdName: r.owner_type === "herd" ? r.herd_name : undefined,
          }));
        return [...restored, ...prev];
      });
      },
    )
    .catch((err) => console.warn("failed to fetch persistent terminals:", err));
  terminalsHydrationCancel = () => {
    cancelled = true;
  };

  loadConversations();

  globalStreamHandle = connectGlobalStream({
    getHash: () => conversationListHash,
    onListPatch: handleConversationListPatch,
    onNotificationEvent: handleNotificationEvent,
    onStatusChange: (status) => (streamStatus.value = status),
    onReconnect: () => {
      reconnectNonce.value++;
    },
  });

  document.addEventListener("keydown", handleKeyDown);
  window.addEventListener("popstate", handlePopState);
});

let terminalsHydrationCancel: (() => void) | null = null;

onUnmounted(() => {
  terminalsHydrationCancel?.();
  globalStreamHandle?.close();
  globalStreamHandle = null;
  document.removeEventListener("keydown", handleKeyDown);
  window.removeEventListener("popstate", handlePopState);
  clearChord();
});
</script>

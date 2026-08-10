<!-- Herd control room: list packs, manage members, open/close all, focus PTYs. -->
<template>
  <div class="herds-page" @keydown="onPageKeydown">
    <header class="herds-page-header">
      <div class="herds-page-header-left">
        <button
          type="button"
          class="btn-icon herds-back-btn"
          aria-label="Back to conversations"
          @click="emit('back')"
        >
          ←
        </button>
        <h1 class="herds-page-title">
          {{ detail ? detail.name : "Herds" }}
          <span v-if="!detail && globalNeedsUser > 0" class="herds-badge">
            {{ globalNeedsUser }} need you
          </span>
        </h1>
      </div>
      <div class="herds-page-header-actions">
        <template v-if="!detail">
          <Button
            :label="showArchived ? 'Active herds' : 'Archived herds'"
            severity="secondary"
            @click="toggleArchived"
          />
          <Button v-if="!showArchived" label="New herd" @click="beginCreateHerd" />
        </template>
        <template v-else>
          <Button
            label="Open all"
            :disabled="detail.lifecycle !== 'active' || bulkBusy"
            @click="openAll"
          />
          <Button
            label="Close all"
            severity="secondary"
            :disabled="detail.lifecycle !== 'active' || bulkBusy"
            @click="beginCloseAll"
          />
          <Button
            label="Add existing terminal"
            severity="secondary"
            :disabled="detail.lifecycle !== 'active'"
            @click="showAttach = true"
          />
          <Button
            label="New terminal"
            severity="secondary"
            :disabled="detail.lifecycle !== 'active'"
            @click="createNewTerminalMember"
          />
          <Button
            v-if="detail.lifecycle === 'active'"
            label="Rename"
            text
            severity="secondary"
            @click="beginRename"
          />
          <Button
            v-if="detail.lifecycle === 'active'"
            label="Archive"
            text
            severity="secondary"
            @click="setLifecycle('archived')"
          />
          <Button
            v-else
            label="Restore"
            text
            severity="secondary"
            @click="setLifecycle('active')"
          />
        </template>
      </div>
    </header>

    <div class="sr-only" role="status" aria-live="polite" aria-atomic="true">
      {{ liveAnnouncement }}
    </div>

    <div v-if="error" class="herds-error" role="alert">{{ error }}</div>
    <div v-if="bulkResultText" class="herds-bulk-result" role="status">{{ bulkResultText }}</div>

    <!-- List -->
    <div v-if="!detail" class="herds-list">
      <p v-if="!loading && visibleHerds.length === 0" class="herds-empty">
        {{
          showArchived
            ? "No archived herds."
            : "No herds yet. Create a herd for the work in front of you."
        }}
      </p>
      <div
        v-if="!loading && visibleHerds.length === 0 && !showArchived"
        class="herds-empty-actions"
      >
        <Button label="Create herd" @click="beginCreateHerd" />
      </div>
      <ul v-else class="herds-list-ul" aria-label="Herds">
        <li v-for="h in visibleHerds" :key="h.id" class="herds-list-item">
          <a class="herds-list-link" :href="`/herds/${h.id}`" @click.prevent="openHerd(h.id)">
            <span class="herds-list-name">{{ h.name }}</span>
            <span class="herds-list-summary">{{ summaryText(h.summary) }}</span>
            <span v-if="h.default_cwd" class="herds-list-cwd">{{ h.default_cwd }}</span>
          </a>
          <div class="herds-list-actions">
            <button
              v-if="h.lifecycle === 'active'"
              type="button"
              class="btn-icon-sm"
              :aria-label="`Open all in ${h.name}`"
              @click="openAllHerd(h.id)"
            >
              Open all
            </button>
            <button
              v-if="h.lifecycle === 'active'"
              type="button"
              class="btn-icon-sm"
              :aria-label="`Close all in ${h.name}`"
              @click="beginCloseAllHerd(h.id)"
            >
              Close all
            </button>
            <button
              v-if="h.lifecycle === 'active'"
              type="button"
              class="btn-icon-sm"
              :aria-label="`Archive ${h.name}`"
              @click="archiveHerd(h.id)"
            >
              Archive
            </button>
            <button
              v-else
              type="button"
              class="btn-icon-sm"
              :aria-label="`Restore ${h.name}`"
              @click="restoreHerd(h.id)"
            >
              Restore
            </button>
          </div>
        </li>
      </ul>

      <section class="herds-loose-section" aria-labelledby="loose-terminals-heading">
        <div class="herds-loose-header">
          <div>
            <h2 id="loose-terminals-heading" class="herds-section-title">Loose terminals</h2>
            <p class="herds-section-help">
              Terminals without a conversation or herd. Assign them to a herd or close them here.
            </p>
          </div>
          <div class="herds-list-actions">
            <Button
              v-if="looseTerminals.length > 0"
              label="Create herd from loose"
              severity="secondary"
              @click="createFromLoose"
            />
            <Button
              v-if="looseTerminals.length > 0"
              label="Close loose terminals"
              severity="danger"
              @click="beginCloseLoose"
            />
          </div>
        </div>
        <p v-if="looseTerminals.length === 0" class="herds-empty">No loose terminals.</p>
        <ul v-else class="herds-loose-list" aria-label="Loose terminals">
          <li v-for="t in looseTerminals" :key="t.id">
            <div class="herds-loose-item">
              <strong>{{ t.command }}</strong>
              <span>{{ t.cwd }}</span>
              <span class="herds-mono">{{ t.id }}</span>
            </div>
          </li>
        </ul>
      </section>
    </div>

    <!-- Detail -->
    <div v-else class="herds-detail">
      <p class="herds-detail-summary">{{ summaryText(detail.summary) }}</p>
      <div class="herds-filters" role="toolbar" aria-label="Member filters">
        <button
          v-for="f in filters"
          :key="f.id"
          type="button"
          :class="['herds-filter', { active: filter === f.id }]"
          :aria-pressed="filter === f.id"
          @click="filter = f.id"
        >
          {{ f.label }}
        </button>
      </div>

      <div class="herds-detail-body">
        <div class="herds-members" role="list" aria-label="Herd members" tabindex="0">
          <p v-if="filteredMembers.length === 0" class="herds-empty">
            No members yet. Add an open terminal or create one for this herd.
          </p>
          <div
            v-for="m in filteredMembers"
            :key="m.id"
            role="listitem"
            :class="['herds-member-row', { selected: selectedMemberId === m.id }]"
          >
            <button
              type="button"
              class="herds-member-select"
              :aria-pressed="selectedMemberId === m.id"
              @click="selectMember(m)"
            >
              <span class="herds-member-top">
                <span class="herds-member-label">{{ m.label }}</span>
                <span class="herds-member-state"
                  >{{ m.process_state }} · {{ m.attention_state }}</span
                >
              </span>
              <span class="herds-member-meta">
                <span>{{ m.command || m.recipe.command || "—" }}</span>
                <span v-if="m.cwd || m.recipe.cwd">{{ m.cwd || m.recipe.cwd }}</span>
              </span>
              <span v-if="m.recent_output" class="herds-member-output">{{ m.recent_output }}</span>
            </button>
            <div class="herds-member-actions">
              <template v-if="m.process_state === 'running'">
                <button type="button" @click="focusMember(m)">Focus</button>
                <template v-if="detail.lifecycle === 'active'">
                  <button type="button" @click="detachMember(m)">Detach</button>
                  <button type="button" @click="closeMember(m)">Close</button>
                </template>
              </template>
              <template v-else-if="m.process_state === 'closed' && detail.lifecycle === 'active'">
                <button type="button" @click="openMember(m)">Open</button>
                <button type="button" @click="removeMember(m)">Remove</button>
              </template>
              <template v-else-if="m.process_state === 'missing' && detail.lifecycle === 'active'">
                <button type="button" @click="openMember(m)">Respawn</button>
                <button type="button" @click="repairMember(m)">Repair recipe</button>
                <button type="button" @click="removeMember(m)">Remove</button>
              </template>
              <button
                v-if="detail.lifecycle === 'active'"
                type="button"
                @click="beginLinkConversation(m)"
              >
                {{ m.conversation_id ? "Change conversation" : "Link conversation" }}
              </button>
            </div>
          </div>
        </div>

        <div class="herds-focus-pane">
          <div v-if="!selectedMember" class="herds-focus-empty">
            Select a member. Focusing a terminal is explicit (f or Focus).
          </div>
          <template v-else>
            <div class="herds-focus-header">
              <strong>{{ selectedMember.label }}</strong>
              <span>{{ selectedMember.process_state }} · {{ selectedMember.attention_state }}</span>
              <button
                v-if="selectedMember.process_state === 'running' && selectedMember.terminal_id"
                type="button"
                ref="focusTerminalBtn"
                @click="focusMember(selectedMember)"
              >
                Focus terminal
              </button>
              <a
                v-if="selectedMember.conversation_id"
                class="herds-conv-link"
                :href="convHref(selectedMember)"
                @click.prevent="emit('open-conversation', selectedMember.conversation_id!)"
              >
                Open conversation{{
                  selectedMember.conversation_slug ? ` (${selectedMember.conversation_slug})` : ""
                }}
              </a>
            </div>
            <div
              v-if="focusedTermId && selectedMember.terminal_id === focusedTermId"
              class="herds-xterm-host"
            >
              <TerminalInstance
                :key="focusedTermId"
                :term="{
                  id: 'herd-focus',
                  command: selectedMember.command || selectedMember.recipe.command || 'bash',
                  cwd: selectedMember.cwd || selectedMember.recipe.cwd || '',
                  createdAt: new Date(),
                  termId: focusedTermId,
                }"
                :is-visible="true"
                :is-dark="true"
                @attached="onFocusAttached"
                @status-change="() => {}"
              />
            </div>
            <p v-else class="herds-focus-hint">
              Terminal is not attached to the UI. Press Focus terminal to attach the live PTY.
            </p>
          </template>
        </div>
      </div>
    </div>

    <!-- Create herd modal -->
    <Modal
      :is-open="showCreate"
      :title="createFromLoosePending ? 'New herd from open terminals' : 'New herd'"
      @close="cancelCreateHerd"
    >
      <p v-if="createFromLoosePending">
        Name the herd that will receive every currently loose terminal.
      </p>
      <label class="herds-field">
        Name
        <input v-model="createName" type="text" class="herds-input" @keydown.enter="createHerd" />
      </label>
      <label class="herds-field">
        Default cwd
        <input v-model="createCwd" type="text" class="herds-input" />
      </label>
      <label class="herds-field">
        Default command
        <input v-model="createCmd" type="text" class="herds-input" />
      </label>
      <template #footer>
        <Button label="Cancel" text severity="secondary" @click="cancelCreateHerd" />
        <Button label="Create" @click="createHerd" />
      </template>
    </Modal>

    <!-- Attach existing -->
    <Modal :is-open="showAttach" title="Add existing terminal" @close="showAttach = false">
      <label class="herds-field">
        Linked conversation
        <select v-model="attachConversationId" class="herds-input">
          <option value="">None</option>
          <option
            v-for="conversation in conversations"
            :key="conversation.conversation_id"
            :value="conversation.conversation_id"
          >
            {{ conversationLabel(conversation) }}
          </option>
        </select>
      </label>
      <p v-if="looseTerminals.length === 0" class="herds-empty">No loose terminals.</p>
      <ul v-else class="herds-loose-list">
        <li v-for="t in looseTerminals" :key="t.id">
          <button type="button" class="herds-loose-item" @click="attachLoose(t)">
            <strong>{{ t.command }}</strong>
            <span>{{ t.cwd }}</span>
            <span class="herds-mono">{{ t.id }}</span>
          </button>
        </li>
      </ul>
      <template #footer>
        <Button label="Cancel" text severity="secondary" @click="showAttach = false" />
      </template>
    </Modal>

    <!-- Close all confirm -->
    <Modal :is-open="!!closeConfirm" title="Close all terminals" @close="closeConfirm = null">
      <p>
        Close {{ closeConfirm?.live ?? 0 }} terminal{{ closeConfirm?.live === 1 ? "" : "s" }}?
        Linked conversations will continue after their PTY closes.
      </p>
      <ul v-if="closeConfirm?.warnings?.length" class="herds-warn-list">
        <li v-for="w in closeConfirm.warnings" :key="w.member_id">
          {{ w.label }} — {{ w.attention_state }}
          <span v-if="w.conversation_slug">({{ w.conversation_slug }})</span>
        </li>
      </ul>
      <template #footer>
        <Button label="Cancel" text severity="secondary" autofocus @click="closeConfirm = null" />
        <Button label="Close all terminals" severity="danger" @click="confirmCloseAll" />
      </template>
    </Modal>

    <Modal
      :is-open="!!closeLooseConfirm"
      title="Close loose terminals"
      @close="closeLooseConfirm = false"
    >
      <p>
        Close {{ looseTerminals.length }} loose terminal{{ looseTerminals.length === 1 ? "" : "s" }}?
        Terminals assigned to a conversation or herd will not be affected.
      </p>
      <template #footer>
        <Button label="Cancel" text severity="secondary" @click="closeLooseConfirm = false" />
        <Button label="Close loose terminals" severity="danger" @click="confirmCloseLoose" />
      </template>
    </Modal>

    <!-- Close one member -->
    <Modal
      :is-open="!!pendingCloseMember"
      title="Close terminal"
      @close="pendingCloseMember = null"
    >
      <p>Close terminal for {{ pendingCloseMember?.label }}?</p>
      <p v-if="pendingCloseMember?.conversation_id">
        Its linked conversation is {{ pendingCloseMember.attention_state }} and will continue after
        the PTY closes.
      </p>
      <template #footer>
        <Button label="Cancel" text severity="secondary" @click="pendingCloseMember = null" />
        <Button label="Close" severity="danger" @click="confirmCloseMember" />
      </template>
    </Modal>

    <!-- Move confirm -->
    <Modal :is-open="!!pendingMove" title="Move terminal" @close="pendingMove = null">
      <p>
        Terminal {{ pendingMove?.id }} belongs to another herd. Move it here without restarting?
      </p>
      <template #footer>
        <Button label="Cancel" text severity="secondary" @click="pendingMove = null" />
        <Button label="Move" @click="confirmMove" />
      </template>
    </Modal>

    <!-- Repair recipe -->
    <Modal :is-open="!!repairTarget" title="Repair recipe" @close="repairTarget = null">
      <label class="herds-field">
        Command
        <input v-model="repairCmd" type="text" class="herds-input" />
      </label>
      <label class="herds-field">
        Cwd
        <input v-model="repairCwd" type="text" class="herds-input" />
      </label>
      <template #footer>
        <Button label="Cancel" text severity="secondary" @click="repairTarget = null" />
        <Button label="Save recipe" @click="saveRepair" />
      </template>
    </Modal>

    <Modal :is-open="showRename" title="Rename herd" @close="showRename = false">
      <label class="herds-field">
        Name
        <input v-model="renameValue" type="text" class="herds-input" @keydown.enter="saveRename" />
      </label>
      <template #footer>
        <Button label="Cancel" text severity="secondary" @click="showRename = false" />
        <Button label="Rename" @click="saveRename" />
      </template>
    </Modal>

    <Modal :is-open="!!linkTarget" title="Link conversation" @close="linkTarget = null">
      <label class="herds-field">
        Conversation
        <select v-model="linkConversationId" class="herds-input">
          <option value="">None</option>
          <option
            v-for="conversation in conversations"
            :key="conversation.conversation_id"
            :value="conversation.conversation_id"
          >
            {{ conversationLabel(conversation) }}
          </option>
        </select>
      </label>
      <template #footer>
        <Button label="Cancel" text severity="secondary" @click="linkTarget = null" />
        <Button label="Save link" @click="saveConversationLink" />
      </template>
    </Modal>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from "vue";
import Button from "primevue/button";
import Modal from "./Modal.vue";
import TerminalInstance from "./TerminalInstance.vue";

export interface HerdSummary {
  open: number;
  closed: number;
  missing: number;
  working: number;
  needs_user: number;
  total: number;
}

export interface Herd {
  id: string;
  name: string;
  notes: string;
  default_cwd: string;
  default_command: string;
  lifecycle: string;
  updated_at: string;
  summary: HerdSummary;
  members?: HerdMember[];
}

export interface HerdMember {
  id: string;
  herd_id: string;
  terminal_id: string | null;
  conversation_id: string | null;
  conversation_slug?: string | null;
  label: string;
  sort_order: number;
  recipe: { command: string; cwd: string; env: Record<string, string> };
  desired_state: string;
  process_state: string;
  attention_state: string;
  command: string;
  cwd: string;
  recent_output?: string;
}

interface ConversationOption {
  conversation_id: string;
  slug?: string | null;
}

interface LooseTerminal {
  id: string;
  command: string;
  cwd: string;
  created_at: string;
}

const props = defineProps<{
  herdId: string | null;
  conversations: ConversationOption[];
}>();

const emit = defineEmits<{
  (e: "back"): void;
  (e: "navigate-herd", id: string | null): void;
  (e: "open-conversation", id: string): void;
  (e: "terminals-closed", ids: string[]): void;
}>();

const herds = ref<Herd[]>([]);
const detail = ref<Herd | null>(null);
const loading = ref(true);
const error = ref<string | null>(null);
const liveAnnouncement = ref("");
const bulkResultText = ref("");
const bulkBusy = ref(false);
const filter = ref<"all" | "needs_you" | "working" | "quiet" | "closed">("all");
const selectedMemberId = ref<string | null>(null);
const focusedTermId = ref<string | null>(null);
const showCreate = ref(false);
const createFromLoosePending = ref(false);
const pendingLooseTerminals = ref<LooseTerminal[]>([]);
const showAttach = ref(false);
const attachConversationId = ref("");
const showArchived = ref(false);
const showRename = ref(false);
const renameValue = ref("");
const linkTarget = ref<HerdMember | null>(null);
const linkConversationId = ref("");
const createName = ref("");
const createCwd = ref("");
const createCmd = ref("bash");
const looseTerminals = ref<LooseTerminal[]>([]);
const closeLooseConfirm = ref(false);
const closeConfirm = ref<{
  herdId: string;
  live: number;
  warnings: Array<{
    member_id: string;
    label: string;
    attention_state: string;
    conversation_slug?: string;
  }>;
} | null>(null);
const repairTarget = ref<HerdMember | null>(null);
const repairCmd = ref("");
const repairCwd = ref("");

const filters = [
  { id: "all" as const, label: "All" },
  { id: "needs_you" as const, label: "Needs you" },
  { id: "working" as const, label: "Working" },
  { id: "quiet" as const, label: "Quiet" },
  { id: "closed" as const, label: "Closed" },
];

const globalNeedsUser = computed(() =>
  herds.value
    .filter((herd) => herd.lifecycle === "active")
    .reduce((count, herd) => count + (herd.summary?.needs_user || 0), 0),
);
const visibleHerds = computed(() =>
  herds.value.filter((herd) => herd.lifecycle === (showArchived.value ? "archived" : "active")),
);

const selectedMember = computed(
  () => detail.value?.members?.find((m) => m.id === selectedMemberId.value) ?? null,
);

const filteredMembers = computed(() => {
  const list = detail.value?.members ?? [];
  switch (filter.value) {
    case "needs_you":
      return list.filter((m) => m.attention_state === "needs_user");
    case "working":
      return list.filter((m) => m.attention_state === "working");
    case "quiet":
      return list.filter((m) => m.attention_state === "quiet");
    case "closed":
      return list.filter((m) => m.process_state === "closed" || m.process_state === "missing");
    default:
      return list;
  }
});

function summaryText(s: HerdSummary): string {
  if (!s) return "";
  const parts: string[] = [];
  if (s.open) parts.push(`${s.open} open`);
  if (s.needs_user) parts.push(`${s.needs_user} need you`);
  if (s.working) parts.push(`${s.working} working`);
  if (s.missing) parts.push(`${s.missing} missing`);
  if (s.closed) parts.push(`${s.closed} closed`);
  if (parts.length === 0) return `${s.total} members`;
  return parts.join(" · ");
}

function convHref(m: HerdMember): string {
  if (m.conversation_slug) return `/c/${m.conversation_slug}`;
  if (m.conversation_id) return `/c/${m.conversation_id}`;
  return "/";
}

async function apiJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const r = await fetch(url, {
    ...init,
    headers: { "Content-Type": "application/json", ...(init?.headers || {}) },
  });
  if (!r.ok) {
    let msg = r.statusText;
    try {
      const body = await r.json();
      msg = body.error || msg;
    } catch {
      /* ignore */
    }
    throw new Error(msg);
  }
  if (r.status === 204) return undefined as T;
  return r.json();
}

async function loadList() {
  loading.value = true;
  error.value = null;
  try {
    const [list, loose] = await Promise.all([
      apiJSON<Herd[]>(showArchived.value ? "/api/herds?archived=1" : "/api/herds"),
      apiJSON<LooseTerminal[]>("/api/terminals/loose"),
    ]);
    herds.value = list;
    looseTerminals.value = loose;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    loading.value = false;
  }
}

async function loadDetail(id: string) {
  loading.value = true;
  error.value = null;
  try {
    detail.value = await apiJSON<Herd>(`/api/herds/${encodeURIComponent(id)}`);
    const members = detail.value.members ?? [];
    // Prefer needs_user, then working, then first
    const pick =
      members.find((m) => m.attention_state === "needs_user") ||
      members.find((m) => m.attention_state === "working") ||
      members[0];
    if (pick) {
      selectedMemberId.value = pick.id;
      liveAnnouncement.value = `${pick.label}, ${pick.process_state}, ${pick.attention_state}`;
    } else {
      selectedMemberId.value = null;
      focusedTermId.value = null;
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
    detail.value = null;
  } finally {
    loading.value = false;
  }
}

async function refresh() {
  if (props.herdId) await loadDetail(props.herdId);
  else {
    detail.value = null;
    await loadList();
  }
}

onMounted(refresh);
watch(() => props.herdId, refresh);

function openHerd(id: string) {
  emit("navigate-herd", id);
}

function conversationLabel(conversation: ConversationOption): string {
  return conversation.slug || conversation.conversation_id;
}

function beginCreateHerd() {
  createFromLoosePending.value = false;
  pendingLooseTerminals.value = [];
  createName.value = "";
  showCreate.value = true;
}

function cancelCreateHerd() {
  showCreate.value = false;
  createFromLoosePending.value = false;
  pendingLooseTerminals.value = [];
}

async function toggleArchived() {
  showArchived.value = !showArchived.value;
  await loadList();
}

async function createHerd() {
  const name = createName.value.trim();
  if (!name) return;
  try {
    const h = await apiJSON<Herd>("/api/herds", {
      method: "POST",
      body: JSON.stringify({
        name,
        default_cwd: createCwd.value.trim(),
        default_command: createCmd.value.trim() || "bash",
      }),
    });
    let attachFailures = 0;
    if (createFromLoosePending.value) {
      for (const terminal of pendingLooseTerminals.value) {
        try {
          await apiJSON(`/api/herds/${h.id}/members`, {
            method: "POST",
            body: JSON.stringify({ terminal_id: terminal.id, label: terminal.command }),
          });
        } catch {
          attachFailures++;
        }
      }
    }
    showCreate.value = false;
    createFromLoosePending.value = false;
    pendingLooseTerminals.value = [];
    createName.value = "";
    emit("navigate-herd", h.id);
    if (attachFailures > 0) {
      error.value = `${attachFailures} terminal${attachFailures === 1 ? "" : "s"} could not be attached.`;
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

async function createFromLoose() {
  try {
    const loose = await apiJSON<LooseTerminal[]>("/api/terminals/loose");
    if (loose.length === 0) {
      liveAnnouncement.value = "No loose terminals.";
      return;
    }
    pendingLooseTerminals.value = loose;
    createFromLoosePending.value = true;
    createName.value = "pack";
    showCreate.value = true;
    await nextTick();
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

function beginCloseLoose() {
  if (looseTerminals.value.length > 0) closeLooseConfirm.value = true;
}

async function confirmCloseLoose() {
  closeLooseConfirm.value = false;
  try {
    const result = await apiJSON<{ closed: string[] }>("/api/terminals/loose", { method: "DELETE" });
    if (result.closed.length > 0) emit('terminals-closed', result.closed);
    looseTerminals.value = [];
    liveAnnouncement.value = `Closed ${result.closed.length} loose terminal${result.closed.length === 1 ? "" : "s"}.`;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

async function setLifecycle(lifecycle: string) {
  if (!detail.value) return;
  try {
    detail.value = await apiJSON<Herd>(`/api/herds/${detail.value.id}`, {
      method: "PATCH",
      body: JSON.stringify({ lifecycle }),
    });
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

async function archiveHerd(id: string) {
  try {
    await apiJSON(`/api/herds/${id}`, {
      method: "PATCH",
      body: JSON.stringify({ lifecycle: "archived" }),
    });
    await loadList();
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

async function restoreHerd(id: string) {
  try {
    await apiJSON(`/api/herds/${id}`, {
      method: "PATCH",
      body: JSON.stringify({ lifecycle: "active" }),
    });
    await loadList();
    liveAnnouncement.value = "Herd restored.";
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

function beginRename() {
  if (!detail.value) return;
  renameValue.value = detail.value.name;
  showRename.value = true;
}

async function saveRename() {
  if (!detail.value || !renameValue.value.trim()) return;
  try {
    detail.value = await apiJSON<Herd>(`/api/herds/${detail.value.id}`, {
      method: "PATCH",
      body: JSON.stringify({ name: renameValue.value.trim() }),
    });
    showRename.value = false;
    liveAnnouncement.value = `Renamed herd to ${detail.value.name}`;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

function beginLinkConversation(member: HerdMember) {
  linkTarget.value = member;
  linkConversationId.value = member.conversation_id || "";
}

async function saveConversationLink() {
  if (!detail.value || !linkTarget.value) return;
  try {
    const body = linkConversationId.value
      ? { conversation_id: linkConversationId.value }
      : { clear_conversation: true };
    await apiJSON(`/api/herds/${detail.value.id}/members/${linkTarget.value.id}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    });
    const label = linkTarget.value.label;
    const linkedConversation = props.conversations.find(
      (conversation) => conversation.conversation_id === linkConversationId.value,
    );
    linkTarget.value = null;
    await loadDetail(detail.value.id);
    liveAnnouncement.value = linkConversationId.value
      ? `Linked ${label} to ${linkedConversation ? conversationLabel(linkedConversation) : linkConversationId.value}`
      : `Unlinked conversation from ${label}`;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

function selectMember(m: HerdMember) {
  selectedMemberId.value = m.id;
  focusedTermId.value = null; // selection must not steal focus into PTY
  liveAnnouncement.value = `${m.label}, ${m.process_state}, ${m.attention_state}`;
}

function focusMember(m: HerdMember) {
  if (!m.terminal_id || m.process_state !== "running") return;
  focusedTermId.value = m.terminal_id;
  liveAnnouncement.value = `Focused terminal for ${m.label}`;
}

function onFocusAttached(_id: string, termId: string) {
  focusedTermId.value = termId;
}

async function openMember(m: HerdMember) {
  if (!detail.value) return;
  try {
    await apiJSON(`/api/herds/${detail.value.id}/members/${m.id}/open`, { method: "POST" });
    await loadDetail(detail.value.id);
    liveAnnouncement.value = `Opened ${m.label}`;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

const pendingCloseMember = ref<HerdMember | null>(null);

function closeMember(m: HerdMember) {
  pendingCloseMember.value = m;
}

async function confirmCloseMember() {
  const m = pendingCloseMember.value;
  if (!detail.value || !m) return;
  pendingCloseMember.value = null;
  try {
    await apiJSON(`/api/herds/${detail.value.id}/members/${m.id}/close`, {
      method: "POST",
      body: JSON.stringify({ mode: "graceful" }),
    });
    if (m.terminal_id) emit("terminals-closed", [m.terminal_id]);
    focusedTermId.value = null;
    await loadDetail(detail.value.id);
    liveAnnouncement.value = `Closed ${m.label}`;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

async function detachMember(m: HerdMember) {
  if (!detail.value) return;
  try {
    await apiJSON(`/api/herds/${detail.value.id}/members/${m.id}/detach`, { method: "POST" });
    focusedTermId.value = null;
    await loadDetail(detail.value.id);
    liveAnnouncement.value = `Detached ${m.label}`;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

async function removeMember(m: HerdMember) {
  if (!detail.value) return;
  try {
    await apiJSON(`/api/herds/${detail.value.id}/members/${m.id}`, { method: "DELETE" });
    await loadDetail(detail.value.id);
    liveAnnouncement.value = `Removed ${m.label}`;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

function repairMember(m: HerdMember) {
  repairTarget.value = m;
  repairCmd.value = m.recipe.command || m.command || "";
  repairCwd.value = m.recipe.cwd || m.cwd || "";
}

async function saveRepair() {
  if (!detail.value || !repairTarget.value) return;
  try {
    await apiJSON(`/api/herds/${detail.value.id}/members/${repairTarget.value.id}`, {
      method: "PATCH",
      body: JSON.stringify({
        recipe: {
          command: repairCmd.value,
          cwd: repairCwd.value,
          env: repairTarget.value.recipe.env || {},
        },
      }),
    });
    repairTarget.value = null;
    await loadDetail(detail.value.id);
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

async function createNewTerminalMember() {
  if (!detail.value) return;
  const cmd = detail.value.default_command || "bash";
  const cwd = detail.value.default_cwd || "";
  try {
    await apiJSON(`/api/herds/${detail.value.id}/members`, {
      method: "POST",
      body: JSON.stringify({ label: cmd, recipe: { command: cmd, cwd, env: {} } }),
    });
    // Open it immediately
    await loadDetail(detail.value.id);
    const last = detail.value.members?.[detail.value.members.length - 1];
    if (last) await openMember(last);
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

watch(showAttach, async (open) => {
  if (!open) return;
  try {
    looseTerminals.value = await apiJSON<LooseTerminal[]>("/api/terminals/loose");
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
});

async function attachLoose(t: LooseTerminal) {
  if (!detail.value) return;
  try {
    await apiJSON(`/api/herds/${detail.value.id}/members`, {
      method: "POST",
      body: JSON.stringify({
        terminal_id: t.id,
        label: t.command,
        ...(attachConversationId.value ? { conversation_id: attachConversationId.value } : {}),
      }),
    });
    showAttach.value = false;
    attachConversationId.value = "";
    await loadDetail(detail.value.id);
    liveAnnouncement.value = `Attached ${t.command}`;
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e);
    if (
      msg.includes("another herd") ||
      msg.includes("confirm") ||
      msg.includes("belongs to another")
    ) {
      pendingMove.value = t;
      return;
    }
    error.value = msg;
  }
}

const pendingMove = ref<LooseTerminal | null>(null);

async function confirmMove() {
  const t = pendingMove.value;
  if (!detail.value || !t) return;
  pendingMove.value = null;
  try {
    await apiJSON(`/api/herds/${detail.value.id}/members`, {
      method: "POST",
      body: JSON.stringify({
        terminal_id: t.id,
        label: t.command,
        confirm_move: true,
        ...(attachConversationId.value ? { conversation_id: attachConversationId.value } : {}),
      }),
    });
    showAttach.value = false;
    attachConversationId.value = "";
    await loadDetail(detail.value.id);
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

async function openAll() {
  if (!detail.value) return;
  await openAllHerd(detail.value.id);
  await loadDetail(detail.value.id);
}

async function openAllHerd(id: string) {
  bulkBusy.value = true;
  bulkResultText.value = "";
  try {
    liveAnnouncement.value = "Opening herd…";
    const result = await apiJSON<{
      items: Array<{ label: string; outcome: string; error?: string }>;
    }>(`/api/herds/${id}/open`, { method: "POST" });
    const counts = { opened: 0, unchanged: 0, failed: 0, closed: 0 };
    for (const it of result.items) {
      if (it.outcome in counts) (counts as Record<string, number>)[it.outcome]++;
    }
    bulkResultText.value = `Opened ${counts.opened}, unchanged ${counts.unchanged}, failed ${counts.failed}`;
    liveAnnouncement.value = bulkResultText.value;
    if (counts.failed) {
      const failed = result.items.filter((i) => i.outcome === "failed");
      error.value = failed.map((f) => `${f.label}: ${f.error || "failed"}`).join("; ");
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    bulkBusy.value = false;
  }
}

async function beginCloseAll() {
  if (!detail.value) return;
  await beginCloseAllHerd(detail.value.id);
}

async function beginCloseAllHerd(id: string) {
  try {
    const preview = await apiJSON<{
      live_terminals: number;
      warnings: Array<{
        member_id: string;
        label: string;
        attention_state: string;
        conversation_slug?: string;
      }>;
    }>(`/api/herds/${id}/close-preview`);
    closeConfirm.value = {
      herdId: id,
      live: preview.live_terminals,
      warnings: preview.warnings || [],
    };
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

async function confirmCloseAll() {
  if (!closeConfirm.value) return;
  const id = closeConfirm.value.herdId;
  closeConfirm.value = null;
  bulkBusy.value = true;
  try {
    const result = await apiJSON<{
      items: Array<{ outcome: string; label: string; error?: string; terminal_id?: string }>;
    }>(`/api/herds/${id}/close`, { method: "POST", body: JSON.stringify({ mode: "graceful" }) });
    const closed = result.items.filter((i) => i.outcome === "closed").length;
    const failed = result.items.filter((i) => i.outcome === "failed").length;
    const unchanged = result.items.filter((i) => i.outcome === "unchanged").length;
    const closedTerminalIds = result.items
      .filter((i) => i.outcome === "closed" || i.outcome === "unchanged")
      .flatMap((i) => (i.terminal_id ? [i.terminal_id] : []));
    if (closedTerminalIds.length > 0) emit("terminals-closed", closedTerminalIds);
    bulkResultText.value = `Closed ${closed}, unchanged ${unchanged}, failed ${failed}`;
    liveAnnouncement.value = bulkResultText.value;
    focusedTermId.value = null;
    if (props.herdId === id) await loadDetail(id);
    else await loadList();
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    bulkBusy.value = false;
  }
}

function onPageKeydown(e: KeyboardEvent) {
  const t = e.target as HTMLElement | null;
  if (!t) return;
  const tag = t.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || t.isContentEditable) return;
  // xterm focus
  if (t.closest(".xterm") || t.closest(".herds-xterm-host")) return;
  if (!detail.value) return;
  const list = filteredMembers.value;
  const idx = list.findIndex((m) => m.id === selectedMemberId.value);
  if (e.key === "ArrowDown") {
    e.preventDefault();
    const next = list[Math.min(list.length - 1, Math.max(0, idx + 1))];
    if (next) selectMember(next);
  } else if (e.key === "ArrowUp") {
    e.preventDefault();
    const prev = list[Math.max(0, idx <= 0 ? 0 : idx - 1)];
    if (prev) selectMember(prev);
  } else if (e.key === "o" && selectedMember.value) {
    e.preventDefault();
    void openMember(selectedMember.value);
  } else if (e.key === "f" && selectedMember.value) {
    e.preventDefault();
    focusMember(selectedMember.value);
  } else if (e.key === "d" && selectedMember.value) {
    e.preventDefault();
    void detachMember(selectedMember.value);
  } else if (e.key === "x" && selectedMember.value) {
    e.preventDefault();
    void closeMember(selectedMember.value);
  } else if (e.key === "?") {
    e.preventDefault();
    liveAnnouncement.value =
      "Shortcuts: arrows select member, o open, f focus, d detach, x close. Selection does not attach the PTY.";
  }
}
</script>

<!-- Vue port of components/TerminalPanel.tsx. A bottom dock of ephemeral
     terminals backed by server-side dtach sessions (so they survive
     conversation switches + reloads). Preserves the .terminal-panel* class
     contract, the action-button titles, and the tab status indicators.

     The EphemeralTerminal type is re-exported here (from terminalTypes.ts) so
     other code can `import type { EphemeralTerminal } from
     "./components/TerminalPanel.vue"` exactly as it imported from the React
     module. The actual xterm.js + websocket lifecycle lives in the
     TerminalInstance.vue child (one per terminal).

     React callback props are mapped to emits:
       onClose                -> emit("close", id)
       onInsertIntoInput      -> emit("insert-into-input", text)
       onAutoFocusConsumed    -> emit("auto-focus-consumed")
       onActiveTerminalExited -> emit("active-terminal-exited")
       onAttached             -> emit("attached", id, termId)
     The presence of an onInsertIntoInput handler in React (which gates the
     insert buttons) is mirrored by the required `canInsertIntoInput` prop. -->
<template>
  <div
    v-if="terminals.length > 0"
    :class="`terminal-panel${minimized ? ' terminal-panel-minimized' : ''}`"
    :style="minimized ? undefined : { height: `${height}px`, flexShrink: 0 }"
  >
    <!-- Resize handle at top — hidden when minimized -->
    <div v-if="!minimized" class="terminal-panel-resize-handle" @mousedown="handleResizeMouseDown">
      <div class="terminal-panel-resize-grip" />
    </div>

    <!-- Tab bar + actions -->
    <div class="terminal-panel-header">
      <!-- Minimize/maximize toggle -->
      <button
        v-tooltip.top="minimized ? 'Expand terminals' : 'Minimize terminals'"
        class="terminal-panel-action-btn"
        :aria-label="minimized ? 'Expand terminals' : 'Minimize terminals'"
        @click="toggleMinimized"
      >
        <ChevronUpIcon v-if="minimized" />
        <ChevronDownIcon v-else />
      </button>

      <div
        class="terminal-panel-tabs"
        role="tablist"
        aria-label="Terminal sessions. Control+Shift+] next, Control+Shift+[ previous, Control+Shift+W close."
      >
        <div
          v-for="(t, idx) in terminals"
          :key="t.id"
          role="tab"
          :tabindex="t.id === activeTabId ? 0 : -1"
          :aria-selected="t.id === activeTabId"
          :aria-label="`Terminal ${idx + 1}: ${tabLabel(t.command)}`"
          :class="`terminal-panel-tab${t.id === activeTabId ? ' terminal-panel-tab-active' : ''}`"
          :title="t.command"
          @click="onTabClick(t.id)"
          @keydown="onTabKeydown($event, t.id, idx)"
        >
          <span
            v-if="statusMap.get(t.id)?.status === 'running'"
            class="terminal-panel-tab-indicator terminal-panel-tab-running"
            >●</span
          >
          <span
            v-if="statusMap.get(t.id)?.status === 'exited' && statusMap.get(t.id)?.exitCode === 0"
            class="terminal-panel-tab-indicator terminal-panel-tab-success"
            >✓</span
          >
          <span
            v-if="statusMap.get(t.id)?.status === 'exited' && statusMap.get(t.id)?.exitCode !== 0"
            class="terminal-panel-tab-indicator terminal-panel-tab-error"
            >✗</span
          >
          <span
            v-if="statusMap.get(t.id)?.status === 'error'"
            class="terminal-panel-tab-indicator terminal-panel-tab-error"
            >✗</span
          >
          <span class="terminal-panel-tab-label">{{ tabLabel(t.command) }}</span>
          <button
            v-tooltip.top="'Close terminal'"
            class="terminal-panel-tab-close"
            :aria-label="`Close terminal ${idx + 1}`"
            tabindex="-1"
            @click.stop="emit('close', t.id)"
          >
            ×
          </button>
        </div>
      </div>

      <!-- Action buttons — hidden when minimized -->
      <div v-if="!minimized" class="terminal-panel-actions">
        <button
          v-tooltip.top="'Copy visible screen'"
          :class="`terminal-panel-action-btn${copyFeedback === 'copyScreen' ? ' terminal-panel-action-btn-feedback' : ''}`"
          aria-label="Copy visible screen"
          @click="copyScreen"
        >
          <CheckIcon v-if="copyFeedback === 'copyScreen'" />
          <CopyIcon v-else />
        </button>
        <button
          v-tooltip.top="'Copy all output (clipboard; enable Screen reader mode to arrow-read the buffer)'"
          :class="`terminal-panel-action-btn${copyFeedback === 'copyAll' ? ' terminal-panel-action-btn-feedback' : ''}`"
          aria-label="Copy all terminal output to clipboard"
          @click="copyAll"
        >
          <CheckIcon v-if="copyFeedback === 'copyAll'" />
          <CopyAllIcon v-else />
        </button>
        <template v-if="canInsertIntoInput">
          <button
            v-tooltip.top="'Insert visible screen into input'"
            :class="`terminal-panel-action-btn${copyFeedback === 'insertScreen' ? ' terminal-panel-action-btn-feedback' : ''}`"
            aria-label="Insert visible screen into input"
            @click="insertScreen"
          >
            <CheckIcon v-if="copyFeedback === 'insertScreen'" />
            <InsertIcon v-else />
          </button>
          <button
            v-tooltip.top="'Insert all output into input'"
            :class="`terminal-panel-action-btn${copyFeedback === 'insertAll' ? ' terminal-panel-action-btn-feedback' : ''}`"
            aria-label="Insert all output into input"
            @click="insertAll"
          >
            <CheckIcon v-if="copyFeedback === 'insertAll'" />
            <InsertAllIcon v-else />
          </button>
        </template>
        <div class="terminal-panel-actions-divider" />
        <button
          v-tooltip.top="'Command history'"
          class="terminal-panel-action-btn"
          aria-label="Command history"
          aria-haspopup="dialog"
          :aria-expanded="historyOpen"
          @click="historyOpen = !historyOpen"
        >
          H
        </button>
        <button
          v-tooltip.top="'Close active terminal'"
          class="terminal-panel-action-btn"
          aria-label="Close active terminal"
          @click="handleCloseActive"
        >
          <CloseIcon />
        </button>
      </div>
    </div>

    <div
      v-if="historyOpen && !minimized"
      class="terminal-panel-history"
      role="dialog"
      aria-label="Terminal command history"
    >
      <div class="terminal-panel-history-header">
        <span>Recent commands</span>
        <button type="button" class="terminal-panel-action-btn" aria-label="Close history" @click="historyOpen = false">
          ×
        </button>
      </div>
      <ul class="terminal-panel-history-list">
        <li v-if="commandHistory.length === 0" class="terminal-panel-history-empty">No history yet.</li>
        <li v-for="(cmd, i) in commandHistory" :key="i">
          <button type="button" class="terminal-panel-history-item" @click="insertHistoryCommand(cmd)">
            {{ cmd }}
          </button>
        </li>
      </ul>
    </div>

    <!-- Terminal content area — hidden (not unmounted) when minimized -->
    <div class="terminal-panel-content" :style="minimized ? { display: 'none' } : undefined">
      <TerminalInstance
        v-for="t in terminals"
        :key="t.id"
        :term="t"
        :is-visible="t.id === activeTabId"
        :is-dark="isDark"
        :conversation-id="conversationId ?? null"
        :workspace-id="workspaceId ?? null"
        :model="t.model || model || null"
        @status-change="handleStatusChange"
        @register="registerXterm"
        @unregister="unregisterXterm"
        @attached="handleAttached"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from "vue";
import type { Terminal } from "@xterm/xterm";
import { isDarkModeActive } from "../../services/theme";
import TerminalInstance from "./TerminalInstance.vue";
import type { TermStatus } from "./terminalHelpers";
import type { EphemeralTerminal } from "./terminalTypes";
import CopyIcon from "./terminalIcons/CopyIcon.vue";
import CopyAllIcon from "./terminalIcons/CopyAllIcon.vue";
import InsertIcon from "./terminalIcons/InsertIcon.vue";
import InsertAllIcon from "./terminalIcons/InsertAllIcon.vue";
import CheckIcon from "./terminalIcons/CheckIcon.vue";
import CloseIcon from "./terminalIcons/CloseIcon.vue";
import ChevronUpIcon from "./terminalIcons/ChevronUpIcon.vue";
import ChevronDownIcon from "./terminalIcons/ChevronDownIcon.vue";
import { announceA11y } from "../../services/a11yAnnouncer";

// Re-export EphemeralTerminal so importers can keep importing it from this
// module (the canonical definition lives in terminalTypes.ts).
export type { EphemeralTerminal } from "./terminalTypes";

const props = defineProps<{
  terminals: EphemeralTerminal[];
  autoFocusId?: string | null;
  // Mirrors the presence of React's onInsertIntoInput callback, which gates
  // the insert buttons. When false the insert actions are not rendered.
  canInsertIntoInput?: boolean;
  // Context surfaced to spawned sessions via SHELLEY_* env vars. Only used on
  // initial spawn; reattaches use the env baked in when the session was
  // created.
  conversationId?: string | null;
  workspaceId?: string | null;
  model?: string | null;
}>();

const emit = defineEmits<{
  (e: "close", id: string): void;
  (e: "insert-into-input", text: string): void;
  (e: "auto-focus-consumed"): void;
  (e: "active-terminal-exited"): void;
  (e: "attached", id: string, termId: string): void;
}>();

const activeTabId = ref<string | null>(null);
const height = ref(300);
const minimized = ref(false);
const copyFeedback = ref<string | null>(null);
const statusMap = ref<Map<string, { status: TermStatus; exitCode: number | null }>>(new Map());
const isResizingRef = { current: false };
const startYRef = { current: 0 };
const startHeightRef = { current: 0 };
const historyOpen = ref(false);
const HISTORY_KEY = "shelley-terminal-command-history";
const commandHistory = ref<string[]>(loadHistory());

function loadHistory(): string[] {
  try {
    const raw = JSON.parse(localStorage.getItem(HISTORY_KEY) || "[]");
    return Array.isArray(raw) ? raw.filter((x): x is string => typeof x === "string").slice(0, 40) : [];
  } catch {
    return [];
  }
}

function pushHistory(command: string) {
  const cmd = command.trim();
  if (!cmd) return;
  const next = [cmd, ...commandHistory.value.filter((c) => c !== cmd)].slice(0, 40);
  commandHistory.value = next;
  try {
    localStorage.setItem(HISTORY_KEY, JSON.stringify(next));
  } catch {
    /* ignore */
  }
}

function insertHistoryCommand(cmd: string) {
  if (!props.canInsertIntoInput) {
    announceA11y("Cannot insert command: composer insert is unavailable.");
    return;
  }
  emit("insert-into-input", cmd);
  historyOpen.value = false;
  announceA11y(`Inserted command into input: ${cmd}`);
}

// Detect dark mode
const isDark = ref(isDarkModeActive());
let observer: MutationObserver | null = null;
onMounted(() => {
  observer = new MutationObserver(() => {
    isDark.value = isDarkModeActive();
  });
  observer.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["class"],
  });
  window.addEventListener("keydown", onPanelShortcut, true);
});
onUnmounted(() => {
  observer?.disconnect();
  window.removeEventListener("keydown", onPanelShortcut, true);
});

// Auto-select newest tab when a new terminal is added (React effect on
// [terminals.length]). immediate: true so a mount with pre-existing terminals
// (e.g. after an HMR reload or remount) still selects an active tab; otherwise
// activeTabId stays null and every terminal renders hidden.
watch(
  () => props.terminals.length,
  (len, previousLength) => {
    if (len > 0) {
      const lastTerminal = props.terminals[props.terminals.length - 1];
      activeTabId.value = lastTerminal.id;
      minimized.value = false; // expand when a new terminal arrives
      if (len > (previousLength ?? 0)) {
        pushHistory(lastTerminal.command);
        announceA11y(`Terminal opened for ${lastTerminal.command}.`);
      }
    } else {
      activeTabId.value = null;
    }
  },
  { immediate: true },
);

// If active tab got closed, switch to the last remaining (React effect on
// [terminals, activeTabId]).
watch(
  () => [props.terminals, activeTabId.value] as const,
  () => {
    if (activeTabId.value && !props.terminals.find((t) => t.id === activeTabId.value)) {
      if (props.terminals.length > 0) {
        activeTabId.value = props.terminals[props.terminals.length - 1].id;
      } else {
        activeTabId.value = null;
      }
    }
  },
);

function handleStatusChange(id: string, status: TermStatus, exitCode: number | null) {
  const prev = statusMap.value;
  const next = new Map(prev);
  const existing = next.get(id);
  // Don't overwrite exit status with ws.onclose
  if (existing && existing.status === "exited" && status === "exited") {
    return;
  }
  next.set(id, {
    status,
    exitCode: exitCode ?? existing?.exitCode ?? null,
  });
  statusMap.value = next;
  const terminal = props.terminals.find((item) => item.id === id);
  const command = terminal?.command || "terminal session";
  if (status === "running" && existing?.status !== "running") {
    announceA11y(`${terminal?.termId ? "Terminal attached" : "Terminal connected"}: ${command}.`);
  } else if (status === "exited" && existing?.status !== "exited") {
    announceA11y(
      exitCode === null
        ? `Terminal disconnected: ${command}.`
        : `Terminal exited with code ${exitCode}: ${command}.`,
      exitCode && exitCode !== 0 ? "assertive" : "polite",
    );
  } else if (status === "error" && existing?.status !== "error") {
    announceA11y(`Terminal error: ${command}.`, "assertive");
  }
}

function handleAttached(id: string, termId: string) {
  emit("attached", id, termId);
  const terminal = props.terminals.find((item) => item.id === id);
  announceA11y(`Terminal session attached: ${terminal?.command || termId}.`);
}

// Resize drag
function handleResizeMouseDown(e: MouseEvent) {
  e.preventDefault();
  isResizingRef.current = true;
  startYRef.current = e.clientY;
  startHeightRef.current = height.value;

  const handleMouseMove = (ev: MouseEvent) => {
    if (!isResizingRef.current) return;
    // Dragging up increases height
    const delta = startYRef.current - ev.clientY;
    height.value = Math.max(80, Math.min(800, startHeightRef.current + delta));
  };

  const handleMouseUp = () => {
    isResizingRef.current = false;
    document.removeEventListener("mousemove", handleMouseMove);
    document.removeEventListener("mouseup", handleMouseUp);
  };

  document.addEventListener("mousemove", handleMouseMove);
  document.addEventListener("mouseup", handleMouseUp);
}

function showFeedback(type: string) {
  copyFeedback.value = type;
  setTimeout(() => (copyFeedback.value = null), 1500);
}

// Registry of xterm instances by terminal id.
const xtermRegistry = new Map<string, Terminal>();
function registerXterm(id: string, xterm: Terminal) {
  xtermRegistry.set(id, xterm);
}
function unregisterXterm(id: string) {
  xtermRegistry.delete(id);
}

// Auto-focus terminal when autoFocusId is set (React effect on
// [autoFocusId, onAutoFocusConsumed]).
watch(
  () => props.autoFocusId,
  (autoFocusId) => {
    if (!autoFocusId) return;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    let attempt = 0;
    const tryFocus = () => {
      if (cancelled) return;
      const xterm = xtermRegistry.get(autoFocusId);
      if (xterm) {
        activeTabId.value = autoFocusId;
        minimized.value = false; // expand when focusing a terminal
        // Double-rAF to ensure we're past any keyup/form events that might steal focus
        requestAnimationFrame(() => {
          requestAnimationFrame(() => {
            xterm.focus();
          });
        });
        emit("auto-focus-consumed");
        return;
      }
      if (++attempt < 10) {
        timer = setTimeout(tryFocus, 50);
      }
    };
    // Small initial delay to let the form submit / keyup events settle
    timer = setTimeout(tryFocus, 50);
    // Cleanup when autoFocusId changes again.
    const stop = watch(
      () => props.autoFocusId,
      () => {
        cancelled = true;
        clearTimeout(timer);
        stop();
      },
    );
  },
);

// Restore focus to message input when the active terminal exits (React effect
// on [activeTabId, statusMap, onActiveTerminalExited]).
const prevActiveStatusRef = {
  current: { tabId: null as string | null, status: undefined as TermStatus | undefined },
};
watch(
  () => [activeTabId.value, statusMap.value] as const,
  () => {
    if (!activeTabId.value) return;
    const info = statusMap.value.get(activeTabId.value);
    const prev = prevActiveStatusRef.current;
    // Only trigger on status transition within the same tab
    const wasRunning = prev.tabId === activeTabId.value && prev.status === "running";
    prevActiveStatusRef.current = { tabId: activeTabId.value, status: info?.status };
    if (wasRunning && (info?.status === "exited" || info?.status === "error")) {
      emit("active-terminal-exited");
    }
  },
);

function getBufferText(mode: "screen" | "all"): string {
  if (!activeTabId.value) return "";
  const xterm = xtermRegistry.get(activeTabId.value);
  if (!xterm) return "";

  const lines: string[] = [];
  const buffer = xterm.buffer.active;

  if (mode === "screen") {
    const startRow = buffer.viewportY;
    for (let i = 0; i < xterm.rows; i++) {
      const line = buffer.getLine(startRow + i);
      if (line) lines.push(line.translateToString(true));
    }
  } else {
    for (let i = 0; i < buffer.length; i++) {
      const line = buffer.getLine(i);
      if (line) lines.push(line.translateToString(true));
    }
  }
  return lines.join("\n").trimEnd();
}

function copyScreen() {
  const text = getBufferText("screen");
  void navigator.clipboard.writeText(text);
  showFeedback("copyScreen");
  const n = text ? text.split("\n").length : 0;
  announceA11y(
    n
      ? `Copied ${n} visible line${n === 1 ? "" : "s"} to clipboard.`
      : "Nothing to copy from the visible screen.",
  );
}
function copyAll() {
  const text = getBufferText("all");
  void navigator.clipboard.writeText(text);
  showFeedback("copyAll");
  const n = text ? text.split("\n").length : 0;
  // Clipboard is not navigable with VO left/right; announce so the action is audible.
  announceA11y(
    n
      ? `Copied ${n} line${n === 1 ? "" : "s"} of terminal output to clipboard. Paste elsewhere to read, or use Screen reader mode and arrow through the terminal rows.`
      : "Nothing to copy from the terminal.",
  );
}
function insertScreen() {
  if (props.canInsertIntoInput) {
    const text = getBufferText("screen");
    emit("insert-into-input", text);
    showFeedback("insertScreen");
    const n = text ? text.split("\n").length : 0;
    announceA11y(n ? `Inserted ${n} visible lines into the message input.` : "Nothing to insert.");
  }
}
function insertAll() {
  if (props.canInsertIntoInput) {
    const text = getBufferText("all");
    emit("insert-into-input", text);
    showFeedback("insertAll");
    const n = text ? text.split("\n").length : 0;
    announceA11y(
      n ? `Inserted ${n} lines into the message input.` : "Nothing to insert.",
    );
  }
}

function handleCloseActive() {
  if (!activeTabId.value) return;
  const id = activeTabId.value;
  const label = tabLabel(props.terminals.find((t) => t.id === id)?.command || "terminal");
  emit("close", id);
  announceA11y(`Closed terminal: ${label}.`);
}

function toggleMinimized() {
  minimized.value = !minimized.value;
  announceA11y(minimized.value ? "Terminals minimized." : "Terminals expanded.");
}

function onTabClick(id: string) {
  selectTerminal(id);
}

function selectTerminal(id: string, announce = true) {
  activeTabId.value = id;
  if (minimized.value) minimized.value = false;
  const term = props.terminals.find((t) => t.id === id);
  const idx = props.terminals.findIndex((t) => t.id === id);
  if (announce && term) {
    announceA11y(
      `Terminal ${idx + 1} of ${props.terminals.length}: ${tabLabel(term.command)}.`,
    );
  }
  // Focus the shell after the tab is shown.
  requestAnimationFrame(() => {
    xtermRegistry.get(id)?.focus();
  });
}

function switchTerminal(delta: number) {
  if (props.terminals.length === 0) return;
  const cur = props.terminals.findIndex((t) => t.id === activeTabId.value);
  const base = cur < 0 ? 0 : cur;
  const next = (base + delta + props.terminals.length) % props.terminals.length;
  selectTerminal(props.terminals[next].id);
}

function onTabKeydown(e: KeyboardEvent, id: string, idx: number) {
  if (e.key === "ArrowRight" || e.key === "ArrowDown") {
    e.preventDefault();
    switchTerminal(1);
    return;
  }
  if (e.key === "ArrowLeft" || e.key === "ArrowUp") {
    e.preventDefault();
    switchTerminal(-1);
    return;
  }
  if (e.key === "Home") {
    e.preventDefault();
    selectTerminal(props.terminals[0].id);
    return;
  }
  if (e.key === "End") {
    e.preventDefault();
    selectTerminal(props.terminals[props.terminals.length - 1].id);
    return;
  }
  if (e.key === "Delete" || e.key === "Backspace") {
    e.preventDefault();
    emit("close", id);
    announceA11y(`Closed terminal ${idx + 1}.`);
    return;
  }
  if (e.key === "Enter" || e.key === " ") {
    e.preventDefault();
    selectTerminal(id);
  }
}

// Global chords while any terminal exists. Shift+Ctrl avoids clobbering shell
// readline (Ctrl+W = kill-word, Ctrl+[ = esc, etc.).
function onPanelShortcut(e: KeyboardEvent) {
  if (props.terminals.length === 0) return;
  if (e.type !== "keydown") return;
  // Need Ctrl (or Meta on Mac for consistency we accept both) + Shift.
  if (!e.shiftKey || !(e.ctrlKey || e.metaKey) || e.altKey) return;

  const key = e.key;
  // Next / previous tab
  if (key === "]" || key === "}") {
    e.preventDefault();
    e.stopPropagation();
    switchTerminal(1);
    return;
  }
  if (key === "[" || key === "{") {
    e.preventDefault();
    e.stopPropagation();
    switchTerminal(-1);
    return;
  }
  // Close active
  if (key === "w" || key === "W") {
    e.preventDefault();
    e.stopPropagation();
    handleCloseActive();
    return;
  }
  // Minimize / expand
  if (key === "m" || key === "M") {
    e.preventDefault();
    e.stopPropagation();
    toggleMinimized();
    return;
  }
  // Jump to terminal 1–9
  if (key >= "1" && key <= "9") {
    const n = parseInt(key, 10) - 1;
    if (n < props.terminals.length) {
      e.preventDefault();
      e.stopPropagation();
      selectTerminal(props.terminals[n].id);
    }
  }
}

// Refit terminals when un-minimizing by nudging the container to trigger
// ResizeObserver (React effect on [minimized, activeTabId]).
const wasMinimizedRef = { current: minimized.value };
watch(
  () => [minimized.value, activeTabId.value] as const,
  () => {
    const wasMinimized = wasMinimizedRef.current;
    wasMinimizedRef.current = minimized.value;
    if (wasMinimized && !minimized.value && activeTabId.value) {
      const timer = setTimeout(() => {
        const el = document.querySelector(`[data-terminal-id="${activeTabId.value}"]`);
        if (el) {
          (el as HTMLElement).style.height = "99.9%";
          requestAnimationFrame(() => {
            (el as HTMLElement).style.height = "100%";
          });
        }
      }, 30);
      // No explicit cleanup needed; the timer is short-lived.
      void timer;
    }
  },
);

// Truncate command for tab label
function tabLabel(cmd: string): string {
  // Show first word or first 30 chars
  const firstWord = cmd.split(/\s+/)[0];
  if (firstWord.length > 30) return firstWord.substring(0, 27) + "...";
  return firstWord;
}
</script>

<!-- Vue port of the TerminalInstanceWithRegistry inner component of
     components/TerminalPanel.tsx. Owns a single xterm.js instance + its
     dtach-backed websocket. The Terminal is created imperatively in
     onMounted on the container template ref and disposed in onUnmounted
     (mirrors the React effect + cleanup). The xterm instance is surfaced to
     the parent via the "register"/"unregister" emits (the React
     onRegister/onUnregister callbacks).

     shelley-a11y: a plain-text buffer mirror sits beside xterm so VO can
     arrow/Tab through full command output (ls, etc.). xterm's live region
     only announces short bursts and traps Tab in the helper textarea. -->
<template>
  <div
    class="terminal-instance"
    :data-terminal-id="term.id"
    :style="{
      display: isVisible ? 'flex' : 'none',
      backgroundColor: isDark ? '#1a1b26' : '#f8f9fa',
    }"
  >
    <div ref="containerRef" class="terminal-instance-xterm" aria-label="Terminal shell input" />
    <!-- Plain text mirror of the buffer: navigable with VO left/right and Tab. -->
    <pre
      ref="outputLogRef"
      class="terminal-instance-a11y-log"
      tabindex="0"
      role="log"
      aria-live="off"
      aria-atomic="false"
      aria-label="Terminal output (read-only). Tab returns to shell input; Escape leaves the terminal."
      @keydown="onOutputLogKeydown"
      >{{ bufferText || "(no output yet)" }}</pre
    >
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from "vue";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import type { EphemeralTerminal } from "./terminalTypes";
import {
  getTerminalTheme,
  base64ToUint8Array,
  recordTerminalLiveOutput,
  shouldPauseTerminalLiveOutput,
  TERMINAL_LIVE_OUTPUT_LIMIT_MS,
  type TerminalLiveOutputWindow,
  type TermStatus,
} from "./terminalHelpers";
import { announceA11y } from "../../services/a11yAnnouncer";

const props = defineProps<{
  term: EphemeralTerminal;
  isVisible: boolean;
  isDark: boolean;
  conversationId?: string | null;
  workspaceId?: string | null;
  model?: string | null;
}>();

const emit = defineEmits<{
  // status-change: id, status, exitCode (React onStatusChange)
  (e: "status-change", id: string, status: TermStatus, exitCode: number | null): void;
  // register/unregister: id, xterm instance (React onRegister/onUnregister)
  (e: "register", id: string, xterm: Terminal): void;
  (e: "unregister", id: string): void;
  // attached: id, termId (React onAttached)
  (e: "attached", id: string, termId: string): void;
}>();

const containerRef = ref<HTMLDivElement | null>(null);
const outputLogRef = ref<HTMLPreElement | null>(null);
const bufferText = ref("");
let xtermInst: Terminal | null = null;
let fitAddon: FitAddon | null = null;
let ws: WebSocket | null = null;
let ro: ResizeObserver | null = null;
let handlePointerDown: ((e: PointerEvent) => void) | null = null;
let handleShellFocus: (() => void) | null = null;
let handleShellBlur: (() => void) | null = null;
let mirrorTimer: ReturnType<typeof setTimeout> | null = null;
let liveOutputTimer: ReturnType<typeof setTimeout> | null = null;
let liveOutputWindow: TerminalLiveOutputWindow | null = null;
let liveOutputPaused = true;

function stopTerminalLiveOutput(announce: boolean) {
  if (!xtermInst) return;
  liveOutputPaused = true;
  if (liveOutputTimer) clearTimeout(liveOutputTimer);
  liveOutputTimer = null;
  liveOutputWindow = null;
  // Disposing xterm's accessibility manager prevents it from accumulating a
  // huge silent live-region value that would all be read when resumed. The
  // separate output log remains available for manual review.
  xtermInst.options.screenReaderMode = false;
  if (announce) {
    announceA11y(
      "Terminal live output paused after 20 seconds. Refocus the shell to resume, or Tab to Terminal output to read.",
    );
  }
}

function checkTerminalLiveOutput() {
  if (liveOutputWindow && shouldPauseTerminalLiveOutput(liveOutputWindow, Date.now())) {
    stopTerminalLiveOutput(true);
    return;
  }
  liveOutputTimer = null;
  liveOutputWindow = null;
}

function trackTerminalLiveOutput() {
  if (liveOutputPaused) return;
  const previousStartedAt = liveOutputWindow?.startedAt;
  liveOutputWindow = recordTerminalLiveOutput(liveOutputWindow, Date.now());
  if (previousStartedAt === liveOutputWindow.startedAt) return;
  if (liveOutputTimer) clearTimeout(liveOutputTimer);
  liveOutputTimer = setTimeout(checkTerminalLiveOutput, TERMINAL_LIVE_OUTPUT_LIMIT_MS);
}

function resumeTerminalLiveOutput() {
  if (!liveOutputPaused || !xtermInst) return;
  liveOutputPaused = false;
  liveOutputWindow = null;
  xtermInst.options.screenReaderMode = true;
  announceA11y("Terminal live output resumed.");
}

function readBufferAll(xterm: Terminal): string {
  const lines: string[] = [];
  const buffer = xterm.buffer.active;
  for (let i = 0; i < buffer.length; i++) {
    const line = buffer.getLine(i);
    if (line) lines.push(line.translateToString(true));
  }
  return lines.join("\n").replace(/\s+$/, "");
}

function scheduleMirrorRefresh(xterm: Terminal) {
  if (mirrorTimer) clearTimeout(mirrorTimer);
  // Debounce: shell bursts (ls) arrive as many small websocket frames.
  mirrorTimer = setTimeout(() => {
    mirrorTimer = null;
    const text = readBufferAll(xterm);
    bufferText.value = text;
  }, 80);
}

function focusOutputLog() {
  outputLogRef.value?.focus();
  announceA11y("Terminal output. Arrow to read. Tab returns to shell.");
}

function focusShell() {
  xtermInst?.focus();
  announceA11y("Shell input.");
}

function leaveTerminalForward() {
  const panel = containerRef.value?.closest(".terminal-panel");
  const candidates = Array.from(
    document.querySelectorAll<HTMLElement>(
      'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex="0"]',
    ),
  ).filter((el) => el.offsetParent !== null || el === document.activeElement);

  if (panel) {
    const after = candidates.find(
      (el) =>
        !panel.contains(el) &&
        !!(panel.compareDocumentPosition(el) & Node.DOCUMENT_POSITION_FOLLOWING),
    );
    if (after) {
      after.focus();
      announceA11y("Left terminal.");
      return;
    }
  }
  const input = document.querySelector<HTMLElement>('[data-testid="message-input"]');
  input?.focus();
  announceA11y(input ? "Message input." : "Left terminal.");
}

function onOutputLogKeydown(e: KeyboardEvent) {
  if (e.key === "Tab" && !e.shiftKey && !e.metaKey && !e.ctrlKey && !e.altKey) {
    e.preventDefault();
    focusShell();
    return;
  }
  if (e.key === "Tab" && e.shiftKey) {
    e.preventDefault();
    focusShell();
    return;
  }
  if (e.key === "Escape") {
    e.preventDefault();
    leaveTerminalForward();
  }
}

onMounted(() => {
  if (!containerRef.value) return;

  // Enable xterm's live region only while the shell has focus. A hidden or
  // background terminal must never interrupt navigation elsewhere in Shelley.
  // The full buffer remains available in the explicit read-only output log.
  const xterm = new Terminal({
    cursorBlink: true,
    fontSize: 14,
    fontFamily: 'Consolas, "Liberation Mono", Menlo, Courier, monospace',
    theme: getTerminalTheme(props.isDark),
    scrollback: 10000,
    screenReaderMode: false,
    // Kitty keyboard protocol — clients opt in via `CSI = u` so this is safe to leave on.
    vtExtensions: { kittyKeyboard: true },
  } as ConstructorParameters<typeof Terminal>[0]);
  xtermInst = xterm;

  // Ensure control key combinations (like Ctrl-B for tmux) are passed
  // through to the terminal and not intercepted by the browser.
  xterm.attachCustomKeyEventHandler((e: KeyboardEvent) => {
    if (e.type !== "keydown") return true;

    // Tab: leave shell input → output log (do not send tab to shell for a11y).
    // Shift+Tab: same for now (output log is the browse surface).
    // Shell tab-completion: use Ctrl+I (same as Tab to the PTY) if needed.
    if (e.key === "Tab" && !e.metaKey && !e.ctrlKey && !e.altKey) {
      e.preventDefault();
      e.stopPropagation();
      focusOutputLog();
      return false;
    }

    // Escape: leave the terminal panel entirely.
    if (e.key === "Escape" && !e.metaKey && !e.ctrlKey && !e.altKey) {
      e.preventDefault();
      e.stopPropagation();
      leaveTerminalForward();
      return false;
    }

    // Ctrl+I still sends tab to the shell for completion.
    if (e.ctrlKey && !e.shiftKey && !e.altKey && !e.metaKey && (e.key === "i" || e.key === "I")) {
      e.preventDefault();
      return true;
    }

    // Cmd/Ctrl+A: never let the browser select the whole Shelley page
    // (Chrome + VO "select all" was painting the chat UI and stealing focus).
    // - Cmd+A (macOS): select the xterm buffer only, for copy.
    // - Ctrl+A (no meta): pass through as ^A (readline beginning-of-line).
    if ((e.key === "a" || e.key === "A") && (e.metaKey || e.ctrlKey) && !e.altKey && !e.shiftKey) {
      e.preventDefault();
      e.stopPropagation();
      if (e.metaKey) {
        window.getSelection()?.removeAllRanges();
        xterm.selectAll();
        return false;
      }
      // bare Ctrl+A → shell
      return true;
    }

    // Cmd+C with an xterm selection: copy buffer text, not the DOM page.
    if ((e.key === "c" || e.key === "C") && e.metaKey && !e.altKey && !e.shiftKey) {
      const sel = xterm.getSelection();
      if (sel) {
        e.preventDefault();
        e.stopPropagation();
        void navigator.clipboard.writeText(sel);
        return false;
      }
    }

    // Allow Ctrl+Shift+C / Ctrl+Shift+V for copy/paste
    if (e.ctrlKey && e.shiftKey && (e.key === "C" || e.key === "V")) {
      return false; // Let browser handle it
    }
    // For all Ctrl+<key> combos (e.g. Ctrl-B for tmux prefix),
    // prevent the browser default and let xterm handle it.
    if (e.ctrlKey && !e.shiftKey && !e.altKey && !e.metaKey) {
      e.preventDefault();
      return true; // Let xterm process it
    }
    return true;
  });

  fitAddon = new FitAddon();
  xterm.loadAddon(fitAddon);
  xterm.loadAddon(new WebLinksAddon());

  xterm.open(containerRef.value);
  fitAddon.fit();
  emit("register", props.term.id, xterm);

  handleShellFocus = resumeTerminalLiveOutput;
  handleShellBlur = () => stopTerminalLiveOutput(false);
  xterm.textarea?.addEventListener("focus", handleShellFocus);
  xterm.textarea?.addEventListener("blur", handleShellBlur);

  // Keep the plain-text mirror in sync whenever the viewport paints.
  xterm.onRender(() => scheduleMirrorRefresh(xterm));
  xterm.onWriteParsed(() => scheduleMirrorRefresh(xterm));

  // Mobile soft-keyboard fix: on touch devices the xterm helper textarea
  // can't be focused by tapping (it has pointer-events: none so the
  // viewport remains scrollable). Listen for pointerdown inside the
  // terminal area and focus xterm programmatically — this happens inside
  // a user gesture, which is what iOS/Android require to open the keyboard.
  handlePointerDown = (e: PointerEvent) => {
    // Only handle touch — pen/stylus shouldn't auto-summon the OSK, and
    // mouse already focuses xterm through its own handlers.
    if (e.pointerType !== "touch") return;
    xterm.focus();
  };
  containerRef.value.addEventListener("pointerdown", handlePointerDown);

  // Show the command as a banner so users can see and copy/paste what they
  // ran. Written client-side on every attach (the xterm buffer is fresh on
  // each mount, so there's no duplication).
  xterm.write(`\x1b[2m$ ${props.term.command}\x1b[0m\r\n`);
  scheduleMirrorRefresh(xterm);

  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  // Reattach and spawn are deliberately separate. Sending a command with a
  // persistent session id would let a stale tab resurrect a terminal that the
  // user explicitly closed.
  const params = new URLSearchParams();
  if (props.term.termId) {
    params.set("term_id", props.term.termId);
  } else {
    params.set("cmd", props.term.command);
    params.set("cwd", props.term.cwd);
  }
  if (props.conversationId) params.set("conversation_id", props.conversationId);
  if (props.workspaceId) params.set("workspace_id", props.workspaceId);
  if (props.model) params.set("model", props.model);
  const wsUrl = `${protocol}//${window.location.host}/api/exec-ws?${params.toString()}`;
  ws = new WebSocket(wsUrl);
  const socket = ws;

  socket.onopen = () => {
    socket.send(JSON.stringify({ type: "init", cols: xterm.cols, rows: xterm.rows }));
    emit("status-change", props.term.id, "running", null);
  };

  socket.onmessage = (event) => {
    try {
      const msg = JSON.parse(event.data);
      if (msg.type === "output" && msg.data) {
        trackTerminalLiveOutput();
        xterm.write(base64ToUint8Array(msg.data));
        scheduleMirrorRefresh(xterm);
      } else if (msg.type === "attached" && msg.term_id) {
        emit("attached", props.term.id, msg.term_id);
      } else if (msg.type === "exit") {
        const code = parseInt(msg.data, 10) || 0;
        const color = code === 0 ? "32" : "31";
        xterm.write(
          `\r\n\x1b[2;${color}m${props.term.command} completed with exit code ${code}\x1b[0m\r\n`,
        );
        scheduleMirrorRefresh(xterm);
        emit("status-change", props.term.id, "exited", code);
      } else if (msg.type === "error") {
        xterm.write(`\r\n\x1b[31mError: ${msg.data}\x1b[0m\r\n`);
        scheduleMirrorRefresh(xterm);
        emit("status-change", props.term.id, "error", null);
      }
    } catch (err) {
      console.error("Failed to parse terminal message:", err);
    }
  };

  socket.onerror = (event) => console.error("WebSocket error:", event);
  socket.onclose = () => {
    emit("status-change", props.term.id, "exited", null);
  };

  xterm.onData((data) => {
    if (socket.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: "input", data }));
    }
  });

  ro = new ResizeObserver(() => {
    if (!fitAddon) return;
    fitAddon.fit();
    if (socket.readyState === WebSocket.OPEN && xtermInst) {
      socket.send(
        JSON.stringify({
          type: "resize",
          cols: xtermInst.cols,
          rows: xtermInst.rows,
        }),
      );
    }
  });
  ro.observe(containerRef.value);
});

onUnmounted(() => {
  ro?.disconnect();
  if (mirrorTimer) clearTimeout(mirrorTimer);
  if (liveOutputTimer) clearTimeout(liveOutputTimer);
  if (handlePointerDown && containerRef.value) {
    containerRef.value.removeEventListener("pointerdown", handlePointerDown);
  }
  if (handleShellFocus) xtermInst?.textarea?.removeEventListener("focus", handleShellFocus);
  if (handleShellBlur) xtermInst?.textarea?.removeEventListener("blur", handleShellBlur);
  ws?.close();
  xtermInst?.dispose();
  emit("unregister", props.term.id);
});

// Update theme (React effect on [isDark]).
watch(
  () => props.isDark,
  (dark) => {
    if (xtermInst) {
      xtermInst.options.theme = getTerminalTheme(dark);
    }
  },
);

// Refit when visibility changes (React effect on [isVisible]).
watch(
  () => props.isVisible,
  (visible) => {
    if (visible && fitAddon) {
      setTimeout(() => fitAddon?.fit(), 20);
      if (xtermInst) scheduleMirrorRefresh(xtermInst);
    }
  },
);
</script>

<!-- Vue port of components/MarkdownContent.tsx. Renders sanitized markdown HTML
     via v-html. The pure pipeline lives in utils/markdownRender.ts.
     Preserves the .markdown-content .break-words container contract.

     With `commentable`, images in the rendered markdown open the image
     annotation view when clicked (or activated from the keyboard). That is
     opt-in because the view is hosted by ChatInterface: the export page renders
     the same markdown with no host, and an image that announces itself as a
     button and then does nothing is worse than a plain image. -->
<template>
  <div
    ref="containerRef"
    class="markdown-content break-words"
    @click="onImageActivate"
    @keydown="onKeydown"
    v-html="html"
  ></div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from "vue";
import { COMMENT_ICON } from "../../utils/icons";
import { renderMarkdownToSafeHTML } from "../../utils/markdownRender";
import { announceA11y } from "../../services/a11yAnnouncer";
import { perfWrap } from "../../utils/perf";
import { handleImageCommentClick, openImageComment } from "../composables/imageComment";

const props = defineProps<{
  text: string;
  // When set, local-path markdown images (relative or absolute file paths) are
  // rewritten to the per-message file endpoint and rendered. Without it we
  // cannot authorize a local file, so such images are dropped.
  messageId?: string;
  // Make images here open the annotation view. Only for hosts rendered inside
  // ChatInterface, which owns that view. Read as a fixed property of the host,
  // not something toggled on a mounted instance: turning it off would need the
  // wrappers below torn down again.
  commentable?: boolean;
  // Object whose lifetime bounds the render cache entry (the owning,
  // immutable Message). Omitted by callers whose text isn't tied to a
  // stable, immutable object — the streaming preview, the distillation
  // preview, export — which always re-render.
  cacheOwner?: object;
  // Distinguishes multiple markdown runs within the same cacheOwner (e.g. a
  // message with several text blocks split by tool calls). Required
  // whenever cacheOwner is set.
  runKey?: string;
}>();

const containerRef = ref<HTMLDivElement | null>(null);

const html = computed(
  perfWrap("markdown.render", () =>
    renderMarkdownToSafeHTML(
      props.text,
      props.messageId,
      props.cacheOwner && props.runKey !== undefined
        ? { owner: props.cacheOwner, runKey: props.runKey }
        : undefined,
    ),
  ),
);

// --- a11y: code block navigation (from HEAD) ---
function codeBlocks(): HTMLElement[] {
  return Array.from(containerRef.value?.querySelectorAll<HTMLElement>("pre > code") ?? []);
}

function prepareCodeBlocks() {
  const blocks = codeBlocks();
  blocks.forEach((block, index) => {
    const language = Array.from(block.classList)
      .find((name) => name.startsWith("language-"))
      ?.slice("language-".length);
    const lineCount = block.textContent?.split("\n").length ?? 0;
    block.tabIndex = 0;
    block.setAttribute(
      "aria-label",
      `${language ? `${language} ` : ""}code block ${index + 1} of ${blocks.length}, ${lineCount} ${lineCount === 1 ? "line" : "lines"}`,
    );
    block.setAttribute(
      "aria-keyshortcuts",
      "Alt+ArrowDown Alt+ArrowUp Control+Shift+C Meta+Shift+C",
    );
  });
}

function focusCodeBlock(direction: 1 | -1) {
  const blocks = codeBlocks();
  if (blocks.length === 0) return;
  const current =
    document.activeElement instanceof HTMLElement ? blocks.indexOf(document.activeElement) : -1;
  const next =
    current === -1
      ? direction === 1
        ? 0
        : blocks.length - 1
      : (current + direction + blocks.length) % blocks.length;
  blocks[next].focus();
  announceA11y(`Code block ${next + 1} of ${blocks.length}.`);
}

function onKeydown(event: KeyboardEvent) {
  // First handle image comment activation (upstream)
  const imgTarget = event.target;
  if (imgTarget instanceof HTMLImageElement && isCommentable(imgTarget)) {
    if (event.key === "Enter" || event.key === " ") {
      if ((event as KeyboardEvent).repeat) return;
      event.preventDefault();
      openImageComment({ src: imgTarget.src });
      return;
    }
  }
  // Then handle code block navigation (a11y)
  if (event.altKey && event.key === "ArrowDown") {
    event.preventDefault();
    focusCodeBlock(1);
    return;
  }
  if (event.altKey && event.key === "ArrowUp") {
    event.preventDefault();
    focusCodeBlock(-1);
    return;
  }
  if ((event.ctrlKey || event.metaKey) && event.shiftKey && event.key.toLowerCase() === "c") {
    const target =
      event.target instanceof HTMLElement ? event.target.closest<HTMLElement>("pre > code") : null;
    if (!target) return;
    event.preventDefault();
    navigator.clipboard.writeText(target.textContent ?? "").then(
      () => announceA11y("Code block copied."),
      () => announceA11y("Could not copy code block.", "assertive"),
    );
  }
}

onMounted(prepareCodeBlocks);
watch(html, () => nextTick(prepareCodeBlocks));

// --- upstream: commentable images ---
function isCommentable(img: HTMLImageElement): boolean {
  return !!props.commentable && !img.parentElement?.closest("a");
}

watch(
  [html, containerRef],
  () => {
    const root = containerRef.value;
    if (!root || !props.commentable) return;
    for (const img of root.querySelectorAll("img")) {
      if (!isCommentable(img as HTMLImageElement) || img.closest(".commentable-image-link")) continue;
      img.setAttribute("role", "button");
      img.setAttribute("tabindex", "0");
      img.classList.add("commentable-image");
      const wrap = document.createElement("span");
      wrap.className = "commentable-image-link";
      img.replaceWith(wrap);
      wrap.append(img, badge());
    }
  },
  { flush: "post", immediate: true },
);

function badge(): HTMLElement {
  const el = document.createElement("span");
  el.className = "commentable-image-badge";
  el.setAttribute("aria-hidden", "true");
  el.innerHTML = `${COMMENT_ICON} Comment`;
  return el;
}

function onImageActivate(e: MouseEvent | KeyboardEvent) {
  const img = e.target;
  if (!(img instanceof HTMLImageElement) || !isCommentable(img)) return;
  if (e instanceof KeyboardEvent) {
    if (e.repeat || (e.key !== "Enter" && e.key !== " ")) return;
    e.preventDefault();
    openImageComment({ src: img.src });
    return;
  }
  handleImageCommentClick(e, { src: img.src });
}
</script>

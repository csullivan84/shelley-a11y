# Shelley: a coding agent for exe.dev

> **This repository is the [VaxTui](https://github.com/csullivan84/VaxTui) fork** —
> VoiceOver-first Shelley with accessibility hardening.
> Upstream lives at [boldsoftware/shelley](https://github.com/boldsoftware/shelley).
> See [FORK.md](./FORK.md). The binary remains `shelley`.

Shelley is a mobile-friendly, web-based, multi-conversation, multi-modal,
multi-model, single-user coding agent built for but not exclusive to
[exe.dev](https://exe.dev/). It does not come with authorization or sandboxing:
bring your own.

## VaxTui enhancements

Subscription-backed providers such as ChatGPT and OpenCode are discovered and
can be imported during onboarding from their existing local credentials.

Herds group related, long-running terminals into durable work packs with shared lifecycle controls.
VaxTui makes Herds accessible through semantic controls, status announcements, and predictable focus.

- VoiceOver-first navigation, reliable focus escape from the workspace editor,
  descriptive path controls, and text diffs by default.
- Durable named workspaces with short URL slugs, a unified file tree and editor,
  and folder selection that restores the intended workspace.
- Terminals owned by a workspace or herd, with stale-session pruning and bulk
  cleanup instead of conversation-local terminal clutter.
- Compact conversation summaries that reveal full transcripts only after a
  conversation is opened.
- Manual editor saves by default, including an accessible dirty-file prompt
  before closing an unsaved tab.
- Resilient multi-provider onboarding keeps valid providers available when
  another provider cannot be discovered.

*Mobile-friendly* because ideas can come any time.

*Web-based*, because terminal-based scroll back is punishment for shoplifting in some countries.

*Multi-modal* because screenshots, charts, and graphs are necessary, not to mention delightful.

*Multi-model* to benefit from all the innovation going on.

*Single-user* because it makes sense to bring the agent to the compute.

# Installation

## Pre-Built Binaries (macOS/Linux)

```bash
curl -Lo shelley "https://github.com/boldsoftware/shelley/releases/latest/download/shelley_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')" && chmod +x shelley
```

The binaries are on the [releases page](https://github.com/boldsoftware/shelley/releases/latest).

## Homebrew (macOS)

```bash
brew install --cask boldsoftware/tap/shelley
```

## Build from Source

You'll need Go and Node.

```bash
git clone https://github.com/boldsoftware/shelley.git
cd shelley
make
```

# Releases

New releases are automatically created on every commit to `main`. Versions
follow the pattern `v0.N.9OCTAL` where N is the total commit count and 9OCTAL is the commit SHA encoded as octal (prefixed with 9).

# Architecture 

The technical stack is Go for the backend, SQLite for storage, and Typescript
with Vue 3 + PrimeVue for the UI. 

The data model is that Conversations have Messages, which might be from the
user, the model, the tools, or the harness. All of that is stored in the
database, and we use a SSE endpoint to keep the UI updated. 

# History

Shelley is partially based on our previous coding agent effort, [Sketch](https://github.com/boldsoftware/sketch). 

Unsurprisingly, much of Shelley is written by Shelley, Sketch, Claude Code, and Codex. 

# Shelley's Name

Shelley is so named because the main tool it uses is the shell, and I like
putting "-ey" at the end of words. It is also named after Percy Bysshe Shelley,
with an appropriately ironic nod at
"[Ozymandias](https://www.poetryfoundation.org/poems/46565/ozymandias)."
Shelley is a computer program, and, it's an it.

# Open source

Shelley is Apache licensed. We require a CLA for contributions.

# Building Shelley

Run `make`. Run `make serve` to start Shelley locally.

# Keyboard shortcuts (terminals)

Open a terminal from the overflow menu or command palette
(`Ctrl+K` / `⌘K` → “Terminal”). While any terminal is open:

| Shortcut | Action |
| --- | --- |
| `Ctrl+Shift+]` | Next terminal tab |
| `Ctrl+Shift+[` | Previous terminal tab |
| `Ctrl+Shift+W` | Close the active terminal |
| `Ctrl+Shift+1` … `9` | Jump to terminal tab N |
| `Ctrl+Shift+M` | Minimize or expand the terminal panel |
| `Tab` (in shell input) | Focus the terminal output log (read with VoiceOver) |
| `Tab` (in output log) | Return to shell input |
| `Escape` | Leave the terminal panel |
| `Ctrl+I` | Send Tab to the shell (completion) |
| `⌘A` (in terminal) | Select the terminal buffer only (not the whole page) |

On the tab strip (when a tab has focus): arrow keys move between
tabs; `Delete` / `Backspace` closes that tab; `Enter` activates it.

A fuller list of app shortcuts (composer, diffs, git graph) is under
`?` → Keyboard shortcuts and accessibility.

# Screen reader defaults (VaxTui)

This fork defaults **screen reader mode on** and opens **diffs in
text mode**. Turn screen reader mode off from the overflow menu
(⋯) if you want the old tool-collapse / visual-diff defaults; the
choice is stored in `localStorage`.

## Dev Tricks

If you want to see how mobile looks, and you're on your home
network where you've got mDNS working fine, you can
run 

```
socat TCP-LISTEN:9001,fork TCP:localhost:9000
```

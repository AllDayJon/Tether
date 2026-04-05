# Tether — Design Document

A terminal companion that lets you work normally while keeping an AI assistant in the loop. You drive. Claude rides along. When you need help, it's already up to speed.

## The Problem

Current AI coding tools (Claude Code, Cursor, etc.) put the AI in the driver's seat. You tell it what to do, it runs commands, you approve. If you want to do something yourself — SSH into a box, check logs, poke around — you leave the AI's world entirely. It loses context. You have to re-explain everything when you come back.

This tool flips the model. You're the sysadmin. You work in your terminal like normal. Claude watches passively, building context from what you do. When you want help — a question, a fix, a second opinion — you invoke it. Claude already knows what's going on because it's been watching.

## Core Concepts

### You are the driver

You work in your terminal. SSH into servers, edit files, read logs, run commands. The tool is invisible until you invoke it. No approval dialogs interrupting your flow. No AI deciding what to do next.

### Continuous context

Everything you do is captured locally. When you talk to Claude, relevant context is injected automatically. You never have to explain what you've been doing — Claude already saw it.

### Three modes

| Mode | Claude can... | Use when... |
|------|--------------|-------------|
| **Watch** | Read only. Answer questions, explain errors, suggest commands. Cannot type into your terminal. | You want advice but no risk. Default for production hosts. |
| **Assist** | Propose commands. You approve each one with a single keystroke before it executes. | You want help but want to stay in control. |
| **Act** | Execute commands directly into your terminal. You see everything happen and can kill it anytime. | You're watching closely, you trust the context, you want speed. |

### Token-aware

Full terminal history stays local. Claude gets a compressed summary of your session plus the last N lines of raw output. The system tracks what Claude has already seen and only sends diffs. You're not paying for 500 lines of log output every time you ask a question.

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│  Your tmux session                                      │
│                                                         │
│  Work pane                    Tether Chat (split)       │
│  ┌───────────────────┐   ┌───────────────────────────┐  │
│  │ You work here     │   │ tether  ASSIST  watching  │  │
│  │ normally          │   │                           │  │
│  │                   │   │ Streaming Claude response │  │
│  │ Claude watches    │   │ Command proposal UI       │  │
│  │ this pane         │   │ Mode indicator (badge)    │  │
│  │ (and any others   │   │                           │  │
│  │  you tell it to)  │   │ > your input here         │  │
│  └───────────────────┘   └───────────────────────────┘  │
└────────────────┬────────────────────────────────────────┘
                 │ tmux capture-pane (750ms poll)
┌────────────────▼────────────────────────────────────────┐
│  Daemon (~/.tether/tether.sock)                         │
│                                                         │
│  ┌──────────────┐ ┌──────────────┐ ┌─────────────────┐  │
│  │ Watcher      │ │ Summary      │ │ Mode / Policy   │  │
│  │              │ │ Generator    │ │                 │  │
│  │ capture-pane │ │              │ │ Watch/Assist/   │  │
│  │ polling per  │ │ 5-min timer  │ │ Act             │  │
│  │ watched pane │ │ rolls up     │ │                 │  │
│  │              │ │ session into │ │ Session allow   │  │
│  │ Ring buffer  │ │ narrative    │ │ list (in-mem)   │  │
│  │ (in-memory)  │ │              │ │                 │  │
│  │              │ │              │ │ cmdguard        │  │
│  │ Delta cursor │ │              │ │ classifier      │  │
│  └──────┬───────┘ └──────┬───────┘ └────────┬────────┘  │
│         └────────────────┴──────────────────┘           │
│                          │ IPC (JSON over Unix socket)   │
└──────────────────────────┼──────────────────────────────┘
                           │
         ┌─────────────────▼───────────────────┐
         │  Chat TUI / tether ask              │
         │                                     │
         │  Relevance filtering                │
         │  Conversation history               │
         │  Prompt assembly                    │
         │  claude -p …  (subprocess)          │
         │  Streaming output                   │
         └─────────────────────────────────────┘
```

## Components

### 1. Session Watcher (`internal/watcher/`)

Polls `tmux capture-pane` every 750ms for each watched pane. New lines are detected by diff and appended to an in-memory ring buffer. Nothing is written to disk.

- `ringbuffer.go` — fixed-size line buffer with a `Last(n)` and `Delta()` API
- `diff.go` — detects new lines since the last capture
- `tmux.go` — wraps `capture-pane` and `send-keys`

The daemon only watches panes explicitly added with `tether watch <pane>`. It does not scan all panes.

### 2. Summary Generator (`internal/summary/`)

A background goroutine fires every 5 minutes and asks Claude to summarise recent terminal activity into a short narrative paragraph. The summary is stored in memory and included in each prompt. It gives Claude a high-level understanding of what you've been doing without sending raw line-by-line history.

### 3. cmdguard Classifier (`internal/cmdguard/`)

A pure-Go classifier that determines what should happen to a command Claude suggests, evaluated in order:

1. **Hard deny** — fork bombs, pipe-to-shell (`curl | bash`, etc.). Always blocked. Not overridable.
2. **Hard protect** — `sudo`, file writes (`>`, `>>`, `tee`), command chaining (`&&`, `;`). Always proposed.
3. **Config deny list** — from `~/.tether/config.json`.
4. **Config protect list** — always proposed even in Act mode.
5. **Config allow list** — auto-executed in Act mode.
6. **Session allow list** — added at runtime via `[a]` in the proposal UI; stored in the daemon.
7. **Default** — anything not in any list; proposed in Assist, blocked in Watch.

Claude's output is never trusted directly. The classifier decides the outcome.

### 4. IPC Server (`internal/daemon/`)

A Unix socket server at `~/.tether/tether.sock`. The CLI and chat TUI communicate with the daemon over JSON-newline messages.

Message types: `watch`, `unwatch`, `status`, `get_context`, `clear_buffers`, `reset_seen`, `exec_in_pane`, `set_mode`, `add_session_allow`, `get_session_allow`, `stop`.

### 5. Conversation (`internal/conversation/`)

Manages the chat history and prompt assembly.

- Persists to `~/.tether/conversation.json`
- Auto-compacts when history exceeds 20 messages or ~32K chars
- `BuildPrompt` assembles: system prompt + mode instructions + history + session summary + filtered terminal context + user question

### 6. Context Selection (`internal/context/`)

Question-aware relevance filtering — runs locally, no model call.

- `relevance.go` — `SelectForQuestion`: scores each buffer line by keyword overlap with the question, returns top-K + last-N (always include recent lines for immediacy)
- `truncate.go` — `TruncateBlocks`: collapses runs of >15 similar lines to `first 3 … N omitted … last 3`

Reduces per-pane context from ~200 lines to ~25 lines, biased toward what's actually relevant.

### 7. Chat TUI (`internal/chat/`)

A bubbletea terminal UI opened as a vertical tmux split (`tether chat`).

- Streaming Claude responses via `claude -p` subprocess + goroutine/channel
- Mode badge in header (WATCH/ASSIST/ACT with colour coding)
- Proposal UI: `[Enter]` run, `[e]` edit, `[x]` reject, `[a]` allow for session
- Kill switch: `Ctrl+K` sends `Ctrl+C` to work pane
- Conversation history displayed with scroll
- Debug mode (`--debug`): logs prompt and context stats to `~/.tether/chat-debug.log`

## Security Model

```
Claude suggests a command
        │
        ▼
  cmdguard.Classify()
        │
   ┌────┴────────────────────────────┐
   │                                 │
Hard deny?                      Hard protect?
(fork bomb, pipe-to-shell)      (sudo, >, &&)
   │                                 │
   ▼                                 ▼
 Block                          Propose (always)
                                     │
                               Config lists?
                          ┌──────────┼──────────┐
                        Deny      Protect     Allow
                          │          │           │
                        Block    Propose    Watch→Block
                                           Assist→Propose
                                           Act→Execute
```

The key invariant: **Claude's text output has no direct execution path.** Every command passes through the classifier. Claude can influence what is proposed, but not what is executed.

## Data Flow

```
tmux pane output
    → capture-pane poll (750ms)
    → diff against last capture
    → ring buffer (in-memory, never written to disk)
    → Delta() called on each chat message
    → SelectForQuestion() relevance filter
    → TruncateBlocks() collapse long runs
    → BuildPrompt() assembles with history + summary
    → claude -p <prompt>  (subprocess)
    → streaming output to chat TUI
    → ExtractBashBlocks() parse suggestions
    → cmdguard.Decide() classify each command
    → Propose or Execute
```

## Implementation Status

| Phase | Status | Description |
|-------|--------|-------------|
| 1 — Foundation | ✅ | Daemon, IPC, watcher, ring buffer, `tether ask` |
| 2 — Chat window | ✅ | bubbletea TUI, streaming, conversation persistence |
| 3 — Context intelligence | ✅ | Rolling summary, delta tracking, compaction |
| 3.5 — Smart context | ✅ | Relevance filtering, truncation, token visibility |
| 4 — Modes and safety | ✅ | Watch/Assist/Act, cmdguard classifier, proposal UI, kill switch |
| 5 — Polish | ✅ | Config system, keybinds, UX commands, debug logging |
| 6 — Session allow | ✅ | `[a]` in proposal UI, IPC add/get, in-memory merge on TUI start |
| 7 — Project polish | ✅ | README, LICENSE, CHANGELOG, Makefile, CI, tests, `tether doctor` |

## Configuration

`~/.tether/config.json` (created by `tether config init`):

```json
{
  "chat_split_percent": 40,
  "chat_key": "g",
  "ask_lines": 200,
  "auto_watch": false,
  "allow": [],
  "protect": [],
  "deny": []
}
```

## File Layout

```
~/.tether/
  tether.sock        IPC socket
  daemon.pid         PID
  daemon.log         daemon log
  conversation.json  chat history
  config.json        user config
  chat-debug.log     optional prompt/token debug log

internal/
  watcher/           tmux polling, ring buffer, diff, send-keys
  summary/           rolling session summariser
  cmdguard/          command classifier and security rules
  ipc/               message types, client helpers, path helpers
  daemon/            IPC server, request dispatch
  conversation/      history, prompt assembly, compaction
  context/           relevance filtering, truncation
  chat/              bubbletea TUI

cmd/
  start, stop, status, ping, doctor
  watch, unwatch, panes
  chat, ask, mode
  tokens, tail, context, summary, history
  config, keybind, logs, clear
  version
```

## Future

- **PTY mode** — remove the tmux requirement; spawn a pty proxy so any terminal works
- **Auto-watch** — detect newly opened panes and watch them automatically
- **Per-host mode defaults** — set `prod-*` to Watch automatically based on SSH hostname
- **Multi-pane awareness** — smarter context merging when multiple panes are watched simultaneously
- **Remote tether** — optional lightweight agent on remote hosts for richer SSH session context

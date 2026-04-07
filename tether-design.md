# Tether — Design Document

A terminal companion that lets you work normally while keeping an AI assistant in the loop. You drive. Claude rides along. When you need help, it's already up to speed.

## The Problem

Current AI coding tools put the AI in the driver's seat. You tell it what to do, it runs commands, you approve. If you want to do something yourself — SSH into a box, check logs, poke around — you leave the AI's world entirely. It loses context. You have to re-explain everything when you come back.

Tether flips the model. You're the operator. You work in your terminal like normal. Claude watches passively, building context from what you do. When you want help, you ask. Claude already knows what's going on.

## Core Concepts

### You are the driver

You work in your terminal. SSH into servers, edit files, read logs, run commands. Tether is invisible until you invoke it. No approval dialogs interrupting your flow. No AI deciding what to do next.

### Continuous context

Everything you do in a `tether shell` session is captured locally. When you talk to Claude, relevant context is injected automatically. You never have to explain what you've been doing.

### Three modes

| Mode | Claude can... | Use when... |
|------|--------------|-------------|
| **Watch** | Read only. Answer questions, explain errors, suggest commands. Cannot execute anything. | You want advice with no risk. Default for production. |
| **Assist** | Propose commands. You approve each one with a single keystroke before it executes. | You want help but want to stay in control. |
| **Act** | Execute allow-listed commands directly. Propose everything else. | You're watching closely and want speed. |

### Token-aware

The full session buffer stays local. Claude gets a filtered snapshot of what's relevant to the question, plus a rolling session summary and recent conversation history. The system tracks what Claude has already seen and deprioritises stale lines. You're not paying for 500 lines of log output every time you ask a question.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  tether shell (PTY proxy)                                   │
│                                                             │
│  Your shell runs here normally. tether sits between your    │
│  terminal and the shell process, reading output as it       │
│  arrives. OSC 133 markers (from shell integration) tag      │
│  prompt/command boundaries.                                 │
└──────────────────────┬──────────────────────────────────────┘
                       │ PTY output stream (real-time)
┌──────────────────────▼──────────────────────────────────────┐
│  Session Buffer  (~/.tether/sessions/<pid>.sock)              │
│                                                             │
│  ┌────────────────┐  ┌──────────────┐  ┌─────────────────┐  │
│  │ session.Buffer │  │ Summary      │  │ Mode / Policy   │  │
│  │                │  │ Generator    │  │                 │  │
│  │ Ring buffer    │  │              │  │ Watch/Assist/   │  │
│  │ 5,000 lines    │  │ 5-min timer  │  │ Act             │  │
│  │ in-memory      │  │ narrative    │  │                 │  │
│  │                │  │ summary      │  │ Session allow   │  │
│  │ Delta cursor   │  │              │  │ list (in-mem)   │  │
│  │ Last/Delta API │  │              │  │                 │  │
│  │                │  │              │  │ cmdguard        │  │
│  └───────┬────────┘  └──────┬───────┘  └────────┬────────┘  │
│          └─────────────────┴───────────────────┘           │
│                             │ IPC (JSON over Unix socket)   │
└─────────────────────────────┼───────────────────────────────┘
                              │
          ┌───────────────────▼──────────────────────┐
          │  tether chat (TUI)                       │
          │                                          │
          │  fetchDeltaContext() — all active sessions│
          │  SelectForQuestion() relevance filter    │
          │  TruncateBlocks() collapse long runs     │
          │  BuildPrompt() assemble prompt           │
          │  claude -p … (subprocess, streaming)    │
          │  Proposal UI / mode enforcement         │
          └──────────────────────────────────────────┘
```

## Components

### 1. PTY Proxy (`internal/pty/`)

`tether shell` spawns the user's shell as a subprocess under a PTY proxy. The proxy sits between the user's terminal and the shell:

- All output from the shell is tee'd into the session buffer in real time
- `proxy.go` — manages the PTY pair, raw terminal mode, window resize, signal forwarding
- `osc.go` — parses OSC 133 escape sequences (prompt start/end, command start/end) emitted by shell integration; feeds structured command/output blocks to the buffer

The proxy is transparent — the shell behaves exactly as normal. When the user exits the shell, the proxy tears down cleanly.

### 2. Shell Integration (`internal/shellintegration/`)

`tether install` writes integration scripts for bash, zsh, and fish into `~/.tether/` and sources them from the appropriate shell config file (`.bashrc`, `.zshrc`, or `~/.config/fish/conf.d/`). macOS uses `.bash_profile` instead of `.bashrc`.

The scripts emit OSC 133 markers around prompts and command output. These markers are what allow `osc.go` to identify command boundaries — enabling block-aware context scoring.

### 3. Session Buffer (`internal/session/`)

A thread-safe ring buffer that receives output from the PTY proxy.

- `buffer.go` — 5,000-line ring buffer with `Last(n)` and `Delta()` API
- `Delta()` returns lines added since the last call and advances a cursor — used by the chat TUI to fetch only new output each turn
- `Append()` accepts pre-split lines from OSC 133 events; `Write()` implements `io.Writer` for raw PTY output

Each `tether shell` session registers its own Unix socket (`~/.tether/sessions/<pid>.sock`). The chat TUI discovers all active sessions via directory listing.

### 4. Summary Generator (`internal/summary/`)

A background goroutine fires every 5 minutes and asks Claude to summarise recent terminal activity into a short narrative paragraph. The summary is persisted to `~/.tether/summary.txt` and included in each prompt. It gives Claude a high-level understanding of what the user has been doing without sending raw line-by-line history.

### 5. cmdguard Classifier (`internal/cmdguard/`)

A pure-Go classifier that determines what should happen to a command Claude suggests, evaluated in order:

1. **Hard deny** — fork bombs, pipe-to-shell (`curl | bash`, etc.). Always blocked. Not overridable.
2. **Hard protect** — `sudo`, file writes (`>`, `>>`, `tee`), command chaining (`&&`, `;`). Always proposed.
3. **Config deny list** — from `~/.tether/config.json`.
4. **Config protect list** — always proposed even in Act mode.
5. **Config allow list** — auto-executed in Act mode.
6. **Session allow list** — added at runtime via `[a]` in the proposal UI; stored in the daemon.
7. **Default** — anything not matched; proposed in Assist, blocked in Watch.

Claude's output is never trusted directly. The classifier decides the outcome.

### 6. IPC (`internal/ipc/`)

JSON-newline messages over Unix sockets. The chat TUI connects to each active session socket directly.

Message types: `get_context`, `clear_buffers`, `reset_seen`, `exec_in_pane`, `set_mode`, `add_session_allow`, `get_session_allow`, `status`, `stop`.

Scanner buffer is set to 4 MB to handle large context payloads without truncation.

### 7. Context Selection (`internal/context/`)

Question-aware relevance filtering — runs locally, no model call.

- `relevance.go` — `SelectForQuestion`: scores each buffer line by keyword overlap with the question plus an error-signal bonus (+3) for lines matching error/warning patterns; returns top-K + always-include last-N recency lines
- Error signals: `error`, `failed`, `panic`, `connection refused`, `timeout`, `permission denied`, etc.
- Cross-turn dedup: lines in `SentLines` (already sent last turn) receive a −3 penalty to deprioritise stale context
- Block expansion: selected lines expand to their surrounding command block (command + output); large blocks >40 lines get a ±2 line window instead of full expansion
- Per-line cap: 500 chars; per-pane cap: 200 lines after merging; context section cap: 20,000 chars in prompt
- `truncate.go` — `TruncateBlocks`: collapses runs of >15 similar lines to `first 3 … N omitted … last 3`

### 8. Conversation (`internal/conversation/`)

Manages chat history and prompt assembly.

- Persists to `~/.tether/conversation.json`
- Auto-compacts when history exceeds 20 messages or ~32K chars (via Claude summary)
- History compression in `BuildPrompt`: last 3 exchanges included verbatim; older assistant responses dropped (user turns kept for context continuity)
- `BuildPrompt` assembles: system prompt + mode instructions + compressed history + session summary (capped 1,000 chars) + filtered terminal context + user question

### 9. Chat TUI (`internal/chat/`)

A bubbletea terminal UI. Runs standalone in any terminal (`tether chat`).

- `fetchDeltaContext()` — connects to all active session sockets, fetches delta output from each, returns merged panes + session summary + IPC diagnostics
- `launchClaude()` — owns the full filter pipeline: `SelectForQuestion` → `TruncatePanes` → `BuildPrompt` → `claude -p` subprocess
- Streaming Claude responses via goroutine/channel
- Per-message timestamps and token cost estimates (↑ prompt / ↓ response)
- `/debug` slash command: toggles a debug block showing keywords, fetched vs. filtered line counts, and per-component token breakdown (system / history / summary / context)
- Mode badge in header (WATCH/ASSIST/ACT); right side shows ctx lines, message count, total token estimate
- Proposal UI: `[Enter]` run, `[e]` edit, `[x]` reject, `[a]` allow for session
- `Ctrl+K` aborts a streaming response mid-flight
- Markdown rendering for assistant responses

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

The key invariant: **Claude's text output has no direct execution path.** Every command passes through the classifier. Claude can influence what is proposed, not what is executed.

## Data Flow

```
shell output (PTY stream)
    → osc.go parses OSC 133 markers (command boundaries)
    → session.Buffer.Write() / Append() (ring buffer, in-memory)
    → Delta() called on each chat turn
    → SelectForQuestion() relevance filter + block expansion
    → TruncateBlocks() collapse long repeated runs
    → BuildPrompt() assemble with history + summary
    → claude -p <prompt>  (subprocess)
    → streaming output to chat TUI
    → ExtractBashBlocks() parse suggestions
    → cmdguard.Decide() classify each command
    → Propose or Execute
```

## File Layout

```
~/.tether/
  sessions/<pid>.sock  Per-session IPC socket
  daemon.log           Daemon log (startup/stop, errors)
  conversation.json  Chat history
  summary.txt        Rolling session summary
  config.json        User config
  shell-integration.bash  Shell integration script (bash)
  shell-integration.zsh   Shell integration script (zsh)
  shell-integration.fish  Shell integration script (fish)

internal/
  pty/               PTY proxy, OSC 133 parser
  session/           Ring buffer (5k lines, in-memory)
  shellintegration/  Install/uninstall shell integration scripts
  summary/           Rolling session summariser
  cmdguard/          Command classifier and security rules
  ipc/               Message types, client helpers, path helpers
  daemon/            IPC server, request dispatch
  conversation/      History, prompt assembly, compaction
  context/           Relevance filtering, block expansion, truncation
  chat/              bubbletea TUI
  config/            Config loading and defaults
  claude/            Claude subprocess client

cmd/
  install, uninstall
  shell
  chat, ask, mode
  status, doctor, ping
  tokens, context, summary, history
  config, logs, clear, stop
  version
```


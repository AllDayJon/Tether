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
│  Window 1: Work          Window 2: Tether Chat         │
│  ┌───────────────────┐   ┌───────────────────────────┐  │
│  │ You work here     │   │ Conversation with Claude  │  │
│  │ normally           │   │                           │  │
│  │                   │   │ Shows streaming responses │  │
│  │ Claude watches    │   │ Command proposals         │  │
│  │ this pane         │   │ Mode indicator            │  │
│  │ (and any others   │   │                           │  │
│  │  you tell it to)  │   │ > your input here         │  │
│  └───────────────────┘   └───────────────────────────┘  │
└────────────────┬────────────────────────────────────────┘
                 │ tmux control mode
┌────────────────▼────────────────────────────────────────┐
│  Daemon (long-running, Go)                              │
│                                                         │
│  ┌──────────────┐ ┌──────────────┐ ┌─────────────────┐  │
│  │ Session      │ │ Context      │ │ Mode / Policy   │  │
│  │ Watcher      │ │ Manager      │ │ Engine          │  │
│  │              │ │              │ │                 │  │
│  │ Captures     │ │ Maintains    │ │ Watch / Assist  │  │
│  │ terminal     │ │ rolling      │ │ / Act           │  │
│  │ output via   │ │ summary +    │ │                 │  │
│  │ control mode │ │ recent raw   │ │ Per-host policy │  │
│  │              │ │ output       │ │ defaults        │  │
│  │ Watches      │ │              │ │                 │  │
│  │ specified    │ │ Tracks what  │ │ Command allow / │  │
│  │ panes only   │ │ Claude has   │ │ deny lists      │  │
│  │              │ │ seen         │ │                 │  │
│  └──────────────┘ └──────┬───────┘ └────────┬────────┘  │
│                          │                  │           │
│  ┌───────────────────────▼──────────────────▼────────┐  │
│  │ API Client                                        │  │
│  │                                                   │  │
│  │ Persistent conversation with Claude API           │  │
│  │ Injects context on each invocation                │  │
│  │ Routes Claude's actions through mode engine       │  │
│  └───────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

## Components

### 1. Session Watcher

Connects to your tmux session via control mode (`-CC`). Passively captures all `%output` events and stores them in a local ring buffer. No polling — event-driven.

Also watches for:
- Which pane is active (so Claude knows where you're working)
- Current command running in each pane (via tmux's `pane_current_command`)
- Directory changes
- SSH sessions (hostname detection from prompt or environment)

All data stays local on disk. Nothing leaves your machine until you invoke Claude.

### 2. Context Manager

The most important component for keeping token costs down. Maintains two layers of context:

**Rolling summary.** A local, periodically updated text summary of what you've been doing. Example: "User SSHed into prod-web-01. Checked nginx status (active). Tailed /var/log/nginx/error.log — saw repeated 502 errors from upstream. Opened /etc/nginx/sites-enabled/api.conf in vim. Currently editing proxy_pass directive."

This summary is generated locally on a timer or triggered by significant events (new SSH session, editor opened, command failure). It can use a cheap/fast model or even simple heuristics for parts of it.

**Recent raw output.** The last N lines of terminal output from each active pane. This gives Claude exact details — error messages, config file contents, command output — without sending the full session history.

**Seen tracking.** A high-water mark of what context Claude has already received. On the next invocation, only the delta gets sent. If Claude saw the nginx error log 2 minutes ago and you haven't done much since, the next message is small.

### 3. Mode / Policy Engine

Controls what Claude is allowed to do. Three layers:

**Global default.** Set in a config file. New sessions start in this mode.

**Per-host overrides.** Define in config: `prod-*` hosts default to `watch`, homelab defaults to `act`. Detected automatically from SSH hostname.

**Runtime toggle.** Switch modes with a hotkey or command at any time. Overrides the default until the session ends or you switch again.

**Command policies** (optional, for assist/act modes):

```toml
[policy.default]
mode = "assist"

[policy.hosts."prod-*"]
mode = "watch"

[policy.hosts."homelab-*"]
mode = "act"

# Commands that never require approval (in assist mode)
allow = ["cat", "ls", "grep", "head", "tail", "ps", "df", "free", "top"]

# Commands that always require approval regardless of mode
protect = ["rm -rf", "dd ", "mkfs", "shutdown", "reboot"]

# Commands that are completely blocked
deny = [":(){ :|:& };:"]
```

### 4. API Client

Manages the conversation with Claude's API. Key responsibilities:

- Maintains a persistent message history for the session
- Prepends the context summary + recent output on each invocation
- Handles streaming responses (so you see Claude's reply as it types)
- Parses Claude's responses for actionable commands vs. conversational text
- Routes commands through the mode engine before execution

Uses a system prompt that establishes Claude's role: you're a terminal companion for a sysadmin, you can see their terminal, you should be concise, and when you want to run commands, format them in a specific way so the tool can parse and route them.

### 5. Chat UI

Two modes of interaction:

**Chat window.** A separate tmux window dedicated to the conversation. Switch to it with a hotkey or `tmux select-window`. Supports scrollback, multi-line input, streaming responses, and command proposals with approve/reject controls in assist mode. Lives alongside your work windows — you flip to it when you want to talk, flip back when you're done. Could also be on a second monitor or a separate terminal entirely. The chat window doesn't need to be visible for the daemon to keep watching your work panes.

**Inline mode.** For quick one-offs without switching windows. Run `tether ask "what does this error mean"` from your shell. Response prints directly in your terminal. Good for fast questions where you don't need a back-and-forth conversation.

## Interaction Flow

### Asking a question

```
You: (working in terminal, see a weird error)
You: (switch to tether chat window)
You: "What's this segfault about?"

Tool: (packages last 30 lines of output + session summary)
Tool: (sends to Claude API)

Claude: "That segfault is from the Redis connection pool. The pool
         size is set to 5 but you have 12 worker threads competing
         for connections. You can either increase the pool size in
         redis.conf or reduce workers."

You: (switches back to work window, keeps working)
```

### Asking Claude to do something (assist mode)

```
You: "Fix the Redis pool size — set it to 20"

Claude: (proposes)
  ┌─────────────────────────────────────────────────┐
  │  sed -i 's/pool_size = 5/pool_size = 20/'       │
  │      /etc/redis/redis.conf                      │
  │                                                 │
  │  [Enter] approve  [e] edit  [x] reject          │
  └─────────────────────────────────────────────────┘

You: (presses Enter)
Tool: (executes in your work pane)
Claude: "Done. Want me to restart the Redis service?"
```

### Asking Claude to do something (act mode)

```
You: "Fix the Redis pool size — set it to 20"

Claude: (executes immediately, you see it happen in your work pane)
Claude: "Done — set pool_size to 20 in /etc/redis/redis.conf.
         Want me to restart Redis?"
```

### Kill switch

At any time, a hotkey (e.g., `Ctrl+c` in the chat window, or a dedicated kill key) immediately:
- Stops any command Claude is currently executing (sends SIGINT to the pane)
- Drops Claude back to watch mode
- Prints what was interrupted

## Token Budget Strategy

Target: keep most interactions under 4K tokens of context injection.

| Component | Approximate tokens | Sent when |
|-----------|-------------------|-----------|
| System prompt | ~500 | Every request |
| Session summary | ~200-400 | Every request |
| Recent raw output (last 30 lines) | ~300-600 | Every request |
| Delta since last seen | ~100-500 | If available |
| Conversation history | varies | Compacted periodically |

**Compaction.** After the conversation reaches a threshold (e.g., 20 messages or 8K tokens of history), the tool asks Claude to summarize the conversation so far and replaces the history with the summary. Same idea as Claude Code's `/compact`.

**Selective detail.** When Claude needs more context — "can you show me the full config file?" — it requests it explicitly and the tool sends it as a one-time injection. You don't pay for the full file on every subsequent message.

## Implementation Plan

### Phase 1 — Foundation

- Go project scaffold
- tmux control mode connection (session watcher)
- Ring buffer for terminal output
- Basic CLI: `tether start`, `tether stop`, `tether status`
- `tether watch %0` — tell the daemon which panes to observe
- Inline `tether ask` command — sends question + last N lines to Claude API, prints response

### Phase 2 — Chat window

- `tether chat` — opens a dedicated tmux window with the chat TUI
- Chat TUI (input area, scrollable output, streaming responses)
- Persistent conversation (message history within a session)
- Hotkey binding to toggle between work and chat windows

### Phase 3 — Context intelligence

- Session summary generation (command detection, SSH host tracking, directory tracking)
- Seen tracking and delta computation
- Conversation compaction

### Phase 3.5 — Smart Context Selection

**Motivation.** The Claude Code subscription has hourly token limits. Even with delta tracking and summaries, blindly sending the last 50 lines per pane wastes tokens on irrelevant output (log spam, unrelated commands, repeated prompts). The goal is to keep the full terminal buffer local and only send the parts that are actually useful for the current question.

**Approach: question-aware relevance filtering.**

When a question is asked, instead of slicing the last-N lines, the system:
1. Fetches the full local buffer (up to 200 lines per pane — never sent to Claude)
2. Scores each line by keyword overlap with the question
3. Selects the top-K relevant lines plus the last-N lines for recency (so Claude always has immediate context)
4. Deduplicates and re-orders by original line position (preserving chronological flow)
5. Truncates large consecutive output blocks (>15 lines) to first 3 + "… N lines omitted …" + last 3

**Implementation: `internal/context/` package.**

```
internal/context/
  relevance.go   — keyword extraction, line scoring, SelectForQuestion()
  truncate.go    — TruncateBlocks() for large output runs
```

`SelectForQuestion(question string, panes []ipc.PaneContext, opts Options) []ipc.PaneContext`
- `opts.TopK` — max relevant lines per pane (default 20)
- `opts.LastN` — always include last N lines regardless of score (default 5)
- `opts.MaxLines` — hard cap per pane (default 25)

**Keyword scoring.**

Simple tf-style scoring — no model call, no external deps:
1. Tokenise the question into lowercase words, strip stop words
2. For each buffer line, count how many question keywords appear
3. Rank lines by score, take top-K

**Token impact.** Reduces per-pane context from ~50 lines (full last-N) to ~25 lines, but those 25 lines are significantly more relevant. Combined with delta tracking, most follow-up messages stay small.

**Changes to existing code.**
- `internal/claude/client.go BuildPrompt` — apply relevance filtering on panes before building prompt
- `internal/conversation/conversation.go BuildPrompt` — same
- `cmd/ask.go` — fetch 200 lines before filtering (was 50)
- `cmd/tokens.go` — add "after relevance filter" row showing lines before → after and % saved

### Phase 4 — Modes and safety

- Watch / assist / act modes with runtime switching
- Policy file (per-host defaults, allow/deny lists)
- Command proposal UI (approve/edit/reject in assist mode)
- Kill switch

### Phase 5 — Polish ✓

- **Config file** (`~/.tether/config.json`) — set defaults for split percent, model, ask lines, auto-watch. CLI flags override config. `tether config` shows current settings, `tether config init` creates the file.
- **Session persistence** — conversation and session summary are already persisted to disk (`~/.tether/conversation.json`, `~/.tether/summary.txt`). The daemon reloads the summary on restart so Claude has context immediately without waiting 5 minutes.
- **SSH pane highlighting** — the `tether watch` interactive picker marks SSH/mosh/telnet panes with an `[ssh]` badge so you notice remote sessions.
- **Quality-of-life commands** — `tether summary`, `tether history`, `tether config`. Auto-start daemon when `tether ask` or `tether chat` is run without one running. `tether start` shows version + uptime + buffer stats when daemon is already running.
- **Token visibility** — chat TUI header shows live `~Ntok` estimate. `tether ask --debug` shows `fetched N lines → sent M lines (-X%)` with token counts.

## Future: PTY Mode (v2)

The tmux dependency means users need to be in tmux. A future PTY proxy mode would remove this requirement. The tool would spawn a pseudo-terminal, run the user's shell inside it, and transparently capture the stream while forwarding everything to the real terminal. Launch with `tether shell` instead of your normal terminal. Same daemon, same context manager, same chat — just a different capture backend. This opens the tool up to users who don't use tmux. Could also support terminal-specific integrations (Kitty remote control, iTerm2 API, WezTerm Lua) as additional backends.

## Open Questions

- **Summary generation.** Use a cheap model for local summarization, or heuristic-based? Heuristics are free but less accurate. A fast model call adds latency and cost but produces better context.
- **Multi-host.** If you SSH from one box to another mid-session, how does the context manager handle the transition? Probably needs to detect hostname changes from the prompt and note it in the summary.
- **Team use.** Is this always single-user, or could multiple people share a session with Claude? Probably single-user for v1.
- **Conversation persistence across sessions.** If you detach tmux and come back tomorrow, should Claude remember yesterday's context? Probably yes — save conversation + summary to disk, reload on reattach.
- **Model selection.** Should the user be able to choose which Claude model to use? Haiku for cheap quick questions, Sonnet for most work, Opus for complex reasoning? Could default to Sonnet and let the user override.

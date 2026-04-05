<br>

<div align="center">

# ⛓ Tether

**A terminal companion that keeps Claude in the loop while you work.**

You drive. Claude rides along. When you need help, it's already up to speed.

[![Version](https://img.shields.io/badge/version-0.7.0-blue?style=flat-square)](https://github.com/allday/tether/releases)
[![Go](https://img.shields.io/badge/go-1.22+-00ADD8?style=flat-square&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green?style=flat-square)](LICENSE)
[![Requires tmux](https://img.shields.io/badge/requires-tmux-1DB954?style=flat-square)](https://github.com/tmux/tmux)

</div>

---

## The idea

Most AI coding tools put the AI in the driver's seat — you describe a task, it runs commands, you approve. The moment you want to do something yourself (SSH into a box, dig through logs, poke around a prod issue) you leave the AI's world entirely. It loses context. You re-explain everything when you come back.

Tether flips that model. **You work in your terminal like normal.** Tether watches your tmux panes in the background, building context as you go. When you want a second opinion, you hit a keybind. Claude already knows what's going on.

```
┌──────────────────────────────┬─────────────────────────────────────┐
│  your terminal               │  tether  ASSIST  watching: %0       │
│                              │                                     │
│  $ ssh prod-01               │  You                                │
│  prod-01 $ tail -f app.log   │  why are these 502s spiking?        │
│  [502 Bad Gateway ...]       │                                     │
│  [502 Bad Gateway ...]       │  Claude                             │
│  prod-01 $                   │  Upstream timeout from nginx —      │
│                              │  worker_processes is likely         │
│                              │  saturated. Check:                  │
│                              │                                     │
│                              │  $ journalctl -u nginx -n 50        │
│                              │                                     │
│                              │  ╭─ Run this command? ────────────╮ │
│                              │  │ $ journalctl -u nginx -n 50   │ │
│                              │  │ [Enter] run  [e] edit  [x] no │ │
│                              │  │ [a] allow 'journalctl' session│ │
│                              │  ╰───────────────────────────────╯ │
│                              │ > _                                 │
└──────────────────────────────┴─────────────────────────────────────┘
```

---

## Features

- **Persistent context** — Claude sees your terminal history before you ask anything
- **Delta-only updates** — only new output is sent each message, not the full buffer
- **Relevance filtering** — context is scored against your question; irrelevant lines are dropped
- **Three modes** — Watch (read-only), Assist (propose + approve), Act (auto-execute)
- **Session allow list** — approve an unlisted command once, it auto-executes for the rest of the session
- **SSH-transparent** — commands are injected via `tmux send-keys`; no tether install needed on remote hosts
- **Kill switch** — `Ctrl+K` sends `Ctrl+C` to the work pane at any time
- **Conversation memory** — chat history persists across sessions and compacts automatically
- **Rolling summary** — a background summarizer keeps a narrative of your session

---

## Requirements

| Dependency | Notes |
|------------|-------|
| **Go 1.22+** | For building from source |
| **tmux** | Session capture and command injection |
| **Claude Code CLI** | `claude` must be on PATH and logged in — [install here](https://claude.ai/code) |

---

## Installation

```sh
git clone https://github.com/allday/tether
cd tether
go install .
```

Verify:

```sh
tether version
```

---

## Quick start

```sh
# 1. Start a tmux session
tmux new -s work

# 2. Start the tether daemon
tether start

# 3. Watch your current pane
tether watch %0

# 4. Open the chat split (40% right side)
tether chat

# — or install a keybind so prefix+g opens it from anywhere —
tether keybind install --persist
```

---

## Modes

Tether has three operating modes, switchable at any time with `tether mode <name>`.

| Mode | What Claude can do | When to use |
|------|--------------------|-------------|
| **Watch** | Read only — answer questions, explain errors, suggest commands in plain text | Default. Safe for production. |
| **Assist** | Propose commands — you approve each one before it runs | You want help but want to stay in control |
| **Act** | Auto-execute allow-listed commands; propose everything else | You're watching closely and want speed |

```sh
tether mode assist   # enable proposals
tether mode act      # enable auto-execution
tether mode watch    # back to read-only
```

### Proposal UI (Assist / Act)

When Claude suggests a command, a proposal appears above the input:

```
╭─ Run this command? ──────────────────────────────────╮
│ $ journalctl -u nginx -n 50                          │
│                                                      │
│ [Enter] run  [e] edit  [x] reject                    │
│ [a] allow 'journalctl' this session                  │
╰──────────────────────────────────────────────────────╯
```

| Key | Action |
|-----|--------|
| `Enter` | Run the command |
| `e` | Edit before running |
| `a` | Allow this command for the rest of the session |
| `x` / `Esc` | Reject |

---

## Security

Commands pass through a classifier before anything happens. Rules are evaluated in order:

1. **Hard deny** — fork bombs, pipe-to-shell (`curl | bash`, `wget | sh`). Always blocked, no override.
2. **Hard protect** — `sudo`, output redirects (`>`), `tee`, command chaining (`&&`, `;`). Always proposed, never auto-executed.
3. **Config rules** — your `deny`, `protect`, and `allow` lists in `~/.tether/config.json`.

Claude's output is **never trusted directly**. The classifier decides what happens to a command — Claude influences the proposal, not the outcome.

**Kill switch:** `Ctrl+K` in the chat window sends `Ctrl+C` to the work pane immediately.

---

## Configuration

Run `tether config init` to create `~/.tether/config.json` with defaults.

```jsonc
{
  "chat_split_percent": 40,   // width of the chat pane (% of terminal)
  "chat_key": "g",            // tmux prefix key to open chat
  "ask_lines": 200,           // context lines sent to `tether ask`
  "auto_watch": false,        // auto-watch the launching pane on daemon start
  "allow":   ["git", "go"],   // auto-execute in Act mode
  "protect": [],              // always propose, never auto-execute
  "deny":    []               // always block
}
```

---

## Command reference

```
tether start                    Start the background daemon
tether stop                     Stop the daemon
tether status                   Daemon status, watched panes, mode, allow list
tether watch <pane>             Start watching a pane (e.g. %0, %1)
tether unwatch <pane>           Stop watching a pane
tether chat                     Open chat as a vertical tmux split
tether ask <question>           One-shot question, prints to stdout
tether mode [watch|assist|act]  Show or change the current mode
tether tokens                   Show context buffer size
tether tail                     Stream pane output in real time
tether summary                  Print the rolling session summary
tether history                  Show conversation history with token stats
tether config                   Show current config
tether config init              Write default config to ~/.tether/config.json
tether keybind install          Install tmux keybinding (--persist to save)
tether keybind remove           Remove tether lines from ~/.tmux.conf
tether keybind show             Print the bindings that would be applied
tether version                  Print version
```

---

## Chat keybindings

| Key | Action |
|-----|--------|
| `Enter` | Send message |
| `PgUp` / `PgDn` | Scroll history |
| `Ctrl+L` | Clear conversation |
| `Ctrl+K` | Kill switch — send `Ctrl+C` to work pane |
| `Ctrl+C` | Close chat |

---

## Data

Everything lives in `~/.tether/`:

| Path | Contents |
|------|----------|
| `tether.sock` | IPC socket |
| `daemon.pid` | Daemon process ID |
| `daemon.log` | Daemon log output |
| `conversation.json` | Persistent chat history |
| `config.json` | User configuration |
| `chat-debug.log` | Context/token debug log (`tether chat --debug`) |

---

## License

MIT — see [LICENSE](LICENSE)

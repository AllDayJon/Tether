<div align="center">

<br>

# ⛓ Tether

**You work. Claude rides along.**

Tether runs in your terminal session, building context as you go.
When you need help, it's already up to speed — no copy-paste, no re-explaining.

<br>

[![CI](https://github.com/AllDayJon/Tether/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/AllDayJon/Tether/actions/workflows/ci.yml)
[![Version](https://img.shields.io/badge/version-0.9.0-cyan?style=flat-square)](https://github.com/AllDayJon/Tether/releases)
[![Go](https://img.shields.io/badge/go-1.22+-00ADD8?style=flat-square&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green?style=flat-square)](LICENSE)
[![Claude Code](https://img.shields.io/badge/powered%20by-Claude%20Code-blueviolet?style=flat-square)](https://claude.ai/code)

<br>

</div>

```
┌──────────────────────────────┬──────────────────────────────────────────┐
│  tether shell — work         │  tether chat                             │
│                              │                                          │
│  $ ssh prod-01               │  tether  ASSIST       12ctx  1 msg ~320tok│
│  prod-01 $ tail -f app.log   │                                          │
│  [502 Bad Gateway ...]       │  You                                     │
│  [502 Bad Gateway ...]       │  why are these 502s spiking?             │
│  prod-01 $                   │                                          │
│                              │  Claude  14:22                           │
│                              │  Upstream timeout from nginx —           │
│                              │  worker_processes is likely saturated.   │
│                              │                                          │
│                              │  ╭─ Run this command? ─────────────────╮ │
│                              │  │ $ journalctl -u nginx -n 50         │ │
│                              │  │ [Enter] run  [e] edit  [x] reject   │ │
│                              │  │ [a] allow 'journalctl' this session │ │
│                              │  ╰─────────────────────────────────────╯ │
│                              │  ↑ ~320 tok  ↓ ~85 tok                  │
│                              │ > _                                      │
└──────────────────────────────┴──────────────────────────────────────────┘
```

---

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/AllDayJon/Tether/main/install.sh | sh
```

Open a new terminal, then run `tether shell`.

That's all. The script detects your OS and architecture, downloads the right binary to `/usr/local/bin`, and sets up shell integration automatically.

<details>
<summary>Manual install</summary>

<br>

Download the binary for your platform from the [releases page](https://github.com/AllDayJon/Tether/releases):

| Platform | Binary |
|----------|--------|
| Linux x86_64 | `tether-linux-amd64` |
| Linux ARM64 | `tether-linux-arm64` |
| macOS Intel | `tether-darwin-amd64` |
| macOS Apple Silicon | `tether-darwin-arm64` |

```sh
# Example — macOS Apple Silicon
curl -L https://github.com/AllDayJon/Tether/releases/latest/download/tether-darwin-arm64 -o tether
chmod +x tether && sudo mv tether /usr/local/bin/

# Verify checksum (Linux)
curl -L https://github.com/AllDayJon/Tether/releases/latest/download/checksums.txt | sha256sum --check --ignore-missing
# Verify checksum (macOS)
curl -L https://github.com/AllDayJon/Tether/releases/latest/download/checksums.txt | grep tether-darwin-arm64 | shasum -a 256 -c
```

Then set up shell integration:

```sh
tether install   # adds source line to your shell config (bash, zsh, or fish)
# open a new terminal
tether doctor    # verify everything is ready
```

**Build from source** (requires Go 1.22+):

```sh
git clone https://github.com/AllDayJon/Tether && cd Tether && go install .
```

</details>

---

## Quick start

```sh
# Terminal 1 — your work session
tether shell

# Terminal 2 — chat
tether chat
```

That's it. Work normally in terminal 1. Ask Claude anything in terminal 2.

---

## Features

| | |
|:---|:---|
| **Persistent context** | Claude sees your terminal history before you ask anything |
| **Relevance filtering** | Lines are scored against your question — only what matters gets sent |
| **Error surfacing** | Errors and failures are always prioritised, even when your keywords don't match |
| **Cross-turn dedup** | Previously sent lines are deprioritised so new context is preferred each turn |
| **SSH-transparent** | Captures everything in your local PTY — SSH output included, no remote install |
| **Conversation memory** | Chat history persists across sessions and compacts automatically |
| **Rolling summary** | A background summariser keeps a running narrative of your session |
| **Token visibility** | Estimated cost shown after each reply — `/debug` for a full breakdown |
| **Claude Code handoff** | `/handoff <task>` packages your session context and launches Claude Code with it pre-loaded |

---

## Modes

Switch at any time with `tether mode <name>`.

| Mode | Behaviour | When to use |
|------|-----------|-------------|
| **Watch** | Read-only. Claude answers, explains, and suggests — but can't execute | Default. Safe for production. |
| **Assist** | Claude proposes commands. You approve each one before it runs. Enable auto-run with `[t]` in the chat TUI to automatically run allow-listed commands. | You want help but want to stay in control |

```sh
tether mode watch     # read-only
tether mode assist    # propose + approve
```

When Claude proposes a command:

```
╭─ Run this command? ──────────────────────────────────╮
│ $ journalctl -u nginx -n 50                          │
│ [Enter] run  [e] edit  [x] reject                    │
│ [a] allow 'journalctl' for this session              │
╰──────────────────────────────────────────────────────╯
```

---

## Security

Every command Claude suggests passes through a classifier before anything happens.

1. **Hard deny** — fork bombs, pipe-to-shell. Always blocked, no override.
2. **Hard protect** — `sudo`, redirects (`>`), chaining (`&&`, `;`). Always proposed, never auto-run.
3. **Config rules** — your `deny`, `protect`, and `allow` lists in `~/.tether/config.json`.

Claude's output is **never trusted directly**. Claude influences what is proposed — the classifier decides what runs.

---

## Configuration

`~/.tether/config.json` — create with `tether config init`.

```jsonc
{
  "ask_lines": 200,          // lines fetched before relevance filtering
  "allow":   ["git", "go"],  // auto-run in Assist mode with auto-run enabled
  "protect": [],             // always propose, never auto-execute
  "deny":    []              // always block
}
```

---

<details>
<summary><strong>Command reference</strong></summary>

<br>

```
tether install                  Install shell integration (bash/zsh/fish)
tether uninstall                Remove shell integration and config files
tether shell                    Start your shell through the PTY proxy
tether chat                     Open the chat TUI
tether ask <question>           One-shot question, prints to stdout
tether mode [watch|assist]      Show or change the current mode
tether status                   Current session status, mode, session allow list
tether doctor                   Check all dependencies
tether tokens                   Show context buffer size
tether summary                  Print the rolling session summary
tether history                  Show conversation history with token stats
tether context                  Print current context that would be sent
tether clear                    Clear context buffers
tether config                   Show current config
tether config init              Write default config to ~/.tether/config.json
tether stop                     Stop the background daemon
tether version                  Print version
```

</details>

<details>
<summary><strong>Chat keybindings</strong></summary>

<br>

| Key | Action |
|-----|--------|
| `Enter` | Send message |
| `PgUp` / `PgDn` | Scroll history |
| `Ctrl+K` | Abort streaming response |
| `Ctrl+L` | Clear conversation |
| `Ctrl+C` | Close chat |
| `e` | Edit a proposed command |
| `a` | Allow command for the rest of the session |
| `x` / `Esc` | Reject a proposed command |
| `/debug` | Toggle debug info — keywords, line counts, token breakdown |
| `/clear` | Clear conversation history |
| `/handoff <task>` | Package session context and launch Claude Code with it pre-loaded |

</details>

---

## Privacy

Terminal output is held **in memory only** — never written to disk. The buffer is gone when your session ends.

Tether has no servers and collects no analytics. The only network traffic is the Claude Code CLI talking to Anthropic's API — the same as using Claude Code directly, governed by [Anthropic's privacy policy](https://www.anthropic.com/privacy).

> **Heads up:** If your session displays passwords or API keys, those could appear in context sent to Claude. Only run `tether shell` in sessions you'd be comfortable referencing with Claude.

<details>
<summary>Files written to disk</summary>

<br>

| Path | Contents |
|------|----------|
| `~/.tether/sessions/<pid>.sock` | Per-session IPC socket |
| `~/.tether/daemon.log` | Startup/stop events and errors |
| `~/.tether/conversation.json` | Chat history |
| `~/.tether/summary.txt` | Rolling session summary |
| `~/.tether/config.json` | Your config |

</details>

<details>
<summary>Auditing the data path</summary>

<br>

- Capture: [`internal/pty/proxy.go`](internal/pty/proxy.go)
- Buffering: [`internal/session/buffer.go`](internal/session/buffer.go)
- Filtering: [`internal/context/relevance.go`](internal/context/relevance.go)
- Prompt assembly: [`internal/conversation/conversation.go`](internal/conversation/conversation.go)
- Claude call: [`internal/chat/tui.go`](internal/chat/tui.go)

</details>

---

<div align="center">

MIT License — see [LICENSE](LICENSE)

</div>

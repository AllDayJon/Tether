# Tether

A terminal companion that keeps Claude in the loop while you work. You drive. Claude rides along. When you need help, it's already up to speed.

```
┌─────────────────────────┬────────────────────────────┐
│  your terminal (work)   │  tether chat               │
│                         │                            │
│  $ ssh prod-server      │  Claude                    │
│  $ tail -f app.log      │  That 502 is coming from   │
│  [502 Bad Gateway ...]  │  nginx upstream timeout.   │
│  $ systemctl status ... │  Check:                    │
│                         │  $ journalctl -u nginx -n  │
│                         │                            │
│                         │  [Enter] run  [x] reject   │
│                         │ > _                        │
└─────────────────────────┴────────────────────────────┘
```

## What it does

- **Watches your tmux panes** — captures terminal output continuously
- **Builds context automatically** — Claude sees what you've been doing before you even ask
- **Three modes** — Watch (advise only), Assist (propose + approve), Act (auto-execute allow-listed commands)
- **Works through SSH** — command injection via `tmux send-keys`, so remote sessions are transparent
- **Token-efficient** — sends only diffs, not full history; relevance-filters context per question

## Requirements

- Go 1.22+
- tmux
- [Claude Code CLI](https://claude.ai/code) (`claude` on PATH, logged in)

## Install

```sh
git clone <this repo>
cd tether
go install .
```

This puts `tether` on your PATH via `$GOPATH/bin`.

## Quick start

```sh
# Start a tmux session if you don't have one
tmux new -s work

# Start the tether daemon
tether start

# Watch your current pane
tether watch %0

# Open the chat split (right side of terminal)
tether chat

# Or set a keybind so prefix+g opens it from anywhere
tether keybind install --persist
```

## Commands

| Command | Description |
|---------|-------------|
| `tether start` | Start the background daemon |
| `tether stop` | Stop the daemon |
| `tether status` | Show daemon status, watched panes, mode |
| `tether watch <pane>` | Start watching a tmux pane (e.g. `%0`) |
| `tether unwatch <pane>` | Stop watching a pane |
| `tether chat` | Open chat TUI as a vertical split |
| `tether ask <question>` | One-shot question, prints answer to stdout |
| `tether mode` | Show current mode |
| `tether mode watch\|assist\|act` | Change mode |
| `tether tokens` | Show context size for the current buffer |
| `tether tail` | Stream pane output in real time |
| `tether summary` | Print the rolling session summary |
| `tether history` | Show conversation history with token stats |
| `tether config` | Show current config |
| `tether config init` | Write default config to `~/.tether/config.json` |
| `tether keybind install` | Install tmux keybinding (prefix+g) |
| `tether keybind remove` | Remove tether lines from `~/.tmux.conf` |
| `tether version` | Print version |

## Modes

### Watch (default)
Claude reads your terminal and answers questions. It cannot type anything. Safe to use anywhere.

### Assist
Claude proposes commands in a `\`\`\`bash` block. Each one is shown as a proposal:

```
Run this command?
$ journalctl -u nginx -n 50

[Enter] run  [e] edit  [x] reject
[a] allow 'journalctl' this session
```

Press `Enter` to run, `e` to edit first, `x` to skip, or `a` to allow that command for the rest of the session.

### Act
Commands on your allow list run automatically. Anything else is shown as a proposal. Use for repetitive tasks where you trust the context.

Switch modes:
```sh
tether mode assist
tether mode act
tether mode watch
```

## Security

Commands are gated by three layers of rules, evaluated in order:

1. **Hard deny** — fork bombs, pipe-to-shell (`curl ... | bash`), etc. Always blocked, no override.
2. **Hard protect** — `sudo`, redirects (`>`), pipes to `tee`, command chaining (`&&`, `;`). Always proposed, never auto-executed.
3. **Config lists** — `deny`, `protect`, `allow` in `~/.tether/config.json`.

Claude's output is **never trusted directly**. The classifier decides what happens to a command — Claude can only influence proposals, not bypass rules.

Kill switch: `Ctrl+K` in the chat window sends `Ctrl+C` to the work pane.

## Config

`~/.tether/config.json` — run `tether config init` to create it.

```json
{
  "chat_split_percent": 40,
  "chat_key": "g",
  "ask_lines": 200,
  "auto_watch": false,
  "allow": ["git", "go", "ls", "cat"],
  "protect": [],
  "deny": []
}
```

| Key | Default | Description |
|-----|---------|-------------|
| `chat_split_percent` | `40` | Width of chat pane as % of terminal |
| `chat_key` | `"g"` | tmux prefix key to open chat |
| `ask_lines` | `200` | Lines of context sent to `tether ask` |
| `auto_watch` | `false` | Auto-watch the launching pane on daemon start |
| `allow` | `[]` | Commands auto-executed in Act mode |
| `protect` | `[]` | Commands always proposed, never auto-executed |
| `deny` | `[]` | Commands always blocked |

## Chat keybindings

| Key | Action |
|-----|--------|
| `Enter` | Send message |
| `PgUp` / `PgDn` | Scroll history |
| `Ctrl+L` | Clear conversation |
| `Ctrl+K` | Send Ctrl+C to work pane (kill switch) |
| `Ctrl+C` | Close chat |

## Data

All data is stored in `~/.tether/`:

| File | Contents |
|------|----------|
| `tether.sock` | Daemon IPC socket |
| `daemon.pid` | Daemon PID |
| `daemon.log` | Daemon log |
| `conversation.json` | Chat history (persists across sessions) |
| `config.json` | User config |
| `chat-debug.log` | Context/token debug log (with `--debug`) |

## SSH sessions

Tether runs on your **local** machine. When you SSH into a remote host from a watched pane, tether keeps watching. When Claude proposes a command, it is injected into the pane via `tmux send-keys` — the keystrokes arrive at whatever is running in the pane, including your SSH session. No tether installation needed on remote hosts.

## License

MIT

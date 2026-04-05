# Changelog

All notable changes to Tether are documented here.

---

## [0.7.0] — 2026-04-05

### Added
- **Session allow list** — press `[a]` on any proposal to allow that command prefix for the rest of the daemon session; synced to daemon via IPC so it persists if you reopen the chat
- `tether doctor` — dependency checker; verifies tmux, claude CLI, and daemon connectivity
- `Makefile` with `build`, `install`, `test`, `vet`, `clean` targets
- Tests for `cmdguard` (classifier, Decide, ExtractBashBlocks) and `context` (relevance filtering)
- GitHub Actions CI — builds, vets, and tests on every push to main

### Changed
- Mode instructions prompt updated to tell Claude to output **exactly one** `bash` block — prevents multi-proposal floods
- Proposal hint split onto two lines so it no longer overflows on narrow splits
- `proposalH` in viewport calculation bumped from 7 → 8 to match
- Version bump to 0.7.0

---

## [0.6.0]

### Added
- **Watch / Assist / Act modes** with `tether mode` command
- `cmdguard` classifier — hard deny (fork bombs, pipe-to-shell), hard protect (sudo, redirects, chaining), config lists
- Proposal UI in chat — `[Enter]` run, `[e]` edit, `[x]` reject
- Kill switch `Ctrl+K` — sends `Ctrl+C` to the work pane
- `tether keybind install/remove/show` — manage tmux prefix bindings
- Mode badge in chat header (WATCH / ASSIST / ACT with colour coding)
- `--work-pane` flag; auto-detection from daemon watched panes
- Session allow IPC messages (`set_mode`, `add_session_allow`, `get_session_allow`)

### Changed
- `BuildPrompt` extended with optional `mode` argument
- `launchClaude` fetches current mode from daemon before each message
- Header dim text colour fixed to be readable on the blue-purple background

---

## [0.5.0]

### Added
- Config system — `~/.tether/config.json` with `tether config` / `tether config init`
- `tether summary` — print rolling session summary
- `tether history` — show conversation history with token stats
- `tether keybind` skeleton (initial version using `tether _chat`)
- `--debug` flag on `tether chat` — logs prompt and context stats to `~/.tether/chat-debug.log`
- Chat split percent configurable via `--percent` flag and `chat_split_percent` config key
- Version banner in `tether start` output

### Changed
- Default tmux keybind changed from `T` to `g` (avoids conflict with tmux clock)

---

## [0.4.0]

### Added
- **Smart context selection** — relevance filtering scores terminal lines against the question; only top-K are sent
- `context.SelectForQuestion` and `context.TruncateBlocks` (collapses long repeated sections)
- `tether tokens` — show context buffer size and line counts
- Token stats visible in chat header (`N msgs ~Ntok`)
- `NLines` increased from 50 → 200 in `tether ask`

---

## [0.3.0]

### Added
- Rolling session summary — 5-minute background timer summarises pane activity
- Conversation compaction — auto-compacts when history exceeds 20 messages or ~32K chars
- `tether tail` — stream pane output in real-time
- `tether context` — print current context that would be sent to Claude
- `tether clear` — clear ring buffers

---

## [0.2.0]

### Added
- **Chat TUI** (`tether chat`) — bubbletea-powered streaming chat as a vertical tmux split
- Conversation persistence (`~/.tether/conversation.json`)
- `tether history` skeleton
- `_chat` internal command (hidden) — launched by `tether chat` inside the split
- Auto-start daemon if not running when chat opens

---

## [0.1.0]

### Added
- Daemon with Unix socket IPC (`~/.tether/tether.sock`)
- `tether start / stop / status / ping`
- `tether watch / unwatch` — opt-in pane watching
- `tmux capture-pane` polling (750ms interval) with ring buffer
- `tether ask <question>` — one-shot Claude query with terminal context
- PID file and log file at `~/.tether/`

# Changelog

All notable changes to Tether are documented here.

---

## [0.9.0] — 2026-04-15

### Added
- **One-liner installer** — `curl -fsSL https://raw.githubusercontent.com/AllDayJon/Tether/main/install.sh | sh` detects OS and architecture, installs to `/usr/local/bin`, and runs shell integration setup automatically.
- **`/handoff` command** — packages your current session context (cwd, git state, recent errors, terminal output) and launches Claude Code with it pre-loaded. Use when you want to hand the task off to Claude Code while keeping full context.
- **macOS PATH warning** — `tether install` now warns when the binary location is not on PATH and prints the correct shell command to fix it.
- **CONTRIBUTING.md** and **SECURITY.md** — contribution guide and private vulnerability disclosure process.

### Changed
- Go module renamed from `tether` to `github.com/AllDayJon/Tether`.
- **Act mode removed** — the two-mode system is now Watch and Assist only. Assist mode's auto-run toggle (`[t]` in chat) replaces Act mode's behaviour.
- Website overhauled for end-user clarity: install flow, handoff callout, mode descriptions updated.

---

## [0.8.0] — 2026-04-06

### Added
- **PTY proxy** — `tether shell` replaces tmux pane watching entirely. Run your shell through Tether's PTY proxy; the daemon captures output with zero polling latency and proper line framing. tmux is no longer required.
- **OSC 133 shell integration** — `tether install` writes integration scripts for bash, zsh, and fish. OSC 133 semantic markers tag each prompt and command block, enabling block-aware context scoring.
- **Block-aware context scoring** — `expandToCommandBlocks` expands selected output lines to their surrounding command block (command + output together). Large blocks (>40 lines, e.g. `ps aux`) get a tight ±2 line window instead of full expansion, preventing token blowout.
- **Cross-turn deduplication** — lines already sent to Claude in the previous turn are tracked in `SentLines` and penalised (−3) in the scorer. Stale context doesn't crowd out new output; high-signal lines (errors) still re-surface if they score above the penalty.
- **Error signal boosting** — lines matching error/warning signals (`error`, `panic`, `failed`, `connection refused`, `timeout`, `permission denied`, etc.) receive a +3 score bonus, surfacing them even when the question keywords don't exactly match.
- **Token cost per message** — chat TUI shows `↑ ~Ntok ↓ ~Ntok` after each assistant message (estimated prompt and response tokens). Uses `formatTokens()` with `k` suffix for ≥1000.
- **`/debug` mode in chat** — type `/debug` to toggle a debug block after each response showing IPC diagnostics, selected keywords, context line counts (fetched vs. filtered), and a per-component token breakdown (system / history / summary / context).
- **History compression in prompts** — the last 3 full exchanges are included verbatim; older assistant responses are dropped to save tokens. Claude is informed of the omission count.
- **macOS support** — `tether install` detects macOS and writes to `~/.bash_profile` instead of `~/.bashrc`. Post-install message also reflects the correct file per platform.
- **Per-session Unix sockets** — each `tether shell` session registers its own socket path so multiple sessions can run concurrently. `tether chat` auto-discovers all active sessions.
- **Abort / retry / copy in TUI** — streaming responses can be cancelled mid-flight; previous responses have retry and copy-to-clipboard actions.
- **Markdown rendering** — assistant responses are rendered with lipgloss-styled markdown (bold, code blocks, bullet lists) rather than plain text.
- **Timestamps** — each message pair shows a timestamp in the chat view.

### Changed
- `tether chat` is now a standalone command — no tmux split required. Run it in any terminal alongside your `tether shell` session.
- Context pipeline moved out of `BuildPrompt` into `launchClaude` — `SelectForQuestion` and `TruncatePanes` are called in the TUI before building the prompt, giving the TUI full control and enabling dedup tracking.
- `bufio.Scanner` token limit increased from 64 KB to 4 MB in `ipc.Recv` — fixes "token too long" error when fetching large context over IPC.
- Per-line character cap of 500 chars — very long lines (e.g. minified JS, base64 blobs) are truncated with `…` to avoid token blowout.
- Context section hard cap of 20 000 chars (~5 k tokens) — stops at budget regardless of line count.
- Session summary capped at 1 000 chars in prompt — the summary is a brief narrative, not a transcript.
- System prompt updated from "tmux session" to "terminal session".
- `DefaultOptions`: TopK 150, LastN 30, MaxLines 200.
- `tether doctor` updated to check `tether shell` integration instead of tmux.

### Removed
- tmux dependency for core operation (tmux still works for splitting, but is not required for capture).
- `--debug` flag on `tether chat` replaced by the in-chat `/debug` slash command.

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

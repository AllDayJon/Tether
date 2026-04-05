package chat

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	tctx "tether/internal/context"
	"tether/internal/cmdguard"
	"tether/internal/config"
	"tether/internal/conversation"
	"tether/internal/ipc"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	headerStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("62")).
			Foreground(lipgloss.Color("230")).
			Padding(0, 1)

	headerDimStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("62")).
			Foreground(lipgloss.Color("189"))

	modeWatchStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("34")).
			Foreground(lipgloss.Color("255")).
			Bold(true).
			Padding(0, 1)

	modeAssistStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("214")).
			Foreground(lipgloss.Color("0")).
			Bold(true).
			Padding(0, 1)

	modeActStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("196")).
			Foreground(lipgloss.Color("255")).
			Bold(true).
			Padding(0, 1)

	userLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("86")).
			Bold(true)

	claudeLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("135")).
				Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	inputBorderStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.NormalBorder()).
				BorderTop(true).
				BorderForeground(lipgloss.Color("62"))

	cursorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("86"))

	proposalBorderStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("214")).
				Padding(0, 1)

	proposalActBorderStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("196")).
				Padding(0, 1)

	proposalCmdStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("230")).
				Bold(true)

	blockedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)
)

// ── Message types ─────────────────────────────────────────────────────────────

type chunkMsg string
type doneMsg struct{ err error }
type panesRefreshedMsg []string
type compactDoneMsg struct{}
type streamStartedMsg struct{ mode ipc.Mode }
type cmdExecutedMsg struct {
	paneID  string
	command string
	err     error
}
type sessionAllowedMsg struct {
	pattern string
	err     error
}

func refreshPanes() tea.Cmd {
	return func() tea.Msg {
		return panesRefreshedMsg(watchedPanesFromDaemon())
	}
}

// ── Model ─────────────────────────────────────────────────────────────────────

// Model is the bubbletea model for the chat TUI.
type Model struct {
	conv     *conversation.Conversation
	input    textinput.Model
	viewport viewport.Model

	width, height int
	content       string

	streaming   bool
	streamBuf   string
	chunkCh     chan string
	err         string
	currentMode ipc.Mode

	watchedPanes []string
	workPane     string // pane where commands are executed
	tmuxSocket   string // local tmux socket path

	// Proposal state — non-empty when waiting for approval.
	proposals    []string         // queue of commands to propose
	proposalEdit bool             // editing the command
	proposalInput textinput.Model // text input for editing

	cfg      config.Config
	debugLog string
	msgCount int
}

// New creates a chat Model.
func New(conv *conversation.Conversation, workPane, tmuxSocket, debugLog string) Model {
	cfg, _ := config.Load()

	ti := textinput.New()
	ti.Placeholder = "Ask anything... (Enter to send, Ctrl+C to quit)"
	ti.Focus()
	ti.CharLimit = 4096

	pi := textinput.New()
	pi.CharLimit = 1024

	vp := viewport.New(0, 0)
	vp.SetContent("")

	m := Model{
		conv:          conv,
		input:         ti,
		proposalInput: pi,
		viewport:      vp,
		workPane:      workPane,
		tmuxSocket:    tmuxSocket,
		debugLog:      debugLog,
		cfg:           cfg,
		currentMode:   ipc.ModeWatch,
	}
	m.renderContent()
	return m
}

// ── bubbletea interface ───────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, refreshPanes(), fetchCurrentModeAndAllow())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalcViewport()
		m.viewport.SetContent(m.content)
		m.viewport.GotoBottom()

	case tea.KeyMsg:
		// ── Proposal mode keys ────────────────────────────────────────────
		if len(m.proposals) > 0 {
			if m.proposalEdit {
				switch msg.Type {
				case tea.KeyEsc:
					m.proposalEdit = false
					m.proposalInput.Blur()
					m.input.Focus()
				case tea.KeyEnter:
					edited := strings.TrimSpace(m.proposalInput.Value())
					if edited != "" {
						cmds = append(cmds, execInPane(m.workPane, edited))
					}
					m.proposals = m.proposals[1:]
					m.proposalEdit = false
					m.proposalInput.Blur()
					m.input.Focus()
				case tea.KeyCtrlC:
					return m, tea.Quit
				default:
					var inputCmd tea.Cmd
					m.proposalInput, inputCmd = m.proposalInput.Update(msg)
					cmds = append(cmds, inputCmd)
				}
			} else {
				switch msg.String() {
				case "enter":
					cmds = append(cmds, execInPane(m.workPane, m.proposals[0]))
					m.proposals = m.proposals[1:]
				case "e", "E":
					m.proposalEdit = true
					m.proposalInput.SetValue(m.proposals[0])
					m.proposalInput.Focus()
					m.input.Blur()
				case "a", "A":
					// Allow the base command for the rest of this session.
					fields := strings.Fields(m.proposals[0])
					if len(fields) > 0 {
						baseCmd := fields[0]
						cmd := m.proposals[0]
						m.proposals = m.proposals[1:]
						// Update in-memory allow list immediately so future Decide() calls see it.
						found := false
						for _, existing := range m.cfg.Allow {
							if existing == baseCmd {
								found = true
								break
							}
						}
						if !found {
							m.cfg.Allow = append(m.cfg.Allow, baseCmd)
						}
						cmds = append(cmds, allowForSession(baseCmd))
						if m.workPane != "" {
							cmds = append(cmds, execInPane(m.workPane, cmd))
						}
					}
				case "x", "X", "esc":
					m.proposals = m.proposals[1:]
				case "ctrl+c":
					return m, tea.Quit
				}
			}
			return m, tea.Batch(cmds...)
		}

		// ── Normal mode keys ──────────────────────────────────────────────
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit

		case tea.KeyCtrlK:
			// Kill switch — interrupt whatever is running in the work pane.
			if m.workPane != "" {
				cmds = append(cmds, killPane(m.tmuxSocket, m.workPane))
			}

		case tea.KeyEnter:
			if m.streaming {
				break
			}
			question := strings.TrimSpace(m.input.Value())
			if question == "" {
				break
			}
			m.input.Reset()
			m.err = ""
			m.conv.Add("user", question)
			m.streamBuf = ""
			m.streaming = true
			m.msgCount++
			m.chunkCh = make(chan string, 512)
			m.renderContent()
			m.viewport.GotoBottom()
			cmds = append(cmds,
				m.launchClaude(question),
				listenToStream(m.chunkCh),
				refreshPanes(),
			)

		case tea.KeyCtrlL:
			m.conv.Clear()
			m.streamBuf = ""
			m.streaming = false
			m.proposals = nil
			m.err = ""
			m.renderContent()
			m.viewport.GotoBottom()

		case tea.KeyPgUp, tea.KeyPgDown:
			var vpCmd tea.Cmd
			m.viewport, vpCmd = m.viewport.Update(msg)
			cmds = append(cmds, vpCmd)

		default:
			var inputCmd tea.Cmd
			m.input, inputCmd = m.input.Update(msg)
			cmds = append(cmds, inputCmd)
		}

	case panesRefreshedMsg:
		m.watchedPanes = []string(msg)

	case modeStatusMsg:
		m.currentMode = msg.mode
		// Merge session allows from the daemon into the in-memory allow list.
		for _, pattern := range msg.sessionAllow {
			found := false
			for _, existing := range m.cfg.Allow {
				if existing == pattern {
					found = true
					break
				}
			}
			if !found {
				m.cfg.Allow = append(m.cfg.Allow, pattern)
			}
		}

	case streamStartedMsg:
		m.currentMode = msg.mode

	case chunkMsg:
		m.streamBuf += string(msg)
		m.renderContent()
		m.viewport.GotoBottom()
		cmds = append(cmds, listenToStream(m.chunkCh))

	case doneMsg:
		m.streaming = false
		if msg.err != nil {
			m.err = msg.err.Error()
		}

		// Extract bash blocks before clearing the stream buffer.
		blocks := cmdguard.ExtractBashBlocks(m.streamBuf)

		if m.streamBuf != "" {
			m.conv.Add("assistant", m.streamBuf)
			m.streamBuf = ""
		}

		// Process commands based on current mode.
		if m.currentMode != ipc.ModeWatch && len(blocks) > 0 {
			for _, block := range blocks {
				decision := cmdguard.Decide(block, string(m.currentMode), m.cfg.Allow, m.cfg.Protect, m.cfg.Deny)
				switch decision {
				case cmdguard.DecisionExecute:
					if m.workPane != "" {
						cmds = append(cmds, execInPane(m.workPane, block))
					} else {
						// No work pane — fall back to proposal so user can still see the command.
						m.proposals = append(m.proposals, block)
					}
				case cmdguard.DecisionPropose:
					m.proposals = append(m.proposals, block)
				case cmdguard.DecisionBlock:
					m.err = fmt.Sprintf("blocked: %q (denied by security rules)", block)
				}
			}
		}

		m.renderContent()
		m.viewport.GotoBottom()
		m.recalcViewport()
		cmds = append(cmds, compactAndSave(m.conv))

	case cmdExecutedMsg:
		if msg.err != nil {
			m.err = "exec failed: " + msg.err.Error()
		}

	case sessionAllowedMsg:
		if msg.err != nil {
			m.err = "session allow failed: " + msg.err.Error()
		}

	case compactDoneMsg:
		// nothing visual to update
	}

	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}
	parts := []string{
		m.headerView(),
		m.viewport.View(),
	}
	if len(m.proposals) > 0 {
		parts = append(parts, m.proposalView())
	}
	parts = append(parts, m.inputView())
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// ── View helpers ──────────────────────────────────────────────────────────────

func (m Model) headerView() string {
	// Mode badge.
	var modeBadge string
	switch m.currentMode {
	case ipc.ModeAssist:
		modeBadge = modeAssistStyle.Render("ASSIST")
	case ipc.ModeAct:
		modeBadge = modeActStyle.Render("ACT")
	default:
		modeBadge = modeWatchStyle.Render("WATCH")
	}

	panes := m.watchedPanes
	var status string
	if len(panes) == 0 {
		status = headerDimStyle.Render("no panes watched")
	} else {
		status = headerDimStyle.Render("watching: " + strings.Join(panes, ", "))
	}

	totalChars := 0
	for _, msg := range m.conv.Messages {
		totalChars += len(msg.Content)
	}
	totalChars += len(m.streamBuf)
	right := headerDimStyle.Render(fmt.Sprintf("%d msgs  ~%dtok", m.conv.Len(), totalChars/4))

	label := "tether"
	// Measure widths carefully to avoid overflow.
	labelW := lipgloss.Width(label)
	badgeW := lipgloss.Width(modeBadge)
	statusW := lipgloss.Width(status)
	rightW := lipgloss.Width(right)
	gap := m.width - labelW - badgeW - statusW - rightW - 5
	if gap < 1 {
		gap = 1
	}
	line := label + " " + modeBadge + "  " + status + strings.Repeat(" ", gap) + right
	return headerStyle.Width(m.width).Render(line)
}

func (m Model) proposalView() string {
	cmd := m.proposals[0]
	count := ""
	if len(m.proposals) > 1 {
		count = fmt.Sprintf(" (%d/%d)", 1, len(m.proposals))
	}

	var title, body, hint string
	bStyle := proposalBorderStyle
	if m.currentMode == ipc.ModeAct {
		bStyle = proposalActBorderStyle
	}

	if m.proposalEdit {
		title = "Edit command" + count
		body = m.proposalInput.View()
		hint = dimStyle.Render("[Enter] run  [Esc] cancel")
	} else {
		title = "Run this command?" + count
		body = proposalCmdStyle.Render("$ " + cmd)
		// Show why it needs approval, with session-allow option on a second line
		// so the hint never overflows on narrow splits.
		class := cmdguard.Classify(cmd, m.cfg.Allow, m.cfg.Protect, m.cfg.Deny)
		baseCmd := ""
		if fields := strings.Fields(cmd); len(fields) > 0 {
			baseCmd = fields[0]
		}
		line1 := "[Enter] run  [e] edit  [x] reject"
		var line2 string
		if baseCmd != "" && m.currentMode == ipc.ModeAct && class != cmdguard.ClassAllowed {
			line2 = fmt.Sprintf("[a] allow '%s' this session  (%s)", baseCmd, cmdguard.ClassLabel(class))
		} else if baseCmd != "" {
			line2 = fmt.Sprintf("[a] allow '%s' this session", baseCmd)
		}
		if line2 != "" {
			hint = dimStyle.Render(line1 + "\n" + line2)
		} else {
			hint = dimStyle.Render(line1)
		}
	}

	inner := title + "\n\n" + body + "\n\n" + hint
	return bStyle.Width(m.width - 2).Render(inner)
}

func (m Model) inputView() string {
	var hint string
	if m.streaming {
		hint = dimStyle.Render(" Claude is thinking...")
	} else if len(m.proposals) > 0 {
		hint = dimStyle.Render(" waiting for approval above ↑")
	} else if m.err != "" {
		hint = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(" " + m.err)
	} else if m.workPane == "" && m.currentMode != ipc.ModeWatch {
		hint = dimStyle.Render(" no work pane — commands will not execute")
	}

	prefix := "> "
	inputLine := prefix + m.input.View() + hint
	return inputBorderStyle.Width(m.width).Render(inputLine)
}

func (m *Model) renderContent() {
	var sb strings.Builder

	for _, msg := range m.conv.Messages {
		if msg.Role == "user" {
			sb.WriteString(userLabelStyle.Render("You"))
		} else {
			sb.WriteString(claudeLabelStyle.Render("Claude"))
		}
		sb.WriteByte('\n')
		sb.WriteString(msg.Content)
		sb.WriteString("\n\n")
	}

	if m.streaming || m.streamBuf != "" {
		sb.WriteString(claudeLabelStyle.Render("Claude"))
		sb.WriteByte('\n')
		sb.WriteString(m.streamBuf)
		if m.streaming {
			sb.WriteString(cursorStyle.Render("▊"))
		}
		sb.WriteString("\n\n")
	}

	if sb.Len() == 0 {
		sb.WriteString(dimStyle.Render("No messages yet. Type a question below.\n\nTip: Ctrl+L clears the conversation. Ctrl+K kills a running command."))
	}

	m.content = sb.String()
	m.viewport.SetContent(m.content)
}

// recalcViewport adjusts the viewport height based on current state.
func (m *Model) recalcViewport() {
	headerH := 1
	inputH := 2
	proposalH := 0
	if len(m.proposals) > 0 {
		proposalH = 8 // title + blank + cmd + blank + hint(2 lines) + borders
	}
	m.viewport.Width = m.width
	m.viewport.Height = m.height - headerH - inputH - proposalH
	if m.viewport.Height < 1 {
		m.viewport.Height = 1
	}
	m.input.Width = m.width - 2
}

// ── Streaming ─────────────────────────────────────────────────────────────────

func compactAndSave(conv *conversation.Conversation) tea.Cmd {
	return func() tea.Msg {
		conv.Save()
		if conv.ShouldCompact() {
			conv.Compact()
		}
		return compactDoneMsg{}
	}
}

func (m Model) launchClaude(question string) tea.Cmd {
	ch := m.chunkCh
	conv := m.conv
	debugLog := m.debugLog
	msgCount := m.msgCount
	return func() tea.Msg {
		mode := fetchCurrentMode()
		panes, sessionSummary := fetchDeltaContext()
		prompt := conv.BuildPrompt(question, panes, sessionSummary, string(mode))

		if debugLog != "" {
			writeDebugLog(debugLog, msgCount, question, panes, prompt)
		}

		go func() {
			defer close(ch)
			cmd := exec.Command("claude", "-p", prompt)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				ch <- "[error: " + err.Error() + "]"
				return
			}
			cmd.Stderr = nil
			if err := cmd.Start(); err != nil {
				ch <- "[error: " + err.Error() + "]"
				return
			}
			buf := make([]byte, 256)
			for {
				n, readErr := stdout.Read(buf)
				if n > 0 {
					ch <- string(buf[:n])
				}
				if readErr != nil {
					break
				}
			}
			cmd.Wait()
		}()

		return streamStartedMsg{mode: mode}
	}
}

func listenToStream(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		chunk, ok := <-ch
		if !ok {
			return doneMsg{}
		}
		return chunkMsg(chunk)
	}
}

// execInPane sends a command to the work pane via the daemon.
func execInPane(paneID, command string) tea.Cmd {
	return func() tea.Msg {
		conn, err := ipc.Dial()
		if err != nil {
			return cmdExecutedMsg{err: err}
		}
		defer conn.Close()
		if err := ipc.SendMsg(conn, ipc.TypeExecInPane, ipc.ExecInPanePayload{
			PaneID:  paneID,
			Command: command,
		}); err != nil {
			return cmdExecutedMsg{err: err}
		}
		var resp ipc.OKResp
		if err := ipc.Recv(conn, &resp); err != nil {
			return cmdExecutedMsg{err: err}
		}
		return cmdExecutedMsg{paneID: paneID, command: command}
	}
}

// allowForSession sends TypeAddSessionAllow to the daemon, persisting the pattern
// for the lifetime of the daemon process.
func allowForSession(pattern string) tea.Cmd {
	return func() tea.Msg {
		conn, err := ipc.Dial()
		if err != nil {
			return sessionAllowedMsg{pattern: pattern, err: err}
		}
		defer conn.Close()
		if err := ipc.SendMsg(conn, ipc.TypeAddSessionAllow, ipc.AddSessionAllowPayload{Pattern: pattern}); err != nil {
			return sessionAllowedMsg{pattern: pattern, err: err}
		}
		var resp ipc.OKResp
		if err := ipc.Recv(conn, &resp); err != nil {
			return sessionAllowedMsg{pattern: pattern, err: err}
		}
		return sessionAllowedMsg{pattern: pattern}
	}
}

// killPane sends Ctrl+C to the work pane to interrupt a running command.
func killPane(tmuxSocket, paneID string) tea.Cmd {
	return func() tea.Msg {
		args := []string{}
		if tmuxSocket != "" {
			args = append(args, "-S", tmuxSocket)
		}
		args = append(args, "send-keys", "-t", paneID, "C-c", "")
		exec.Command("tmux", args...).Run()
		return nil
	}
}

// ── Debug logging ─────────────────────────────────────────────────────────────

func writeDebugLog(path string, msgNum int, question string, panes []ipc.PaneContext, prompt string) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	defer f.Close()

	rawLines, rawChars := 0, 0
	for _, p := range panes {
		rawLines += len(p.Lines)
		for _, l := range p.Lines {
			rawChars += len(l) + 1
		}
	}
	filtered := tctx.SelectForQuestion(question, panes, tctx.DefaultOptions())
	sentLines, sentChars := 0, 0
	for _, p := range filtered {
		sentLines += len(p.Lines)
		for _, l := range p.Lines {
			sentChars += len(l) + 1
		}
	}
	lineSavePct := 0
	if rawLines > 0 {
		lineSavePct = 100 - (sentLines*100)/rawLines
	}

	bar := strings.Repeat("═", 60)
	thin := strings.Repeat("─", 60)
	fmt.Fprintf(f, "\n%s\n", bar)
	fmt.Fprintf(f, "[%s] MESSAGE #%d\n", time.Now().Format("15:04:05"), msgNum)
	fmt.Fprintf(f, "%s\n", thin)
	fmt.Fprintf(f, "context : fetched %d lines (~%d tok) → sent %d lines (~%d tok)  -%d%%\n",
		rawLines, rawChars/4, sentLines, sentChars/4, lineSavePct)
	fmt.Fprintf(f, "prompt  : %d chars  ~%d tokens\n", len(prompt), len(prompt)/4)
	fmt.Fprintf(f, "%s\n", thin)
	fmt.Fprintln(f, prompt)
	fmt.Fprintf(f, "%s\n", bar)
}

// ── Daemon helpers ────────────────────────────────────────────────────────────

func fetchDeltaContext() (panes []ipc.PaneContext, summary string) {
	conn, err := ipc.Dial()
	if err != nil {
		return nil, ""
	}
	defer conn.Close()
	if err := ipc.SendMsg(conn, ipc.TypeGetContext, ipc.GetContextPayload{DeltaOnly: true}); err != nil {
		return nil, ""
	}
	var resp ipc.ContextResp
	if err := ipc.Recv(conn, &resp); err != nil {
		return nil, ""
	}
	return resp.Panes, resp.Summary
}

type modeStatusMsg struct {
	mode         ipc.Mode
	sessionAllow []string
}

func fetchCurrentModeAndAllow() tea.Cmd {
	return func() tea.Msg {
		conn, err := ipc.Dial()
		if err != nil {
			return modeStatusMsg{mode: ipc.ModeWatch}
		}
		defer conn.Close()
		if err := ipc.SendMsg(conn, ipc.TypeStatus, nil); err != nil {
			return modeStatusMsg{mode: ipc.ModeWatch}
		}
		var resp ipc.StatusResp
		if err := ipc.Recv(conn, &resp); err != nil {
			return modeStatusMsg{mode: ipc.ModeWatch}
		}
		if resp.Mode == "" {
			resp.Mode = ipc.ModeWatch
		}
		return modeStatusMsg{mode: resp.Mode, sessionAllow: resp.SessionAllow}
	}
}

func fetchCurrentMode() ipc.Mode {
	conn, err := ipc.Dial()
	if err != nil {
		return ipc.ModeWatch
	}
	defer conn.Close()
	if err := ipc.SendMsg(conn, ipc.TypeStatus, nil); err != nil {
		return ipc.ModeWatch
	}
	var resp ipc.StatusResp
	if err := ipc.Recv(conn, &resp); err != nil {
		return ipc.ModeWatch
	}
	if resp.Mode == "" {
		return ipc.ModeWatch
	}
	return resp.Mode
}

func watchedPanesFromDaemon() []string {
	conn, err := ipc.Dial()
	if err != nil {
		return nil
	}
	defer conn.Close()
	if err := ipc.SendMsg(conn, ipc.TypeStatus, nil); err != nil {
		return nil
	}
	var resp ipc.StatusResp
	if err := ipc.Recv(conn, &resp); err != nil {
		return nil
	}
	return resp.WatchedPanes
}

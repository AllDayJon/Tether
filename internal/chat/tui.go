package chat

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"tether/internal/cmdguard"
	"tether/internal/config"
	tctx "tether/internal/context"
	"tether/internal/conversation"
	"tether/internal/ipc"
)

// ── Colors ────────────────────────────────────────────────────────────────────

const (
	cyan    = "#00d4ff"
	navy    = "#0d1b2a"
	dimCyan = "#007a99"
	white   = "#e8e8e8"
	dimGray = "#555555"
)

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	headerStyle = lipgloss.NewStyle().
			Background(lipgloss.Color(navy)).
			Foreground(lipgloss.Color(cyan)).
			Padding(0, 1)

	headerDimStyle = lipgloss.NewStyle().
			Background(lipgloss.Color(navy)).
			Foreground(lipgloss.Color(dimCyan))

	modeWatchStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#1a3a4a")).
			Foreground(lipgloss.Color(cyan)).
			Bold(true).
			Padding(0, 1)

	modeAssistStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#4a3a00")).
			Foreground(lipgloss.Color("#ffd700")).
			Bold(true).
			Padding(0, 1)

	modeActStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#4a0000")).
			Foreground(lipgloss.Color("#ff6060")).
			Bold(true).
			Padding(0, 1)

	userLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(cyan)).
			Bold(true)

	claudeLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(white)).
				Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(dimGray))

	sepStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#2a3a4a"))

	tsStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(dimGray))

	inputBorderStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.NormalBorder()).
				BorderTop(true).
				BorderForeground(lipgloss.Color(dimCyan))

	proposalBorderStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#ffd700")).
				Padding(0, 1)

	proposalActBorderStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#ff6060")).
				Padding(0, 1)

	proposalCmdStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(white)).
				Bold(true)

	blockedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff6060")).
			Bold(true)

	debugHeaderStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(dimCyan)).
				Bold(true)

	debugLineStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#444466"))

	spinnerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(cyan))
)

// ── Message types ─────────────────────────────────────────────────────────────

type chunkMsg string
type doneMsg struct{ err error }
type compactDoneMsg struct{}
type streamStartedMsg struct {
	mode         ipc.Mode
	ctxLineCount int
	cancel       context.CancelFunc
	debugBlock   string
	promptTokens int                 // estimated tokens in the full prompt sent to Claude
	newSentLines map[string]struct{} // lines sent this turn — stored for next turn's dedup
}
type cmdExecutedMsg struct {
	command string
	err     error
}
type sessionAllowedMsg struct {
	pattern string
	err     error
}
type clipboardMsg struct{ err error }

// ── Model ─────────────────────────────────────────────────────────────────────

type Model struct {
	conv     *conversation.Conversation
	input    textarea.Model
	viewport viewport.Model
	spin     spinner.Model

	width, height int
	content       string

	streaming    bool
	streamBuf    string
	chunkCh      chan string
	cancelStream context.CancelFunc
	err          string
	currentMode  ipc.Mode
	ctxLineCount int

	lastQuestion string
	lastResponse string

	// Per-message timestamps. Index matches conv.Messages; zero = no timestamp.
	msgTimestamps []time.Time

	// Per-assistant-message prompt token estimate. Index matches conv.Messages.
	// Zero means unknown. Stored so it survives after streaming ends.
	msgPromptTokens []int
	lastPromptTokens int // prompt tokens for the currently streaming response

	// Proposal state.
	proposals     []string
	proposalEdit  bool
	proposalInput textarea.Model

	// Input history navigation.
	inputHistory []string
	historyIdx   int // -1 = not browsing history

	// Glamour renderer cache — recreated only when viewport width changes.
	glamRenderer *glamour.TermRenderer
	glamWidth    int

	// Per-message render cache — index matches conv.Messages.
	// Set to nil to invalidate (width change, compaction).
	renderedMsgs []string

	cfg          config.Config
	debugLog     string
	debugMode    bool
	debugBlock   string
	sentLineSet  map[string]struct{} // lines sent in the last context — deprioritized next turn
	msgCount     int
}

func New(conv *conversation.Conversation, debugLog string) Model {
	cfg, _ := config.Load()

	ti := newTextarea("Ask anything...  (enter to send, ctrl+j for newline)")

	pi := newTextarea("")
	pi.CharLimit = 1024

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinnerStyle

	vp := viewport.New(0, 0)
	vp.SetContent("")

	m := Model{
		conv:          conv,
		input:         ti,
		proposalInput: pi,
		viewport:      vp,
		spin:          sp,
		debugLog:      debugLog,
		cfg:           cfg,
		currentMode:   ipc.ModeWatch,
		historyIdx:    -1,
	}
	m.renderContent()
	return m
}

func newTextarea(placeholder string) textarea.Model {
	ti := textarea.New()
	ti.Placeholder = placeholder
	ti.ShowLineNumbers = false
	ti.Prompt = ""
	ti.SetHeight(1)
	ti.CharLimit = 0
	ti.Focus()

	// Remove default borders — layout wraps it in inputBorderStyle.
	noStyle := lipgloss.NewStyle()
	ti.FocusedStyle.Base = noStyle
	ti.BlurredStyle.Base = noStyle
	ti.FocusedStyle.CursorLine = noStyle
	ti.BlurredStyle.CursorLine = noStyle
	ti.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color(dimGray))
	ti.BlurredStyle.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color(dimGray))

	// ctrl+j inserts a newline; enter is intercepted by Update for sending.
	ti.KeyMap.InsertNewline.SetKeys("ctrl+j", "shift+enter")

	return ti
}

// ── bubbletea interface ───────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, fetchCurrentModeAndAllow())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(m.width - 4)
		m.recalcViewport()
		m.renderContent()
		m.viewport.GotoBottom()

	case tea.MouseMsg:
		var vpCmd tea.Cmd
		m.viewport, vpCmd = m.viewport.Update(msg)
		cmds = append(cmds, vpCmd)

	case spinner.TickMsg:
		if m.streaming {
			var spinCmd tea.Cmd
			m.spin, spinCmd = m.spin.Update(msg)
			cmds = append(cmds, spinCmd)
		}

	case clipboardMsg:
		if msg.err != nil {
			m.err = "clipboard: " + msg.err.Error()
		} else {
			m.err = "copied to clipboard"
		}

	case tea.KeyMsg:
		// ── Proposal mode ─────────────────────────────────────────────────
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
						cmds = append(cmds, execCommand(edited))
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
					cmds = append(cmds, execCommand(m.proposals[0]))
					m.proposals = m.proposals[1:]
				case "e", "E":
					m.proposalEdit = true
					m.proposalInput.SetValue(m.proposals[0])
					m.proposalInput.Focus()
					m.input.Blur()
				case "a", "A":
					fields := strings.Fields(m.proposals[0])
					if len(fields) > 0 {
						baseCmd := fields[0]
						cmd := m.proposals[0]
						m.proposals = m.proposals[1:]
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
						cmds = append(cmds, allowForSession(baseCmd), execCommand(cmd))
					}
				case "x", "X", "esc":
					m.proposals = m.proposals[1:]
				case "ctrl+c":
					return m, tea.Quit
				}
			}
			return m, tea.Batch(cmds...)
		}

		// ── Normal mode ───────────────────────────────────────────────────
		switch msg.Type {

		case tea.KeyEsc:
			return m, tea.Quit

		case tea.KeyCtrlK:
			// Abort a streaming response mid-flight.
			if m.streaming && m.cancelStream != nil {
				m.cancelStream()
				m.cancelStream = nil
				m.streaming = false
				if m.streamBuf != "" {
					m.streamBuf += "\n[cancelled]"
					m.conv.Add("assistant", m.streamBuf)
					m.lastResponse = m.streamBuf
					m.streamBuf = ""
					m.renderedMsgs = nil
				}
				m.err = ""
				m.renderContent()
			}

		case tea.KeyCtrlC:
			if m.streaming && m.cancelStream != nil {
				// Cancel the running request, stay in chat.
				m.cancelStream()
				m.cancelStream = nil
				m.streaming = false
				if m.streamBuf != "" {
					m.streamBuf += "\n[cancelled]"
					m.conv.Add("assistant", m.streamBuf)
					m.lastResponse = m.streamBuf
					m.streamBuf = ""
					m.renderedMsgs = nil
				}
				m.err = ""
				m.renderContent()
			} else {
				return m, tea.Quit
			}

		case tea.KeyCtrlR:
			if !m.streaming && m.lastQuestion != "" {
				return m.sendQuestion(m.lastQuestion, cmds)
			}

		case tea.KeyCtrlY:
			if m.lastResponse != "" {
				cmds = append(cmds, copyToClipboard(m.lastResponse))
			}

		case tea.KeyCtrlL:
			m.conv.Clear()
			m.streamBuf = ""
			m.streaming = false
			m.proposals = nil
			m.err = ""
			m.debugBlock = ""
			m.sentLineSet = nil
			m.renderedMsgs = nil
			m.msgTimestamps = nil
			m.msgPromptTokens = nil
			m.renderContent()
			m.viewport.GotoBottom()

		case tea.KeyEnter:
			if m.streaming {
				break
			}
			question := strings.TrimSpace(m.input.Value())
			if question == "" {
				break
			}
			// Slash commands.
			if question == "/clear" {
				m.conv.Clear()
				m.streamBuf = ""
				m.proposals = nil
				m.err = ""
				m.debugBlock = ""
				m.sentLineSet = nil
				m.renderedMsgs = nil
				m.msgTimestamps = nil
				m.msgPromptTokens = nil
				m.input.Reset()
				m.historyIdx = -1
				m.renderContent()
				m.viewport.GotoBottom()
				break
			}
			if question == "/debug" {
				m.debugMode = !m.debugMode
				m.input.Reset()
				m.historyIdx = -1
				if m.debugMode {
					m.err = "debug mode ON — context details shown before each response"
				} else {
					m.err = "debug mode OFF"
					m.debugBlock = ""
				}
				m.renderContent()
				break
			}
			return m.sendQuestion(question, cmds)

		case tea.KeyUp:
			if !m.streaming && m.input.Value() == "" && len(m.inputHistory) > 0 {
				if m.historyIdx == -1 {
					m.historyIdx = len(m.inputHistory) - 1
				} else if m.historyIdx > 0 {
					m.historyIdx--
				}
				m.input.SetValue(m.inputHistory[m.historyIdx])
				break // don't pass up to textarea (would move cursor)
			}
			var inputCmd tea.Cmd
			m.input, inputCmd = m.input.Update(msg)
			cmds = append(cmds, inputCmd)
			m.recalcViewport()

		case tea.KeyDown:
			if !m.streaming && m.historyIdx >= 0 {
				m.historyIdx++
				if m.historyIdx >= len(m.inputHistory) {
					m.historyIdx = -1
					m.input.Reset()
				} else {
					m.input.SetValue(m.inputHistory[m.historyIdx])
				}
				break
			}
			var inputCmd tea.Cmd
			m.input, inputCmd = m.input.Update(msg)
			cmds = append(cmds, inputCmd)
			m.recalcViewport()

		case tea.KeyPgUp, tea.KeyPgDown:
			var vpCmd tea.Cmd
			m.viewport, vpCmd = m.viewport.Update(msg)
			cmds = append(cmds, vpCmd)

		default:
			if m.historyIdx >= 0 {
				m.historyIdx = -1
			}
			var inputCmd tea.Cmd
			m.input, inputCmd = m.input.Update(msg)
			cmds = append(cmds, inputCmd)
			m.recalcViewport()
		}

	case modeStatusMsg:
		m.currentMode = msg.mode
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
		m.ctxLineCount = msg.ctxLineCount
		m.cancelStream = msg.cancel
		m.debugBlock = msg.debugBlock
		m.lastPromptTokens = msg.promptTokens
		m.sentLineSet = msg.newSentLines

	case chunkMsg:
		if !m.streaming {
			break // cancelled; discard late chunks
		}
		m.streamBuf += string(msg)
		m.renderContent()
		m.viewport.GotoBottom()
		cmds = append(cmds, listenToStream(m.chunkCh))

	case doneMsg:
		if !m.streaming {
			break // already cancelled
		}
		m.streaming = false
		m.cancelStream = nil
		if msg.err != nil {
			m.err = msg.err.Error()
		}

		blocks := cmdguard.ExtractBashBlocks(m.streamBuf)

		if m.streamBuf != "" {
			m.lastResponse = m.streamBuf
			m.conv.Add("assistant", m.streamBuf)
			assistantIdx := len(m.conv.Messages) - 1
			m.msgTimestamps = appendTimestamp(m.msgTimestamps, assistantIdx, time.Now())
			m.msgPromptTokens = appendInt(m.msgPromptTokens, assistantIdx, m.lastPromptTokens)
			m.streamBuf = ""
		}

		if m.currentMode != ipc.ModeWatch && len(blocks) > 0 {
			for _, block := range blocks {
				decision := cmdguard.Decide(block, string(m.currentMode), m.cfg.Allow, m.cfg.Protect, m.cfg.Deny)
				switch decision {
				case cmdguard.DecisionExecute:
					cmds = append(cmds, execCommand(block))
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
		m.renderedMsgs = nil
		m.msgTimestamps = nil
		m.msgPromptTokens = nil
	}

	return m, tea.Batch(cmds...)
}

// sendQuestion is the shared logic for sending (Enter) and retrying (ctrl+r).
func (m Model) sendQuestion(question string, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	m.inputHistory = append(m.inputHistory, question)
	m.historyIdx = -1
	m.lastQuestion = question
	m.input.Reset()
	m.err = ""
	m.debugBlock = ""
	m.lastPromptTokens = 0
	m.conv.Add("user", question)
	// Timestamp the user message.
	m.msgTimestamps = appendTimestamp(m.msgTimestamps, len(m.conv.Messages)-1, time.Now())
	m.streamBuf = ""
	m.streaming = true
	m.msgCount++
	m.chunkCh = make(chan string, 512)
	m.renderContent()
	m.viewport.GotoBottom()
	cmds = append(cmds,
		m.launchClaude(question),
		listenToStream(m.chunkCh),
		m.spin.Tick,
	)
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
	var modeBadge string
	switch m.currentMode {
	case ipc.ModeAssist:
		modeBadge = modeAssistStyle.Render("ASSIST")
	case ipc.ModeAct:
		modeBadge = modeActStyle.Render("ACT")
	default:
		modeBadge = modeWatchStyle.Render("WATCH")
	}

	// Right side: scroll position, ctx lines, message count, token estimate.
	totalChars := 0
	for _, msg := range m.conv.Messages {
		totalChars += len(msg.Content)
	}
	totalChars += len(m.streamBuf)

	var rightParts []string
	pct := m.viewport.ScrollPercent()
	if pct < 0.999 && m.viewport.TotalLineCount() > m.viewport.Height {
		rightParts = append(rightParts, fmt.Sprintf("↑%d%%", int(pct*100)))
	}
	if m.ctxLineCount > 0 {
		rightParts = append(rightParts, fmt.Sprintf("%dctx", m.ctxLineCount))
	}
	rightParts = append(rightParts, fmt.Sprintf("%d msgs  ~%dtok", m.conv.Len(), totalChars/4))
	right := headerDimStyle.Render(strings.Join(rightParts, "  "))

	label := "tether"
	gap := m.width - lipgloss.Width(label) - 1 - lipgloss.Width(modeBadge) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	line := label + " " + modeBadge + strings.Repeat(" ", gap) + right
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
	var status string
	if m.streaming {
		status = dimStyle.Render(" " + m.spin.View() + " thinking...  ctrl+c to cancel")
	} else if len(m.proposals) > 0 {
		status = dimStyle.Render(" waiting for approval above ↑")
	} else if m.err != "" {
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff6060")).Render(" " + m.err)
	}

	inputContent := m.input.View()
	if status != "" {
		inputContent += "\n" + status
	}
	return inputBorderStyle.Width(m.width).Render(inputContent)
}

func (m *Model) renderContent() {
	ready := m.ensureRenderer()

	nMsgs := len(m.conv.Messages)
	if len(m.renderedMsgs) != nMsgs {
		next := make([]string, nMsgs)
		copy(next, m.renderedMsgs)
		m.renderedMsgs = next
	}
	// Sync per-message slice lengths.
	for len(m.msgTimestamps) < nMsgs {
		m.msgTimestamps = append(m.msgTimestamps, time.Time{})
	}
	for len(m.msgPromptTokens) < nMsgs {
		m.msgPromptTokens = append(m.msgPromptTokens, 0)
	}

	sep := sepStyle.Render(strings.Repeat("─", m.viewport.Width))

	var sb strings.Builder
	for i, msg := range m.conv.Messages {
		if m.renderedMsgs[i] == "" {
			if msg.Role == "assistant" && ready {
				m.renderedMsgs[i] = m.renderMarkdown(msg.Content)
			} else {
				m.renderedMsgs[i] = msg.Content
			}
		}

		// Label + optional timestamp.
		var label string
		ts := ""
		if i < len(m.msgTimestamps) && !m.msgTimestamps[i].IsZero() {
			ts = tsStyle.Render("  " + m.msgTimestamps[i].Format("15:04"))
		}
		if msg.Role == "user" {
			label = userLabelStyle.Render("You") + ts
		} else {
			label = claudeLabelStyle.Render("Claude") + ts
		}

		sb.WriteString(label)
		sb.WriteByte('\n')
		sb.WriteString(m.renderedMsgs[i])
		sb.WriteString("\n")

		// Token cost line — shown after each assistant message.
		if msg.Role == "assistant" {
			promptTok := m.msgPromptTokens[i]
			respTok := len(msg.Content) / 4
			if promptTok > 0 {
				sb.WriteString(tsStyle.Render(fmt.Sprintf("  ↑ ~%s tok  ↓ ~%s tok", formatTokens(promptTok), formatTokens(respTok))))
				sb.WriteString("\n")
			}
		}

		// Separator after each complete exchange (after assistant messages).
		if msg.Role == "assistant" && i < nMsgs-1 {
			sb.WriteString("\n")
			sb.WriteString(sep)
			sb.WriteString("\n")
		} else {
			sb.WriteString("\n")
		}
	}

	// Debug block — show context details before the response starts.
	if m.debugBlock != "" {
		sb.WriteString(m.debugBlock)
		sb.WriteString("\n")
	}

	// Streaming response — no caching, no glamour (content incomplete).
	if m.streaming || m.streamBuf != "" {
		sb.WriteString(claudeLabelStyle.Render("Claude"))
		sb.WriteByte('\n')
		sb.WriteString(m.streamBuf)
		if m.streaming {
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(cyan)).Render("▊"))
		}
		sb.WriteString("\n\n")
	}

	if sb.Len() == 0 {
		sb.WriteString(dimStyle.Render(
			"No messages yet. Type a question below.\n\n" +
				"  enter    send message\n" +
				"  ctrl+j   insert newline\n" +
				"  ctrl+r   retry last question\n" +
				"  ctrl+y   copy last response\n" +
				"  ctrl+l   clear conversation\n" +
				"  /clear   clear conversation\n" +
				"  /debug   toggle context debug info\n" +
				"  esc      close",
		))
	}

	m.content = sb.String()
	m.viewport.SetContent(m.content)
}

// ── Glamour ───────────────────────────────────────────────────────────────────

func (m *Model) ensureRenderer() bool {
	w := m.viewport.Width
	if w <= 0 {
		return false
	}
	if m.glamRenderer != nil && m.glamWidth == w {
		return true
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStylesFromJSONBytes([]byte(glamourStyle)),
		glamour.WithWordWrap(w),
	)
	if err != nil {
		return false
	}
	if m.glamWidth != w {
		m.renderedMsgs = nil
	}
	m.glamRenderer = r
	m.glamWidth = w
	return true
}

func (m *Model) renderMarkdown(content string) string {
	if m.glamRenderer == nil {
		return content
	}
	rendered, err := m.glamRenderer.Render(content)
	if err != nil {
		return content
	}
	return strings.TrimSpace(rendered)
}

const glamourStyle = `{
  "document":      { "margin": 0, "color": "#e8e8e8" },
  "paragraph":     { "block_suffix": "\n" },
  "block_quote":   { "indent": 1, "indent_token": "│ ", "color": "#888888" },
  "list":          { "level_indent": 2 },
  "h1": { "block_suffix": "\n", "color": "#00d4ff", "bold": true,  "prefix": "# "   },
  "h2": { "block_suffix": "\n", "color": "#00d4ff", "bold": true,  "prefix": "## "  },
  "h3": { "block_suffix": "\n", "color": "#007a99",               "prefix": "### " },
  "h4": { "color": "#007a99", "prefix": "#### "  },
  "h5": { "color": "#007a99", "prefix": "##### " },
  "h6": { "color": "#007a99", "prefix": "###### "},
  "strong": { "bold": true },
  "emph":   { "italic": true, "color": "#aaaaaa" },
  "hr":     { "color": "#007a99", "format": "\n─────────────────────────────\n\n" },
  "item":        { "block_prefix": "• " },
  "enumeration": { "block_prefix": ". " },
  "code": { "color": "#00d4ff", "bold": true },
  "code_block": {
    "color":            "#e8e8e8",
    "background_color": "#1a2a3a",
    "margin":           1,
    "indent":           1,
    "indent_token":     "  "
  },
  "link":      { "color": "#00d4ff" },
  "link_text": { "bold": true, "color": "#00d4ff" },
  "image_text":{ "color": "#555555" },
  "table": {
    "center_separator": "┼",
    "column_separator":  "│",
    "row_separator":     "─"
  }
}`

// ── Layout ────────────────────────────────────────────────────────────────────

func (m *Model) recalcViewport() {
	headerH := 1
	proposalH := 0
	if len(m.proposals) > 0 {
		proposalH = 8
	}

	// Input area: textarea height (1–4 lines) + status line + border.
	inputLines := strings.Count(m.input.Value(), "\n") + 1
	if inputLines > 4 {
		inputLines = 4
	}
	m.input.SetHeight(inputLines)
	statusH := 0
	if m.streaming || len(m.proposals) > 0 || m.err != "" {
		statusH = 1
	}
	inputH := inputLines + 1 + statusH // textarea + border-top + optional status

	m.viewport.Width = m.width
	m.viewport.Height = m.height - headerH - inputH - proposalH
	if m.viewport.Height < 1 {
		m.viewport.Height = 1
	}
	m.input.SetWidth(m.width - 2)
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
	debugMode := m.debugMode
	sentLineSet := m.sentLineSet
	msgCount := m.msgCount
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())

		mode := fetchCurrentMode()
		rawPanes, sessionSummary, fetchDiag := fetchDeltaContext()

		// Filter and truncate context here so we control exactly what's sent.
		opts := tctx.DefaultOptions()
		opts.SentLines = sentLineSet
		filtered := tctx.SelectForQuestion(question, rawPanes, opts)
		filtered = tctx.TruncatePanes(filtered)

		ctxLines := 0
		for _, p := range filtered {
			ctxLines += len(p.Lines)
		}

		// Build the set of sent lines for the next turn's dedup.
		newSentLines := make(map[string]struct{})
		for _, p := range filtered {
			for _, l := range p.Lines {
				newSentLines[l] = struct{}{}
			}
		}

		prompt := conv.BuildPrompt(question, filtered, sessionSummary, string(mode))
		promptTokens := len(prompt) / 4

		var dbgBlock string
		if debugMode {
			histTok := historyTokens(conv)
			sumTok := len(sessionSummary) / 4
			ctxTok := 0
			for _, p := range filtered {
				for _, l := range p.Lines {
					ctxTok += len(l)
				}
			}
			ctxTok /= 4
			costs := tokenCosts{
				system:  promptTokens - histTok - sumTok - ctxTok - len(question)/4,
				history: histTok,
				summary: sumTok,
				context: ctxTok,
				total:   promptTokens,
			}
			dbgBlock = formatDebugBlock(question, rawPanes, filtered, fetchDiag, costs)
		}

		if debugLog != "" {
			writeDebugLog(debugLog, msgCount, question, rawPanes, prompt)
		}

		go func() {
			defer close(ch)
			cmd := exec.CommandContext(ctx, "claude", "-p", prompt)
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

		return streamStartedMsg{mode: mode, ctxLineCount: ctxLines, cancel: cancel, debugBlock: dbgBlock, promptTokens: promptTokens, newSentLines: newSentLines}
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

// ── Clipboard ─────────────────────────────────────────────────────────────────

func copyToClipboard(text string) tea.Cmd {
	return func() tea.Msg {
		return clipboardMsg{err: clipboard.WriteAll(text)}
	}
}

// ── IPC helpers ───────────────────────────────────────────────────────────────

func execCommand(command string) tea.Cmd {
	return func() tea.Msg {
		conn, err := ipc.Dial()
		if err != nil {
			return cmdExecutedMsg{err: err}
		}
		defer conn.Close()
		if err := ipc.SendMsg(conn, ipc.TypeExec, ipc.ExecPayload{Command: command}); err != nil {
			return cmdExecutedMsg{err: err}
		}
		var resp ipc.OKResp
		if err := ipc.Recv(conn, &resp); err != nil {
			return cmdExecutedMsg{err: err}
		}
		return cmdExecutedMsg{command: command}
	}
}

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

// fetchDeltaContext fetches the full session buffer from every active tether
// shell and returns it as pane context. We send all available lines (up to the
// buffer cap) so the relevance filter in BuildPrompt can select whatever is
// most useful for the question — regardless of when it was run.
// diag is a human-readable diagnostic string for debug mode.
func fetchDeltaContext() (panes []ipc.PaneContext, summary, diag string) {
	sessDir, _ := ipc.SessionsDir()
	sessions := ipc.ListActiveSessions()
	diag = fmt.Sprintf("sessions dir: %s  |  active sessions found: %d", sessDir, len(sessions))

	for i, sess := range sessions {
		conn, err := ipc.DialSession(sess.SocketPath)
		if err != nil {
			diag += fmt.Sprintf("\n  session pid=%d: dial error: %v", sess.PID, err)
			continue
		}
		// NLines: 5000 — larger than the buffer cap, so we always get everything stored.
		err = ipc.SendMsg(conn, ipc.TypeGetContext, ipc.GetContextPayload{NLines: 5000})
		if err != nil {
			diag += fmt.Sprintf("\n  session pid=%d: send error: %v", sess.PID, err)
			conn.Close()
			continue
		}
		var resp ipc.ContextResp
		if err := ipc.Recv(conn, &resp); err != nil {
			diag += fmt.Sprintf("\n  session pid=%d: recv error: %v", sess.PID, err)
			conn.Close()
			continue
		}
		conn.Close()
		diag += fmt.Sprintf("\n  session pid=%d: %d lines buffered", sess.PID, len(resp.Lines))
		if len(resp.Lines) > 0 {
			paneID := "session"
			if len(sessions) > 1 {
				paneID = fmt.Sprintf("session-%d", i+1)
			}
			panes = append(panes, ipc.PaneContext{PaneID: paneID, Lines: resp.Lines})
		}
		if resp.Summary != "" && summary == "" {
			summary = resp.Summary
		}
	}
	return panes, summary, diag
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

// ── Timestamp helpers ─────────────────────────────────────────────────────────

func appendTimestamp(ts []time.Time, idx int, t time.Time) []time.Time {
	for len(ts) <= idx {
		ts = append(ts, time.Time{})
	}
	ts[idx] = t
	return ts
}

func appendInt(s []int, idx, val int) []int {
	for len(s) <= idx {
		s = append(s, 0)
	}
	s[idx] = val
	return s
}

// formatTokens formats a token count with a k suffix for thousands.
func formatTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// ── Debug helpers ─────────────────────────────────────────────────────────────

type tokenCosts struct {
	system  int
	history int
	summary int
	context int
	total   int
}

// historyTokens estimates the token cost of the conversation history as it
// will appear in the next prompt (excluding the current question).
func historyTokens(conv *conversation.Conversation) int {
	msgs := conv.Messages
	if len(msgs) > 0 && msgs[len(msgs)-1].Role == "user" {
		msgs = msgs[:len(msgs)-1] // exclude trailing current question
	}
	total := 0
	for _, m := range msgs {
		total += len(m.Content)
	}
	return total / 4
}

// formatDebugBlock builds a human-readable summary of what context was fetched
// and which lines the relevance filter selected to send to Claude.
func formatDebugBlock(question string, fetched, filtered []ipc.PaneContext, fetchDiag string, costs tokenCosts) string {
	var sb strings.Builder

	totalFetched := 0
	for _, p := range fetched {
		totalFetched += len(p.Lines)
	}
	totalFiltered := 0
	for _, p := range filtered {
		totalFiltered += len(p.Lines)
	}

	keywords := tctx.ExportKeywords(question)

	sb.WriteString(debugHeaderStyle.Render(fmt.Sprintf(
		"[debug] fetched %d lines from %d session(s)  →  selected %d lines  |  keywords: %s",
		totalFetched, len(fetched), totalFiltered,
		strings.Join(keywords, ", "),
	)))
	sb.WriteString("\n")

	// Token cost breakdown.
	if costs.total > 0 {
		sb.WriteString(debugLineStyle.Render(fmt.Sprintf(
			"  tokens: total ~%s  |  system ~%d  history ~%d  summary ~%d  context ~%d",
			formatTokens(costs.total), costs.system, costs.history, costs.summary, costs.context,
		)))
		sb.WriteString("\n")
	}

	if fetchDiag != "" {
		for _, line := range strings.Split(fetchDiag, "\n") {
			sb.WriteString(debugLineStyle.Render("  " + line))
			sb.WriteString("\n")
		}
	}

	for _, p := range filtered {
		sb.WriteString(debugHeaderStyle.Render(fmt.Sprintf("  [%s] selected lines:", p.PaneID)))
		sb.WriteString("\n")
		for _, line := range p.Lines {
			sb.WriteString(debugLineStyle.Render("    " + line))
			sb.WriteString("\n")
		}
	}
	if totalFiltered == 0 {
		sb.WriteString(debugLineStyle.Render("  (no lines selected — Claude will respond from conversation history only)"))
		sb.WriteString("\n")
	}

	return sb.String()
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

// Keep blockedStyle referenced.
var _ = blockedStyle

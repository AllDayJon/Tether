package conversation

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"tether/internal/ipc"
)

const (
	maxHistoryMessages  = 20 // total messages to consider from history
	fullExchangeCount   = 3  // last N exchanges (user+assistant pairs) to include in full
	compactMessageCount = 20 // compact when history hits this many messages
	compactCharCount    = 32000 // compact when history hits ~8K tokens (1 token ≈ 4 chars)
	chatSystemPrompt    = "You are Tether, a persistent terminal assistant embedded in the user's terminal session.\n\n" +
		"Guidelines:\n" +
		"- Be concise. The user is working in a terminal — skip preamble.\n" +
		"- Maintain context across the conversation. You remember what was said earlier.\n" +
		"- When suggesting commands, use ```bash blocks.\n" +
		"- You have access to the user's recent terminal output. Reference it directly.\n" +
		"- Do not use any tools or attempt to edit files. Respond with text only.\n" +
		"- If you don't have enough context to answer accurately, say so."
)

// Message is a single turn in the conversation.
type Message struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content string `json:"content"`
}

// Conversation holds the message history and persists it to disk.
type Conversation struct {
	Messages        []Message `json:"messages"`
	CompactionCount int       `json:"compaction_count,omitempty"`
	filePath        string
}

// DefaultPath returns ~/.tether/conversation.json.
func DefaultPath() (string, error) {
	dir, err := ipc.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "conversation.json"), nil
}

// Load reads a conversation from path, or returns an empty one if it doesn't exist.
func Load(path string) (*Conversation, error) {
	c := &Conversation{filePath: path}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("loading conversation: %w", err)
	}
	if err := json.Unmarshal(data, c); err != nil {
		// Corrupted file — start fresh rather than hard-failing.
		return &Conversation{filePath: path}, nil
	}
	return c, nil
}

// Add appends a message to the conversation.
func (c *Conversation) Add(role, content string) {
	c.Messages = append(c.Messages, Message{Role: role, Content: content})
}

// Save writes the conversation to disk.
func (c *Conversation) Save() error {
	if c.filePath == "" {
		return nil
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.filePath, data, 0600)
}

// Clear removes all messages from the conversation and deletes the file.
func (c *Conversation) Clear() error {
	c.Messages = nil
	if c.filePath != "" {
		os.Remove(c.filePath) // best-effort
	}
	return nil
}

// Len returns the number of messages in the conversation.
func (c *Conversation) Len() int {
	return len(c.Messages)
}

// ShouldCompact returns true when the conversation history is large enough
// that it should be replaced with a compact summary.
func (c *Conversation) ShouldCompact() bool {
	if len(c.Messages) < compactMessageCount {
		return false
	}
	total := 0
	for _, m := range c.Messages {
		total += len(m.Content)
	}
	return len(c.Messages) >= compactMessageCount || total >= compactCharCount
}

// Compact summarises the conversation history via Claude and replaces it with
// the summary. The summary is stored as a single assistant message so it flows
// naturally into future prompts.
func (c *Conversation) Compact() error {
	if len(c.Messages) == 0 {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("Summarize this conversation concisely so it can be used as context for continuing the discussion.\n")
	sb.WriteString("Include: key topics, decisions made, important facts established, and the current state.\n")
	sb.WriteString("Aim for 4-6 sentences. Be specific, not generic.\n\n")
	for _, msg := range c.Messages {
		if msg.Role == "user" {
			sb.WriteString("User: ")
		} else {
			sb.WriteString("Claude: ")
		}
		sb.WriteString(msg.Content)
		sb.WriteString("\n\n")
	}

	cmd := exec.Command("claude", "-p", sb.String())
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("compaction failed: %w", err)
	}

	summary := strings.TrimSpace(string(out))
	if summary == "" {
		return fmt.Errorf("compaction returned empty summary")
	}

	c.Messages = []Message{
		{Role: "assistant", Content: "[Conversation summary — earlier history compacted]\n" + summary},
	}
	c.CompactionCount++
	return c.Save()
}

// modeInstructions returns mode-specific instructions to append to the prompt.
func modeInstructions(mode string) string {
	switch mode {
	case "assist":
		return "[Mode: ASSIST]\n" +
			"When you want the user to run a command, wrap exactly ONE command in a ```bash code block.\n" +
			"Choose the single best command for the situation — do not offer multiple alternatives.\n" +
			"The user will be shown a proposal to approve or reject before anything executes.\n" +
			"Only put commands you actually intend to run in bash blocks — use plain text for explanations.\n"
	case "act":
		return "[Mode: ACT]\n" +
			"Commands you output in ```bash blocks will be executed automatically if they are on\n" +
			"the user's allow list. Commands not on the allow list will be shown as proposals.\n" +
			"Output exactly ONE ```bash block containing the single best command. Do not give alternatives.\n" +
			"Be deliberate — only include commands you are confident are correct for the current context.\n"
	default:
		return ""
	}
}

// BuildPrompt assembles the full prompt: system instructions + conversation
// history + session summary + terminal context + the new user question.
// summary is the rolling session summary from the daemon (may be empty).
// mode is optional ("watch", "assist", "act") — appends mode-specific instructions.
func (c *Conversation) BuildPrompt(question string, panes []ipc.PaneContext, summary string, mode ...string) string {
	var sb strings.Builder

	sb.WriteString(chatSystemPrompt)
	sb.WriteString("\n\n")

	if len(mode) > 0 && mode[0] != "" && mode[0] != "watch" {
		sb.WriteString(modeInstructions(mode[0]))
		sb.WriteString("\n")
	}

	// Conversation history. Exclude the trailing user message — it was just added
	// by sendQuestion and appears at the end as "[User's message]". Including it
	// here too would send the current question twice.
	history := c.Messages
	if len(history) > 0 && history[len(history)-1].Role == "user" {
		history = history[:len(history)-1]
	}
	if len(history) > maxHistoryMessages {
		history = history[len(history)-maxHistoryMessages:]
	}
	if len(history) > 0 {
		// Determine the cutoff index: messages at or after this index are "recent"
		// and included in full. Earlier messages get user-only compression.
		fullStart := len(history) - fullExchangeCount*2
		if fullStart < 0 {
			fullStart = 0
		}

		sb.WriteString("[Conversation so far]\n")
		compressed := 0
		for i, msg := range history {
			if i < fullStart && msg.Role == "assistant" {
				// Drop old assistant responses — saves tokens, context covered by summary.
				compressed++
				continue
			}
			if msg.Role == "user" {
				sb.WriteString("User: ")
			} else {
				sb.WriteString("Claude: ")
			}
			sb.WriteString(msg.Content)
			sb.WriteString("\n\n")
		}
		if compressed > 0 {
			// Let Claude know some responses were omitted.
			sb.WriteString(fmt.Sprintf("[Note: %d earlier assistant responses omitted to save context — ask if you need details from earlier.]\n\n", compressed))
		}
	}

	// Rolling session summary (narrative context). Cap at 1000 chars (~250 tokens)
	// — it's meant to be a brief narrative, not a transcript.
	const maxSummaryChars = 1000
	if summary != "" {
		if len(summary) > maxSummaryChars {
			summary = summary[:maxSummaryChars] + "…"
		}
		sb.WriteString("[Session summary]\n")
		sb.WriteString(summary)
		sb.WriteString("\n\n")
	}

	// Terminal context — panes should already be filtered and truncated by the caller.
	// Hard cap: stop writing context if we've already used 20k chars (~5k tokens).
	const maxContextChars = 20000
	ctxChars := 0
	hasContent := false
	for _, p := range panes {
		if len(p.Lines) > 0 {
			hasContent = true
			break
		}
	}
	if hasContent {
		sb.WriteString("[Recent terminal activity]\n")
		for _, p := range panes {
			if len(p.Lines) == 0 {
				continue
			}
			fmt.Fprintf(&sb, "Pane %s (%d lines):\n```\n", p.PaneID, len(p.Lines))
			for _, l := range p.Lines {
				if ctxChars+len(l) > maxContextChars {
					sb.WriteString("… context truncated (line budget exceeded) …\n")
					goto doneContext
				}
				sb.WriteString(l)
				sb.WriteByte('\n')
				ctxChars += len(l) + 1
			}
			sb.WriteString("```\n")
		}
	doneContext:
		sb.WriteString("```\n\n")
	}

	// The new message.
	sb.WriteString("[User's message]\n")
	sb.WriteString(question)

	return sb.String()
}

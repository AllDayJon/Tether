package claude

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	tctx "tether/internal/context"
	"tether/internal/ipc"
)

const systemPromptStr = "You are Tether, a terminal assistant. You passively observe the user's tmux terminal session and help when asked.\n\n" +
	"Guidelines:\n" +
	"- Be concise. The user is a sysadmin working in a terminal — they don't need lengthy preambles.\n" +
	"- Answer directly. Lead with the answer, then explain if needed.\n" +
	"- When suggesting commands, wrap them in ```bash blocks.\n" +
	"- You have access to the user's recent terminal output as context. Reference it directly.\n" +
	"- If the context doesn't contain enough information to answer, say so clearly."

// SystemPrompt returns the system prompt used for one-shot ask calls.
func SystemPrompt() string { return systemPromptStr }

// Ask sends a question to Claude via the `claude` CLI, streaming the response
// to w. Uses the user's Claude Code subscription — no API key required.
func Ask(ctx context.Context, question string, panes []ipc.PaneContext, w io.Writer, model string) error {
	prompt := BuildPrompt(question, panes)

	args := []string{"-p", prompt}
	if model != "" && model != DefaultModel {
		args = append(args, "--model", model)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Stdout = w
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("claude CLI error: %w", err)
	}
	return nil
}

// BuildPrompt assembles the full prompt: system instructions + terminal context + question.
// It applies relevance filtering to send only lines pertinent to the question.
func BuildPrompt(question string, panes []ipc.PaneContext) string {
	var sb strings.Builder

	sb.WriteString(systemPromptStr)
	sb.WriteString("\n\n")

	filtered := tctx.SelectForQuestion(question, panes, tctx.DefaultOptions())
	filtered = tctx.TruncatePanes(filtered)

	hasContent := false
	for _, p := range filtered {
		if len(p.Lines) > 0 {
			hasContent = true
			break
		}
	}
	if hasContent {
		sb.WriteString("[Terminal context]\n")
		for _, p := range filtered {
			if len(p.Lines) == 0 {
				continue
			}
			fmt.Fprintf(&sb, "\nPane %s (%d lines):\n```\n", p.PaneID, len(p.Lines))
			for _, l := range p.Lines {
				sb.WriteString(l)
				sb.WriteByte('\n')
			}
			sb.WriteString("```\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("[Question]\n")
	sb.WriteString(question)
	return sb.String()
}

// DefaultModel is a sentinel meaning "use claude CLI's default model".
const DefaultModel = ""

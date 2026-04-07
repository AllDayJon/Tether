package cmd

import (
	"fmt"
	"os"
	"strings"
	"tether/internal/claude"
	tctx "tether/internal/context"
	"tether/internal/conversation"
	"tether/internal/ipc"

	"github.com/spf13/cobra"
)

var tokensCmd = &cobra.Command{
	Use:   "tokens",
	Short: "Show estimated token breakdown for the next message",
	Long: `Breaks down how many tokens would be sent to Claude on the next ask or chat
message. Useful for understanding how much context each component contributes
and whether the minimisation techniques (delta, summary, compaction) are helping.

Token estimates use the approximation: 1 token ≈ 4 characters.`,
	RunE: runTokens,
}

func init() {
	rootCmd.AddCommand(tokensCmd)
}

func runTokens(cmd *cobra.Command, args []string) error {
	var fullPanes, deltaPanes []ipc.PaneContext
	var sessionSummary string

	conn, err := ipc.Dial()
	if err != nil {
		fmt.Fprintln(os.Stderr, "note: tether not running — context will be empty")
	} else {
		defer conn.Close()

		// Full context.
		if ipc.SendMsg(conn, ipc.TypeGetContext, ipc.GetContextPayload{NLines: 200}) == nil {
			var resp ipc.ContextResp
			if ipc.Recv(conn, &resp) == nil && len(resp.Lines) > 0 {
				fullPanes = []ipc.PaneContext{{PaneID: "session", Lines: resp.Lines}}
				sessionSummary = resp.Summary
			}
		}

		// Delta context — note: this advances the session buffer's delta cursor,
		// which may affect the next chat turn. A proper fix requires per-client
		// cursors in the IPC layer.
		conn2, err2 := ipc.Dial()
		if err2 == nil {
			defer conn2.Close()
			if ipc.SendMsg(conn2, ipc.TypeGetContext, ipc.GetContextPayload{DeltaOnly: true}) == nil {
				var resp ipc.ContextResp
				if ipc.Recv(conn2, &resp) == nil && len(resp.Lines) > 0 {
					deltaPanes = []ipc.PaneContext{{PaneID: "session", Lines: resp.Lines}}
				}
			}
		}
	}

	convPath, _ := conversation.DefaultPath()
	conv, _ := conversation.Load(convPath)
	if conv == nil {
		conv = &conversation.Conversation{}
	}

	placeholderQ := "(question)"
	fullPrompt := conv.BuildPrompt(placeholderQ, fullPanes, sessionSummary)
	deltaPrompt := conv.BuildPrompt(placeholderQ, deltaPanes, sessionSummary)

	filteredFull := tctx.SelectForQuestion(placeholderQ, fullPanes, tctx.DefaultOptions())
	filteredDelta := tctx.SelectForQuestion(placeholderQ, deltaPanes, tctx.DefaultOptions())

	sep := strings.Repeat("─", 50)
	fmt.Println(sep)
	fmt.Println("TOKEN ESTIMATE FOR NEXT MESSAGE")
	fmt.Println(sep)

	printSection := func(label, content string) {
		chars := len(content)
		tokens := chars / 4
		fmt.Printf("  %-28s %5d chars  ~%d tokens\n", label, chars, tokens)
	}

	printSection("System prompt", claude.SystemPrompt())

	histChars := 0
	for _, m := range conv.Messages {
		histChars += len(m.Content)
	}
	fmt.Printf("  %-28s %5d chars  ~%d tokens  (%d messages)\n",
		"Conversation history", histChars, histChars/4, conv.Len())

	printSection("Session summary", sessionSummary)

	fullContextChars := 0
	for _, p := range fullPanes {
		for _, l := range p.Lines {
			fullContextChars += len(l) + 1
		}
	}
	deltaContextChars := 0
	for _, p := range deltaPanes {
		for _, l := range p.Lines {
			deltaContextChars += len(l) + 1
		}
	}
	filteredFullLines := countLines(filteredFull)
	filteredDeltaLines := countLines(filteredDelta)
	rawFullLines := countLines(fullPanes)
	rawDeltaLines := countLines(deltaPanes)

	fullSavePct := 0
	if rawFullLines > 0 {
		fullSavePct = 100 - (filteredFullLines*100)/rawFullLines
	}
	deltaSavePct := 0
	if rawDeltaLines > 0 {
		deltaSavePct = 100 - (filteredDeltaLines*100)/rawDeltaLines
	}

	fmt.Printf("  %-28s %5d chars  ~%d tokens  (full, %d lines → %d after filter, -%d%%)\n",
		"Terminal context (full)", fullContextChars, fullContextChars/4,
		rawFullLines, filteredFullLines, fullSavePct)
	fmt.Printf("  %-28s %5d chars  ~%d tokens  (delta, %d lines → %d after filter, -%d%%)\n",
		"Terminal context (delta)", deltaContextChars, deltaContextChars/4,
		rawDeltaLines, filteredDeltaLines, deltaSavePct)

	fmt.Println(sep)

	fullTotal := len(fullPrompt)
	deltaTotal := len(deltaPrompt)
	fmt.Printf("  %-28s %5d chars  ~%d tokens\n", "TOTAL (full context)", fullTotal, fullTotal/4)
	fmt.Printf("  %-28s %5d chars  ~%d tokens\n", "TOTAL (delta context)", deltaTotal, deltaTotal/4)

	if fullTotal > 0 && deltaTotal < fullTotal {
		saving := 100 - (deltaTotal*100)/fullTotal
		fmt.Printf("\n  Delta saves ~%d%% vs full context.\n", saving)
	}

	if conv.ShouldCompact() {
		fmt.Printf("\n  Conversation is large — compaction will trigger after next response.\n")
	}

	fmt.Println(sep)
	return nil
}

func countLines(panes []ipc.PaneContext) int {
	n := 0
	for _, p := range panes {
		n += len(p.Lines)
	}
	return n
}

package cmd

import (
	"fmt"
	"strings"
	"tether/internal/claude"
	"tether/internal/ipc"

	"github.com/spf13/cobra"
)

var contextLines int

var contextCmd = &cobra.Command{
	Use:   "context",
	Short: "Show what context would be sent to Claude on the next ask",
	Long: `Dumps the full prompt that tether would send to Claude — system instructions,
terminal context from watched panes, and a placeholder question.
Useful for verifying the daemon is capturing the right output.`,
	RunE: runContext,
}

func init() {
	rootCmd.AddCommand(contextCmd)
	contextCmd.Flags().IntVarP(&contextLines, "lines", "n", 200, "number of lines to fetch from daemon (relevance filtering selects the best ones)")
}

func runContext(cmd *cobra.Command, args []string) error {
	conn, err := ipc.Dial()
	if err != nil {
		fmt.Println("daemon not running — start with `tether start`")
		return nil
	}
	defer conn.Close()

	if err := ipc.SendMsg(conn, ipc.TypeGetContext, ipc.GetContextPayload{NLines: contextLines}); err != nil {
		return fmt.Errorf("requesting context: %w", err)
	}

	var resp ipc.ContextResp
	if err := ipc.Recv(conn, &resp); err != nil {
		return fmt.Errorf("reading context: %w", err)
	}

	if len(resp.Panes) == 0 {
		fmt.Println("no panes being watched — use `tether watch <pane-id>` first")
		return nil
	}

	// Check if all panes are empty.
	totalLines := 0
	for _, p := range resp.Panes {
		totalLines += len(p.Lines)
	}
	if totalLines == 0 {
		fmt.Println("watching panes but no output captured yet — wait a moment and try again")
		return nil
	}

	prompt := claude.BuildPrompt("<your question here>", resp.Panes)

	// Print with a clear header/footer so it's easy to read.
	sep := strings.Repeat("─", 60)
	fmt.Println(sep)
	fmt.Println("PROMPT THAT WOULD BE SENT TO CLAUDE")
	fmt.Println(sep)
	fmt.Println(prompt)
	fmt.Println(sep)
	fmt.Printf("Total characters: %d\n", len(prompt))
	return nil
}

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
terminal context from the current session, and a placeholder question.
Useful for verifying that tether is capturing the right output.`,
	RunE: runContext,
}

func init() {
	rootCmd.AddCommand(contextCmd)
	contextCmd.Flags().IntVarP(&contextLines, "lines", "n", 200, "number of lines to fetch (relevance filtering selects the best ones)")
}

func runContext(cmd *cobra.Command, args []string) error {
	conn, err := ipc.Dial()
	if err != nil {
		fmt.Println("tether not running — start with: tether shell")
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

	if len(resp.Lines) == 0 {
		fmt.Println("no output captured yet — wait a moment and try again")
		return nil
	}

	panes := []ipc.PaneContext{{PaneID: "session", Lines: resp.Lines}}
	prompt := claude.BuildPrompt("<your question here>", panes)

	sep := strings.Repeat("─", 60)
	fmt.Println(sep)
	fmt.Println("PROMPT THAT WOULD BE SENT TO CLAUDE")
	fmt.Println(sep)
	fmt.Println(prompt)
	fmt.Println(sep)
	fmt.Printf("Total characters: %d\n", len(prompt))
	return nil
}

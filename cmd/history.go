package cmd

import (
	"fmt"
	"strings"
	"github.com/AllDayJon/Tether/internal/conversation"

	"github.com/spf13/cobra"
)

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "Show the current conversation history",
	Long: `Prints the full conversation history in a readable format.
Useful for reviewing what was discussed and what Claude knows about your session.`,
	RunE: runHistory,
}

func init() {
	rootCmd.AddCommand(historyCmd)
}

func runHistory(cmd *cobra.Command, args []string) error {
	path, err := conversation.DefaultPath()
	if err != nil {
		return err
	}

	conv, err := conversation.Load(path)
	if err != nil {
		return fmt.Errorf("loading conversation: %w", err)
	}

	if conv.Len() == 0 {
		fmt.Println("no conversation history — start chatting with `tether chat` or `tether ask`")
		return nil
	}

	sep := strings.Repeat("─", 60)
	fmt.Printf("%s\n", sep)
	fmt.Printf("CONVERSATION HISTORY  (%d messages", conv.Len())
	if conv.CompactionCount > 0 {
		fmt.Printf(", compacted %d time(s)", conv.CompactionCount)
	}
	fmt.Printf(")\n%s\n\n", sep)

	for i, msg := range conv.Messages {
		if msg.Role == "user" {
			fmt.Printf("You [%d]\n", i+1)
		} else {
			fmt.Printf("Claude [%d]\n", i+1)
		}
		fmt.Println(msg.Content)
		fmt.Println()
	}

	totalChars := 0
	for _, m := range conv.Messages {
		totalChars += len(m.Content)
	}
	fmt.Printf("%s\n", sep)
	fmt.Printf("  %d messages  |  %d chars  |  ~%d tokens\n", conv.Len(), totalChars, totalChars/4)
	if conv.ShouldCompact() {
		fmt.Println("  ⚠  conversation is large — compaction will trigger after next response")
	}

	return nil
}

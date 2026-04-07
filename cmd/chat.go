package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"tether/internal/chat"
	"tether/internal/conversation"
	"tether/internal/ipc"
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Open a standalone Claude chat window",
	Long: `Open the Claude chat TUI in the current terminal.

Works from any terminal — automatically connects to all running tether
shell sessions and aggregates their context. Useful for keeping a
dedicated chat window open while you work in other terminals.

Press ctrl+c or esc to close.`,
	RunE: runChat,
}

func init() {
	rootCmd.AddCommand(chatCmd)
}

func runChat(cmd *cobra.Command, args []string) error {
	sessions := ipc.ListActiveSessions()
	if len(sessions) == 0 {
		fmt.Println("No active tether shell sessions found.")
		fmt.Println("Start one with: tether shell")
		fmt.Println()
		fmt.Println("Opening chat without live context — conversation history still available.")
	}

	path, err := conversation.DefaultPath()
	if err != nil {
		return err
	}
	conv, err := conversation.Load(path)
	if err != nil {
		return err
	}

	p := tea.NewProgram(
		chat.New(conv, ""),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err = p.Run()
	return err
}

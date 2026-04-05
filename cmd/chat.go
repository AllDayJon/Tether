package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"tether/internal/chat"
	"tether/internal/config"
	"tether/internal/conversation"
	"tether/internal/ipc"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var (
	chatClearHistory bool
	chatSplitPercent int
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Open the chat pane as a vertical split",
	Long: `Open the Tether chat TUI as a vertical split alongside your current pane.

The chat maintains conversation history — Claude remembers what you said
earlier in the session. Terminal context from watched panes is automatically
included with each message.

Keybindings:
  Enter    send message
  PgUp/Dn  scroll history
  Ctrl+L   clear conversation
  Ctrl+C   close chat

Must be run from inside a tmux session.`,
	RunE: runChat,
}

// _chatCmd is the internal entrypoint that runs the bubbletea TUI.
// It is launched inside the tmux window created by chatCmd.
var _chatCmd = &cobra.Command{
	Use:    "_chat",
	Hidden: true,
	RunE:   runChatInternal,
}

func init() {
	rootCmd.AddCommand(chatCmd)
	rootCmd.AddCommand(_chatCmd)
	cfg, _ := config.Load()
	chatCmd.Flags().BoolVar(&chatClearHistory, "clear", false, "clear conversation history before opening")
	chatCmd.Flags().Bool("debug", false, "log prompts and context stats to ~/.tether/chat-debug.log")
	chatCmd.Flags().IntVarP(&chatSplitPercent, "percent", "p", cfg.ChatSplitPercent, "percentage of terminal width for the chat pane (overrides config)")
	_chatCmd.Flags().Bool("clear", false, "clear conversation history")
	_chatCmd.Flags().Bool("debug", false, "log prompts and context stats to ~/.tether/chat-debug.log")
	_chatCmd.Flags().String("work-pane", "", "tmux pane ID where commands are executed")
}

func runChat(cmd *cobra.Command, args []string) error {
	tmuxEnv := os.Getenv("TMUX")
	if tmuxEnv == "" {
		return fmt.Errorf("tether chat requires a tmux session — run `tmux new -s work` first")
	}
	socketPath := strings.SplitN(tmuxEnv, ",", 3)[0]

	debugFlag, _ := cmd.Flags().GetBool("debug")
	extraArgs := ""
	if chatClearHistory {
		extraArgs += " --clear"
	}
	if debugFlag {
		extraArgs += " --debug"
	}
	// Pass the current pane as the work pane — this is the pane commands execute in.
	if workPane := os.Getenv("TMUX_PANE"); workPane != "" {
		extraArgs += " --work-pane " + workPane
	}

	// Split the current pane vertically: chat on the right, work on the left.
	shellCmd := fmt.Sprintf("tether _chat%s", extraArgs)
	return exec.Command("tmux", "-S", socketPath,
		"split-window", "-h",
		"-p", fmt.Sprintf("%d", chatSplitPercent),
		shellCmd,
	).Run()
}

func runChatInternal(cmd *cobra.Command, args []string) error {
	clearFlag, _ := cmd.Flags().GetBool("clear")
	debugFlag, _ := cmd.Flags().GetBool("debug")
	workPane, _ := cmd.Flags().GetString("work-pane")

	if err := ipc.EnsureDir(); err != nil {
		return err
	}

	// Auto-start daemon if not running.
	if !isDaemonRunning() {
		if err := startDaemon(false, true); err != nil {
			// Non-fatal — chat works without the daemon, just no terminal context.
			fmt.Fprintf(os.Stderr, "warning: could not start daemon: %v\n", err)
		}
	}

	path, err := conversation.DefaultPath()
	if err != nil {
		return err
	}

	conv, err := conversation.Load(path)
	if err != nil {
		return err
	}

	if clearFlag {
		conv.Clear()
	}

	var debugLogPath string
	if debugFlag {
		debugLogPath, _ = ipc.ChatDebugLogPath()
	}

	tmuxSocket := ""
	if tmuxEnv := os.Getenv("TMUX"); tmuxEnv != "" {
		tmuxSocket = strings.SplitN(tmuxEnv, ",", 3)[0]
	}

	// If no work pane was passed, use the first watched pane that isn't ours.
	if workPane == "" {
		myPane := os.Getenv("TMUX_PANE")
		if conn, err := ipc.Dial(); err == nil {
			_ = ipc.SendMsg(conn, ipc.TypeStatus, nil)
			var resp ipc.StatusResp
			if ipc.Recv(conn, &resp) == nil {
				for _, p := range resp.WatchedPanes {
					if p != myPane {
						workPane = p
						break
					}
				}
			}
			conn.Close()
		}
	}

	p := tea.NewProgram(
		chat.New(conv, workPane, tmuxSocket, debugLogPath),
		tea.WithAltScreen(),
	)
	_, err = p.Run()
	return err
}

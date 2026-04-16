package cmd

import (
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/AllDayJon/Tether/internal/chat"
	"github.com/AllDayJon/Tether/internal/conversation"
	"github.com/AllDayJon/Tether/internal/daemon"
	"github.com/AllDayJon/Tether/internal/ipc"
	"github.com/AllDayJon/Tether/internal/pty"
	"github.com/AllDayJon/Tether/internal/session"
)

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Start a shell session with Tether context capture",
	Long: `Start your shell ($SHELL) inside Tether's PTY proxy.

All terminal output is captured in real time — no tmux required.
Press ctrl+\ at any time to open the Claude chat overlay.

To install shell integration (OSC 133 markers for richer context):
  tether install`,
	RunE: runShell,
}

func init() {
	rootCmd.AddCommand(shellCmd)
}

func runShell(cmd *cobra.Command, args []string) error {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	if err := ipc.EnsureDir(); err != nil {
		return err
	}

	fmt.Printf("tether v%s — context capture active  (ctrl+\\ to chat, ctrl+d to exit)\n", Version)

	buf := session.New()

	// overlayFn is called by the proxy when ctrl+\ is pressed.
	// It runs the bubbletea chat TUI inline, then returns so the shell resumes.
	// input is the pipe reader that gives bubbletea exclusive stdin.
	overlayFn := func(input io.Reader) {
		if err := runChatOverlay(buf, input); err != nil {
			fmt.Fprintf(os.Stderr, "\ntether: overlay error: %v\n", err)
		}
	}

	proxy := pty.New(shell, buf, overlayFn)
	if err := proxy.Start(); err != nil {
		return fmt.Errorf("starting shell: %w", err)
	}

	// execFn writes a command to the PTY shell (used by the IPC TypeExec handler).
	execFn := func(command string) error {
		return proxy.WriteToShell([]byte(command + "\n"))
	}

	// Run the IPC daemon server in the background. It serves tether ask,
	// tether status, tether stop, etc. from other terminal windows.
	go func() {
		if err := daemon.Run(buf, shell, execFn); err != nil {
			fmt.Fprintf(os.Stderr, "tether: daemon error: %v\n", err)
		}
	}()

	return proxy.Wait()
}

// runChatOverlay launches the bubbletea chat TUI in the current terminal.
// input is the pipe reader provided by the PTY proxy — bubbletea reads from it
// exclusively so it doesn't compete with the proxy for os.Stdin.
func runChatOverlay(buf *session.Buffer, input io.Reader) error {
	_ = buf

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
		tea.WithInput(input),
		tea.WithMouseCellMotion(),
	)
	_, err = p.Run()
	return err
}

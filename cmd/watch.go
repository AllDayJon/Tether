package cmd

import (
	"fmt"
	"os/exec"
	"strings"
	"tether/internal/ipc"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var watchCmd = &cobra.Command{
	Use:   "watch [pane-id]",
	Short: "Watch a tmux pane (interactive picker if no pane given)",
	Long: `Watch a tmux pane so the daemon captures its output.

With no arguments, opens an interactive picker showing all panes in the
current tmux session. Use arrow keys to move, space to toggle, enter to apply.

With a pane ID argument (e.g. %0), watches that pane directly.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWatch,
}

var unwatchCmd = &cobra.Command{
	Use:   "unwatch <pane-id>",
	Short: "Stop watching a tmux pane",
	Args:  cobra.ExactArgs(1),
	RunE:  runUnwatch,
}

func init() {
	rootCmd.AddCommand(unwatchCmd)
}

func runWatch(cmd *cobra.Command, args []string) error {
	if len(args) == 1 {
		return watchPane(args[0])
	}
	return runWatchPicker()
}

func watchPane(paneID string) error {
	conn, err := ipc.Dial()
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := ipc.SendMsg(conn, ipc.TypeWatch, ipc.WatchPayload{PaneID: paneID}); err != nil {
		return fmt.Errorf("sending watch request: %w", err)
	}
	var resp ipc.OKResp
	if err := ipc.Recv(conn, &resp); err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	fmt.Printf("watching pane %s\n", paneID)
	return nil
}

func runUnwatch(cmd *cobra.Command, args []string) error {
	paneID := args[0]
	conn, err := ipc.Dial()
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := ipc.SendMsg(conn, ipc.TypeUnwatch, ipc.WatchPayload{PaneID: paneID}); err != nil {
		return fmt.Errorf("sending unwatch request: %w", err)
	}
	var resp ipc.OKResp
	if err := ipc.Recv(conn, &resp); err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	fmt.Printf("stopped watching pane %s\n", paneID)
	return nil
}

// ── Interactive picker ────────────────────────────────────────────────────────

func runWatchPicker() error {
	socketPath, watchedSet, err := panesInfo()
	if err != nil {
		return err
	}

	// Get all panes from tmux.
	out, err := exec.Command("tmux", "-S", socketPath, "list-panes", "-a",
		"-F", "#{pane_id}\t#{pane_current_command}\t#{pane_width}x#{pane_height}\t#{window_name}",
	).Output()
	if err != nil {
		return fmt.Errorf("tmux list-panes: %w", err)
	}

	var items []pickerPane
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		items = append(items, pickerPane{
			id:      parts[0],
			command: parts[1],
			size:    parts[2],
			window:  parts[3],
			watched: watchedSet[parts[0]],
			isSSH:   looksLikeSSH(parts[1]),
		})
	}

	if len(items) == 0 {
		return fmt.Errorf("no panes found in tmux session")
	}

	m := newPickerModel(items)
	p := tea.NewProgram(m)
	result, err := p.Run()
	if err != nil {
		return err
	}

	final := result.(pickerModel)
	if final.quit {
		return nil
	}

	// Apply changes: watch newly selected, unwatch deselected.
	conn, err := ipc.Dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	for _, item := range final.items {
		if item.selected && !item.watched {
			ipc.SendMsg(conn, ipc.TypeWatch, ipc.WatchPayload{PaneID: item.id})
			var r ipc.OKResp
			ipc.Recv(conn, &r)
			fmt.Printf("watching %s (%s)\n", item.id, item.command)
		} else if !item.selected && item.watched {
			ipc.SendMsg(conn, ipc.TypeUnwatch, ipc.WatchPayload{PaneID: item.id})
			var r ipc.OKResp
			ipc.Recv(conn, &r)
			fmt.Printf("stopped watching %s\n", item.id)
		}
	}
	return nil
}

// ── Picker model ──────────────────────────────────────────────────────────────

type pickerPane struct {
	id, command, size, window string
	watched                   bool
	selected                  bool // current toggle state in the picker
	isSSH                     bool // command looks like an SSH session
}

// looksLikeSSH returns true for commands that suggest a remote connection.
func looksLikeSSH(command string) bool {
	for _, c := range []string{"ssh", "mosh", "telnet"} {
		if command == c {
			return true
		}
	}
	return false
}

type pickerModel struct {
	items  []pickerPane
	cursor int
	quit   bool
}

var (
	selectedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
	unselectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	cursorPaneStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Bold(true)
	sshBadgeStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
)

func newPickerModel(items []pickerPane) pickerModel {
	// Pre-select already-watched panes.
	for i, it := range items {
		items[i].selected = it.watched
	}
	return pickerModel{items: items}
}

func (m pickerModel) Init() tea.Cmd { return nil }

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.quit = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case " ":
			m.items[m.cursor].selected = !m.items[m.cursor].selected
		case "enter":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m pickerModel) View() string {
	var sb strings.Builder
	sb.WriteString("Select panes to watch  (↑↓ move · space toggle · enter apply · q quit)\n\n")

	for i, item := range m.items {
		checkbox := "[ ]"
		labelStyle := unselectedStyle
		if item.selected {
			checkbox = "[✓]"
			labelStyle = selectedStyle
		}

		badge := ""
		if item.isSSH {
			badge = " " + sshBadgeStyle.Render("[ssh]")
		}

		line := fmt.Sprintf("%s  %-4s  %-12s  %-10s  %s",
			checkbox, item.id, item.command, item.size, item.window)

		if i == m.cursor {
			line = cursorPaneStyle.Render("> "+line) + badge
		} else {
			line = "  " + labelStyle.Render(line) + badge
		}
		sb.WriteString(line + "\n")
	}

	sb.WriteString("\n" + unselectedStyle.Render("already watching: "))
	watching := []string{}
	for _, it := range m.items {
		if it.watched {
			watching = append(watching, it.id)
		}
	}
	if len(watching) == 0 {
		sb.WriteString(unselectedStyle.Render("none"))
	} else {
		sb.WriteString(unselectedStyle.Render(strings.Join(watching, ", ")))
	}

	return sb.String()
}

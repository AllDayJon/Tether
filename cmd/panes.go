package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"tether/internal/ipc"

	"github.com/spf13/cobra"
)

var panesCmd = &cobra.Command{
	Use:   "panes",
	Short: "List tmux panes and their watch status",
	Long: `List all tmux panes in the current session with their IDs, running
commands, sizes, and whether the daemon is watching them.

Use this to find pane IDs before running 'tether watch'.`,
	RunE: runPanes,
}

func init() {
	rootCmd.AddCommand(panesCmd)
}

func runPanes(cmd *cobra.Command, args []string) error {
	socketPath, watchedSet, err := panesInfo()
	if err != nil {
		return err
	}

	// List all panes: id, command, size, window name
	tmuxArgs := []string{"-S", socketPath, "list-panes", "-a",
		"-F", "#{pane_id}\t#{pane_current_command}\t#{pane_width}x#{pane_height}\t#{window_name}\t#{session_name}"}
	out, err := exec.Command("tmux", tmuxArgs...).Output()
	if err != nil {
		return fmt.Errorf("tmux list-panes failed: %w", err)
	}

	// Get buffer sizes from daemon (best-effort).
	bufSizes := map[string]int{}
	if conn, dialErr := ipc.Dial(); dialErr == nil {
		defer conn.Close()
		if ipc.SendMsg(conn, ipc.TypeStatus, nil) == nil {
			var resp ipc.StatusResp
			if ipc.Recv(conn, &resp) == nil {
				bufSizes = resp.BufferSizes
			}
		}
	}

	currentPane := os.Getenv("TMUX_PANE")

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PANE\tCOMMAND\tSIZE\tWINDOW\tWATCHED\tBUFFERED")
	fmt.Fprintln(w, "────\t───────\t────\t──────\t───────\t────────")

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 5)
		if len(parts) < 4 {
			continue
		}
		paneID, command, size, window := parts[0], parts[1], parts[2], parts[3]

		watched := ""
		buffered := ""
		if watchedSet[paneID] {
			watched = "yes"
			if n := bufSizes[paneID]; n > 0 {
				buffered = fmt.Sprintf("%d lines", n)
			}
		}

		marker := ""
		if paneID == currentPane {
			marker = " ←"
		}

		fmt.Fprintf(w, "%s%s\t%s\t%s\t%s\t%s\t%s\n",
			paneID, marker, command, size, window, watched, buffered)
	}
	w.Flush()
	return nil
}

// panesInfo returns the tmux socket path and a set of currently watched pane IDs.
func panesInfo() (socketPath string, watched map[string]bool, err error) {
	tmuxEnv := os.Getenv("TMUX")
	if tmuxEnv == "" {
		return "", nil, fmt.Errorf("not inside a tmux session")
	}
	socketPath = strings.SplitN(tmuxEnv, ",", 3)[0]

	watched = map[string]bool{}
	if conn, dialErr := ipc.Dial(); dialErr == nil {
		defer conn.Close()
		if ipc.SendMsg(conn, ipc.TypeStatus, nil) == nil {
			var resp ipc.StatusResp
			if ipc.Recv(conn, &resp) == nil {
				for _, id := range resp.WatchedPanes {
					watched[id] = true
				}
			}
		}
	}
	return socketPath, watched, nil
}

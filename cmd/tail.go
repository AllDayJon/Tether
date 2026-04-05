package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
	"tether/internal/ipc"
	"tether/internal/watcher"

	"github.com/spf13/cobra"
)

var tailLines int

var tailCmd = &cobra.Command{
	Use:   "tail [pane-id]",
	Short: "Live stream of what the daemon is capturing",
	Long: `Stream new lines from the daemon's ring buffer as they appear.
Defaults to the first watched pane if no pane-id is given.

Press Ctrl+C to stop.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTail,
}

func init() {
	rootCmd.AddCommand(tailCmd)
	tailCmd.Flags().IntVarP(&tailLines, "lines", "n", 20, "number of historical lines to show on start")
}

func runTail(cmd *cobra.Command, args []string) error {
	// Resolve pane ID.
	paneID := ""
	if len(args) > 0 {
		paneID = args[0]
	} else {
		// Default to first watched pane.
		conn, err := ipc.Dial()
		if err != nil {
			return fmt.Errorf("daemon not running — start with `tether start`")
		}
		ipc.SendMsg(conn, ipc.TypeStatus, nil)
		var resp ipc.StatusResp
		ipc.Recv(conn, &resp)
		conn.Close()
		if len(resp.WatchedPanes) == 0 {
			return fmt.Errorf("no panes being watched — run `tether watch <pane-id>` first")
		}
		paneID = resp.WatchedPanes[0]
	}

	fmt.Fprintf(os.Stderr, "tailing pane %s — Ctrl+C to stop\n\n", paneID)

	// Handle Ctrl+C gracefully.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()

	var prev []string

	for {
		select {
		case <-sigCh:
			return nil
		case <-ticker.C:
			curr := fetchPaneLines(paneID, 500)
			if len(curr) == 0 {
				continue
			}

			if prev == nil {
				// First poll: show last N lines as history.
				start := len(curr) - tailLines
				if start < 0 {
					start = 0
				}
				for _, l := range curr[start:] {
					fmt.Println(l)
				}
			} else {
				// Print only lines that are new since last poll.
				for _, l := range watcher.NewLinesSince(prev, curr) {
					fmt.Println(l)
				}
			}
			prev = curr
		}
	}
}

// fetchPaneLines asks the daemon for up to n lines from paneID.
func fetchPaneLines(paneID string, n int) []string {
	conn, err := ipc.Dial()
	if err != nil {
		return nil
	}
	defer conn.Close()
	if err := ipc.SendMsg(conn, ipc.TypeGetContext, ipc.GetContextPayload{NLines: n}); err != nil {
		return nil
	}
	var resp ipc.ContextResp
	if err := ipc.Recv(conn, &resp); err != nil {
		return nil
	}
	for _, p := range resp.Panes {
		if p.PaneID == paneID {
			return p.Lines
		}
	}
	return nil
}


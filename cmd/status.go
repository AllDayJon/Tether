package cmd

import (
	"fmt"
	"tether/internal/ipc"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status",
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	conn, err := ipc.Dial()
	if err != nil {
		fmt.Println("tether: not running")
		return nil
	}
	defer conn.Close()

	if err := ipc.SendMsg(conn, ipc.TypeStatus, nil); err != nil {
		return fmt.Errorf("sending status request: %w", err)
	}

	var resp ipc.StatusResp
	if err := ipc.Recv(conn, &resp); err != nil {
		return fmt.Errorf("reading status: %w", err)
	}

	fmt.Println("tether: running")
	fmt.Printf("  tmux socket:  %s\n", resp.TmuxSocket)
	fmt.Printf("  tmux session: %s\n", resp.TmuxSession)

	if len(resp.WatchedPanes) == 0 {
		fmt.Println("  watching:     (none — use `tether watch <pane-id>`)")
	} else {
		fmt.Println("  watching:")
		for _, id := range resp.WatchedPanes {
			fmt.Printf("    %s  (%d lines buffered)\n", id, resp.BufferSizes[id])
		}
	}
	return nil
}

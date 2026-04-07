package cmd

import (
	"fmt"
	"tether/internal/ipc"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show tether status",
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	conn, err := ipc.Dial()
	if err != nil {
		fmt.Println("tether: not running  (start with: tether shell)")
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
	fmt.Printf("  shell:   %s\n", resp.Shell)
	fmt.Printf("  mode:    %s\n", resp.Mode)
	fmt.Printf("  buffer:  %d lines\n", resp.BufferedLines)
	if len(resp.SessionAllow) > 0 {
		fmt.Printf("  session allow: %v\n", resp.SessionAllow)
	}
	return nil
}

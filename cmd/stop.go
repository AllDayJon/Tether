package cmd

import (
	"fmt"
	"github.com/AllDayJon/Tether/internal/ipc"

	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the tether daemon",
	RunE:  runStop,
}

func runStop(cmd *cobra.Command, args []string) error {
	conn, err := ipc.Dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := ipc.SendMsg(conn, ipc.TypeStop, nil); err != nil {
		return fmt.Errorf("sending stop: %w", err)
	}

	var resp ipc.OKResp
	if err := ipc.Recv(conn, &resp); err != nil {
		// Daemon may close the connection before we read — that's fine.
		fmt.Println("tether stopped")
		return nil
	}

	fmt.Println("tether stopped")
	return nil
}

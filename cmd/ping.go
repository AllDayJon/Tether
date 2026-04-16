package cmd

import (
	"fmt"
	"time"
	"github.com/AllDayJon/Tether/internal/ipc"

	"github.com/spf13/cobra"
)

var pingCmd = &cobra.Command{
	Use:   "ping",
	Short: "Check tether connectivity and health",
	RunE:  runPing,
}

func init() {
	rootCmd.AddCommand(pingCmd)
}

func runPing(cmd *cobra.Command, args []string) error {
	sockPath, err := ipc.SocketPath()
	if err != nil {
		return err
	}
	fmt.Printf("socket:  %s\n", sockPath)

	start := time.Now()
	conn, err := ipc.Dial()
	if err != nil {
		fmt.Println("tether:  not running")
		return nil
	}
	defer conn.Close()

	if err := ipc.SendMsg(conn, ipc.TypeStatus, nil); err != nil {
		fmt.Printf("tether:  connected but unresponsive (%s)\n", time.Since(start).Round(time.Millisecond))
		return nil
	}

	var resp ipc.StatusResp
	if err := ipc.Recv(conn, &resp); err != nil {
		fmt.Printf("tether:  connected but bad response (%s)\n", time.Since(start).Round(time.Millisecond))
		return nil
	}
	rtt := time.Since(start).Round(time.Millisecond)

	fmt.Printf("tether:  ok (%s)\n", rtt)
	fmt.Printf("shell:   %s\n", resp.Shell)
	fmt.Printf("mode:    %s\n", resp.Mode)
	fmt.Printf("buffer:  %d lines\n", resp.BufferedLines)
	return nil
}

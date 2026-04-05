package cmd

import (
	"fmt"
	"strings"
	"time"
	"tether/internal/ipc"

	"github.com/spf13/cobra"
)

var pingCmd = &cobra.Command{
	Use:   "ping",
	Short: "Check daemon connectivity and health",
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
		fmt.Println("daemon:  not running")
		return nil
	}
	defer conn.Close()

	if err := ipc.SendMsg(conn, ipc.TypeStatus, nil); err != nil {
		fmt.Printf("daemon:  connected but unresponsive (%s)\n", time.Since(start).Round(time.Millisecond))
		return nil
	}

	var resp ipc.StatusResp
	if err := ipc.Recv(conn, &resp); err != nil {
		fmt.Printf("daemon:  connected but bad response (%s)\n", time.Since(start).Round(time.Millisecond))
		return nil
	}
	rtt := time.Since(start).Round(time.Millisecond)

	fmt.Printf("daemon:  ok (%s)\n", rtt)
	fmt.Printf("session: %s  (socket: %s)\n", resp.TmuxSession, resp.TmuxSocket)

	if len(resp.WatchedPanes) == 0 {
		fmt.Println("watching: (none)")
	} else {
		totalLines := 0
		for _, n := range resp.BufferSizes {
			totalLines += n
		}
		fmt.Printf("watching: %s  (%d lines buffered total)\n",
			strings.Join(resp.WatchedPanes, ", "), totalLines)
	}
	return nil
}

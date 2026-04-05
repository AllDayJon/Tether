package cmd

import (
	"fmt"
	"tether/internal/ipc"

	"github.com/spf13/cobra"
)

var summaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Show the current rolling session summary",
	Long: `Prints the session summary the daemon has built from your terminal activity.
The summary is regenerated every 5 minutes and captures what you've been doing
in plain English — useful for checking what context Claude has about your session.`,
	RunE: runSummary,
}

func init() {
	rootCmd.AddCommand(summaryCmd)
}

func runSummary(cmd *cobra.Command, args []string) error {
	conn, err := ipc.Dial()
	if err != nil {
		return fmt.Errorf("daemon not running — start with `tether start`")
	}
	defer conn.Close()

	if err := ipc.SendMsg(conn, ipc.TypeGetContext, ipc.GetContextPayload{NLines: 1}); err != nil {
		return fmt.Errorf("requesting summary: %w", err)
	}
	var resp ipc.ContextResp
	if err := ipc.Recv(conn, &resp); err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.Summary == "" {
		fmt.Println("no summary yet — the daemon generates one every 5 minutes once there is terminal activity")
		return nil
	}

	fmt.Println(resp.Summary)
	return nil
}

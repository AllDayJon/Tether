package cmd

import (
	"fmt"
	"tether/internal/ipc"

	"github.com/spf13/cobra"
)

// execCmd is a hidden debug command for testing send-keys execution.
// This is the building block for Phase 4 Assist/Act mode.
var execCmd = &cobra.Command{
	Use:    "exec <pane-id> <command>",
	Short:  "Inject a command into a watched pane (debug)",
	Hidden: true,
	Args:   cobra.ExactArgs(2),
	RunE:   runExec,
}

func init() {
	rootCmd.AddCommand(execCmd)
}

func runExec(cmd *cobra.Command, args []string) error {
	paneID := args[0]
	command := args[1]

	conn, err := ipc.Dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := ipc.SendMsg(conn, ipc.TypeExecInPane, ipc.ExecInPanePayload{
		PaneID:  paneID,
		Command: command,
	}); err != nil {
		return fmt.Errorf("sending exec request: %w", err)
	}

	var resp ipc.OKResp
	if err := ipc.Recv(conn, &resp); err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	fmt.Printf("sent to pane %s: %s\n", paneID, command)
	return nil
}

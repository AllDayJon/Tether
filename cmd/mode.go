package cmd

import (
	"fmt"
	"github.com/AllDayJon/Tether/internal/ipc"

	"github.com/spf13/cobra"
)

var modeCmd = &cobra.Command{
	Use:   "mode [watch|assist]",
	Short: "Show or set the current mode (watch/assist)",
	Long: `Show or change the mode that controls how tether handles commands Claude suggests.

  watch   Claude observes and advises only. No commands are ever executed. (default)
  assist  Claude proposes commands. You see each one and press Enter to run, e to edit,
          or x to reject. Press [t] in the TUI to enable auto-run, which automatically
          executes commands on your allow list without prompting.

The mode is stored in the daemon and resets to 'watch' when the daemon restarts
unless you set a default in ~/.tether/config.json.

Examples:
  tether mode            # show current mode
  tether mode assist     # switch to assist mode
  tether mode watch      # switch back to watch`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMode,
}

func init() {
	rootCmd.AddCommand(modeCmd)
}

func runMode(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return showMode()
	}
	return setMode(ipc.Mode(args[0]))
}

func showMode() error {
	conn, err := ipc.Dial()
	if err != nil {
		return fmt.Errorf("daemon not running — start with `tether start`")
	}
	defer conn.Close()

	if err := ipc.SendMsg(conn, ipc.TypeStatus, nil); err != nil {
		return err
	}
	var resp ipc.StatusResp
	if err := ipc.Recv(conn, &resp); err != nil {
		return err
	}

	mode := resp.Mode
	if mode == "" {
		mode = ipc.ModeWatch
	}

	fmt.Printf("current mode: %s\n", mode)
	fmt.Println()
	switch mode {
	case ipc.ModeWatch:
		fmt.Println("  Claude observes and advises. No commands execute through tether.")
	case ipc.ModeAssist:
		fmt.Println("  Claude proposes commands. You approve each one before it runs.")
		fmt.Println("  Press [t] in the chat TUI to enable auto-run for allow-listed commands.")
	}
	return nil
}

func setMode(mode ipc.Mode) error {
	switch mode {
	case ipc.ModeWatch, ipc.ModeAssist:
		// valid
	default:
		return fmt.Errorf("unknown mode %q — use watch or assist", mode)
	}

	conn, err := ipc.Dial()
	if err != nil {
		return fmt.Errorf("daemon not running — start with `tether start`")
	}
	defer conn.Close()

	if err := ipc.SendMsg(conn, ipc.TypeSetMode, ipc.SetModePayload{Mode: mode}); err != nil {
		return err
	}
	var resp ipc.OKResp
	if err := ipc.Recv(conn, &resp); err != nil {
		return err
	}

	fmt.Printf("mode set to %s\n", mode)
	return nil
}

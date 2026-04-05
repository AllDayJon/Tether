package cmd

// daemonCmd is the internal entrypoint invoked by `tether start`.
// It is hidden from the help output — users never call it directly.

import (
	"fmt"
	"os"
	"tether/internal/daemon"

	"github.com/spf13/cobra"
)

var (
	daemonTmuxSocket  string
	daemonTmuxSession string
)

var daemonCmd = &cobra.Command{
	Use:    "_daemon",
	Hidden: true,
	Short:  "Run the tether daemon (internal)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if daemonTmuxSocket == "" || daemonTmuxSession == "" {
			fmt.Fprintln(os.Stderr, "error: --socket and --session are required")
			os.Exit(1)
		}
		return daemon.Run(daemonTmuxSocket, daemonTmuxSession)
	},
}

func init() {
	daemonCmd.Flags().StringVar(&daemonTmuxSocket, "socket", "", "tmux socket path")
	daemonCmd.Flags().StringVar(&daemonTmuxSession, "session", "", "tmux session name")
}

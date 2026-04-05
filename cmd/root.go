package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:     "tether",
	Short:   "Terminal companion — you drive, Claude rides along",
	Long:    `Tether keeps Claude in the loop while you work normally in your terminal.
Start the daemon, point it at a pane, then ask questions with full context.`,
	Version: Version,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(watchCmd)
	rootCmd.AddCommand(askCmd)
	rootCmd.AddCommand(daemonCmd)
}

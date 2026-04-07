package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:     "tether",
	Short:   "Terminal companion — you drive, Claude rides along",
	Long: `Tether captures your terminal output in real time and gives Claude context
without you having to re-explain what you've been doing.

Get started:
  tether install          install shell integration (OSC 133 markers)
  tether shell            start your shell with context capture
  ctrl+\                  open Claude chat overlay (inside tether shell)
  tether ask "question"   one-shot question from any terminal`,
	Version: Version,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(askCmd)
}

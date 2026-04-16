package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"github.com/AllDayJon/Tether/internal/config"
	"github.com/AllDayJon/Tether/internal/ipc"

	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show or edit tether configuration",
	Long: `Show the current tether configuration and where it is loaded from.

To customise, edit ~/.tether/config.json. Missing keys fall back to defaults.

Available settings:
  ask_model   Claude model for ask; empty = CLI default (default: "")
  ask_lines   Lines fetched before relevance filtering (default: 200)
  allow       Commands that auto-execute in Act mode
  protect     Commands that always require approval
  deny        Commands that are never run`,
	RunE: runConfig,
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Write a default config file",
	Long:  `Creates ~/.tether/config.json with default values if it doesn't already exist.`,
	RunE:  runConfigInit,
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configInitCmd)
}

func runConfig(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	p, _ := config.Path()
	_, fileErr := os.Stat(p)
	source := p
	if os.IsNotExist(fileErr) {
		source = "(no config file — showing defaults)"
	}

	sep := strings.Repeat("─", 50)
	fmt.Println(sep)
	fmt.Printf("TETHER CONFIG  %s\n", source)
	fmt.Println(sep)
	fmt.Printf("  %-12s %q\n", "ask_model", cfg.AskModel)
	fmt.Printf("  %-12s %d\n", "ask_lines", cfg.AskLines)
	fmt.Printf("  %-12s %d entries\n", "allow", len(cfg.Allow))
	fmt.Printf("  %-12s %d entries\n", "protect", len(cfg.Protect))
	fmt.Printf("  %-12s %d entries\n", "deny", len(cfg.Deny))
	fmt.Println(sep)
	fmt.Printf("  Edit %s to change settings.\n", p)
	fmt.Println("  Run `tether config init` to create a default config file.")
	return nil
}

func runConfigInit(cmd *cobra.Command, args []string) error {
	if err := ipc.EnsureDir(); err != nil {
		return err
	}

	p, err := config.Path()
	if err != nil {
		return err
	}

	if _, err := os.Stat(p); err == nil {
		fmt.Printf("config already exists: %s\n", p)
		return nil
	}

	cfg := config.Defaults()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(p, data, 0600); err != nil {
		return err
	}

	fmt.Printf("created %s\n", p)
	fmt.Println("edit it to customise tether, then restart tether shell")
	return nil
}

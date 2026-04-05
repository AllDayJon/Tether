package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"tether/internal/config"
	"tether/internal/ipc"

	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show or edit tether configuration",
	Long: `Show the current tether configuration and where it is loaded from.

To customise, edit ~/.tether/config.json. Missing keys fall back to defaults.

Available settings:
  chat_split_percent   Width of the chat pane as % of terminal (default: 40)
  ask_model            Claude model for ask/chat; empty = CLI default (default: "")
  ask_lines            Lines fetched per pane before relevance filtering (default: 200)
  auto_watch           Auto-watch current pane on daemon start (default: true)`,
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
	fmt.Printf("  %-26s %v\n", "chat_split_percent", cfg.ChatSplitPercent)
	fmt.Printf("  %-26s %q\n", "ask_model", cfg.AskModel)
	fmt.Printf("  %-26s %d\n", "ask_lines", cfg.AskLines)
	fmt.Printf("  %-26s %v\n", "auto_watch", cfg.AutoWatch)
	chatKeyDesc := cfg.ChatKey
	if chatKeyDesc == "" {
		chatKeyDesc = "(disabled)"
	} else {
		chatKeyDesc = fmt.Sprintf("%q  → prefix+%s opens chat split", cfg.ChatKey, cfg.ChatKey)
	}
	fmt.Printf("  %-26s %s\n", "chat_key", chatKeyDesc)
	fmt.Println(sep)
	fmt.Printf("  Edit %s to change settings.\n", p)
	fmt.Println("  Run `tether config init` to create a default config file.")
	fmt.Println("  Run `tether keybind install` to set up tmux key bindings (works from SSH panes too).")
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
	fmt.Println("edit it to customise tether, then restart the daemon")
	return nil
}

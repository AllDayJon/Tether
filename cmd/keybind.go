package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"tether/internal/config"

	"github.com/spf13/cobra"
)

var keybindCmd = &cobra.Command{
	Use:   "keybind",
	Short: "Manage tmux key bindings for tether",
	Long: `Install or remove tmux key bindings so you can invoke tether from any pane —
including SSH sessions — without leaving your terminal.

How it works:
  tmux bindings run locally, so prefix+T opens a tether chat split regardless
  of whether the active pane is local or an SSH session.

Sub-commands:
  install   Set bindings in the current tmux session and optionally ~/.tmux.conf
  remove    Remove tether bindings from ~/.tmux.conf
  show      Print the bindings that would be applied`,
}

var keybindInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install tether key bindings in tmux",
	Long: `Adds key bindings to the live tmux session immediately.
Optionally appends them to ~/.tmux.conf so they persist across restarts.`,
	RunE: runKeybindInstall,
}

var keybindRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove tether key bindings from ~/.tmux.conf",
	RunE:  runKeybindRemove,
}

var keybindShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the tmux bindings that would be applied",
	RunE:  runKeybindShow,
}

var keybindPersist bool

func init() {
	rootCmd.AddCommand(keybindCmd)
	keybindCmd.AddCommand(keybindInstallCmd)
	keybindCmd.AddCommand(keybindRemoveCmd)
	keybindCmd.AddCommand(keybindShowCmd)
	keybindInstallCmd.Flags().BoolVar(&keybindPersist, "persist", false, "also append bindings to ~/.tmux.conf")
}

// binding holds a tmux keybinding as both its tmux args and its tmux.conf line.
type binding struct {
	args    []string // passed directly to tmux (no shell quoting issues)
	confLine string  // written to ~/.tmux.conf
}

// buildBindings returns the bindings for the current config.
func buildBindings(cfg config.Config) []binding {
	if cfg.ChatKey == "" {
		return nil
	}
	pct := fmt.Sprintf("%d", cfg.ChatSplitPercent)
	// split-window opens an interactive pane running tether _chat.
	// This works from any pane including SSH sessions because tmux executes
	// the binding locally before the SSH session sees the keypress.
	// Use run-shell so TMUX_PANE is set when tether chat runs, letting it
	// capture the current pane as the work pane before opening the split.
	chatCmd := fmt.Sprintf("tether chat -p %s", pct)
	return []binding{
		{
			args:     []string{"bind-key", cfg.ChatKey, "run-shell", chatCmd},
			confLine: fmt.Sprintf("bind-key %s run-shell '%s'", cfg.ChatKey, chatCmd),
		},
	}
}

func runKeybindShow(cmd *cobra.Command, args []string) error {
	cfg, _ := config.Load()
	bs := buildBindings(cfg)
	if len(bs) == 0 {
		fmt.Println("no bindings configured (chat_key is empty in config)")
		return nil
	}
	fmt.Println("# Tether tmux bindings")
	for _, b := range bs {
		fmt.Println(b.confLine)
	}
	return nil
}

func runKeybindInstall(cmd *cobra.Command, args []string) error {
	if os.Getenv("TMUX") == "" {
		return fmt.Errorf("not inside a tmux session — bindings can only be installed from within tmux")
	}

	cfg, _ := config.Load()
	bs := buildBindings(cfg)
	if len(bs) == 0 {
		fmt.Println("no bindings to install (chat_key is empty in config)")
		return nil
	}

	socketPath := strings.SplitN(os.Getenv("TMUX"), ",", 3)[0]

	// Apply to live session.
	for _, b := range bs {
		tmuxArgs := append([]string{"-S", socketPath}, b.args...)
		out, err := exec.Command("tmux", tmuxArgs...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("tmux %s: %w\n%s", strings.Join(b.args, " "), err, out)
		}
	}
	fmt.Printf("installed: prefix+%s → tether chat\n", cfg.ChatKey)

	if keybindPersist {
		lines := make([]string, len(bs))
		for i, b := range bs {
			lines[i] = b.confLine
		}
		if err := appendToTmuxConf(lines); err != nil {
			return err
		}
	} else {
		fmt.Println("binding is live for this session only — use --persist to save to ~/.tmux.conf")
	}
	return nil
}

func runKeybindRemove(cmd *cobra.Command, args []string) error {
	confPath, err := tmuxConfPath()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(confPath)
	if os.IsNotExist(err) {
		fmt.Println("~/.tmux.conf not found — nothing to remove")
		return nil
	}
	if err != nil {
		return err
	}

	var kept []string
	removed := 0
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "tether") {
			removed++
			continue
		}
		kept = append(kept, line)
	}

	if removed == 0 {
		fmt.Println("no tether bindings found in ~/.tmux.conf")
		return nil
	}

	if err := os.WriteFile(confPath, []byte(strings.Join(kept, "\n")+"\n"), 0644); err != nil {
		return err
	}
	fmt.Printf("removed %d tether line(s) from %s\n", removed, confPath)
	return nil
}

func appendToTmuxConf(bs []string) error {
	confPath, err := tmuxConfPath()
	if err != nil {
		return err
	}

	// Check if bindings are already present.
	if data, err := os.ReadFile(confPath); err == nil {
		if strings.Contains(string(data), "# tether keybinds") {
			fmt.Printf("bindings already in %s — remove with `tether keybind remove` first\n", confPath)
			return nil
		}
	}

	f, err := os.OpenFile(confPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("opening %s: %w", confPath, err)
	}
	defer f.Close()

	fmt.Fprintln(f, "\n# tether keybinds — managed by `tether keybind`")
	for _, b := range bs {
		fmt.Fprintln(f, b)
	}

	fmt.Printf("appended to %s\n", confPath)
	fmt.Println("reload with: tmux source-file ~/.tmux.conf")
	return nil
}

func tmuxConfPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".tmux.conf"), nil
}

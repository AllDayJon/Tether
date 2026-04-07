// Package shellintegration provides OSC 133 shell integration scripts.
// These scripts emit semantic markers around prompts and commands so the
// PTY proxy can distinguish command boundaries from raw output, enabling
// structured context capture instead of blind line accumulation.
package shellintegration

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"tether/internal/ipc"
)

// ScriptBash is the bash shell integration script.
// Source it from ~/.bashrc: source ~/.tether/shell-integration.bash
const ScriptBash = `# Tether shell integration — bash
# Emits OSC 133 markers so tether can detect command boundaries,
# shows a prompt indicator, and binds ctrl+\ to open the chat overlay.
# Source this from ~/.bashrc.

if [ -n "$TETHER" ]; then
  # Bind ctrl+\ to signal the tether proxy to open the chat overlay.
  bind '"\034":"kill -USR1 $TETHER_PID 2>/dev/null\n"'

  __tether_exit_code=0

  __tether_precmd() {
    local code=$?
    printf '\033]133;D;%d\033\\' "$code"
    printf '\033]133;A\033\\'
    __tether_exit_code=$code
  }

  __tether_preexec() {
    printf '\033]133;C\033\\'
  }

  # Hook into bash's DEBUG trap for preexec and PROMPT_COMMAND for precmd.
  __tether_prev_cmd=""
  __tether_debug_hook() {
    if [ "$BASH_COMMAND" != "__tether_precmd" ] && \
       [ "$BASH_COMMAND" != "__tether_preexec" ] && \
       [ -n "$BASH_COMMAND" ] && \
       [ "$BASH_COMMAND" != "$__tether_prev_cmd" ]; then
      __tether_preexec
    fi
    __tether_prev_cmd="$BASH_COMMAND"
  }

  trap '__tether_debug_hook' DEBUG
  PROMPT_COMMAND="__tether_precmd${PROMPT_COMMAND:+; $PROMPT_COMMAND}"

  # Mark prompt start/end around PS1 and add tether indicator.
  PS1='\[\033[0;36m\][tether]\[\033[0m\] \[\033]133;B\033\\\]'"$PS1"
fi
`

// ScriptZsh is the zsh shell integration script.
// Source it from ~/.zshrc: source ~/.tether/shell-integration.zsh
const ScriptZsh = `# Tether shell integration — zsh
# Emits OSC 133 markers so tether can detect command boundaries,
# shows a prompt indicator, and binds ctrl+\ to open the chat overlay.
# Source this from ~/.zshrc.

if [[ -n "$TETHER" ]]; then
  # Bind ctrl+\ to signal the tether proxy to open the chat overlay.
  __tether_overlay() { kill -USR1 $TETHER_PID 2>/dev/null }
  zle -N __tether_overlay
  bindkey '^\' __tether_overlay

  __tether_preexec() {
    printf '\033]133;C\033\\'
  }

  __tether_precmd() {
    local code=$?
    printf '\033]133;D;%d\033\\' "$code"
    printf '\033]133;A\033\\'
  }

  __tether_prompt_marker() {
    printf '\033]133;B\033\\'
  }

  # Add tether indicator to the prompt.
  PROMPT='%F{cyan}[tether]%f '"$PROMPT"

  autoload -Uz add-zsh-hook
  add-zsh-hook preexec __tether_preexec
  add-zsh-hook precmd __tether_precmd
  precmd_functions+=(__tether_prompt_marker)
fi
`

// ScriptFish is the fish shell integration script.
// Place it at ~/.config/fish/conf.d/tether.fish (fish loads conf.d automatically).
const ScriptFish = `# Tether shell integration — fish
# Emits OSC 133 markers so tether can detect command boundaries,
# shows a prompt indicator, and binds ctrl+\ to open the chat overlay.
# Place this file at ~/.config/fish/conf.d/tether.fish

if set -q TETHER
  # Bind ctrl+\ to signal the tether proxy to open the chat overlay.
  # \x1c is the hex code for ASCII 28 (ctrl+\), which is what fish expects.
  # SIGUSR1 is used so this works with all terminal keyboard encoding protocols.
  bind \x1c 'kill -USR1 $TETHER_PID 2>/dev/null'

  function __tether_preexec --on-event fish_preexec
    printf '\033]133;C\033\\'
  end

  function __tether_postexec --on-event fish_postexec
    printf '\033]133;D;%d\033\\' $status
  end

  # Wrap fish_prompt to emit A/B markers and show a tether indicator.
  if functions -q fish_prompt
    functions --copy fish_prompt __tether_orig_fish_prompt
  else
    function __tether_orig_fish_prompt
      echo '> '
    end
  end

  function fish_prompt
    printf '\033]133;A\033\\'
    # Tether indicator — shown in cyan before the normal prompt.
    printf '\033[0;36m[tether]\033[0m '
    __tether_orig_fish_prompt
    printf '\033]133;B\033\\'
  end
end
`

// InstallPaths returns the path where each shell's integration script is written.
func InstallPaths() (bash, zsh, fish string, err error) {
	dir, err := ipc.Dir()
	if err != nil {
		return "", "", "", err
	}
	bash = filepath.Join(dir, "shell-integration.bash")
	zsh = filepath.Join(dir, "shell-integration.zsh")
	fish = filepath.Join(dir, "shell-integration.fish")
	return bash, zsh, fish, nil
}

// InstalledMarkerPath returns the path to a marker file written after install.
// Its existence signals that tether install has been run at least once.
func InstalledMarkerPath() (string, error) {
	dir, err := ipc.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "shell-integration.bash"), nil
}

// Install writes the shell integration scripts to ~/.tether/ and adds source
// lines to the appropriate shell config files. It is idempotent.
func Install(shell string) error {
	if err := ipc.EnsureDir(); err != nil {
		return err
	}

	bashPath, zshPath, fishPath, err := InstallPaths()
	if err != nil {
		return err
	}

	// Always write the scripts (update them if they exist).
	if err := os.WriteFile(bashPath, []byte(ScriptBash), 0644); err != nil {
		return fmt.Errorf("writing bash integration: %w", err)
	}
	if err := os.WriteFile(zshPath, []byte(ScriptZsh), 0644); err != nil {
		return fmt.Errorf("writing zsh integration: %w", err)
	}
	if err := os.WriteFile(fishPath, []byte(ScriptFish), 0644); err != nil {
		return fmt.Errorf("writing fish integration: %w", err)
	}

	// Add source lines based on the current shell (or all shells if unknown).
	shells := detectShells(shell)
	for _, s := range shells {
		if err := addSourceLine(s, bashPath, zshPath, fishPath); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not update %s config: %v\n", s, err)
		}
	}

	return nil
}

// detectShells returns which shells to configure, preferring the current shell.
func detectShells(currentShell string) []string {
	base := filepath.Base(currentShell)
	switch base {
	case "fish":
		return []string{"fish"}
	case "zsh":
		return []string{"zsh"}
	case "bash":
		return []string{"bash"}
	default:
		// Unknown shell — try all three.
		return []string{"bash", "zsh", "fish"}
	}
}

func addSourceLine(shell, bashScript, zshScript, fishScript string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	switch shell {
	case "bash":
		// macOS Terminal.app opens login shells which read .bash_profile, not .bashrc.
		// Linux interactive shells read .bashrc.
		rcFile := ".bashrc"
		if runtime.GOOS == "darwin" {
			rcFile = ".bash_profile"
		}
		rcPath := filepath.Join(home, rcFile)
		sourceLine := fmt.Sprintf("\n# Tether shell integration\n[ -f %q ] && source %q\n", bashScript, bashScript)
		return appendIfMissing(rcPath, sourceLine, "tether shell integration")

	case "zsh":
		rcPath := filepath.Join(home, ".zshrc")
		sourceLine := fmt.Sprintf("\n# Tether shell integration\n[ -f %q ] && source %q\n", zshScript, zshScript)
		return appendIfMissing(rcPath, sourceLine, "tether shell integration")

	case "fish":
		// fish loads ~/.config/fish/conf.d/*.fish automatically.
		confDir := filepath.Join(home, ".config", "fish", "conf.d")
		if err := os.MkdirAll(confDir, 0755); err != nil {
			return err
		}
		dest := filepath.Join(confDir, "tether.fish")
		// Symlink or copy the script into conf.d.
		_ = os.Remove(dest)
		return os.WriteFile(dest, []byte(ScriptFish), 0644)
	}
	return nil
}

// Uninstall removes tether integration scripts and source lines from shell
// configs, then removes the ~/.tether directory entirely.
func Uninstall() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	// Remove source lines from shell configs.
	bashRC := ".bashrc"
	if runtime.GOOS == "darwin" {
		bashRC = ".bash_profile"
	}
	configs := []string{
		filepath.Join(home, bashRC),
		filepath.Join(home, ".zshrc"),
	}
	for _, rc := range configs {
		removeLinesContaining(rc, "tether shell integration")
	}

	// Remove fish conf.d entry.
	fishConf := filepath.Join(home, ".config", "fish", "conf.d", "tether.fish")
	os.Remove(fishConf)

	// Remove individual files from ~/.tether rather than the whole directory,
	// to avoid pulling active session sockets out from under running sessions.
	dir, err := ipc.Dir()
	if err != nil {
		return err
	}
	for _, name := range []string{
		"daemon.log",
		"conversation.json",
		"summary.txt",
		"config.json",
		"shell-integration.bash",
		"shell-integration.zsh",
		"shell-integration.fish",
	} {
		os.Remove(filepath.Join(dir, name)) // best-effort
	}
	// Remove the directory itself only if it is now empty.
	os.Remove(dir) // no-op if non-empty
	return nil
}

func removeLinesContaining(path, marker string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	var kept []string
	skip := false
	for _, line := range lines {
		if strings.Contains(line, marker) {
			skip = true
		}
		if skip && line == "" {
			skip = false
			continue
		}
		if !skip {
			kept = append(kept, line)
		}
	}
	os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0644)
}

func appendIfMissing(path, content, marker string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if strings.Contains(string(existing), marker) {
		return nil // already installed
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}

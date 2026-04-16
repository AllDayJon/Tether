package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"github.com/AllDayJon/Tether/internal/shellintegration"

	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install shell integration (OSC 133 markers)",
	Long: `Installs OSC 133 shell integration for your current shell.

This writes integration scripts to ~/.tether/ and adds a source line
to your shell config (~/.bashrc, ~/.zshrc, or ~/.config/fish/conf.d/).

The scripts emit semantic markers around prompts and commands so tether
can detect command boundaries and capture output with proper structure —
giving Claude much richer context than plain line capture.

After installing, restart your shell or source the config file, then
run: tether shell`,
	RunE: runInstall,
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove tether shell integration and config files",
	Long: `Removes tether shell integration scripts from ~/.tether/ and removes
the source lines added to your shell config files.

Your conversation history and session summary are also removed.
The tether binary itself is not removed — delete it manually if needed.`,
	RunE: runUninstall,
}

func init() {
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(uninstallCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	shell := os.Getenv("SHELL")
	if err := shellintegration.Install(shell); err != nil {
		return fmt.Errorf("install failed: %w", err)
	}

	bashPath, zshPath, fishPath, _ := shellintegration.InstallPaths()
	fmt.Println("Shell integration installed:")
	fmt.Printf("  bash: %s\n", bashPath)
	fmt.Printf("  zsh:  %s\n", zshPath)
	fmt.Printf("  fish: %s\n", fishPath)
	fmt.Println()

	shell = getShellBase(shell)
	switch shell {
	case "fish":
		fmt.Println("Added:  ~/.config/fish/conf.d/tether.fish")
		fmt.Println()
		fmt.Println("Restart fish or open a new terminal, then run:")
	case "zsh":
		fmt.Println("Added source line to ~/.zshrc")
		fmt.Println()
		fmt.Println("Run `source ~/.zshrc` or open a new terminal, then:")
	default:
		bashRC := "~/.bashrc"
		if runtime.GOOS == "darwin" {
			bashRC = "~/.bash_profile"
		}
		fmt.Printf("Added source line to %s\n", bashRC)
		fmt.Println()
		fmt.Printf("Run `source %s` or open a new terminal, then:\n", bashRC)
	}
	fmt.Println("  tether shell")

	warnIfNotOnPath()
	return nil
}

// warnIfNotOnPath prints a warning when the tether binary cannot be found on
// PATH. This catches the common case where go install placed the binary in
// $GOPATH/bin but that directory is not in the user's PATH.
func warnIfNotOnPath() {
	if _, err := exec.LookPath("tether"); err == nil {
		return // already reachable
	}

	// Try to find where the binary actually lives so we can give a precise hint.
	binDir := goBinDir()

	fmt.Println()
	fmt.Println("WARNING: 'tether' is not on your PATH.")
	if binDir != "" {
		fmt.Printf("         The binary is at: %s/tether\n", binDir)
		fmt.Println()
		printPathInstructions(binDir)
	} else {
		fmt.Println("         Make sure the directory containing the tether binary is on PATH.")
	}
}

// goBinDir returns the directory where `go install` places binaries.
// It respects $GOBIN, then $GOPATH/bin, then ~/go/bin.
func goBinDir() string {
	if gobin := os.Getenv("GOBIN"); gobin != "" {
		if _, err := os.Stat(filepath.Join(gobin, "tether")); err == nil {
			return gobin
		}
	}
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		dir := filepath.Join(gopath, "bin")
		if _, err := os.Stat(filepath.Join(dir, "tether")); err == nil {
			return dir
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		dir := filepath.Join(home, "go", "bin")
		if _, err := os.Stat(filepath.Join(dir, "tether")); err == nil {
			return dir
		}
	}
	return ""
}

// printPathInstructions prints shell-specific instructions for adding binDir to PATH.
func printPathInstructions(binDir string) {
	shell := getShellBase(os.Getenv("SHELL"))
	switch shell {
	case "fish":
		fmt.Printf("         Run:  fish_add_path %s\n", binDir)
		fmt.Println("         This persists automatically in fish.")
	case "zsh":
		fmt.Printf("         Add to ~/.zshrc:  export PATH=\"%s:$PATH\"\n", binDir)
		fmt.Println("         Then run:         source ~/.zshrc")
	default:
		rc := "~/.bashrc"
		if runtime.GOOS == "darwin" {
			rc = "~/.bash_profile"
		}
		fmt.Printf("         Add to %s:  export PATH=\"%s:$PATH\"\n", rc, binDir)
		fmt.Printf("         Then run:         source %s\n", rc)
	}
}

func runUninstall(cmd *cobra.Command, args []string) error {
	if err := shellintegration.Uninstall(); err != nil {
		return fmt.Errorf("uninstall failed: %w", err)
	}
	fmt.Println("Tether uninstalled.")
	fmt.Println("The tether binary was not removed — delete it manually if needed.")
	return nil
}

func getShellBase(shell string) string {
	for i := len(shell) - 1; i >= 0; i-- {
		if shell[i] == '/' {
			return shell[i+1:]
		}
	}
	return shell
}

// shellIntegrationInstalledPath returns the path to the bash integration script
// if it exists, or empty string if tether install has not been run.
func shellIntegrationInstalledPath() (string, error) {
	path, err := shellintegration.InstalledMarkerPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		return "", nil
	}
	return path, nil
}

package cmd

import (
	"fmt"
	"os/exec"
	"tether/internal/ipc"

	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check that all tether dependencies are satisfied",
	Long: `Runs a series of checks to verify that tether's dependencies are
installed and reachable. Useful for debugging a fresh install.`,
	RunE: runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(_ *cobra.Command, _ []string) error {
	ok := true

	// ── claude CLI ───────────────────────────────────────────────────────────
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		printCheck(false, "claude", "not found — install Claude Code CLI and make sure it's on PATH")
		ok = false
	} else {
		printCheck(true, "claude", claudePath)
	}

	// ── tether shell ─────────────────────────────────────────────────────────
	conn, err := ipc.Dial()
	if err != nil {
		printCheck(false, "tether shell", "not running — start with: tether shell")
		ok = false
	} else {
		defer conn.Close()
		_ = ipc.SendMsg(conn, ipc.TypeStatus, nil)
		var resp ipc.StatusResp
		if err := ipc.Recv(conn, &resp); err != nil {
			printCheck(false, "tether shell", "connected but unresponsive")
			ok = false
		} else {
			detail := fmt.Sprintf("running  (shell: %s, mode: %s, buffer: %d lines)",
				resp.Shell, resp.Mode, resp.BufferedLines)
			printCheck(true, "tether shell", detail)
		}
	}

	// ── shell integration ─────────────────────────────────────────────────────
	// Check if tether install has been run (OSC 133 markers configured).
	// We detect this by looking for the integration file in ~/.tether/.
	integrationPath, _ := shellIntegrationInstalledPath()
	if integrationPath != "" {
		printCheck(true, "shell integration", integrationPath)
	} else {
		printCheck(false, "shell integration", "not installed — run: tether install")
		ok = false
	}

	if !ok {
		fmt.Println()
		fmt.Println("One or more checks failed. See above for details.")
	} else {
		fmt.Println()
		fmt.Println("All checks passed.")
	}

	return nil
}

func printCheck(pass bool, label, detail string) {
	icon := "✓"
	if !pass {
		icon = "✗"
	}
	fmt.Printf("  %s  %-20s  %s\n", icon, label, detail)
}

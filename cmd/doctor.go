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

	// ── tmux ─────────────────────────────────────────────────────────────────
	if out, err := exec.Command("tmux", "-V").Output(); err != nil {
		printCheck(false, "tmux", "not found — install tmux to use tether")
		ok = false
	} else {
		version := string(out)
		if len(version) > 0 && version[len(version)-1] == '\n' {
			version = version[:len(version)-1]
		}
		printCheck(true, "tmux", version)
	}

	// ── claude CLI ───────────────────────────────────────────────────────────
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		printCheck(false, "claude", "not found — install Claude Code CLI and make sure it's on PATH")
		ok = false
	} else {
		printCheck(true, "claude", claudePath)
	}

	// ── daemon ───────────────────────────────────────────────────────────────
	conn, err := ipc.Dial()
	if err != nil {
		printCheck(false, "daemon", "not running — start with: tether start")
		ok = false
	} else {
		defer conn.Close()
		_ = ipc.SendMsg(conn, ipc.TypeStatus, nil)
		var resp ipc.StatusResp
		if err := ipc.Recv(conn, &resp); err != nil {
			printCheck(false, "daemon", "connected but unresponsive")
			ok = false
		} else {
			detail := fmt.Sprintf("running  (session: %s, mode: %s)", resp.TmuxSession, resp.Mode)
			printCheck(true, "daemon", detail)

			// ── watched panes ─────────────────────────────────────────────────
			if len(resp.WatchedPanes) == 0 {
				printCheck(false, "panes", "none watched — run: tether watch <pane-id>")
			} else {
				total := 0
				for _, n := range resp.BufferSizes {
					total += n
				}
				detail := fmt.Sprintf("%v  (%d lines buffered)", resp.WatchedPanes, total)
				printCheck(true, "panes", detail)
			}
		}
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
	fmt.Printf("  %s  %-10s  %s\n", icon, label, detail)
}

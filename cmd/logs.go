package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"tether/internal/ipc"

	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Tail the daemon log",
	Long:  "Stream the daemon log in real time. Press Ctrl+C to stop.",
	RunE:  runLogs,
}

func init() {
	rootCmd.AddCommand(logsCmd)
}

func runLogs(cmd *cobra.Command, args []string) error {
	logPath, err := ipc.LogPath()
	if err != nil {
		return err
	}

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		fmt.Println("no log file yet — start the daemon first with `tether start`")
		return nil
	}

	tail := exec.Command("tail", "-f", logPath)
	tail.Stdout = os.Stdout
	tail.Stderr = os.Stderr
	tail.Stdin = os.Stdin
	return tail.Run()
}

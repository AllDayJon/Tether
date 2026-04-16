package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
	"github.com/AllDayJon/Tether/internal/ipc"

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

	return tailFollow(logPath)
}

// tailFollow streams a file's contents and polls for new data until
// interrupted. It is a dependency-free replacement for `tail -f`.
func tailFollow(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Print existing content first.
	if _, err := io.Copy(os.Stdout, f); err != nil {
		return err
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	scanner := bufio.NewScanner(f)
	for {
		select {
		case <-sigCh:
			return nil
		case <-ticker.C:
			for scanner.Scan() {
				fmt.Println(scanner.Text())
			}
			if err := scanner.Err(); err != nil {
				return err
			}
		}
	}
}

package cmd

import (
	"fmt"
	"os"
	"github.com/AllDayJon/Tether/internal/conversation"
	"github.com/AllDayJon/Tether/internal/ipc"

	"github.com/spf13/cobra"
)

var (
	clearHistoryOnly bool
	clearContextOnly bool
)

var clearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear conversation history and/or terminal context buffers",
	Long: `Clear conversation history and terminal context buffers.

By default clears both. Use flags to clear only one:
  tether clear              — clear everything
  tether clear --history    — clear only conversation history
  tether clear --context    — clear only terminal context buffers`,
	RunE: runClear,
}

func init() {
	rootCmd.AddCommand(clearCmd)
	clearCmd.Flags().BoolVar(&clearHistoryOnly, "history", false, "clear only conversation history")
	clearCmd.Flags().BoolVar(&clearContextOnly, "context", false, "clear only terminal context buffers")
}

func runClear(cmd *cobra.Command, args []string) error {
	doHistory := !clearContextOnly
	doContext := !clearHistoryOnly

	if clearHistoryOnly && clearContextOnly {
		return fmt.Errorf("--history and --context are mutually exclusive; omit both to clear everything")
	}

	if doHistory {
		path, err := conversation.DefaultPath()
		if err != nil {
			return err
		}
		os.Remove(path)
		fmt.Println("conversation history cleared")
	}

	if doContext {
		conn, err := ipc.Dial()
		if err != nil {
			fmt.Fprintln(os.Stderr, "note: daemon not running — skipping context buffer clear")
		} else {
			defer conn.Close()
			if err := ipc.SendMsg(conn, ipc.TypeClearBuffers, nil); err != nil {
				return fmt.Errorf("sending clear request: %w", err)
			}
			var resp ipc.OKResp
			ipc.Recv(conn, &resp) // best-effort read
			fmt.Println("terminal context buffers cleared")
		}
	}

	return nil
}

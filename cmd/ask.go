package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"tether/internal/claude"
	"tether/internal/config"
	tctx "tether/internal/context"
	"tether/internal/ipc"

	"github.com/spf13/cobra"
)

var (
	askLines int
	askModel string
	askDebug bool
)

var askCmd = &cobra.Command{
	Use:   "ask <question>",
	Short: "Ask Claude a question with your current terminal context",
	Long: `Ask Claude a question. Tether's captured terminal output is automatically
included as context, so Claude already knows what you've been doing.

Examples:
  tether ask "what does this error mean"
  tether ask "how do I fix the 502s in nginx"
  tether ask -n 100 "summarize what I've been doing"
  tether ask --debug "why did that fail"   # show full prompt before sending`,
	Args: cobra.MinimumNArgs(1),
	RunE: runAsk,
}

func init() {
	cfg, _ := config.Load()
	askCmd.Flags().IntVarP(&askLines, "lines", "n", cfg.AskLines, "number of terminal lines to include (relevance filtering selects the best ones)")
	askCmd.Flags().StringVarP(&askModel, "model", "m", cfg.AskModel, "Claude model to use (overrides config)")
	askCmd.Flags().BoolVarP(&askDebug, "debug", "d", false, "print the full prompt before sending")
	_ = claude.DefaultModel // keep import used
}

func runAsk(cmd *cobra.Command, args []string) error {
	question := strings.Join(args, " ")

	// Fetch context from the running tether shell session (best-effort).
	var paneCtx []ipc.PaneContext
	conn, dialErr := ipc.Dial()
	if dialErr != nil {
		fmt.Fprintln(os.Stderr, "note: tether shell not running — asking without terminal context")
		fmt.Fprintln(os.Stderr, "      start one with: tether shell")
	} else {
		defer conn.Close()
		if sendErr := ipc.SendMsg(conn, ipc.TypeGetContext, ipc.GetContextPayload{NLines: askLines}); sendErr == nil {
			var resp ipc.ContextResp
			if recvErr := ipc.Recv(conn, &resp); recvErr == nil && len(resp.Lines) > 0 {
				paneCtx = []ipc.PaneContext{{PaneID: "session", Lines: resp.Lines}}
			}
		}
	}

	if askDebug {
		sep := strings.Repeat("─", 60)
		prompt := claude.BuildPrompt(question, paneCtx)

		rawLines, rawChars := 0, 0
		for _, p := range paneCtx {
			rawLines += len(p.Lines)
			for _, l := range p.Lines {
				rawChars += len(l) + 1
			}
		}
		filtered := tctx.SelectForQuestion(question, paneCtx, tctx.DefaultOptions())
		sentLines, sentChars := 0, 0
		for _, p := range filtered {
			sentLines += len(p.Lines)
			for _, l := range p.Lines {
				sentChars += len(l) + 1
			}
		}
		lineSavePct := 0
		if rawLines > 0 {
			lineSavePct = 100 - (sentLines*100)/rawLines
		}

		fmt.Fprintln(os.Stderr, sep)
		fmt.Fprintln(os.Stderr, "DEBUG: full prompt being sent to Claude")
		fmt.Fprintln(os.Stderr, sep)
		fmt.Fprintln(os.Stderr, prompt)
		fmt.Fprintln(os.Stderr, sep)
		fmt.Fprintf(os.Stderr, "context : fetched %d lines (~%d tok) → sent %d lines (~%d tok)  -%d%%\n",
			rawLines, rawChars/4, sentLines, sentChars/4, lineSavePct)
		fmt.Fprintf(os.Stderr, "prompt  : %d chars  ~%d tokens\n\n", len(prompt), len(prompt)/4)
	}

	if err := claude.Ask(context.Background(), question, paneCtx, os.Stdout, askModel); err != nil {
		return fmt.Errorf("ask failed: %w", err)
	}
	return nil
}

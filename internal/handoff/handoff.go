// Package handoff prepares a structured context package for handoff to an
// external agent (e.g. Claude Code). It extracts the relevant situation from
// Tether's session buffer and formats it into a prompt the agent can act on.
package handoff

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tctx "tether/internal/context"
	"tether/internal/ipc"
)

// Failure is a detected error pattern and how many times it appeared.
type Failure struct {
	Line  string
	Count int
}

// Package is the structured handoff prepared for the agent.
type Package struct {
	Goal     string
	CWD      string
	Branch   string
	Dirty    string // trimmed `git status --short` output
	Summary  string
	Failures []Failure
	Evidence []string
	BuiltAt  time.Time
}

// Build constructs a Package from the current session context.
// goal is the user's task description; panes and summary come from the
// active tether shell session(s).
func Build(goal string, panes []ipc.PaneContext, summary string) Package {
	pkg := Package{
		Goal:    goal,
		BuiltAt: time.Now(),
		Summary: summary,
		CWD:     cwdString(),
		Branch:  gitBranch(),
		Dirty:   gitDirty(),
	}

	pkg.Failures = extractFailures(panes)

	// Select evidence lines most relevant to the goal. Keep it tight —
	// the agent will explore further once it has context.
	opts := tctx.DefaultOptions()
	opts.TopK = 60
	opts.MaxLines = 80
	filtered := tctx.SelectForQuestion(goal, panes, opts)
	for _, p := range filtered {
		pkg.Evidence = append(pkg.Evidence, p.Lines...)
	}

	return pkg
}

// Prompt builds the full prompt string to pass as the agent's first message.
func (p Package) Prompt() string {
	var sb strings.Builder

	sb.WriteString("You are picking up a task prepared by Tether, a terminal session context tool.\n")
	sb.WriteString("The context below was captured from the user's live terminal session.\n\n")

	sb.WriteString("## Goal\n")
	sb.WriteString(p.Goal)
	sb.WriteString("\n\n")

	sb.WriteString("## Session\n")
	if p.CWD != "" {
		fmt.Fprintf(&sb, "Directory: %s\n", p.CWD)
	}
	if p.Branch != "" {
		fmt.Fprintf(&sb, "Branch:    %s\n", p.Branch)
	}
	if p.Dirty != "" {
		fmt.Fprintf(&sb, "Uncommitted changes:\n%s\n", p.Dirty)
	}
	sb.WriteString("\n")

	if p.Summary != "" {
		sb.WriteString("## What the user was doing\n")
		sb.WriteString(p.Summary)
		sb.WriteString("\n\n")
	}

	if len(p.Failures) > 0 {
		sb.WriteString("## Repeated failures\n")
		for _, f := range p.Failures {
			if f.Count > 1 {
				fmt.Fprintf(&sb, "- %s (×%d)\n", f.Line, f.Count)
			} else {
				fmt.Fprintf(&sb, "- %s\n", f.Line)
			}
		}
		sb.WriteString("\n")
	}

	if len(p.Evidence) > 0 {
		sb.WriteString("## Key terminal output\n```\n")
		for _, l := range p.Evidence {
			sb.WriteString(l)
			sb.WriteByte('\n')
		}
		sb.WriteString("```\n\n")
	}

	sb.WriteString("---\n")
	sb.WriteString("Confirm the current state of the problem before making any changes.\n")

	return sb.String()
}

// Write persists the handoff prompt to ~/.tether/handoffs/<ts>.md and
// returns the file path.
func (p Package) Write() (string, error) {
	dir, err := ipc.Dir()
	if err != nil {
		return "", fmt.Errorf("handoff dir: %w", err)
	}
	handoffsDir := filepath.Join(dir, "handoffs")
	if err := os.MkdirAll(handoffsDir, 0700); err != nil {
		return "", fmt.Errorf("mkdir handoffs: %w", err)
	}
	promptPath := filepath.Join(handoffsDir, fmt.Sprintf("%d.md", p.BuiltAt.Unix()))
	if err := os.WriteFile(promptPath, []byte(p.Prompt()), 0600); err != nil {
		return "", fmt.Errorf("write prompt: %w", err)
	}
	return promptPath, nil
}

// LaunchCmd writes the prompt file and a small launch script, then returns
// the shell command to run it. Using `sh` explicitly makes this work
// regardless of the user's login shell (bash/zsh/fish).
func (p Package) LaunchCmd() (string, error) {
	promptPath, err := p.Write()
	if err != nil {
		return "", err
	}

	// Write a portable sh script. After starting claude, send SIGWINCH to the
	// process group so Claude Code's TUI redraws immediately — without this,
	// the trust-folder prompt can be invisible until the user presses a key.
	scriptPath := strings.TrimSuffix(promptPath, ".md") + "-launch.sh"
	script := fmt.Sprintf(`#!/bin/sh
claude "$(cat '%s')" &
CPID=$!
sleep 0.3
kill -WINCH $CPID 2>/dev/null
wait $CPID
`, promptPath)
	if err := os.WriteFile(scriptPath, []byte(script), 0700); err != nil {
		return "", fmt.Errorf("write launch script: %w", err)
	}

	return "sh " + scriptPath, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func cwdString() string {
	wd, _ := os.Getwd()
	return wd
}

func gitBranch() string {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitDirty() string {
	out, err := exec.Command("git", "status", "--short").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// errorSignals are patterns that indicate a line is a failure worth surfacing.
var errorSignals = []string{
	"error", "err:", "failed", "failure", "panic", "fatal",
	"exception", "permission denied", "connection refused",
	"timeout", "timed out", "exit status", "no such", "not found",
	"cannot", "unable to", "refused",
}

// extractFailures scans pane lines for error-signal lines, deduplicates them
// by a short prefix, and returns the most-repeated ones first.
func extractFailures(panes []ipc.PaneContext) []Failure {
	counts := make(map[string]int)
	var order []string

	for _, p := range panes {
		for _, line := range p.Lines {
			lower := strings.ToLower(line)
			for _, sig := range errorSignals {
				if strings.Contains(lower, sig) {
					key := strings.TrimSpace(line)
					if len(key) > 100 {
						key = key[:100]
					}
					if counts[key] == 0 {
						order = append(order, key)
					}
					counts[key]++
					break
				}
			}
		}
	}

	failures := make([]Failure, 0, len(order))
	for _, key := range order {
		failures = append(failures, Failure{Line: key, Count: counts[key]})
	}
	sort.Slice(failures, func(i, j int) bool {
		return failures[i].Count > failures[j].Count
	})
	if len(failures) > 8 {
		failures = failures[:8]
	}
	return failures
}

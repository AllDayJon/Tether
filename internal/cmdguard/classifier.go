// Package cmdguard classifies shell commands and decides whether tether
// should block, propose, or auto-execute them.
//
// Security model:
//
//	Hard rules are evaluated first and CANNOT be overridden by config.
//	  - Hard deny: fork bombs, pipe-to-shell (curl|bash etc.) — always blocked.
//	  - Hard protect: sudo, file writes (>, >>, tee) — always need approval.
//
//	Config lists are evaluated next:
//	  Deny    — additional patterns the operator never wants to run via tether.
//	  Protect — patterns that always require human approval.
//	  Allow   — patterns that may auto-execute in Act mode.
//	  Default — anything not in any list: approval required, blocked in Act.
//
// Decision by mode:
//
//	Watch:  always Block   (no execution path exists)
//	Assist: Deny → Block   | everything else → Propose
//	Act:    Deny → Block   | Protect/Default → Propose | Allow → Execute
package cmdguard

import (
	"strings"
)

// Class is the security classification of a command.
type Class int

const (
	ClassAllowed   Class = iota // on the allow list
	ClassProtected              // on the protect list or matched a hard-protect rule
	ClassDenied                 // on the deny list or matched a hard-deny rule
	ClassDefault                // not on any list — treated conservatively
)

// Decision is the action tether should take for a command given the current mode.
type Decision int

const (
	DecisionBlock   Decision = iota // do not run
	DecisionPropose                 // show proposal UI for human approval
	DecisionExecute                 // auto-execute (Act mode + Allowed only)
)

// hardDenyPatterns are absolute blocks that cannot be overridden by config.
// These represent commands that should never be executed through tether.
var hardDenyPatterns = []string{
	":(){ :|:& };:", // fork bomb
	"|bash",         // pipe to shell (various spacing)
	"| bash",
	"|sh",
	"| sh",
	"| python",
	"|python",
	"| perl",
	"|perl",
	"| ruby",
	"|ruby",
}

// hardProtectPatterns always require human approval regardless of the allow list.
var hardProtectPatterns = []string{
	"sudo ",  // privilege escalation
	" > ",    // file write (with spaces, avoids false positives)
	" >> ",   // file append
	" | tee", // tee to file
}

// Classify returns the security class of cmd based on hard rules and the
// provided allow/protect/deny config lists.
func Classify(cmd string, allow, protect, deny []string) Class {
	lower := strings.ToLower(strings.TrimSpace(cmd))

	// 1. Hard deny — absolute blocks.
	for _, p := range hardDenyPatterns {
		if strings.Contains(lower, p) {
			return ClassDenied
		}
	}

	// 2. Config deny list.
	for _, p := range deny {
		if containsPattern(lower, strings.ToLower(p)) {
			return ClassDenied
		}
	}

	// 3. Hard protect — always need approval even if on allow list.
	for _, p := range hardProtectPatterns {
		if strings.Contains(lower, p) {
			return ClassProtected
		}
	}
	// Redirect at start of command (e.g. "> file").
	if strings.HasPrefix(lower, ">") {
		return ClassProtected
	}
	// Compound commands (&&, ;) — treat conservatively.
	if strings.Contains(lower, "&&") || strings.Contains(lower, " ; ") {
		return ClassProtected
	}

	// 4. Config protect list.
	for _, p := range protect {
		if containsPattern(lower, strings.ToLower(p)) {
			return ClassProtected
		}
	}

	// 5. Config allow list.
	for _, p := range allow {
		if containsPattern(lower, strings.ToLower(p)) {
			return ClassAllowed
		}
	}

	// 6. Not in any list.
	return ClassDefault
}

// Decide returns what tether should do with cmd given mode and config lists.
// mode should be one of "watch", "assist", "act".
func Decide(cmd, mode string, allow, protect, deny []string) Decision {
	class := Classify(cmd, allow, protect, deny)

	switch mode {
	case "watch":
		return DecisionBlock

	case "assist":
		if class == ClassDenied {
			return DecisionBlock
		}
		return DecisionPropose

	case "act":
		switch class {
		case ClassDenied:
			return DecisionBlock
		case ClassAllowed:
			return DecisionExecute
		default: // Protected or Default — fall back to proposal
			return DecisionPropose
		}
	}

	return DecisionBlock // safe default for unknown modes
}

// ExtractBashBlocks parses markdown-style code fences from text and returns
// the contents of ```bash (or ```sh / ```shell) blocks.
func ExtractBashBlocks(text string) []string {
	var blocks []string
	lines := strings.Split(text, "\n")
	inBlock := false
	var current []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inBlock {
			if trimmed == "```bash" || trimmed == "```sh" || trimmed == "```shell" {
				inBlock = true
				current = nil
			}
		} else {
			if trimmed == "```" {
				inBlock = false
				cmd := strings.TrimSpace(strings.Join(current, "\n"))
				if cmd != "" {
					blocks = append(blocks, cmd)
				}
			} else {
				current = append(current, line)
			}
		}
	}
	return blocks
}

// ClassLabel returns a short human-readable label for display.
func ClassLabel(c Class) string {
	switch c {
	case ClassAllowed:
		return "allowed"
	case ClassProtected:
		return "protected"
	case ClassDenied:
		return "denied"
	default:
		return "unlisted"
	}
}

// containsPattern checks if s contains pattern as a word prefix.
// "rm" matches "rm -rf /", "ls" matches "ls -la", etc.
func containsPattern(s, pattern string) bool {
	if pattern == "" {
		return false
	}
	if s == pattern {
		return true
	}
	// Match as prefix of the full command or after a space.
	return strings.HasPrefix(s, pattern+" ") ||
		strings.HasPrefix(s, pattern+"\t") ||
		strings.Contains(s, " "+pattern+" ") ||
		strings.Contains(s, " "+pattern+"\t") ||
		strings.HasSuffix(s, " "+pattern) ||
		s == pattern
}

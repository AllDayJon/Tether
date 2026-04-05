package context

import (
	"fmt"
	"tether/internal/ipc"
)

const (
	// blockThreshold is the minimum consecutive run of lines before truncation kicks in.
	blockThreshold = 15
	// blockKeepEdge is how many lines to keep at the start and end of a truncated block.
	blockKeepEdge = 3
)

// TruncatePanes applies TruncateBlocks to every pane in the slice.
func TruncatePanes(panes []ipc.PaneContext) []ipc.PaneContext {
	out := make([]ipc.PaneContext, len(panes))
	for i, p := range panes {
		out[i] = ipc.PaneContext{PaneID: p.PaneID, Lines: TruncateBlocks(p.Lines)}
	}
	return out
}

// TruncateBlocks collapses runs of blockThreshold or more consecutive lines
// (typically large command outputs like long stack traces or log dumps) to:
//
//	first blockKeepEdge lines
//	… N lines omitted …
//	last blockKeepEdge lines
//
// Runs shorter than blockThreshold are passed through unchanged.
func TruncateBlocks(lines []string) []string {
	if len(lines) <= blockThreshold {
		return lines
	}

	segs := splitSegments(lines)
	out := make([]string, 0, len(lines))
	for _, seg := range segs {
		run := lines[seg.start:seg.end]
		length := seg.end - seg.start
		if !seg.isPrompt && length > blockThreshold {
			// Truncate the run.
			out = append(out, run[:blockKeepEdge]...)
			omitted := length - blockKeepEdge*2
			out = append(out, fmt.Sprintf("… %d lines omitted …", omitted))
			out = append(out, run[length-blockKeepEdge:]...)
		} else {
			out = append(out, run...)
		}
	}
	return out
}

type segment struct {
	start, end int
	isPrompt   bool
}

// splitSegments divides lines into alternating prompt / output segments.
func splitSegments(lines []string) []segment {
	var segs []segment
	i := 0
	for i < len(lines) {
		prompt := looksLikePrompt(lines[i])
		start := i
		i++
		for i < len(lines) && looksLikePrompt(lines[i]) == prompt {
			i++
		}
		segs = append(segs, segment{start: start, end: i, isPrompt: prompt})
	}
	return segs
}

// looksLikePrompt returns true for lines that look like shell prompts.
func looksLikePrompt(line string) bool {
	if len(line) == 0 {
		return false
	}
	// Common shell prompt endings: "$ ", "# ", "> ", "% ", or just those chars.
	for _, suffix := range []string{"$ ", "# ", "> ", "% ", "$", "#"} {
		if len(line) >= len(suffix) && line[len(line)-len(suffix):] == suffix {
			return true
		}
	}
	return false
}

package context

import (
	"sort"
	"strings"
	"tether/internal/ipc"
)

// Options controls how SelectForQuestion trims pane context.
type Options struct {
	TopK      int                    // max top-scoring lines to include
	LastN     int                    // always include the last N lines for recency
	MaxLines  int                    // hard cap per pane after merging
	SentLines map[string]struct{}    // lines sent in the previous turn — deprioritized
}

func (o *Options) defaults() {
	if o.TopK <= 0 {
		o.TopK = 150
	}
	if o.LastN <= 0 {
		o.LastN = 30
	}
	if o.MaxLines <= 0 {
		o.MaxLines = 200
	}
}

// DefaultOptions returns sensible defaults for context selection.
func DefaultOptions() Options {
	return Options{TopK: 150, LastN: 30, MaxLines: 200}
}

// ExportKeywords extracts the signal keywords from a question (exported for debug UI).
func ExportKeywords(question string) []string {
	return extractKeywords(question)
}

// stopWords are common English words that carry no signal for relevance.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true,
	"but": true, "in": true, "on": true, "at": true, "to": true,
	"for": true, "of": true, "with": true, "is": true, "it": true,
	"be": true, "as": true, "by": true, "do": true, "if": true,
	"my": true, "me": true, "i": true, "we": true, "you": true,
	"he": true, "she": true, "they": true, "what": true, "how": true,
	"why": true, "when": true, "where": true, "this": true, "that": true,
	"was": true, "are": true, "not": true, "have": true, "has": true,
	"can": true, "will": true, "would": true, "could": true, "should": true,
}

// extractKeywords tokenises text into lowercase words, dropping stop words
// and single-character tokens.
func extractKeywords(text string) []string {
	raw := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.')
	})
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool)
	for _, w := range raw {
		if len(w) <= 1 || stopWords[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}

type scoredLine struct {
	idx   int
	score int
	text  string
}

// errorSignals are substrings that indicate a line is likely relevant to
// debugging regardless of keyword match. They get a score bonus.
var errorSignals = []string{
	"error", "err:", "errno", "failed", "failure",
	"panic", "fatal", "exception", "traceback", "stack trace",
	"warning", "warn:", "undefined", "not found", "no such",
	"permission denied", "connection refused", "timeout", "timed out",
	"exit status", "signal", "killed", "segfault", "core dumped",
}

// scoreLine counts how many keywords appear in line (case-insensitive),
// plus a bonus for lines that contain error/warning signals.
func scoreLine(lineLower string, keywords []string) int {
	n := 0
	for _, kw := range keywords {
		if strings.Contains(lineLower, kw) {
			n++
		}
	}
	// Error signal bonus: these lines are almost always relevant to debugging.
	for _, sig := range errorSignals {
		if strings.Contains(lineLower, sig) {
			n += 3
			break // one bonus per line is enough
		}
	}
	return n
}

// SelectForQuestion filters pane context down to the lines most relevant to
// the question, while always preserving the most recent lines for immediacy.
// The full pane content is used as the candidate pool — nothing is sent to
// Claude; this is pure local computation.
func SelectForQuestion(question string, panes []ipc.PaneContext, opts Options) []ipc.PaneContext {
	opts.defaults()
	keywords := extractKeywords(question)

	result := make([]ipc.PaneContext, 0, len(panes))
	for _, p := range panes {
		lines := p.Lines
		if len(lines) == 0 {
			continue
		}

		selected := selectLines(lines, keywords, opts)
		if len(selected) == 0 {
			// Fall back to last-N if nothing scored.
			start := len(lines) - opts.LastN
			if start < 0 {
				start = 0
			}
			selected = lines[start:]
		}

		result = append(result, ipc.PaneContext{PaneID: p.PaneID, Lines: selected})
	}
	return result
}

// selectLines picks the best lines from the candidate pool.
func selectLines(lines, keywords []string, opts Options) []string {
	n := len(lines)

	// Always include the last LastN lines (recency anchors).
	recencyStart := n - opts.LastN
	if recencyStart < 0 {
		recencyStart = 0
	}
	recencySet := make(map[int]bool)
	for i := recencyStart; i < n; i++ {
		recencySet[i] = true
	}

	// Score all lines that aren't already in the recency set.
	scored := make([]scoredLine, 0, n)
	for i, line := range lines {
		if recencySet[i] {
			continue
		}
		s := scoreLine(strings.ToLower(line), keywords)

		// Dedup penalty: lines sent in the previous turn are deprioritized.
		// They're still selected if they score high (e.g. error + keyword match),
		// but won't crowd out new context for weak matches.
		if opts.SentLines != nil {
			if _, wasSent := opts.SentLines[line]; wasSent {
				s -= 3
			}
		}

		if s > 0 {
			scored = append(scored, scoredLine{idx: i, score: s, text: line})
		}
	}

	// Sort by score descending, take top-K.
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})
	if len(scored) > opts.TopK {
		scored = scored[:opts.TopK]
	}

	// Expand each selected index to its surrounding command block so Claude
	// gets the command that produced relevant output, not just isolated lines.
	selectedIdxs := make([]int, len(scored))
	for i := range scored {
		selectedIdxs[i] = scored[i].idx
	}
	expandedIdxs := expandToCommandBlocks(lines, selectedIdxs)

	// Merge expanded block indices with recency set.
	idxSet := make(map[int]bool)
	for _, idx := range expandedIdxs {
		idxSet[idx] = true
	}
	for i := recencyStart; i < n; i++ {
		idxSet[i] = true
	}

	// Collect in original order.
	merged := make([]int, 0, len(idxSet))
	for idx := range idxSet {
		merged = append(merged, idx)
	}
	sort.Ints(merged)

	// Apply hard cap — prefer recency (keep the tail).
	if len(merged) > opts.MaxLines {
		merged = merged[len(merged)-opts.MaxLines:]
	}

	const maxLineChars = 500 // cap individual lines — prevents one giant line from blowing token budget
	out := make([]string, len(merged))
	for i, idx := range merged {
		l := lines[idx]
		if len(l) > maxLineChars {
			l = l[:maxLineChars] + "…"
		}
		out[i] = l
	}
	return out
}

// maxExpandLines is the largest output segment we'll expand to in full.
// Larger segments (e.g. ps aux, find, large log dumps) have self-contained
// lines — the matching line already carries all the context needed, so
// pulling in hundreds of surrounding lines wastes tokens.
const maxExpandLines = 40

// expandToCommandBlocks expands a set of selected line indices to include
// their surrounding command blocks. For each selected output line, we look up
// which segment it belongs to (using the same prompt-detection logic as
// TruncateBlocks) and include the full segment plus the preceding prompt
// segment (the command that produced the output).
//
// Large segments (>maxExpandLines) are NOT expanded — just the selected
// lines plus a small ±context window are returned.
func expandToCommandBlocks(lines []string, selectedIdxs []int) []int {
	if len(selectedIdxs) == 0 {
		return nil
	}

	segs := splitSegments(lines)
	if len(segs) == 0 {
		return selectedIdxs
	}

	// Map each line index to its segment index.
	lineToSeg := make([]int, len(lines))
	for si, seg := range segs {
		for i := seg.start; i < seg.end; i++ {
			lineToSeg[i] = si
		}
	}

	idxSet := make(map[int]bool)

	for _, idx := range selectedIdxs {
		if idx < 0 || idx >= len(lines) {
			continue
		}
		si := lineToSeg[idx]
		seg := segs[si]
		segLen := seg.end - seg.start

		if segLen > maxExpandLines {
			// Large output block: include just the matching line ± a small window.
			// The individual line is self-contained (ps aux, find, etc.).
			const window = 2
			start := idx - window
			if start < seg.start {
				start = seg.start
			}
			end := idx + window + 1
			if end > seg.end {
				end = seg.end
			}
			for i := start; i < end; i++ {
				idxSet[i] = true
			}
			// Include the command (preceding prompt segment) if it's short.
			if si > 0 && segs[si-1].isPrompt {
				ps := segs[si-1]
				for i := ps.start; i < ps.end; i++ {
					idxSet[i] = true
				}
			}
		} else {
			// Small output block: include the entire segment for full context.
			for i := seg.start; i < seg.end; i++ {
				idxSet[i] = true
			}
			// Include the preceding prompt segment (the command).
			if si > 0 && segs[si-1].isPrompt && !segs[si].isPrompt {
				ps := segs[si-1]
				for i := ps.start; i < ps.end; i++ {
					idxSet[i] = true
				}
			}
			// If selected line is a prompt, include the following output.
			if segs[si].isPrompt && si+1 < len(segs) {
				ns := segs[si+1]
				if ns.end-ns.start <= maxExpandLines {
					for i := ns.start; i < ns.end; i++ {
						idxSet[i] = true
					}
				}
			}
		}
	}

	result := make([]int, 0, len(idxSet))
	for i := range idxSet {
		result = append(result, i)
	}
	sort.Ints(result)
	return result
}

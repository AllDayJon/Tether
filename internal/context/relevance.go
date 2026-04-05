package context

import (
	"sort"
	"strings"
	"tether/internal/ipc"
)

// Options controls how SelectForQuestion trims pane context.
type Options struct {
	TopK     int // max top-scoring lines to include (default 20)
	LastN    int // always include the last N lines for recency (default 5)
	MaxLines int // hard cap per pane after merging (default 25)
}

func (o *Options) defaults() {
	if o.TopK <= 0 {
		o.TopK = 20
	}
	if o.LastN <= 0 {
		o.LastN = 5
	}
	if o.MaxLines <= 0 {
		o.MaxLines = 25
	}
}

// DefaultOptions returns sensible defaults for context selection.
func DefaultOptions() Options {
	return Options{TopK: 20, LastN: 5, MaxLines: 25}
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

// scoreLine counts how many keywords appear in line (case-insensitive).
func scoreLine(lineLower string, keywords []string) int {
	n := 0
	for _, kw := range keywords {
		if strings.Contains(lineLower, kw) {
			n++
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
		if len(keywords) == 0 {
			// No keywords: score by nothing; we'll just use recency.
			continue
		}
		s := scoreLine(strings.ToLower(line), keywords)
		if s > 0 {
			scored = append(scored, scoredLine{idx: i, score: s, text: line})
		}
	}

	// Sort by score descending, take top-K.
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})
	topK := opts.TopK
	if len(scored) > topK {
		scored = scored[:topK]
	}

	// Build a merged, deduplicated index set.
	idxSet := make(map[int]bool)
	for _, sl := range scored {
		idxSet[sl.idx] = true
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

	// Apply hard cap.
	if len(merged) > opts.MaxLines {
		// Keep the last MaxLines (prefer recency when capping).
		merged = merged[len(merged)-opts.MaxLines:]
	}

	out := make([]string, len(merged))
	for i, idx := range merged {
		out[i] = lines[idx]
	}
	return out
}

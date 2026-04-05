package context

import (
	"testing"
	"tether/internal/ipc"
)

// ── extractKeywords ───────────────────────────────────────────────────────────

func TestExtractKeywords_Basic(t *testing.T) {
	kws := extractKeywords("nginx 502 error")
	want := map[string]bool{"nginx": true, "502": true, "error": true}
	if len(kws) != len(want) {
		t.Fatalf("got %v, want keywords %v", kws, want)
	}
	for _, k := range kws {
		if !want[k] {
			t.Errorf("unexpected keyword %q", k)
		}
	}
}

func TestExtractKeywords_StopWordsRemoved(t *testing.T) {
	kws := extractKeywords("how do i check the error")
	for _, k := range kws {
		if stopWords[k] {
			t.Errorf("stop word %q should have been removed", k)
		}
	}
}

func TestExtractKeywords_Deduplicated(t *testing.T) {
	kws := extractKeywords("error error error")
	if len(kws) != 1 || kws[0] != "error" {
		t.Errorf("got %v, want [\"error\"]", kws)
	}
}

func TestExtractKeywords_SingleCharsDropped(t *testing.T) {
	kws := extractKeywords("a b c nginx")
	for _, k := range kws {
		if len(k) <= 1 {
			t.Errorf("single-char token %q should have been dropped", k)
		}
	}
}

func TestExtractKeywords_Empty(t *testing.T) {
	kws := extractKeywords("")
	if len(kws) != 0 {
		t.Errorf("expected empty, got %v", kws)
	}
}

// ── scoreLine ─────────────────────────────────────────────────────────────────

func TestScoreLine_MatchesKeywords(t *testing.T) {
	score := scoreLine("nginx upstream timeout 502", []string{"nginx", "502"})
	if score != 2 {
		t.Errorf("expected score 2, got %d", score)
	}
}

func TestScoreLine_NoMatch(t *testing.T) {
	score := scoreLine("ls -la /tmp", []string{"nginx", "502"})
	if score != 0 {
		t.Errorf("expected score 0, got %d", score)
	}
}

func TestScoreLine_EmptyKeywords(t *testing.T) {
	score := scoreLine("nginx upstream timeout", nil)
	if score != 0 {
		t.Errorf("expected score 0 with no keywords, got %d", score)
	}
}

// ── SelectForQuestion ─────────────────────────────────────────────────────────

func pane(id string, lines []string) ipc.PaneContext {
	return ipc.PaneContext{PaneID: id, Lines: lines}
}

func TestSelectForQuestion_ReturnsRelevantLines(t *testing.T) {
	lines := []string{
		"$ ls -la",
		"total 42",
		"502 Bad Gateway nginx",
		"$ cat /etc/hosts",
		"127.0.0.1 localhost",
	}
	panes := []ipc.PaneContext{pane("%0", lines)}
	result := SelectForQuestion("nginx 502 error", panes, DefaultOptions())

	if len(result) == 0 {
		t.Fatal("expected non-empty result")
	}
	found := false
	for _, l := range result[0].Lines {
		if l == "502 Bad Gateway nginx" {
			found = true
		}
	}
	if !found {
		t.Errorf("relevant line not in result: %v", result[0].Lines)
	}
}

func TestSelectForQuestion_AlwaysIncludesLastN(t *testing.T) {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "unrelated line"
	}
	lines[19] = "most recent line"

	opts := Options{TopK: 5, LastN: 3, MaxLines: 10}
	panes := []ipc.PaneContext{pane("%0", lines)}
	result := SelectForQuestion("completely unrelated question xyz", panes, opts)

	if len(result) == 0 {
		t.Fatal("expected result even when nothing scores")
	}
	last := result[0].Lines[len(result[0].Lines)-1]
	if last != "most recent line" {
		t.Errorf("last line should be included for recency, got %q", last)
	}
}

func TestSelectForQuestion_RespectsMaxLines(t *testing.T) {
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "nginx error 502 timeout upstream"
	}
	opts := Options{TopK: 40, LastN: 5, MaxLines: 10}
	panes := []ipc.PaneContext{pane("%0", lines)}
	result := SelectForQuestion("nginx 502", panes, opts)

	if len(result[0].Lines) > 10 {
		t.Errorf("MaxLines=10 violated: got %d lines", len(result[0].Lines))
	}
}

func TestSelectForQuestion_EmptyPanesSkipped(t *testing.T) {
	panes := []ipc.PaneContext{pane("%0", nil), pane("%1", []string{})}
	result := SelectForQuestion("nginx", panes, DefaultOptions())
	if len(result) != 0 {
		t.Errorf("empty panes should be omitted, got %d panes", len(result))
	}
}

func TestSelectForQuestion_PreservesOrder(t *testing.T) {
	lines := []string{"line-a nginx", "line-b", "line-c nginx", "line-d", "line-e"}
	panes := []ipc.PaneContext{pane("%0", lines)}
	result := SelectForQuestion("nginx", panes, Options{TopK: 10, LastN: 1, MaxLines: 10})

	// Results must appear in original index order.
	for i := 1; i < len(result[0].Lines); i++ {
		prev := result[0].Lines[i-1]
		curr := result[0].Lines[i]
		prevIdx, currIdx := -1, -1
		for j, l := range lines {
			if l == prev {
				prevIdx = j
			}
			if l == curr {
				currIdx = j
			}
		}
		if prevIdx > currIdx {
			t.Errorf("lines out of order: %q (idx %d) before %q (idx %d)", prev, prevIdx, curr, currIdx)
		}
	}
}

func TestSelectForQuestion_FallsBackToLastNWhenNothingScores(t *testing.T) {
	lines := []string{"aaa", "bbb", "ccc", "ddd", "eee"}
	opts := Options{TopK: 5, LastN: 2, MaxLines: 10}
	panes := []ipc.PaneContext{pane("%0", lines)}
	result := SelectForQuestion("zzz completely unmatched", panes, opts)

	// Should fall back to last 2 lines.
	if len(result) == 0 {
		t.Fatal("expected fallback result")
	}
	got := result[0].Lines
	if len(got) != 2 || got[0] != "ddd" || got[1] != "eee" {
		t.Errorf("fallback: got %v, want [ddd eee]", got)
	}
}

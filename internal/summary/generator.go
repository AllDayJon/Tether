package summary

import (
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"tether/internal/ipc"
	"tether/internal/watcher"
	"os"
)

const (
	DefaultInterval = 5 * time.Minute
	contextLines    = 200 // lines fed to the summariser
	minLinesForGen  = 20  // don't generate a summary until we have this many lines
)

const summaryPrompt = `Summarize what this person has been doing in their terminal in 3-5 sentences.
Be specific: mention commands run, files edited, servers accessed, errors seen, and current state.
Write in present-perfect tense ("The user has been..."). Be concise — no filler.

Terminal output:
`

// Generator periodically produces a prose summary of terminal activity.
type Generator struct {
	w        *watcher.Watcher
	interval time.Duration
	filePath string

	mu      sync.RWMutex
	summary string

	stopCh  chan struct{}
	stopped bool
}

// New creates a Generator. filePath is where the summary is persisted across restarts.
func New(w *watcher.Watcher, interval time.Duration, filePath string) *Generator {
	g := &Generator{
		w:        w,
		interval: interval,
		filePath: filePath,
		stopCh:   make(chan struct{}),
	}
	// Load any persisted summary from a previous run.
	if data, err := os.ReadFile(filePath); err == nil {
		g.summary = strings.TrimSpace(string(data))
	}
	return g
}

// DefaultPath returns ~/.tether/summary.txt.
func DefaultPath() (string, error) {
	dir, err := ipc.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "summary.txt"), nil
}

// Start launches the background summarisation loop.
func (g *Generator) Start() {
	go g.loop()
}

// Stop shuts down the background loop.
func (g *Generator) Stop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.stopped {
		g.stopped = true
		close(g.stopCh)
	}
}

// Get returns the current summary (empty string if none yet).
func (g *Generator) Get() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.summary
}

// Trigger forces an immediate regeneration regardless of the timer.
func (g *Generator) Trigger() {
	go g.regenerate()
}

func (g *Generator) loop() {
	ticker := time.NewTicker(g.interval)
	defer ticker.Stop()
	for {
		select {
		case <-g.stopCh:
			return
		case <-ticker.C:
			g.regenerate()
		}
	}
}

func (g *Generator) regenerate() {
	panes := g.w.WatchedPanes()
	if len(panes) == 0 {
		return
	}

	// Collect recent lines from all watched panes.
	var lines []string
	for _, id := range panes {
		lines = append(lines, g.w.Last(id, contextLines)...)
	}
	if len(lines) < minLinesForGen {
		return
	}

	// Cap to contextLines total to keep prompt small.
	if len(lines) > contextLines {
		lines = lines[len(lines)-contextLines:]
	}

	prompt := summaryPrompt + strings.Join(lines, "\n")

	cmd := exec.Command("claude", "-p", prompt)
	out, err := cmd.Output()
	if err != nil {
		log.Printf("[summary] generation failed: %v", err)
		return
	}

	result := strings.TrimSpace(string(out))
	if result == "" {
		return
	}

	g.mu.Lock()
	g.summary = result
	g.mu.Unlock()

	// Persist so it survives daemon restarts.
	if g.filePath != "" {
		os.WriteFile(g.filePath, []byte(result), 0600)
	}

	log.Printf("[summary] updated (%d chars)", len(result))
}

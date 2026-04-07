package summary

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"tether/internal/ipc"
	"tether/internal/session"
)

const (
	DefaultInterval = 5 * time.Minute
	contextLines    = 200 // lines fed to the summariser
	minLinesForGen  = 20  // don't generate until we have this many lines
)

const summaryPrompt = `Summarize what this person has been doing in their terminal in 3-5 sentences.
Be specific: mention commands run, files edited, servers accessed, errors seen, and current state.
Write in present-perfect tense ("The user has been..."). Be concise — no filler.

Terminal output:
`

// Generator periodically produces a prose summary of terminal activity.
type Generator struct {
	buf      *session.Buffer
	interval time.Duration
	filePath string

	mu      sync.RWMutex
	summary string

	stopCh  chan struct{}
	stopped bool
}

// New creates a Generator. filePath is where the summary is persisted across restarts.
func New(buf *session.Buffer, interval time.Duration, filePath string) *Generator {
	g := &Generator{
		buf:      buf,
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
	lines := g.buf.Last(contextLines)
	if len(lines) < minLinesForGen {
		return
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

	// Persist so it survives proxy restarts.
	if g.filePath != "" {
		if err := os.WriteFile(g.filePath, []byte(result), 0600); err != nil {
			log.Printf("[summary] failed to persist summary: %v", err)
		}
	}

	log.Printf("[summary] updated (%d chars)", len(result))
}

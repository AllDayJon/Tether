package watcher

import (
	"crypto/sha256"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	defaultBufferSize   = 1000 // lines per pane
	defaultPollLines    = 500  // lines captured per poll
	defaultPollInterval = 750 * time.Millisecond
)

// Watcher polls tmux panes and stores their output in append-only ring buffers.
type Watcher struct {
	socketPath string
	session    string

	mu      sync.Mutex
	panes   map[string]*paneState
	stopCh  chan struct{}
	stopped bool
}

type paneState struct {
	buf      *RingBuffer
	lastHash [32]byte // sha256 of last captured content
	seenN    int      // how many buffered lines Claude has already been sent
}

// New creates a Watcher for the given tmux socket and session.
func New(socketPath, session string) *Watcher {
	return &Watcher{
		socketPath: socketPath,
		session:    session,
		panes:      make(map[string]*paneState),
		stopCh:     make(chan struct{}),
	}
}

// Watch starts observing a pane. Safe to call multiple times on the same pane.
func (w *Watcher) Watch(paneID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.panes[paneID]; !ok {
		w.panes[paneID] = &paneState{buf: NewRingBuffer(defaultBufferSize)}
	}
}

// Unwatch stops observing a pane and discards its buffer.
func (w *Watcher) Unwatch(paneID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.panes, paneID)
}

// ClearAll resets all ring buffers and seen cursors.
func (w *Watcher) ClearAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, ps := range w.panes {
		ps.buf.Reset()
		ps.lastHash = [32]byte{}
		ps.seenN = 0
	}
}

// ResetSeen resets the seen cursor for all panes without clearing the buffer.
// Call this when starting a new conversation so the next Delta sends everything.
func (w *Watcher) ResetSeen() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, ps := range w.panes {
		ps.seenN = 0
	}
}

// WatchedPanes returns the IDs of all currently watched panes.
func (w *Watcher) WatchedPanes() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	ids := make([]string, 0, len(w.panes))
	for id := range w.panes {
		ids = append(ids, id)
	}
	return ids
}

// Last returns up to n lines from paneID's buffer (for full-context requests).
func (w *Watcher) Last(paneID string, n int) []string {
	w.mu.Lock()
	ps, ok := w.panes[paneID]
	w.mu.Unlock()
	if !ok {
		return nil
	}
	return ps.buf.Last(n)
}

// Delta returns lines added to paneID's buffer since the last Delta call,
// then advances the seen cursor. Returns nil if nothing is new.
func (w *Watcher) Delta(paneID string) []string {
	w.mu.Lock()
	defer w.mu.Unlock()

	ps, ok := w.panes[paneID]
	if !ok {
		return nil
	}

	total := ps.buf.Len()

	// If seenN > total the buffer was cleared; restart from 0.
	if ps.seenN > total {
		ps.seenN = 0
	}
	if ps.seenN >= total {
		return nil
	}

	all := ps.buf.Last(total)
	delta := make([]string, len(all[ps.seenN:]))
	copy(delta, all[ps.seenN:])
	ps.seenN = total
	return delta
}

// SeenCount returns how many lines have already been sent for paneID.
func (w *Watcher) SeenCount(paneID string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ps, ok := w.panes[paneID]; ok {
		return ps.seenN
	}
	return 0
}

// BufferLen returns the number of lines stored for paneID.
func (w *Watcher) BufferLen(paneID string) int {
	w.mu.Lock()
	ps, ok := w.panes[paneID]
	w.mu.Unlock()
	if !ok {
		return 0
	}
	return ps.buf.Len()
}

// Start begins polling in the background.
func (w *Watcher) Start() {
	go w.loop()
}

// Stop terminates the polling goroutine.
func (w *Watcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.stopped {
		w.stopped = true
		close(w.stopCh)
	}
}

func (w *Watcher) loop() {
	ticker := time.NewTicker(defaultPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.pollAll()
		}
	}
}

func (w *Watcher) pollAll() {
	w.mu.Lock()
	ids := make([]string, 0, len(w.panes))
	for id := range w.panes {
		ids = append(ids, id)
	}
	w.mu.Unlock()

	for _, id := range ids {
		w.pollPane(id)
	}
}

// pollPane captures pane output and appends only genuinely new lines to the
// ring buffer. The buffer is append-only so that the seen cursor stays valid.
func (w *Watcher) pollPane(paneID string) {
	content, err := capturePaneOutput(w.socketPath, paneID, defaultPollLines)
	if err != nil {
		return
	}

	hash := sha256.Sum256([]byte(content))

	w.mu.Lock()
	ps, ok := w.panes[paneID]
	if !ok {
		w.mu.Unlock()
		return
	}
	if ps.lastHash == hash {
		w.mu.Unlock()
		return
	}
	ps.lastHash = hash

	// Compute new lines by diffing against what's already in the buffer.
	existing := ps.buf.Last(ps.buf.Len())
	w.mu.Unlock()

	incoming := splitLines(content)
	newLines := NewLinesSince(existing, incoming)

	if len(newLines) > 0 {
		w.mu.Lock()
		if ps, ok := w.panes[paneID]; ok {
			ps.buf.WriteAll(newLines)
		}
		w.mu.Unlock()
	}
}

// SendKeys injects a command string into paneID as if typed by the user.
// The command is followed by Enter so it executes immediately.
// This works transparently through SSH — tmux injects keystrokes at the
// terminal level, so the remote shell receives and executes them.
func (w *Watcher) SendKeys(paneID, command string) error {
	args := []string{}
	if w.socketPath != "" {
		args = append(args, "-S", w.socketPath)
	}
	args = append(args, "send-keys", "-t", paneID, command, "Enter")
	return exec.Command("tmux", args...).Run()
}

func capturePaneOutput(socketPath, paneID string, numLines int) (string, error) {
	args := []string{}
	if socketPath != "" {
		args = append(args, "-S", socketPath)
	}
	args = append(args, "capture-pane", "-p", "-t", paneID, "-S", fmt.Sprintf("-%d", numLines))
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func splitLines(s string) []string {
	lines := strings.Split(s, "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

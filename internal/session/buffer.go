// Package session provides an event-driven context buffer for the PTY proxy.
// The PTY proxy calls Append as bytes arrive from the shell; Last and Delta
// expose the buffered lines to the daemon and chat TUI over IPC.
package session

import (
	"strings"
	"sync"
)

const (
	defaultBufferSize = 5000 // lines kept in memory
)

// Buffer is a thread-safe, event-driven ring buffer for terminal output lines.
// The PTY proxy feeds it directly; consumers use Last/Delta to read context.
type Buffer struct {
	mu      sync.Mutex
	lines   []string // circular storage
	cap     int      // max lines stored
	head    int      // index of oldest line
	total   int      // total lines ever written (never decreases)
	seenN   int      // lines already sent to Claude via Delta
	partial string   // incomplete line waiting for \n
}

// New returns a Buffer with the default ring size.
func New() *Buffer {
	return &Buffer{
		lines: make([]string, defaultBufferSize),
		cap:   defaultBufferSize,
	}
}

// Write implements io.Writer so the PTY output stream can write directly.
// Splits on newlines; incomplete lines are held until the next Write.
func (b *Buffer) Write(data []byte) (int, error) {
	text := b.partial + string(data)
	parts := strings.Split(text, "\n")
	// All but the last element are complete lines.
	complete := parts[:len(parts)-1]
	b.partial = parts[len(parts)-1]

	if len(complete) > 0 {
		b.mu.Lock()
		for _, line := range complete {
			b.appendLocked(line)
		}
		b.mu.Unlock()
	}
	return len(data), nil
}

// Append adds pre-split lines directly. Used by structured OSC 133 events.
func (b *Buffer) Append(lines []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, l := range lines {
		b.appendLocked(l)
	}
}

func (b *Buffer) appendLocked(line string) {
	idx := (b.head + b.total) % b.cap
	b.lines[idx] = line
	b.total++
	if b.total > b.cap {
		// Oldest line evicted — advance head.
		b.head = (b.head + 1) % b.cap
		if b.seenN > 0 {
			b.seenN-- // keep seenN relative to actual stored lines
		}
	}
}

// Len returns the number of lines currently stored.
func (b *Buffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.total < b.cap {
		return b.total
	}
	return b.cap
}

// Last returns the most recent n lines (up to what is stored).
func (b *Buffer) Last(n int) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	stored := b.storedLocked()
	if n > stored {
		n = stored
	}
	if n == 0 {
		return nil
	}
	out := make([]string, n)
	start := (b.head + stored - n) % b.cap
	for i := 0; i < n; i++ {
		out[i] = b.lines[(start+i)%b.cap]
	}
	return out
}

// Delta returns lines added since the last Delta call and advances the cursor.
func (b *Buffer) Delta() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	stored := b.storedLocked()
	if b.seenN >= stored {
		return nil
	}
	unseen := stored - b.seenN
	out := make([]string, unseen)
	start := (b.head + b.seenN) % b.cap
	for i := 0; i < unseen; i++ {
		out[i] = b.lines[(start+i)%b.cap]
	}
	b.seenN = stored
	return out
}

// ResetSeen resets the Delta cursor so the next call returns all stored lines.
func (b *Buffer) ResetSeen() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seenN = 0
}

// Clear discards all stored lines and resets the cursor.
func (b *Buffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.head = 0
	b.total = 0
	b.seenN = 0
	b.partial = ""
}

func (b *Buffer) storedLocked() int {
	if b.total < b.cap {
		return b.total
	}
	return b.cap
}

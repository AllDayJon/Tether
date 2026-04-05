package watcher

import "sync"

// RingBuffer is a fixed-capacity circular buffer of strings.
// When full, new writes overwrite the oldest entries.
type RingBuffer struct {
	buf  []string
	size int
	head int // index of the next write slot
	n    int // number of entries currently stored
	mu   sync.Mutex
}

// NewRingBuffer creates a RingBuffer with the given capacity.
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		buf:  make([]string, size),
		size: size,
	}
}

// Write appends a line to the buffer, evicting the oldest if full.
func (rb *RingBuffer) Write(line string) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.buf[rb.head] = line
	rb.head = (rb.head + 1) % rb.size
	if rb.n < rb.size {
		rb.n++
	}
}

// WriteAll appends multiple lines at once.
func (rb *RingBuffer) WriteAll(lines []string) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	for _, l := range lines {
		rb.buf[rb.head] = l
		rb.head = (rb.head + 1) % rb.size
		if rb.n < rb.size {
			rb.n++
		}
	}
}

// Last returns up to n of the most recently written lines, oldest first.
func (rb *RingBuffer) Last(n int) []string {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if n > rb.n {
		n = rb.n
	}
	if n == 0 {
		return nil
	}
	out := make([]string, n)
	// The oldest of the last-n entries is at (head - n) mod size.
	start := ((rb.head - n) % rb.size + rb.size) % rb.size
	for i := 0; i < n; i++ {
		out[i] = rb.buf[(start+i)%rb.size]
	}
	return out
}

// Len returns the number of lines currently stored.
func (rb *RingBuffer) Len() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.n
}

// Reset empties the buffer.
func (rb *RingBuffer) Reset() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.head = 0
	rb.n = 0
}

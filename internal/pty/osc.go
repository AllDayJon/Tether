package pty

import (
	"strings"
	"tether/internal/session"
)

// oscParser is a streaming byte parser that sits in the PTY→stdout pipeline.
// It intercepts OSC 133 A/B/C/D sequences to detect command boundaries, then
// feeds structured output into the session buffer. All bytes (including the
// OSC sequences) pass through to stdout so terminals that understand them
// (kitty, ghostty) can use them for their own shell integration features.
//
// OSC 133 protocol:
//   ESC ] 133 ; A ST  — prompt start
//   ESC ] 133 ; B ST  — command input start (user typing)
//   ESC ] 133 ; C ST  — command executing (Enter pressed)
//   ESC ] 133 ; D ; N ST — command done, exit code N
//
// ST (string terminator) is either ESC \ or BEL (0x07).
type oscParser struct {
	buf     *session.Buffer
	state   parseState
	oscBody   []byte // accumulates the body of an OSC sequence
	oscSawESC bool   // true when the last byte in stateOSC was ESC (first byte of ESC \ ST)

	// Output accumulation: lines captured between OSC C and OSC D.
	capturing bool
	outLines  []byte // raw output bytes during command execution

	// Line splitter for non-command output.
	lineBuf string
}

type parseState int

const (
	stateNormal parseState = iota
	stateESC               // saw ESC, waiting for next byte
	stateOSC               // inside an OSC sequence body
)

func newOSCParser(buf *session.Buffer) *oscParser {
	return &oscParser{buf: buf}
}

// Write processes a chunk of bytes from the PTY. It updates internal state
// but does NOT write anything — the caller uses io.MultiWriter to send bytes
// to both stdout and this parser simultaneously.
func (p *oscParser) Write(data []byte) (int, error) {
	for _, b := range data {
		switch p.state {
		case stateNormal:
			if b == 0x1b { // ESC
				p.state = stateESC
			} else {
				p.handleNormalByte(b)
			}

		case stateESC:
			if b == ']' { // ESC ] starts an OSC sequence
				p.state = stateOSC
				p.oscBody = p.oscBody[:0]
			} else if b == '\\' {
				// ESC \ as a lone sequence — not an OSC terminator here.
				p.state = stateNormal
			} else {
				// Some other escape sequence — not our concern.
				p.state = stateNormal
				p.handleNormalByte(b)
			}

		case stateOSC:
			if b == 0x07 { // BEL terminates OSC
				p.handleOSC(string(p.oscBody))
				p.oscSawESC = false
				p.state = stateNormal
			} else if b == 0x1b {
				// First byte of ESC \ (ST) — record and wait for the backslash.
				p.oscSawESC = true
			} else if b == '\\' && p.oscSawESC {
				// ESC \ — proper String Terminator.
				p.handleOSC(string(p.oscBody))
				p.oscSawESC = false
				p.state = stateNormal
			} else {
				if p.oscSawESC {
					// The ESC was not followed by \ — not an ST, keep accumulating.
					p.oscBody = append(p.oscBody, 0x1b)
					p.oscSawESC = false
				}
				p.oscBody = append(p.oscBody, b)
			}
		}
	}
	return len(data), nil
}

// handleNormalByte accumulates non-OSC bytes. During a command execution,
// bytes go only into the capture buffer — lineBuf is not accumulated while
// capturing to prevent duplicate output when the command block is flushed.
func (p *oscParser) handleNormalByte(b byte) {
	if p.capturing {
		p.outLines = append(p.outLines, b)
		return
	}
	// Line splitting for non-command output only.
	if b == '\n' {
		if p.lineBuf != "" {
			clean := stripANSI(p.lineBuf)
			if clean != "" {
				p.buf.Append([]string{clean})
			}
			p.lineBuf = ""
		}
	} else if b != '\r' {
		p.lineBuf += string(b)
	}
}

// handleOSC is called when a complete OSC sequence has been received.
func (p *oscParser) handleOSC(body string) {
	if !strings.HasPrefix(body, "133;") {
		return // Not an OSC 133 sequence — ignore.
	}
	payload := body[4:] // strip "133;"

	switch {
	case payload == "A": // prompt start — flush any pending line buffer
		if p.lineBuf != "" {
			p.buf.Append([]string{p.lineBuf})
			p.lineBuf = ""
		}

	case payload == "B": // command input start
		// Nothing to do; we don't intercept keystrokes here.

	case payload == "C": // command executing — start capturing output
		if p.lineBuf != "" {
			p.buf.Append([]string{p.lineBuf})
			p.lineBuf = ""
		}
		p.capturing = true
		p.outLines = p.outLines[:0]

	case strings.HasPrefix(payload, "D"): // command done
		if p.capturing {
			p.capturing = false
			lines := splitOutputLines(string(p.outLines))
			if len(lines) > 0 {
				p.buf.Append(lines)
			}
			p.outLines = p.outLines[:0]
		}
	}
}

// splitOutputLines splits raw PTY output into clean lines, stripping
// ANSI escape sequences, carriage returns, and blank lines.
func splitOutputLines(raw string) []string {
	parts := strings.Split(raw, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		line := stripANSI(strings.TrimRight(p, "\r"))
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// stripANSI removes ANSI/VT100 escape sequences from s, leaving only
// printable text. This covers:
//   - CSI sequences: ESC [ ... <final byte>  (colors, cursor movement, modes)
//   - OSC sequences: ESC ] ... BEL/ST        (already intercepted, but strip anyway)
//   - Two-byte ESC sequences: ESC <byte>
//   - C1 control codes: 0x80–0x9F
func stripANSI(s string) string {
	if !strings.ContainsRune(s, 0x1b) && !hasC1Controls(s) {
		return s // fast path — no escape sequences
	}

	var out []byte
	i := 0
	b := []byte(s)
	for i < len(b) {
		if b[i] == 0x1b {
			i++ // consume ESC
			if i >= len(b) {
				break
			}
			switch b[i] {
			case '[': // CSI — consume until final byte (0x40–0x7E)
				i++
				for i < len(b) && (b[i] < 0x40 || b[i] > 0x7e) {
					i++
				}
				i++ // consume final byte
			case ']': // OSC — consume until BEL or ESC \
				i++
				for i < len(b) {
					if b[i] == 0x07 {
						i++
						break
					}
					if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
			case 'P', 'X', '^', '_': // DCS, SOS, PM, APC — consume until ST
				i++
				for i < len(b) {
					if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
			default:
				// Two-byte escape sequence (e.g. ESC M, ESC =, ESC >, ESC ?)
				i++ // consume the second byte
			}
		} else if b[i] >= 0x80 && b[i] <= 0x9f {
			// C1 control code — skip it (handles 8-bit CSI, OSC, etc.)
			i++
		} else {
			out = append(out, b[i])
			i++
		}
	}
	return string(out)
}

func hasC1Controls(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 && s[i] <= 0x9f {
			return true
		}
	}
	return false
}

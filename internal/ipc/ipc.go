package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Message types sent from CLI clients to the proxy daemon.
type MsgType string

const (
	TypeStatus          MsgType = "status"
	TypeGetContext      MsgType = "get_context"
	TypeClearBuffers    MsgType = "clear_buffers"
	TypeResetSeen       MsgType = "reset_seen"
	TypeStop            MsgType = "stop"
	TypeExec            MsgType = "exec"              // write a command to the PTY shell
	TypeSetMode         MsgType = "set_mode"          // change Watch/Assist/Act mode
	TypeAddSessionAllow MsgType = "add_session_allow" // add pattern to in-memory allow list
	TypeGetSessionAllow MsgType = "get_session_allow" // fetch current session allow list
)

// Mode controls how tether handles commands Claude suggests.
type Mode string

const (
	ModeWatch  Mode = "watch"  // read-only — Claude observes and advises only
	ModeAssist Mode = "assist" // Claude proposes commands; auto-run can be toggled on
)

// Msg is the envelope for all IPC messages.
type Msg struct {
	Type    MsgType         `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// ExecPayload is the payload for TypeExec.
type ExecPayload struct {
	Command string `json:"command"`
}

// SetModePayload is the payload for TypeSetMode.
type SetModePayload struct {
	Mode Mode `json:"mode"`
}

// AddSessionAllowPayload is the payload for TypeAddSessionAllow.
type AddSessionAllowPayload struct {
	Pattern string `json:"pattern"`
}

// GetContextPayload is the payload for TypeGetContext.
type GetContextPayload struct {
	NLines    int  `json:"n_lines"`
	DeltaOnly bool `json:"delta_only"`
}

// --- Responses ---

type OKResp struct {
	OK bool `json:"ok"`
}

type ErrResp struct {
	Error string `json:"error"`
}

type StatusResp struct {
	Running       bool     `json:"running"`
	Mode          Mode     `json:"mode"`
	Shell         string   `json:"shell"`
	BufferedLines int      `json:"buffered_lines"`
	SessionAllow  []string `json:"session_allow,omitempty"`
}

type ContextResp struct {
	Lines   []string `json:"lines"`
	Summary string   `json:"summary,omitempty"`
}

// PaneContext is a compatibility shim for the conversation/context packages.
// In PTY mode there is one entry per active tether session.
type PaneContext struct {
	PaneID string   `json:"pane_id"`
	Lines  []string `json:"lines"`
}

// --- Path helpers ---

func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".tether"), nil
}

// SessionsDir returns ~/.tether/sessions/ — one socket per tether shell instance.
func SessionsDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sessions"), nil
}

// SessionSocketPath returns the Unix socket path for a specific session PID.
func SessionSocketPath(pid int) (string, error) {
	dir, err := SessionsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("%d.sock", pid)), nil
}

// SocketPath returns the legacy single-session socket path (kept for compatibility).
func SocketPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tether.sock"), nil
}

func PIDPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon.pid"), nil
}

func LogPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon.log"), nil
}

func ChatDebugLogPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "chat-debug.log"), nil
}

func EnsureDir() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0700)
}

func EnsureSessionsDir() error {
	dir, err := SessionsDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0700)
}

// --- Session discovery ---

// SessionInfo describes a discovered active tether session.
type SessionInfo struct {
	PID        int
	SocketPath string
}

// ListActiveSessions scans ~/.tether/sessions/ and returns all sessions with a
// responsive daemon. Stale socket files (dead processes) are removed automatically.
func ListActiveSessions() []SessionInfo {
	dir, err := SessionsDir()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var sessions []SessionInfo
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".sock" {
			continue
		}
		sockPath := filepath.Join(dir, e.Name())
		conn, err := net.DialTimeout("unix", sockPath, 200*time.Millisecond)
		if err != nil {
			os.Remove(sockPath)
			continue
		}
		conn.Close()
		name := strings.TrimSuffix(e.Name(), ".sock")
		pid := 0
		fmt.Sscanf(name, "%d", &pid)
		sessions = append(sessions, SessionInfo{PID: pid, SocketPath: sockPath})
	}
	return sessions
}

// DialSession connects to a specific session by socket path.
func DialSession(sockPath string) (net.Conn, error) {
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to tether session: %w", err)
	}
	return conn, nil
}

// DialAny connects to the most recently started active session.
// Returns an error if no sessions are running.
func DialAny() (net.Conn, error) {
	sessions := ListActiveSessions()
	if len(sessions) == 0 {
		return nil, fmt.Errorf("no active tether sessions (start one with: tether shell)")
	}
	return DialSession(sessions[len(sessions)-1].SocketPath)
}

// Dial connects to a tether session. It tries per-session sockets first,
// falling back to the legacy single socket for backward compatibility.
func Dial() (net.Conn, error) {
	conn, err := DialAny()
	if err == nil {
		return conn, nil
	}
	// Fallback: try legacy socket.
	sock, legacyErr := SocketPath()
	if legacyErr != nil {
		return nil, err // return the DialAny error
	}
	conn, legacyErr = net.Dial("unix", sock)
	if legacyErr != nil {
		return nil, fmt.Errorf("no active tether sessions (start one with: tether shell)")
	}
	return conn, nil
}

// --- Wire helpers ---

func Send(conn net.Conn, msg Msg) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = conn.Write(append(b, '\n'))
	return err
}

func Recv(conn net.Conn, v any) error {
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 4<<20), 4<<20) // 4 MB — large enough for 5000-line context dumps
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return err
		}
		return fmt.Errorf("connection closed before response")
	}
	return json.Unmarshal(scanner.Bytes(), v)
}

func SendMsg(conn net.Conn, t MsgType, payload any) error {
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		raw = b
	}
	return Send(conn, Msg{Type: t, Payload: raw})
}

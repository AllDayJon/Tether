package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// Message types sent from CLI to daemon.
type MsgType string

const (
	TypeWatch        MsgType = "watch"
	TypeUnwatch      MsgType = "unwatch"
	TypeStatus       MsgType = "status"
	TypeGetContext   MsgType = "get_context"
	TypeClearBuffers MsgType = "clear_buffers"
	TypeResetSeen    MsgType = "reset_seen"
	TypeStop         MsgType = "stop"
	TypeExecInPane   MsgType = "exec_in_pane" // inject a command into a watched pane via send-keys
	TypeSetMode          MsgType = "set_mode"           // change the current Watch/Assist/Act mode
	TypeAddSessionAllow  MsgType = "add_session_allow"  // add a pattern to the in-memory allow list
	TypeGetSessionAllow  MsgType = "get_session_allow"  // fetch the current session allow list
)

// Mode controls how tether handles commands Claude suggests.
type Mode string

const (
	ModeWatch  Mode = "watch"  // read-only — Claude observes and advises only
	ModeAssist Mode = "assist" // Claude proposes commands, human approves each one
	ModeAct    Mode = "act"    // Claude auto-executes allow-listed commands
)

// Msg is the envelope for all IPC messages.
type Msg struct {
	Type    MsgType         `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// WatchPayload is the payload for TypeWatch / TypeUnwatch.
type WatchPayload struct {
	PaneID string `json:"pane_id"`
}

// SetModePayload is the payload for TypeSetMode.
type SetModePayload struct {
	Mode Mode `json:"mode"`
}

// AddSessionAllowPayload is the payload for TypeAddSessionAllow.
type AddSessionAllowPayload struct {
	Pattern string `json:"pattern"` // command prefix to allow for the session
}

// ExecInPanePayload is the payload for TypeExecInPane.
// The daemon uses tmux send-keys to inject the command into the pane, which
// works transparently through SSH sessions.
type ExecInPanePayload struct {
	PaneID  string `json:"pane_id"`
	Command string `json:"command"`
}

// GetContextPayload is the payload for TypeGetContext.
type GetContextPayload struct {
	NLines    int  `json:"n_lines"`    // max lines per pane (full mode); 0 = default (50)
	DeltaOnly bool `json:"delta_only"` // if true, return only lines since last call and advance cursor
}

// --- Responses ---

// OKResp is a generic success response.
type OKResp struct {
	OK bool `json:"ok"`
}

// ErrResp is returned when the daemon encounters an error.
type ErrResp struct {
	Error string `json:"error"`
}

// StatusResp is the response to TypeStatus.
type StatusResp struct {
	Running      bool           `json:"running"`
	TmuxSocket   string         `json:"tmux_socket"`
	TmuxSession  string         `json:"tmux_session"`
	WatchedPanes []string       `json:"watched_panes"`
	BufferSizes  map[string]int `json:"buffer_sizes"` // pane_id → lines stored
	Mode         Mode           `json:"mode"`
	SessionAllow []string       `json:"session_allow,omitempty"` // in-memory allow list for this session
}

// ContextResp is the response to TypeGetContext.
type ContextResp struct {
	Panes   []PaneContext `json:"panes"`
	Summary string        `json:"summary,omitempty"` // rolling session summary (may be empty)
}

// PaneContext holds the recent output from one pane.
type PaneContext struct {
	PaneID string   `json:"pane_id"`
	Lines  []string `json:"lines"`
}

// --- Path helpers ---

// Dir returns the ~/.tether directory path.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".tether"), nil
}

// SocketPath returns the Unix socket path for the daemon.
func SocketPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tether.sock"), nil
}

// PIDPath returns the path to the daemon PID file.
func PIDPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon.pid"), nil
}

// LogPath returns the path to the daemon log file.
func LogPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon.log"), nil
}

// ChatDebugLogPath returns the path to the chat debug log file.
func ChatDebugLogPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "chat-debug.log"), nil
}

// EnsureDir creates ~/.tether if it doesn't exist.
func EnsureDir() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0700)
}

// --- Client helpers ---

// Dial connects to the daemon socket and returns a buffered connection.
func Dial() (net.Conn, error) {
	sock, err := SocketPath()
	if err != nil {
		return nil, err
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to daemon (is it running? try: tether start): %w", err)
	}
	return conn, nil
}

// Send writes a message to conn as a JSON line.
func Send(conn net.Conn, msg Msg) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = conn.Write(b)
	return err
}

// Recv reads one JSON line from conn and unmarshals it into v.
func Recv(conn net.Conn, v any) error {
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return err
		}
		return fmt.Errorf("connection closed before response")
	}
	return json.Unmarshal(scanner.Bytes(), v)
}

// SendMsg is a convenience wrapper that marshals payload and sends a Msg.
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

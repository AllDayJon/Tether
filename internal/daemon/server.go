package daemon

import (
	"bufio"
	"encoding/json"
	"log"
	"net"
	"sync"
	"tether/internal/ipc"
	"tether/internal/session"
	"tether/internal/summary"
)

type server struct {
	sockPath string
	buf      *session.Buffer
	gen      *summary.Generator
	shell    string
	execFn   func(cmd string) error // writes a command to the PTY shell
	listener net.Listener
	stopCh   chan struct{}

	modeMu       sync.RWMutex
	mode         ipc.Mode
	sessionAllow []string
}

func newServer(sockPath string, buf *session.Buffer, gen *summary.Generator, shell string, execFn func(cmd string) error) *server {
	return &server{
		sockPath: sockPath,
		buf:      buf,
		gen:      gen,
		shell:    shell,
		execFn:   execFn,
		stopCh:   make(chan struct{}),
		mode:     ipc.ModeWatch,
	}
}

func (s *server) start() error {
	ln, err := net.Listen("unix", s.sockPath)
	if err != nil {
		return err
	}
	s.listener = ln
	go s.acceptLoop()
	log.Printf("IPC socket listening at %s", s.sockPath)
	return nil
}

func (s *server) stop() {
	if s.listener != nil {
		s.listener.Close()
	}
}

func (s *server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *server) handleConn(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var msg ipc.Msg
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			s.writeErr(conn, "malformed message: "+err.Error())
			continue
		}
		s.dispatch(conn, msg)
	}
}

func (s *server) dispatch(conn net.Conn, msg ipc.Msg) {
	switch msg.Type {

	case ipc.TypeSetMode:
		var p ipc.SetModePayload
		if err := json.Unmarshal(msg.Payload, &p); err != nil {
			s.writeErr(conn, "bad set_mode payload: "+err.Error())
			return
		}
		if p.Mode != ipc.ModeWatch && p.Mode != ipc.ModeAssist {
			s.writeErr(conn, "unknown mode: "+string(p.Mode))
			return
		}
		s.modeMu.Lock()
		s.mode = p.Mode
		s.modeMu.Unlock()
		log.Printf("mode set to %s", p.Mode)
		s.writeOK(conn)

	case ipc.TypeAddSessionAllow:
		var p ipc.AddSessionAllowPayload
		if err := json.Unmarshal(msg.Payload, &p); err != nil {
			s.writeErr(conn, "bad add_session_allow payload: "+err.Error())
			return
		}
		if p.Pattern == "" {
			s.writeErr(conn, "add_session_allow: pattern is required")
			return
		}
		s.modeMu.Lock()
		found := false
		for _, existing := range s.sessionAllow {
			if existing == p.Pattern {
				found = true
				break
			}
		}
		if !found {
			s.sessionAllow = append(s.sessionAllow, p.Pattern)
		}
		s.modeMu.Unlock()
		log.Printf("session allow added: %q", p.Pattern)
		s.writeOK(conn)

	case ipc.TypeGetSessionAllow:
		s.modeMu.RLock()
		allow := make([]string, len(s.sessionAllow))
		copy(allow, s.sessionAllow)
		s.modeMu.RUnlock()
		s.writeJSON(conn, struct {
			Patterns []string `json:"patterns"`
		}{Patterns: allow})

	case ipc.TypeStatus:
		s.modeMu.RLock()
		mode := s.mode
		sessionAllow := make([]string, len(s.sessionAllow))
		copy(sessionAllow, s.sessionAllow)
		s.modeMu.RUnlock()
		s.writeJSON(conn, ipc.StatusResp{
			Running:       true,
			Mode:          mode,
			Shell:         s.shell,
			BufferedLines: s.buf.Len(),
			SessionAllow:  sessionAllow,
		})

	case ipc.TypeGetContext:
		var p ipc.GetContextPayload
		if msg.Payload != nil {
			json.Unmarshal(msg.Payload, &p)
		}

		var lines []string
		if p.DeltaOnly {
			lines = s.buf.Delta()
		} else {
			n := p.NLines
			if n <= 0 {
				n = 50
			}
			lines = s.buf.Last(n)
		}

		s.writeJSON(conn, ipc.ContextResp{
			Lines:   lines,
			Summary: s.gen.Get(),
		})

	case ipc.TypeClearBuffers:
		s.buf.Clear()
		log.Printf("session buffer cleared")
		s.writeOK(conn)

	case ipc.TypeResetSeen:
		s.buf.ResetSeen()
		log.Printf("seen cursor reset")
		s.writeOK(conn)

	case ipc.TypeExec:
		var p ipc.ExecPayload
		if err := json.Unmarshal(msg.Payload, &p); err != nil {
			s.writeErr(conn, "bad exec payload: "+err.Error())
			return
		}
		if p.Command == "" {
			s.writeErr(conn, "exec: command is required")
			return
		}
		if s.execFn == nil {
			s.writeErr(conn, "exec: no shell attached")
			return
		}
		if err := s.execFn(p.Command); err != nil {
			s.writeErr(conn, "exec failed: "+err.Error())
			return
		}
		log.Printf("exec: %s", p.Command)
		s.writeOK(conn)

	case ipc.TypeStop:
		s.writeOK(conn)
		select {
		case <-s.stopCh:
		default:
			close(s.stopCh)
		}

	default:
		s.writeErr(conn, "unknown message type: "+string(msg.Type))
	}
}

func (s *server) writeOK(conn net.Conn)            { s.writeJSON(conn, ipc.OKResp{OK: true}) }
func (s *server) writeErr(conn net.Conn, m string) {
	log.Printf("error: %s", m)
	s.writeJSON(conn, ipc.ErrResp{Error: m})
}
func (s *server) writeJSON(conn net.Conn, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		log.Printf("marshal error: %v", err)
		return
	}
	conn.Write(append(b, '\n'))
}

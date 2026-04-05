package daemon

import (
	"bufio"
	"encoding/json"
	"log"
	"net"
	"sync"
	"tether/internal/ipc"
	"tether/internal/summary"
	"tether/internal/watcher"
)

type server struct {
	sockPath    string
	w           *watcher.Watcher
	gen         *summary.Generator
	tmuxSocket  string
	tmuxSession string
	listener    net.Listener
	stopCh      chan struct{}

	modeMu       sync.RWMutex
	mode         ipc.Mode // current Watch/Assist/Act mode
	sessionAllow []string // in-memory allow list, resets on daemon restart
}

func newServer(sockPath string, w *watcher.Watcher, gen *summary.Generator, tmuxSocket, tmuxSession string) *server {
	return &server{
		sockPath:    sockPath,
		w:           w,
		gen:         gen,
		tmuxSocket:  tmuxSocket,
		tmuxSession: tmuxSession,
		stopCh:      make(chan struct{}),
		mode:        ipc.ModeWatch, // safe default
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

	case ipc.TypeWatch:
		var p ipc.WatchPayload
		if err := json.Unmarshal(msg.Payload, &p); err != nil {
			s.writeErr(conn, "bad watch payload: "+err.Error())
			return
		}
		s.w.Watch(p.PaneID)
		log.Printf("watching pane %s", p.PaneID)
		s.writeOK(conn)

	case ipc.TypeUnwatch:
		var p ipc.WatchPayload
		if err := json.Unmarshal(msg.Payload, &p); err != nil {
			s.writeErr(conn, "bad unwatch payload: "+err.Error())
			return
		}
		s.w.Unwatch(p.PaneID)
		log.Printf("unwatching pane %s", p.PaneID)
		s.writeOK(conn)

	case ipc.TypeSetMode:
		var p ipc.SetModePayload
		if err := json.Unmarshal(msg.Payload, &p); err != nil {
			s.writeErr(conn, "bad set_mode payload: "+err.Error())
			return
		}
		if p.Mode != ipc.ModeWatch && p.Mode != ipc.ModeAssist && p.Mode != ipc.ModeAct {
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
		// Avoid duplicates.
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
		panes := s.w.WatchedPanes()
		sizes := make(map[string]int, len(panes))
		for _, id := range panes {
			sizes[id] = s.w.BufferLen(id)
		}
		s.modeMu.RLock()
		mode := s.mode
		sessionAllow := make([]string, len(s.sessionAllow))
		copy(sessionAllow, s.sessionAllow)
		s.modeMu.RUnlock()
		s.writeJSON(conn, ipc.StatusResp{
			Running:      true,
			TmuxSocket:   s.tmuxSocket,
			TmuxSession:  s.tmuxSession,
			WatchedPanes: panes,
			BufferSizes:  sizes,
			Mode:         mode,
			SessionAllow: sessionAllow,
		})

	case ipc.TypeGetContext:
		var p ipc.GetContextPayload
		if msg.Payload != nil {
			json.Unmarshal(msg.Payload, &p)
		}

		panes := s.w.WatchedPanes()
		resp := ipc.ContextResp{
			Panes:   make([]ipc.PaneContext, 0, len(panes)),
			Summary: s.gen.Get(),
		}

		if p.DeltaOnly {
			// Return only lines added since the last get_context call.
			for _, id := range panes {
				delta := s.w.Delta(id)
				if len(delta) > 0 {
					resp.Panes = append(resp.Panes, ipc.PaneContext{PaneID: id, Lines: delta})
				}
			}
		} else {
			n := p.NLines
			if n <= 0 {
				n = 50
			}
			for _, id := range panes {
				resp.Panes = append(resp.Panes, ipc.PaneContext{
					PaneID: id,
					Lines:  s.w.Last(id, n),
				})
			}
		}
		s.writeJSON(conn, resp)

	case ipc.TypeClearBuffers:
		s.w.ClearAll()
		log.Printf("ring buffers cleared")
		s.writeOK(conn)

	case ipc.TypeResetSeen:
		s.w.ResetSeen()
		log.Printf("seen cursors reset")
		s.writeOK(conn)

	case ipc.TypeExecInPane:
		var p ipc.ExecInPanePayload
		if err := json.Unmarshal(msg.Payload, &p); err != nil {
			s.writeErr(conn, "bad exec_in_pane payload: "+err.Error())
			return
		}
		if p.PaneID == "" || p.Command == "" {
			s.writeErr(conn, "exec_in_pane: pane_id and command are required")
			return
		}
		if err := s.w.SendKeys(p.PaneID, p.Command); err != nil {
			s.writeErr(conn, "send-keys failed: "+err.Error())
			return
		}
		log.Printf("exec in pane %s: %s", p.PaneID, p.Command)
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

func (s *server) writeOK(conn net.Conn)          { s.writeJSON(conn, ipc.OKResp{OK: true}) }
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

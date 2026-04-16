// Package pty implements a PTY proxy that sits between the user's terminal
// and their shell. It captures all output into a session buffer (for context),
// handles terminal resize, and triggers the inline chat overlay via SIGUSR1.
package pty

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"github.com/AllDayJon/Tether/internal/session"

	"github.com/creack/pty"
	"golang.org/x/term"
)

type Proxy struct {
	shell     string
	ptmx      *os.File
	cmd       *exec.Cmd
	buf       *session.Buffer
	overlayFn func(input io.Reader)

	overlayWriter *os.File
	overlayMu     sync.Mutex
	overlayActive bool

	pausedMu sync.Mutex
	paused   bool

	// doneCh is closed by Wait() to unblock routeStdin when the shell exits.
	doneCh chan struct{}

	wg sync.WaitGroup
}

func New(shell string, buf *session.Buffer, overlayFn func(input io.Reader)) *Proxy {
	return &Proxy{
		shell:     shell,
		buf:       buf,
		overlayFn: overlayFn,
		doneCh:    make(chan struct{}),
	}
}

func (p *Proxy) Start() error {
	pid := os.Getpid()
	cmd := exec.Command(p.shell)
	cmd.Env = append(os.Environ(),
		"TETHER=1",
		fmt.Sprintf("TETHER_PID=%d", pid),
	)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return err
	}
	p.ptmx = ptmx
	p.cmd = cmd

	if sz, err := pty.GetsizeFull(os.Stdin); err == nil {
		pty.Setsize(ptmx, sz)
	}

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		ptmx.Close()
		return err
	}

	usr1Ch := make(chan os.Signal, 1)
	winchCh := make(chan os.Signal, 1)
	signal.Notify(usr1Ch, syscall.SIGUSR1)
	signal.Notify(winchCh, syscall.SIGWINCH)

	p.wg.Add(3)

	// Goroutine 1: signal handler.
	go func() {
		defer p.wg.Done()
		defer func() {
			signal.Stop(usr1Ch)
			signal.Stop(winchCh)
		}()
		for {
			select {
			case _, ok := <-winchCh:
				if !ok {
					return
				}
				if sz, err := pty.GetsizeFull(os.Stdin); err == nil {
					pty.Setsize(ptmx, sz)
				}
			case _, ok := <-usr1Ch:
				if !ok {
					return
				}
				p.runOverlay()
			}
		}
	}()

	// Goroutine 2: PTY master → stdout + session buffer.
	go func() {
		defer p.wg.Done()
		p.copyPTYToStdout()
	}()

	// Goroutine 3: stdin router.
	// Runs a nested goroutine for the blocking os.Stdin.Read so the outer loop
	// can also select on doneCh — this is what fixes the exit hang.
	go func() {
		defer p.wg.Done()
		defer func() {
			// Stop signal delivery before closing the channels to prevent a
			// panic if a signal arrives after close but before the handler
			// goroutine's signal.Stop runs.
			signal.Stop(winchCh)
			signal.Stop(usr1Ch)
			close(winchCh)
			close(usr1Ch)
			term.Restore(int(os.Stdin.Fd()), oldState)
		}()
		p.routeStdin()
	}()

	return nil
}

// Wait blocks until the shell process exits, then signals all goroutines to stop.
func (p *Proxy) Wait() error {
	err := p.cmd.Wait()
	p.ptmx.Close()
	close(p.doneCh) // unblocks routeStdin's select, allowing clean shutdown
	p.wg.Wait()
	return err
}

func (p *Proxy) WriteToShell(data []byte) error {
	_, err := p.ptmx.Write(data)
	return err
}

// routeStdin reads from os.Stdin in a background goroutine and dispatches bytes
// to either the PTY or the overlay pipe. The select on doneCh allows this
// function to return promptly when the shell exits, instead of hanging forever
// on a blocking os.Stdin.Read.
func (p *Proxy) routeStdin() {
	type readResult struct {
		data []byte
		err  error
	}
	readCh := make(chan readResult, 4)

	go func() {
		buf := make([]byte, 256)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				data := make([]byte, n)
				copy(data, buf[:n])
				select {
				case readCh <- readResult{data: data}:
				case <-p.doneCh:
					return
				}
			}
			if err != nil {
				select {
				case readCh <- readResult{err: err}:
				case <-p.doneCh:
				}
				return
			}
		}
	}()

	for {
		select {
		case r := <-readCh:
			if r.err != nil {
				return
			}
			p.overlayMu.Lock()
			active := p.overlayActive
			w := p.overlayWriter
			p.overlayMu.Unlock()

			if active && w != nil {
				w.Write(r.data)
			} else {
				p.ptmx.Write(r.data)
			}
		case <-p.doneCh:
			return
		}
	}
}

func (p *Proxy) copyPTYToStdout() {
	buf := make([]byte, 4096)
	parser := newOSCParser(p.buf)

	for {
		n, err := p.ptmx.Read(buf)
		if n > 0 {
			parser.Write(buf[:n])

			p.pausedMu.Lock()
			paused := p.paused
			p.pausedMu.Unlock()

			if !paused {
				os.Stdout.Write(buf[:n])
			}
		}
		if err != nil {
			return
		}
	}
}

func (p *Proxy) runOverlay() {
	if p.overlayFn == nil {
		return
	}

	r, w, err := os.Pipe()
	if err != nil {
		return
	}

	p.pausedMu.Lock()
	p.paused = true
	p.pausedMu.Unlock()

	p.overlayMu.Lock()
	p.overlayActive = true
	p.overlayWriter = w
	p.overlayMu.Unlock()

	p.overlayFn(r)

	p.overlayMu.Lock()
	p.overlayActive = false
	p.overlayWriter = nil
	p.overlayMu.Unlock()

	w.Close()
	r.Close()

	p.pausedMu.Lock()
	p.paused = false
	p.pausedMu.Unlock()
}

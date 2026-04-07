package daemon

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"tether/internal/ipc"
	"tether/internal/session"
	"tether/internal/summary"
)

// Run starts the IPC server using the provided session buffer and exec function.
// execFn is called when a TypeExec message is received — it writes the command
// to the PTY shell. Run blocks until SIGTERM/SIGINT or a stop IPC message.
func Run(buf *session.Buffer, shell string, execFn func(cmd string) error) error {
	if err := ipc.EnsureDir(); err != nil {
		return err
	}

	// Redirect all daemon log output to the log file so it doesn't pollute
	// the terminal. The terminal is in raw mode during tether shell, so any
	// output without \r\n causes misaligned text.
	logPath, err := ipc.LogPath()
	if err != nil {
		return err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("cannot open log file: %w", err)
	}
	// logFile intentionally not closed — daemon lives for the process lifetime.
	log.SetOutput(logFile)
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[tether] ")
	log.Printf("starting — shell=%s", shell)

	summaryPath, err := summary.DefaultPath()
	if err != nil {
		log.Printf("warning: cannot determine summary path: %v", err)
	}
	gen := summary.New(buf, summary.DefaultInterval, summaryPath)
	gen.Start()
	defer gen.Stop()

	if err := ipc.EnsureSessionsDir(); err != nil {
		return err
	}
	sockPath, err := ipc.SessionSocketPath(os.Getpid())
	if err != nil {
		return err
	}
	os.Remove(sockPath) // remove stale socket from a previous run of this PID

	srv := newServer(sockPath, buf, gen, shell, execFn)
	if err := srv.start(); err != nil {
		return err
	}
	defer srv.stop()

	pidPath, err := ipc.PIDPath()
	if err != nil {
		return err
	}
	if err := writePID(pidPath); err != nil {
		log.Printf("warning: could not write PID file: %v", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	select {
	case sig := <-sigCh:
		log.Printf("received signal %s, shutting down", sig)
	case <-srv.stopCh:
		log.Printf("stop requested via IPC")
	}

	os.Remove(pidPath)
	log.Printf("tether exiting")
	return nil
}

func writePID(path string) error {
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644)
}

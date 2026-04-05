package daemon

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"tether/internal/ipc"
	"tether/internal/summary"
	"tether/internal/watcher"
)

// Run is the daemon entry point.
func Run(tmuxSocket, tmuxSession string) error {
	if err := ipc.EnsureDir(); err != nil {
		return err
	}

	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[tether-daemon] ")
	log.Printf("starting — socket=%s session=%s", tmuxSocket, tmuxSession)

	w := watcher.New(tmuxSocket, tmuxSession)
	w.Start()
	defer w.Stop()

	// Start the rolling summary generator.
	summaryPath, err := summary.DefaultPath()
	if err != nil {
		log.Printf("warning: cannot determine summary path: %v", err)
	}
	gen := summary.New(w, summary.DefaultInterval, summaryPath)
	gen.Start()
	defer gen.Stop()

	sockPath, err := ipc.SocketPath()
	if err != nil {
		return err
	}
	os.Remove(sockPath) // remove stale socket

	srv := newServer(sockPath, w, gen, tmuxSocket, tmuxSession)
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
	log.Printf("daemon exiting")
	return nil
}

func writePID(path string) error {
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644)
}

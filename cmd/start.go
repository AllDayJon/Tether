package cmd

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"tether/internal/config"
	"tether/internal/ipc"
	"time"

	"github.com/spf13/cobra"
)

var startNoWatch bool

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the tether daemon",
	Long: `Start the tether daemon. Must be run from inside a tmux session.

Automatically begins watching the current pane so you don't need to run
'tether watch' separately. Use --no-watch to skip this.`,
	RunE: runStart,
}

func init() {
	startCmd.Flags().BoolVar(&startNoWatch, "no-watch", false, "don't auto-watch the current pane")
}

func runStart(cmd *cobra.Command, args []string) error {
	// Check if already running — show status instead of just erroring.
	if isDaemonRunning() {
		showRunningStatus()
		return nil
	}

	cfg, _ := config.Load()
	autoWatch := cfg.AutoWatch && !startNoWatch
	if err := startDaemon(true, autoWatch); err != nil {
		return err
	}
	return nil
}

// startDaemon launches the daemon process. If verbose, prints startup lines.
// If autoWatch, watches $TMUX_PANE after the daemon is ready.
func startDaemon(verbose, autoWatch bool) error {
	socketPath, session, err := getTmuxInfo()
	if err != nil {
		return err
	}

	if err := ipc.EnsureDir(); err != nil {
		return fmt.Errorf("cannot create ~/.tether: %w", err)
	}

	logPath, err := ipc.LogPath()
	if err != nil {
		return err
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("cannot open log file: %w", err)
	}
	defer logFile.Close()

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine executable path: %w", err)
	}

	daemonCmd := exec.Command(exe, "_daemon", "--socket", socketPath, "--session", session)
	daemonCmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	daemonCmd.Stdout = logFile
	daemonCmd.Stderr = logFile
	daemonCmd.Stdin = nil

	if err := daemonCmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Wait briefly for the socket to appear.
	sockPath, _ := ipc.SocketPath()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sockPath); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if _, err := os.Stat(sockPath); err != nil {
		return fmt.Errorf("daemon started (PID %d) but socket not ready — check %s", daemonCmd.Process.Pid, logPath)
	}

	if verbose {
		fmt.Printf("tether v%s started (PID %d) — socket=%s session=%s\n", Version, daemonCmd.Process.Pid, socketPath, session)
		fmt.Printf("log: %s\n", logPath)
	}

	if autoWatch {
		if paneID := os.Getenv("TMUX_PANE"); paneID != "" {
			if conn, err := ipc.Dial(); err == nil {
				ipc.SendMsg(conn, ipc.TypeWatch, ipc.WatchPayload{PaneID: paneID})
				var resp ipc.OKResp
				ipc.Recv(conn, &resp)
				conn.Close()
				if verbose {
					fmt.Printf("watching current pane %s (use `tether watch` to add more)\n", paneID)
				}
			}
		}
	}

	return nil
}

// showRunningStatus prints daemon info when it's already running.
func showRunningStatus() {
	conn, err := ipc.Dial()
	if err != nil {
		fmt.Println("tether daemon is already running")
		return
	}
	defer conn.Close()

	if err := ipc.SendMsg(conn, ipc.TypeStatus, nil); err != nil {
		fmt.Println("tether daemon is already running")
		return
	}
	var resp ipc.StatusResp
	if err := ipc.Recv(conn, &resp); err != nil {
		fmt.Println("tether daemon is already running")
		return
	}

	// Read PID from file.
	pidPath, _ := ipc.PIDPath()
	pidBytes, _ := os.ReadFile(pidPath)
	pid := strings.TrimSpace(string(pidBytes))

	// Use socket mtime as a proxy for uptime.
	sockPath, _ := ipc.SocketPath()
	uptime := ""
	if info, err := os.Stat(sockPath); err == nil {
		dur := time.Since(info.ModTime())
		uptime = formatDuration(dur)
	}

	fmt.Printf("tether v%s already running", Version)
	if pid != "" {
		fmt.Printf(" (PID %s)", pid)
	}
	if uptime != "" {
		fmt.Printf(" — up %s", uptime)
	}
	fmt.Println()

	if len(resp.WatchedPanes) == 0 {
		fmt.Println("watching: no panes")
	} else {
		total := 0
		for _, n := range resp.BufferSizes {
			total += n
		}
		fmt.Printf("watching: %s  (%d lines buffered)\n", strings.Join(resp.WatchedPanes, ", "), total)
	}
}

func formatDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	}
}

// getTmuxInfo parses $TMUX and returns the socket path and session name.
func getTmuxInfo() (socketPath, session string, err error) {
	tmuxEnv := os.Getenv("TMUX")
	if tmuxEnv == "" {
		return "", "", fmt.Errorf("not inside a tmux session ($TMUX is not set)")
	}

	// $TMUX format: /tmp/tmux-UID/default,SERVER_PID,SESSION_ID
	parts := strings.SplitN(tmuxEnv, ",", 3)
	if len(parts) < 1 || parts[0] == "" {
		return "", "", fmt.Errorf("unexpected $TMUX format: %q", tmuxEnv)
	}
	socketPath = parts[0]

	// Get the session name.
	out, err := exec.Command("tmux", "-S", socketPath, "display-message", "-p", "#S").Output()
	if err != nil {
		return "", "", fmt.Errorf("cannot get tmux session name: %w", err)
	}
	session = strings.TrimSpace(string(out))
	if session == "" {
		return "", "", fmt.Errorf("tmux returned empty session name")
	}
	return socketPath, session, nil
}

// isDaemonRunning returns true if the daemon socket is listening.
func isDaemonRunning() bool {
	sockPath, err := ipc.SocketPath()
	if err != nil {
		return false
	}
	conn, err := net.DialTimeout("unix", sockPath, 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

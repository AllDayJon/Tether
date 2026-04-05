package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"tether/internal/ipc"
)

// Config holds user-configurable settings. All fields have sensible defaults
// so the file is optional — tether works without it.
type Config struct {
	// Chat split width as a percentage of terminal width (default 40).
	ChatSplitPercent int `json:"chat_split_percent"`

	// Claude model to use for ask/chat (empty string = claude CLI default).
	AskModel string `json:"ask_model"`

	// Lines to fetch from the daemon buffer per pane before relevance filtering.
	AskLines int `json:"ask_lines"`

	// Auto-watch the current pane when the daemon starts (default true).
	AutoWatch bool `json:"auto_watch"`

	// tmux key for opening the chat split (default "g", used as prefix+g).
	// Set to "" to disable the binding.
	ChatKey string `json:"chat_key"`

	// ── Mode / safety ────────────────────────────────────────────────────────

	// Commands that may auto-execute in Act mode (prefix-matched).
	// Hard protect/deny rules always take precedence over this list.
	Allow []string `json:"allow"`

	// Commands that always require human approval even in Act mode.
	Protect []string `json:"protect"`

	// Commands that tether will never run, regardless of mode.
	Deny []string `json:"deny"`
}

// Defaults returns a Config populated with the built-in defaults.
func Defaults() Config {
	return Config{
		ChatSplitPercent: 40,
		AskModel:         "",
		AskLines:         200,
		AutoWatch:        true,
		ChatKey:          "g",
		Allow: []string{
			"ls", "cat", "head", "tail", "grep", "ps", "df", "free",
			"top", "htop", "systemctl status", "journalctl", "ping",
			"curl -s", "curl -I", "which", "whoami", "id",
			"uname", "uptime", "hostname", "date", "echo", "pwd",
			"find", "du", "wc", "sort", "uniq", "diff", "stat",
			"netstat", "ss", "lsof", "ip addr", "ip route",
		},
		Protect: []string{
			"rm", "mv", "cp", "chmod", "chown", "chgrp",
			"dd", "mkfs", "fdisk", "parted",
			"shutdown", "reboot", "poweroff", "halt",
			"systemctl stop", "systemctl restart", "systemctl disable",
			"apt", "apt-get", "yum", "dnf", "pacman",
			"pip install", "npm install",
			"git push", "git reset",
			"docker rm", "docker stop", "docker rmi",
			"kubectl delete", "kubectl apply",
			"truncate", "shred",
		},
		Deny: []string{},
	}
}

// Path returns ~/.tether/config.json.
func Path() (string, error) {
	dir, err := ipc.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads ~/.tether/config.json and merges it over the defaults.
// If the file does not exist, defaults are returned with no error.
func Load() (Config, error) {
	cfg := Defaults()

	p, err := Path()
	if err != nil {
		return cfg, err
	}

	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return cfg, nil // no config file — use defaults
	}
	if err != nil {
		return cfg, fmt.Errorf("reading config: %w", err)
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing config: %w", err)
	}
	return cfg, nil
}

// Save writes cfg to ~/.tether/config.json.
func Save(cfg Config) error {
	p, err := Path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0600)
}

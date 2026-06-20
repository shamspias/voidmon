package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config is the resolved runtime configuration. Precedence is:
//
//	defaults  <  config file  <  command-line flags
//
// A missing config file is not an error — zero config yields today's look.
type Config struct {
	Theme        string
	Refresh      time.Duration
	ProcessCount int
	NoColor      bool
	Icons        bool

	Alerts    bool
	CPUAlert  float64
	MemAlert  float64
	TempAlert float64
	DiskAlert float64
}

func DefaultConfig() Config {
	return Config{
		Theme:        "matrix",
		Refresh:      2 * time.Second,
		ProcessCount: 25,
		NoColor:      os.Getenv("NO_COLOR") != "",
		Icons:        true,
		Alerts:       false,
		CPUAlert:     90,
		MemAlert:     90,
		TempAlert:    85,
		DiskAlert:    90,
	}
}

// ConfigPath returns the location voidmon reads its config from
// (e.g. ~/.config/voidmon/config on Linux, ~/Library/Application Support/... on
// macOS, %AppData%\voidmon\config on Windows).
func ConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "voidmon", "config")
}

// LoadConfig reads the config file if present, layering it over the defaults.
func LoadConfig() Config {
	cfg := DefaultConfig()

	path := ConfigPath()
	if path == "" {
		return cfg
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.ToLower(key))
		val = strings.TrimSpace(val)

		switch key {
		case "theme":
			cfg.Theme = strings.ToLower(val)
		case "refresh":
			if d, err := time.ParseDuration(val); err == nil {
				cfg.Refresh = d
			}
		case "process_count", "processes":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.ProcessCount = n
			}
		case "no_color":
			cfg.NoColor = parseBool(val)
		case "icons":
			cfg.Icons = parseBool(val)
		case "alerts":
			cfg.Alerts = parseBool(val)
		case "cpu_alert":
			cfg.CPUAlert = parseFloat(val, cfg.CPUAlert)
		case "mem_alert":
			cfg.MemAlert = parseFloat(val, cfg.MemAlert)
		case "temp_alert":
			cfg.TempAlert = parseFloat(val, cfg.TempAlert)
		case "disk_alert":
			cfg.DiskAlert = parseFloat(val, cfg.DiskAlert)
		}
	}

	return cfg
}

func parseBool(s string) bool {
	switch strings.ToLower(s) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func parseFloat(s string, def float64) float64 {
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v
	}
	return def
}

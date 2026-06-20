package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const appName = "voidmon"

// version is a var (not const) so it can be overwritten at build time via
// -ldflags "-X main.version=...".
var version = "dev"

// GitHubRelease parses the latest release from the GitHub API.
type GitHubRelease struct {
	TagName string `json:"tag_name"`
}

func main() {
	cfg := LoadConfig()

	refreshRate := flag.Duration("r", cfg.Refresh, "refresh rate (e.g., 1s, 2s, 500ms)")
	showVersion := flag.Bool("v", false, "show version")
	updateApp := flag.Bool("u", false, "check for updates and upgrade")
	jsonOut := flag.Bool("json", false, "print metrics as JSON and exit (headless, no TUI)")
	watch := flag.Bool("watch", false, "with -json: stream one JSON object per refresh (NDJSON)")
	theme := flag.String("theme", cfg.Theme, "color theme: "+strings.Join(themeNames(), ", "))
	noColor := flag.Bool("no-color", cfg.NoColor, "disable colors (also honors NO_COLOR env)")
	icons := flag.Bool("icons", cfg.Icons, "use emoji icons in panel titles (use -icons=false to disable)")
	flag.Parse()

	// Flags override config / defaults.
	cfg.Refresh = *refreshRate
	if cfg.Refresh < 500*time.Millisecond {
		cfg.Refresh = 500 * time.Millisecond
	}
	cfg.Theme = *theme
	cfg.NoColor = *noColor
	cfg.Icons = *icons

	// NO_COLOR convention: its mere presence disables color, overriding any
	// config-file `no_color=false` or the flag's inherited default.
	if os.Getenv("NO_COLOR") != "" {
		cfg.NoColor = true
	}

	if *showVersion {
		printBanner()
		fmt.Printf("  %s %s\n", appName, displayVersion())
		fmt.Printf("  %s/%s · Go %s\n", runtime.GOOS, runtime.GOARCH, runtime.Version())
		fmt.Println("  Terminal system monitor with hacker aesthetics")
		fmt.Println("  https://github.com/shamspias/voidmon")
		os.Exit(0)
	}

	if *updateApp {
		if err := performUpdate(); err != nil {
			fmt.Fprintf(os.Stderr, "\033[31m[voidmon] update failed: %v\033[0m\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Headless: a one-shot JSON snapshot or an NDJSON stream. Reuses the exact
	// same Collect() the TUI uses — voidmon doubles as a scriptable exporter.
	if *jsonOut {
		runHeadless(cfg, *watch)
		return
	}

	cfg.Theme = applyTheme(cfg.Theme, cfg.NoColor)

	ui := NewUI(cfg)
	if err := ui.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "\033[31m[voidmon] fatal: %v\033[0m\n", err)
		os.Exit(1)
	}
}

// runHeadless prints metrics without a TUI. It primes the collector once (so
// IO/network/CPU deltas are real), waits one interval, then emits.
func runHeadless(cfg Config, watch bool) {
	c := NewCollector()
	c.Collect() // prime deltas

	warmup := cfg.Refresh
	if warmup > time.Second {
		warmup = time.Second
	}
	time.Sleep(warmup)

	enc := json.NewEncoder(os.Stdout)

	if !watch {
		m := c.Collect()
		m.Processes = capProcesses(m.Processes, cfg.ProcessCount)
		enc.SetIndent("", "  ")
		_ = enc.Encode(m)
		return
	}

	// NDJSON: one compact object per line, forever (Ctrl-C to stop).
	for {
		m := c.Collect()
		m.Processes = capProcesses(m.Processes, cfg.ProcessCount)
		_ = enc.Encode(m)
		time.Sleep(cfg.Refresh)
	}
}

func capProcesses(p []ProcessInfo, n int) []ProcessInfo {
	if n > 0 && len(p) > n {
		return p[:n]
	}
	return p
}

func displayVersion() string {
	v := version
	if v == "" {
		v = "dev"
	}
	if v != "dev" && !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

func printBanner() {
	banner := `
  ┌──────────────────────────────────────────┐
  │  ░▒▓█  V O I D M O N  █▓▒░               │
  │  ─────────────────────────────           │
  │  System Monitor · Terminal Edition       │
  └──────────────────────────────────────────┘
`
	fmt.Print(banner)
}

func performUpdate() error {
	fmt.Printf("  Current version: %s\n", displayVersion())
	fmt.Println("  → Checking latest release on GitHub...")

	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/shamspias/voidmon/releases/latest")
	if err != nil {
		return fmt.Errorf("network error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API returned status: %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return fmt.Errorf("failed to parse GitHub response: %v", err)
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	// Normalize the build-injected version (e.g. "v1.0.1-3-gabc-dirty") down to
	// its base tag so the comparison can actually match.
	cur := strings.SplitN(strings.TrimPrefix(version, "v"), "-", 2)[0]
	if cur != "" && cur == latest {
		fmt.Println("  ✓ You are already using the latest version!")
		return nil
	}

	fmt.Printf("  → New version available: %s. Upgrading...\n\n", latest)

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("powershell", "-NoProfile", "-Command",
			"irm https://raw.githubusercontent.com/shamspias/voidmon/main/install.ps1 | iex")
	} else {
		cmd = exec.Command("bash", "-c",
			"curl -fsSL https://raw.githubusercontent.com/shamspias/voidmon/main/install.sh | bash")
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

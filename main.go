package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

const appName = "voidmon"

// version is now a var instead of const so it can be overwritten at build time
var version = "dev"

// GitHubRelease is used to parse the latest release from the GitHub API
type GitHubRelease struct {
	TagName string `json:"tag_name"`
}

func main() {
	// Flags
	refreshRate := flag.Duration("r", 2*time.Second, "refresh rate (e.g., 1s, 2s, 5s)")
	showVersion := flag.Bool("v", false, "show version")
	updateApp := flag.Bool("u", false, "check for updates and upgrade to the latest version")
	flag.Parse()

	if *showVersion {
		printBanner()
		fmt.Printf("  %s v%s\n", appName, version)
		fmt.Println("  Terminal system monitor with hacker aesthetics")
		fmt.Println("  https://github.com/shamspias/voidmon")
		os.Exit(0)
	}

	// Handle self-updating
	if *updateApp {
		if err := performUpdate(); err != nil {
			fmt.Fprintf(os.Stderr, "\033[31m[voidmon] update failed: %v\033[0m\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Minimum refresh rate
	if *refreshRate < 500*time.Millisecond {
		*refreshRate = 500 * time.Millisecond
	}

	// Launch the UI
	ui := NewUI(*refreshRate)
	if err := ui.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "\033[31m[voidmon] fatal: %v\033[0m\n", err)
		os.Exit(1)
	}
}

func printBanner() {
	banner := `
  ┌──────────────────────────────────────────┐
  │  ░▒▓█  V O I D M O N  █▓▒░             │
  │  ─────────────────────────────           │
  │  System Monitor · Terminal Edition       │
  └──────────────────────────────────────────┘
`
	fmt.Print(banner)
}

func performUpdate() error {
	fmt.Printf("  Current version: %s\n", version)
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

	// Strip the "v" prefix from the tag (e.g., "v1.0.1" -> "1.0.1")
	latestVersion := strings.TrimPrefix(release.TagName, "v")
	if latestVersion == version {
		fmt.Println("  ✓ You are already using the latest version!")
		return nil
	}

	fmt.Printf("  → New version available: %s. Upgrading...\n\n", latestVersion)

	// Run the existing install script to perform the update
	cmd := exec.Command("bash", "-c", "curl -fsSL https://raw.githubusercontent.com/shamspias/voidmon/main/install.sh | bash")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

const (
	version = "1.0.0"
	appName = "voidmon"
)

func main() {
	// Flags
	refreshRate := flag.Duration("r", 2*time.Second, "refresh rate (e.g., 1s, 2s, 5s)")
	showVersion := flag.Bool("v", false, "show version")
	flag.Parse()

	if *showVersion {
		printBanner()
		fmt.Printf("  %s v%s\n", appName, version)
		fmt.Println("  Terminal system monitor with hacker aesthetics")
		fmt.Println("  https://github.com/shamspias/voidmon")
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

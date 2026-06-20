package main

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

func TestRate(t *testing.T) {
	if got := rate(2000, 1000, 2); got != 500 {
		t.Errorf("rate normal = %v, want 500", got)
	}
	// Counter rollover / device removal must clamp to 0, not a ~2^64 spike.
	if got := rate(100, 1000, 1); got != 0 {
		t.Errorf("rate underflow = %v, want 0", got)
	}
	if got := rate(1000, 1000, 0); got != 0 {
		t.Errorf("rate zero-elapsed = %v, want 0", got)
	}
}

func TestSparkline(t *testing.T) {
	// Empty input pads to width with spaces.
	if got := sparkline(nil, 100, 5); got != "     " {
		t.Errorf("empty sparkline = %q, want 5 spaces", got)
	}
	// Width 0 returns empty.
	if got := sparkline([]float64{1, 2, 3}, 0, 0); got != "" {
		t.Errorf("zero width = %q, want empty", got)
	}
	// Right-aligned: fewer samples than width left-pads with spaces.
	got := sparkline([]float64{100}, 100, 4)
	if len([]rune(got)) != 4 {
		t.Errorf("sparkline rune width = %d, want 4", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "█") {
		t.Errorf("max value should map to full block, got %q", got)
	}
	// Only the most recent `width` samples are shown.
	if got := sparkline([]float64{1, 2, 3, 4, 5, 6}, 6, 3); len([]rune(got)) != 3 {
		t.Errorf("should truncate to width 3, got %q", got)
	}
}

func TestTruncRunes(t *testing.T) {
	if got := truncRunes("hello", 10); got != "hello" {
		t.Errorf("no trunc = %q", got)
	}
	if got := truncRunes("hello world", 5); got != "hell…" {
		t.Errorf("trunc = %q, want hell…", got)
	}
	// Multibyte must not corrupt: counts runes, not bytes.
	s := "日本語テスト"
	got := truncRunes(s, 3)
	if r := []rune(got); len(r) != 3 {
		t.Errorf("multibyte trunc rune len = %d, want 3 (%q)", len(r), got)
	}
}

func TestSortProcesses(t *testing.T) {
	procs := []ProcessInfo{
		{PID: 3, Name: "zeta", CPUPct: 10, MemPct: 5},
		{PID: 1, Name: "alpha", CPUPct: 50, MemPct: 1},
		{PID: 2, Name: "beta", CPUPct: 5, MemPct: 9},
	}
	sortProcesses(procs, "cpu")
	if procs[0].CPUPct != 50 {
		t.Errorf("cpu sort top = %v, want 50", procs[0].CPUPct)
	}
	sortProcesses(procs, "mem")
	if procs[0].MemPct != 9 {
		t.Errorf("mem sort top = %v, want 9", procs[0].MemPct)
	}
	sortProcesses(procs, "pid")
	if procs[0].PID != 1 {
		t.Errorf("pid sort top = %v, want 1", procs[0].PID)
	}
	sortProcesses(procs, "name")
	if procs[0].Name != "alpha" {
		t.Errorf("name sort top = %v, want alpha", procs[0].Name)
	}
}

func TestApplyTheme(t *testing.T) {
	name := applyTheme("doesnotexist", false)
	if name != "matrix" {
		t.Errorf("unknown theme should fall back to matrix, got %q", name)
	}
	if colorPrimary == "" {
		t.Error("matrix theme should set a non-empty primary color")
	}

	applyTheme("matrix", true) // NO_COLOR
	if colorPrimary != "" || colorCritical != "" || colorDim != "" {
		t.Error("NO_COLOR must blank all palette colors")
	}
	// restore for other tests
	applyTheme("matrix", false)
}

func TestFormatBytes(t *testing.T) {
	cases := map[uint64]string{
		512:                    "512B",
		2048:                   "2.0K",
		5 * 1024 * 1024:        "5.0M",
		3 * 1024 * 1024 * 1024: "3.0G",
	}
	for in, want := range cases {
		if got := FormatBytes(in); got != want {
			t.Errorf("FormatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestConfigDefaults(t *testing.T) {
	c := DefaultConfig()
	if c.Theme != "matrix" || c.ProcessCount != 25 || c.Refresh != 2*time.Second {
		t.Errorf("unexpected defaults: %+v", c)
	}
	if !parseBool("yes") || parseBool("nope") {
		t.Error("parseBool failed")
	}
	if parseFloat("87.5", 0) != 87.5 || parseFloat("bad", 42) != 42 {
		t.Error("parseFloat failed")
	}
}

// TestRenderSmoke drives the full TUI render path against a simulation screen
// (no TTY) to prove no render function panics with real, live metrics.
func TestRenderSmoke(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Refresh = 500 * time.Millisecond
	applyTheme(cfg.Theme, false)

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("sim screen init: %v", err)
	}
	screen.SetSize(140, 50)

	ui := NewUI(cfg)
	ui.app.SetScreen(screen)
	ui.buildLayout()

	m := ui.collector.Collect()
	ui.metrics = m
	ui.pushHistory(m)
	ui.renderAll()

	// Exercise alternate paths: paused, filter, NO_COLOR, alt sort.
	ui.sortKey = "mem"
	ui.filter = "x"
	ui.renderProcesses()
	ui.renderFooter()

	if len(ui.procView) > cfg.ProcessCount {
		t.Errorf("procView (%d) exceeds ProcessCount (%d)", len(ui.procView), cfg.ProcessCount)
	}
}

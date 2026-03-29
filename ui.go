package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ─────────────────────────────────────────────
// Color Scheme — Hacker Aesthetic
// ─────────────────────────────────────────────

const (
	colorPrimary   = "[#00ff41]" // Matrix green
	colorSecondary = "[#00d4ff]" // Cyan
	colorAccent    = "[#ff6ac1]" // Neon pink
	colorWarning   = "[#f5a623]" // Amber
	colorCritical  = "[#ff3333]" // Red
	colorDim       = "[#4a5568]" // Gray
	colorMuted     = "[#2d3748]" // Dark gray
	colorWhite     = "[#e2e8f0]" // Soft white
	colorReset     = "[-]"
	colorBold      = "[::b]"
	colorNoBold    = "[::-]"
)

// Bar characters
const (
	barFull  = "█"
	barEmpty = "░"
	barHalf  = "▓"
)

// ─────────────────────────────────────────────
// UI Application
// ─────────────────────────────────────────────

type UI struct {
	app         *tview.Application
	collector   *Collector
	metrics     SystemMetrics
	refreshRate time.Duration

	// Layout panels
	header     *tview.TextView
	cpuPanel   *tview.TextView
	memPanel   *tview.TextView
	diskPanel  *tview.TextView
	ioPanel    *tview.TextView
	gpuPanel   *tview.TextView
	powerPanel *tview.TextView
	procPanel  *tview.TextView
	footer     *tview.TextView
	netPanel   *tview.TextView
}

func NewUI(refreshRate time.Duration) *UI {
	return &UI{
		app:         tview.NewApplication(),
		collector:   NewCollector(),
		refreshRate: refreshRate,
	}
}

func (u *UI) Run() error {
	u.buildLayout()
	u.collectAndRender()
	u.startAutoRefresh()
	u.setupKeybindings()
	return u.app.Run()
}

// ─────────────────────────────────────────────
// Layout Construction
// ─────────────────────────────────────────────

func (u *UI) buildLayout() {
	// Create panels
	u.header = u.newPanel("")
	u.header.SetBorder(false)
	u.header.SetTextAlign(tview.AlignCenter)

	u.cpuPanel = u.newPanel(" ⚡ CPU ")
	u.memPanel = u.newPanel(" 🧠 MEMORY ")
	u.diskPanel = u.newPanel(" 💾 DISK ")
	u.ioPanel = u.newPanel(" ⇄  I/O ")
	u.gpuPanel = u.newPanel(" 🎮 GPU ")
	u.powerPanel = u.newPanel(" ⚡ POWER ")
	u.netPanel = u.newPanel(" 🌐 NETWORK ")
	u.procPanel = u.newPanel(" ▶ PROCESSES ")
	u.footer = u.newPanel("")
	u.footer.SetBorder(false)
	u.footer.SetTextAlign(tview.AlignCenter)

	// Grid layout
	grid := tview.NewGrid().
		SetRows(4, 0, 0, 0, 0, 1).
		SetColumns(0, 0).
		SetBorders(false)

	// Row 0: Header (spans 2 cols)
	grid.AddItem(u.header, 0, 0, 1, 2, 0, 0, false)

	// Row 1: CPU | Memory
	grid.AddItem(u.cpuPanel, 1, 0, 1, 1, 0, 0, false)
	grid.AddItem(u.memPanel, 1, 1, 1, 1, 0, 0, false)

	// Row 2: Disk | I/O + Network
	grid.AddItem(u.diskPanel, 2, 0, 1, 1, 0, 0, false)

	// I/O + Network combined right panel
	rightMid := tview.NewGrid().
		SetRows(0, 0).
		SetColumns(0).
		SetBorders(false)
	rightMid.AddItem(u.ioPanel, 0, 0, 1, 1, 0, 0, false)
	rightMid.AddItem(u.netPanel, 1, 0, 1, 1, 0, 0, false)
	grid.AddItem(rightMid, 2, 1, 1, 1, 0, 0, false)

	// Row 3: GPU | Power
	grid.AddItem(u.gpuPanel, 3, 0, 1, 1, 0, 0, false)
	grid.AddItem(u.powerPanel, 3, 1, 1, 1, 0, 0, false)

	// Row 4: Processes (spans 2 cols)
	grid.AddItem(u.procPanel, 4, 0, 1, 2, 0, 0, false)

	// Row 5: Footer
	grid.AddItem(u.footer, 5, 0, 1, 2, 0, 0, false)

	u.app.SetRoot(grid, true)
}

func (u *UI) newPanel(title string) *tview.TextView {
	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(true).
		SetWordWrap(false)

	tv.SetBorder(true).
		SetBorderColor(tcell.ColorDarkGreen).
		SetTitle(title).
		SetTitleColor(tcell.ColorGreen).
		SetTitleAlign(tview.AlignLeft).
		SetBackgroundColor(tcell.ColorBlack).
		SetBorderPadding(0, 0, 1, 1)

	return tv
}

// ─────────────────────────────────────────────
// Rendering
// ─────────────────────────────────────────────

func (u *UI) collectAndRender() {
	u.metrics = u.collector.Collect()
	u.renderHeader()
	u.renderCPU()
	u.renderMemory()
	u.renderDisk()
	u.renderIO()
	u.renderNetwork()
	u.renderGPU()
	u.renderPower()
	u.renderProcesses()
	u.renderFooter()
}

func (u *UI) startAutoRefresh() {
	go func() {
		ticker := time.NewTicker(u.refreshRate)
		defer ticker.Stop()
		for range ticker.C {
			u.app.QueueUpdateDraw(func() {
				u.collectAndRender()
			})
		}
	}()
}

func (u *UI) setupKeybindings() {
	u.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			u.app.Stop()
			return nil
		case tcell.KeyRune:
			switch event.Rune() {
			case 'q', 'Q':
				u.app.Stop()
				return nil
			}
		}
		return event
	})
}

// ─────────────────────────────────────────────
// Header
// ─────────────────────────────────────────────

func (u *UI) renderHeader() {
	m := u.metrics.Host

	banner := fmt.Sprintf(`%s%s░▒▓  V O I D M O N  ▓▒░%s  %s%s · %s · %s · up %s%s`,
		colorPrimary, colorBold, colorNoBold,
		colorDim, m.Hostname, m.Platform, m.Kernel, FormatUptime(m.Uptime),
		colorReset,
	)

	u.header.SetText(banner)
}

// ─────────────────────────────────────────────
// CPU Panel
// ─────────────────────────────────────────────

func (u *UI) renderCPU() {
	var b strings.Builder
	c := u.metrics.CPU

	// Model name (truncated)
	name := c.ModelName
	if len(name) > 40 {
		name = name[:37] + "..."
	}
	fmt.Fprintf(&b, " %s%s%s\n", colorDim, name, colorReset)

	// Per-core bars
	for i, pct := range c.PerCore {
		label := fmt.Sprintf("  Core %-2d", i)
		bar := renderBar(pct, 20)
		pctColor := percentColor(pct)
		fmt.Fprintf(&b, "%s%s%s %s %s%5.1f%%%s\n",
			colorSecondary, label, colorReset, bar, pctColor, pct, colorReset)
	}

	// Summary line
	fmt.Fprintf(&b, "\n  %sOverall:%s %s%.1f%%%s", colorDim, colorReset, percentColor(c.Overall), c.Overall, colorReset)
	if c.Temperature > 0 {
		fmt.Fprintf(&b, "  %sTemp:%s %s%.0f°C%s", colorDim, colorReset, tempColor(c.Temperature), c.Temperature, colorReset)
	}
	fmt.Fprintf(&b, "  %sLoad:%s %.2f %.2f %.2f",
		colorDim, colorReset, c.LoadAvg1, c.LoadAvg5, c.LoadAvg15)

	u.cpuPanel.SetText(b.String())
}

// ─────────────────────────────────────────────
// Memory Panel
// ─────────────────────────────────────────────

func (u *UI) renderMemory() {
	var b strings.Builder
	m := u.metrics.Memory

	// RAM bar
	fmt.Fprintf(&b, "\n  %s%sRAM%s    %s  %s%5.1f%%%s\n",
		colorSecondary, colorBold, colorReset,
		renderBar(m.UsedPercent, 24),
		percentColor(m.UsedPercent), m.UsedPercent, colorReset)
	fmt.Fprintf(&b, "          %sUsed: %s / %s%s\n",
		colorDim, FormatBytes(m.Used), FormatBytes(m.Total), colorReset)
	fmt.Fprintf(&b, "          %sAvail: %s  Cache: %s%s\n\n",
		colorDim, FormatBytes(m.Available), FormatBytes(m.Cached), colorReset)

	// Swap bar
	if m.SwapTotal > 0 {
		fmt.Fprintf(&b, "  %s%sSWAP%s   %s  %s%5.1f%%%s\n",
			colorAccent, colorBold, colorReset,
			renderBar(m.SwapPercent, 24),
			percentColor(m.SwapPercent), m.SwapPercent, colorReset)
		fmt.Fprintf(&b, "          %sUsed: %s / %s%s\n",
			colorDim, FormatBytes(m.SwapUsed), FormatBytes(m.SwapTotal), colorReset)
	} else {
		fmt.Fprintf(&b, "  %sSWAP%s   %sN/A%s\n", colorAccent, colorReset, colorDim, colorReset)
	}

	u.memPanel.SetText(b.String())
}

// ─────────────────────────────────────────────
// Disk Panel
// ─────────────────────────────────────────────

func (u *UI) renderDisk() {
	var b strings.Builder
	disks := u.metrics.Disks

	if len(disks) == 0 {
		fmt.Fprintf(&b, "\n  %sNo disks detected%s", colorDim, colorReset)
		u.diskPanel.SetText(b.String())
		return
	}

	for _, d := range disks {
		mount := d.MountPoint
		if len(mount) > 12 {
			mount = "..." + mount[len(mount)-9:]
		}

		fmt.Fprintf(&b, "  %s%-12s%s %s %s%5.1f%%%s\n",
			colorSecondary, mount, colorReset,
			renderBar(d.UsedPercent, 18),
			percentColor(d.UsedPercent), d.UsedPercent, colorReset)
		fmt.Fprintf(&b, "  %s             %s / %s  [%s]%s\n",
			colorDim, FormatBytes(d.Used), FormatBytes(d.Total), d.Fstype, colorReset)
	}

	u.diskPanel.SetText(b.String())
}

// ─────────────────────────────────────────────
// I/O Panel
// ─────────────────────────────────────────────

func (u *UI) renderIO() {
	var b strings.Builder
	io := u.metrics.IO

	fmt.Fprintf(&b, "\n  %s▲ READ %s  %s%s%s\n",
		colorPrimary, colorReset, colorWhite, FormatBytesSpeed(io.ReadSpeed), colorReset)
	fmt.Fprintf(&b, "  %s▼ WRITE%s  %s%s%s\n\n",
		colorAccent, colorReset, colorWhite, FormatBytesSpeed(io.WriteSpeed), colorReset)
	fmt.Fprintf(&b, "  %sIOPS  R: %-8.0f W: %.0f%s\n",
		colorDim, io.ReadIOPS, io.WriteIOPS, colorReset)
	fmt.Fprintf(&b, "  %sTotal R: %-8s W: %s%s",
		colorDim, FormatBytes(io.ReadBytes), FormatBytes(io.WriteBytes), colorReset)

	u.ioPanel.SetText(b.String())
}

// ─────────────────────────────────────────────
// Network Panel
// ─────────────────────────────────────────────

func (u *UI) renderNetwork() {
	var b strings.Builder
	n := u.metrics.Network

	fmt.Fprintf(&b, "  %s▲ SEND%s  %s%s%s    %s▼ RECV%s  %s%s%s",
		colorPrimary, colorReset, colorWhite, FormatBytesSpeed(n.SendSpeed), colorReset,
		colorAccent, colorReset, colorWhite, FormatBytesSpeed(n.RecvSpeed), colorReset)

	u.netPanel.SetText(b.String())
}

// ─────────────────────────────────────────────
// GPU Panel
// ─────────────────────────────────────────────

func (u *UI) renderGPU() {
	var b strings.Builder
	g := u.metrics.GPU

	if !g.Available {
		fmt.Fprintf(&b, "\n  %sNo GPU detected%s\n", colorDim, colorReset)
		fmt.Fprintf(&b, "  %sInstall nvidia-smi or check AMD sysfs%s", colorDim, colorReset)
		u.gpuPanel.SetText(b.String())
		return
	}

	// GPU Name
	name := g.Name
	if len(name) > 35 {
		name = name[:32] + "..."
	}
	fmt.Fprintf(&b, " %s%s%s", colorWhite, name, colorReset)
	if g.DriverVer != "" {
		fmt.Fprintf(&b, " %s[%s]%s", colorDim, g.DriverVer, colorReset)
	}
	fmt.Fprintf(&b, "\n")

	// Usage bar
	fmt.Fprintf(&b, "  %sUsage%s  %s %s%5.1f%%%s\n",
		colorSecondary, colorReset,
		renderBar(g.Utilization, 20),
		percentColor(g.Utilization), g.Utilization, colorReset)

	// VRAM bar
	if g.MemTotal > 0 {
		fmt.Fprintf(&b, "  %sVRAM %s  %s %s%5.1f%%%s\n",
			colorAccent, colorReset,
			renderBar(g.MemPercent, 20),
			percentColor(g.MemPercent), g.MemPercent, colorReset)
		fmt.Fprintf(&b, "         %s%s / %s%s\n",
			colorDim, FormatBytes(g.MemUsed), FormatBytes(g.MemTotal), colorReset)
	}

	// Temperature, Fan, Power
	var info []string
	if g.Temperature > 0 {
		info = append(info, fmt.Sprintf("%sTemp:%s %s%.0f°C%s",
			colorDim, colorReset, tempColor(g.Temperature), g.Temperature, colorReset))
	}
	if g.FanSpeed > 0 {
		info = append(info, fmt.Sprintf("%sFan:%s %.0f%%", colorDim, colorReset, g.FanSpeed))
	}
	if g.PowerDraw > 0 {
		info = append(info, fmt.Sprintf("%sPwr:%s %.0fW/%.0fW",
			colorDim, colorReset, g.PowerDraw, g.PowerLimit))
	}
	if len(info) > 0 {
		fmt.Fprintf(&b, "  %s", strings.Join(info, "  "))
	}

	u.gpuPanel.SetText(b.String())
}

// ─────────────────────────────────────────────
// Power Panel
// ─────────────────────────────────────────────

func (u *UI) renderPower() {
	var b strings.Builder
	p := u.metrics.Power

	if !p.Available {
		fmt.Fprintf(&b, "\n  %sNo power data available%s\n", colorDim, colorReset)
		fmt.Fprintf(&b, "  %sDesktop / VM detected%s", colorDim, colorReset)
		u.powerPanel.SetText(b.String())
		return
	}

	// AC or Battery icon
	icon := "🔌"
	if !p.OnAC {
		icon = "🔋"
	}

	fmt.Fprintf(&b, "\n  %s  %s%s%s\n\n", icon, colorWhite, p.Status, colorReset)

	if p.BatteryPct > 0 {
		fmt.Fprintf(&b, "  %sBattery%s  %s %s%5.1f%%%s\n",
			colorSecondary, colorReset,
			renderBar(p.BatteryPct, 20),
			batteryColor(p.BatteryPct), p.BatteryPct, colorReset)
	}

	if p.PowerRate > 0 {
		fmt.Fprintf(&b, "\n  %sDraw:%s %.1fW", colorDim, colorReset, p.PowerRate)
	}

	u.powerPanel.SetText(b.String())
}

// ─────────────────────────────────────────────
// Process Panel
// ─────────────────────────────────────────────

func (u *UI) renderProcesses() {
	var b strings.Builder
	procs := u.metrics.Processes

	// Header
	fmt.Fprintf(&b, " %s%s  %-7s  %-20s  %7s  %7s  %8s  %-10s  %-10s%s\n",
		colorSecondary, colorBold,
		"PID", "NAME", "CPU%", "MEM%", "RSS", "STATUS", "USER",
		colorReset)

	// Separator
	fmt.Fprintf(&b, " %s%s%s\n", colorMuted, strings.Repeat("─", 82), colorReset)

	for _, p := range procs {
		name := p.Name
		if len(name) > 20 {
			name = name[:17] + "..."
		}
		user := p.User
		if len(user) > 10 {
			user = user[:10]
		}

		cpuColor := colorWhite
		if p.CPUPct > 50 {
			cpuColor = colorWarning
		}
		if p.CPUPct > 80 {
			cpuColor = colorCritical
		}

		statusColor := colorDim
		if p.Status == "running" {
			statusColor = colorPrimary
		} else if p.Status == "zombie" {
			statusColor = colorCritical
		}

		fmt.Fprintf(&b, "   %-7d  %s%-20s%s  %s%7.1f%s  %7.1f  %8s  %s%-10s%s  %-10s\n",
			p.PID,
			colorPrimary, name, colorReset,
			cpuColor, p.CPUPct, colorReset,
			p.MemPct,
			FormatBytes(p.RSS),
			statusColor, p.Status, colorReset,
			user,
		)
	}

	u.procPanel.SetText(b.String())
}

// ─────────────────────────────────────────────
// Footer
// ─────────────────────────────────────────────

func (u *UI) renderFooter() {
	ts := time.Now().Format("15:04:05")
	footer := fmt.Sprintf(" %s[q]%s Quit   %s[Esc]%s Exit   %sRefresh: %v%s   %s%s%s",
		colorSecondary, colorReset,
		colorSecondary, colorReset,
		colorDim, u.refreshRate, colorReset,
		colorDim, ts, colorReset,
	)
	u.footer.SetText(footer)
}

// ─────────────────────────────────────────────
// Bar Rendering
// ─────────────────────────────────────────────

func renderBar(pct float64, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}

	filled := int(pct / 100.0 * float64(width))
	if filled > width {
		filled = width
	}

	empty := width - filled

	// Color gradient based on percentage
	fillColor := colorPrimary
	if pct > 60 {
		fillColor = colorWarning
	}
	if pct > 85 {
		fillColor = colorCritical
	}

	return fmt.Sprintf("%s[%s%s%s%s%s]%s",
		colorDim,
		fillColor, strings.Repeat(barFull, filled),
		colorMuted, strings.Repeat(barEmpty, empty),
		colorDim,
		colorReset,
	)
}

func percentColor(pct float64) string {
	if pct > 85 {
		return colorCritical
	}
	if pct > 60 {
		return colorWarning
	}
	return colorPrimary
}

func tempColor(temp float64) string {
	if temp > 85 {
		return colorCritical
	}
	if temp > 65 {
		return colorWarning
	}
	return colorPrimary
}

func batteryColor(pct float64) string {
	if pct < 15 {
		return colorCritical
	}
	if pct < 30 {
		return colorWarning
	}
	return colorPrimary
}

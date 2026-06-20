package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/shirou/gopsutil/v3/process"
)

// ─────────────────────────────────────────────
// UI Application
//
// Concurrency model: the heavy Collect() runs on a background ticker
// goroutine. The resulting snapshot is handed to the tview event-loop
// goroutine via QueueUpdateDraw, where it is stored and rendered. Therefore
// u.metrics, the history rings and all interactive state are only ever touched
// on the event-loop goroutine — no mutex needed, and the UI never blocks on
// collection.
// ─────────────────────────────────────────────

type UI struct {
	app       *tview.Application
	pages     *tview.Pages
	collector *Collector
	cfg       Config

	metrics     SystemMetrics
	refreshRate time.Duration

	grid        *tview.Grid
	header      *tview.TextView
	cpuPanel    *tview.TextView
	memPanel    *tview.TextView
	diskPanel   *tview.TextView
	ioPanel     *tview.TextView
	gpuPanel    *tview.TextView
	powerPanel  *tview.TextView
	netPanel    *tview.TextView
	procTable   *tview.Table
	footer      *tview.TextView
	filterInput *tview.InputField

	// history rings (sparklines)
	histCPU, histMem, histRx, histTx, histIO *Ring

	// interactive state
	sortKey   string // cpu | mem | pid | name
	filter    string
	filtering bool
	paused    bool
	overlay   string // "" | "help"
	procView  []ProcessInfo

	// alert edge state (rising-edge bell)
	alertOver map[string]bool
}

func NewUI(cfg Config) *UI {
	return &UI{
		app:         tview.NewApplication(),
		collector:   NewCollector(),
		cfg:         cfg,
		refreshRate: cfg.Refresh,
		histCPU:     NewRing(180),
		histMem:     NewRing(180),
		histRx:      NewRing(180),
		histTx:      NewRing(180),
		histIO:      NewRing(180),
		sortKey:     "cpu",
		alertOver:   make(map[string]bool),
	}
}

func (u *UI) Run() error {
	u.buildLayout()

	// Prime once synchronously so the first frame isn't blank and so IO/net/CPU
	// deltas are warm by the next tick.
	m := u.collector.Collect()
	u.metrics = m
	u.pushHistory(m)
	u.renderAll()

	u.startAutoRefresh()
	u.setupKeybindings()
	u.app.EnableMouse(true)
	u.app.SetFocus(u.procTable)
	return u.app.Run()
}

// ─────────────────────────────────────────────
// Layout
// ─────────────────────────────────────────────

func (u *UI) buildLayout() {
	u.header = u.newPanel("")
	u.header.SetBorder(false)
	u.header.SetTextAlign(tview.AlignCenter)

	u.cpuPanel = u.newPanel(u.panelTitle("⚡", "CPU"))
	u.memPanel = u.newPanel(u.panelTitle("🧠", "MEMORY"))
	u.diskPanel = u.newPanel(u.panelTitle("💾", "DISK"))
	u.ioPanel = u.newPanel(u.panelTitle("⇄", "I/O"))
	u.gpuPanel = u.newPanel(u.panelTitle("🎮", "GPU"))
	u.powerPanel = u.newPanel(u.panelTitle("🔌", "POWER"))
	u.netPanel = u.newPanel(u.panelTitle("🌐", "NETWORK"))

	u.procTable = tview.NewTable()
	u.procTable.SetBorder(true).
		SetBorderColor(uiBorderColor).
		SetTitle(u.panelTitle("▶", "PROCESSES")).
		SetTitleColor(uiTitleColor).
		SetTitleAlign(tview.AlignLeft).
		SetBackgroundColor(tcell.ColorBlack).
		SetBorderPadding(0, 0, 1, 1)
	u.procTable.SetSelectable(true, false).
		SetFixed(1, 0).
		SetSelectedStyle(tcell.StyleDefault.Background(uiTitleColor).Foreground(tcell.ColorBlack))

	u.footer = u.newPanel("")
	u.footer.SetBorder(false)
	u.footer.SetTextAlign(tview.AlignLeft)

	u.filterInput = tview.NewInputField().
		SetLabel(" /filter: ").
		SetFieldWidth(0).
		SetLabelColor(tcell.ColorAqua).
		SetFieldBackgroundColor(tcell.ColorBlack).
		SetFieldTextColor(tcell.ColorWhite)
	u.filterInput.SetChangedFunc(func(text string) {
		u.filter = text
		u.renderProcesses()
	})
	u.filterInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			u.filter = ""
		}
		u.closeFilter()
	})

	u.grid = tview.NewGrid().
		SetRows(1, 0, 0, 0, 0, 1).
		SetColumns(0, 0).
		SetBorders(false)

	u.grid.AddItem(u.header, 0, 0, 1, 2, 0, 0, false)
	u.grid.AddItem(u.cpuPanel, 1, 0, 1, 1, 0, 0, false)
	u.grid.AddItem(u.memPanel, 1, 1, 1, 1, 0, 0, false)
	u.grid.AddItem(u.diskPanel, 2, 0, 1, 1, 0, 0, false)

	rightMid := tview.NewGrid().SetRows(0, 0).SetColumns(0).SetBorders(false)
	rightMid.AddItem(u.ioPanel, 0, 0, 1, 1, 0, 0, false)
	rightMid.AddItem(u.netPanel, 1, 0, 1, 1, 0, 0, false)
	u.grid.AddItem(rightMid, 2, 1, 1, 1, 0, 0, false)

	u.grid.AddItem(u.gpuPanel, 3, 0, 1, 1, 0, 0, false)
	u.grid.AddItem(u.powerPanel, 3, 1, 1, 1, 0, 0, false)
	u.grid.AddItem(u.procTable, 4, 0, 1, 2, 0, 0, true)
	u.grid.AddItem(u.footer, 5, 0, 1, 2, 0, 0, false)

	u.pages = tview.NewPages()
	u.pages.AddPage("main", u.grid, true, true)
	u.app.SetRoot(u.pages, true)
}

func (u *UI) newPanel(title string) *tview.TextView {
	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(false).
		SetWordWrap(false)

	tv.SetBorder(true).
		SetBorderColor(uiBorderColor).
		SetTitle(title).
		SetTitleColor(uiTitleColor).
		SetTitleAlign(tview.AlignLeft).
		SetBackgroundColor(tcell.ColorBlack).
		SetBorderPadding(0, 0, 1, 1)

	return tv
}

func (u *UI) panelTitle(emoji, label string) string {
	if u.cfg.Icons {
		return " " + emoji + "  " + label + " "
	}
	return " " + label + " "
}

// ─────────────────────────────────────────────
// Collection loop & render dispatch
// ─────────────────────────────────────────────

func (u *UI) startAutoRefresh() {
	go func() {
		// A collector panic would otherwise abort the process with the terminal
		// still in raw/alt-screen mode; Stop() lets tview restore it.
		defer func() {
			if r := recover(); r != nil {
				u.app.Stop()
				fmt.Fprintf(os.Stderr, "\033[31m[voidmon] collector panic: %v\033[0m\n", r)
			}
		}()

		ticker := time.NewTicker(u.refreshRate)
		defer ticker.Stop()
		for range ticker.C {
			m := u.collector.Collect() // heavy work — off the UI thread
			u.app.QueueUpdateDraw(func() {
				u.metrics = m
				u.pushHistory(m)
				if u.paused {
					u.renderFooter()
					return
				}
				u.checkAlerts(m) // only when live, so a frozen view never beeps
				u.renderAll()
			})
		}
	}()
}

func (u *UI) pushHistory(m SystemMetrics) {
	u.histCPU.Push(m.CPU.Overall)
	u.histMem.Push(m.Memory.UsedPercent)
	u.histRx.Push(m.Network.RecvSpeed)
	u.histTx.Push(m.Network.SendSpeed)
	u.histIO.Push(m.IO.ReadSpeed + m.IO.WriteSpeed)
}

func (u *UI) renderAll() {
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

// ─────────────────────────────────────────────
// Keybindings & interaction
// ─────────────────────────────────────────────

func (u *UI) setupKeybindings() {
	u.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// While typing a filter, let the InputField consume everything.
		if u.filtering {
			return event
		}

		// Kill-confirm modal owns its keys. Escape cancels it here so it can't
		// fall through to the global quit handler; everything else (arrows,
		// Tab, Enter) is left for the modal's own buttons.
		if u.pages.HasPage("kill") {
			if event.Key() == tcell.KeyEscape {
				u.pages.RemovePage("kill")
				u.app.SetFocus(u.procTable)
				return nil
			}
			return event
		}

		// Help overlay: any of ?/h/Esc/q closes it; swallow the rest.
		if u.overlay == "help" {
			switch {
			case event.Key() == tcell.KeyEscape, event.Rune() == '?', event.Rune() == 'h', event.Rune() == 'q':
				u.hideHelp()
			}
			return nil
		}

		switch event.Key() {
		case tcell.KeyEscape:
			u.app.Stop()
			return nil
		case tcell.KeyRune:
			switch event.Rune() {
			case 'q', 'Q':
				u.app.Stop()
				return nil
			case 'c', 'C':
				u.setSort("cpu")
				return nil
			case 'm', 'M':
				u.setSort("mem")
				return nil
			case 'p', 'P':
				u.setSort("pid")
				return nil
			case 'n', 'N':
				u.setSort("name")
				return nil
			case '/':
				u.openFilter()
				return nil
			case ' ':
				u.paused = !u.paused
				u.renderFooter()
				return nil
			case 'k', 'K':
				u.askKill()
				return nil
			case '?', 'h', 'H':
				u.showHelp()
				return nil
			}
		}
		return event // navigation (arrows, pgup/pgdn, home/end) reaches the table
	})
}

func (u *UI) setSort(key string) {
	u.sortKey = key
	u.renderProcesses()
	u.renderFooter()
}

func (u *UI) openFilter() {
	u.filtering = true
	u.filterInput.SetText(u.filter)
	u.grid.RemoveItem(u.footer)
	u.grid.AddItem(u.filterInput, 5, 0, 1, 2, 0, 0, true)
	u.app.SetFocus(u.filterInput)
}

func (u *UI) closeFilter() {
	u.filtering = false
	u.grid.RemoveItem(u.filterInput)
	u.grid.AddItem(u.footer, 5, 0, 1, 2, 0, 0, false)
	u.app.SetFocus(u.procTable)
	u.renderProcesses()
	u.renderFooter()
}

func (u *UI) askKill() {
	row, _ := u.procTable.GetSelection()
	idx := row - 1 // row 0 is the header
	if idx < 0 || idx >= len(u.procView) {
		return
	}
	p := u.procView[idx]

	modal := tview.NewModal().
		SetText(fmt.Sprintf("Send SIGTERM to:\n\n  [%d] %s\n  (user: %s)", p.PID, p.Name, p.User)).
		AddButtons([]string{"Cancel", "Kill"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			u.pages.RemovePage("kill")
			u.app.SetFocus(u.procTable)
			if buttonLabel == "Kill" {
				_ = killPID(p.PID)
			}
		})
	u.pages.AddPage("kill", modal, true, true)
	u.app.SetFocus(modal)
}

func killPID(pid int32) error {
	p, err := process.NewProcess(pid)
	if err != nil {
		return err
	}
	return p.Terminate()
}

func (u *UI) showHelp() {
	u.overlay = "help"
	help := tview.NewTextView().SetDynamicColors(true)
	help.SetBorder(true).
		SetTitle(" voidmon · keys ").
		SetTitleColor(uiTitleColor).
		SetBorderColor(uiBorderColor).
		SetBackgroundColor(tcell.ColorBlack)
	fmt.Fprint(help, helpText())
	u.pages.AddPage("help", modalWrap(help, 48, 18), true, true)
}

func (u *UI) hideHelp() {
	u.overlay = ""
	u.pages.RemovePage("help")
	u.app.SetFocus(u.procTable)
}

func helpText() string {
	return fmt.Sprintf(`
 %sNAVIGATION%s
   ↑/↓  k/j     move selection
   PgUp/PgDn    page through list
   mouse wheel  scroll

 %sSORT PROCESSES%s
   c   by CPU
   m   by memory
   p   by PID
   n   by name

 %sACTIONS%s
   /   filter by name
   k   kill selected (SIGTERM)
   spc pause / resume
   ?   toggle this help
   q   quit
`,
		colorSecondary, colorReset,
		colorSecondary, colorReset,
		colorSecondary, colorReset,
	)
}

func modalWrap(p tview.Primitive, w, h int) tview.Primitive {
	return tview.NewGrid().
		SetColumns(0, w, 0).
		SetRows(0, h, 0).
		AddItem(p, 1, 1, 1, 1, 0, 0, true)
}

// ─────────────────────────────────────────────
// Alerts (edge-triggered bell + panel badge)
// ─────────────────────────────────────────────

func (u *UI) checkAlerts(m SystemMetrics) {
	if !u.cfg.Alerts {
		return
	}
	u.edge("cpu", m.CPU.Overall >= u.cfg.CPUAlert)
	u.edge("mem", m.Memory.UsedPercent >= u.cfg.MemAlert)
	u.edge("temp", m.CPU.Temperature >= u.cfg.TempAlert && m.CPU.Temperature > 0)
	var maxDisk float64
	for _, d := range m.Disks {
		if d.UsedPercent > maxDisk {
			maxDisk = d.UsedPercent
		}
	}
	u.edge("disk", maxDisk >= u.cfg.DiskAlert)
}

func (u *UI) edge(key string, over bool) {
	if over && !u.alertOver[key] {
		fmt.Fprint(os.Stdout, "\a") // terminal bell on the rising edge only
	}
	u.alertOver[key] = over
}

func (u *UI) alertBadge(key string) string {
	if u.cfg.Alerts && u.alertOver[key] {
		return colorCritical + colorBold + " ⚠ " + colorReset
	}
	return ""
}

// ─────────────────────────────────────────────
// Rendering
// ─────────────────────────────────────────────

func (u *UI) renderHeader() {
	m := u.metrics.Host
	pausedTag := ""
	if u.paused {
		pausedTag = fmt.Sprintf("  %s%s[PAUSED]%s", colorCritical, colorBold, colorReset)
	}
	banner := fmt.Sprintf("%s%s░▒▓  V O I D M O N  ▓▒░%s  %s%s · %s · %s · up %s%s%s",
		colorPrimary, colorBold, colorNoBold,
		colorDim, m.Hostname, m.Platform, m.Kernel, FormatUptime(m.Uptime),
		colorReset, pausedTag,
	)
	u.header.SetText(banner)
}

func (u *UI) renderCPU() {
	var b strings.Builder
	c := u.metrics.CPU
	_, _, innerW, _ := u.cpuPanel.GetInnerRect()
	if innerW <= 0 {
		innerW = 44
	}

	u.cpuPanel.SetTitle(u.panelTitle("⚡", "CPU") + u.alertBadge("cpu"))

	// Summary FIRST so it's never clipped on high-core-count machines.
	fmt.Fprintf(&b, " %sOverall%s %s%5.1f%%%s", colorDim, colorReset, percentColor(c.Overall), c.Overall, colorReset)
	if c.Temperature > 0 {
		fmt.Fprintf(&b, "  %sTemp%s %s%.0f°C%s", colorDim, colorReset, tempColor(c.Temperature), c.Temperature, colorReset)
	}
	fmt.Fprintf(&b, "  %sLoad%s %.2f %.2f %.2f\n", colorDim, colorReset, c.LoadAvg1, c.LoadAvg5, c.LoadAvg15)

	// CPU history sparkline.
	sp := sparkline(u.histCPU.Values(), 100, innerW-9)
	fmt.Fprintf(&b, " %shist%s %s%s%s\n", colorDim, colorReset, percentColor(c.Overall), sp, colorReset)

	// Model name (dim, truncated to width).
	if c.ModelName != "" {
		fmt.Fprintf(&b, " %s%s%s\n", colorDim, truncRunes(c.ModelName, innerW-1), colorReset)
	}

	if len(c.PerCore) == 0 {
		// nothing more
	} else if len(c.PerCore) <= 16 {
		barW := clampInt(innerW-18, 6, 28)
		for i, pct := range c.PerCore {
			fmt.Fprintf(&b, " %sc%-2d%s %s %s%5.1f%%%s\n",
				colorSecondary, i, colorReset, renderBar(pct, barW), percentColor(pct), pct, colorReset)
		}
	} else {
		// Many cores: one block glyph per core, colored by load — scales to any
		// core count and never overflows the panel height.
		fmt.Fprintf(&b, " %scores%s %s\n", colorDim, colorReset, coresStrip(c.PerCore))
	}

	u.cpuPanel.SetText(b.String())
}

func (u *UI) renderMemory() {
	var b strings.Builder
	m := u.metrics.Memory
	_, _, innerW, _ := u.memPanel.GetInnerRect()
	if innerW <= 0 {
		innerW = 44
	}
	barW := clampInt(innerW-16, 8, 30)

	u.memPanel.SetTitle(u.panelTitle("🧠", "MEMORY") + u.alertBadge("mem"))

	fmt.Fprintf(&b, "\n  %s%sRAM%s  %s %s%5.1f%%%s\n",
		colorSecondary, colorBold, colorReset,
		renderBar(m.UsedPercent, barW), percentColor(m.UsedPercent), m.UsedPercent, colorReset)
	fmt.Fprintf(&b, "       %sUsed %s / %s   Avail %s%s\n",
		colorDim, FormatBytes(m.Used), FormatBytes(m.Total), FormatBytes(m.Available), colorReset)
	sp := sparkline(u.histMem.Values(), 100, innerW-9)
	fmt.Fprintf(&b, "  %shist%s %s%s%s\n\n", colorDim, colorReset, percentColor(m.UsedPercent), sp, colorReset)

	if m.SwapTotal > 0 {
		fmt.Fprintf(&b, "  %s%sSWAP%s %s %s%5.1f%%%s\n",
			colorAccent, colorBold, colorReset,
			renderBar(m.SwapPercent, barW), percentColor(m.SwapPercent), m.SwapPercent, colorReset)
		fmt.Fprintf(&b, "       %sUsed %s / %s%s\n",
			colorDim, FormatBytes(m.SwapUsed), FormatBytes(m.SwapTotal), colorReset)
	} else {
		fmt.Fprintf(&b, "  %sSWAP%s %snone%s\n", colorAccent, colorReset, colorDim, colorReset)
	}

	u.memPanel.SetText(b.String())
}

func (u *UI) renderDisk() {
	var b strings.Builder
	disks := u.metrics.Disks
	_, _, innerW, _ := u.diskPanel.GetInnerRect()
	if innerW <= 0 {
		innerW = 44
	}

	maxDisk := 0.0
	for _, d := range disks {
		if d.UsedPercent > maxDisk {
			maxDisk = d.UsedPercent
		}
	}
	u.diskPanel.SetTitle(u.panelTitle("💾", "DISK") + u.alertBadge("disk"))

	if len(disks) == 0 {
		fmt.Fprintf(&b, "\n  %sNo disks detected%s", colorDim, colorReset)
		u.diskPanel.SetText(b.String())
		return
	}

	barW := clampInt(innerW-26, 8, 24)
	for _, d := range disks {
		mount := truncRunes(d.MountPoint, 12)
		fmt.Fprintf(&b, "  %s%-12s%s %s %s%5.1f%%%s\n",
			colorSecondary, mount, colorReset,
			renderBar(d.UsedPercent, barW), percentColor(d.UsedPercent), d.UsedPercent, colorReset)
		fmt.Fprintf(&b, "  %s%13s%s / %s  [%s]%s\n",
			colorDim, FormatBytes(d.Used), "", FormatBytes(d.Total), d.Fstype, colorReset)
	}

	u.diskPanel.SetText(b.String())
}

func (u *UI) renderIO() {
	var b strings.Builder
	io := u.metrics.IO
	_, _, innerW, _ := u.ioPanel.GetInnerRect()
	if innerW <= 0 {
		innerW = 40
	}

	fmt.Fprintf(&b, " %s▲ READ %s %s%-12s%s  %sIOPS %.0f%s\n",
		colorPrimary, colorReset, colorWhite, FormatBytesSpeed(io.ReadSpeed), colorReset,
		colorDim, io.ReadIOPS, colorReset)
	fmt.Fprintf(&b, " %s▼ WRITE%s %s%-12s%s  %sIOPS %.0f%s\n",
		colorAccent, colorReset, colorWhite, FormatBytesSpeed(io.WriteSpeed), colorReset,
		colorDim, io.WriteIOPS, colorReset)
	sp := sparkline(u.histIO.Values(), 0, innerW-7)
	fmt.Fprintf(&b, " %sact%s  %s%s%s\n", colorDim, colorReset, colorSecondary, sp, colorReset)
	fmt.Fprintf(&b, " %sΣ R %s W %s%s", colorDim, FormatBytes(io.ReadBytes), FormatBytes(io.WriteBytes), colorReset)

	u.ioPanel.SetText(b.String())
}

func (u *UI) renderNetwork() {
	var b strings.Builder
	n := u.metrics.Network
	_, _, innerW, _ := u.netPanel.GetInnerRect()
	if innerW <= 0 {
		innerW = 40
	}

	fmt.Fprintf(&b, " %s▲ SEND%s %s%-12s%s %s%s%s\n",
		colorPrimary, colorReset, colorWhite, FormatBytesSpeed(n.SendSpeed), colorReset,
		colorPrimary, sparkline(u.histTx.Values(), 0, innerW-22), colorReset)
	fmt.Fprintf(&b, " %s▼ RECV%s %s%-12s%s %s%s%s",
		colorAccent, colorReset, colorWhite, FormatBytesSpeed(n.RecvSpeed), colorReset,
		colorAccent, sparkline(u.histRx.Values(), 0, innerW-22), colorReset)

	u.netPanel.SetText(b.String())
}

func (u *UI) renderGPU() {
	var b strings.Builder
	gpus := u.metrics.GPUs
	_, _, innerW, _ := u.gpuPanel.GetInnerRect()
	if innerW <= 0 {
		innerW = 44
	}

	if len(gpus) == 0 {
		fmt.Fprintf(&b, "\n  %sNo GPU detected%s\n", colorDim, colorReset)
		fmt.Fprintf(&b, "  %s(install nvidia-smi, or AMD/Intel via sysfs)%s", colorDim, colorReset)
		u.gpuPanel.SetText(b.String())
		return
	}

	barW := clampInt(innerW-14, 8, 24)
	for i, g := range gpus {
		if i > 0 {
			fmt.Fprintf(&b, "\n")
		}
		name := g.Name
		if len(gpus) > 1 {
			name = fmt.Sprintf("[%d] %s", i, name)
		}
		fmt.Fprintf(&b, " %s%s%s", colorWhite, truncRunes(name, innerW-2), colorReset)
		if g.DriverVer != "" {
			fmt.Fprintf(&b, " %s[%s]%s", colorDim, g.DriverVer, colorReset)
		}
		fmt.Fprintf(&b, "\n")

		fmt.Fprintf(&b, " %sUsage%s %s %s%5.1f%%%s\n",
			colorSecondary, colorReset, renderBar(g.Utilization, barW), percentColor(g.Utilization), g.Utilization, colorReset)

		if g.MemTotal > 0 {
			fmt.Fprintf(&b, " %sVRAM %s %s %s%5.1f%%%s\n",
				colorAccent, colorReset, renderBar(g.MemPercent, barW), percentColor(g.MemPercent), g.MemPercent, colorReset)
			fmt.Fprintf(&b, "       %s%s / %s%s\n", colorDim, FormatBytes(g.MemUsed), FormatBytes(g.MemTotal), colorReset)
		}

		var info []string
		if g.Temperature > 0 {
			info = append(info, fmt.Sprintf("%sTemp%s %s%.0f°C%s", colorDim, colorReset, tempColor(g.Temperature), g.Temperature, colorReset))
		}
		if g.FanSpeed > 0 {
			info = append(info, fmt.Sprintf("%sFan%s %.0f%%", colorDim, colorReset, g.FanSpeed))
		}
		if g.PowerDraw > 0 {
			info = append(info, fmt.Sprintf("%sPwr%s %.0f/%.0fW", colorDim, colorReset, g.PowerDraw, g.PowerLimit))
		}
		if len(info) > 0 {
			fmt.Fprintf(&b, " %s\n", strings.Join(info, "  "))
		}
	}

	u.gpuPanel.SetText(b.String())
}

func (u *UI) renderPower() {
	var b strings.Builder
	p := u.metrics.Power
	_, _, innerW, _ := u.powerPanel.GetInnerRect()
	if innerW <= 0 {
		innerW = 44
	}

	if !p.Available {
		fmt.Fprintf(&b, "\n  %sNo power data%s\n", colorDim, colorReset)
		fmt.Fprintf(&b, "  %s(desktop / VM)%s", colorDim, colorReset)
		u.powerPanel.SetText(b.String())
		return
	}

	icon := "AC"
	if u.cfg.Icons {
		icon = "🔌"
		if !p.OnAC {
			icon = "🔋"
		}
	}
	fmt.Fprintf(&b, "\n  %s  %s%s%s", icon, colorWhite, p.Status, colorReset)
	if p.TimeRemain != "" {
		fmt.Fprintf(&b, "  %s(%s left)%s", colorDim, p.TimeRemain, colorReset)
	}
	fmt.Fprintf(&b, "\n\n")

	if p.BatteryPct > 0 {
		barW := clampInt(innerW-16, 8, 24)
		fmt.Fprintf(&b, "  %sBattery%s %s %s%5.1f%%%s\n",
			colorSecondary, colorReset, renderBar(p.BatteryPct, barW), batteryColor(p.BatteryPct), p.BatteryPct, colorReset)
	}
	if p.PowerRate > 0 {
		fmt.Fprintf(&b, "\n  %sDraw%s %.1fW", colorDim, colorReset, p.PowerRate)
	}

	u.powerPanel.SetText(b.String())
}

func (u *UI) renderProcesses() {
	// Filter across ALL processes, then sort, then cap to the display count.
	procs := make([]ProcessInfo, 0, len(u.metrics.Processes))
	f := strings.ToLower(strings.TrimSpace(u.filter))
	for _, p := range u.metrics.Processes {
		if f == "" || strings.Contains(strings.ToLower(p.Name), f) || strings.Contains(p.Port, f) {
			procs = append(procs, p)
		}
	}
	sortProcesses(procs, u.sortKey)
	if len(procs) > u.cfg.ProcessCount {
		procs = procs[:u.cfg.ProcessCount]
	}
	u.procView = procs

	t := u.procTable
	t.Clear()

	headers := []string{"PID", "NAME", "CPU%", "RAM", "GPU MEM", "PORT(S)", "STATUS", "USER"}
	for col, h := range headers {
		cell := tview.NewTableCell(h).
			SetTextColor(tcSecondary).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false)
		if col == 0 || col == 2 || col == 3 {
			cell.SetAlign(tview.AlignRight)
		}
		t.SetCell(0, col, cell)
	}

	for i, p := range procs {
		row := i + 1
		name := truncRunes(p.Name, 22)
		user := truncRunes(p.User, 12)

		cpuColor := tcWhite
		if p.CPUPct > 50 {
			cpuColor = tcWarning
		}
		if p.CPUPct > 90 {
			cpuColor = tcCritical
		}
		statusColor := tcDim
		switch p.Status {
		case "running":
			statusColor = tcPrimary
		case "zombie":
			statusColor = tcCritical
		}

		gpuStr := "-"
		if p.GPUMem > 0 {
			gpuStr = FormatBytes(p.GPUMem)
		}
		portStr := p.Port
		if portStr == "" {
			portStr = "-"
		} else {
			portStr = truncRunes(portStr, 14)
		}

		t.SetCell(row, 0, tview.NewTableCell(fmt.Sprintf("%d", p.PID)).SetTextColor(tcDim).SetAlign(tview.AlignRight))
		t.SetCell(row, 1, tview.NewTableCell(name).SetTextColor(tcPrimary).SetExpansion(1))
		t.SetCell(row, 2, tview.NewTableCell(fmt.Sprintf("%.1f", p.CPUPct)).SetTextColor(cpuColor).SetAlign(tview.AlignRight))
		t.SetCell(row, 3, tview.NewTableCell(fmt.Sprintf("%.1f%% %s", p.MemPct, FormatBytes(p.RSS))).SetTextColor(tcWhite).SetAlign(tview.AlignRight))
		t.SetCell(row, 4, tview.NewTableCell(gpuStr).SetTextColor(tcAccent))
		t.SetCell(row, 5, tview.NewTableCell(portStr).SetTextColor(tcSecondary))
		t.SetCell(row, 6, tview.NewTableCell(p.Status).SetTextColor(statusColor))
		t.SetCell(row, 7, tview.NewTableCell(user).SetTextColor(tcDim))
	}

	// Keep the selection within bounds after a refresh.
	if n := len(procs); n > 0 {
		row, _ := t.GetSelection()
		if row < 1 {
			row = 1
		}
		if row > n {
			row = n
		}
		t.Select(row, 0)
	}
}

func (u *UI) renderFooter() {
	sortLabel := map[string]string{"cpu": "CPU", "mem": "MEM", "pid": "PID", "name": "NAME"}[u.sortKey]
	filterPart := ""
	if u.filter != "" {
		filterPart = fmt.Sprintf("   %sfilter:%s%s%s", colorDim, colorWhite, u.filter, colorReset)
	}
	pausePart := ""
	if u.paused {
		pausePart = fmt.Sprintf("   %s%sPAUSED%s", colorCritical, colorBold, colorReset)
	}
	footer := fmt.Sprintf(" %s[q]%squit %s[?]%shelp %s[/]%sfilter %s[k]%skill %s[c/m/p/n]%ssort:%s%s%s%s%s   %s%s%s",
		colorSecondary, colorReset, colorSecondary, colorReset,
		colorSecondary, colorReset, colorSecondary, colorReset,
		colorSecondary, colorReset, colorWhite, sortLabel, colorReset,
		filterPart, pausePart,
		colorDim, time.Now().Format("15:04:05"), colorReset,
	)
	u.footer.SetText(footer)
}

// ─────────────────────────────────────────────
// Process sorting
// ─────────────────────────────────────────────

func sortProcesses(procs []ProcessInfo, key string) {
	switch key {
	case "mem":
		sort.SliceStable(procs, func(i, j int) bool { return procs[i].MemPct > procs[j].MemPct })
	case "pid":
		sort.SliceStable(procs, func(i, j int) bool { return procs[i].PID < procs[j].PID })
	case "name":
		sort.SliceStable(procs, func(i, j int) bool {
			return strings.ToLower(procs[i].Name) < strings.ToLower(procs[j].Name)
		})
	default: // cpu
		sort.SliceStable(procs, func(i, j int) bool { return procs[i].CPUPct > procs[j].CPUPct })
	}
}

// ─────────────────────────────────────────────
// Bar / glyph rendering
// ─────────────────────────────────────────────

func renderBar(pct float64, width int) string {
	if width < 1 {
		return ""
	}
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
		colorDim, colorReset,
	)
}

// coresStrip renders one block glyph per core, colored by load.
func coresStrip(cores []float64) string {
	var b strings.Builder
	last := len(sparkRunes) - 1
	for _, pct := range cores {
		if pct < 0 {
			pct = 0
		}
		if pct > 100 {
			pct = 100
		}
		idx := int(pct / 100 * float64(last))
		if idx > last {
			idx = last
		}
		b.WriteString(percentColor(pct))
		b.WriteRune(sparkRunes[idx])
	}
	b.WriteString(colorReset)
	return b.String()
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

// ─────────────────────────────────────────────
// Small helpers
// ─────────────────────────────────────────────

func truncRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

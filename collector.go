package main

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

// ─────────────────────────────────────────────
// Data Structures
// ─────────────────────────────────────────────

type SystemMetrics struct {
	CPU       CPUMetrics
	Memory    MemoryMetrics
	Disks     []DiskMetrics
	IO        IOMetrics
	Network   NetworkMetrics
	GPUs      []GPUMetrics
	Processes []ProcessInfo
	Power     PowerMetrics
	Host      HostInfo
}

type CPUMetrics struct {
	PerCore     []float64
	Overall     float64
	Threads     int
	ModelName   string
	Frequency   float64
	LoadAvg1    float64
	LoadAvg5    float64
	LoadAvg15   float64
	Temperature float64
}

type MemoryMetrics struct {
	Total       uint64
	Used        uint64
	Available   uint64
	UsedPercent float64
	SwapTotal   uint64
	SwapUsed    uint64
	SwapPercent float64
	Cached      uint64
	Buffers     uint64
}

type DiskMetrics struct {
	MountPoint  string
	Device      string
	Fstype      string
	Total       uint64
	Used        uint64
	Free        uint64
	UsedPercent float64
}

type IOMetrics struct {
	ReadBytes  uint64
	WriteBytes uint64
	ReadSpeed  float64 // bytes/sec
	WriteSpeed float64 // bytes/sec
	ReadCount  uint64
	WriteCount uint64
	ReadIOPS   float64
	WriteIOPS  float64
}

type NetworkMetrics struct {
	BytesSent uint64
	BytesRecv uint64
	SendSpeed float64
	RecvSpeed float64
}

type GPUMetrics struct {
	Available   bool
	Name        string
	MemTotal    uint64
	MemUsed     uint64
	MemPercent  float64
	Utilization float64
	Temperature float64
	FanSpeed    float64
	PowerDraw   float64
	PowerLimit  float64
	DriverVer   string
}

type ProcessInfo struct {
	PID    int32
	Name   string
	CPUPct float64
	MemPct float32
	Status string
	User   string
	RSS    uint64
}

type PowerMetrics struct {
	Available  bool
	OnAC       bool
	BatteryPct float64
	Status     string
	PowerRate  float64 // watts
	TimeRemain string
}

type HostInfo struct {
	Hostname string
	OS       string
	Platform string
	Kernel   string
	Uptime   time.Duration
	Arch     string
}

// ─────────────────────────────────────────────
// Collector
// ─────────────────────────────────────────────

type Collector struct {
	prevIORead   uint64
	prevIOWrite  uint64
	prevIOReadC  uint64
	prevIOWriteC uint64
	prevNetSent  uint64
	prevNetRecv  uint64
	prevTime     time.Time
	initialized  bool
}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Collect() SystemMetrics {
	now := time.Now()
	elapsed := now.Sub(c.prevTime).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}

	m := SystemMetrics{
		CPU:       c.collectCPU(),
		Memory:    c.collectMemory(),
		Disks:     c.collectDisks(),
		IO:        c.collectIO(elapsed),
		Network:   c.collectNetwork(elapsed),
		GPUs:      c.collectGPUs(),
		Processes: c.collectProcesses(),
		Power:     c.collectPower(),
		Host:      c.collectHost(),
	}

	c.prevTime = now
	c.initialized = true
	return m
}

// ─────────────────────────────────────────────
// CPU Collection
// ─────────────────────────────────────────────

func (c *Collector) collectCPU() CPUMetrics {
	m := CPUMetrics{
		Threads: runtime.NumCPU(),
	}

	// Per-core usage
	perCore, err := cpu.Percent(0, true)
	if err == nil {
		m.PerCore = perCore
	}

	// Overall usage
	overall, err := cpu.Percent(0, false)
	if err == nil && len(overall) > 0 {
		m.Overall = overall[0]
	}

	// CPU info
	infos, err := cpu.Info()
	if err == nil && len(infos) > 0 {
		m.ModelName = infos[0].ModelName
		m.Frequency = infos[0].Mhz
	}

	// Load average
	loadAvg, err := load.Avg()
	if err == nil {
		m.LoadAvg1 = loadAvg.Load1
		m.LoadAvg5 = loadAvg.Load5
		m.LoadAvg15 = loadAvg.Load15
	}

	// CPU temperature
	m.Temperature = readCPUTemp()

	return m
}

// readCPUTemp is implemented in collector_linux.go / collector_darwin.go

// ─────────────────────────────────────────────
// Memory Collection
// ─────────────────────────────────────────────

func (c *Collector) collectMemory() MemoryMetrics {
	m := MemoryMetrics{}

	v, err := mem.VirtualMemory()
	if err == nil {
		m.Total = v.Total
		m.Used = v.Used
		m.Available = v.Available
		m.UsedPercent = v.UsedPercent
		m.Cached = v.Cached
		m.Buffers = v.Buffers
	}

	s, err := mem.SwapMemory()
	if err == nil {
		m.SwapTotal = s.Total
		m.SwapUsed = s.Used
		m.SwapPercent = s.UsedPercent
	}

	return m
}

// ─────────────────────────────────────────────
// Disk Collection
// ─────────────────────────────────────────────

func (c *Collector) collectDisks() []DiskMetrics {
	var disks []DiskMetrics

	partitions, err := disk.Partitions(false)
	if err != nil {
		return disks
	}

	seen := make(map[string]bool)
	for _, p := range partitions {
		// Skip pseudo/virtual filesystems
		if strings.HasPrefix(p.Mountpoint, "/snap") ||
			strings.HasPrefix(p.Mountpoint, "/boot/efi") ||
			p.Fstype == "squashfs" || p.Fstype == "tmpfs" ||
			p.Fstype == "devtmpfs" || p.Fstype == "overlay" {
			continue
		}

		if seen[p.Device] {
			continue
		}
		seen[p.Device] = true

		usage, err := disk.Usage(p.Mountpoint)
		if err != nil || usage.Total == 0 {
			continue
		}

		disks = append(disks, DiskMetrics{
			MountPoint:  p.Mountpoint,
			Device:      p.Device,
			Fstype:      p.Fstype,
			Total:       usage.Total,
			Used:        usage.Used,
			Free:        usage.Free,
			UsedPercent: usage.UsedPercent,
		})
	}

	// Sort by mountpoint
	sort.Slice(disks, func(i, j int) bool {
		return disks[i].MountPoint < disks[j].MountPoint
	})

	// Limit to 6 entries max for display
	if len(disks) > 6 {
		disks = disks[:6]
	}

	return disks
}

// ─────────────────────────────────────────────
// I/O Collection
// ─────────────────────────────────────────────

func (c *Collector) collectIO(elapsed float64) IOMetrics {
	m := IOMetrics{}

	counters, err := disk.IOCounters()
	if err != nil {
		return m
	}

	var totalRead, totalWrite, totalReadC, totalWriteC uint64
	for _, counter := range counters {
		totalRead += counter.ReadBytes
		totalWrite += counter.WriteBytes
		totalReadC += counter.ReadCount
		totalWriteC += counter.WriteCount
	}

	m.ReadBytes = totalRead
	m.WriteBytes = totalWrite
	m.ReadCount = totalReadC
	m.WriteCount = totalWriteC

	if c.initialized && elapsed > 0 {
		m.ReadSpeed = float64(totalRead-c.prevIORead) / elapsed
		m.WriteSpeed = float64(totalWrite-c.prevIOWrite) / elapsed
		m.ReadIOPS = float64(totalReadC-c.prevIOReadC) / elapsed
		m.WriteIOPS = float64(totalWriteC-c.prevIOWriteC) / elapsed
	}

	c.prevIORead = totalRead
	c.prevIOWrite = totalWrite
	c.prevIOReadC = totalReadC
	c.prevIOWriteC = totalWriteC

	return m
}

// ─────────────────────────────────────────────
// Network Collection
// ─────────────────────────────────────────────

func (c *Collector) collectNetwork(elapsed float64) NetworkMetrics {
	m := NetworkMetrics{}

	counters, err := net.IOCounters(false)
	if err != nil || len(counters) == 0 {
		return m
	}

	m.BytesSent = counters[0].BytesSent
	m.BytesRecv = counters[0].BytesRecv

	if c.initialized && elapsed > 0 {
		m.SendSpeed = float64(m.BytesSent-c.prevNetSent) / elapsed
		m.RecvSpeed = float64(m.BytesRecv-c.prevNetRecv) / elapsed
	}

	c.prevNetSent = m.BytesSent
	c.prevNetRecv = m.BytesRecv

	return m
}

// ─────────────────────────────────────────────
// GPU Collection (platform-dispatched)
// ─────────────────────────────────────────────

func (c *Collector) collectGPUs() []GPUMetrics {
	// collectGPUPlatform is in collector_linux.go / collector_darwin.go
	return collectGPUPlatform()
}

// collectGPUPlatform is implemented in collector_linux.go / collector_darwin.go

// ─────────────────────────────────────────────
// Process Collection
// ─────────────────────────────────────────────

func (c *Collector) collectProcesses() []ProcessInfo {
	var procs []ProcessInfo

	pids, err := process.Processes()
	if err != nil {
		return procs
	}

	for _, p := range pids {
		name, err := p.Name()
		if err != nil || name == "" {
			continue
		}

		cpuPct, _ := p.CPUPercent()
		memPct, _ := p.MemoryPercent()
		status, _ := p.Status()
		user, _ := p.Username()

		var rss uint64
		memInfo, err := p.MemoryInfo()
		if err == nil && memInfo != nil {
			rss = memInfo.RSS
		}

		statusStr := "unknown"
		if len(status) > 0 {
			switch status[0] {
			case "R":
				statusStr = "running"
			case "S":
				statusStr = "sleeping"
			case "T":
				statusStr = "stopped"
			case "Z":
				statusStr = "zombie"
			case "D":
				statusStr = "io-wait"
			case "I":
				statusStr = "idle"
			default:
				statusStr = status[0]
			}
		}

		procs = append(procs, ProcessInfo{
			PID:    p.Pid,
			Name:   name,
			CPUPct: cpuPct,
			MemPct: memPct,
			Status: statusStr,
			User:   user,
			RSS:    rss,
		})
	}

	// Sort by CPU usage descending
	sort.Slice(procs, func(i, j int) bool {
		return procs[i].CPUPct > procs[j].CPUPct
	})

	// Top 15 processes
	if len(procs) > 15 {
		procs = procs[:15]
	}

	return procs
}

// ─────────────────────────────────────────────
// Power Collection (platform-dispatched)
// ─────────────────────────────────────────────

func (c *Collector) collectPower() PowerMetrics {
	// collectPowerPlatform is in collector_linux.go / collector_darwin.go
	return collectPowerPlatform()
}

// collectPowerPlatform is implemented in collector_linux.go / collector_darwin.go

// ─────────────────────────────────────────────
// Host Collection
// ─────────────────────────────────────────────

func (c *Collector) collectHost() HostInfo {
	h := HostInfo{
		Arch: runtime.GOARCH,
	}

	info, err := host.Info()
	if err == nil {
		h.Hostname = info.Hostname
		h.OS = info.OS
		h.Platform = info.Platform
		h.Kernel = info.KernelVersion
		h.Uptime = time.Duration(info.Uptime) * time.Second
	}

	return h
}

// ─────────────────────────────────────────────
// Utility Functions
// ─────────────────────────────────────────────

func FormatBytes(b uint64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)
	switch {
	case b >= TB:
		return fmt.Sprintf("%.1fT", float64(b)/float64(TB))
	case b >= GB:
		return fmt.Sprintf("%.1fG", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1fM", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.1fK", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

func FormatBytesSpeed(bps float64) string {
	if bps < 0 {
		bps = 0
	}
	const (
		KB = 1024.0
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bps >= GB:
		return fmt.Sprintf("%.1f GB/s", bps/GB)
	case bps >= MB:
		return fmt.Sprintf("%.1f MB/s", bps/MB)
	case bps >= KB:
		return fmt.Sprintf("%.1f KB/s", bps/KB)
	default:
		return fmt.Sprintf("%.0f B/s", bps)
	}
}

func FormatUptime(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

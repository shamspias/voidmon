package main

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
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
// Data Structures (json tags power the headless --json mode)
// ─────────────────────────────────────────────

type SystemMetrics struct {
	CPU       CPUMetrics     `json:"cpu"`
	Memory    MemoryMetrics  `json:"memory"`
	Disks     []DiskMetrics  `json:"disks"`
	IO        IOMetrics      `json:"io"`
	Network   NetworkMetrics `json:"network"`
	GPUs      []GPUMetrics   `json:"gpus"`
	Processes []ProcessInfo  `json:"processes"`
	Power     PowerMetrics   `json:"power"`
	Host      HostInfo       `json:"host"`
	Timestamp int64          `json:"timestamp"` // unix seconds, when sampled
}

type CPUMetrics struct {
	PerCore     []float64 `json:"per_core"`
	Overall     float64   `json:"overall"`
	Threads     int       `json:"threads"`
	ModelName   string    `json:"model_name"`
	Frequency   float64   `json:"frequency_mhz"`
	LoadAvg1    float64   `json:"load_avg_1"`
	LoadAvg5    float64   `json:"load_avg_5"`
	LoadAvg15   float64   `json:"load_avg_15"`
	Temperature float64   `json:"temperature_c"`
}

type MemoryMetrics struct {
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Available   uint64  `json:"available"`
	UsedPercent float64 `json:"used_percent"`
	SwapTotal   uint64  `json:"swap_total"`
	SwapUsed    uint64  `json:"swap_used"`
	SwapPercent float64 `json:"swap_percent"`
	Cached      uint64  `json:"cached"`
	Buffers     uint64  `json:"buffers"`
}

type DiskMetrics struct {
	MountPoint  string  `json:"mount_point"`
	Device      string  `json:"device"`
	Fstype      string  `json:"fstype"`
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Free        uint64  `json:"free"`
	UsedPercent float64 `json:"used_percent"`
}

type IOMetrics struct {
	ReadBytes  uint64  `json:"read_bytes"`
	WriteBytes uint64  `json:"write_bytes"`
	ReadSpeed  float64 `json:"read_speed"`  // bytes/sec
	WriteSpeed float64 `json:"write_speed"` // bytes/sec
	ReadCount  uint64  `json:"read_count"`
	WriteCount uint64  `json:"write_count"`
	ReadIOPS   float64 `json:"read_iops"`
	WriteIOPS  float64 `json:"write_iops"`
}

type NetworkMetrics struct {
	BytesSent uint64  `json:"bytes_sent"`
	BytesRecv uint64  `json:"bytes_recv"`
	SendSpeed float64 `json:"send_speed"`
	RecvSpeed float64 `json:"recv_speed"`
}

type GPUMetrics struct {
	Available   bool    `json:"available"`
	Name        string  `json:"name"`
	MemTotal    uint64  `json:"mem_total"`
	MemUsed     uint64  `json:"mem_used"`
	MemPercent  float64 `json:"mem_percent"`
	Utilization float64 `json:"utilization"`
	Temperature float64 `json:"temperature_c"`
	FanSpeed    float64 `json:"fan_speed"`
	PowerDraw   float64 `json:"power_draw"`
	PowerLimit  float64 `json:"power_limit"`
	DriverVer   string  `json:"driver_version"`
}

type ProcessInfo struct {
	PID    int32   `json:"pid"`
	Name   string  `json:"name"`
	CPUPct float64 `json:"cpu_percent"`
	MemPct float32 `json:"mem_percent"`
	RSS    uint64  `json:"rss"`
	GPUMem uint64  `json:"gpu_mem"`
	Port   string  `json:"port"`
	Status string  `json:"status"`
	User   string  `json:"user"`
}

type PowerMetrics struct {
	Available  bool    `json:"available"`
	OnAC       bool    `json:"on_ac"`
	BatteryPct float64 `json:"battery_percent"`
	Status     string  `json:"status"`
	PowerRate  float64 `json:"power_rate"` // watts
	TimeRemain string  `json:"time_remaining"`
}

type HostInfo struct {
	Hostname  string        `json:"hostname"`
	OS        string        `json:"os"`
	Platform  string        `json:"platform"`
	Kernel    string        `json:"kernel"`
	Uptime    time.Duration `json:"-"`
	UptimeSec uint64        `json:"uptime_sec"`
	Arch      string        `json:"arch"`
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

	// prevProcCPU holds the previous total CPU-seconds per PID so we can
	// derive *instantaneous* per-process CPU%, not a since-boot average.
	prevProcCPU map[int32]float64

	// Cached identity — fetched once, never changes at runtime.
	cpuModel string
	cpuFreq  float64
	hostBase HostInfo
}

func NewCollector() *Collector {
	c := &Collector{prevProcCPU: make(map[int32]float64)}
	c.loadIdentity()
	return c
}

// loadIdentity fetches data that never changes while the program runs so we
// don't re-spawn/re-query it on every refresh.
func (c *Collector) loadIdentity() {
	if infos, err := cpu.Info(); err == nil && len(infos) > 0 {
		c.cpuModel = infos[0].ModelName
		c.cpuFreq = infos[0].Mhz
	}
	c.hostBase = HostInfo{Arch: runtime.GOARCH}
	if info, err := host.Info(); err == nil {
		c.hostBase.Hostname = info.Hostname
		c.hostBase.OS = info.OS
		c.hostBase.Platform = info.Platform
		c.hostBase.Kernel = info.KernelVersion
	}
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
		Processes: c.collectProcesses(elapsed),
		Power:     c.collectPower(),
		Host:      c.collectHost(),
		Timestamp: now.Unix(),
	}

	c.prevTime = now
	c.initialized = true
	return m
}

func (c *Collector) collectCPU() CPUMetrics {
	m := CPUMetrics{
		Threads:   runtime.NumCPU(),
		ModelName: c.cpuModel,
		Frequency: c.cpuFreq,
	}

	// One sample of per-core usage since the previous call. We derive Overall
	// from the same sample instead of a second cpu.Percent() call (which would
	// measure a near-zero interval and produce noise).
	if perCore, err := cpu.Percent(0, true); err == nil && len(perCore) > 0 {
		m.PerCore = perCore
		var sum float64
		for _, v := range perCore {
			sum += v
		}
		m.Overall = sum / float64(len(perCore))
	}

	if loadAvg, err := load.Avg(); err == nil {
		m.LoadAvg1 = loadAvg.Load1
		m.LoadAvg5 = loadAvg.Load5
		m.LoadAvg15 = loadAvg.Load15
	}

	m.Temperature = readCPUTemp()
	return m
}

func (c *Collector) collectMemory() MemoryMetrics {
	m := MemoryMetrics{}

	if v, err := mem.VirtualMemory(); err == nil {
		m.Total = v.Total
		m.Used = v.Used
		m.Available = v.Available
		m.UsedPercent = v.UsedPercent
		m.Cached = v.Cached
		m.Buffers = v.Buffers
	}

	if s, err := mem.SwapMemory(); err == nil {
		m.SwapTotal = s.Total
		m.SwapUsed = s.Used
		m.SwapPercent = s.UsedPercent
	}

	return m
}

func (c *Collector) collectDisks() []DiskMetrics {
	var disks []DiskMetrics

	partitions, err := disk.Partitions(false)
	if err != nil {
		return disks
	}

	seen := make(map[string]bool)
	for _, p := range partitions {
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

	sort.Slice(disks, func(i, j int) bool {
		return disks[i].MountPoint < disks[j].MountPoint
	})

	if len(disks) > 6 {
		disks = disks[:6]
	}

	return disks
}

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

	if c.initialized {
		m.ReadSpeed = rate(totalRead, c.prevIORead, elapsed)
		m.WriteSpeed = rate(totalWrite, c.prevIOWrite, elapsed)
		m.ReadIOPS = rate(totalReadC, c.prevIOReadC, elapsed)
		m.WriteIOPS = rate(totalWriteC, c.prevIOWriteC, elapsed)
	}

	c.prevIORead = totalRead
	c.prevIOWrite = totalWrite
	c.prevIOReadC = totalReadC
	c.prevIOWriteC = totalWriteC

	return m
}

func (c *Collector) collectNetwork(elapsed float64) NetworkMetrics {
	m := NetworkMetrics{}

	counters, err := net.IOCounters(true)
	if err != nil || len(counters) == 0 {
		return m
	}

	// Sum across real interfaces (loopback excluded) so the totals stay stable
	// even when the interface set changes between ticks.
	var sent, recv uint64
	for _, iface := range counters {
		name := strings.ToLower(iface.Name)
		if name == "lo" || strings.HasPrefix(name, "lo0") || strings.Contains(name, "loopback") {
			continue
		}
		sent += iface.BytesSent
		recv += iface.BytesRecv
	}

	m.BytesSent = sent
	m.BytesRecv = recv

	if c.initialized {
		m.SendSpeed = rate(sent, c.prevNetSent, elapsed)
		m.RecvSpeed = rate(recv, c.prevNetRecv, elapsed)
	}

	c.prevNetSent = sent
	c.prevNetRecv = recv

	return m
}

func (c *Collector) collectGPUs() []GPUMetrics {
	return collectGPUPlatform()
}

// ─────────────────────────────────────────────
// Process Collection
// ─────────────────────────────────────────────

func (c *Collector) collectProcesses(elapsed float64) []ProcessInfo {
	var procs []ProcessInfo

	// 1. Map listening network ports to PIDs.
	portMap := make(map[int32]string)
	if conns, err := net.Connections("inet"); err == nil {
		for _, conn := range conns {
			if conn.Status == "LISTEN" && conn.Pid > 0 {
				portStr := strconv.Itoa(int(conn.Laddr.Port))
				if existing, ok := portMap[conn.Pid]; ok {
					if !strings.Contains(existing, portStr) {
						portMap[conn.Pid] = existing + "," + portStr
					}
				} else {
					portMap[conn.Pid] = portStr
				}
			}
		}
	}

	// 2. Map GPU memory to PIDs.
	gpuMemMap := collectProcessGPU()

	// 3. Collect per-process details.
	pids, err := process.Processes()
	if err != nil {
		return procs
	}

	// Rebuilt each tick so dead PIDs are pruned automatically.
	newProcCPU := make(map[int32]float64, len(pids))

	for _, p := range pids {
		name, err := p.Name()
		if err != nil || name == "" {
			continue
		}

		// Instantaneous CPU%: delta of (user+system) CPU-seconds over wall time.
		// 100% == one core saturated; a multi-threaded process can exceed 100%.
		var cpuPct float64
		if t, err := p.Times(); err == nil {
			total := t.User + t.System
			newProcCPU[p.Pid] = total
			if prev, ok := c.prevProcCPU[p.Pid]; ok && elapsed > 0 {
				if d := total - prev; d > 0 {
					cpuPct = d / elapsed * 100
				}
			}
		}

		memPct, _ := p.MemoryPercent()
		status, _ := p.Status()
		user, _ := p.Username()

		var rss uint64
		if memInfo, err := p.MemoryInfo(); err == nil && memInfo != nil {
			rss = memInfo.RSS
		}

		procs = append(procs, ProcessInfo{
			PID:    p.Pid,
			Name:   name,
			CPUPct: cpuPct,
			MemPct: memPct,
			RSS:    rss,
			GPUMem: gpuMemMap[p.Pid],
			Port:   portMap[p.Pid],
			Status: normalizeStatus(status),
			User:   user,
		})
	}

	c.prevProcCPU = newProcCPU

	// Default ordering: highest CPU first. The UI may re-sort/filter on top.
	sort.Slice(procs, func(i, j int) bool {
		return procs[i].CPUPct > procs[j].CPUPct
	})

	return procs
}

func normalizeStatus(status []string) string {
	if len(status) == 0 {
		return "unknown"
	}
	switch status[0] {
	case "R":
		return "running"
	case "S":
		return "sleeping"
	case "T":
		return "stopped"
	case "Z":
		return "zombie"
	case "D":
		return "io-wait"
	case "I":
		return "idle"
	default:
		return status[0]
	}
}

// ─────────────────────────────────────────────
// Power and Host Collection
// ─────────────────────────────────────────────

func (c *Collector) collectPower() PowerMetrics {
	return collectPowerPlatform()
}

func (c *Collector) collectHost() HostInfo {
	h := c.hostBase
	if up, err := host.Uptime(); err == nil {
		h.Uptime = time.Duration(up) * time.Second
		h.UptimeSec = up
	}
	return h
}

// ─────────────────────────────────────────────
// Shared helpers
// ─────────────────────────────────────────────

// rate returns a non-negative per-second delta, treating any counter
// rollover / device-removal (now < prev) as zero instead of a ~2^64 spike.
func rate(now, prev uint64, elapsed float64) float64 {
	if now < prev || elapsed <= 0 {
		return 0
	}
	return float64(now-prev) / elapsed
}

// cmdOutput runs an external command with a hard timeout so a wedged helper
// (nvidia-smi, powermetrics, system_profiler, ...) can never stall a refresh.
func cmdOutput(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}

// ─────────────────────────────────────────────
// Formatting utilities
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

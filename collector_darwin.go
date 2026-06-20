//go:build darwin

package main

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ─────────────────────────────────────────────
// powermetrics — sampled once per tick, shared by CPU temp / GPU / power.
//
// powermetrics requires sudo. We probe once; if passwordless sudo is not
// available we cache the negative and never spawn it again. When it IS
// available we run a SINGLE invocation per refresh (smc + gpu_power) and the
// three collectors read from the cached parse instead of shelling out 3x.
// ─────────────────────────────────────────────

type pmSample struct {
	cpuTemp  float64
	gpuUtil  float64
	gpuPower float64 // watts
	sysPower float64 // watts
	taken    time.Time
	valid    bool
}

var (
	pmMu        sync.Mutex
	pmLast      pmSample
	pmAvailable = true
	pmNextProbe time.Time // earliest time to re-probe after a failure
)

func powermetricsSample() pmSample {
	pmMu.Lock()
	defer pmMu.Unlock()

	// Reuse within a single refresh (the three callers run microseconds apart).
	// The TTL must NOT survive into the next tick (refresh floor is 500ms), so
	// keep it small.
	if pmLast.valid && time.Since(pmLast.taken) < 100*time.Millisecond {
		return pmLast
	}
	// After a failure, back off instead of latching forever: a transient
	// timeout (busy box) or briefly-uncached sudo must not disable these metrics
	// for the whole session, and the fast `sudo -n` denial is cheap to retry.
	if !pmAvailable && time.Now().Before(pmNextProbe) {
		return pmSample{}
	}

	out, err := cmdOutput(3*time.Second, "sudo", "-n", "powermetrics",
		"--samplers", "smc,gpu_power", "-i", "200", "-n", "1")
	if err != nil {
		pmAvailable = false
		pmNextProbe = time.Now().Add(30 * time.Second)
		return pmSample{}
	}
	pmAvailable = true

	s := pmSample{taken: time.Now(), valid: true}
	// powermetrics formats every metric as "Label: <number> <unit>", so we read
	// the first number AFTER the colon — robust to cluster indices in the label
	// (e.g. "GPU 0 Power:" must not yield 0).
	reVal := regexp.MustCompile(`:\s*([\d.]+)`)
	val := func(lower string) (float64, bool) {
		if m := reVal.FindStringSubmatch(lower); len(m) > 1 {
			if v, err := strconv.ParseFloat(m[1], 64); err == nil {
				return v, true
			}
		}
		return 0, false
	}
	for _, line := range strings.Split(string(out), "\n") {
		lower := strings.ToLower(line)
		switch {
		case strings.Contains(lower, "cpu die temperature") || strings.Contains(lower, "cpu proximity"):
			if v, ok := val(lower); ok {
				s.cpuTemp = v
			}
		// The real line is "GPU HW active residency: NN.NN%".
		case strings.Contains(lower, "gpu") && strings.Contains(lower, "active residency"):
			if v, ok := val(lower); ok {
				s.gpuUtil = v
			}
		case strings.Contains(lower, "gpu power"):
			if v, ok := val(lower); ok {
				s.gpuPower = mwToW(v, lower)
			}
		case strings.Contains(lower, "combined power") || strings.Contains(lower, "system power"):
			if v, ok := val(lower); ok {
				s.sysPower = mwToW(v, lower)
			}
		}
	}

	pmLast = s
	return s
}

func mwToW(v float64, lower string) float64 {
	if strings.Contains(lower, "mw") {
		return v / 1000.0
	}
	return v
}

// ─────────────────────────────────────────────
// macOS CPU Temperature
// ─────────────────────────────────────────────

func readCPUTemp() float64 {
	// osx-cpu-temp (brew) — fast, no sudo.
	if out, err := cmdOutput(2*time.Second, "osx-cpu-temp", "-c"); err == nil {
		s := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(string(out)), "°C"))
		if v, err := strconv.ParseFloat(s, 64); err == nil && v > 0 {
			return v
		}
	}

	// istats gem (if installed).
	if out, err := cmdOutput(2*time.Second, "istats", "cpu", "temp", "--value-only"); err == nil {
		s := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(string(out)), "°C"))
		if v, err := strconv.ParseFloat(s, 64); err == nil && v > 0 {
			return v
		}
	}

	// Last resort: powermetrics (needs passwordless sudo; probed once).
	return powermetricsSample().cpuTemp
}

// ─────────────────────────────────────────────
// macOS GPU Collection
// Supports: Apple Silicon (M1/M2/M3/M4), Intel+AMD, Intel+NVIDIA
// ─────────────────────────────────────────────

type spDisplayData struct {
	SPDisplaysDataType []struct {
		Name        string `json:"_name"`
		ChipType    string `json:"sppci_model"`
		Bus         string `json:"sppci_bus"`
		VRAM        string `json:"sppci_vram"`
		VRAMShared  string `json:"sppci_vram_shared"`
		Vendor      string `json:"spdisplays_vendor"`
		DeviceID    string `json:"spdisplays_device-id"`
		MetalFamily string `json:"spdisplays_metal"`
	} `json:"SPDisplaysDataType"`
}

var (
	gpuIdentityMu   sync.Mutex
	gpuIdentity     []GPUMetrics
	gpuIdentityDone bool
	appleSiliconVal bool
	appleSiliconHas bool
	appleSiliconMu  sync.Mutex
)

// gpuIdentityCached runs the (relatively heavy) system_profiler probe and caches
// the GPU identity — names/VRAM/Metal don't change at runtime. A transient
// failure is NOT cached (we return a best-effort fallback and retry next tick),
// so a busy-startup hiccup can't blank the GPU panel for the whole session.
func gpuIdentityCached() []GPUMetrics {
	gpuIdentityMu.Lock()
	defer gpuIdentityMu.Unlock()
	if gpuIdentityDone {
		return gpuIdentity
	}

	out, err := cmdOutput(5*time.Second, "system_profiler", "SPDisplaysDataType", "-json")
	if err != nil {
		return collectGPUFallback()
	}
	var data spDisplayData
	if err := json.Unmarshal(out, &data); err != nil || len(data.SPDisplaysDataType) == 0 {
		return collectGPUFallback()
	}

	var ids []GPUMetrics
	for _, gpu := range data.SPDisplaysDataType {
		m := GPUMetrics{Available: true}
		switch {
		case gpu.ChipType != "":
			m.Name = gpu.ChipType
		case gpu.Name != "":
			m.Name = gpu.Name
		default:
			m.Name = "Unknown GPU"
		}
		vramStr := gpu.VRAM
		if vramStr == "" {
			vramStr = gpu.VRAMShared
		}
		if vramStr != "" {
			m.MemTotal = parseVRAMString(vramStr)
		}
		if gpu.MetalFamily != "" {
			m.DriverVer = gpu.MetalFamily
		}
		ids = append(ids, m)
	}

	gpuIdentity = ids
	gpuIdentityDone = true
	return gpuIdentity
}

func collectGPUPlatform() []GPUMetrics {
	identity := gpuIdentityCached()
	if len(identity) == 0 {
		return nil
	}

	// Copy so live metrics don't mutate the cached identity.
	gpus := make([]GPUMetrics, len(identity))
	copy(gpus, identity)

	if isAppleSilicon() {
		// powermetrics reports aggregate GPU stats for the SoC.
		s := powermetricsSample()
		gpus[0].Utilization = s.gpuUtil
		gpus[0].PowerDraw = s.gpuPower
	} else {
		for i := range gpus {
			gpus[i] = collectIntelMacGPUMetrics(gpus[i])
		}
	}

	return gpus
}

func isAppleSilicon() bool {
	appleSiliconMu.Lock()
	defer appleSiliconMu.Unlock()
	if appleSiliconHas {
		return appleSiliconVal
	}
	appleSiliconHas = true

	if out, err := cmdOutput(2*time.Second, "sysctl", "-n", "machdep.cpu.brand_string"); err == nil {
		appleSiliconVal = strings.Contains(strings.ToLower(strings.TrimSpace(string(out))), "apple")
		return appleSiliconVal
	}
	if out, err := cmdOutput(2*time.Second, "uname", "-m"); err == nil {
		appleSiliconVal = strings.TrimSpace(string(out)) == "arm64"
	}
	return appleSiliconVal
}

func collectIntelMacGPUMetrics(m GPUMetrics) GPUMetrics {
	out, err := cmdOutput(3*time.Second, "ioreg", "-rc", "IOAccelerator")
	if err != nil {
		return m
	}

	reInt := regexp.MustCompile(`=\s*(\d+)`)
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "GPU Core Utilization") || strings.Contains(line, "Device Utilization") {
			re := regexp.MustCompile(`"?(?:GPU Core Utilization|Device Utilization %)"?\s*=\s*(\d+)`)
			if match := re.FindStringSubmatch(line); len(match) > 1 {
				if v, err := strconv.ParseFloat(match[1], 64); err == nil {
					m.Utilization = v
				}
			}
		}

		// VRAM total is reported in MB.
		if strings.Contains(line, "VRAM,totalMB") {
			if match := reInt.FindStringSubmatch(line); len(match) > 1 {
				if v, err := strconv.ParseUint(match[1], 10, 64); err == nil {
					m.MemTotal = v * 1024 * 1024
				}
			}
		}
		// Used VRAM: handle the two keys with their correct units — never guess
		// from magnitude.
		if strings.Contains(line, "VRAM,usedMB") {
			if match := reInt.FindStringSubmatch(line); len(match) > 1 {
				if v, err := strconv.ParseUint(match[1], 10, 64); err == nil {
					m.MemUsed = v * 1024 * 1024
				}
			}
		} else if strings.Contains(line, "vramUsedBytes") {
			if match := reInt.FindStringSubmatch(line); len(match) > 1 {
				if v, err := strconv.ParseUint(match[1], 10, 64); err == nil {
					m.MemUsed = v
				}
			}
		}
	}

	if m.MemTotal > 0 && m.MemUsed > 0 {
		m.MemPercent = float64(m.MemUsed) / float64(m.MemTotal) * 100
		if m.MemPercent > 100 {
			m.MemPercent = 100
		}
	}

	return m
}

func collectGPUFallback() []GPUMetrics {
	var gpus []GPUMetrics

	if out, err := cmdOutput(2*time.Second, "sysctl", "-n", "machdep.cpu.brand_string"); err == nil {
		brand := strings.TrimSpace(string(out))
		if strings.Contains(strings.ToLower(brand), "apple") {
			m := GPUMetrics{Available: true, Name: "Apple GPU"}
			if out2, err := cmdOutput(2*time.Second, "sysctl", "-n", "hw.model"); err == nil {
				m.Name = "Apple GPU (" + strings.TrimSpace(string(out2)) + ")"
			}
			gpus = append(gpus, m)
		}
	}

	return gpus
}

func parseVRAMString(s string) uint64 {
	re := regexp.MustCompile(`(\d+)\s*(MB|GB|TB)`)
	match := re.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(s)))
	if len(match) < 3 {
		return 0
	}
	val, err := strconv.ParseUint(match[1], 10, 64)
	if err != nil {
		return 0
	}
	switch match[2] {
	case "TB":
		return val * 1024 * 1024 * 1024 * 1024
	case "GB":
		return val * 1024 * 1024 * 1024
	case "MB":
		return val * 1024 * 1024
	}
	return 0
}

// ─────────────────────────────────────────────
// macOS Power via pmset (+ optional powermetrics for live watts)
// ─────────────────────────────────────────────

func collectPowerPlatform() PowerMetrics {
	m := PowerMetrics{}

	out, err := cmdOutput(2*time.Second, "pmset", "-g", "batt")
	if err != nil {
		return m
	}

	output := string(out)
	m.Available = true

	if strings.Contains(output, "AC Power") {
		m.OnAC = true
	}

	if match := regexp.MustCompile(`(\d+)%`).FindStringSubmatch(output); len(match) > 1 {
		if v, err := strconv.ParseFloat(match[1], 64); err == nil {
			m.BatteryPct = v
		}
	}

	switch {
	case strings.Contains(output, "charging") && !strings.Contains(output, "not charging"):
		m.Status = "Charging"
	case strings.Contains(output, "discharging"):
		m.Status = "Discharging"
	case strings.Contains(output, "charged") || strings.Contains(output, "finishing charge"):
		m.Status = "Full"
	case strings.Contains(output, "not charging"):
		m.Status = "Not Charging"
		m.OnAC = true
	case m.OnAC:
		m.Status = "AC Power"
	default:
		m.Status = "Unknown"
	}

	if match := regexp.MustCompile(`(\d+:\d+)\s+remaining`).FindStringSubmatch(output); len(match) > 1 {
		m.TimeRemain = match[1]
	}

	// Live system power draw (watts), if powermetrics is available.
	if p := powermetricsSample().sysPower; p > 0 {
		m.PowerRate = p
	}

	return m
}

// ─────────────────────────────────────────────
// macOS Process GPU Collection
// ─────────────────────────────────────────────

func collectProcessGPU() map[int32]uint64 {
	// macOS does not expose per-process GPU memory via standard CLI tools
	// without root/private frameworks.
	return make(map[int32]uint64)
}

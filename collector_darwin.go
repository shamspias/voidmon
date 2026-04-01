//go:build darwin

package main

import (
	"encoding/json"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// ─────────────────────────────────────────────
// macOS CPU Temperature
// ─────────────────────────────────────────────

func readCPUTemp() float64 {
	// Method 1: osx-cpu-temp (if installed via brew)
	if out, err := exec.Command("osx-cpu-temp", "-c").Output(); err == nil {
		s := strings.TrimSpace(string(out))
		s = strings.TrimSuffix(s, "°C")
		s = strings.TrimSpace(s)
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			return v
		}
	}

	// Method 2: Parse from powermetrics (requires sudo, best-effort)
	if out, err := exec.Command("sudo", "-n", "powermetrics",
		"--samplers", "smc", "-i", "500", "-n", "1").Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "cpu die temperature") ||
				strings.Contains(lower, "cpu proximity") {
				re := regexp.MustCompile(`([\d.]+)\s*°?C`)
				if m := re.FindStringSubmatch(line); len(m) > 1 {
					if v, err := strconv.ParseFloat(m[1], 64); err == nil {
						return v
					}
				}
			}
		}
	}

	// Method 3: istats gem (if installed)
	if out, err := exec.Command("istats", "cpu", "temp", "--value-only").Output(); err == nil {
		s := strings.TrimSpace(string(out))
		s = strings.TrimSuffix(s, "°C")
		if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return v
		}
	}

	return 0
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

func collectGPUPlatform() []GPUMetrics {
	var gpus []GPUMetrics

	// ── Step 1: Get GPU identity from system_profiler ──
	out, err := exec.Command("system_profiler", "SPDisplaysDataType", "-json").Output()
	if err != nil {
		return collectGPUFallback()
	}

	var data spDisplayData
	if err := json.Unmarshal(out, &data); err != nil || len(data.SPDisplaysDataType) == 0 {
		return collectGPUFallback()
	}

	for _, gpu := range data.SPDisplaysDataType {
		m := GPUMetrics{Available: true}

		// Determine GPU name
		if gpu.ChipType != "" {
			m.Name = gpu.ChipType
		} else if gpu.Name != "" {
			m.Name = gpu.Name
		} else {
			m.Name = "Unknown GPU"
		}

		// Parse VRAM
		vramStr := gpu.VRAM
		if vramStr == "" {
			vramStr = gpu.VRAMShared
		}
		if vramStr != "" {
			m.MemTotal = parseVRAMString(vramStr)
		}

		// Metal support as driver version
		if gpu.MetalFamily != "" {
			m.DriverVer = gpu.MetalFamily
		}

		gpus = append(gpus, m)
	}

	// ── Step 2: Try to get live utilization ──
	if isAppleSilicon() && len(gpus) > 0 {
		// powermetrics generally reports aggregate for the SoC
		gpus[0] = collectAppleSiliconGPUMetrics(gpus[0])
	} else {
		// Intel Mac with discrete GPU(s) — try ioreg
		for i := range gpus {
			gpus[i] = collectIntelMacGPUMetrics(gpus[i])
		}
	}

	return gpus
}

func isAppleSilicon() bool {
	out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output()
	if err != nil {
		// Fallback: check arch
		out2, err2 := exec.Command("uname", "-m").Output()
		if err2 == nil && strings.TrimSpace(string(out2)) == "arm64" {
			return true
		}
		return false
	}
	brand := strings.ToLower(strings.TrimSpace(string(out)))
	return strings.Contains(brand, "apple")
}

func collectAppleSiliconGPUMetrics(m GPUMetrics) GPUMetrics {
	// powermetrics gives GPU usage on Apple Silicon (requires sudo)
	out, err := exec.Command("sudo", "-n", "powermetrics",
		"--samplers", "gpu_power", "-i", "500", "-n", "1").Output()
	if err != nil {
		return m
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		lower := strings.ToLower(line)

		// GPU active residency = utilization
		if strings.Contains(lower, "gpu active residency") {
			re := regexp.MustCompile(`([\d.]+)\s*%`)
			if match := re.FindStringSubmatch(line); len(match) > 1 {
				if v, err := strconv.ParseFloat(match[1], 64); err == nil {
					m.Utilization = v
				}
			}
		}

		// GPU power
		if strings.Contains(lower, "gpu power") && strings.Contains(lower, "mw") {
			re := regexp.MustCompile(`([\d.]+)\s*mW`)
			if match := re.FindStringSubmatch(line); len(match) > 1 {
				if v, err := strconv.ParseFloat(match[1], 64); err == nil {
					m.PowerDraw = v / 1000.0 // mW to W
				}
			}
		}

		// GPU frequency
		if strings.Contains(lower, "gpu hw active frequency") || strings.Contains(lower, "gpu active frequency") {
			re := regexp.MustCompile(`([\d.]+)\s*mhz`)
			if match := re.FindStringSubmatch(strings.ToLower(line)); len(match) > 1 {
				// Store frequency info as fan speed field (reuse, no fan on MacBook)
				if v, err := strconv.ParseFloat(match[1], 64); err == nil {
					_ = v // Could display this somewhere
				}
			}
		}
	}

	return m
}

func collectIntelMacGPUMetrics(m GPUMetrics) GPUMetrics {
	// For Intel Macs with AMD discrete GPU, try ioreg
	out, err := exec.Command("ioreg", "-rc", "IOAccelerator").Output()
	if err != nil {
		return m
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		// Look for GPU utilization percentage
		if strings.Contains(line, "PerformanceStatistics") ||
			strings.Contains(line, "GPU Core Utilization") ||
			strings.Contains(line, "Device Utilization") {
			re := regexp.MustCompile(`"?(?:GPU Core Utilization|Device Utilization %)"?\s*=\s*(\d+)`)
			if match := re.FindStringSubmatch(line); len(match) > 1 {
				if v, err := strconv.ParseFloat(match[1], 64); err == nil {
					m.Utilization = v
				}
			}
		}

		// VRAM usage
		if strings.Contains(line, "VRAM,totalMB") {
			re := regexp.MustCompile(`=\s*(\d+)`)
			if match := re.FindStringSubmatch(line); len(match) > 1 {
				if v, err := strconv.ParseUint(match[1], 10, 64); err == nil {
					m.MemTotal = v * 1024 * 1024
				}
			}
		}
		if strings.Contains(line, "VRAM,usedMB") || strings.Contains(line, "vramUsedBytes") {
			re := regexp.MustCompile(`=\s*(\d+)`)
			if match := re.FindStringSubmatch(line); len(match) > 1 {
				if v, err := strconv.ParseUint(match[1], 10, 64); err == nil {
					if v < 1024*1024 {
						m.MemUsed = v * 1024 * 1024 // MB
					} else {
						m.MemUsed = v // already bytes
					}
				}
			}
		}
	}

	if m.MemTotal > 0 && m.MemUsed > 0 {
		m.MemPercent = float64(m.MemUsed) / float64(m.MemTotal) * 100
	}

	// Temperature from ioreg
	out2, err := exec.Command("ioreg", "-rc", "AppleSMC").Output()
	if err == nil {
		// Best-effort GPU temp from SMC
		_ = out2
	}

	return m
}

func collectGPUFallback() []GPUMetrics {
	var gpus []GPUMetrics
	m := GPUMetrics{}

	// Simple fallback: just get the chip name from sysctl on Apple Silicon
	out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output()
	if err == nil {
		brand := strings.TrimSpace(string(out))
		if strings.Contains(strings.ToLower(brand), "apple") {
			m.Available = true
			// Derive GPU name from chip
			if out2, err := exec.Command("sysctl", "-n", "hw.model").Output(); err == nil {
				m.Name = "Apple GPU (" + strings.TrimSpace(string(out2)) + ")"
			} else {
				m.Name = "Apple GPU"
			}
			gpus = append(gpus, m)
		}
	}

	return gpus
}

func parseVRAMString(s string) uint64 {
	s = strings.TrimSpace(s)
	re := regexp.MustCompile(`(\d+)\s*(MB|GB|TB)`)
	match := re.FindStringSubmatch(strings.ToUpper(s))
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
// macOS Power via pmset
// ─────────────────────────────────────────────

func collectPowerPlatform() PowerMetrics {
	m := PowerMetrics{}

	out, err := exec.Command("pmset", "-g", "batt").Output()
	if err != nil {
		return m
	}

	output := string(out)
	m.Available = true

	// Check AC or battery
	if strings.Contains(output, "AC Power") {
		m.OnAC = true
	}

	// Parse battery percentage
	re := regexp.MustCompile(`(\d+)%`)
	if match := re.FindStringSubmatch(output); len(match) > 1 {
		if v, err := strconv.ParseFloat(match[1], 64); err == nil {
			m.BatteryPct = v
		}
	}

	// Parse status
	if strings.Contains(output, "charging") && !strings.Contains(output, "not charging") {
		m.Status = "Charging"
	} else if strings.Contains(output, "discharging") {
		m.Status = "Discharging"
	} else if strings.Contains(output, "charged") || strings.Contains(output, "finishing charge") {
		m.Status = "Full"
	} else if strings.Contains(output, "not charging") {
		m.Status = "Not Charging"
		m.OnAC = true
	} else if m.OnAC {
		m.Status = "AC Power"
	} else {
		m.Status = "Unknown"
	}

	// Time remaining
	reTime := regexp.MustCompile(`(\d+:\d+)\s+remaining`)
	if match := reTime.FindStringSubmatch(output); len(match) > 1 {
		m.TimeRemain = match[1]
	}

	// Power draw via powermetrics (best effort)
	if out2, err := exec.Command("sudo", "-n", "powermetrics",
		"--samplers", "battery", "-i", "500", "-n", "1").Output(); err == nil {
		lines := strings.Split(string(out2), "\n")
		for _, line := range lines {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "combined power") || strings.Contains(lower, "system power") {
				re := regexp.MustCompile(`([\d.]+)\s*(?:mW|W)`)
				if match := re.FindStringSubmatch(line); len(match) > 1 {
					if v, err := strconv.ParseFloat(match[1], 64); err == nil {
						if strings.Contains(lower, "mw") {
							m.PowerRate = v / 1000.0
						} else {
							m.PowerRate = v
						}
					}
				}
			}
		}
	}

	return m
}

// ─────────────────────────────────────────────
// macOS Process GPU Collection
// ─────────────────────────────────────────────

func collectProcessGPU() map[int32]uint64 {
	// macOS does not easily expose per-process GPU metrics via standard CLI tools
	// without root/private frameworks, so we return an empty map.
	return make(map[int32]uint64)
}

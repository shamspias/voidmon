//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ─────────────────────────────────────────────
// Linux CPU Temperature via sysfs / hwmon
// ─────────────────────────────────────────────

func readCPUTemp() float64 {
	// Try thermal zones first.
	paths := []string{
		"/sys/class/thermal/thermal_zone0/temp",
		"/sys/class/thermal/thermal_zone1/temp",
	}
	for _, p := range paths {
		if data, err := os.ReadFile(p); err == nil {
			if val, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
				return val / 1000.0
			}
		}
	}

	// Fall back to hwmon.
	matches, _ := filepath.Glob("/sys/class/hwmon/hwmon*/temp1_input")
	for _, p := range matches {
		if data, err := os.ReadFile(p); err == nil {
			if val, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
				return val / 1000.0
			}
		}
	}
	return 0
}

// ─────────────────────────────────────────────
// Linux GPU: NVIDIA (shared) + AMD (sysfs) + Intel (sysfs)
// ─────────────────────────────────────────────

func collectGPUPlatform() []GPUMetrics {
	var gpus []GPUMetrics

	gpus = append(gpus, collectNvidiaGPUs()...)
	gpus = append(gpus, collectAMDGPUs()...)
	if intel := collectIntelGPU(); intel.Available {
		gpus = append(gpus, intel)
	}

	return gpus
}

func collectAMDGPUs() []GPUMetrics {
	var gpus []GPUMetrics

	matches, _ := filepath.Glob("/sys/class/drm/card*/device/gpu_busy_percent")
	seen := make(map[string]bool)

	for _, match := range matches {
		// match = /sys/class/drm/cardN/device/gpu_busy_percent
		// devDir = /sys/class/drm/cardN/device  (all attrs are siblings here)
		devDir := filepath.Dir(match)

		// De-dupe: resolve to the real PCI device path so a GPU exposed by
		// multiple DRM nodes is only counted once. Fall back to the unresolved
		// path if the symlink can't be resolved (container / permissions), so
		// de-duping is never silently skipped.
		key := devDir
		if real, err := filepath.EvalSymlinks(devDir); err == nil {
			key = real
		}
		if seen[key] {
			continue
		}
		seen[key] = true

		m := GPUMetrics{Available: true, Name: "AMD GPU"}

		if nameData, err := os.ReadFile(filepath.Join(devDir, "product_name")); err == nil {
			if n := strings.TrimSpace(string(nameData)); n != "" {
				m.Name = n
			}
		}

		// Utilization (the file we globbed on).
		if data, err := os.ReadFile(match); err == nil {
			if v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
				m.Utilization = v
			}
		}

		// Temperature.
		tempPaths, _ := filepath.Glob(filepath.Join(devDir, "hwmon", "hwmon*", "temp1_input"))
		for _, p := range tempPaths {
			if data, err := os.ReadFile(p); err == nil {
				if v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
					m.Temperature = v / 1000.0
					break
				}
			}
		}

		// VRAM.
		if data, err := os.ReadFile(filepath.Join(devDir, "mem_info_vram_total")); err == nil {
			if v, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); err == nil {
				m.MemTotal = v
			}
		}
		if data, err := os.ReadFile(filepath.Join(devDir, "mem_info_vram_used")); err == nil {
			if v, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); err == nil {
				m.MemUsed = v
			}
		}
		if m.MemTotal > 0 && m.MemUsed > 0 {
			m.MemPercent = float64(m.MemUsed) / float64(m.MemTotal) * 100
		}

		// Fan speed (PWM 0-255 -> percent).
		fanPaths, _ := filepath.Glob(filepath.Join(devDir, "hwmon", "hwmon*", "pwm1"))
		for _, p := range fanPaths {
			if data, err := os.ReadFile(p); err == nil {
				if v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
					m.FanSpeed = v / 255.0 * 100
					break
				}
			}
		}

		// Power draw (µW -> W).
		powerPaths, _ := filepath.Glob(filepath.Join(devDir, "hwmon", "hwmon*", "power1_average"))
		for _, p := range powerPaths {
			if data, err := os.ReadFile(p); err == nil {
				if v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
					m.PowerDraw = v / 1000000.0
					break
				}
			}
		}

		gpus = append(gpus, m)
	}

	return gpus
}

func collectIntelGPU() GPUMetrics {
	m := GPUMetrics{}

	// Check for an Intel i915 GPU.
	if _, err := os.Stat("/sys/class/drm/card0/gt/gt0"); err != nil {
		// Fallback: is the i915 module loaded?
		if data, err := os.ReadFile("/proc/modules"); err == nil {
			if !strings.Contains(string(data), "i915") {
				return m
			}
		} else {
			return m
		}
	}

	m.Available = true
	m.Name = "Intel Integrated GPU"

	// Best-effort name from lspci.
	if out, err := cmdOutput(2*time.Second, "lspci"); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, "VGA") && strings.Contains(strings.ToLower(line), "intel") {
				if parts := strings.SplitN(line, ": ", 2); len(parts) > 1 {
					m.Name = strings.TrimSpace(parts[1])
				}
				break
			}
		}
	}

	// Use the current/max frequency ratio as a rough utilization indicator.
	if data, err := os.ReadFile("/sys/class/drm/card0/gt_cur_freq_mhz"); err == nil {
		curFreq, _ := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
		if maxData, err := os.ReadFile("/sys/class/drm/card0/gt_max_freq_mhz"); err == nil {
			maxFreq, _ := strconv.ParseFloat(strings.TrimSpace(string(maxData)), 64)
			if maxFreq > 0 {
				m.Utilization = (curFreq / maxFreq) * 100
			}
		}
	}

	return m
}

// ─────────────────────────────────────────────
// Linux Power via sysfs
// ─────────────────────────────────────────────

func collectPowerPlatform() PowerMetrics {
	m := PowerMetrics{}

	// Find a battery.
	batPath := "/sys/class/power_supply/BAT0"
	if _, err := os.Stat(batPath); os.IsNotExist(err) {
		batPath = "/sys/class/power_supply/BAT1"
		if _, err := os.Stat(batPath); os.IsNotExist(err) {
			// No battery — check for an AC adapter (desktop / server).
			acPaths := []string{
				"/sys/class/power_supply/AC",
				"/sys/class/power_supply/ADP0",
				"/sys/class/power_supply/ADP1",
				"/sys/class/power_supply/ACAD",
			}
			for _, p := range acPaths {
				if data, err := os.ReadFile(filepath.Join(p, "online")); err == nil {
					m.Available = true
					m.OnAC = strings.TrimSpace(string(data)) == "1"
					m.Status = "AC Power"
					return m
				}
			}
			return m
		}
	}

	m.Available = true

	if data, err := os.ReadFile(filepath.Join(batPath, "capacity")); err == nil {
		if v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
			m.BatteryPct = v
		}
	}

	if data, err := os.ReadFile(filepath.Join(batPath, "status")); err == nil {
		m.Status = strings.TrimSpace(string(data))
		m.OnAC = m.Status == "Charging" || m.Status == "Full" || m.Status == "Not charging"
	}

	if data, err := os.ReadFile(filepath.Join(batPath, "power_now")); err == nil {
		if v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
			m.PowerRate = v / 1000000.0 // µW -> W
		}
	} else if data, err := os.ReadFile(filepath.Join(batPath, "current_now")); err == nil {
		if current, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
			if vData, err := os.ReadFile(filepath.Join(batPath, "voltage_now")); err == nil {
				if voltage, err := strconv.ParseFloat(strings.TrimSpace(string(vData)), 64); err == nil {
					m.PowerRate = (current * voltage) / 1e12
				}
			}
		}
	}

	return m
}

// ─────────────────────────────────────────────
// Linux Process GPU Collection (NVIDIA, shared parser)
// ─────────────────────────────────────────────

func collectProcessGPU() map[int32]uint64 {
	return nvidiaProcessGPU()
}

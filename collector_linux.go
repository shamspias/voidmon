//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ─────────────────────────────────────────────
// Linux CPU Temperature via sysfs / hwmon
// ─────────────────────────────────────────────

func readCPUTemp() float64 {
	// Try thermal zones first
	paths := []string{
		"/sys/class/thermal/thermal_zone0/temp",
		"/sys/class/thermal/thermal_zone1/temp",
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err == nil {
			val, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
			if err == nil {
				return val / 1000.0
			}
		}
	}

	// Try hwmon
	matches, _ := filepath.Glob("/sys/class/hwmon/hwmon*/temp1_input")
	for _, p := range matches {
		data, err := os.ReadFile(p)
		if err == nil {
			val, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
			if err == nil {
				return val / 1000.0
			}
		}
	}
	return 0
}

// ─────────────────────────────────────────────
// Linux GPU: NVIDIA (nvidia-smi) + AMD (sysfs) + Intel (sysfs)
// ─────────────────────────────────────────────

func collectGPUPlatform() []GPUMetrics {
	var gpus []GPUMetrics

	// Collect NVIDIA
	gpus = append(gpus, collectNvidiaGPUs()...)

	// Collect AMD
	gpus = append(gpus, collectAMDGPUs()...)

	// Collect Intel integrated
	if intel := collectIntelGPU(); intel.Available {
		gpus = append(gpus, intel)
	}

	return gpus
}

func collectNvidiaGPUs() []GPUMetrics {
	var gpus []GPUMetrics

	out, err := exec.Command("nvidia-smi",
		"--query-gpu=name,memory.total,memory.used,utilization.gpu,temperature.gpu,fan.speed,power.draw,power.limit,driver_version",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return gpus
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		m := GPUMetrics{Available: true}
		parts := strings.Split(line, ", ")
		if len(parts) >= 9 {
			m.Name = strings.TrimSpace(parts[0])
			if v, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64); err == nil {
				m.MemTotal = v * 1024 * 1024
			}
			if v, err := strconv.ParseUint(strings.TrimSpace(parts[2]), 10, 64); err == nil {
				m.MemUsed = v * 1024 * 1024
			}
			if m.MemTotal > 0 {
				m.MemPercent = float64(m.MemUsed) / float64(m.MemTotal) * 100
			}
			if v, err := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64); err == nil {
				m.Utilization = v
			}
			if v, err := strconv.ParseFloat(strings.TrimSpace(parts[4]), 64); err == nil {
				m.Temperature = v
			}
			if v, err := strconv.ParseFloat(strings.TrimSpace(parts[5]), 64); err == nil {
				m.FanSpeed = v
			}
			if v, err := strconv.ParseFloat(strings.TrimSpace(parts[6]), 64); err == nil {
				m.PowerDraw = v
			}
			if v, err := strconv.ParseFloat(strings.TrimSpace(parts[7]), 64); err == nil {
				m.PowerLimit = v
			}
			m.DriverVer = strings.TrimSpace(parts[8])
		}
		gpus = append(gpus, m)
	}

	return gpus
}

func collectAMDGPUs() []GPUMetrics {
	var gpus []GPUMetrics

	matches, _ := filepath.Glob("/sys/class/drm/card*/device/gpu_busy_percent")
	for _, match := range matches {
		m := GPUMetrics{Available: true, Name: "AMD GPU"}

		// Try to get a better name from the device
		devDir := filepath.Dir(filepath.Dir(match))
		if nameData, err := os.ReadFile(filepath.Join(devDir, "device", "product_name")); err == nil {
			m.Name = strings.TrimSpace(string(nameData))
		}

		// Utilization
		if data, err := os.ReadFile(match); err == nil {
			if v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
				m.Utilization = v
			}
		}

		// Temperature
		tempPaths, _ := filepath.Glob(filepath.Join(devDir, "device", "hwmon", "hwmon*", "temp1_input"))
		for _, p := range tempPaths {
			if data, err := os.ReadFile(p); err == nil {
				if v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
					m.Temperature = v / 1000.0
					break
				}
			}
		}

		// VRAM total
		if data, err := os.ReadFile(filepath.Join(devDir, "device", "mem_info_vram_total")); err == nil {
			if v, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); err == nil {
				m.MemTotal = v
			}
		}

		// VRAM used
		if data, err := os.ReadFile(filepath.Join(devDir, "device", "mem_info_vram_used")); err == nil {
			if v, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); err == nil {
				m.MemUsed = v
			}
		}

		if m.MemTotal > 0 && m.MemUsed > 0 {
			m.MemPercent = float64(m.MemUsed) / float64(m.MemTotal) * 100
		}

		// Fan speed
		fanPaths, _ := filepath.Glob(filepath.Join(devDir, "device", "hwmon", "hwmon*", "pwm1"))
		for _, p := range fanPaths {
			if data, err := os.ReadFile(p); err == nil {
				if v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
					m.FanSpeed = v / 255.0 * 100 // PWM 0-255 to percent
					break
				}
			}
		}

		// Power draw (microwatts)
		powerPaths, _ := filepath.Glob(filepath.Join(devDir, "device", "hwmon", "hwmon*", "power1_average"))
		for _, p := range powerPaths {
			if data, err := os.ReadFile(p); err == nil {
				if v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
					m.PowerDraw = v / 1000000.0 // µW to W
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

	// Check for Intel i915 GPU
	_, err := os.Stat("/sys/class/drm/card0/gt/gt0")
	if err != nil {
		// Fallback: check if i915 module is loaded
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

	// Try to get GPU name from lspci (best effort)
	if out, err := exec.Command("lspci").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, "VGA") && strings.Contains(strings.ToLower(line), "intel") {
				parts := strings.SplitN(line, ": ", 2)
				if len(parts) > 1 {
					m.Name = strings.TrimSpace(parts[1])
				}
				break
			}
		}
	}

	// Intel GPU frequency as a rough utilization indicator
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

	// Find battery
	batPath := "/sys/class/power_supply/BAT0"
	if _, err := os.Stat(batPath); os.IsNotExist(err) {
		batPath = "/sys/class/power_supply/BAT1"
		if _, err := os.Stat(batPath); os.IsNotExist(err) {
			// No battery — check AC
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

	// Read capacity
	if data, err := os.ReadFile(filepath.Join(batPath, "capacity")); err == nil {
		if v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
			m.BatteryPct = v
		}
	}

	// Read status
	if data, err := os.ReadFile(filepath.Join(batPath, "status")); err == nil {
		m.Status = strings.TrimSpace(string(data))
		m.OnAC = m.Status == "Charging" || m.Status == "Full" || m.Status == "Not charging"
	}

	// Read power draw
	if data, err := os.ReadFile(filepath.Join(batPath, "power_now")); err == nil {
		if v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
			m.PowerRate = v / 1000000.0 // µW to W
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
// Linux Process GPU Collection (NVIDIA)
// ─────────────────────────────────────────────

func collectProcessGPU() map[int32]uint64 {
	m := make(map[int32]uint64)

	// Fast lookup of used memory by PID via nvidia-smi
	out, err := exec.Command("nvidia-smi", "--query-compute-apps=pid,used_memory", "--format=csv,noheader,nounits").Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			parts := strings.Split(line, ", ")
			if len(parts) == 2 {
				if pid, err := strconv.ParseInt(parts[0], 10, 32); err == nil {
					if memMiB, err := strconv.ParseUint(parts[1], 10, 64); err == nil {
						m[int32(pid)] = memMiB * 1024 * 1024 // MiB to Bytes
					}
				}
			}
		}
	}

	return m
}

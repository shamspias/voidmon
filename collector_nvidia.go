package main

import (
	"strconv"
	"strings"
	"time"
)

// NVIDIA collection via `nvidia-smi`. The tool ships an identical CLI on Linux
// and Windows, so both platforms share this one parser. (On macOS nvidia-smi
// is absent and these simply return nothing.)

func collectNvidiaGPUs() []GPUMetrics {
	var gpus []GPUMetrics

	out, err := cmdOutput(2*time.Second, "nvidia-smi",
		"--query-gpu=name,memory.total,memory.used,utilization.gpu,temperature.gpu,fan.speed,power.draw,power.limit,driver_version",
		"--format=csv,noheader,nounits")
	if err != nil {
		return gpus
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}

		m := GPUMetrics{Available: true}
		parts := strings.Split(line, ", ")
		if len(parts) < 9 {
			continue
		}

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

		gpus = append(gpus, m)
	}

	return gpus
}

// nvidiaProcessGPU maps PID -> GPU memory (bytes) for compute processes.
func nvidiaProcessGPU() map[int32]uint64 {
	m := make(map[int32]uint64)

	out, err := cmdOutput(2*time.Second, "nvidia-smi",
		"--query-compute-apps=pid,used_memory", "--format=csv,noheader,nounits")
	if err != nil {
		return m
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, ", ")
		if len(parts) != 2 {
			continue
		}
		if pid, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 32); err == nil {
			if memMiB, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64); err == nil {
				m[int32(pid)] = memMiB * 1024 * 1024 // MiB -> bytes
			}
		}
	}

	return m
}

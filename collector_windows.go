//go:build windows

package main

import (
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"github.com/yusufpapurcu/wmi"
	"golang.org/x/sys/windows"
)

// Windows platform collectors. The shared collector (cpu/mem/disk/io/net/
// process/host) already works via gopsutil's *_windows.go files, so this file
// only supplies the four platform hooks. It adds NO new module: both
// golang.org/x/sys and github.com/yusufpapurcu/wmi already ship as transitive
// deps of gopsutil — `go mod tidy` just promotes them to direct.

// ─────────────────────────────────────────────
// CPU Temperature via WMI (best-effort)
//
// MSAcpi_ThermalZoneTemperature lives in root\wmi and reports tenths of Kelvin.
// Many desktops/VMs return "not supported" — we cache that negative so we don't
// re-issue a failing WMI query on every refresh.
// ─────────────────────────────────────────────

type msAcpiThermalZoneTemperature struct {
	CurrentTemperature uint32
}

var (
	thermalMu          sync.Mutex
	thermalUnsupported bool
)

func readCPUTemp() float64 {
	thermalMu.Lock()
	defer thermalMu.Unlock()
	if thermalUnsupported {
		return 0
	}

	var dst []msAcpiThermalZoneTemperature
	if err := wmi.QueryNamespace(
		"SELECT CurrentTemperature FROM MSAcpi_ThermalZoneTemperature",
		&dst, "root/wmi"); err != nil {
		return 0 // transient WMI/COM error — retry next tick, don't latch
	}

	for _, z := range dst {
		if z.CurrentTemperature > 0 {
			c := float64(z.CurrentTemperature)/10.0 - 273.15
			if c > 0 && c < 150 {
				return c
			}
		}
	}
	// Query succeeded but reported no usable reading: genuinely unsupported
	// (most desktops/VMs). Cache the negative to stop polling a dead class.
	thermalUnsupported = true
	return 0
}

// ─────────────────────────────────────────────
// GPU Collection: NVIDIA (shared parser) + WMI name/driver fallback
// ─────────────────────────────────────────────

type win32VideoController struct {
	Name          string
	DriverVersion string
}

var (
	wmiGPUMu   sync.Mutex
	wmiGPU     []GPUMetrics
	wmiGPUDone bool
)

func collectGPUPlatform() []GPUMetrics {
	// nvidia-smi is identical on Windows and gives live utilization/VRAM/power.
	if gpus := collectNvidiaGPUs(); len(gpus) > 0 {
		return gpus
	}
	// Otherwise fall back to the adapter name/driver via WMI (static — cached).
	return wmiVideoControllers()
}

func wmiVideoControllers() []GPUMetrics {
	wmiGPUMu.Lock()
	defer wmiGPUMu.Unlock()
	if wmiGPUDone {
		return wmiGPU
	}

	var dst []win32VideoController
	// Note: Win32_VideoController.AdapterRAM is a uint32 capped at 4GB by a
	// well-known WMI limitation, so we deliberately do NOT report VRAM here.
	if err := wmi.Query("SELECT Name, DriverVersion FROM Win32_VideoController", &dst); err != nil {
		return nil // transient WMI/COM error — retry next tick, don't latch
	}

	var gpus []GPUMetrics
	for _, v := range dst {
		name := strings.TrimSpace(v.Name)
		if name == "" {
			continue
		}
		gpus = append(gpus, GPUMetrics{
			Available: true,
			Name:      name,
			DriverVer: strings.TrimSpace(v.DriverVersion),
		})
	}
	wmiGPU = gpus
	wmiGPUDone = true
	return wmiGPU
}

// ─────────────────────────────────────────────
// Per-process GPU memory (NVIDIA, shared parser)
// ─────────────────────────────────────────────

func collectProcessGPU() map[int32]uint64 {
	return nvidiaProcessGPU()
}

// ─────────────────────────────────────────────
// Power / Battery via GetSystemPowerStatus (kernel32 syscall — no admin, instant)
// ─────────────────────────────────────────────

type systemPowerStatus struct {
	ACLineStatus        byte   // 0 offline, 1 online, 255 unknown
	BatteryFlag         byte   // 1 high, 2 low, 4 critical, 8 charging, 128 no battery, 255 unknown
	BatteryLifePercent  byte   // 0..100, 255 unknown
	SystemStatusFlag    byte   // battery saver on/off
	BatteryLifeTime     uint32 // seconds remaining, 0xFFFFFFFF unknown
	BatteryFullLifeTime uint32
}

var (
	modkernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procGetSystemPowerStatus = modkernel32.NewProc("GetSystemPowerStatus")
)

func collectPowerPlatform() PowerMetrics {
	var s systemPowerStatus
	r, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&s)))
	if r == 0 {
		return PowerMetrics{} // call failed
	}

	m := PowerMetrics{Available: true}
	m.OnAC = s.ACLineStatus == 1

	// No system battery — desktop / server. 255 is the "unknown" sentinel, so
	// exclude it: a momentarily-unknown laptop (sleep/resume, driver reload)
	// must not be reported as a battery-less desktop.
	if s.BatteryFlag != 255 && s.BatteryFlag&128 != 0 {
		m.OnAC = true
		m.Status = "AC Power"
		return m
	}

	if s.BatteryLifePercent != 255 {
		m.BatteryPct = float64(s.BatteryLifePercent)
	}

	switch {
	case s.BatteryFlag&8 != 0:
		m.Status = "Charging"
	case m.OnAC:
		m.Status = "AC Power"
	default:
		m.Status = "Discharging"
	}

	if !m.OnAC && s.BatteryLifeTime != 0xFFFFFFFF {
		h := s.BatteryLifeTime / 3600
		mm := (s.BatteryLifeTime % 3600) / 60
		m.TimeRemain = fmt.Sprintf("%d:%02d", h, mm)
	}

	// GetSystemPowerStatus does not expose instantaneous wattage — leave 0.
	return m
}

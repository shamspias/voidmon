<div align="center">

```
░▒▓█  V O I D M O N  █▓▒░
```

**A sleek, hacker-aesthetic terminal system monitor written in Go.**

Fast. Minimal. Beautiful. Now cross-platform — and scriptable.

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey)]()

</div>

---

## Screenshots

<div align="center">

| Linux                                      | macOS                                      |
|--------------------------------------------|--------------------------------------------|
| ![Linux Screenshot](screenshots/linux.png) | ![macOS Screenshot](screenshots/macos.png) |

</div>

---

## Features

- **CPU** — Per-core usage bars (compact one-glyph-per-core strip for high core counts), temperature, load averages, live history sparkline
- **Memory** — RAM & SWAP usage with detailed breakdown + history sparkline
- **Disk** — All mounted filesystems with usage bars
- **I/O** — Real-time disk read/write speeds, IOPS, and an activity sparkline
- **Network** — Send/receive throughput with per-direction sparklines
- **GPU** — Cross-platform:
    - NVIDIA (via `nvidia-smi`) — Linux **and** Windows
    - AMD (via sysfs / ROCm) — Linux
    - Intel Integrated (via sysfs / i915) — Linux
    - Apple Silicon M1/M2/M3/M4 (via `system_profiler` + `powermetrics`)
    - Intel Mac with discrete GPU (via `ioreg`)
    - Windows AMD/Intel — name & driver via WMI
- **Power** — Battery status, AC detection, power draw (Linux sysfs, macOS `pmset`, Windows `GetSystemPowerStatus`)
- **Processes** — Interactive table: **correct instantaneous CPU%**, RAM, GPU memory, listening ports, status, user

### What makes it different

Most lightweight terminal monitors are *display-only*. voidmon adds three things they don't, while staying a single small binary with no extra runtime dependencies:

- 📈 **Live history sparklines** — CPU, memory, network and I/O are drawn as rolling time-series graphs using pure Unicode block glyphs (no fonts, no deps).
- 🤖 **Headless / scriptable mode** — `void --json` prints a full machine snapshot and exits; `void --json --watch` streams **NDJSON** (one object per refresh). voidmon doubles as a tiny, dependency-free metrics exporter you can pipe into anything.
- 🎮 **A genuinely interactive process panel** — sort, filter, scroll, and kill processes, with a correct *instantaneous* CPU% (not the misleading since-boot average most quick monitors show).

Plus selectable **themes**, an optional **config file**, and edge-triggered **alerts**.

---

## Install

### Linux & macOS (one-liner)

```bash
curl -fsSL https://raw.githubusercontent.com/shamspias/voidmon/main/install.sh | bash
```

### Windows (PowerShell one-liner)

```powershell
irm https://raw.githubusercontent.com/shamspias/voidmon/main/install.ps1 | iex
```

Installs `void.exe` to `%LOCALAPPDATA%\voidmon` and adds it to your user `PATH`.

### Go install (requires Go 1.26+)

```bash
go install github.com/shamspias/voidmon@latest
```

> The binary installs as `voidmon`. To use the `void` command, create an alias:
> ```bash
> echo 'alias void="voidmon"' >> ~/.bashrc  # or ~/.zshrc
> ```

### From source

```bash
git clone https://github.com/shamspias/voidmon.git
cd voidmon
make build       # -> build/void
make install     # installs to /usr/local/bin (Unix)
```

### From GitHub Releases

| Platform            | Asset                       |
|---------------------|-----------------------------|
| Linux x86_64        | `void-linux-amd64.tar.gz`   |
| Linux ARM64         | `void-linux-arm64.tar.gz`   |
| macOS Apple Silicon | `void-darwin-arm64.tar.gz`  |
| macOS Intel         | `void-darwin-amd64.tar.gz`  |
| Windows x86_64      | `void-windows-amd64.zip`    |
| Windows ARM64       | `void-windows-arm64.zip`    |

---

## Usage

```bash
void                 # launch the dashboard
void -r 1s           # custom refresh rate (min 500ms)
void --theme cyber   # pick a color theme
void --no-color      # disable colors (also honors the NO_COLOR env var)
void -icons=false    # ASCII panel titles instead of emoji
void -v              # version info
void -u              # check for updates and upgrade

# Headless / scripting
void --json          # one-shot JSON snapshot, then exit
void --json --watch  # stream NDJSON (one object per refresh)
```

### Keybindings

| Key            | Action                          |
|----------------|---------------------------------|
| `↑`/`↓`, `j`/`k` | Move selection in the process list |
| `PgUp`/`PgDn`  | Page through processes          |
| `c` / `m` / `p` / `n` | Sort by CPU / memory / PID / name |
| `/`            | Filter processes by name or port |
| `k`            | Kill the selected process (SIGTERM, with confirm) |
| `Space`        | Pause / resume                  |
| `?` / `h`      | Toggle the help overlay         |
| `q` / `Esc`    | Quit                            |

Mouse is supported — scroll and click to select in the process list.

---

## Themes

Pick with `--theme NAME` or set it in the config file:

| Theme    | Vibe                          |
|----------|-------------------------------|
| `matrix` | Classic Matrix green (default) |
| `amber`  | Vintage amber phosphor        |
| `cyber`  | Cyberpunk neon cyan / pink    |
| `ice`    | Cool blues                    |
| `mono`   | Grayscale, low distraction    |

---

## Scripting with `--json`

`void --json` reuses the exact same collector the dashboard uses, so it's a faithful snapshot:

```bash
# Grab overall CPU and memory usage
void --json | jq '{cpu: .cpu.overall, mem: .memory.used_percent}'

# Watch top processes as a live NDJSON stream
void --json --watch -r 1s | jq -c '.processes[0]'

# Log host metrics to a file every 5s
void --json --watch -r 5s >> metrics.ndjson
```

Every value is exposed under snake_case keys (`cpu`, `memory`, `disks`, `io`,
`network`, `gpus`, `processes`, `power`, `host`, `timestamp`).

---

## Configuration

Optional. voidmon reads `config` from your OS config directory
(`os.UserConfigDir()`):

- Linux: `~/.config/voidmon/config`
- macOS: `~/Library/Application Support/voidmon/config`
- Windows: `%AppData%\voidmon\config`

Simple `key = value` lines (`#` comments). Flags always override the file:

```ini
# ~/.config/voidmon/config
theme         = cyber
refresh       = 1s
process_count = 30
icons         = true
no_color      = false

# Alerts: highlight + a single terminal bell when a metric crosses the line
alerts        = true
cpu_alert     = 90
mem_alert     = 90
temp_alert    = 85
disk_alert    = 90
```

---

## Platform notes

### Windows

- **CPU temperature** is usually `0` — Windows has no reliable built-in sensor
  (voidmon queries `MSAcpi_ThermalZoneTemperature` via WMI, which most desktops
  report as "not supported").
- **NVIDIA** GPUs report full live stats if `nvidia-smi` is on your `PATH`.
  AMD/Intel GPUs show name + driver only.
- **Battery** comes from `GetSystemPowerStatus` (no admin needed); desktops show
  "AC Power".
- The TUI uses Windows' native VT/console support — no extra setup.

### macOS

For full GPU utilization / power and CPU die temperature on Apple Silicon,
voidmon uses `powermetrics`, which needs sudo:

```bash
# Run with sudo:
sudo void

# …or allow passwordless powermetrics (sudo visudo):
# yourusername ALL=(ALL) NOPASSWD: /usr/bin/powermetrics
```

Without it, voidmon still shows the GPU name and VRAM. For CPU temperature
without sudo, install `osx-cpu-temp` (`brew install osx-cpu-temp`).

### Linux

GPU/temperature/power read straight from sysfs (plus `nvidia-smi` for NVIDIA);
no sudo required.

---

## Building releases

```bash
make build-all   # all platforms into build/
make release      # tar.gz (Unix) + zip (Windows) into build/release/
make test         # run the test suite
```

---

## Contributing

1. Fork it
2. Create your branch (`git checkout -b feat/thing`)
3. Commit (`git commit -am 'add thing'`)
4. Push (`git push origin feat/thing`)
5. Open a PR

---

## License

MIT — do whatever you want.

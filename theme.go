package main

import "github.com/gdamore/tcell/v2"

// ─────────────────────────────────────────────
// Color scheme
//
// The eight palette colors are package vars set by applyTheme() so the whole
// UI can be re-skinned (or stripped for NO_COLOR) without touching render code.
// The structural tags (reset/bold) and bar glyphs are fixed.
// ─────────────────────────────────────────────

const (
	colorReset  = "[-]"
	colorBold   = "[::b]"
	colorNoBold = "[::-]"
)

// Bar glyphs.
const (
	barFull  = "█"
	barEmpty = "░"
)

// Active palette (tview color tags like "[#00ff41]"). Set by applyTheme.
var (
	colorPrimary   string
	colorSecondary string
	colorAccent    string
	colorWarning   string
	colorCritical  string
	colorDim       string
	colorMuted     string
	colorWhite     string
)

// Border/title colors for panels (tcell, set by applyTheme).
var (
	uiBorderColor = tcell.ColorDarkGreen
	uiTitleColor  = tcell.ColorGreen
)

// tcell equivalents of the palette, used where real colors (not markup tags)
// are needed — e.g. tview.Table cells, which align better without tag chars.
var (
	tcPrimary   = tcell.ColorGreen
	tcSecondary = tcell.ColorAqua
	tcAccent    = tcell.ColorFuchsia
	tcWarning   = tcell.ColorOrange
	tcCritical  = tcell.ColorRed
	tcDim       = tcell.ColorGray
	tcWhite     = tcell.ColorWhite
)

// Theme is a named palette. Values are hex strings ("#rrggbb").
type Theme struct {
	Primary, Secondary, Accent, Warning, Critical, Dim, Muted, White string
}

var themes = map[string]Theme{
	// Classic Matrix green — the default voidmon look.
	"matrix": {
		Primary: "#00ff41", Secondary: "#00d4ff", Accent: "#ff6ac1",
		Warning: "#f5a623", Critical: "#ff3333",
		Dim: "#4a5568", Muted: "#2d3748", White: "#e2e8f0",
	},
	// Amber phosphor — vintage terminal.
	"amber": {
		Primary: "#ffb000", Secondary: "#ffd479", Accent: "#ff7b00",
		Warning: "#ff8c00", Critical: "#ff3b00",
		Dim: "#7a5a1e", Muted: "#3a2a0e", White: "#ffe8b0",
	},
	// Cyberpunk — neon cyan / hot pink / yellow.
	"cyber": {
		Primary: "#00f0ff", Secondary: "#f9f871", Accent: "#ff2e97",
		Warning: "#ffb700", Critical: "#ff003c",
		Dim: "#5a4b81", Muted: "#241a3a", White: "#eae0ff",
	},
	// Ice — cool blues.
	"ice": {
		Primary: "#79c0ff", Secondary: "#a5d8ff", Accent: "#b197fc",
		Warning: "#ffd43b", Critical: "#ff6b6b",
		Dim: "#4b5563", Muted: "#1e293b", White: "#e7f5ff",
	},
	// Monochrome — grayscale, low distraction.
	"mono": {
		Primary: "#b8c0c8", Secondary: "#e8edf2", Accent: "#8a929a",
		Warning: "#cfd6dd", Critical: "#ffffff",
		Dim: "#555b62", Muted: "#2a2e33", White: "#e8edf2",
	},
}

// themeNames lists available themes (for help/usage text).
func themeNames() []string {
	return []string{"matrix", "amber", "cyber", "ice", "mono"}
}

// applyTheme installs the named palette. Unknown names fall back to matrix.
// When noColor is true all color tags are blanked and borders go neutral.
// Returns the effective theme name.
func applyTheme(name string, noColor bool) string {
	t, ok := themes[name]
	if !ok {
		name, t = "matrix", themes["matrix"]
	}

	if noColor {
		colorPrimary, colorSecondary, colorAccent = "", "", ""
		colorWarning, colorCritical = "", ""
		colorDim, colorMuted, colorWhite = "", "", ""
		uiBorderColor = tcell.ColorWhite
		uiTitleColor = tcell.ColorWhite
		tcPrimary, tcSecondary, tcAccent = tcell.ColorWhite, tcell.ColorWhite, tcell.ColorWhite
		tcWarning, tcCritical = tcell.ColorWhite, tcell.ColorWhite
		tcDim, tcWhite = tcell.ColorWhite, tcell.ColorWhite
		return name
	}

	tag := func(hex string) string { return "[" + hex + "]" }
	colorPrimary = tag(t.Primary)
	colorSecondary = tag(t.Secondary)
	colorAccent = tag(t.Accent)
	colorWarning = tag(t.Warning)
	colorCritical = tag(t.Critical)
	colorDim = tag(t.Dim)
	colorMuted = tag(t.Muted)
	colorWhite = tag(t.White)

	uiBorderColor = tcell.GetColor(t.Dim)
	uiTitleColor = tcell.GetColor(t.Primary)

	tcPrimary = tcell.GetColor(t.Primary)
	tcSecondary = tcell.GetColor(t.Secondary)
	tcAccent = tcell.GetColor(t.Accent)
	tcWarning = tcell.GetColor(t.Warning)
	tcCritical = tcell.GetColor(t.Critical)
	tcDim = tcell.GetColor(t.Dim)
	tcWhite = tcell.GetColor(t.White)
	return name
}

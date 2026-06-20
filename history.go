package main

import "strings"

// Ring is a fixed-capacity float64 buffer holding the most recent samples for a
// time-series sparkline. Cheap: a single backing slice, no external deps.
type Ring struct {
	data []float64
	cap  int
}

func NewRing(capacity int) *Ring {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring{cap: capacity, data: make([]float64, 0, capacity)}
}

func (r *Ring) Push(v float64) {
	r.data = append(r.data, v)
	if len(r.data) > r.cap {
		r.data = r.data[len(r.data)-r.cap:]
	}
}

func (r *Ring) Values() []float64 { return r.data }

// Max returns the largest sample (0 if empty).
func (r *Ring) Max() float64 {
	var m float64
	for _, v := range r.data {
		if v > m {
			m = v
		}
	}
	return m
}

// sparkRunes are the block-eighths glyphs, lowest to highest.
var sparkRunes = []rune("▁▂▃▄▅▆▇█")

// sparkline renders the most recent `width` samples as block glyphs, right
// aligned (newest on the right). max<=0 auto-scales to the sample maximum.
// Returns a plain (uncolored) string — callers wrap it in color tags.
func sparkline(vals []float64, max float64, width int) string {
	if width <= 0 || len(vals) == 0 {
		return strings.Repeat(" ", maxInt(width, 0))
	}
	if len(vals) > width {
		vals = vals[len(vals)-width:]
	}
	if max <= 0 {
		for _, v := range vals {
			if v > max {
				max = v
			}
		}
	}
	if max <= 0 {
		max = 1
	}

	var b strings.Builder
	for i := 0; i < width-len(vals); i++ {
		b.WriteByte(' ')
	}
	last := len(sparkRunes) - 1
	for _, v := range vals {
		if v < 0 {
			v = 0
		}
		idx := int(v / max * float64(last))
		if idx < 0 {
			idx = 0
		}
		if idx > last {
			idx = last
		}
		b.WriteRune(sparkRunes[idx])
	}
	return b.String()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

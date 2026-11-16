package tui

import (
	"strings"
	"time"
)

func splitLines(s string) []string { return strings.Split(s, "\n") }
func joinLines(l []string) string  { return strings.Join(l, "\n") }

// clamp limits v to [lo, hi].
func clamp(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Timeouts for background work.
const (
	applyTimeout = 30 * time.Second
	shortTimeout = 15 * time.Second
)

package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
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

// wrapUnits word-wraps plain text to w cells without ever splitting a word:
// a URL, an email address or a placeholder such as <owner> moves to the next
// line as one unit. Only a word longer than w is broken, after a "/" when it
// has one and anywhere else when it does not. Existing newlines are kept.
func wrapUnits(s string, w int) []string {
	if w < 4 {
		return splitLines(s)
	}
	var out []string
	for _, para := range splitLines(s) {
		line := ""
		flush := func() {
			out = append(out, line)
			line = ""
		}
		for _, word := range strings.Fields(para) {
			for _, piece := range breakLong(word, w) {
				switch {
				case line == "":
					line = piece
				case lipgloss.Width(line)+1+lipgloss.Width(piece) <= w:
					line += " " + piece
				default:
					flush()
					line = piece
				}
			}
		}
		flush()
	}
	return out
}

// breakLong splits a word wider than w after its "/" characters, so a URL
// keeps whole path segments together; a segment still too wide is cut hard.
// A word that fits is returned as is.
func breakLong(word string, w int) []string {
	if lipgloss.Width(word) <= w {
		return []string{word}
	}
	var segs []string
	for rest := word; rest != ""; {
		i := strings.IndexByte(rest, '/')
		if i < 0 {
			segs, rest = append(segs, rest), ""
			continue
		}
		segs, rest = append(segs, rest[:i+1]), rest[i+1:]
	}
	var out []string
	cur := ""
	add := func(piece string) {
		if cur != "" && lipgloss.Width(cur)+lipgloss.Width(piece) > w {
			out = append(out, cur)
			cur = ""
		}
		cur += piece
	}
	for _, seg := range segs {
		for lipgloss.Width(seg) > w {
			head, n := "", 0
			for _, r := range seg {
				if lipgloss.Width(head+string(r)) > w {
					break
				}
				head += string(r)
				n += len(string(r))
			}
			add(head)
			out, cur = append(out, cur), ""
			seg = seg[n:]
		}
		add(seg)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// hintText prepares a form hint for huh, which wraps descriptions after "."
// and "-" as well as spaces and would split a URL: the text is wrapped here,
// word-aware, so every line already fits when huh sees it.
func hintText(s string, w int) string {
	return joinLines(wrapUnits(s, w))
}

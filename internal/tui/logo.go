package tui

import "github.com/charmbracelet/lipgloss"

// logoLines is the wordmark of the start menu: six rows of block letters drawn
// with the full block and box-drawing runes. Those runes have an ambiguous East
// Asian width, so the full layout checks the measured width (logoWidth) before
// drawing it and falls back to the one-line wordmark when it does not fit.
var logoLines = []string{
	" ██████╗ ██╗████████╗██╗██████╗ ",
	"██╔════╝ ██║╚══██╔══╝██║██╔══██╗",
	"██║  ███╗██║   ██║   ██║██║  ██║",
	"██║   ██║██║   ██║   ██║██║  ██║",
	"╚██████╔╝██║   ██║   ██║██████╔╝",
	" ╚═════╝ ╚═╝   ╚═╝   ╚═╝╚═════╝ ",
}

// logoWidth is the width of the widest logo row.
func logoWidth() int {
	w := 0
	for _, l := range logoLines {
		w = maxInt(w, lipgloss.Width(l))
	}
	return w
}

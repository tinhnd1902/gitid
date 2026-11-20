package tui

import "github.com/charmbracelet/lipgloss"

// logoLines is the wordmark of the start menu: five rows of printable ASCII
// (no glyph of ambiguous width, so the columns never shift). The backticks and
// backslashes are why these are interpreted strings.
var logoLines = []string{
	"        _  _    _      _",
	"  __ _ (_)| |_ (_)  __| |",
	" / _` || ||  _|| | / _` |",
	" \\__, ||_| \\__||_| \\__,_|",
	" |___/",
}

// logoWidth is the width of the widest logo row.
func logoWidth() int {
	w := 0
	for _, l := range logoLines {
		w = maxInt(w, lipgloss.Width(l))
	}
	return w
}

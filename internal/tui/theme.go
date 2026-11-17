package tui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tinhnd1902/gitid/internal/platform"
)

// Palette: one set of adaptive colors shared by every screen so that the
// interface stays cohesive on light and dark terminals.
var (
	colAccent = lipgloss.AdaptiveColor{Light: "#5B3FD9", Dark: "#A594FF"}
	colTeal   = lipgloss.AdaptiveColor{Light: "#0B7F7B", Dark: "#5EEAD4"}
	colText   = lipgloss.AdaptiveColor{Light: "#1B1F2A", Dark: "#E7E9F0"}
	colMuted  = lipgloss.AdaptiveColor{Light: "#5F6677", Dark: "#9097AB"}
	colFaint  = lipgloss.AdaptiveColor{Light: "#8A90A0", Dark: "#646B80"}
	colLine   = lipgloss.AdaptiveColor{Light: "#C9CEDA", Dark: "#3B4157"}
	colSelBg  = lipgloss.AdaptiveColor{Light: "#E8E4FF", Dark: "#2C2F4A"}
	colOK     = lipgloss.AdaptiveColor{Light: "#177A3C", Dark: "#4ADE80"}
	colWarn   = lipgloss.AdaptiveColor{Light: "#A15C07", Dark: "#FBBF24"}
	colErr    = lipgloss.AdaptiveColor{Light: "#B42318", Dark: "#F87171"}
	colInfo   = lipgloss.AdaptiveColor{Light: "#0B6BA8", Dark: "#7DD3FC"}
	colOnAcc  = lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#14121F"}
)

// Styles is the single theme used by the whole TUI.
type Styles struct {
	Brand        lipgloss.Style // "gitid" pill in the header
	Crumb        lipgloss.Style
	CrumbSep     lipgloss.Style
	Text         lipgloss.Style
	Muted        lipgloss.Style
	Faint        lipgloss.Style
	Accent       lipgloss.Style
	Bold         lipgloss.Style
	OK           lipgloss.Style
	Warn         lipgloss.Style
	Err          lipgloss.Style
	Info         lipgloss.Style
	Panel        lipgloss.Style // blurred panel
	PanelFocus   lipgloss.Style // focused panel
	PanelTitle   lipgloss.Style
	Label        lipgloss.Style // "Email:" style labels
	Selected     lipgloss.Style // selected list row
	Button       lipgloss.Style
	ButtonOn     lipgloss.Style
	ButtonDanger lipgloss.Style
	DiffAdd      lipgloss.Style
	DiffDel      lipgloss.Style
	DiffHunk     lipgloss.Style
	DiffMeta     lipgloss.Style
	StatusBar    lipgloss.Style
	Badge        lipgloss.Style // highlighted status badge (pending changes)
	HelpPrimary  lipgloss.Style // the next-step item of the help bar
	Help         help.Styles
	Spinner      spinner.Model
}

// NewStyles builds the theme.
func NewStyles() *Styles {
	s := &Styles{}
	s.Brand = lipgloss.NewStyle().Bold(true).Foreground(colOnAcc).Background(colAccent).Padding(0, 1)
	s.Crumb = lipgloss.NewStyle().Foreground(colText).Bold(true)
	s.CrumbSep = lipgloss.NewStyle().Foreground(colFaint)
	s.Text = lipgloss.NewStyle().Foreground(colText)
	s.Muted = lipgloss.NewStyle().Foreground(colMuted)
	s.Faint = lipgloss.NewStyle().Foreground(colFaint)
	s.Accent = lipgloss.NewStyle().Foreground(colAccent)
	s.Bold = lipgloss.NewStyle().Bold(true).Foreground(colText)
	s.OK = lipgloss.NewStyle().Foreground(colOK)
	s.Warn = lipgloss.NewStyle().Foreground(colWarn)
	s.Err = lipgloss.NewStyle().Foreground(colErr)
	s.Info = lipgloss.NewStyle().Foreground(colInfo)
	s.Panel = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colLine).Padding(0, 1)
	s.PanelFocus = s.Panel.BorderForeground(colAccent)
	s.PanelTitle = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	s.Label = lipgloss.NewStyle().Foreground(colMuted)
	s.Selected = lipgloss.NewStyle().Background(colSelBg).Foreground(colText).Bold(true)
	s.Button = lipgloss.NewStyle().Padding(0, 2).Foreground(colText).Background(colSelBg)
	s.ButtonOn = lipgloss.NewStyle().Padding(0, 2).Bold(true).Foreground(colOnAcc).Background(colAccent)
	s.ButtonDanger = lipgloss.NewStyle().Padding(0, 2).Bold(true).Foreground(colOnAcc).Background(colErr)
	s.DiffAdd = lipgloss.NewStyle().Foreground(colOK)
	s.DiffDel = lipgloss.NewStyle().Foreground(colErr)
	s.DiffHunk = lipgloss.NewStyle().Foreground(colInfo)
	s.DiffMeta = lipgloss.NewStyle().Foreground(colMuted).Bold(true)
	s.StatusBar = lipgloss.NewStyle().Foreground(colMuted)
	// Reverse video plus bold plus the "●" marker: the badge never relies on
	// color alone. colOnAcc is white on the light warn and near-black on the
	// dark warn, which keeps the contrast readable in both themes.
	s.Badge = lipgloss.NewStyle().Bold(true).Foreground(colOnAcc).Background(colWarn).Padding(0, 1)
	s.HelpPrimary = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	s.Help = help.Styles{
		ShortKey:       lipgloss.NewStyle().Foreground(colAccent),
		ShortDesc:      lipgloss.NewStyle().Foreground(colMuted),
		ShortSeparator: lipgloss.NewStyle().Foreground(colFaint),
		Ellipsis:       lipgloss.NewStyle().Foreground(colFaint),
		FullKey:        lipgloss.NewStyle().Foreground(colAccent),
		FullDesc:       lipgloss.NewStyle().Foreground(colMuted),
		FullSeparator:  lipgloss.NewStyle().Foreground(colFaint),
	}
	s.Spinner = spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(lipgloss.NewStyle().Foreground(colAccent)))
	return s
}

// Platform badge colors (the text is always the short platform name so the
// meaning never depends on color alone).
var platformColor = map[platform.Platform]lipgloss.AdaptiveColor{
	platform.GitHub:      {Light: "#24292F", Dark: "#C9D1D9"},
	platform.GitLab:      {Light: "#C2410C", Dark: "#FB923C"},
	platform.Bitbucket:   {Light: "#1D4ED8", Dark: "#60A5FA"},
	platform.Gitea:       {Light: "#15803D", Dark: "#86EFAC"},
	platform.AzureDevOps: {Light: "#0369A1", Dark: "#38BDF8"},
}

// platformTag is the short badge text of a platform.
func platformTag(p platform.Platform) string {
	switch p {
	case platform.GitHub:
		return "GH"
	case platform.GitLab:
		return "GL"
	case platform.Bitbucket:
		return "BB"
	case platform.Gitea:
		return "GT"
	case platform.AzureDevOps:
		return "AZ"
	}
	return "??"
}

// platformBadge renders a colored platform tag.
func (s *Styles) platformBadge(p platform.Platform) string {
	c, ok := platformColor[p]
	if !ok {
		c = colMuted
	}
	return lipgloss.NewStyle().Foreground(c).Bold(true).Render(platformTag(p))
}

// huhTheme maps the palette onto huh forms.
func (s *Styles) huhTheme() *huh.Theme {
	t := huh.ThemeBase()
	t.Focused.Base = lipgloss.NewStyle().PaddingLeft(1).BorderStyle(lipgloss.ThickBorder()).BorderLeft(true).BorderForeground(colAccent)
	t.Focused.Card = t.Focused.Base
	t.Focused.Title = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	t.Focused.NoteTitle = lipgloss.NewStyle().Foreground(colAccent).Bold(true).MarginBottom(1)
	t.Focused.Description = lipgloss.NewStyle().Foreground(colMuted)
	t.Focused.ErrorIndicator = lipgloss.NewStyle().Foreground(colErr).SetString(" *")
	t.Focused.ErrorMessage = lipgloss.NewStyle().Foreground(colErr)
	t.Focused.SelectSelector = lipgloss.NewStyle().Foreground(colAccent).SetString("> ")
	t.Focused.NextIndicator = lipgloss.NewStyle().Foreground(colAccent).MarginLeft(1).SetString("→")
	t.Focused.PrevIndicator = lipgloss.NewStyle().Foreground(colAccent).MarginRight(1).SetString("←")
	t.Focused.Option = lipgloss.NewStyle().Foreground(colText)
	t.Focused.SelectedOption = lipgloss.NewStyle().Foreground(colOK)
	t.Focused.SelectedPrefix = lipgloss.NewStyle().Foreground(colOK).SetString("✓ ")
	t.Focused.UnselectedPrefix = lipgloss.NewStyle().Foreground(colFaint).SetString("• ")
	t.Focused.UnselectedOption = lipgloss.NewStyle().Foreground(colText)
	t.Focused.FocusedButton = s.ButtonOn
	t.Focused.Next = s.ButtonOn
	t.Focused.BlurredButton = s.Button
	t.Focused.TextInput.Cursor = lipgloss.NewStyle().Foreground(colTeal)
	// Placeholders are faint italics; typed text stays at full color, so an
	// example ("e.g. acme") never looks like a value the user already entered.
	t.Focused.TextInput.Placeholder = lipgloss.NewStyle().Foreground(colFaint).Italic(true)
	t.Focused.TextInput.Prompt = lipgloss.NewStyle().Foreground(colAccent)
	t.Focused.TextInput.Text = lipgloss.NewStyle().Foreground(colText)

	t.Blurred = t.Focused
	t.Blurred.Base = lipgloss.NewStyle().PaddingLeft(1).BorderStyle(lipgloss.HiddenBorder()).BorderLeft(true)
	t.Blurred.Card = t.Blurred.Base
	t.Blurred.Title = lipgloss.NewStyle().Foreground(colMuted)
	t.Blurred.TextInput.Text = lipgloss.NewStyle().Foreground(colText).Bold(true) // a filled field reads as a value
	t.Blurred.NextIndicator = lipgloss.NewStyle()
	t.Blurred.PrevIndicator = lipgloss.NewStyle()
	t.Group.Title = t.Focused.Title
	t.Group.Description = t.Focused.Description
	return t
}

// trunc cuts s (which may contain ANSI escapes) to at most w cells.
func trunc(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

// pad pads or cuts s to exactly w cells.
func pad(s string, w int) string {
	s = trunc(s, w)
	if n := w - lipgloss.Width(s); n > 0 {
		s += spaces(n)
	}
	return s
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}

// fitHeight pads or cuts s to exactly h lines.
func fitHeight(s string, h int) string {
	if h <= 0 {
		return ""
	}
	lines := splitLines(s)
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return joinLines(lines)
}

// truncLeft keeps the tail of s (a path) within w cells, with a leading
// ellipsis.
func truncLeft(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.TruncateLeft(s, lipgloss.Width(s)-w+1, "…")
}

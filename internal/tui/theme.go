package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/platform"
)

// Palette: one set of adaptive colors shared by every screen so that the
// interface stays cohesive on light and dark terminals.
var (
	// accent marks what has the keyboard: the focused border, the focused
	// field, the cursor, the chosen item, the brand pill and the spinner.
	// Nothing else uses it.
	colAccent = lipgloss.AdaptiveColor{Light: "#5B3FD9", Dark: "#A594FF"}
	colText   = lipgloss.AdaptiveColor{Light: "#1F2328", Dark: "#E6EDF3"}
	colMuted  = lipgloss.AdaptiveColor{Light: "#59636E", Dark: "#9198A1"}
	// colFaint is an alias of muted: there is one grey for secondary text.
	colFaint = colMuted
	colLine  = lipgloss.AdaptiveColor{Light: "#D1D9E0", Dark: "#3D444D"}
	colSelBg = lipgloss.AdaptiveColor{Light: "#EEEBFF", Dark: "#2A2B45"}
	colOK    = lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#56D364"}
	colWarn  = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#E3B341"}
	colErr   = lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#FF7B72"}
	colOnAcc = lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#14121F"}
)

// Styles is the single theme used by the whole TUI.
type Styles struct {
	Brand          lipgloss.Style // "gitid" pill in the header
	Crumb          lipgloss.Style
	CrumbSep       lipgloss.Style
	Text           lipgloss.Style
	Muted          lipgloss.Style
	Faint          lipgloss.Style // alias of Muted: there is one grey
	Accent         lipgloss.Style
	Logo           lipgloss.Style // the wordmark of the start menu: accent colour only
	Bold           lipgloss.Style
	OK             lipgloss.Style
	Warn           lipgloss.Style
	Err            lipgloss.Style
	Info           lipgloss.Style // text colour; the "i" glyph carries the level
	Panel          lipgloss.Style // blurred panel
	PanelFocus     lipgloss.Style // focused panel
	PanelActive    lipgloss.Style // the pane that has the keyboard when two panes are shown (heavy border)
	PanelTitle     lipgloss.Style
	PanelTitleBlur lipgloss.Style // title of a pane that does not have the keyboard
	Label          lipgloss.Style // "Email" style labels
	Selected       lipgloss.Style // selected list row
	Button         lipgloss.Style
	ButtonOn       lipgloss.Style
	ButtonDanger   lipgloss.Style
	DiffAdd        lipgloss.Style
	DiffDel        lipgloss.Style
	DiffHunk       lipgloss.Style
	DiffMeta       lipgloss.Style
	StatusBar      lipgloss.Style
	Badge          lipgloss.Style // pending-changes badge: warn colour, no fill
	HelpPrimary    lipgloss.Style // the next-step item of the help bar
	HelpLabel      lipgloss.Style // "This row:" / "Always:" in front of a help row
	HelpSelected   lipgloss.Style // the selected row of the "?" overlay
	Help           help.Styles
	Spinner        spinner.Model

	// Form styles (form.go). Palette colors only.
	FormTitle       lipgloss.Style // title of the focused field
	FormTitleBlur   lipgloss.Style // title of the other fields
	FormHint        lipgloss.Style
	FormGutter      lipgloss.Style // the "┃" at the left of the focused field
	FormPrompt      lipgloss.Style // the "> " of a text input
	FormCursor      lipgloss.Style
	FormValue       lipgloss.Style // text typed in the focused input
	FormValueBlur   lipgloss.Style // a filled field reads as a value: bold
	FormPlaceholder lipgloss.Style // muted italics, never mistaken for a value
	FormOptionOn    lipgloss.Style // chosen option of the focused field
	FormOption      lipgloss.Style
	FormSection     lipgloss.Style
}

// NewStyles builds the theme. Bold marks a title, a section, the key inside a
// sentence, a filled form value and the chosen radio label; nothing else.
func NewStyles() *Styles {
	s := &Styles{}
	s.Brand = lipgloss.NewStyle().Foreground(colOnAcc).Background(colAccent).Padding(0, 1)
	s.Crumb = lipgloss.NewStyle().Foreground(colText).Bold(true)
	s.CrumbSep = lipgloss.NewStyle().Foreground(colMuted)
	s.Text = lipgloss.NewStyle().Foreground(colText)
	s.Muted = lipgloss.NewStyle().Foreground(colMuted)
	s.Faint = s.Muted
	s.Accent = lipgloss.NewStyle().Foreground(colAccent)
	s.Logo = lipgloss.NewStyle().Foreground(colAccent)
	s.Bold = lipgloss.NewStyle().Bold(true).Foreground(colText)
	s.OK = lipgloss.NewStyle().Foreground(colOK)
	s.Warn = lipgloss.NewStyle().Foreground(colWarn)
	s.Err = lipgloss.NewStyle().Foreground(colErr)
	s.Info = s.Text
	s.Panel = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colLine).Padding(0, 1)
	s.PanelFocus = s.Panel.BorderForeground(colAccent)
	// a heavy border has the same frame size as a rounded one (one cell per
	// side), so no layout arithmetic changes
	s.PanelActive = s.Panel.Border(lipgloss.ThickBorder()).BorderForeground(colAccent)
	// the focus is told by the border, not by the title colour
	s.PanelTitle = lipgloss.NewStyle().Bold(true).Foreground(colText)
	s.PanelTitleBlur = lipgloss.NewStyle().Bold(true).Foreground(colMuted)
	s.Label = lipgloss.NewStyle().Foreground(colMuted)
	s.Selected = lipgloss.NewStyle().Background(colSelBg).Foreground(colText)
	s.Button = lipgloss.NewStyle().Padding(0, 2).Foreground(colText).Background(colSelBg)
	s.ButtonOn = lipgloss.NewStyle().Padding(0, 2).Foreground(colOnAcc).Background(colAccent)
	s.ButtonDanger = lipgloss.NewStyle().Padding(0, 2).Foreground(colOnAcc).Background(colErr)
	s.DiffAdd = lipgloss.NewStyle().Foreground(colOK)
	s.DiffDel = lipgloss.NewStyle().Foreground(colErr)
	s.DiffHunk = lipgloss.NewStyle().Foreground(colMuted)
	s.DiffMeta = lipgloss.NewStyle().Foreground(colMuted)
	s.StatusBar = lipgloss.NewStyle().Foreground(colMuted)
	// A normal state (edits not applied yet) must not be the loudest thing on
	// the screen: warn colour and the "●" marker, no fill.
	s.Badge = lipgloss.NewStyle().Foreground(colWarn)
	s.HelpPrimary = lipgloss.NewStyle().Foreground(colAccent)
	s.HelpLabel = lipgloss.NewStyle().Foreground(colMuted)
	s.HelpSelected = s.Selected
	s.Help = help.Styles{
		ShortKey:       lipgloss.NewStyle().Foreground(colText),
		ShortDesc:      lipgloss.NewStyle().Foreground(colMuted),
		ShortSeparator: lipgloss.NewStyle().Foreground(colMuted),
		Ellipsis:       lipgloss.NewStyle().Foreground(colMuted),
		FullKey:        lipgloss.NewStyle().Foreground(colText),
		FullDesc:       lipgloss.NewStyle().Foreground(colMuted),
		FullSeparator:  lipgloss.NewStyle().Foreground(colMuted),
	}
	s.FormTitle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	s.FormTitleBlur = lipgloss.NewStyle().Foreground(colMuted)
	s.FormHint = lipgloss.NewStyle().Foreground(colMuted)
	s.FormGutter = lipgloss.NewStyle().Foreground(colAccent)
	s.FormPrompt = lipgloss.NewStyle().Foreground(colAccent)
	s.FormCursor = lipgloss.NewStyle().Foreground(colAccent)
	s.FormValue = lipgloss.NewStyle().Foreground(colText)
	s.FormValueBlur = lipgloss.NewStyle().Foreground(colText).Bold(true)
	s.FormPlaceholder = lipgloss.NewStyle().Foreground(colMuted).Italic(true)
	s.FormOptionOn = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	s.FormOption = lipgloss.NewStyle().Foreground(colText)
	s.FormSection = lipgloss.NewStyle().Foreground(colText).Bold(true)
	s.Spinner = spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(lipgloss.NewStyle().Foreground(colAccent)))
	return s
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

// platformBadge renders the platform tag. It has no colour of its own: the
// text already says which platform it is.
func (s *Styles) platformBadge(p platform.Platform) string {
	return s.Text.Render(platformTag(p))
}

// identityStamp is how gitid names an identity everywhere: Git's own author
// notation behind the gitid ref, "acme/alice · Alice Nguyen <alice@acme.com>".
// The ref and the name are in text colour, the address in muted. It is what
// ends up in a commit, so the user sees who they are about to commit as.
func (s *Styles) identityStamp(ref config.AccountRef) string {
	out := s.Text.Render(ref.String())
	a := ref.Account
	if a.Name != "" {
		out += s.Text.Render(" · " + a.Name)
	}
	if a.Email != "" {
		out += " " + s.Muted.Render("<"+a.Email+">")
	}
	return out
}

// identityStampPlain is identityStamp without styling (for rows that carry
// their own background).
func identityStampPlain(ref config.AccountRef) string {
	out := ref.String()
	a := ref.Account
	if a.Name != "" {
		out += " · " + a.Name
	}
	if a.Email != "" {
		out += " <" + a.Email + ">"
	}
	return out
}

// trunc cuts s (which may contain ANSI escapes) to at most w cells.
func trunc(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

// truncWords cuts plain text to w cells at a word boundary, with an ellipsis,
// so a word is never shown cut in the middle. A single word wider than w is
// cut like trunc does.
func truncWords(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	cut := ansi.Truncate(s, w-1, "")
	// the cut is clean when it ends at a space or the text goes on with one
	rest := strings.TrimPrefix(s, cut)
	if !strings.HasSuffix(cut, " ") && !strings.HasPrefix(rest, " ") {
		if i := strings.LastIndexByte(cut, ' '); i > 0 {
			cut = cut[:i]
		}
	}
	return strings.TrimRight(cut, " ") + "…"
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

// Package block edits a marker-delimited managed block inside a text file
// without touching any byte outside the markers. Insert + Remove round-trips
// to the original bytes.
package block

import (
	"errors"
	"fmt"
	"strings"
)

// ErrCorruptBlock means the markers are duplicated, unbalanced or reversed.
var ErrCorruptBlock = errors.New("block: corrupt managed block markers")

// Markers are the exact start and end lines of a block (compared as whole
// lines, ignoring a trailing CR).
type Markers struct {
	Start, End string
}

// Placement says where a new block is inserted.
type Placement int

// Placements.
const (
	AtEnd Placement = iota
	AtTop
)

// Loc is the byte range [Start, End) of a block: from the first byte of the
// start-marker line through the end-marker line including its line break (if
// it has one).
type Loc struct {
	Start, End int
	// Unterminated is true when the end marker is the last line of the file
	// and has no line break.
	Unterminated bool
}

type line struct {
	start, end int // end includes the line break
	text       string
}

func lines(s string) []line {
	var out []line
	for i := 0; i < len(s); {
		j := strings.IndexByte(s[i:], '\n')
		var l line
		if j < 0 {
			l = line{i, len(s), s[i:]}
			i = len(s)
		} else {
			l = line{i, i + j + 1, s[i : i+j]}
			i += j + 1
		}
		l.text = strings.TrimSuffix(l.text, "\r")
		out = append(out, l)
	}
	return out
}

// Find locates the block. ok is false if there is none. Duplicated,
// unbalanced or reversed markers return ErrCorruptBlock.
func Find(text string, m Markers) (loc Loc, ok bool, err error) {
	var starts, ends []line
	for _, l := range lines(text) {
		switch l.text {
		case m.Start:
			starts = append(starts, l)
		case m.End:
			ends = append(ends, l)
		}
	}
	switch {
	case len(starts) == 0 && len(ends) == 0:
		return Loc{}, false, nil
	case len(starts) > 1:
		return Loc{}, false, fmt.Errorf("%w: %d start markers", ErrCorruptBlock, len(starts))
	case len(ends) > 1:
		return Loc{}, false, fmt.Errorf("%w: %d end markers", ErrCorruptBlock, len(ends))
	case len(starts) == 1 && len(ends) == 0:
		return Loc{}, false, fmt.Errorf("%w: start marker without end marker", ErrCorruptBlock)
	case len(starts) == 0 && len(ends) == 1:
		return Loc{}, false, fmt.Errorf("%w: end marker without start marker", ErrCorruptBlock)
	case ends[0].start < starts[0].start:
		return Loc{}, false, fmt.Errorf("%w: end marker before start marker", ErrCorruptBlock)
	}
	e := ends[0]
	return Loc{Start: starts[0].start, End: e.end, Unterminated: !strings.HasSuffix(text[e.start:e.end], "\n")}, true, nil
}

// EOL returns the dominant line ending of text ("\r\n" when any CRLF is
// present, else "\n").
func EOL(text string) string {
	if strings.Contains(text, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

func render(m Markers, body, eol string, terminated bool) string {
	body = strings.TrimRight(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var b strings.Builder
	b.WriteString(m.Start + eol)
	if body != "" {
		b.WriteString(strings.ReplaceAll(body, "\n", eol) + eol)
	}
	b.WriteString(m.End)
	if terminated {
		b.WriteString(eol)
	}
	return b.String()
}

// Upsert inserts or replaces the block so that its inner content is body.
// An empty body removes the block (clean uninstall). A new block goes at the
// end or the top according to place; an existing block is replaced in place,
// except that AtTop moves it to byte 0. Output is byte-identical when nothing
// needs to change.
func Upsert(text, body string, m Markers, place Placement) (string, error) {
	if strings.TrimSpace(body) == "" {
		return Remove(text, m)
	}
	loc, found, err := Find(text, m)
	if err != nil {
		return "", err
	}
	eol := EOL(text)
	if found {
		if place == AtTop && loc.Start != 0 {
			stripped, err := Remove(text, m)
			if err != nil {
				return "", err
			}
			return Upsert(stripped, body, m, place)
		}
		return text[:loc.Start] + render(m, body, eol, !loc.Unterminated) + text[loc.End:], nil
	}
	switch place {
	case AtTop:
		return render(m, body, eol, true) + text, nil
	default:
		if text == "" {
			return render(m, body, eol, true), nil
		}
		if strings.HasSuffix(text, "\n") {
			return text + render(m, body, eol, true), nil
		}
		// Original had no trailing line break: keep that property. The block
		// stays unterminated, which also lets Remove undo the added break.
		return text + eol + render(m, body, eol, false), nil
	}
}

// Remove deletes the block (if any) and returns the original bytes that
// preceded Upsert.
func Remove(text string, m Markers) (string, error) {
	loc, found, err := Find(text, m)
	if err != nil {
		return "", err
	}
	if !found {
		return text, nil
	}
	start := loc.Start
	if loc.Unterminated {
		// Upsert added a line break before an unterminated trailing block.
		switch {
		case strings.HasSuffix(text[:start], "\r\n"):
			start -= 2
		case strings.HasSuffix(text[:start], "\n"):
			start--
		}
	}
	return text[:start] + text[loc.End:], nil
}

// Body returns the inner content of the block (without markers), with line
// breaks normalised to "\n".
func Body(text string, m Markers) (string, bool, error) {
	loc, found, err := Find(text, m)
	if err != nil || !found {
		return "", false, err
	}
	ls := lines(text[loc.Start:loc.End])
	var out []string
	for _, l := range ls[1 : len(ls)-1] {
		out = append(out, l.text)
	}
	if len(out) == 0 {
		return "", true, nil
	}
	return strings.Join(out, "\n") + "\n", true, nil
}

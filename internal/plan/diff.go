package plan

import (
	"strings"

	"github.com/pmezard/go-difflib/difflib"

	"github.com/tinhnd1902/gitid/internal/fsx"
)

// Diff renders a unified diff of the change.
func (c FileChange) Diff() string {
	from, to := "a/"+c.Display, "b/"+c.Display
	switch c.Op {
	case fsx.OpCreate:
		from = "/dev/null"
	case fsx.OpDelete:
		to = "/dev/null"
	}
	ud := difflib.UnifiedDiff{
		A:        splitLines(string(c.Old)),
		B:        splitLines(string(c.New)),
		FromFile: from,
		ToFile:   to,
		Context:  3,
	}
	out, err := difflib.GetUnifiedDiffString(ud)
	if err != nil {
		return "diff error: " + err.Error() + "\n"
	}
	if !strings.HasSuffix(out, "\n") && out != "" {
		out += "\n"
	}
	return out
}

// splitLines splits keeping line breaks (difflib.SplitLines appends a bogus
// trailing newline). A last line without a line break gets git's
// "\ No newline at end of file" marker so that it shows up in the diff.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		return lines[:len(lines)-1]
	}
	lines[len(lines)-1] += "\n\\ No newline at end of file\n"
	return lines
}

// Diff renders the unified diff of every change, in apply order.
func (p Plan) Diff() string {
	var b strings.Builder
	for _, c := range p.Changes {
		b.WriteString(c.Diff())
	}
	return b.String()
}

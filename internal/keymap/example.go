package keymap

import (
	"strconv"
	"strings"
)

// exampleWidth is the widest line of the example.
const exampleWidth = 80

// Example returns the content of an example keys.yaml for the file at path.
// Every binding is a commented line holding its default, so saving the text
// as is changes nothing until a line is uncommented. gitid only prints it;
// it never writes the file.
//
// All comment lines start with "##" or carry a trailing "##" so that removing
// every "# " from the text still gives valid YAML.
func Example(path string) string {
	var b strings.Builder
	b.WriteString("## gitid key bindings (optional file; gitid reads it and never writes it).\n")
	b.WriteString("## Location: " + path + "\n")
	b.WriteString("## Every line below is a comment. Remove the leading \"#\" of a line to change\n")
	b.WriteString("## that key.\n")
	b.WriteString("## A key is one printable ASCII character, ctrl+<letter>, alt+<character> or\n")
	b.WriteString("## f1..f12.\n")
	b.WriteString("## Use a list for several keys, for example: clone: [\"c\", \"x\"]\n")
	b.WriteString("## Quote the key when it is one of ? # & * ! | > % @ ` so YAML reads it as text.\n")
	b.WriteString("## Two actions that are live on the same screen can not share a key.\n")
	b.WriteString("## Check the result with: gitid keys\n")
	b.WriteString("##\n")
	width := 0
	for _, d := range defs {
		if n := len(d.Action) + 2 + len(yamlKeys(d.Default)); n > width {
			width = n
		}
	}
	for _, d := range defs {
		left := string(d.Action) + ": " + yamlKeys(d.Default)
		prefix := "# " + left + strings.Repeat(" ", width-len(left)+2) + "## "
		scopes := make([]string, len(d.Scopes))
		for i, s := range d.Scopes {
			scopes[i] = string(s)
		}
		// Long scope lists continue on "##" lines so no line passes 80 columns.
		line, empty := prefix, true
		for i, sc := range scopes {
			word := sc
			if i < len(scopes)-1 {
				word += ","
			}
			if !empty && len(line)+1+len(word) > exampleWidth {
				b.WriteString(line + "\n")
				line, empty = "##"+strings.Repeat(" ", len(prefix)-2), true
			}
			if !empty {
				line += " "
			}
			line += word
			empty = false
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func yamlKeys(keys []string) string {
	q := make([]string, len(keys))
	for i, k := range keys {
		q[i] = strconv.Quote(k)
	}
	if len(q) == 1 {
		return q[0]
	}
	return "[" + strings.Join(q, ", ") + "]"
}

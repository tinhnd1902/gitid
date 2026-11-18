package cli

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
	"text/tabwriter"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/keymap"
)

// keysWidth is the widest line the CLI prints (a classic terminal).
const keysWidth = 80

var errKeysFlags = errors.New("keys: --example and --json can not be used together")

type keysBindingJSON struct {
	Action      string   `json:"action"`
	Keys        []string `json:"keys"`
	Default     []string `json:"default"`
	Scopes      []string `json:"scopes"`
	Custom      bool     `json:"custom"`
	Description string   `json:"description"`
}

type keysWarningJSON struct {
	Code    string `json:"code"`
	Action  string `json:"action"`
	Key     string `json:"key"`
	Message string `json:"message"`
}

type keysJSON struct {
	Bindings []keysBindingJSON `json:"bindings"`
	Warnings []keysWarningJSON `json:"warnings"`
}

func buildKeys(m keymap.Map, issues []keymap.Issue) keysJSON {
	out := keysJSON{Bindings: []keysBindingJSON{}, Warnings: []keysWarningJSON{}}
	for _, d := range keymap.Defs() {
		scopes := make([]string, len(d.Scopes))
		for i, s := range d.Scopes {
			scopes[i] = string(s)
		}
		out.Bindings = append(out.Bindings, keysBindingJSON{
			Action: string(d.Action), Keys: m.Keys(d.Action), Default: d.Default, Scopes: scopes,
			Custom: m.Custom(d.Action), Description: i18n.T(d.DescKey),
		})
	}
	for _, is := range issues {
		out.Warnings = append(out.Warnings, keysWarningJSON{Code: is.Code, Action: is.SafeAction(), Key: is.SafeKey(), Message: is.Message()})
	}
	return out
}

func (c *cli) keysCmd() *cobra.Command {
	var asJSON, example bool
	cmd := &cobra.Command{
		Use:   "keys",
		Short: i18n.T("cli.keys.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if asJSON && example {
				return errKeysFlags
			}
			path := c.d.Env.Paths().KeyBindingsFile
			if example {
				// stdout only: gitid never writes keys.yaml
				c.print(keymap.Example(c.d.Env.Contract(path)))
				return nil
			}
			m, issues := keymap.Load(c.d.Env)
			doc := buildKeys(m, issues)
			if asJSON {
				b, err := json.MarshalIndent(doc, "", "  ")
				if err != nil {
					return err
				}
				c.println(string(b))
			} else {
				c.printKeys(path, doc)
			}
			if len(issues) > 0 {
				return problem()
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, i18n.T("cli.flag.json"))
	cmd.Flags().BoolVar(&example, "example", false, i18n.T("cli.keys.flag_example"))
	return cmd
}

func (c *cli) printKeys(path string, doc keysJSON) {
	shown := c.d.Env.Contract(path)
	if _, err := os.Stat(path); err != nil && errors.Is(err, fs.ErrNotExist) {
		c.println(i18n.T("cli.keys.file_absent", shown))
	} else {
		c.println(i18n.T("cli.keys.file_present", shown))
	}
	c.println()
	// The scopes column can be long: it is printed last and wrapped so that
	// no line passes 80 columns.
	var buf strings.Builder
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	row := func(cols ...string) { _, _ = tw.Write([]byte(strings.Join(cols, "\t") + "\t\n")) }
	row("", i18n.T("cli.keys.col_action"), i18n.T("cli.keys.col_keys"), i18n.T("cli.keys.col_default"))
	custom := false
	scopes := []string{i18n.T("cli.keys.col_scopes")}
	for _, b := range doc.Bindings {
		mark := ""
		if b.Custom {
			mark, custom = "*", true
		}
		row(mark, b.Action, strings.Join(b.Keys, ", "), strings.Join(b.Default, ", "))
		scopes = append(scopes, strings.Join(b.Scopes, ", "))
	}
	_ = tw.Flush()
	for i, l := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		pad := strings.Repeat(" ", utf8.RuneCountInString(l))
		for n, part := range wrapText(scopes[i], keysWidth-utf8.RuneCountInString(l)) {
			if n == 0 {
				c.println(l + part)
			} else {
				c.println(pad + part)
			}
		}
	}
	if custom {
		c.println()
		c.println(i18n.T("cli.keys.custom_note"))
	}
	if len(doc.Warnings) > 0 {
		c.println()
		c.println(i18n.T("cli.keys.warnings"))
		for _, w := range doc.Warnings {
			for n, part := range wrapText(w.Message, keysWidth-4) {
				if n == 0 {
					c.println("  ! " + part)
				} else {
					c.println("    " + part)
				}
			}
		}
	}
}

// wrapText breaks s at spaces into lines of at most width runes (a word longer
// than width stays whole). width is at least 20.
func wrapText(s string, width int) []string {
	width = max(width, 20)
	var out []string
	line := ""
	for _, w := range strings.Fields(s) {
		switch {
		case line == "":
			line = w
		case utf8.RuneCountInString(line)+1+utf8.RuneCountInString(w) <= width:
			line += " " + w
		default:
			out = append(out, line)
			line = w
		}
	}
	return append(out, line)
}

package cli

import (
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/tinhnd1902/gitid/internal/doctor"
	"github.com/tinhnd1902/gitid/internal/i18n"
)

type doctorJSON struct {
	doctor.Result
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func args(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func (c *cli) renderResult(r doctor.Result) doctorJSON {
	j := doctorJSON{Result: r, Message: i18n.T(r.Msg, args(r.Args)...)}
	if r.HintKey != "" {
		j.Hint = i18n.T(r.HintKey)
	}
	return j
}

func (c *cli) doctorCmd() *cobra.Command {
	var asJSON, sshTest bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: i18n.T("cli.doctor.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := c.load()
			if err != nil {
				return err
			}
			results := c.svc.Doctor(cmd.Context(), cfg, doctor.Options{SSHTest: sshTest})
			rendered := make([]doctorJSON, 0, len(results))
			for _, r := range results {
				rendered = append(rendered, c.renderResult(r))
			}
			if asJSON {
				b, err := json.MarshalIndent(map[string]any{"results": rendered, "worst": doctor.Worst(results)}, "", "  ")
				if err != nil {
					return err
				}
				c.println(string(b))
			} else {
				for _, r := range rendered {
					c.printf("%-8s %s\n", "["+i18n.T("cli.status."+string(r.Status))+"]", r.Message)
					if r.Detail != "" && r.Status != doctor.OK {
						for _, part := range wrapText(r.Detail, keysWidth-11) {
							c.printf("%-8s   %s\n", "", part)
						}
					}
					if r.Hint != "" && r.Status != doctor.OK {
						c.printf("%-8s   -> %s\n", "", r.Hint)
					}
				}
			}
			if doctor.Worst(results) == doctor.Fail {
				return problem()
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, i18n.T("cli.flag.json"))
	cmd.Flags().BoolVar(&sshTest, "ssh-test", false, i18n.T("cli.flag.ssh_test"))
	return cmd
}

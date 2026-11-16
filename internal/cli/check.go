package cli

import (
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/i18n"
)

func (c *cli) printCheck(res app.CheckResult) {
	c.printf("%-10s %s\n", i18n.T("cli.check.repo"), res.Repo)
	remoteText := res.Remote
	if remoteText == "" {
		remoteText = "-"
	}
	c.printf("%-10s %s\n", i18n.T("cli.check.remote"), remoteText)
	email := res.EffectiveEmail
	if email == "" {
		email = "-"
	}
	if res.EmailOrigin != "" {
		email += "  (" + res.EmailOrigin + ")"
	}
	c.printf("%-10s %s\n", i18n.T("cli.check.email"), email)
	acct := "-"
	if res.Account != "" {
		acct = res.Account + "  (" + i18n.T("cli.reason."+res.Reason) + ")"
	}
	c.printf("%-10s %s\n", i18n.T("cli.check.account"), acct)
	if res.ExpectedEmail != "" {
		c.printf("%-10s %s\n", i18n.T("cli.check.expected"), res.ExpectedEmail)
	}
	if res.OK() {
		c.println(i18n.T("cli.check.ok"))
		return
	}
	for _, w := range res.Warnings {
		c.println("! " + i18n.T(w))
	}
}

// checkWarningJSON is a warning in `check --json`: a stable code plus the
// translated text, so scripts match on code and people read message.
type checkWarningJSON struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// checkJSON is the stable `check --json` schema.
type checkJSON struct {
	Repo           string             `json:"repo"`
	Remote         string             `json:"remote"`
	EffectiveEmail string             `json:"effective_email"`
	EmailOrigin    string             `json:"email_origin"`
	Account        string             `json:"account"`
	Reason         string             `json:"reason"`
	ExpectedEmail  string             `json:"expected_email,omitempty"`
	Warnings       []checkWarningJSON `json:"warnings"`
}

func toCheckJSON(r app.CheckResult) checkJSON {
	out := checkJSON{Repo: r.Repo, Remote: r.Remote, EffectiveEmail: r.EffectiveEmail, EmailOrigin: r.EmailOrigin,
		Account: r.Account, Reason: r.Reason, ExpectedEmail: r.ExpectedEmail, Warnings: []checkWarningJSON{}}
	for _, w := range r.Warnings {
		out.Warnings = append(out.Warnings, checkWarningJSON{Code: w, Message: i18n.T(w)})
	}
	return out
}

func (c *cli) checkCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "check [path]",
		Short: i18n.T("cli.check.short"),
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := c.cwd()
			if err != nil {
				return err
			}
			if len(args) == 1 {
				dir = args[0]
			}
			cfg, err := c.load()
			if err != nil {
				return err
			}
			res, err := c.svc.Check(cmd.Context(), cfg, dir)
			if err != nil {
				return err
			}
			if asJSON {
				b, err := json.MarshalIndent(toCheckJSON(res), "", "  ")
				if err != nil {
					return err
				}
				c.println(string(b))
			} else {
				c.printCheck(res)
			}
			if !res.OK() {
				return problem()
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, i18n.T("cli.flag.json"))
	return cmd
}

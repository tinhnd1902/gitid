package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/gitops"
	"github.com/tinhnd1902/gitid/internal/i18n"
)

func (c *cli) cloneCmd() *cobra.Command {
	var account, dir string
	var dryRun, force, interactive bool
	cmd := &cobra.Command{
		Use:   "clone <url>",
		Short: i18n.T("cli.clone.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := c.load()
			if err != nil {
				return err
			}
			cwd, _ := c.cwd()
			plan, err := c.svc.PlanClone(cfg, app.CloneRequest{URL: args[0], Account: account, Dir: dir, Cwd: cwd, Force: force})
			if err != nil {
				return err
			}
			for _, w := range plan.Warnings {
				c.println("! " + i18n.T(w))
			}
			c.println(i18n.T("cli.clone.original", plan.Original))
			c.println(i18n.T("cli.clone.url", plan.URL))
			c.println(i18n.T("cli.clone.dir", c.svc.Env.Contract(plan.Dir)))
			c.println(i18n.T("cli.clone.account", plan.Account.String()))
			if dryRun {
				c.println(i18n.T("cli.clone.dryrun"))
				return nil
			}
			var res app.CloneResult
			if interactive {
				res, err = c.svc.CloneTerminal(cmd.Context(), plan)
			} else {
				last := ""
				res, err = c.svc.Clone(cmd.Context(), plan, func(p gitops.Progress) {
					if c.isTTY() {
						if line := fmt.Sprintf("%s %d%%", p.Phase, p.Pct); line != last {
							last = line
							fmt.Fprintf(c.err, "\r%-40s", line)
						}
					}
				})
				if c.isTTY() && last != "" {
					fmt.Fprintln(c.err)
				}
			}
			if err != nil {
				if errors.Is(err, gitops.ErrAuth) {
					c.println(i18n.T("cli.clone.auth_hint"))
				}
				return err
			}
			c.println(i18n.T("cli.clone.done", c.svc.Env.Contract(plan.Dir)))
			if res.EmailOK {
				c.println(i18n.T("cli.clone.email_ok", res.EffectiveEmail))
				return nil
			}
			shown := res.EffectiveEmail
			if shown == "" {
				shown = "-"
			}
			c.println("! " + i18n.T("cli.clone.email_bad", shown, plan.Account.Account.Email))
			return problem()
		},
	}
	cmd.Flags().StringVar(&account, "account", "", i18n.T("cli.flag.account"))
	_ = cmd.MarkFlagRequired("account")
	cmd.Flags().StringVar(&dir, "dir", "", i18n.T("cli.flag.dir"))
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, i18n.T("cli.flag.dry_run"))
	cmd.Flags().BoolVar(&force, "force", false, i18n.T("cli.flag.force"))
	cmd.Flags().BoolVar(&interactive, "interactive", false, i18n.T("cli.flag.interactive"))
	return cmd
}

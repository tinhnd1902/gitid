package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/i18n"
)

var errNonInteractive = errors.New("apply: refusing to write without --yes on a non-interactive terminal")

func (c *cli) applyCmd() *cobra.Command {
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "apply",
		Short: i18n.T("cli.apply.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := c.load()
			if err != nil {
				return err
			}
			if errs := c.svc.Validate(cfg); len(errs) > 0 {
				return &app.ValidationError{Errors: errs}
			}
			p, err := c.svc.BuildPlan(cfg)
			if err != nil {
				return err
			}
			for _, w := range p.Warnings {
				c.println("! " + i18n.T(w))
			}
			if p.Empty() {
				c.println(i18n.T("cli.apply.no_changes"))
				return nil
			}
			c.println(i18n.T("cli.apply.plan_header", len(p.Changes)))
			c.print(p.Summary())
			c.println()
			c.print(p.Diff())
			if dryRun {
				c.println(i18n.T("cli.apply.dryrun_note"))
				return nil
			}
			if !yes {
				if !c.isTTY() {
					return errNonInteractive
				}
				if !c.confirm(i18n.T("cli.apply.confirm")) {
					c.println(i18n.T("cli.apply.cancelled"))
					return nil
				}
			}
			res, err := c.svc.Apply(cmd.Context(), p)
			if err != nil {
				return err
			}
			for _, w := range res.Written {
				c.println(i18n.T("cli.apply.written", c.svc.Env.Contract(w)))
			}
			for _, w := range res.Deleted {
				c.println(i18n.T("cli.apply.deleted", c.svc.Env.Contract(w)))
			}
			c.println(i18n.T("cli.apply.done", c.svc.Env.Contract(res.BackupDir)))
			for _, w := range res.Warnings {
				c.println("! " + w)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, i18n.T("cli.flag.dry_run"))
	cmd.Flags().BoolVar(&yes, "yes", false, i18n.T("cli.flag.yes"))
	return cmd
}

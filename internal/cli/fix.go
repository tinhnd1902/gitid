package cli

import (
	"github.com/spf13/cobra"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/i18n"
)

func (c *cli) fixRemoteCmd() *cobra.Command {
	var account, remoteName string
	var dryRun, force bool
	cmd := &cobra.Command{
		Use:   "fix-remote [path]",
		Short: i18n.T("cli.fix.short"),
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
			res, err := c.svc.FixRemote(cmd.Context(), cfg, app.FixRequest{Dir: dir, Remote: remoteName, Account: account, DryRun: dryRun, Force: force})
			if err != nil {
				return err
			}
			if !res.Changed {
				c.println(i18n.T("cli.fix.no_change", res.Remote))
				return nil
			}
			c.println(i18n.T("cli.fix.change", res.Remote, res.Old, res.New, res.Account))
			if dryRun {
				c.println(i18n.T("cli.fix.dryrun"))
			} else {
				c.println(i18n.T("cli.fix.done"))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&account, "account", "", i18n.T("cli.flag.account_optional"))
	cmd.Flags().StringVar(&remoteName, "remote", "origin", i18n.T("cli.flag.remote"))
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, i18n.T("cli.flag.dry_run"))
	cmd.Flags().BoolVar(&force, "force", false, i18n.T("cli.flag.force"))
	return cmd
}

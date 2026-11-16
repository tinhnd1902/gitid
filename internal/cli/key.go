package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/keys"
)

var errPassNeedsTTY = errors.New("key gen: passphrase mode needs a terminal")

var errNoPassRefused = errors.New("key gen: --no-passphrase needs an explicit confirmation (--yes on a non-interactive terminal)")

func (c *cli) keyCmd() *cobra.Command {
	key := &cobra.Command{
		Use:   "key",
		Short: i18n.T("cli.key.short"),
	}
	var noPass, yes, copyKey bool
	gen := &cobra.Command{
		Use:   "gen <company>/<account>",
		Short: i18n.T("cli.key.gen_short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := c.load()
			if err != nil {
				return err
			}
			ref, err := cfg.ParseRef(args[0])
			if err != nil {
				return err
			}
			if !noPass && !c.isTTY() {
				// ssh-keygen would read EOF as an empty passphrase
				return errPassNeedsTTY
			}
			if noPass && !yes {
				if !c.isTTY() {
					return errNoPassRefused
				}
				if !c.confirm(i18n.T("cli.key.confirm_nopass")) {
					c.println(i18n.T("cli.apply.cancelled"))
					return nil
				}
			}
			pub, cfg2, err := c.svc.GenerateKey(cmd.Context(), cfg, ref, noPass)
			if err != nil {
				return err
			}
			c.println(i18n.T("cli.key.generated", c.svc.Env.Contract(pub.PrivatePath)))
			c.println(i18n.T("cli.key.pub_header"))
			c.println(pub.Content)
			if copyKey || c.isTTY() {
				if err := c.svc.CopyKey(cmd.Context(), pub.Content); err != nil {
					c.println(i18n.T("cli.key.copy_failed"))
				} else {
					c.println(i18n.T("cli.key.copied"))
				}
			}
			c.println(i18n.T("cli.key.add_at", keys.KeyURL(ref.Account)))
			if p, err := c.svc.BuildPlan(cfg2); err == nil && !p.Empty() {
				c.println(i18n.T("cli.key.apply_hint"))
			}
			return nil
		},
	}
	gen.Flags().BoolVar(&noPass, "no-passphrase", false, i18n.T("cli.flag.no_pass"))
	gen.Flags().BoolVar(&yes, "yes", false, i18n.T("cli.flag.yes"))
	gen.Flags().BoolVar(&copyKey, "copy", false, i18n.T("cli.flag.copy"))
	key.AddCommand(gen)
	return key
}

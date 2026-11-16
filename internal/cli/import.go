package cli

import (
	"errors"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/importer"
	"github.com/tinhnd1902/gitid/internal/platform"
)

var errImportFlags = errors.New("import: --platform and --username are required with --select")

func (c *cli) printProposals(res importer.Result) {
	if len(res.Proposals) == 0 {
		c.println(i18n.T("cli.import.none"))
	} else {
		c.println(i18n.T("cli.import.proposals"))
	}
	for _, p := range res.Proposals {
		c.printf("  %s  %s  %s  <%s>\n", p.Slug, p.BaseDir, p.UserName, p.UserEmail)
		for _, w := range p.Warnings {
			c.println("      ! " + i18n.T(w))
		}
	}
	if len(res.Hosts) > 0 {
		c.println(i18n.T("cli.import.hosts"))
		for _, h := range res.Hosts {
			c.printf("  Host %s", strings.Join(h.Patterns, " "))
			if len(h.IdentityFile) > 0 {
				c.printf("  (IdentityFile %s)", strings.Join(h.IdentityFile, ", "))
			}
			c.println()
		}
	}
}

func (c *cli) importCmd() *cobra.Command {
	var dryRun bool
	var sel, plat, username, auth, host string
	cmd := &cobra.Command{
		Use:   "import",
		Short: i18n.T("cli.import.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := c.load()
			if err != nil {
				return err
			}
			res, err := c.svc.Import(cfg)
			if err != nil {
				return err
			}
			c.printProposals(res)
			if dryRun || sel == "" {
				if !dryRun && len(res.Proposals) > 0 {
					c.println(i18n.T("cli.import.hint_select"))
				}
				return nil
			}
			if plat == "" || username == "" {
				return errImportFlags
			}
			want := map[string]bool{}
			for _, s := range strings.Split(sel, ",") {
				want[strings.TrimSpace(s)] = true
			}
			added := 0
			for _, p := range res.Proposals {
				if !want[p.Slug] {
					continue
				}
				delete(want, p.Slug)
				co := c.svc.ProposalToCompany(cfg, p, app.ImportChoice{Platform: platform.Platform(plat), Username: username, Auth: config.Auth(auth), Host: host})
				cfg = app.AddCompany(cfg, co)
				added++
			}
			if len(want) > 0 {
				var unknown []string
				for s := range want {
					unknown = append(unknown, s)
				}
				sort.Strings(unknown)
				return errors.New(i18n.T("cli.import.unknown_slug", strings.Join(unknown, ", ")))
			}
			if _, _, err := c.svc.Commit(cfg); err != nil {
				return err
			}
			c.println(i18n.T("cli.import.imported", added))
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, i18n.T("cli.flag.dry_run"))
	cmd.Flags().StringVar(&sel, "select", "", i18n.T("cli.flag.select"))
	cmd.Flags().StringVar(&plat, "platform", "", i18n.T("cli.flag.platform"))
	cmd.Flags().StringVar(&username, "username", "", i18n.T("cli.flag.username"))
	cmd.Flags().StringVar(&auth, "auth", "https", i18n.T("cli.flag.auth"))
	cmd.Flags().StringVar(&host, "host", "", i18n.T("cli.flag.host"))
	return cmd
}

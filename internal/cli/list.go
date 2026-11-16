package cli

import (
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/i18n"
)

type listAccount struct {
	ID        string   `json:"id"`
	Platform  string   `json:"platform"`
	Host      string   `json:"host"`
	Username  string   `json:"username"`
	Name      string   `json:"name"`
	Email     string   `json:"email"`
	Auth      string   `json:"auth"`
	Orgs      []string `json:"orgs,omitempty"`
	SSHAlias  string   `json:"ssh_alias,omitempty"`
	SSHKey    string   `json:"ssh_key,omitempty"`
	IsDefault bool     `json:"default"`
}

type listCompany struct {
	Slug     string        `json:"slug"`
	Name     string        `json:"name"`
	BaseDir  string        `json:"base_dir"`
	Accounts []listAccount `json:"accounts"`
}

type listOutput struct {
	Companies []listCompany `json:"companies"`
}

func buildList(cfg config.Config) listOutput {
	out := listOutput{Companies: []listCompany{}}
	for _, c := range cfg.Companies {
		lc := listCompany{Slug: c.Slug, Name: c.Name, BaseDir: c.BaseDir, Accounts: []listAccount{}}
		for _, a := range c.Accounts {
			la := listAccount{
				ID: a.ID, Platform: string(a.Platform), Host: a.EffectiveHost(), Username: a.Username, Name: a.Name,
				Email: a.Email, Auth: string(a.Auth), Orgs: a.Orgs, SSHKey: a.SSHKey, IsDefault: c.DefaultAccount == a.ID,
			}
			if a.Auth == config.AuthSSH {
				la.SSHAlias = c.Alias(a)
			}
			lc.Accounts = append(lc.Accounts, la)
		}
		out.Companies = append(out.Companies, lc)
	}
	return out
}

func (c *cli) listCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: i18n.T("cli.list.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := c.load()
			if err != nil {
				return err
			}
			out := buildList(cfg)
			if asJSON {
				b, err := json.MarshalIndent(out, "", "  ")
				if err != nil {
					return err
				}
				c.println(string(b))
				return nil
			}
			if len(out.Companies) == 0 {
				c.println(i18n.T("cli.list.empty"))
				return nil
			}
			for _, co := range out.Companies {
				c.printf("%s  %s  %s\n", co.Slug, co.Name, co.BaseDir)
				for _, a := range co.Accounts {
					mark := " "
					if a.IsDefault {
						mark = "*"
					}
					c.printf("  %s %-14s %-12s %-6s %s  <%s>\n", mark, a.ID, a.Platform, a.Auth, a.Name, a.Email)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, i18n.T("cli.flag.json"))
	return cmd
}

func (c *cli) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: i18n.T("cli.version.short"),
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { c.println("gitid " + c.d.Version) },
	}
}

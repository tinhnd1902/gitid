package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tinhnd1902/gitid/internal/app"
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/i18n"
	"github.com/tinhnd1902/gitid/internal/online"
)

func (c *cli) tokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: i18n.T("cli.token.short"),
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(c.tokenSetCmd(), c.tokenRmCmd(), c.tokenStatusCmd())
	return cmd
}

// envNameOf is the variable that holds the token of the account ref.
func (c *cli) envNameOf(cfg config.Config, ref string) string {
	if r, err := config.Normalize(cfg, c.d.Env).ParseRef(ref); err == nil {
		return online.EnvName(r.Company.Slug, r.Account.ID)
	}
	return "GITID_TOKEN_*"
}

// keychainError turns an error of the Keychain write into a translated one.
func (c *cli) keychainError(err error, cfg config.Config, ref string) error {
	return c.keychainErrorKey(err, cfg, ref, "cli.token.failed")
}

// keychainErrorKey is keychainError with the message used when the security
// command itself failed.
func (c *cli) keychainErrorKey(err error, cfg config.Config, ref, failedKey string) error {
	switch {
	case errors.Is(err, online.ErrKeychainUnsupported):
		return c.failf("cli.token.unsupported", c.envNameOf(cfg, ref))
	case errors.Is(err, online.ErrBadToken):
		return c.failf("cli.token.bad")
	case errors.Is(err, online.ErrBadKeychainAccount):
		return c.failf("cli.token.bad_account")
	case errors.Is(err, online.ErrKeychainFailed):
		return c.failf(failedKey)
	}
	return err
}

// readToken reads the token: hidden on a terminal, one line on a pipe. It is
// never printed.
func (c *cli) readToken(ref string) (string, error) {
	if c.isTTY() {
		if c.d.ReadSecret == nil {
			return "", c.failf("cli.token.no_secret_tty")
		}
		fmt.Fprint(c.err, i18n.T("cli.token.prompt", ref))
		tok, err := c.d.ReadSecret()
		fmt.Fprintln(c.err)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(tok), nil
	}
	// one line, bounded: a pipe with a huge or endless input is cut
	r := bufio.NewReader(io.LimitReader(c.d.Stdin, online.MaxTokenBytes+16))
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func (c *cli) tokenSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <company/account>",
		Short: i18n.T("cli.token.set.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := c.load()
			if err != nil {
				return err
			}
			ref, err := config.Normalize(cfg, c.d.Env).ParseRef(args[0])
			if err != nil {
				return err
			}
			if c.d.Env.GOOS != "darwin" {
				return c.keychainError(online.ErrKeychainUnsupported, cfg, args[0])
			}
			tok, err := c.readToken(ref.String())
			if err != nil {
				return err
			}
			if tok == "" {
				return c.failf("cli.token.empty")
			}
			if err := c.svc.SetToken(cmd.Context(), cfg, ref.String(), tok); err != nil {
				return c.keychainError(err, cfg, args[0])
			}
			c.println(i18n.T("cli.token.saved", ref.String()))
			return nil
		},
	}
}

func (c *cli) tokenRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <company/account>",
		Short: i18n.T("cli.token.rm.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := c.load()
			if err != nil {
				return err
			}
			ref, err := config.Normalize(cfg, c.d.Env).ParseRef(args[0])
			if err != nil {
				return err
			}
			removed, err := c.svc.RemoveToken(cmd.Context(), cfg, ref.String())
			if err != nil {
				return c.keychainErrorKey(err, cfg, args[0], "cli.token.rm_failed")
			}
			if removed {
				c.println(i18n.T("cli.token.removed", ref.String()))
			} else {
				c.println(i18n.T("cli.token.nothing", ref.String()))
			}
			return nil
		},
	}
}

// tokenStatusJSON is one account in `token status --json`. It names the
// source of the token and never holds the token.
type tokenStatusJSON struct {
	Account           string `json:"account"`
	Platform          string `json:"platform"`
	Host              string `json:"host"`
	Username          string `json:"username"`
	EnvVar            string `json:"env_var"`
	Source            string `json:"source"`
	Note              string `json:"note"`
	LooksLikePassword bool   `json:"looks_like_password"`
}

func (c *cli) tokenStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status [company/account]",
		Short: i18n.T("cli.token.status.short"),
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := c.load()
			if err != nil {
				return err
			}
			accs := c.svc.OnlineAccounts(cfg)
			if len(args) == 1 {
				ref, err := config.Normalize(cfg, c.d.Env).ParseRef(args[0])
				if err != nil {
					return err
				}
				var sel []app.OnlineAccount
				for _, a := range accs {
					if a.Ref.String() == ref.String() {
						sel = append(sel, a)
					}
				}
				accs = sel
			}
			out := []tokenStatusJSON{}
			for _, a := range accs {
				info, err := c.svc.ResolveSource(cmd.Context(), cfg, a.Ref.String())
				if err != nil {
					return err
				}
				out = append(out, tokenStatusJSON{
					Account: a.Ref.String(), Platform: string(a.Account.Platform), Host: a.Account.Host,
					Username: a.Account.Username, EnvVar: a.EnvName, Source: string(info.Source),
					Note: info.Note, LooksLikePassword: info.LooksLikePassword,
				})
			}
			if asJSON {
				b, err := json.MarshalIndent(struct {
					Accounts []tokenStatusJSON `json:"accounts"`
				}{out}, "", "  ")
				if err != nil {
					return err
				}
				c.println(string(b))
				return nil
			}
			for _, s := range out {
				state := s.Source
				switch {
				case s.Source == string(online.SourceNone):
					state = i18n.T("cli.token.status.none", s.EnvVar, s.Account)
				case s.LooksLikePassword:
					state = i18n.T("cli.token.status.pw", s.Source)
				}
				c.line(fmt.Sprintf("%-20s %-12s %s", s.Account, s.Platform, state))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, i18n.T("cli.flag.json"))
	return cmd
}

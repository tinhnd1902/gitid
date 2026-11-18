package app

import (
	"context"
	"fmt"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/execx"
	"github.com/tinhnd1902/gitid/internal/importer"
	"github.com/tinhnd1902/gitid/internal/keys"
	"github.com/tinhnd1902/gitid/internal/platform"
)

// AddCompany appends a company (without saving).
func AddCompany(cfg config.Config, c config.Company) config.Config {
	out := cfg
	out.Companies = append(append([]config.Company(nil), cfg.Companies...), c)
	return out
}

// UpsertAccount adds acc to the company, or replaces the account with the
// same id. The slug and id are immutable (D5): replacing never renames.
func UpsertAccount(cfg config.Config, slug string, acc config.Account) (config.Config, error) {
	_, ci, ok := cfg.FindCompany(slug)
	if !ok {
		return cfg, fmt.Errorf("%w: %s", config.ErrRefNotFound, slug)
	}
	out := cfg
	out.Companies = append([]config.Company(nil), cfg.Companies...)
	c := out.Companies[ci]
	c.Accounts = append([]config.Account(nil), c.Accounts...)
	if _, ai, found := c.FindAccount(acc.ID); found {
		c.Accounts[ai] = acc
	} else {
		c.Accounts = append(c.Accounts, acc)
	}
	out.Companies[ci] = c
	return out, nil
}

// UpdateCompany replaces the company with the same slug (slug immutable:
// callers pass the company they edited; an unknown slug is an error).
func UpdateCompany(cfg config.Config, c config.Company) (config.Config, error) {
	_, ci, ok := cfg.FindCompany(c.Slug)
	if !ok {
		return cfg, fmt.Errorf("%w: %s", ErrImmutableID, c.Slug)
	}
	out := cfg
	out.Companies = append([]config.Company(nil), cfg.Companies...)
	out.Companies[ci] = c
	return out, nil
}

// DeleteResult describes a deletion.
type DeleteResult struct {
	Config      config.Config
	KeysRemoved []string
	BackupDir   string
}

// DeleteAccount removes an account from the config and saves it. Key files
// are only deleted when keepKey is false AND the key lies under
// ~/.ssh/gitid/; the default is to keep them.
func (s *Service) DeleteAccount(cfg config.Config, ref config.AccountRef, keepKey bool) (DeleteResult, error) {
	cfg = config.Normalize(cfg, s.Env)
	_, ci, ok := cfg.FindCompany(ref.Company.Slug)
	if !ok {
		return DeleteResult{}, config.ErrRefNotFound
	}
	out := cfg
	out.Companies = append([]config.Company(nil), cfg.Companies...)
	c := out.Companies[ci]
	_, ai, found := c.FindAccount(ref.Account.ID)
	if !found {
		return DeleteResult{}, config.ErrRefNotFound
	}
	c.Accounts = append(append([]config.Account(nil), c.Accounts[:ai]...), c.Accounts[ai+1:]...)
	if c.DefaultAccount == ref.Account.ID {
		c.DefaultAccount = ""
		if len(c.Accounts) > 0 {
			c.DefaultAccount = c.Accounts[0].ID
		}
	}
	out.Companies[ci] = c
	saved, dir, err := s.Commit(out)
	if err != nil {
		return DeleteResult{}, err
	}
	res := DeleteResult{Config: saved, BackupDir: dir}
	if !keepKey && ref.Account.SSHKey != "" {
		if err := keys.RemoveFiles(s.Env, ref.Account.SSHKey); err != nil {
			return res, err
		}
		res.KeysRemoved = append(res.KeysRemoved, ref.Account.SSHKey)
	}
	return res, nil
}

// DeleteCompany removes a company with all its accounts (keys kept unless
// keepKeys is false).
func (s *Service) DeleteCompany(cfg config.Config, slug string, keepKeys bool) (DeleteResult, error) {
	cfg = config.Normalize(cfg, s.Env)
	comp, ci, ok := cfg.FindCompany(slug)
	if !ok {
		return DeleteResult{}, config.ErrRefNotFound
	}
	out := cfg
	out.Companies = append(append([]config.Company(nil), cfg.Companies[:ci]...), cfg.Companies[ci+1:]...)
	saved, dir, err := s.Commit(out)
	if err != nil {
		return DeleteResult{}, err
	}
	res := DeleteResult{Config: saved, BackupDir: dir}
	if !keepKeys {
		for _, a := range comp.Accounts {
			if a.SSHKey == "" {
				continue
			}
			if err := keys.RemoveFiles(s.Env, a.SSHKey); err != nil {
				return res, err
			}
			res.KeysRemoved = append(res.KeysRemoved, a.SSHKey)
		}
	}
	return res, nil
}

// PrepareKey runs the pre-checks and returns the ssh-keygen command (for
// tea.ExecProcess or RunTTY) and the private key path.
func (s *Service) PrepareKey(ref config.AccountRef, noPass bool) (execx.Cmd, string, error) {
	if ref.Account.Auth != config.AuthSSH {
		return execx.Cmd{}, "", ErrNotSSH
	}
	path, err := keys.Prepare(s.Env, ref.Company, ref.Account)
	if err != nil {
		return execx.Cmd{}, path, err
	}
	cmd, err := keys.Cmd(s.Env, ref.Company, ref.Account, noPass)
	return cmd, path, err
}

// FinishKey verifies the generated key, records ssh_key in the config and
// saves it. The public key is returned; the private key is never read.
func (s *Service) FinishKey(cfg config.Config, ref config.AccountRef) (keys.Pub, config.Config, error) {
	path := keys.PrivatePath(s.Env, ref.Company, ref.Account)
	pub, err := keys.Finish(path)
	if err != nil {
		return keys.Pub{}, cfg, err
	}
	acc := ref.Account
	acc.SSHKey = s.Env.Contract(path)
	cfg2, err := UpsertAccount(config.Normalize(cfg, s.Env), ref.Company.Slug, acc)
	if err != nil {
		return pub, cfg, err
	}
	saved, _, err := s.Commit(cfg2)
	return pub, saved, err
}

// GenerateKey runs the whole key flow with the process stdio attached.
func (s *Service) GenerateKey(ctx context.Context, cfg config.Config, ref config.AccountRef, noPass bool) (keys.Pub, config.Config, error) {
	cmd, _, err := s.PrepareKey(ref, noPass)
	if err != nil {
		return keys.Pub{}, cfg, err
	}
	if err := s.Run.RunTTY(ctx, cmd); err != nil {
		return keys.Pub{}, cfg, fmt.Errorf("ssh-keygen: %w", err)
	}
	return s.FinishKey(cfg, ref)
}

// CopyKey copies the public key to the clipboard.
func (s *Service) CopyKey(ctx context.Context, pub string) error {
	return keys.Copy(ctx, s.Env, s.Run, pub)
}

// CopyText copies text (a path, a URL) to the clipboard.
func (s *Service) CopyText(ctx context.Context, text string) error {
	return keys.Copy(ctx, s.Env, s.Run, text)
}

// OpenKeyURL opens the platform's "add SSH key" page.
func (s *Service) OpenKeyURL(ctx context.Context, a config.Account) error {
	return keys.OpenURL(ctx, s.Env, s.Run, keys.KeyURL(a))
}

// ImportChoice is what the user supplies for an imported company.
type ImportChoice struct {
	Platform platform.Platform
	Username string
	Auth     config.Auth
	Host     string
}

// ProposalToCompany turns an import proposal into a company with one
// account. It does not validate; Commit does.
func (s *Service) ProposalToCompany(cfg config.Config, p importer.Proposal, ch ImportChoice) config.Company {
	c := config.Company{Slug: p.Slug, Name: p.Name, BaseDir: p.BaseDir}
	acc := config.Account{
		Platform: ch.Platform, Host: ch.Host, Username: ch.Username, Name: p.UserName, Email: p.UserEmail, Auth: ch.Auth,
	}
	if acc.Auth == "" {
		acc.Auth = config.AuthHTTPS
	}
	acc.ID = config.DefaultAccountID(c, ch.Platform, ch.Username)
	c.Accounts = []config.Account{acc}
	c.DefaultAccount = acc.ID
	return c
}

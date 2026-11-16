// Package fixtures builds the account fixtures used across tests (see
// test-cases.md).
package fixtures

import (
	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/platform"
)

// Acme is company acme: alice (https, default), bob (https), carol (ssh).
func Acme() config.Company {
	return config.Company{
		Slug: "acme", Name: "ACME Corp", BaseDir: "~/Code/acme/", DefaultAccount: "alice",
		Accounts: []config.Account{
			{ID: "alice", Platform: platform.GitHub, Username: "alice", Name: "Alice Nguyen", Email: "alice@acme.com", Auth: config.AuthHTTPS},
			{ID: "bob", Platform: platform.GitHub, Username: "bob", Name: "Bob Tran", Email: "bob@acme.com", Auth: config.AuthHTTPS},
			{ID: "carol", Platform: platform.GitHub, Username: "carol", Name: "Carol Le", Email: "carol@acme.com", Auth: config.AuthSSH, SSHKey: "~/.ssh/gitid/acme-carol"},
		},
	}
}

// Globex is company globex: dave on a self-hosted GitLab over ssh, port 2222.
func Globex() config.Company {
	return config.Company{
		Slug: "globex", Name: "Globex", BaseDir: "~/Code/globex/", DefaultAccount: "dave",
		Accounts: []config.Account{
			{ID: "dave", Platform: platform.GitLab, Host: "git.example.com", SSHPort: 2222, Username: "dave", Name: "Dave D", Email: "dave@globex.example", Auth: config.AuthSSH, SSHKey: "~/.ssh/gitid/globex-dave"},
		},
	}
}

// Beta is company beta, with the same username (alice) as acme/alice.
func Beta() config.Company {
	return config.Company{
		Slug: "beta", Name: "Beta", BaseDir: "~/Code/beta/", DefaultAccount: "alice",
		Accounts: []config.Account{
			{ID: "alice", Platform: platform.GitHub, Username: "alice", Name: "Alice B", Email: "alice@beta.io", Auth: config.AuthHTTPS},
		},
	}
}

// Azure is company az with an https and an ssh azure-devops account.
func Azure() config.Company {
	return config.Company{
		Slug: "az", Name: "Azure Co", BaseDir: "~/Code/az/", DefaultAccount: "dev",
		Accounts: []config.Account{
			{ID: "dev", Platform: platform.AzureDevOps, Username: "dev", Name: "Dev One", Email: "dev@az.example", Auth: config.AuthHTTPS},
			{ID: "devssh", Platform: platform.AzureDevOps, Username: "devssh", Name: "Dev Two", Email: "devssh@az.example", Auth: config.AuthSSH, SSHKey: "~/.ssh/gitid/az-devssh"},
		},
	}
}

// Bitbucket is company bb with joe.
func Bitbucket() config.Company {
	return config.Company{
		Slug: "bb", Name: "BB", BaseDir: "~/Code/bb/", DefaultAccount: "joe",
		Accounts: []config.Account{
			{ID: "joe", Platform: platform.Bitbucket, Username: "joe", Name: "Joe", Email: "joe@bb.example", Auth: config.AuthHTTPS},
		},
	}
}

// Gitea is company gt with sam on gitea.example.com.
func Gitea() config.Company {
	return config.Company{
		Slug: "gt", Name: "GT", BaseDir: "~/Code/gt/", DefaultAccount: "sam",
		Accounts: []config.Account{
			{ID: "sam", Platform: platform.Gitea, Host: "gitea.example.com", Username: "sam", Name: "Sam", Email: "sam@gt.example", Auth: config.AuthHTTPS},
		},
	}
}

// Basic is acme, globex and beta.
func Basic() config.Config {
	cfg := config.Default()
	cfg.Companies = []config.Company{Acme(), Globex(), Beta()}
	return cfg
}

// Full contains every fixture company.
func Full() config.Config {
	cfg := Basic()
	cfg.Companies = append(cfg.Companies, Azure(), Bitbucket(), Gitea())
	return cfg
}

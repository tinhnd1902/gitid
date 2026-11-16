package remote

import (
	"net/url"

	"github.com/tinhnd1902/gitid/internal/config"
	"github.com/tinhnd1902/gitid/internal/platform"
)

// Resolve maps a remote whose host is a gitid ssh alias onto the aliased
// account: Host becomes the account's https host, Port is cleared and the
// layout is re-interpreted for the account's platform. The second result is
// the aliased account (nil if the host is not an alias).
func Resolve(cfg config.Config, r Remote) (Remote, *config.AccountRef) {
	for _, ref := range cfg.All() {
		a := ref.Account
		if a.Auth != config.AuthSSH || ref.Alias() != r.Host {
			continue
		}
		out := r
		out.Host, out.Port, out.Platform = a.HostName(), 0, a.Platform
		if err := out.interpret(a.Platform == platform.AzureDevOps); err != nil {
			return r, nil
		}
		ref := ref
		return out, &ref
	}
	return r, nil
}

func userinfo(username string) string { return url.User(username).String() }

// hostMatches reports whether the (resolved) remote lives on the account's
// host. Azure cloud hosts (dev.azure.com, ssh.dev.azure.com, legacy
// visualstudio.com) all match an azure account on dev.azure.com.
func hostMatches(a config.Account, r Remote) bool {
	if a.Platform == platform.AzureDevOps && a.HostName() == "dev.azure.com" && r.Platform == platform.AzureDevOps && azureHost(r.Host) {
		return true
	}
	switch r.Kind {
	case KindHTTPS, KindHTTP:
		return r.HostPort() == a.EffectiveHost()
	default:
		return r.Host == a.HostName() || r.Host == a.EffectiveSSHHost()
	}
}

// OnHost reports whether the (Resolve()d) remote lives on the account's host.
func OnHost(a config.Account, r Remote) bool { return hostMatches(a, r) }

// Options for Render/Rewrite.
type Options struct {
	// Force renders even when the remote's host differs from the account's.
	Force bool
}

// Render renders the remote for the given account: https with the account's
// username, or the ssh alias form. The remote must already be Resolve()d.
func Render(ref config.AccountRef, r Remote, opts Options) (string, error) {
	a := ref.Account
	if !hostMatches(a, r) && !opts.Force {
		return "", ErrHostMismatch
	}
	azure := a.Platform == platform.AzureDevOps
	if r.Azure != azure {
		// reinterpret the path for the account's layout
		if err := r.interpret(azure); err != nil {
			if !opts.Force {
				return "", ErrHostMismatch
			}
			return "", err
		}
	}
	var path string
	if azure {
		if a.Auth == config.AuthSSH {
			path = "v3/" + r.AzOrg + "/" + r.AzProject + "/" + r.Repo
		} else {
			path = r.AzOrg + "/" + r.AzProject + "/" + azureLeaf + "/" + r.Repo
		}
	} else {
		path = r.Owner + "/" + r.Repo
		if r.HadGitSuffix {
			path += ".git"
		}
	}
	if a.Auth == config.AuthSSH {
		return "git@" + ref.Alias() + ":" + path, nil
	}
	scheme := "https"
	if r.Kind == KindHTTP {
		scheme = "http" // keep the scheme the user typed (warned about by Parse)
	}
	return scheme + "://" + userinfo(a.Username) + "@" + a.EffectiveHost() + "/" + path, nil
}

// Rewrite parses input, resolves aliases and renders it for the account.
// The parsed (resolved) remote is returned for warnings and display.
func Rewrite(cfg config.Config, ref config.AccountRef, input string, opts Options) (string, Remote, error) {
	r, err := Parse(input)
	if err != nil {
		return "", Remote{}, err
	}
	r, _ = Resolve(cfg, r)
	out, err := Render(ref, r, opts)
	return out, r, err
}

// Patterns returns the hasconfig:remote.*.url globs that select the account
// (git wildmatch rules; "**" is only special next to "/").
func Patterns(ref config.AccountRef) []string {
	a := ref.Account
	if a.Auth == config.AuthSSH {
		al := ref.Alias()
		return []string{
			"git@" + al + ":*/**",
			al + ":*/**",
			"ssh://git@" + al + "/**",
			"ssh://" + al + "/**",
		}
	}
	base := "https://" + userinfo(a.Username) + "@" + a.EffectiveHost() + "/"
	if len(a.Orgs) == 0 {
		return []string{base + "**"}
	}
	out := make([]string, 0, len(a.Orgs))
	for _, o := range a.Orgs {
		out = append(out, base+o+"/**")
	}
	return out
}

// Package platform knows the supported git hosting platforms and their
// defaults.
package platform

import "strings"

// Platform identifies a hosting platform.
type Platform string

// Supported platforms.
const (
	GitHub      Platform = "github"
	GitLab      Platform = "gitlab"
	Bitbucket   Platform = "bitbucket"
	Gitea       Platform = "gitea"
	AzureDevOps Platform = "azure-devops"
)

// All returns the supported platforms in display order.
func All() []Platform {
	return []Platform{GitHub, GitLab, Bitbucket, Gitea, AzureDevOps}
}

// Valid reports whether p is a supported platform.
func (p Platform) Valid() bool {
	for _, q := range All() {
		if p == q {
			return true
		}
	}
	return false
}

// DisplayName is the human name.
func (p Platform) DisplayName() string {
	switch p {
	case GitHub:
		return "GitHub"
	case GitLab:
		return "GitLab"
	case Bitbucket:
		return "Bitbucket"
	case Gitea:
		return "Gitea"
	case AzureDevOps:
		return "Azure DevOps"
	}
	return string(p)
}

// DefaultHost is the public host of the platform ("" when the user must
// supply one, as for Gitea).
func DefaultHost(p Platform) string {
	switch p {
	case GitHub:
		return "github.com"
	case GitLab:
		return "gitlab.com"
	case Bitbucket:
		return "bitbucket.org"
	case AzureDevOps:
		return "dev.azure.com"
	}
	return ""
}

// DefaultSSHHost is the host to ssh to for the given https host. Azure
// DevOps uses a dedicated ssh host.
func DefaultSSHHost(p Platform, host string) string {
	if p == AzureDevOps && (host == "" || host == "dev.azure.com") {
		return "ssh.dev.azure.com"
	}
	return host
}

// SSHUser is the user part of ssh remotes ("git" everywhere).
func SSHUser(Platform) string { return "git" }

// KeySettingsURL is the page where a user adds an SSH public key. org is
// only used for Azure DevOps and may be empty.
func KeySettingsURL(p Platform, host, org string) string {
	if host == "" {
		host = DefaultHost(p)
	}
	switch p {
	case GitHub:
		return "https://" + host + "/settings/ssh/new"
	case GitLab:
		return "https://" + host + "/-/user_settings/ssh_keys"
	case Bitbucket:
		return "https://bitbucket.org/account/settings/ssh-keys/"
	case Gitea:
		return "https://" + host + "/user/settings/keys"
	case AzureDevOps:
		if org != "" {
			return "https://dev.azure.com/" + strings.Trim(org, "/") + "/_usersSettings/keys"
		}
		return "https://dev.azure.com/"
	}
	return ""
}

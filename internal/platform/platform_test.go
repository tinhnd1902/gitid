package platform

import "testing"

func TestPlatform(t *testing.T) {
	if len(All()) != 5 {
		t.Fatal("platforms")
	}
	for _, p := range All() {
		if !p.Valid() || p.DisplayName() == "" || SSHUser(p) != "git" {
			t.Fatalf("%s", p)
		}
	}
	if Platform("svn").Valid() || Platform("svn").DisplayName() != "svn" {
		t.Fatal("invalid platform")
	}
	hosts := map[Platform]string{GitHub: "github.com", GitLab: "gitlab.com", Bitbucket: "bitbucket.org", Gitea: "", AzureDevOps: "dev.azure.com"}
	for p, h := range hosts {
		if DefaultHost(p) != h {
			t.Errorf("DefaultHost(%s)=%q", p, DefaultHost(p))
		}
	}
	if DefaultSSHHost(AzureDevOps, "dev.azure.com") != "ssh.dev.azure.com" || DefaultSSHHost(AzureDevOps, "") != "ssh.dev.azure.com" {
		t.Fatal("azure ssh host")
	}
	if DefaultSSHHost(GitLab, "git.example.com") != "git.example.com" {
		t.Fatal("ssh host")
	}
}

func TestKeySettingsURL(t *testing.T) { // K8
	cases := []struct {
		p         Platform
		host, org string
		want      string
	}{
		{GitHub, "github.com", "", "https://github.com/settings/ssh/new"},
		{GitHub, "", "", "https://github.com/settings/ssh/new"},
		{GitLab, "gitlab.com", "", "https://gitlab.com/-/user_settings/ssh_keys"},
		{GitLab, "git.example.com", "", "https://git.example.com/-/user_settings/ssh_keys"},
		{Bitbucket, "bitbucket.org", "", "https://bitbucket.org/account/settings/ssh-keys/"},
		{Gitea, "gitea.example.com", "", "https://gitea.example.com/user/settings/keys"},
		{AzureDevOps, "dev.azure.com", "myorg", "https://dev.azure.com/myorg/_usersSettings/keys"},
		{AzureDevOps, "dev.azure.com", "", "https://dev.azure.com/"},
		{Platform("x"), "h", "", ""},
	}
	for _, c := range cases {
		if got := KeySettingsURL(c.p, c.host, c.org); got != c.want {
			t.Errorf("%v %q: %q want %q", c.p, c.host, got, c.want)
		}
	}
}

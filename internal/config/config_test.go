package config

import "testing"

func TestParseServers(t *testing.T) {
	raw := `[
		{"host":"git.corp.com","kind":"gitea","token":"pat_x"},
		{"host":"https://git2.corp.com/group","kind":"gitea","token":"pat_y"},
		{"host":"","token":"drop-me"},
		{"host":"git.corp.com","token":"dup-dropped"},
		{"host":"gitea.example.com","kind":"gitea","token":"gt","user":"alice"}
	]`
	got, err := ParseServers(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d servers, want 3: %+v", len(got), got)
	}
	// The URL host is normalized to a bare hostname.
	if got[1].Host != "git2.corp.com" {
		t.Errorf("host normalize = %q", got[1].Host)
	}
	// The kind is preserved as configured (github/gitlab are no longer distinct
	// kinds — only gitea and generic remain).
	if got[0].Kind != KindGitea {
		t.Errorf("kind = %q", got[0].Kind)
	}
}

func TestParseServersEmptyAndBad(t *testing.T) {
	if s, err := ParseServers(""); err != nil || s != nil {
		t.Errorf("empty => %v %v", s, err)
	}
	if _, err := ParseServers("not json"); err == nil {
		t.Error("malformed should error")
	}
}

func TestAuthUser(t *testing.T) {
	cases := []struct {
		s    Server
		want string
	}{
		// No user configured: the PAT placeholder Gitea accepts. A bare "token"
		// made Gitea look up a user by that name, fail, and answer the git client
		// with "remote: Verify".
		{Server{Kind: KindGitea}, "x-access-token"},
		{Server{Kind: KindGeneric}, "x-access-token"},
		{Server{Kind: KindGitea, User: "alice"}, "alice"},
	}
	for _, c := range cases {
		if got := c.s.AuthUser(); got != c.want {
			t.Errorf("AuthUser(%+v) = %q, want %q", c.s, got, c.want)
		}
	}
}

func TestHostOf(t *testing.T) {
	cases := map[string]string{
		"https://github.com/org/repo.git": "github.com",
		"http://gitlab.corp.com:8080/a/b": "gitlab.corp.com",
		"git@github.com:org/repo.git":     "github.com",
		"ssh://git@gitea.example.com/a/b": "gitea.example.com",
		"/local/path/repo":                "",
	}
	for in, want := range cases {
		if got := HostOf(in); got != want {
			t.Errorf("HostOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestServerForHostAndParse(t *testing.T) {
	cfg, err := Parse(func(name string) string {
		switch name {
		case KnobServers:
			return `[{"host":"github.com","kind":"github","token":"t"}]`
		case KnobGitProtocol:
			return ""
		case KnobDefaultBranch:
			return ""
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitProtocol != "https" || cfg.DefaultBranch != "main" {
		t.Errorf("defaults = %+v", cfg)
	}
	if _, ok := cfg.ServerForHost("github.com"); !ok {
		t.Error("github.com should match")
	}
	if _, ok := cfg.ServerForHost("gitlab.com"); ok {
		t.Error("gitlab.com should not match")
	}
}

func TestParseRejectsBadProtocol(t *testing.T) {
	_, err := Parse(func(name string) string {
		if name == KnobGitProtocol {
			return "ftp"
		}
		return ""
	})
	if err == nil {
		t.Error("bad git-protocol should error")
	}
}

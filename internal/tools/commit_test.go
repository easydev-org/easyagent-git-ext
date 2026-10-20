package tools

import "testing"

// TestGitIdentity: `git commit` refuses to run without a name and email, and a
// fresh sandbox has none — so a commit would fail with "Author identity unknown"
// and, before the fix, the tool still reported success because it only looked at
// `git rev-parse`.
func TestGitIdentity(t *testing.T) {
	cases := []struct {
		author, wantName, wantEmail string
	}{
		// No author: the extension's own identity, so a commit always names who
		// made it (a merge request with anonymous commits is not reviewable).
		{"", "easyagent", "agent@easyagent.local"},
		{"Alice <alice@example.com>", "Alice", "alice@example.com"},
		{"Alice", "Alice", "agent@easyagent.local"},
		// Degenerate forms must still produce a usable identity.
		{"<bob@example.com>", "easyagent", "bob@example.com"},
		{"Carol <>", "Carol", "agent@easyagent.local"},
	}
	for _, c := range cases {
		name, email := gitIdentity(c.author)
		if name != c.wantName || email != c.wantEmail {
			t.Errorf("gitIdentity(%q) = (%q, %q), want (%q, %q)",
				c.author, name, email, c.wantName, c.wantEmail)
		}
	}
}

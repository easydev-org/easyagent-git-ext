package ghclient

import "testing"

// Remote parsing decides which repository every call addresses, so a wrong answer here acts on
// somebody else's repository. The three shapes a remote takes all have to work.
func TestSplitRemote(t *testing.T) {
	ok := map[string][2]string{
		"https://github.com/owner/repo":       {"owner", "repo"},
		"https://github.com/owner/repo.git":   {"owner", "repo"},
		"https://github.com/owner/repo/":      {"owner", "repo"},
		"git@github.com:owner/repo.git":       {"owner", "repo"},
		"ssh://git@github.com/owner/repo.git": {"owner", "repo"},
		"https://github.company.com/a/b.git":  {"a", "b"},
	}
	for in, want := range ok {
		owner, repo, err := SplitRemote(in)
		if err != nil {
			t.Errorf("SplitRemote(%q): %v", in, err)
			continue
		}
		if owner != want[0] || repo != want[1] {
			t.Errorf("SplitRemote(%q) = %s/%s, want %s/%s", in, owner, repo, want[0], want[1])
		}
	}

	// Anything that cannot name a repository must be refused rather than guessed at.
	for _, bad := range []string{"", "owner", "https://github.com/", "not a url", "git@host"} {
		if _, _, err := SplitRemote(bad); err == nil {
			t.Errorf("SplitRemote(%q) should have failed", bad)
		}
	}
}

// CheckTokenCanDelete exists so a missing scope is reported BEFORE the call: GitHub answers 403
// for both "no permission" and "not found" on DELETE, which reads as "already gone".
func TestCheckTokenCanDelete(t *testing.T) {
	if err := CheckTokenCanDelete(Scopes{Classic: []string{"repo", "read:org"}}); err == nil {
		t.Error("a token without delete_repo must be refused")
	}
	if err := CheckTokenCanDelete(Scopes{Classic: []string{"repo", "delete_repo"}}); err != nil {
		t.Errorf("delete_repo should pass: %v", err)
	}
	// Fine-grained tokens express this per repository, so there is no local answer and the API
	// has to decide.
	if err := CheckTokenCanDelete(Scopes{FineGrained: true}); err != nil {
		t.Errorf("fine-grained tokens must be left to the API: %v", err)
	}
}

// The CI summary has to give ONE verdict a caller can branch on.
func TestChecksState(t *testing.T) {
	cases := []struct {
		name string
		in   Checks
		want string
	}{
		{"nothing configured", Checks{}, "none"},
		{"all passed", Checks{Runs: []CheckRun{{Status: "completed", Conclusion: "success"}}}, "success"},
		{"one failed", Checks{Runs: []CheckRun{
			{Status: "completed", Conclusion: "success"},
			{Status: "completed", Conclusion: "failure"},
		}}, "failure"},
		{"still running", Checks{Runs: []CheckRun{{Status: "in_progress"}}}, "pending"},
		// A failure must dominate a pending result: waiting for the rest tells the caller
		// nothing they need once something has already failed.
		{"failed while others run", Checks{Runs: []CheckRun{
			{Status: "in_progress"},
			{Status: "completed", Conclusion: "failure"},
		}}, "failure"},
		{"timed out counts as failure", Checks{Runs: []CheckRun{{Status: "completed", Conclusion: "timed_out"}}}, "failure"},
		{"cancelled counts as failure", Checks{Runs: []CheckRun{{Status: "completed", Conclusion: "cancelled"}}}, "failure"},
		{"skipped is not a failure", Checks{Runs: []CheckRun{{Status: "completed", Conclusion: "skipped"}}}, "success"},
		// The older status API must be considered too, or a repository on a third-party CI
		// would be reported green.
		{"status context failed", Checks{Statuses: []Status{{State: "failure"}}}, "failure"},
		{"status context pending", Checks{Statuses: []Status{{State: "pending"}}}, "pending"},
	}
	for _, c := range cases {
		if got := c.in.State(); got != c.want {
			t.Errorf("%s: State() = %q, want %q", c.name, got, c.want)
		}
	}
}

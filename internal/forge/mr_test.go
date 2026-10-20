package forge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOpenPullRequestSendsHeadAndBase: the MR must name the exact head branch the
// caller just pushed. A wrong field here means the forge opens a request against
// a branch that does not exist, and the agent's work silently never lands.
func TestOpenPullRequestSendsHeadAndBase(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/version") {
			_, _ = w.Write([]byte(`{"version":"1.22.0"}`))
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pulls") {
			_ = json.NewDecoder(r.Body).Decode(&got)
			_, _ = w.Write([]byte(`{"number":7,"title":"t","state":"open","html_url":"http://x/pr/7",` +
				`"head":{"ref":"agent/s-1"},"base":{"ref":"main"}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c, err := New(srv.URL, "tok", "https://git.example.com/owner/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	pr, err := c.OpenPullRequest(context.Background(), "main", "agent/s-1", "My title", "Body")
	if err != nil {
		t.Fatal(err)
	}
	if got["head"] != "agent/s-1" || got["base"] != "main" {
		t.Errorf("posted head=%v base=%v, want agent/s-1 -> main", got["head"], got["base"])
	}
	if got["title"] != "My title" {
		t.Errorf("title = %v", got["title"])
	}
	if pr.Index != 7 || pr.Head != "agent/s-1" || pr.Base != "main" {
		t.Errorf("parsed PR = %+v", pr)
	}
}

// TestListPullRequestsParsesIndex: the SDK names the PR number `Index` while its
// JSON key is "number"; mixing them up reported every MR as #0.
func TestListPullRequestsParsesIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/version") {
			_, _ = w.Write([]byte(`{"version":"1.22.0"}`))
			return
		}
		_, _ = w.Write([]byte(`[{"number":12,"title":"a","state":"open",` +
			`"head":{"ref":"agent/x"},"base":{"ref":"main"}}]`))
	}))
	defer srv.Close()

	c, err := New(srv.URL, "tok", "https://git.example.com/owner/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	prs, err := c.ListPullRequests(context.Background(), "open")
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 || prs[0].Index != 12 {
		t.Fatalf("PRs = %+v, want one with Index 12", prs)
	}
	if prs[0].Head != "agent/x" || prs[0].Base != "main" {
		t.Errorf("head/base = %q/%q", prs[0].Head, prs[0].Base)
	}
}

// TestOpenPullRequestSurfacesForgeError: a rejected MR (e.g. "no commits between
// branches") must reach the caller as a readable message, not a nil error.
func TestOpenPullRequestSurfacesForgeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/version") {
			_, _ = w.Write([]byte(`{"version":"1.22.0"}`))
			return
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"no commits between main and agent/x"}`))
	}))
	defer srv.Close()

	c, err := New(srv.URL, "tok", "https://git.example.com/owner/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.OpenPullRequest(context.Background(), "main", "agent/x", "t", ""); err == nil {
		t.Fatal("a rejected MR must return an error")
	}
}

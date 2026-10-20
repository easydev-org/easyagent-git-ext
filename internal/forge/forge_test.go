package forge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSplitRepoPath: a remote URL reaches this package in several shapes, and
// getting the split wrong would silently query the wrong repository.
func TestSplitRepoPath(t *testing.T) {
	cases := []struct{ in, owner, repo string }{
		{"https://git.example.com/owner/repo.git", "owner", "repo"},
		{"https://git.example.com/owner/repo", "owner", "repo"},
		{"https://git.example.com/owner/repo/", "owner", "repo"},
		{"git@git.example.com:owner/repo.git", "owner", "repo"},
		// Gitea allows nested owners, so the repo is the LAST segment.
		{"https://git.example.com/org/sub/repo.git", "org/sub", "repo"},
	}
	for _, c := range cases {
		owner, repo, err := splitRepoPath(c.in)
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.in, err)
			continue
		}
		if owner != c.owner || repo != c.repo {
			t.Errorf("%s -> (%q,%q), want (%q,%q)", c.in, owner, repo, c.owner, c.repo)
		}
	}
	if _, _, err := splitRepoPath("https://git.example.com/onlyone"); err == nil {
		t.Error("a URL without owner/repo must error, not guess")
	}
}

// TestListRefsPaginates: the SDK takes an explicit page and does NOT iterate, so
// a repository with more refs than one page would silently under-report if the
// wrapper forgot to advance. This server returns a full first page plus a short
// second one, and the test asserts BOTH pages arrive.
func TestListRefsPaginates(t *testing.T) {
	var branchPages int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/version"):
			// The SDK probes the API version when a client is constructed, so a
			// fake server has to answer it or every call fails before it starts.
			_, _ = w.Write([]byte(`{"version":"1.22.0"}`))
		case strings.HasSuffix(r.URL.Path, "/branches"):
			branchPages++
			// Page 1 is a FULL page (2 of pageSize 2 would be ideal, but pageSize
			// is a const; use a full page of 50 then a short one).
			page := r.URL.Query().Get("page")
			if page == "1" {
				items := make([]string, listPageSize)
				for i := range items {
					items[i] = `{"name":"b` + itoa(i) + `","commit":{"id":"sha` + itoa(i) + `"}}`
				}
				_, _ = w.Write([]byte("[" + strings.Join(items, ",") + "]"))
				return
			}
			_, _ = w.Write([]byte(`[{"name":"last","commit":{"id":"shalast"}}]`))
		case strings.HasSuffix(r.URL.Path, "/tags"):
			_, _ = w.Write([]byte(`[{"name":"v1","id":"tagobj","commit":{"sha":"shav1"}}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c, err := New(srv.URL, "", "https://git.example.com/owner/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	refs, err := c.ListRefs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if branchPages != 2 {
		t.Errorf("asked for %d branch pages, want 2 (pagination must advance)", branchPages)
	}
	if len(refs) != listPageSize+2 {
		t.Errorf("got %d refs, want %d branches + 1 tag +1 more", len(refs), listPageSize)
	}
	// Branches come first, sorted; then tags. A tag's commit comes from
	// CommitMeta.SHA (not ID).
	if refs[0].Kind != "branch" {
		t.Errorf("first ref kind = %q, want branch", refs[0].Kind)
	}
	last := refs[len(refs)-1]
	if last.Kind != "tag" || last.Name != "v1" || last.Commit != "shav1" {
		t.Errorf("tag ref = %+v, want v1/shav1 (CommitMeta.SHA)", last)
	}
}

// TestReadFileNotFoundIsActionable: the SDK's bare "target couldn't be found"
// does not tell a caller that a missing token is an equally likely cause, so the
// wrapper adds it.
func TestReadFileNotFoundIsActionable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/version") {
			_, _ = w.Write([]byte(`{"version":"1.22.0"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"The target couldn't be found."}`))
	}))
	defer srv.Close()

	c, err := New(srv.URL, "", "https://git.example.com/owner/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.ReadFile(context.Background(), "README.md", "main")
	if err == nil {
		t.Fatal("expected an error for a 404")
	}
	if !strings.Contains(err.Error(), "token can read") {
		t.Errorf("error must mention the token as a possible cause, got: %v", err)
	}
}

// TestNewRequiresBase: without a base URL there is nothing to talk to, and that
// must be a clear error rather than a request to a relative URL.
func TestNewRequiresBase(t *testing.T) {
	if _, err := New("", "tok", "https://git.example.com/o/r.git"); err == nil {
		t.Error("an empty base URL must be rejected")
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

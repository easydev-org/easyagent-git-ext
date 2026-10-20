package forge

import (
	"context"
	"os"
	"testing"
)

// TestLiveForgejo exercises the wrapper against the real dev forgejo, over its
// public repository. It is skipped unless GITEA_LIVE_URL is set, so `go test`
// stays hermetic while a live check remains one env var away.
//
// WHY a live check at all: the SDK's assumptions (path encoding, pagination
// parameter, error text) only fail against a real server. The unit tests fake
// those, which is exactly how a wrong assumption survives them.
func TestLiveForgejo(t *testing.T) {
	base := os.Getenv("GITEA_LIVE_URL")
	remote := os.Getenv("GITEA_LIVE_REMOTE")
	if base == "" || remote == "" {
		t.Skip("set GITEA_LIVE_URL and GITEA_LIVE_REMOTE to run against a live forge")
	}
	cl, err := New(base, os.Getenv("GITEA_LIVE_TOKEN"), remote)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	refs, err := cl.ListRefs(ctx)
	if err != nil {
		t.Fatalf("ListRefs: %v", err)
	}
	if len(refs) == 0 {
		t.Error("ListRefs returned nothing for a repository that has branches")
	}
	for _, r := range refs {
		t.Logf("  %-8s %-20s %s", r.Kind, r.Name, r.Commit)
	}

	f, err := cl.ReadFile(ctx, "README.md", "")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if f.Size == 0 {
		t.Error("ReadFile returned an empty file for a non-empty README")
	}
	t.Logf("README.md: %d bytes", f.Size)

	// A directory is not a file: it must be a clear error.
	if _, err := cl.ReadFile(ctx, ".", ""); err == nil {
		t.Error("reading a directory as a file must fail")
	} else {
		t.Logf("directory read error (expected): %v", err)
	}
}

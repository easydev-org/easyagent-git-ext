package ghclient

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// The release request must carry draft=false EXPLICITLY when the caller did not ask for a
// draft. GitHub's default is draft=false, so a release that comes back as a DRAFT means the
// field was not sent — and a draft is invisible to everyone, including a downstream subscriber,
// which would make an "publish" tool silently not publish.
func TestLiveCreateReleaseIsPublished(t *testing.T) {
	if os.Getenv("GH_E2E") != "1" {
		t.Skip("set GH_E2E=1 (and GH_TOKEN, GH_E2E_REPO)")
	}
	token, repo := os.Getenv("GH_TOKEN"), os.Getenv("GH_E2E_REPO")
	if token == "" || repo == "" {
		t.Fatal("GH_TOKEN and GH_E2E_REPO are required")
	}

	c, err := New(Options{Remote: "https://github.com/" + repo, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tag := fmt.Sprintf("probe-%d", time.Now().Unix())
	rel, err := c.CreateRelease(ctx, CreateReleaseOptions{
		TagName: tag,
		Target:  "main",
		Name:    "probe",
		Body:    "probe release; should be published, not draft",
		// Draft and Prerelease deliberately left at their zero values, which is what a caller
		// that wants a normal release passes.
	})
	if err != nil {
		t.Fatalf("create release: %v", err)
	}
	if rel.Draft {
		t.Errorf("release came back as a DRAFT: an unrequested draft is invisible to every consumer; got %+v", rel)
	}
	t.Logf("release %s draft=%v prerelease=%v url=%s", rel.TagName, rel.Draft, rel.Prerelease, rel.HTMLURL)

	// Clean up: the release first, then the tag it created.
	if id, ok := c.releaseID(ctx, tag); ok {
		_ = c.deleteRelease(ctx, id)
	}
	_ = c.DeleteTag(ctx, tag)
}

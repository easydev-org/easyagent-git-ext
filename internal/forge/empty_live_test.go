package forge

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

/**
 * An empty repository must be NAMED as empty, not reported as a broken server.
 *
 * The forge answers HTTP 500 with an empty body when the contents endpoint is used on a repository
 * with no commits. That is indistinguishable from a real fault, and the state is easy to reach: the
 * forge's own migrate API leaves exactly this behind when it fails part-way, and the repository then
 * looks healthy — it has an id, a default branch and a 201 on creation.
 */
func TestEmptyRepositoryIsNamedLive(t *testing.T) {
	base := os.Getenv("FORGE_LIVE_URL")
	tok := os.Getenv("FORGE_LIVE_TOKEN")
	repo := os.Getenv("EMPTY_REPO") // agentdev/empty-probe: created with auto_init=false
	if base == "" || tok == "" || repo == "" {
		t.Skip("set FORGE_LIVE_* and EMPTY_REPO to a repository with no commits")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	c, err := New(base, tok, base+"/"+repo+".git")
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CommitFiles(ctx, "main", "probe", []TreeChange{
		{Path: "x.md", Content: []byte("x\n")},
	}, nil)
	if err == nil {
		t.Fatal("committing to an empty repository should fail")
	}
	if !strings.Contains(err.Error(), "EMPTY") {
		t.Errorf("the error should name the empty repository, got: %v", err)
	}
	t.Logf("%v", err)
}

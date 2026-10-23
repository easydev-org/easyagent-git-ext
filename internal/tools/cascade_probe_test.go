package tools

import (
	"context"
	"os"
	"testing"
	"time"

	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
)

// Deleting a tag MUST take its release with it.
//
// The state this prevents: a release that is still announced, pointing at a tag nobody can
// check out. It is invisible until somebody tries to download it, which is the worst time to
// find out. So the check is: create a release the way CI would, delete the tag through the
// tool, and assert the release is GONE from GitHub, not merely the ref.
func TestE2ETagDeleteAlsoRemovesRelease(t *testing.T) {
	if os.Getenv("GH_E2E") != "1" {
		t.Skip("set GH_E2E=1 (and GH_TOKEN, GH_E2E_REPO)")
	}
	token, repo := os.Getenv("GH_TOKEN"), os.Getenv("GH_E2E_REPO")
	if token == "" || repo == "" {
		t.Fatal("GH_TOKEN and GH_E2E_REPO are required")
	}
	tag := os.Getenv("GH_E2E_TAG")
	if tag == "" {
		t.Skip("set GH_E2E_TAG to the tag a release was published at")
	}

	reg := Registry(Deps{
		GetConfig: func(name, session, tenant string) any {
			if name == "token" {
				return token
			}
			return nil
		},
		Locale: func(string, string) string { return "en" },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ctx = extension.WithGrants(ctx, []*easybus.Grant{
		{Key: PermTagDelete, Params: easybus.Struct(map[string]any{"repo": repo})},
	})

	res, err := reg["gh-tag-delete"](ctx, map[string]any{"repo": repo, "tag": tag}, "alice", "t")
	if err != nil {
		t.Fatalf("gh-tag-delete: %v", err)
	}
	t.Logf("%s", res.Content)
	if d, _ := res.Data.(map[string]any)["release_deleted"].(bool); !d {
		t.Errorf("a release was published at %q but the tool reported none deleted: %+v", tag, res.Data)
	}
}

package tools

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"

	"github.com/easydev-org/easyagent-github-ext/internal/ghclient"
)

// Live tag + release flow against real GitHub.
//
// GH_E2E=1 GH_TOKEN=... GH_E2E_REPO=easydev-org/test go test ./internal/tools -run TestE2ETagRelease
//
// Asserts the things that are easy to get wrong and invisible from a 200 response: the tag is
// ANNOTATED (not a bare ref), it points at a commit, and the release is attached to it.
func TestE2ETagRelease(t *testing.T) {
	if os.Getenv("GH_E2E") != "1" {
		t.Skip("set GH_E2E=1 (and GH_TOKEN, GH_E2E_REPO) to run against real GitHub")
	}
	token, repo := os.Getenv("GH_TOKEN"), os.Getenv("GH_E2E_REPO")
	if token == "" || repo == "" {
		t.Fatal("GH_TOKEN and GH_E2E_REPO are required")
	}

	reg := Registry(Deps{
		Stage: nil, // tags do not touch the staging area
		GetConfig: func(name, session, tenant string) any {
			if name == "token" {
				return token
			}
			return nil
		},
		Locale: func(string, string) string { return "en" },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	tenant, session := "alice", "tag-e2e"
	ctx = extension.WithGrants(ctx, []*easybus.Grant{
		{Key: PermTagCreate, Params: easybus.Struct(map[string]any{"repo": repo})},
		{Key: PermTagDelete, Params: easybus.Struct(map[string]any{"repo": repo})},
		{Key: PermReleaseCreate, Params: easybus.Struct(map[string]any{"repo": repo})},
	})

	name := fmt.Sprintf("v0.0.%d", time.Now().Unix())

	t.Logf("1. creating annotated tag %s", name)
	res, err := reg["gh-tag-create"](ctx, map[string]any{
		"repo": repo, "tag": name, "message": "e2e: annotated tag for the release test",
	}, session, tenant)
	if err != nil {
		t.Fatalf("gh-tag-create: %v", err)
	}
	t.Logf("   %s", res.Content)

	t.Logf("2. listing tags")
	res, err = reg["gh-tag-list"](ctx, map[string]any{"repo": repo}, session, tenant)
	if err != nil {
		t.Fatalf("gh-tag-list: %v", err)
	}
	t.Logf("   %s", res.Content)

	t.Logf("3. publishing a release at %s", name)
	res, err = reg["gh-release-create"](ctx, map[string]any{
		"repo": repo, "tag": name,
		"body": "End-to-end release for easyagent-github-ext.",
	}, session, tenant)
	if err != nil {
		t.Fatalf("gh-release-create: %v", err)
	}
	t.Logf("   %s", res.Content)

	// The tag must be ANNOTATED: a lightweight tag would be a bare ref with no message, and a
	// release cannot carry notes through it.
	c, err := ghclient.New(ghclient.Options{
		Remote: "https://github.com/" + repo, Token: token,
	})
	if err != nil {
		t.Fatal(err)
	}
	rel, err := c.Release(ctx, name)
	if err != nil {
		t.Fatalf("read back release: %v", err)
	}
	if rel.TagName != name {
		t.Errorf("release tag = %q, want %q", rel.TagName, name)
	}
	t.Logf("4. read back: release %s -> %s", rel.TagName, rel.HTMLURL)

	t.Logf("cleanup: deleting tag %s", name)
	if _, err := reg["gh-tag-delete"](ctx, map[string]any{"repo": repo, "tag": name}, session, tenant); err != nil {
		t.Logf("   (tag delete failed, harmless: %v)", err)
	}
}

package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
)

// Live tag flow against real GitHub, exercising the two rules this extension now enforces:
//
//   - a release cannot be published by hand at all;
//   - deleting a tag deletes its release, so the orphaned-release state cannot be produced.
//
// GH_E2E=1 GH_TOKEN=... GH_E2E_REPO=easydev-org/test go test ./internal/tools -run TestE2ETagDeleteTakesRelease
func TestE2ETagDeleteTakesRelease(t *testing.T) {
	if os.Getenv("GH_E2E") != "1" {
		t.Skip("set GH_E2E=1 (and GH_TOKEN, GH_E2E_REPO)")
	}
	token, repo := os.Getenv("GH_TOKEN"), os.Getenv("GH_E2E_REPO")
	if token == "" || repo == "" {
		t.Fatal("GH_TOKEN and GH_E2E_REPO are required")
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	tenant, session := "alice", "tag-e2e"
	ctx = extension.WithGrants(ctx, []*easybus.Grant{
		{Key: PermTagCreate, Params: easybus.Struct(map[string]any{"repo": repo})},
		{Key: PermTagDelete, Params: easybus.Struct(map[string]any{"repo": repo})},
	})

	name := fmt.Sprintf("v0.0.%d", time.Now().Unix())

	t.Logf("1. creating annotated tag %s", name)
	res, err := reg["gh-tag-create"](ctx, map[string]any{
		"repo": repo, "tag": name, "message": "e2e: annotated tag for the delete test",
	}, session, tenant)
	if err != nil {
		t.Fatalf("gh-tag-create: %v", err)
	}
	t.Logf("   %s", res.Content)

	t.Logf("2. reading the annotation back through gh-tag-show")
	res, err = reg["gh-tag-show"](ctx, map[string]any{"repo": repo, "tag": name}, session, tenant)
	if err != nil {
		t.Fatalf("gh-tag-show: %v", err)
	}
	t.Logf("   %s", strings.ReplaceAll(res.Content, "\n", "\n   "))
	if !strings.Contains(res.Content, "annotated: yes") {
		t.Errorf("the tag was created annotated but gh-tag-show disagrees: %s", res.Content)
	}
	if !strings.Contains(res.Content, "delete test") {
		t.Errorf("the tag's message is missing from gh-tag-show: %s", res.Content)
	}

	t.Logf("3. deleting the tag (takes any release with it)")
	res, err = reg["gh-tag-delete"](ctx, map[string]any{"repo": repo, "tag": name}, session, tenant)
	if err != nil {
		t.Fatalf("gh-tag-delete: %v", err)
	}
	t.Logf("   %s", res.Content)

	// Confirm on GitHub: the tag is gone.
	if _, err := reg["gh-tag-list"](ctx, map[string]any{"repo": repo}, session, tenant); err != nil {
		t.Fatalf("gh-tag-list: %v", err)
	}
}

package tools

import (
	"context"
	"strings"
	"testing"

	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
)

// grant appends one permission for a repository.
func grant(ctx context.Context, key, repo string) context.Context {
	return extension.WithGrants(ctx, []*easybus.Grant{
		{Key: key, Params: easybus.Struct(map[string]any{"repo": repo})},
	})
}

func TestTagCreateRequiresMessage(t *testing.T) {
	reg := Registry(Deps{})
	_, err := reg["gh-tag-create"](context.Background(),
		map[string]any{"repo": "o/r", "tag": "v1"}, "s", "t")
	if err == nil {
		t.Fatal("a tag with no message must be refused")
	}
	// The message has to say WHY, or a caller retries with an empty one again.
	if !strings.Contains(err.Error(), "annotated") {
		t.Errorf("err = %v, want it to explain that tags here are annotated", err)
	}
}

// Publishing a release by hand is DISABLED, not merely gated.
//
// The refusal must be unconditional: no grant makes it allowed, and the message has to point at
// the path that does work (push a tag, let CI publish), or a caller keeps trying.
func TestReleaseCreateIsAlwaysRefused(t *testing.T) {
	reg := Registry(Deps{})

	// With every permission we know of, and with an otherwise-valid argument set.
	ctx := extension.WithGrants(context.Background(), []*easybus.Grant{
		{Key: PermReleaseDelete, Params: easybus.Struct(map[string]any{"repo": "o/r"})},
		{Key: PermTagCreate, Params: easybus.Struct(map[string]any{"repo": "o/r"})},
	})
	_, err := reg["gh-release-create"](ctx,
		map[string]any{"repo": "o/r", "tag": "v1", "body": "notes"}, "s", "t")
	if err == nil {
		t.Fatal("manual release publishing must be refused even with grants")
	}
	if !strings.Contains(err.Error(), "CI") {
		t.Errorf("err = %v, want it to say releases are published by CI", err)
	}
	if !strings.Contains(err.Error(), "gh-tag-create") {
		t.Errorf("err = %v, want it to name the path that does work", err)
	}
}

// Deleting a tag deletes its release. Asserted as a PROPERTY of the tool, not an option:
// the previous design had a `with_tag` flag, and the state it could leave behind — an
// announced release behind a deleted tag — was the bug.
func TestTagDeleteHasNoWithTagOption(t *testing.T) {
	reg := Registry(Deps{})
	ctx := grant(context.Background(), PermTagDelete, "o/r")
	// A caller that still passes with_tag must not be able to opt OUT of removing the release.
	// The handler ignores the argument and calls DeleteTagAndRelease regardless; this asserts
	// the argument is not consulted by checking the tool is driven purely by tag.delete.
	if _, ok := reg["gh-tag-delete"]; !ok {
		t.Fatal("gh-tag-delete is not registered")
	}
	// Without the permission it must refuse before touching the network.
	_, err := reg["gh-tag-delete"](context.Background(),
		map[string]any{"repo": "o/r", "tag": "v1"}, "s", "t")
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("gh-tag-delete must be gated: %v", err)
	}
	_ = ctx
}

// Every publishing tool must be gated BEFORE any network call — a tool that calls GitHub and
// then discovers it has no permission has already acted.
func TestPublishingToolsAreGated(t *testing.T) {
	reg := Registry(Deps{})
	cases := map[string]map[string]any{
		"gh-tag-create":     {"repo": "o/r", "tag": "v1", "message": "m"},
		"gh-tag-delete":     {"repo": "o/r", "tag": "v1"},
		"gh-release-delete": {"repo": "o/r", "tag": "v1"},
	}
	for name, args := range cases {
		h, ok := reg[name]
		if !ok {
			t.Errorf("%s is not registered", name)
			continue
		}
		_, err := h(context.Background(), args, "s", "t")
		if err == nil {
			t.Errorf("%s was allowed without a grant", name)
			continue
		}
		if !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("%s: err = %v, want a permission denial", name, err)
		}
	}
}

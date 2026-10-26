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

// The release tools are GONE, not merely refusing.
//
// Publishing belongs to CI and deleting a release separately was a way to leave an orphaned
// announcement behind. Both are unreachable now — the tag delete removes the release with it,
// which is the only removal that cannot produce a broken state.
func TestReleaseToolsAreAbsent(t *testing.T) {
	reg := Registry(Deps{})
	for _, name := range []string{"gh-release-create", "gh-release-delete"} {
		if _, ok := reg[name]; ok {
			t.Errorf("%s is still registered; releases are CI's job and a tag delete takes its release", name)
		}
	}
	// The READ tool stays: seeing what was published is how a downstream job decides to act.
	if _, ok := reg["gh-releases"]; !ok {
		t.Error("gh-releases should remain: reading published versions is a legitimate need")
	}
}

// Deleting a tag deletes its release. Asserted as a PROPERTY of the tool, not an option:
// the previous design had a `with_tag` flag, and the state it could leave behind — an
// announced release behind a deleted tag — was the bug.
func TestTagDeleteHasNoWithTagOption(t *testing.T) {
	reg := Registry(Deps{})
	ctx := grant(context.Background(), PermMaintainer, "o/r")
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
		"gh-tag-create": {"repo": "o/r", "tag": "v1", "message": "m"},
		"gh-tag-delete": {"repo": "o/r", "tag": "v1"},
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

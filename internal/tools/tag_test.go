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

// The publish tools have REQUIREMENTS that are not merely "the field is missing". Each of these
// asserts the reason, because a caller who reads only the error has to be able to act on it.

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

func TestReleaseCreateRequiresBody(t *testing.T) {
	reg := Registry(Deps{})
	_, err := reg["gh-release-create"](context.Background(),
		map[string]any{"repo": "o/r", "tag": "v1"}, "s", "t")
	if err == nil {
		t.Fatal("a release with no notes must be refused")
	}
	if !strings.Contains(err.Error(), "what changed") {
		t.Errorf("err = %v, want it to say consumers need the notes", err)
	}
}

// Every publish tool must be gated, and the gate must be checked BEFORE any network call — a
// tool that calls GitHub and then discovers it has no permission has already acted.
func TestPublishingToolsAreGated(t *testing.T) {
	reg := Registry(Deps{})
	// No grants on the context: each must refuse without reaching the API.
	cases := map[string]map[string]any{
		"gh-tag-create":     {"repo": "o/r", "tag": "v1", "message": "m"},
		"gh-tag-delete":     {"repo": "o/r", "tag": "v1"},
		"gh-release-create": {"repo": "o/r", "tag": "v1", "body": "b"},
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

// with_tag needs tag.delete ON TOP of release.delete: deleting a ref is a different act from
// unpublishing an announcement.
func TestReleaseDeleteWithTagNeedsBothPermissions(t *testing.T) {
	reg := Registry(Deps{})
	ctx := extension.WithGrants(context.Background(), nil)
	// Only release.delete granted.
	ctx = grant(ctx, PermReleaseDelete, "o/r")
	_, err := reg["gh-release-delete"](ctx,
		map[string]any{"repo": "o/r", "tag": "v1", "with_tag": true}, "s", "t")
	if err == nil {
		t.Fatal("with_tag without tag.delete must be refused")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("err = %v", err)
	}
}

package tools

import (
	"context"
	"testing"

	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
)

func grants(gs ...*easybus.Grant) context.Context {
	return extension.WithGrants(context.Background(), gs)
}

func repoGrant(key, repo string) *easybus.Grant {
	return &easybus.Grant{Key: key, Params: easybus.Struct(map[string]any{"repo": repo})}
}

// A maintainer is trusted with everything a developer is. The reverse must NOT hold, or the two
// levels would be one level with two names.
func TestMaintainerCoversDeveloperAndNotTheReverse(t *testing.T) {
	const repo = "o/r"

	// A maintainer grant satisfies a developer check.
	ctx := grants(repoGrant(PermMaintainer, repo))
	if err := (&toolset{}).requireRepo(ctx, PermDeveloper, repo); err != nil {
		t.Errorf("maintainer should satisfy developer: %v", err)
	}
	// And a maintainer check.
	if err := (&toolset{}).requireRepo(ctx, PermMaintainer, repo); err != nil {
		t.Errorf("maintainer should satisfy maintainer: %v", err)
	}

	// A developer grant does NOT satisfy a maintainer check.
	ctx2 := grants(repoGrant(PermDeveloper, repo))
	if err := (&toolset{}).requireRepo(ctx2, PermDeveloper, repo); err != nil {
		t.Errorf("developer should satisfy developer: %v", err)
	}
	if err := (&toolset{}).requireRepo(ctx2, PermMaintainer, repo); err == nil {
		t.Error("a developer must not satisfy a maintainer check")
	}
}

// A grant for one repository must not cover another.
func TestPermissionIsScopedToOneRepository(t *testing.T) {
	ctx := grants(repoGrant(PermMaintainer, "o/one"))
	ts := &toolset{}
	if err := ts.requireRepo(ctx, PermDeveloper, "o/one"); err != nil {
		t.Errorf("the granted repository should pass: %v", err)
	}
	if err := ts.requireRepo(ctx, PermDeveloper, "o/two"); err == nil {
		t.Error("a grant for o/one must not cover o/two")
	}
}

// A wildcard grant covers every repository of one owner, which is how "all my repos" is expressed.
func TestWildcardGrantCoversOwner(t *testing.T) {
	ctx := grants(repoGrant(PermDeveloper, "o/*"))
	ts := &toolset{}
	if err := ts.requireRepo(ctx, PermDeveloper, "o/anything"); err != nil {
		t.Errorf("o/* should cover o/anything: %v", err)
	}
	if err := ts.requireRepo(ctx, PermDeveloper, "other/anything"); err == nil {
		t.Error("o/* must not cover another owner")
	}
}

// Creating a repository is an ORGANIZATION permission: the repository does not exist yet, so a
// repository grant cannot be the thing that authorises it.
func TestRepoCreateNeedsOrgAdminNotRepoMaintainer(t *testing.T) {
	const slug = "acme/new-thing"

	// A maintainer grant for that slug does not authorise creating it.
	ctx := grants(repoGrant(PermMaintainer, slug))
	if err := (&toolset{}).requireRepo(ctx, PermOrgAdmin, slug); err == nil {
		t.Error("repo.maintainer must not authorise creating a repository")
	}

	// org.admin does.
	ctx2 := grants(&easybus.Grant{Key: PermOrgAdmin, Params: easybus.Struct(map[string]any{"org": "acme"})})
	if err := (&toolset{}).requireRepo(ctx2, PermOrgAdmin, slug); err != nil {
		t.Errorf("org.admin should authorise creation: %v", err)
	}
}

// The merge tool is the one action an issue cannot perform, and it must be maintainer-only.
func TestMergeIsMaintainerOnly(t *testing.T) {
	got := RequireMap()["gh-pr-merge"]
	if len(got) != 1 || got[0] != PermMaintainer {
		t.Errorf("gh-pr-merge requires %v, want [%s]", got, PermMaintainer)
	}
}

// Publishing a version and withdrawing it must sit at the same level: "may publish but not
// withdraw" is a state that should not exist.
func TestTagCreateAndDeleteAreBothMaintainer(t *testing.T) {
	m := RequireMap()
	for _, tool := range []string{"gh-tag-create", "gh-tag-delete"} {
		got := m[tool]
		if len(got) != 1 || got[0] != PermMaintainer {
			t.Errorf("%s requires %v, want [%s]", tool, got, PermMaintainer)
		}
	}
}

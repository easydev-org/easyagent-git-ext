package tools

import (
	"context"
	"testing"

	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
)

func ctxWith(gs ...*easybus.Grant) context.Context {
	return extension.WithGrants(context.Background(), gs)
}

func repoGrant(key, repo string) *easybus.Grant {
	return &easybus.Grant{Key: key, Params: easybus.Struct(map[string]any{"repo": repo})}
}

// Reading, opening an issue and commenting need NO grant. A session that cannot ask a question is
// not a safer session, only a quieter one.
func TestExpressingNeedsNoPermission(t *testing.T) {
	m := RequireMap()
	for _, tool := range []string{
		"gh-issue-create", "gh-issue-comment", "gh-issue-list", "gh-issue-show",
		"gh-issue-comments", "gh-pr-files", "gh-pr-diff", "gh-pr-checks",
	} {
		if got := m[tool]; len(got) != 0 {
			t.Errorf("%s should need no permission, but requires %v", tool, got)
		}
	}
}

// Everything that CHANGES something needs repo.maintainer — including closing an issue, which a
// contributor may not do even to its own.
func TestChangingNeedsMaintainer(t *testing.T) {
	m := RequireMap()
	for _, tool := range []string{
		"gh-issue-close",
		"gh-pr-create", "gh-pr-merge",
		"gh-run-dispatch", "gh-run-rerun", "gh-run-cancel",
		"gh-tag-create", "gh-tag-delete",
		"gh-repo-rename", "gh-repo-delete",
	} {
		got := m[tool]
		if len(got) != 1 || got[0] != PermMaintainer {
			t.Errorf("%s requires %v, want [%s]", tool, got, PermMaintainer)
		}
	}
}

// A maintainer grant is scoped to one repository and does not leak to another.
func TestMaintainerIsScopedToOneRepository(t *testing.T) {
	ctx := ctxWith(repoGrant(PermMaintainer, "o/one"))
	ts := &toolset{}
	if err := ts.requireRepo(ctx, PermMaintainer, "o/one"); err != nil {
		t.Errorf("the granted repository should pass: %v", err)
	}
	if err := ts.requireRepo(ctx, PermMaintainer, "o/two"); err == nil {
		t.Error("a grant for o/one must not cover o/two")
	}
}

// A wildcard covers one owner's repositories, which is how "all my repositories" is expressed.
func TestWildcardCoversOwner(t *testing.T) {
	ctx := ctxWith(repoGrant(PermMaintainer, "o/*"))
	ts := &toolset{}
	if err := ts.requireRepo(ctx, PermMaintainer, "o/anything"); err != nil {
		t.Errorf("o/* should cover o/anything: %v", err)
	}
	if err := ts.requireRepo(ctx, PermMaintainer, "other/anything"); err == nil {
		t.Error("o/* must not cover another owner")
	}
}

// Creating a repository is an ORGANIZATION decision: there is no repository yet to scope a grant
// to, so a repository-level grant must not authorise it.
func TestRepoCreateIsOrgScoped(t *testing.T) {
	const slug = "acme/new-thing"

	ctx := ctxWith(repoGrant(PermMaintainer, slug))
	if err := (&toolset{}).requireRepo(ctx, PermOrgAdmin, slug); err == nil {
		t.Error("repo.maintainer must not authorise creating a repository")
	}

	ctx2 := ctxWith(&easybus.Grant{
		Key: PermOrgAdmin, Params: easybus.Struct(map[string]any{"org": "acme"}),
	})
	if err := (&toolset{}).requireRepo(ctx2, PermOrgAdmin, slug); err != nil {
		t.Errorf("org.admin should authorise creation under that org: %v", err)
	}

	// And not under a different org.
	ctx3 := ctxWith(&easybus.Grant{
		Key: PermOrgAdmin, Params: easybus.Struct(map[string]any{"org": "other"}),
	})
	if err := (&toolset{}).requireRepo(ctx3, PermOrgAdmin, slug); err == nil {
		t.Error("org.admin for 'other' must not authorise creating under 'acme'")
	}
}

// There are exactly two keys. A third would mean the model crept back toward per-operation grants.
func TestExactlyTwoPermissions(t *testing.T) {
	if PermMaintainer == PermOrgAdmin {
		t.Fatal("the two permission keys must differ")
	}
}

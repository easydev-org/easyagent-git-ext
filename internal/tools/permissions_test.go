package tools

import (
	"context"
	"strings"
	"testing"

	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
)

// The remote-URL shapes a real deployment produces. Getting this wrong in the PERMISSIVE
// direction would let a grant for one repository authorise a call against another, so the
// cases include the shapes that must resolve to nothing.
func TestRepoSlug(t *testing.T) {
	cases := map[string]string{
		"https://git.example.com/agentdev/mrflow.git":  "agentdev/mrflow",
		"https://git.example.com/agentdev/mrflow":      "agentdev/mrflow",
		"http://forgejo.dev.svc.cluster.local/a/b.git": "a/b",
		"git@git.example.com:agentdev/mrflow.git":      "agentdev/mrflow",
		"ssh://git@host/agentdev/mrflow.git":           "agentdev/mrflow",
		// Trailing slash and a leading one: both are things users paste.
		"https://host/agentdev/mrflow/": "agentdev/mrflow",
		// A nested group: the LAST two segments are owner/name, which is what a grant
		// names. (A forge with arbitrary nesting would need its own rule; this is the
		// shape these deployments use.)
		"https://host/group/sub/repo.git": "sub/repo",
		// Not identifiable: no repository can be named, so no grant can cover it.
		"https://host/onlyowner": "",
		"":                       "",
		"not a url":              "",
	}
	for in, want := range cases {
		if got := repoSlug(in); got != want {
			t.Errorf("repoSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

// The whole mechanism, end to end at the extension boundary: a grant for one repository
// must not authorise a call against another.
func TestRequireRepoHonoursTheGrant(t *testing.T) {
	target := "https://git.example.com/agentdev/mrflow.git"

	withGrant := func(params map[string]any) context.Context {
		return extension.WithGrants(context.Background(), []*easybus.Grant{
			{Key: PermMRMerge, Params: easybus.Struct(params)},
		})
	}

	// Covering grant: allowed.
	ctx := withGrant(map[string]any{"repo": "agentdev/mrflow"})
	if err := extension.Require(ctx, PermMRMerge, map[string]string{"repo": repoSlug(target)}); err != nil {
		t.Fatalf("exact grant must cover: %v", err)
	}

	// Wildcard grant: allowed.
	ctx = withGrant(map[string]any{"repo": "agentdev/*"})
	if err := extension.Require(ctx, PermMRMerge, map[string]string{"repo": repoSlug(target)}); err != nil {
		t.Fatalf("wildcard grant must cover: %v", err)
	}

	// Different repository: refused, and the message names both sides.
	ctx = withGrant(map[string]any{"repo": "someone-else/other"})
	err := extension.Require(ctx, PermMRMerge, map[string]string{"repo": repoSlug(target)})
	if err == nil {
		t.Fatal("a grant for a different repository must be refused")
	}
	msg := err.Error()
	if !strings.Contains(msg, "agentdev/mrflow") || !strings.Contains(msg, "someone-else/other") {
		t.Errorf("message must name both the target and what is held: %q", msg)
	}

	// No grant at all: refused, with a distinct message.
	if err := extension.Require(context.Background(), PermMRMerge, map[string]string{"repo": repoSlug(target)}); err == nil {
		t.Fatal("no grant must be refused")
	} else if !strings.Contains(err.Error(), "no grant held") {
		t.Errorf("message should distinguish 'none' from 'not covering': %q", err.Error())
	}
}

// Holding mr.close must not authorise mr.merge — the two are separate keys precisely so a
// reviewer can be allowed to close without being able to land.
func TestCloseGrantDoesNotAuthoriseMerge(t *testing.T) {
	ctx := extension.WithGrants(context.Background(), []*easybus.Grant{
		{Key: PermMRClose, Params: easybus.Struct(map[string]any{"repo": "*"})},
	})
	target := map[string]string{"repo": "agentdev/mrflow"}
	if err := extension.Require(ctx, PermMRMerge, target); err == nil {
		t.Fatal("a close grant must not authorise a merge")
	}
	if err := extension.Require(ctx, PermMRClose, target); err != nil {
		t.Fatalf("the close grant should cover close: %v", err)
	}
}

// A remote we cannot reduce to owner/name must be REFUSED, not waved through.
//
// This is the subtle case that made the check worth writing carefully: a wildcard grant
// ("*") matches ANY string, including the empty one an unparseable remote resolves to. So
// matching alone would authorise a write to a repository nobody could name. requireRepo
// therefore fails before matching.
func TestUnparseableRemoteIsRefused(t *testing.T) {
	// Even a total wildcard must not authorise an unidentifiable repository.
	ctx := extension.WithGrants(context.Background(), []*easybus.Grant{
		{Key: PermMRMerge, Params: easybus.Struct(map[string]any{"repo": "*"})},
	})
	tool := &toolset{}
	err := tool.requireRepo(ctx, PermMRMerge,
		map[string]any{"remote": "https://host/onlyowner"}, "s", "t")
	if err == nil {
		t.Fatal("an unidentifiable remote must be refused even with a '*' grant")
	}
	if !strings.Contains(err.Error(), "cannot identify") {
		t.Errorf("message should say the repository could not be identified: %q", err.Error())
	}
}

// Every permission a tool requires must be declared in the manifest, or the tool would
// refuse every call with nothing to grant.
func TestRequiredPermissionsAreDeclared(t *testing.T) {
	declared := map[string]bool{
		PermMRMerge: true, PermMRClose: true, PermRepoCreate: true, PermRepoDelete: true,
	}
	var keys []string
	for _, ks := range RequireMap() {
		keys = append(keys, ks...)
	}
	if missing := EnsurePermKeysDeclared(declared, keys); len(missing) > 0 {
		t.Fatalf("tools require undeclared permissions: %v", missing)
	}
}

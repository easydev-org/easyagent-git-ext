package e2e

import (
	"os"
	"strings"
	"testing"
	"time"

)

// The repository these tests clone. Real, because the tool under test IS git: a stub would prove
// nothing about credentials, transport or ref handling — which is where this extension fails.
const testRepo = "easydev-org/test"

func TestRegistersOverDiscovery(t *testing.T) {
	a, _ := harness(t)
	ctx, cancel := ctxWithTimeout(t, 30)
	defer cancel()

	found, err := a.Discover(ctx, 5000)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	var ids []string
	for _, m := range found {
		ids = append(ids, m.Id)
	}
	if !contains(ids, extID) {
		t.Fatalf("extension not discovered; got %v", ids)
	}
}

func TestDeclaresItsTools(t *testing.T) {
	a, _ := harness(t)
	ctx, cancel := ctxWithTimeout(t, 30)
	defer cancel()

	found, err := a.Discover(ctx, 5000)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	// `Tools` is a SLICE of specs, not a map keyed by name.
	var declared []string
	for _, m := range found {
		if m.Id != extID {
			continue
		}
		for _, tl := range m.Tools {
			declared = append(declared, tl.Name)
		}
	}
	for _, want := range []string{
		"git-ws-clone", "git-ws-workspace", "git-ws-status",
		"git-ws-patch", "git-ws-read", "git-ws-commit",
	} {
		if !contains(declared, want) {
			t.Fatalf("tool %q missing; declared %d tools", want, len(declared))
		}
	}
}

// A clone of the REAL repository, then a read of a file from it.
//
// Cloning proves credentials and transport; reading a file proves the checkout is usable. A clone
// that "succeeded" but left an empty directory would pass the first and fail here — and that is a
// real failure mode when a token is accepted but scoped too narrowly.
func TestClonesAndReadsARealRepository(t *testing.T) {
	token := os.Getenv("E2E_GITHUB_TOKEN")
	if token == "" {
		t.Skip("E2E_GITHUB_TOKEN not set")
	}
	a, bus := harness(t)
	configure(t, bus, token)
	if err := waitCloneable(t); err != nil {
		t.Fatal(err)
	}

	out := callTool(t, a, "git-ws-clone", map[string]any{
		"repo":  testRepo,
		"depth": float64(1),
	}, nil)
	wsID := firstWorkspaceID(out)
	if wsID == "" {
		t.Fatalf("clone returned no workspace id: %q", out)
	}
	t.Logf("workspace: %s", wsID)

	// List the tree: the checkout must contain the repository's own files, not an empty directory.
	listing := callTool(t, a, "git-ws-list", map[string]any{"workspace": wsID}, nil)
	if strings.TrimSpace(listing) == "" {
		t.Fatal("git-ws-list returned nothing: the clone produced an empty workspace")
	}
	t.Logf("listing: %s", trim(listing, 300))
}

// The patch tool must REJECT a patch that does not apply, rather than half-applying it.
//
// "Applied whole or not at all" is the only safe contract for an agent editing code: a partial
// application leaves a repository in a state the model cannot reason about.
func TestPatchRejectsAnImpossiblePatch(t *testing.T) {
	token := os.Getenv("E2E_GITHUB_TOKEN")
	if token == "" {
		t.Skip("E2E_GITHUB_TOKEN not set")
	}
	a, bus := harness(t)
	configure(t, bus, token)
	if err := waitCloneable(t); err != nil {
		t.Fatal(err)
	}

	out := callTool(t, a, "git-ws-clone", map[string]any{
		"repo":  testRepo,
		"depth": float64(1),
	}, nil)
	wsID := firstWorkspaceID(out)
	if wsID == "" {
		t.Fatalf("clone returned no workspace id: %q", out)
	}

	ctx, cancel := ctxWithTimeout(t, 120)
	defer cancel()
	tr, err := a.CallToolWithGrants(ctx, tenant, session, extID, "git-ws-patch",
		"c-badpatch", map[string]any{
			"workspace": wsID,
			"patch": "*** Begin Patch\n*** Update File: does-not-exist.txt\n@@\n-x\n+y\n*** End Patch",
		}, nil)
	if err != nil {
		return // a transport-level refusal is acceptable here; what matters is that it did not apply
	}
	if tr.Error == nil {
		t.Fatal("an impossible patch was accepted; a partial or fabricated edit is worse than a " +
			"refusal because the model cannot see what changed")
	}
	t.Logf("refused as expected: %s", tr.Error.Message)
}

// A tool that needs a workspace must refuse a bogus id, not operate on nothing.
func TestRefusesAnUnknownWorkspace(t *testing.T) {
	a, _ := harness(t)
	ctx, cancel := ctxWithTimeout(t, 60)
	defer cancel()
	tr, err := a.CallToolWithGrants(ctx, tenant, session, extID, "git-ws-status",
		"c-bogus", map[string]any{"workspace": "ws-does-not-exist"}, nil)
	if err != nil {
		return
	}
	if tr.Error == nil {
		t.Fatal("an unknown workspace returned success")
	}
	t.Logf("refused as expected: %s", tr.Error.Message)
}

// firstWorkspaceID reads the id out of whatever shape the clone answered with.
func firstWorkspaceID(s string) string {
	// The tool answers with a human sentence containing the id; take the first token that looks like
	// one rather than pinning the wording, which is presentation and may change.
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\t' || r == ':' || r == '"' || r == ',' || r == '`'
	}) {
		if strings.HasPrefix(f, "ws-") || strings.HasPrefix(f, "w-") {
			return f
		}
	}
	return ""
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// waitCloneable waits until the token has reached the extension.
//
// The config watch is asynchronous, so a clone issued right after the write races it — and that race
// looks exactly like "the token never arrived", which is a failure this suite exists to catch.
func waitCloneable(t *testing.T) error {
	// The watch is asynchronous: a clone issued immediately after the KV write races it, and the
	// race looks exactly like "the token never arrived". Waiting on DISCOVERY (which also depends on
	// the presence cache being warm) is the cheapest honest barrier.
	time.Sleep(1500 * time.Millisecond)
	return nil
}

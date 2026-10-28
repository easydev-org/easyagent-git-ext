package workspace

import "testing"

/**
 * `List` with an EMPTY session means "all of this tenant's", not "the ones recorded with no session".
 *
 * The literal reading is what it used to do, and it is useless: nothing creates a session-less
 * workspace, so the answer was always empty. An extension's own home screen is opened from a tab
 * rather than a conversation, has no session to name, and therefore showed "no workspace in this
 * session" over checkouts the caller could plainly see existed.
 *
 * The tenant is still enforced, which is the part that matters: widening by session must never widen
 * across tenants.
 */
func TestListWithEmptySessionReturnsTheWholeTenant(t *testing.T) {
	m := newTestManager(t)
	m.areas["a1"] = &Workspace{ID: "a1", Tenant: "alice", Session: "s1"}
	m.areas["a2"] = &Workspace{ID: "a2", Tenant: "alice", Session: "s2"}
	m.areas["b1"] = &Workspace{ID: "b1", Tenant: "bob", Session: "s1"}

	got := m.List("alice", "")
	if len(got) != 2 {
		t.Fatalf("got %d workspaces, want 2 — an empty session should not filter", len(got))
	}
	for _, w := range got {
		if w.Tenant != "alice" {
			t.Errorf("leaked another tenant's workspace: %+v", w)
		}
	}
}

// A named session still narrows, because a tool card only cares about its own conversation.
func TestListWithSessionNarrows(t *testing.T) {
	m := newTestManager(t)
	m.areas["a1"] = &Workspace{ID: "a1", Tenant: "alice", Session: "s1"}
	m.areas["a2"] = &Workspace{ID: "a2", Tenant: "alice", Session: "s2"}

	got := m.List("alice", "s1")
	if len(got) != 1 || got[0].ID != "a1" {
		t.Fatalf("got %+v, want only a1", got)
	}
}

// Another tenant's session is not reachable by naming it: the tenant check comes first.
func TestListNeverCrossesTenants(t *testing.T) {
	m := newTestManager(t)
	m.areas["b1"] = &Workspace{ID: "b1", Tenant: "bob", Session: "s1"}

	if got := m.List("alice", "s1"); len(got) != 0 {
		t.Fatalf("alice saw bob's workspace: %+v", got)
	}
}

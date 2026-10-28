package workspace

import (
	"testing"
	"time"
)

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

// Newest activity first.
//
// A map's iteration order is random, so without this the browser list reshuffles on every request
// — which reads as "the thing I just used moved somewhere". `lastUse` is the field that already
// records "when did anybody last touch this", so it is the ordering, and a missing entry falls back
// to creation time rather than sorting as older than everything.
func TestListOrdersByRecentUse(t *testing.T) {
	m := newTestManager(t)
	m.areas["old"] = &Workspace{ID: "old", Tenant: "alice", Session: "s1"}
	m.areas["mid"] = &Workspace{ID: "mid", Tenant: "alice", Session: "s1"}
	m.areas["new"] = &Workspace{ID: "new", Tenant: "alice", Session: "s1"}

	now := time.Now()
	m.lastUse["old"] = now.Add(-2 * time.Hour)
	m.lastUse["mid"] = now.Add(-1 * time.Hour)
	m.lastUse["new"] = now

	got := m.List("alice", "")
	if len(got) != 3 {
		t.Fatalf("got %d, want 3", len(got))
	}
	if got[0].ID != "new" || got[1].ID != "mid" || got[2].ID != "old" {
		t.Errorf("order = %s,%s,%s; want new,mid,old", got[0].ID, got[1].ID, got[2].ID)
	}
}

// A workspace with no recorded use sorts by when it was created, not as "infinitely old".
func TestListFallsBackToCreatedAt(t *testing.T) {
	m := newTestManager(t)
	now := time.Now()
	m.areas["used"] = &Workspace{ID: "used", Tenant: "alice", Session: "s1", CreatedAt: now.Add(-3 * time.Hour)}
	m.areas["fresh"] = &Workspace{ID: "fresh", Tenant: "alice", Session: "s1", CreatedAt: now}
	m.lastUse["used"] = now.Add(-4 * time.Hour)
	// "fresh" has no lastUse entry at all.

	got := m.List("alice", "")
	if len(got) != 2 || got[0].ID != "fresh" {
		t.Fatalf("got %+v; want fresh first", got)
	}
}

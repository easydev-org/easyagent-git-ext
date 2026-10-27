package tools

import "testing"

// A tool that names a workspace must emit a view pointing at it.
//
// This is the contract the host's panel depends on: without `data.view` the card shows no button,
// and the only symptom is a missing affordance — nothing errors. So each tool's view is pinned here
// next to the route shape the UI actually routes.
func TestViewRouteShapes(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"plain", wsRoute("ws-abc", "/tree/"), "/ws/ws-abc/tree/"},
		{"changes", wsRoute("ws-abc", "/changes"), "/ws/ws-abc/changes"},
		{"id is one segment", wsRoute("ws/a b", "/tree/"), "/ws/ws%2Fa%20b/tree/"},
		{"file path keeps slashes", wsRoute("w1", "/blob/"+escapePath("src/lib/a b.ts")),
			"/ws/w1/blob/src/lib/a%20b.ts"},
	}
	for _, c := range cases {
		if c.in != c.want {
			t.Errorf("%s: %q, want %q", c.name, c.in, c.want)
		}
	}
}

// No workspace id means no view, not a broken link.
func TestViewForNeedsAWorkspace(t *testing.T) {
	if v := viewFor("", "/ws//tree/", "x"); v != nil {
		t.Fatalf("an empty workspace id produced a view: %#v", v)
	}
}

func TestViewForCarriesExtAndRoute(t *testing.T) {
	v := viewFor("w1", "/ws/w1/tree/", "repo")
	if v == nil {
		t.Fatal("no view")
	}
	if v["ext"] != "github" {
		t.Errorf("ext = %v, want github (the Service prefix and mount must agree)", v["ext"])
	}
	if v["route"] != "/ws/w1/tree/" {
		t.Errorf("route = %v", v["route"])
	}
}

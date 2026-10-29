package tools

import "testing"

/**
 * A mirror result must open the panel ON THE THING IT WAS ABOUT.
 *
 * The route is built by the tool because only it knows which repository the result came from — a
 * caller names a remote or a session's own repository, and the resolved owner/name exists in no
 * argument. Getting the SHAPE wrong is silent: the button renders, the click lands on a route the
 * panel does not parse, and the reader sees an empty page.
 */
func TestMirrorViewPointsAtTheFilesPage(t *testing.T) {
	v := mirrorView("agentdev", "e2e", "files/main", "")
	if v == nil {
		t.Fatal("no view")
	}
	if v["ext"] != "gitdev" {
		t.Errorf("ext = %v, want gitdev (the mount and the id must agree)", v["ext"])
	}
	// Branch in the QUERY, not the path: a branch name may contain slashes.
	if v["route"] != "/files/agentdev/e2e?branch=main" {
		t.Errorf("route = %v", v["route"])
	}
}

// A directory goes alongside the branch, still in the query. "?" separates them because a branch
// name may itself contain slashes.
func TestMirrorViewCarriesTheDirectory(t *testing.T) {
	v := mirrorView("o", "r", "files/main?src/lib", "")
	if v == nil {
		t.Fatal("no view")
	}
	if v["route"] != "/files/o/r?branch=main&dir=src%2Flib" {
		t.Errorf("route = %v", v["route"])
	}
}

/**
 * A branch containing a slash must survive as ONE query value.
 *
 * This is the case that made the "?" separator necessary: splitting "files/feat/x" on the first
 * slash reads it as branch "feat" and directory "x", which is a DIFFERENT place — and the failure is
 * silent, because both halves are plausible.
 */
func TestMirrorViewEscapesABranchWithSlashes(t *testing.T) {
	v := mirrorView("o", "r", "files/feat%2Fx", "")
	if v == nil {
		t.Fatal("no view")
	}
	if v["route"] != "/files/o/r?branch=feat%2Fx" {
		t.Errorf("route = %v", v["route"])
	}
}

// No repository means no button: a link to a page with nothing to open is worse than none.
func TestMirrorViewNeedsARepository(t *testing.T) {
	if v := mirrorView("", "r", "files/main", ""); v != nil {
		t.Errorf("an empty owner produced a view: %#v", v)
	}
}

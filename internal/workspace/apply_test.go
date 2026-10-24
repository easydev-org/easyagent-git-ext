package workspace

import (
	"fmt"
	"strings"
	"testing"
)

// fakeWS is an in-memory FileStore.
//
// Deliberately NOT a real Workspace: these tests are about the patch logic, so they must not
// need a directory or a git repository. The narrow FileStore interface is what makes that
// possible.
type fakeWS struct {
	files map[string][]byte
	gone  map[string]bool
}

func newWS(files map[string]string) *fakeWS {
	w := &fakeWS{files: map[string][]byte{}, gone: map[string]bool{}}
	for p, c := range files {
		w.files[p] = []byte(c)
	}
	return w
}

func (w *fakeWS) ReadFile(path string) ([]byte, error) {
	if w.gone[path] {
		return nil, errNotExist
	}
	c, ok := w.files[path]
	if !ok {
		return nil, errNotExist
	}
	return c, nil
}

func (w *fakeWS) WriteFile(path string, content []byte) error {
	w.files[path] = content
	delete(w.gone, path)
	return nil
}

func (w *fakeWS) RemoveFile(path string) error {
	delete(w.files, path)
	w.gone[path] = true
	return nil
}

// errNotExist stands in for a missing file; the patch logic only cares that it failed.
var errNotExist = fmt.Errorf("does not exist")

func (w *fakeWS) content(p string) string { return string(w.files[p]) }

func TestApplyPatchAdd(t *testing.T) {
	w := newWS(nil)
	if _, err := ApplyPatchTo(w, "*** Begin Patch\n*** Add File: a.go\n+package a\n*** End Patch\n"); err != nil {
		t.Fatal(err)
	}
	if got := w.content("a.go"); got != "package a\n" {
		t.Errorf("content = %q", got)
	}
}

func TestApplyPatchUpdate(t *testing.T) {
	w := newWS(map[string]string{"a.txt": "one\ntwo\n"})
	// No anchor: the context line is unique, so no disambiguation is needed.
	p := "*** Begin Patch\n*** Update File: a.txt\n@@\n one\n-two\n+TWO\n*** End Patch\n"
	if _, err := ApplyPatchTo(w, p); err != nil {
		t.Fatal(err)
	}
	if got := w.content("a.txt"); got != "one\nTWO\n" {
		t.Errorf("content = %q", got)
	}
}

func TestApplyPatchDelete(t *testing.T) {
	w := newWS(map[string]string{"gone.txt": "x\n"})
	if _, err := ApplyPatchTo(w, "*** Begin Patch\n*** Delete File: gone.txt\n*** End Patch\n"); err != nil {
		t.Fatal(err)
	}
	if !w.gone["gone.txt"] {
		t.Error("the file was not marked deleted")
	}
}

// One patch touching several files, mixed kinds.
func TestApplyPatchMultipleOps(t *testing.T) {
	w := newWS(map[string]string{"keep.txt": "k\n", "old.txt": "o\n"})
	p := "*** Begin Patch\n" +
		"*** Add File: new.txt\n+n\n" +
		"*** Update File: keep.txt\n@@\n-k\n+K\n" +
		"*** Delete File: old.txt\n" +
		"*** End Patch\n"
	if _, err := ApplyPatchTo(w, p); err != nil {
		t.Fatal(err)
	}
	if w.content("new.txt") != "n\n" {
		t.Error("add failed")
	}
	if w.content("keep.txt") != "K\n" {
		t.Error("update failed")
	}
	if !w.gone["old.txt"] {
		t.Error("delete failed")
	}
}

// THE important property: a patch is applied whole or not at all. A hunk that fails must leave
// the earlier operations UNWRITTEN, or the caller is left with a stage that is neither the old
// content nor the new.
func TestApplyPatchIsAtomic(t *testing.T) {
	w := newWS(map[string]string{"present.txt": "p\n"})
	before := w.content("present.txt")
	p := "*** Begin Patch\n" +
		"*** Add File: good.txt\n+g\n" +
		"*** Update File: present.txt\n@@\n-WRONG\n+x\n" + // does not match
		"*** End Patch\n"
	if _, err := ApplyPatchTo(w, p); err == nil {
		t.Fatal("expected the mismatched hunk to fail")
	}
	if _, ok := w.files["good.txt"]; ok {
		t.Error("the Add was written even though a later hunk failed")
	}
	if w.content("present.txt") != before {
		t.Error("present.txt was modified")
	}
}

// Add on an existing path is refused: it is almost always a caller that meant Update, and
// replacing the file would discard content the patch never mentioned.
func TestApplyPatchAddRefusesExisting(t *testing.T) {
	w := newWS(map[string]string{"a.txt": "original\n"})
	_, err := ApplyPatchTo(w, "*** Begin Patch\n*** Add File: a.txt\n+replacement\n*** End Patch\n")
	if err == nil {
		t.Fatal("add over an existing file must be refused")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v", err)
	}
	if w.content("a.txt") != "original\n" {
		t.Error("the original content was overwritten")
	}
}

// Update and Delete on a missing path are refused: each is a sign the caller is working from a
// stale view of the repository.
func TestApplyPatchRefusesMissingPaths(t *testing.T) {
	w := newWS(nil)
	if _, err := ApplyPatchTo(w, "*** Begin Patch\n*** Update File: nope.txt\n@@\n-a\n+b\n*** End Patch\n"); err == nil {
		t.Error("update of a missing file must be refused")
	}
	if _, err := ApplyPatchTo(w, "*** Begin Patch\n*** Delete File: nope.txt\n*** End Patch\n"); err == nil {
		t.Error("delete of a missing file must be refused")
	}
}

// A path may not escape the workspace.
func TestApplyPatchRefusesEscapingPaths(t *testing.T) {
	// A REAL Workspace: the escape check lives in its safePath, and an in-memory store has no
	// filesystem to escape from. Testing it through the fake would prove nothing.
	w := &Workspace{Dir: t.TempDir()}
	for _, bad := range []string{"/etc/passwd", "../outside.txt", "a/../../x"} {
		p := "*** Begin Patch\n*** Add File: " + bad + "\n+x\n*** End Patch\n"
		if _, err := w.ApplyPatch(p); err == nil {
			t.Errorf("path %q should have been refused", bad)
		}
	}
	// A name that merely CONTAINS ".." is legitimate and must be allowed.
	if _, err := w.ApplyPatch("*** Begin Patch\n*** Add File: a..b.txt\n+x\n*** End Patch\n"); err != nil {
		t.Errorf("a..b.txt should be allowed: %v", err)
	}
}

func TestApplyPatchEmpty(t *testing.T) {
	w := newWS(nil)
	if _, err := ApplyPatchTo(w, "*** Begin Patch\n*** End Patch\n"); err == nil {
		t.Error("an empty patch is a mistake, not a no-op")
	}
}

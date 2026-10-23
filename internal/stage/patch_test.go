package stage

import (
	"strings"
	"testing"
)

// The patch parser is the only way a caller can change files, so a bug here either loses an
// edit silently or corrupts the wrong line. These cases are written to fail loudly on both.

func TestParsePatchAddFile(t *testing.T) {
	ops, err := ParsePatch("*** Begin Patch\n*** Add File: a.go\n+package a\n+\n+func A() {}\n*** End Patch\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0].Kind != "add" || ops[0].Path != "a.go" {
		t.Fatalf("ops = %+v", ops)
	}
	// The blank line survives: a dropped blank line changes the file.
	if want := "package a\n\nfunc A() {}\n"; ops[0].Content != want {
		t.Errorf("content = %q, want %q", ops[0].Content, want)
	}
}

func TestParsePatchDelete(t *testing.T) {
	ops, err := ParsePatch("*** Begin Patch\n*** Delete File: gone.txt\n*** End Patch\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0].Kind != "delete" || ops[0].Path != "gone.txt" {
		t.Fatalf("ops = %+v", ops)
	}
}

func TestParsePatchMultipleFiles(t *testing.T) {
	p := "*** Begin Patch\n" +
		"*** Add File: new.txt\n+hello\n" +
		"*** Update File: old.txt\n@@ x\n keep\n-gone\n+kept\n" +
		"*** Delete File: dead.txt\n" +
		"*** End Patch\n"
	ops, err := ParsePatch(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 3 {
		t.Fatalf("got %d ops, want 3: %+v", len(ops), ops)
	}
	if ops[1].Kind != "update" || len(ops[1].Hunks) != 1 || ops[1].Hunks[0].Anchor != "x" {
		t.Errorf("update op = %+v", ops[1])
	}
}

// Prose before the patch is tolerated — a model often introduces it — but anything malformed
// INSIDE the markers is not.
func TestParsePatchToleratesLeadingProse(t *testing.T) {
	p := "Here is the change you asked for.\n\n*** Begin Patch\n*** Add File: x\n+1\n*** End Patch\n"
	ops, err := ParsePatch(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 {
		t.Fatalf("ops = %+v", ops)
	}
}

func TestParsePatchRejectsMalformed(t *testing.T) {
	cases := map[string]string{
		"no end":            "*** Begin Patch\n*** Add File: a\n+x\n",
		"add without plus":  "*** Begin Patch\n*** Add File: a\nx\n*** End Patch\n",
		"hunk without @@":   "*** Begin Patch\n*** Update File: a\n-x\n+y\n*** End Patch\n",
		"bad hunk prefix":   "*** Begin Patch\n*** Update File: a\n@@ h\n?x\n*** End Patch\n",
		"move unsupported":  "*** Begin Patch\n*** Move to: b\n*** End Patch\n",
		"nested begin":      "*** Begin Patch\n*** Begin Patch\n*** End Patch\n",
		"end without begin": "*** End Patch\n",
	}
	for name, p := range cases {
		if _, err := ParsePatch(p); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestApplyHunksReplaceLine(t *testing.T) {
	old := "a\nb\nc\n"
	got, err := ApplyHunks(old, []PatchHunk{{
		Lines: []string{" a", "-b", "+B", " c"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "a\nB\nc\n" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyHunksInsert(t *testing.T) {
	got, err := ApplyHunks("a\nc\n", []PatchHunk{{Lines: []string{" a", "+b"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "a\nb\nc\n" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyHunksDelete(t *testing.T) {
	got, err := ApplyHunks("a\nb\nc\n", []PatchHunk{{Lines: []string{" a", "-b", " c"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "a\nc\n" {
		t.Fatalf("got %q", got)
	}
}

// A hunk whose context appears twice must be REFUSED. Applying it to the first match is how a
// patch lands in the wrong function while reporting success.
func TestApplyHunksRefusesAmbiguousContext(t *testing.T) {
	old := "func A() {\n}\nfunc B() {\n}\n"
	_, err := ApplyHunks(old, []PatchHunk{{Lines: []string{" }", "-}", "+  x"}}})
	if err == nil {
		t.Fatal("ambiguous context must be refused")
	}
	if !strings.Contains(err.Error(), "more than once") {
		t.Errorf("err = %v, want it to say the context is ambiguous", err)
	}
}

// The anchor narrows the search, which is how a caller disambiguates repeated context.
func TestApplyHunksAnchorDisambiguates(t *testing.T) {
	// Two identical bodies; the anchor is what says WHICH one.
	old := "func A() {\n\tbody()\n}\nfunc B() {\n\tbody()\n}\n"
	got, err := ApplyHunks(old, []PatchHunk{{
		Anchor: "func B() {",
		Lines:  []string{" \tbody()", "+ \tmore()"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "func B() {\n\tbody()\n \tmore()\n}") {
		t.Errorf("the edit did not land in B:\n%s", got)
	}
	if !strings.Contains(got, "func A() {\n\tbody()\n}\n") {
		t.Errorf("A was modified:\n%s", got)
	}
}

// Without the anchor the same patch is ambiguous, which is the whole reason anchors exist.
func TestApplyHunksRepeatedBodyNeedsAnchor(t *testing.T) {
	old := "func A() {\n\tbody()\n}\nfunc B() {\n\tbody()\n}\n"
	_, err := ApplyHunks(old, []PatchHunk{{Lines: []string{" \tbody()", "+ \tmore()"}}})
	if err == nil {
		t.Fatal("two identical bodies without an anchor must be refused")
	}
	if !strings.Contains(err.Error(), "more than once") {
		t.Errorf("err = %v", err)
	}
}

func TestApplyHunksContextMismatch(t *testing.T) {
	_, err := ApplyHunks("a\nb\n", []PatchHunk{{Lines: []string{" a", "-c", "+C"}}})
	if err == nil {
		t.Fatal("a removal that does not match must be refused")
	}
}

// The final newline belongs to the file: a patch must not silently add or drop it.
func TestApplyHunksPreservesTrailingNewline(t *testing.T) {
	got, err := ApplyHunks("a\n", []PatchHunk{{Lines: []string{"-a", "+b"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "b\n" {
		t.Errorf("got %q, want a trailing newline", got)
	}

	noNL, err := ApplyHunks("a", []PatchHunk{{Lines: []string{"-a", "+b"}}})
	if err != nil {
		t.Fatal(err)
	}
	if noNL != "b" {
		t.Errorf("got %q, want no trailing newline added", noNL)
	}
}

// An empty line inside a hunk is a blank CONTEXT line, not noise.
func TestApplyHunksBlankContextLine(t *testing.T) {
	old := "a\n\nb\n"
	got, err := ApplyHunks(old, []PatchHunk{{Lines: []string{" a", " ", "-b", "+B"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "a\n\nB\n" {
		t.Fatalf("got %q", got)
	}
}

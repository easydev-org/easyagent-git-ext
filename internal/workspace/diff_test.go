package workspace

import (
	"strings"
	"testing"
)

// The diff is computed in-process because the stage has no checkout — that is the whole
// reason this package exists. These cases pin the behaviour a reviewer depends on.

func TestDiffLinesCountsChanges(t *testing.T) {
	old := []byte("a\nb\nc\n")
	next := []byte("a\nB\nc\n")

	body, adds, dels := DiffLines(old, next)
	if adds != 1 || dels != 1 {
		t.Fatalf("adds/dels = %d/%d, want 1/1\n%s", adds, dels, body)
	}
	if !strings.Contains(body, "-b\n") || !strings.Contains(body, "+B\n") {
		t.Errorf("body does not show the change:\n%s", body)
	}
	// Unchanged lines must be present and prefixed with a space.
	if !strings.Contains(body, " a\n") || !strings.Contains(body, " c\n") {
		t.Errorf("body lost unchanged context:\n%s", body)
	}
}

func TestDiffLinesPureInsert(t *testing.T) {
	body, adds, dels := DiffLines([]byte("a\nc\n"), []byte("a\nb\nc\n"))
	if adds != 1 || dels != 0 {
		t.Fatalf("adds/dels = %d/%d, want 1/0\n%s", adds, dels, body)
	}
}

func TestDiffLinesPureDelete(t *testing.T) {
	body, adds, dels := DiffLines([]byte("a\nb\nc\n"), []byte("a\nc\n"))
	if adds != 0 || dels != 1 {
		t.Fatalf("adds/dels = %d/%d, want 0/1\n%s", adds, dels, body)
	}
}

func TestDiffLinesIdentical(t *testing.T) {
	body, adds, dels := DiffLines([]byte("same\n"), []byte("same\n"))
	if adds != 0 || dels != 0 {
		t.Fatalf("identical content reported changes: %d/%d\n%s", adds, dels, body)
	}
}

// A trailing newline difference must be visible: a file that loses its final newline is
// a real change, and hiding it makes a "no diff" result wrong.
func TestDiffLinesTrailingNewline(t *testing.T) {
	_, adds, dels := DiffLines([]byte("a\n"), []byte("a"))
	if adds == 0 && dels == 0 {
		t.Error("dropping the trailing newline must register as a change")
	}
}

// Binary content is announced instead of dumped: NUL bytes in a transcript are useless
// to a reader.
func TestDiffLinesBinary(t *testing.T) {
	body, _, _ := DiffLines([]byte{0x00, 0x01}, []byte{0x00, 0x02})
	if !strings.Contains(body, "binary") {
		t.Errorf("binary diff body = %q, want it to say the content is binary", body)
	}
}

// A large change must not hang: the LCS table is bounded, and past the bound the diff
// degrades to whole-file replacement rather than allocating gigabytes.
func TestDiffLinesLargeChangeStaysBounded(t *testing.T) {
	const n = 5000
	old := make([]byte, 0, n*8)
	next := make([]byte, 0, n*8)
	for i := 0; i < n; i++ {
		old = append(old, []byte("old-line-xxxxxxxxxxxxxxxx\n")...)
		next = append(next, []byte("new-line-yyyyyyyyyyyyyyyy\n")...)
	}
	body, adds, dels := DiffLines(old, next)
	if adds == 0 || dels == 0 {
		t.Fatalf("a wholesale rewrite reported no change: %d/%d", adds, dels)
	}
	if len(body) == 0 {
		t.Error("empty body for a real change")
	}
}

// unifiedDiff marks added and deleted files the way a reviewer expects.
func TestUnifiedDiffHeaders(t *testing.T) {
	if got := string(unifiedDiff("a.txt", nil, []byte("new\n"), false)); !strings.Contains(got, "--- /dev/null") {
		t.Errorf("added file header = %q, want /dev/null on the old side", got)
	}
	if got := string(unifiedDiff("a.txt", []byte("old\n"), nil, false)); !strings.Contains(got, "+++ /dev/null") {
		t.Errorf("deleted file header = %q, want /dev/null on the new side", got)
	}
	got := string(unifiedDiff("a.txt", []byte("old\n"), []byte("new\n"), false))
	if !strings.Contains(got, "--- a/a.txt") || !strings.Contains(got, "+++ b/a.txt") {
		t.Errorf("modified file header = %q", got)
	}
}

func TestSplitKeepEnds(t *testing.T) {
	got := splitKeepEnds("a\nb\n")
	if len(got) != 2 || got[0] != "a\n" || got[1] != "b\n" {
		t.Fatalf("splitKeepEnds = %q", got)
	}
	// No trailing newline: the last element carries the remainder, which is what makes
	// the missing terminator visible in a diff.
	got = splitKeepEnds("a\nb")
	if len(got) != 2 || got[1] != "b" {
		t.Fatalf("splitKeepEnds (no trailing NL) = %q", got)
	}
}

func TestIsBinary(t *testing.T) {
	if isBinary([]byte("plain text\n")) {
		t.Error("text reported as binary")
	}
	if !isBinary([]byte{'a', 0x00, 'b'}) {
		t.Error("NUL byte not detected as binary")
	}
}

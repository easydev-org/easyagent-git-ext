package stage

import (
	"fmt"
	"strings"
)

// Line-level diff, implemented here rather than shelled out to `git diff`.
//
// The stage has no checkout: that is the entire point of it. A diff is what a reviewer
// needs before submitting, so it has to be computable from two byte slices.
//
// The algorithm is a longest-common-subsequence over lines with a size guard. A full
// Myers implementation would produce prettier diffs, but LCS is exact, short, and easy to
// reason about — and the guard matters more than the aesthetics: an edit to two sides of
// a 50k-line file must not turn into a 2.5-billion-cell table.
const (
	// diffMaxCells bounds the LCS table. Beyond it the diff degrades to "whole file
	// changed", which is still truthful — it just stops being minimal.
	diffMaxCells = 4_000_000
)

// DiffLines returns a unified-style diff body with counts of added and deleted lines.
//
// Binary inputs are announced, not rendered: dumping NUL bytes into a transcript is
// useless to a reader and worse than saying so.
func DiffLines(old, next []byte) (body string, additions, deletions int) {
	if isBinary(old) || isBinary(next) {
		return "(binary content differs)\n", 0, 0
	}
	a := splitKeepEnds(string(old))
	b := splitKeepEnds(string(next))

	ops := lcsDiff(a, b)
	var sb strings.Builder
	for _, op := range ops {
		switch op.kind {
		case opAdd:
			sb.WriteString("+")
			sb.WriteString(op.text)
			additions++
		case opDel:
			sb.WriteString("-")
			sb.WriteString(op.text)
			deletions++
		case opKeep:
			sb.WriteString(" ")
			sb.WriteString(op.text)
		}
	}
	return sb.String(), additions, deletions
}

// unifiedDiff renders one file's change with headers, the way a reviewer expects.
func unifiedDiff(path string, old, next []byte, binary bool) []byte {
	var b strings.Builder
	switch {
	case len(old) == 0 && len(next) == 0:
		return nil
	case len(old) == 0:
		fmt.Fprintf(&b, "--- /dev/null\n+++ b/%s\n", path)
	case len(next) == 0:
		fmt.Fprintf(&b, "--- a/%s\n+++ /dev/null\n", path)
	default:
		fmt.Fprintf(&b, "--- a/%s\n+++ b/%s\n", path, path)
	}
	if binary {
		b.WriteString("(binary content differs)\n")
		return []byte(b.String())
	}
	body, _, _ := DiffLines(old, next)
	b.WriteString(body)
	return []byte(b.String())
}

type opKind int

const (
	opKeep opKind = iota
	opAdd
	opDel
)

type diffOp struct {
	kind opKind
	text string
}

// lcsDiff computes an edit script from two line slices.
func lcsDiff(a, b []string) []diffOp {
	n, m := len(a), len(b)
	// Size guard: a prefix/suffix trim removes the common bulk cheaply, and if the
	// remainder is still too large the change is reported wholesale.
	pre := 0
	for pre < n && pre < m && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < n-pre && suf < m-pre && a[n-1-suf] == b[m-1-suf] {
		suf++
	}
	midA := a[pre : n-suf]
	midB := b[pre : m-suf]
	if (len(midA)+1)*(len(midB)+1) > diffMaxCells {
		out := make([]diffOp, 0, pre+len(midA)+len(midB)+suf)
		for _, l := range a[:pre] {
			out = append(out, diffOp{opKeep, l})
		}
		for _, l := range midA {
			out = append(out, diffOp{opDel, l})
		}
		for _, l := range midB {
			out = append(out, diffOp{opAdd, l})
		}
		for _, l := range a[n-suf:] {
			out = append(out, diffOp{opKeep, l})
		}
		return out
	}

	// Classic LCS table over the trimmed middle.
	ln, lm := len(midA), len(midB)
	dp := make([][]int, ln+1)
	for i := range dp {
		dp[i] = make([]int, lm+1)
	}
	for i := ln - 1; i >= 0; i-- {
		for j := lm - 1; j >= 0; j-- {
			if midA[i] == midB[j] {
				dp[i][j] = dp[i+1][j+1] + 1
				continue
			}
			if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	out := make([]diffOp, 0, pre+ln+lm+suf)
	for _, l := range a[:pre] {
		out = append(out, diffOp{opKeep, l})
	}
	i, j := 0, 0
	for i < ln && j < lm {
		switch {
		case midA[i] == midB[j]:
			out = append(out, diffOp{opKeep, midA[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			out = append(out, diffOp{opDel, midA[i]})
			i++
		default:
			out = append(out, diffOp{opAdd, midB[j]})
			j++
		}
	}
	for ; i < ln; i++ {
		out = append(out, diffOp{opDel, midA[i]})
	}
	for ; j < lm; j++ {
		out = append(out, diffOp{opAdd, midB[j]})
	}
	for _, l := range a[n-suf:] {
		out = append(out, diffOp{opKeep, l})
	}
	return out
}

// splitKeepEnds splits on newlines and keeps the terminator, so a diff renders with the
// right line boundaries and a missing trailing newline stays visible.
func splitKeepEnds(s string) []string {
	if s == "" {
		return nil
	}
	out := make([]string, 0, strings.Count(s, "\n")+1)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
